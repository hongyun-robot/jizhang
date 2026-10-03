package service

import (
	"bytes"
	"strings"
	"testing"

	"jizhang/internal/model"
	"jizhang/internal/repository"
)

// exportEnv 搭流水后导出（复用预算测试环境的真实仓储）。
func exportEnv(t *testing.T) (*budgetEnv, model.Account, model.Account, int64, int64) {
	t.Helper()
	e := newBudgetEnv(t)
	a1, a2 := e.seedAccounts(t)
	_, lunch, _, _, _, salary := e.seedCats(t)
	return e, a1, a2, lunch, salary
}

// csvDataLines 去掉 BOM 与表头后的数据行数。
func csvDataLines(t *testing.T, csv []byte) []string {
	t.Helper()
	if !bytes.HasPrefix(csv, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatalf("缺少 UTF-8 BOM，首三字节 = % X", csv[:min(3, len(csv))])
	}
	body := string(csv[3:])
	lines := strings.Split(strings.TrimSuffix(body, "\r\n"), "\r\n")
	if len(lines) < 1 {
		t.Fatal("CSV 为空")
	}
	return lines[1:] // 去表头
}

func TestExportCSVBOMHeaderAndRowCount(t *testing.T) {
	e, a1, a2, lunch, salary := exportEnv(t)

	mustCreate(t, e.txn, adminUser(), e.expense(t, a1.ID, lunch, "12.50", "2026-10-01"))
	mustCreate(t, e.txn, adminUser(), e.expense(t, a1.ID, lunch, "30.00", "2026-10-02"))
	in := TxnInput{Kind: model.TxnIncome, Amount: dec("1000"), AccountID: a2.ID,
		CategoryID: salary, EntryDate: mustDate(t, "2026-10-05"), Note: "十月工资"}
	mustCreate(t, e.txn, adminUser(), in)

	// 表头与列表列一致
	csv, err := e.txn.ExportCSV(adminUser(), repository.TxnFilter{})
	mustOK(t, err)
	head := strings.Split(string(csv[3:]), "\r\n")[0]
	want := "日期,类型,分类,账户,转入账户,金额,备注,来源"
	if head != want {
		t.Fatalf("表头 = %q, want %q", head, want)
	}

	// 行数 = 筛选命中数（与 List 的 total 对齐）
	_, total, err := e.txn.List(adminUser(), repository.TxnFilter{})
	mustOK(t, err)
	lines := csvDataLines(t, csv)
	if int64(len(lines)) != total {
		t.Fatalf("导出行数 = %d, 筛选命中 = %d", len(lines), total)
	}

	// 内容与列表一致：分类全路径、金额带方向
	all := strings.Join(lines, "\n")
	for _, want := range []string{"2026-10-01,支出,餐饮/午餐,现金,,-12.50", "十月工资", "收入"} {
		if !strings.Contains(all, want) {
			t.Errorf("导出内容缺少 %q:\n%s", want, all)
		}
	}
}

// TestExportCSVFormulaEscape 公式前缀转义防 CSV 注入；负数金额豁免保持数值语义。
func TestExportCSVFormulaEscape(t *testing.T) {
	e, a1, _, lunch, _ := exportEnv(t)

	for _, note := range []string{"=1+1", "+8613800138000", "-2+3+cmd", "@SUM(A1)", "普通备注"} {
		in := e.expense(t, a1.ID, lunch, "100.00", "2026-10-03")
		in.Note = note
		mustCreate(t, e.txn, adminUser(), in)
	}

	csv, err := e.txn.ExportCSV(adminUser(), repository.TxnFilter{})
	mustOK(t, err)
	body := string(csv[3:])

	for _, want := range []string{
		"'=1+1",               // = 转义
		"'+8613800138000",     // + 转义
		"'-2+3+cmd",           // - 转义（非纯数字）
		"'@SUM(A1)",           // @ 转义
		",-100.00,",           // 纯数字负金额不转义
		"2026-10-03,支出,餐饮/午餐", // 其余正常
	} {
		if !strings.Contains(body, want) {
			t.Errorf("缺少 %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "'-100.00") {
		t.Error("纯数字金额不应被公式转义")
	}
}

// TestExportCSVEscapeSpecialChars 逗号/引号/换行按 RFC4180 加引号。
func TestExportCSVEscapeSpecialChars(t *testing.T) {
	e, a1, _, lunch, _ := exportEnv(t)

	in := e.expense(t, a1.ID, lunch, "8.00", "2026-10-04")
	in.Note = `早餐,加蛋"和牛奶"`
	mustCreate(t, e.txn, adminUser(), in)

	csv, err := e.txn.ExportCSV(adminUser(), repository.TxnFilter{})
	mustOK(t, err)
	if !strings.Contains(string(csv[3:]), `"早餐,加蛋""和牛奶"""`) {
		t.Errorf("逗号/引号未按 RFC4180 转义:\n%s", csv[3:])
	}
}

// TestExportCSVFilterMatchesList 导出行数必须等于当前筛选命中数。
func TestExportCSVFilterMatchesList(t *testing.T) {
	e, a1, _, lunch, _ := exportEnv(t)

	mustCreate(t, e.txn, adminUser(), e.expense(t, a1.ID, lunch, "12.50", "2026-10-01"))
	in2 := e.expense(t, a1.ID, lunch, "30.00", "2026-10-02")
	in2.Note = "外卖炸鸡"
	mustCreate(t, e.txn, adminUser(), in2)
	in3 := e.expense(t, a1.ID, lunch, "66.00", "2026-10-03")
	in3.Note = "外卖奶茶"
	mustCreate(t, e.txn, adminUser(), in3)

	kw := "外卖"
	_, total, err := e.txn.List(adminUser(), repository.TxnFilter{Keyword: kw})
	mustOK(t, err)
	csv, err := e.txn.ExportCSV(adminUser(), repository.TxnFilter{Keyword: kw})
	mustOK(t, err)
	lines := csvDataLines(t, csv)
	if int64(len(lines)) != total {
		t.Fatalf("筛选「%s」导出行数 = %d, 命中 = %d", kw, len(lines), total)
	}
	if total != 2 {
		t.Fatalf("筛选命中 = %d, want 2", total)
	}
	for _, l := range lines {
		if !strings.Contains(l, kw) {
			t.Errorf("导出行未命中筛选: %s", l)
		}
	}

	// 日期区间筛选同样对齐
	start, end := "2026-10-02", "2026-10-03"
	_, dTotal, err := e.txn.List(adminUser(), repository.TxnFilter{StartDate: &start, EndDate: &end})
	mustOK(t, err)
	dCsv, err := e.txn.ExportCSV(adminUser(), repository.TxnFilter{StartDate: &start, EndDate: &end})
	mustOK(t, err)
	if got := len(csvDataLines(t, dCsv)); int64(got) != dTotal {
		t.Fatalf("区间筛选导出行数 = %d, 命中 = %d", got, dTotal)
	}
}
