package service

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"

	"jizhang/internal/model"
	"jizhang/internal/repository"
)

// CSV 导出（specs/transactions「CSV 导出」：UTF-8 BOM、行数=筛选命中数）。

// csvBOM UTF-8 BOM，Excel 依此识别编码避免中文乱码。
var csvBOM = []byte{0xEF, 0xBB, 0xBF}

// csvHeader 导出列头（与流水页列表列一致）。
const csvHeader = "日期,类型,分类,账户,转入账户,金额,备注,来源,用途,券编号,余额影响,消费影响,平台 / 商家\r\n"

// plainNum 纯数字（含负数金额）。负数以 - 开头但不是公式，不参与公式前缀转义，
// 保证 Excel 中金额列仍可直接求和。
var plainNum = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

// csvCell 转义一个 CSV 单元格：
//  1. 以 = + - @ 开头的文本加前导单引号，防 CSV 公式注入；
//     纯数字（如 -100.00）豁免，保持数值语义；
//  2. 含逗号/引号/换行的按 RFC 4180 加双引号并翻倍内部引号。
func csvCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@", rune(s[0])) && !plainNum.MatchString(s) {
		s = "'" + s
	}
	if strings.ContainsAny(s, ",\"\r\n") {
		s = `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

func kindLabel(k model.TxnKind) string {
	switch k {
	case model.TxnExpense:
		return "支出"
	case model.TxnIncome:
		return "收入"
	case model.TxnTransfer:
		return "转账"
	case model.TxnRefund:
		return "退款"
	}
	return string(k)
}

func sourceLabel(s model.TxnSource) string {
	if s == model.SourceVoucherAuto {
		return "券到期自动退款"
	}
	if s == model.SourceRecurring {
		return "自动"
	}
	return "手动"
}

func purposeLabel(p model.TxnPurpose) string {
	switch p {
	case model.PurposeVoucherPurchase:
		return "购券"
	case model.PurposeVoucherRedemption:
		return "券核销"
	case model.PurposeVoucherRefund:
		return "券退款"
	case model.PurposeVoucherWriteoff:
		return "券作废"
	case model.PurposeFundPurchase:
		return "基金买入付款"
	case model.PurposeFundRedemption:
		return "基金赎回到账"
	case model.PurposeFundDividend:
		return "基金分红到账"
	}
	return "普通记账"
}

// catFullPaths 分类 id → 全路径（一级/二级，如「餐饮/午餐」）。
func catFullPaths(cats []model.Category) map[int64]string {
	name := make(map[int64]string, len(cats))
	for _, c := range cats {
		name[c.ID] = c.Name
	}
	paths := make(map[int64]string, len(cats))
	for _, c := range cats {
		if c.ParentID != nil {
			if p, ok := name[*c.ParentID]; ok {
				paths[c.ID] = p + "/" + c.Name
				continue
			}
		}
		paths[c.ID] = c.Name
	}
	return paths
}

// buildTxnCSV 组装 CSV：BOM + 表头 + 数据行（CRLF）。名称未解析时回退为 ID。
func buildTxnCSV(rows []model.Transaction, acctNames map[int64]string, catPaths map[int64]string) []byte {
	var b bytes.Buffer
	b.Write(csvBOM)
	b.WriteString(csvHeader)

	acct := func(id int64) string {
		if n, ok := acctNames[id]; ok {
			return n
		}
		return strconv.FormatInt(id, 10)
	}

	for _, t := range rows {
		cat := "—"
		if t.CategoryID != nil {
			if p, ok := catPaths[*t.CategoryID]; ok {
				cat = p
			}
		}
		transferTo := ""
		if t.TransferAccountID != nil {
			transferTo = acct(*t.TransferAccountID)
		}
		// 金额带方向（与列表展示一致）：收入/退款为正，支出/转账为负
		amt := t.Amount.StringFixed(2)
		if t.Kind != model.TxnIncome && t.Kind != model.TxnRefund {
			amt = "-" + amt
		}
		cells := []string{
			t.EntryDate.Format("2006-01-02"),
			kindLabel(t.Kind),
			cat,
			acct(t.AccountID),
			transferTo,
			amt,
			t.Note,
			sourceLabel(t.Source),
		}
		voucherID := ""
		if t.VoucherID != nil {
			voucherID = strconv.FormatInt(*t.VoucherID, 10)
		}
		balance, expense := t.Effects()
		cells = append(cells, purposeLabel(t.Purpose), voucherID, balance.StringFixed(2), expense.StringFixed(2), t.Platform)
		for i, cell := range cells {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(csvCell(cell))
		}
		b.WriteString("\r\n")
	}
	return b.Bytes()
}

// ExportCSV 导出当前筛选的全部流水为 CSV 文件内容（UTF-8 BOM，四档角色均可读）。
func (s *TransactionService) ExportCSV(actor *model.User, f repository.TxnFilter) ([]byte, error) {
	rows, err := s.ExportRows(actor, f)
	if err != nil {
		return nil, err
	}

	acctNames := make(map[int64]string)
	accs, err := s.deps.Accounts.ForOwner(actor.ID).List()
	if err != nil {
		return nil, err
	}
	for _, a := range accs {
		if ownsAccount(actor, a) {
			acctNames[a.ID] = a.Name
		} else {
			acctNames[a.ID] = "历史关联账户"
		}
	}

	cats, err := s.deps.Cats.ForOwner(actor.ID).List()
	if err != nil {
		return nil, err
	}

	return buildTxnCSV(rows, acctNames, catFullPaths(cats)), nil
}
