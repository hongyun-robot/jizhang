package service

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

func readExportBook(t *testing.T, s *TransactionService, actor *model.User, filter repository.TxnFilter) (*excelize.File, string) {
	t.Helper()
	path, err := s.ExportXLSX(context.Background(), actor, filter)
	mustOK(t, err)
	t.Cleanup(func() { _ = os.Remove(path) })
	book, err := excelize.OpenFile(path)
	mustOK(t, err)
	t.Cleanup(func() { _ = book.Close() })
	return book, path
}

func TestXLSXTypesTextAndEmpty(t *testing.T) {
	e, a, _, cat, _ := exportEnv(t)
	const id = int64(9007199254740993)
	note := "=SUM(A1:A2)\n中文,\"备注\"\t+@-"
	txn := model.Transaction{ID: id, Kind: model.TxnExpense, Amount: dec("9999999999.99"), AccountID: a.ID, CategoryID: &cat, EntryDate: mustDate(t, "2026-10-01"), PeriodKey: "2026-09", Note: note, Platform: "00123", Source: model.SourceManual}
	mustOK(t, e.accRepo.DB().Create(&txn).Error)
	book, path := readExportBook(t, e.txn, adminUser(), repository.TxnFilter{})
	if len(book.GetSheetList()) != 1 || book.GetSheetList()[0] != "流水明细" {
		t.Fatal(book.GetSheetList())
	}
	rows, err := book.GetRows("流水明细")
	mustOK(t, err)
	if len(rows) != 2 || len(rows[0]) != 20 {
		t.Fatalf("rows=%v", rows)
	}
	for cell, want := range map[string]string{"A2": "2026-10-01", "F2": "-9999999999.99", "G2": note, "M2": "00123", "N2": strconv.FormatInt(id, 10), "P2": "2026-09"} {
		got, err := book.GetCellValue("流水明细", cell)
		mustOK(t, err)
		if got != want {
			t.Errorf("%s=%q want %q", cell, got, want)
		}
	}
	for _, cell := range []string{"F2", "K2", "L2"} {
		kind, err := book.GetCellType("流水明细", cell)
		mustOK(t, err)
		if kind != excelize.CellTypeNumber && kind != excelize.CellTypeUnset {
			t.Fatalf("%s numeric type=%v", cell, kind)
		}
	}
	for _, cell := range []string{"G2", "M2", "N2", "P2"} {
		kind, err := book.GetCellType("流水明细", cell)
		mustOK(t, err)
		if kind != excelize.CellTypeInlineString && kind != excelize.CellTypeSharedString {
			t.Fatalf("%s text type=%v", cell, kind)
		}
		formula, err := book.GetCellFormula("流水明细", cell)
		mustOK(t, err)
		if formula != "" {
			t.Fatal("formula injection", formula)
		}
	}
	panes, err := book.GetPanes("流水明细")
	mustOK(t, err)
	if !panes.Freeze || panes.YSplit != 1 {
		t.Fatal(panes)
	}
	archive, err := zip.OpenReader(path)
	mustOK(t, err)
	defer archive.Close()
	for _, entry := range archive.File {
		if strings.Contains(entry.Name, "externalLink") || strings.Contains(entry.Name, "vbaProject") {
			t.Fatal("unexpected active content", entry.Name)
		}
		if entry.Name == "xl/worksheets/sheet1.xml" {
			f, err := entry.Open()
			mustOK(t, err)
			body, err := io.ReadAll(f)
			_ = f.Close()
			mustOK(t, err)
			if !strings.Contains(strings.ReplaceAll(string(body), "$", ""), `autoFilter ref="A1:T2"`) {
				t.Fatalf("missing full filter: %s", body)
			}
		}
	}
	filter := repository.TxnFilter{Keyword: "no matches"}
	empty, _ := readExportBook(t, e.txn, adminUser(), filter)
	emptyRows, err := empty.GetRows("流水明细")
	mustOK(t, err)
	if len(emptyRows) != 1 || len(emptyRows[0]) != 20 {
		t.Fatal(emptyRows)
	}
}

func TestXLSXMatchesFiltersRefundsAndOrder(t *testing.T) {
	e, base := transactionOrderEnv(t)
	_, err := e.txn.Reorder(adminUser(), CatalogOrderInput{OrderedIDs: []int64{base[3], base[1]}, PreviousIDs: []int64{base[1], base[3]}}, "xlsx-order")
	mustOK(t, err)
	filter := repository.TxnFilter{CustomOrder: true, Page: 2, PageSize: 1}
	got, err := e.txn.XLSXRows(context.Background(), adminUser(), filter)
	mustOK(t, err)
	want := transactionOrderIDs(t, e, 1)
	if len(got) != len(want) {
		t.Fatal("pagination limited export")
	}
	for i, id := range want {
		if got[i].Cells[13] != strconv.FormatInt(id, 10) {
			t.Fatal("order mismatch")
		}
	}
	be, a, _, cat, _ := exportEnv(t)
	exp := mustCreate(t, be.txn, adminUser(), be.expense(t, a.ID, cat, "100", "2026-09-01"))
	mustCreate(t, be.txn, adminUser(), TxnInput{Kind: model.TxnRefund, Amount: dec("20"), AccountID: a.ID, RelatedTxnID: exp.ID, EntryDate: mustDate(t, "2026-10-02"), Note: "退款"})
	filter = repository.TxnFilter{RelatedTxnID: exp.ID, Keyword: "退款"}
	got, err = be.txn.XLSXRows(context.Background(), adminUser(), filter)
	mustOK(t, err)
	if len(got) != 1 || got[0].Cells[14] != strconv.FormatInt(exp.ID, 10) || got[0].Amount != 20 || got[0].Expense != -20 {
		t.Fatal(got)
	}
	start, end, minAmount, maxAmount := "2026-09-01", "2026-09-30", "50", "150"
	filter = repository.TxnFilter{StartDate: &start, EndDate: &end, AmountMin: &minAmount, AmountMax: &maxAmount, AccountID: a.ID, CategoryID: cat, Kind: model.TxnExpense}
	got, err = be.txn.XLSXRows(context.Background(), adminUser(), filter)
	mustOK(t, err)
	if len(got) != 1 || got[0].Cells[13] != strconv.FormatInt(exp.ID, 10) {
		t.Fatal(got)
	}
}

func TestXLSXInvestmentReferencesAndFailures(t *testing.T) {
	e, cash, _, _, _ := exportEnv(t)
	db := e.accRepo.DB()
	mustOK(t, db.AutoMigrate(&model.FundInstrument{}, &model.FundPosition{}, &model.FundEvent{}, &model.GoldPosition{}, &model.GoldEvent{}, &model.WealthProduct{}, &model.WealthPosition{}, &model.WealthEvent{}, &model.WealthCashYield{}, &model.Voucher{}, &model.VoucherEvent{}))
	owner := int64(1)
	asset := model.Account{OwnerID: &owner, Name: "投资", Type: model.AccountAsset, Status: model.StatusActive}
	mustOK(t, db.Create(&asset).Error)
	instrument := model.FundInstrument{THSCode: "001234.OF", Ticker: "001234"}
	mustOK(t, db.Create(&instrument).Error)
	fp := model.FundPosition{OwnerID: 1, AccountID: asset.ID, InstrumentID: instrument.ID}
	mustOK(t, db.Create(&fp).Error)
	fe := model.FundEvent{OwnerID: 1, AccountID: asset.ID, PositionID: fp.ID, CashAccountID: &cash.ID}
	mustOK(t, db.Create(&fe).Error)
	gp := model.GoldPosition{OwnerID: 1, AccountID: asset.ID, ProductKey: "ccb:personal-gold-accumulation:CNY"}
	mustOK(t, db.Create(&gp).Error)
	ge := model.GoldEvent{OwnerID: 1, AccountID: asset.ID, CashAccountID: &cash.ID}
	mustOK(t, db.Create(&ge).Error)
	product := model.WealthProduct{Code: "J07241"}
	mustOK(t, db.Create(&product).Error)
	wp := model.WealthPosition{OwnerID: 1, AccountID: asset.ID, ProductID: product.ID}
	mustOK(t, db.Create(&wp).Error)
	we := model.WealthEvent{OwnerID: 1, AccountID: asset.ID, PositionID: wp.ID, CashAccountID: &cash.ID}
	mustOK(t, db.Create(&we).Error)
	for _, c := range []struct {
		purpose            model.TxnPurpose
		fund, gold, wealth *int64
		code, label        string
	}{{model.PurposeFundPurchase, &fe.ID, nil, nil, instrument.THSCode, "基金事件"}, {model.PurposeGoldRedemption, nil, &ge.ID, nil, gp.ProductKey, "黄金事件"}, {model.PurposeWealthRedemption, nil, nil, &we.ID, product.Code, "理财事件"}} {
		txn := model.Transaction{Kind: model.TxnTransfer, Amount: dec("123.45"), AccountID: cash.ID, TransferAccountID: &asset.ID, EntryDate: mustDate(t, "2026-10-02"), PeriodKey: "2026-10", Purpose: c.purpose, FundEventID: c.fund, GoldEventID: c.gold, WealthEventID: c.wealth}
		mustOK(t, db.Create(&txn).Error)
		rows, err := e.txn.XLSXRows(context.Background(), adminUser(), repository.TxnFilter{})
		mustOK(t, err)
		if rows[0].Cells[17] != c.code || rows[0].Cells[18] != c.label || rows[0].Expense != 0 {
			t.Fatal(rows[0])
		}
	}
	for i, kind := range []model.TxnKind{model.TxnIncome, model.TxnExpense} {
		txn := model.Transaction{Kind: kind, Amount: dec("0.28"), AccountID: cash.ID, EntryDate: mustDate(t, "2026-10-03"), Purpose: model.PurposeWealthCashYield}
		mustOK(t, db.Create(&txn).Error)
		y := model.WealthCashYield{OwnerID: 1, AccountID: cash.ID, ProductID: product.ID, TxnID: &txn.ID, NAVDate: mustDate(t, fmtDate(i+1))}
		mustOK(t, db.Create(&y).Error)
		mustOK(t, db.Exec("UPDATE transactions SET wealth_cash_yield_id=? WHERE id=?", y.ID, txn.ID).Error)
		rows, err := e.txn.XLSXRows(context.Background(), adminUser(), repository.TxnFilter{})
		mustOK(t, err)
		if rows[0].Cells[18] != "理财零钱收益记录" || rows[0].Cells[17] != product.Code || rows[0].Expense != 0 || (i == 1 && rows[0].Balance != -0.28) {
			t.Fatal(rows[0])
		}
	}
	// A missing required product is an error rather than a successful blank code.
	mustOK(t, db.Delete(&instrument).Error)
	path, err := e.txn.ExportXLSX(context.Background(), adminUser(), repository.TxnFilter{})
	if !errors.Is(err, ErrExportAssociation) || path != "" {
		t.Fatal(path, err)
	}
	mustOK(t, db.Migrator().DropTable(&model.FundEvent{}))
	if _, err = e.txn.XLSXRows(context.Background(), adminUser(), repository.TxnFilter{}); err == nil {
		t.Fatal("SQL error silently ignored")
	}
}

func fmtDate(day int) string { return "2026-10-0" + strconv.Itoa(day) }

func TestXLSXScopeHistoryAndCancellation(t *testing.T) {
	e, a, _, cat, _ := exportEnv(t)
	db := e.accRepo.DB()
	foreign := model.Account{OwnerID: testOwner(2), Name: "他人隐私", Type: model.AccountAsset}
	mustOK(t, db.Create(&foreign).Error)
	own := model.Transaction{Kind: model.TxnExpense, Amount: dec("12"), AccountID: a.ID, CategoryID: &cat, EntryDate: mustDate(t, "2026-10-02")}
	mustOK(t, db.Create(&own).Error)
	other := own
	other.ID = 0
	other.AccountID = foreign.ID
	mustOK(t, db.Create(&other).Error)
	history := model.Transaction{Kind: model.TxnTransfer, Amount: dec("4"), AccountID: foreign.ID, TransferAccountID: &a.ID, EntryDate: own.EntryDate}
	mustOK(t, db.Create(&history).Error)
	mustOK(t, db.Exec("CREATE TABLE catalog_before_transactions (id BIGINT PRIMARY KEY)").Error)
	mustOK(t, db.Exec("INSERT INTO catalog_before_transactions VALUES (?)", history.ID).Error)
	for _, role := range []model.Role{model.RoleAdmin, model.RoleRecorder, model.RoleMember, model.RoleViewer} {
		rows, err := e.txn.XLSXRows(context.Background(), &model.User{ID: 1, Role: role}, repository.TxnFilter{PersonalOwnerID: 2})
		mustOK(t, err)
		if len(rows) != 2 || rows[0].Cells[3] != "历史关联账户" || strings.Contains(strings.Join(rows[0].Cells[:], ","), foreign.Name) {
			t.Fatal(rows)
		}
	}
	if _, err := e.txn.XLSXRows(context.Background(), nil, repository.TxnFilter{}); !errors.Is(err, ErrTxnForbidden) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if path, err := e.txn.ExportXLSX(ctx, adminUser(), repository.TxnFilter{}); path != "" || !errors.Is(err, context.Canceled) {
		t.Fatal(path, err)
	}
	dir := t.TempDir()
	t.Setenv("TMP", dir)
	if path, err := writeTxnXLSX(ctx, []TxnExportRow{{}}); path != "" || !errors.Is(err, context.Canceled) {
		t.Fatal(path, err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "jizhang-transactions-*"))
	mustOK(t, err)
	if len(files) > 0 {
		t.Fatal(files)
	}
}

type snapshotStoreFunc func(context.Context, repository.TxnFilter, int) (*repository.TxnExportSnapshot, error)

func (f snapshotStoreFunc) ReadXLSX(ctx context.Context, q repository.TxnFilter, n int) (*repository.TxnExportSnapshot, error) {
	return f(ctx, q, n)
}

func TestXLSXLimitBeforeProjection(t *testing.T) {
	s := NewTransactionService(TxnDeps{Exports: snapshotStoreFunc(func(context.Context, repository.TxnFilter, int) (*repository.TxnExportSnapshot, error) {
		return &repository.TxnExportSnapshot{Rows: make([]model.Transaction, XLSXMaxRows+1)}, nil
	})})
	if _, err := s.XLSXRows(context.Background(), adminUser(), repository.TxnFilter{}); !errors.Is(err, ErrExportTooLarge) {
		t.Fatal(err)
	}
}

func TestXLSXVoucherEffectsAndResourceCleanup(t *testing.T) {
	e := newVoucherEnv(t)
	v := e.buy(t, "89", 1, "")
	e.op(t, v.ID, model.VoucherRedeem, "2026-11-02", 1)
	rows, err := e.txn.XLSXRows(context.Background(), adminUser(), repository.TxnFilter{})
	mustOK(t, err)
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	var balance, expense float64
	for _, r := range rows {
		balance += r.Balance
		expense += r.Expense
		if r.Cells[9] != strconv.FormatInt(v.ID, 10) || r.Cells[18] != "券事件" || r.Cells[19] == "" {
			t.Fatal(r)
		}
	}
	if balance != -89 || expense != 89 {
		t.Fatal("double counted consumption", balance, expense)
	}
	e.balance(t, "911")
	dir := t.TempDir()
	t.Setenv("TMP", dir)
	ctx := &cancelDuringEncoding{Context: context.Background()}
	if path, err := writeTxnXLSX(ctx, []TxnExportRow{{}, {}}); path != "" || !errors.Is(err, context.Canceled) {
		t.Fatal(path, err)
	}
	files, err := os.ReadDir(dir)
	mustOK(t, err)
	if len(files) != 0 {
		t.Fatal("temporary files leaked", files)
	}
	t.Setenv("TMP", filepath.Join(dir, "missing"))
	if path, err := writeTxnXLSX(context.Background(), nil); path != "" || err == nil {
		t.Fatal("temporary storage failure was successful", path, err)
	}
}

type cancelDuringEncoding struct {
	context.Context
	checks int
}

func (c *cancelDuringEncoding) Err() error {
	c.checks++
	if c.checks >= 2 {
		return context.Canceled
	}
	return nil
}
