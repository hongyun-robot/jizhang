package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

const XLSXMaxRows = 100000
const XLSXTimeout = 50 * time.Second

var ErrExportTooLarge = errors.New("Excel 最多支持 100,000 条流水，请缩小筛选范围或选择 CSV")
var ErrExportAssociation = errors.New("流水关联资料不完整，无法导出，请核对后重试")

type TxnExportStore interface {
	ReadXLSX(context.Context, repository.TxnFilter, int) (*repository.TxnExportSnapshot, error)
}

// TxnExportRow is a permission-checked file projection, never a raw transaction.
type TxnExportRow struct {
	Date    time.Time
	Cells   [20]string
	Amount  float64
	Balance float64
	Expense float64
}

func (s *TransactionService) XLSXRows(ctx context.Context, actor *model.User, f repository.TxnFilter) ([]TxnExportRow, error) {
	if !CanView(actor) || actor.ID <= 0 {
		return nil, ErrTxnForbidden
	}
	if s.deps.Exports == nil {
		return nil, errors.New("导出读取服务不可用")
	}
	data, err := s.deps.Exports.ReadXLSX(ctx, s.personalFilter(actor, f), XLSXMaxRows)
	if err != nil {
		return nil, err
	}
	if len(data.Rows) > XLSXMaxRows {
		return nil, ErrExportTooLarge
	}
	accounts := map[int64]string{}
	for _, a := range data.Accounts {
		if ownsAccount(actor, a) {
			accounts[a.ID] = a.Name
		}
	}
	cats := []model.Category{}
	for _, c := range data.Categories {
		if c.OwnerID != nil && *c.OwnerID == actor.ID {
			cats = append(cats, c)
		}
	}
	paths := catFullPaths(cats)
	rows := make([]TxnExportRow, 0, len(data.Rows))
	for _, t := range data.Rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		row, err := projectExportRow(t, accounts, paths, data)
		if err != nil {
			return nil, fmt.Errorf("%w（流水 %d）", err, t.ID)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func projectExportRow(t model.Transaction, accounts, cats map[int64]string, data *repository.TxnExportSnapshot) (TxnExportRow, error) {
	r := TxnExportRow{Date: time.Date(t.EntryDate.Year(), t.EntryDate.Month(), t.EntryDate.Day(), 0, 0, 0, 0, time.UTC)}
	account, own := accounts[t.AccountID]
	to := ""
	historical := !own
	if !own {
		account = "历史关联账户"
	}
	if t.TransferAccountID != nil {
		var ok bool
		to, ok = accounts[*t.TransferAccountID]
		if !ok {
			to, historical = "历史关联账户", true
		}
	}
	category := "—"
	if t.CategoryID != nil {
		var ok bool
		category, ok = cats[*t.CategoryID]
		if !ok {
			if !historical {
				return r, ErrExportAssociation
			}
			category = "历史关联分类"
		}
	}
	amount := t.Amount
	if t.Kind != model.TxnIncome && t.Kind != model.TxnRefund {
		amount = amount.Neg()
	}
	balance, expense := t.Effects()
	r.Amount, _ = amount.Float64()
	r.Balance, _ = balance.Float64()
	r.Expense, _ = expense.Float64()
	r.Cells = [20]string{t.EntryDate.Format("2006-01-02"), kindLabel(t.Kind), category, account, to, amount.StringFixed(2), t.Note, sourceLabel(t.Source), purposeLabel(t.Purpose), "", balance.StringFixed(2), expense.StringFixed(2), t.Platform, strconv.FormatInt(t.ID, 10), "", t.PeriodKey}
	if t.RelatedTxnID != nil {
		if data.Related[*t.RelatedTxnID] {
			r.Cells[14] = strconv.FormatInt(*t.RelatedTxnID, 10)
		} else if historical {
			r.Cells[18] = "历史关联"
		} else {
			return r, ErrExportAssociation
		}
	}
	count := 0
	expectedKind := ""
	switch t.Purpose {
	case model.PurposeVoucherPurchase, model.PurposeVoucherRedemption, model.PurposeVoucherRefund, model.PurposeVoucherWriteoff:
		expectedKind = "voucher"
	case model.PurposeFundPurchase, model.PurposeFundRedemption, model.PurposeFundDividend:
		expectedKind = "fund"
	case model.PurposeGoldPurchase, model.PurposeGoldRedemption:
		expectedKind = "gold"
	case model.PurposeWealthPurchase, model.PurposeWealthRedemption, model.PurposeWealthDividend:
		expectedKind = "wealth"
	case model.PurposeWealthCashYield:
		expectedKind = "yield"
	}
	for _, ref := range []struct {
		kind, label, investment string
		id                      *int64
	}{
		{"voucher", "券事件", "", t.VoucherEventID}, {"fund", "基金事件", "基金", t.FundEventID},
		{"gold", "黄金事件", "黄金", t.GoldEventID}, {"wealth", "理财事件", "银行理财", t.WealthEventID},
		{"yield", "理财零钱收益记录", "理财零钱", t.WealthCashYieldID},
	} {
		if ref.id == nil {
			continue
		}
		count++
		if ref.kind != expectedKind {
			return r, ErrExportAssociation
		}
		link, ok := data.References[ref.kind][*ref.id]
		_, ownsLink := accounts[link.AccountID]
		valid := ok && ownsLink && (t.AccountID == link.AccountID || (t.TransferAccountID != nil && *t.TransferAccountID == link.AccountID))
		if ref.kind == "yield" {
			valid = valid && link.TxnID != nil && *link.TxnID == t.ID && t.AccountID == link.AccountID
		}
		if ref.investment != "" && ref.kind != "yield" {
			valid = valid && link.CashAccountID != nil
			if valid {
				_, cashOwned := accounts[*link.CashAccountID]
				valid = cashOwned && ((t.AccountID == link.AccountID && t.TransferAccountID != nil && *t.TransferAccountID == *link.CashAccountID) || (t.AccountID == *link.CashAccountID && t.TransferAccountID != nil && *t.TransferAccountID == link.AccountID))
			}
		}
		if !valid {
			if historical {
				r.Cells[18] = "历史关联"
				continue
			}
			return r, ErrExportAssociation
		}
		if ref.investment != "" && link.Code == "" {
			return r, ErrExportAssociation
		}
		r.Cells[16], r.Cells[17], r.Cells[18], r.Cells[19] = ref.investment, link.Code, ref.label, strconv.FormatInt(link.ID, 10)
		if ref.kind == "voucher" {
			if link.VoucherID == 0 {
				return r, ErrExportAssociation
			}
			r.Cells[9] = strconv.FormatInt(link.VoucherID, 10)
		}
	}
	if count > 1 || (count == 0 && (t.IsFundManaged() || t.IsGoldManaged() || t.IsWealthManaged() || t.IsVoucher())) {
		return r, ErrExportAssociation
	}
	return r, nil
}

// ExportXLSX returns a complete temporary file. The caller must remove it after
// transmission. Failures clean up here and never expose a partial attachment.
func (s *TransactionService) ExportXLSX(ctx context.Context, actor *model.User, f repository.TxnFilter) (string, error) {
	rows, err := s.XLSXRows(ctx, actor, f)
	if err != nil {
		return "", err
	}
	return writeTxnXLSX(ctx, rows)
}

func writeTxnXLSX(ctx context.Context, rows []TxnExportRow) (path string, err error) {
	if err = ctx.Err(); err != nil {
		return "", err
	}
	file, err := os.CreateTemp("", "jizhang-transactions-*.xlsx")
	if err != nil {
		return "", err
	}
	path = file.Name()
	if err = file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	book := excelize.NewFile()
	defer func() {
		closeErr := book.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(path)
			path = ""
		}
	}()
	const sheet = "流水明细"
	if err = book.SetSheetName("Sheet1", sheet); err != nil {
		return
	}
	dateFormat := "yyyy-mm-dd"
	dateStyle, err := book.NewStyle(&excelize.Style{CustomNumFmt: &dateFormat})
	if err != nil {
		return
	}
	numberStyle, err := book.NewStyle(&excelize.Style{NumFmt: 2})
	if err != nil {
		return
	}
	headerStyle, err := book.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return
	}
	sw, err := book.NewStreamWriter(sheet)
	if err != nil {
		return
	}
	if err = sw.SetPanes(&excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return
	}
	if err = sw.SetColWidth(1, 20, 20); err != nil {
		return
	}
	if err = sw.SetColWidth(7, 7, 40); err != nil {
		return
	}
	header := append(strings.Split(strings.TrimSuffix(csvHeader, "\r\n"), ","), "流水 ID", "关联原流水 ID", "所属账期", "投资类型", "产品代码", "关联记录类型", "关联记录 ID")
	cells := make([]any, 20)
	for i, h := range header {
		cells[i] = excelize.Cell{Value: h, StyleID: headerStyle}
	}
	if err = sw.SetRow("A1", cells); err != nil {
		return
	}
	for i, row := range rows {
		if err = ctx.Err(); err != nil {
			return
		}
		for col, value := range row.Cells {
			cells[col] = value
		}
		cells[0] = excelize.Cell{Value: row.Date, StyleID: dateStyle}
		cells[5] = excelize.Cell{Value: row.Amount, StyleID: numberStyle}
		cells[10] = excelize.Cell{Value: row.Balance, StyleID: numberStyle}
		cells[11] = excelize.Cell{Value: row.Expense, StyleID: numberStyle}
		if err = sw.SetRow(fmt.Sprintf("A%d", i+2), cells); err != nil {
			return
		}
	}
	if err = book.AutoFilter(sheet, fmt.Sprintf("A1:T%d", len(rows)+1), nil); err != nil {
		return
	}
	if err = sw.Flush(); err != nil {
		return
	}
	if err = ctx.Err(); err != nil {
		return
	}
	if err = book.SaveAs(path); err != nil {
		return
	}
	err = ctx.Err()
	return
}
