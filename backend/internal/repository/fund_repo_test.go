package repository

import (
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"jizhang/internal/model"
	"testing"
	"time"
)

func TestFundQuoteRevisionAndPrivatePositionKeys(t *testing.T) {
	db, e := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	pool, _ := db.DB()
	pool.SetMaxOpenConns(1)
	defer pool.Close()
	if e = db.AutoMigrate(&model.FundInstrument{}, &model.FundPosition{}, &model.FundNAV{}, &model.FundNAVRevision{}, &model.FundMutation{}); e != nil {
		t.Fatal(e)
	}
	r := NewFundRepo(db)
	i := model.FundInstrument{THSCode: "023564.OF", Ticker: "023564"}
	if e = r.SaveInstrument(&i); e != nil {
		t.Fatal(e)
	}
	date := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	n := model.FundNAV{InstrumentID: i.ID, NAVDate: date, UnitNAV: decimal.RequireFromString("1.19460001"), FetchedAt: date}
	changed, e := r.SaveNAV(&n, date)
	if e != nil || !changed {
		t.Fatal(changed, e)
	}
	n.ID = 0
	changed, e = r.SaveNAV(&n, date)
	if e != nil || changed {
		t.Fatal(changed, e)
	}
	n.UnitNAV = decimal.RequireFromString("1.19460002")
	changed, e = r.SaveNAV(&n, date)
	if e != nil || !changed {
		t.Fatal(changed, e)
	}
	rows, e := r.NAVs(i.ID)
	if e != nil || len(rows) != 1 || rows[0].Revision != 2 || rows[0].UnitNAV.String() != "1.19460002" {
		t.Fatal(rows, e)
	}
	var revisions int64
	db.Model(&model.FundNAVRevision{}).Count(&revisions)
	if revisions != 1 {
		t.Fatal(revisions)
	}
	for _, owner := range []int64{1, 2} {
		p := model.FundPosition{AccountID: owner, OwnerID: owner, InstrumentID: i.ID, ValuationBasis: "cny_otc_unit_nav"}
		if e = db.Create(&p).Error; e != nil {
			t.Fatal(e)
		}
	}
	duplicate := model.FundPosition{AccountID: 1, OwnerID: 1, InstrumentID: i.ID}
	if db.Create(&duplicate).Error == nil {
		t.Fatal("duplicate position allowed")
	}
	for _, owner := range []int64{1, 2} {
		m := model.FundMutation{OwnerID: owner, RequestKey: "same-key", Action: "opening", PayloadHash: "hash"}
		if e = db.Create(&m).Error; e != nil {
			t.Fatal(e)
		}
	}
	if db.Create(&model.FundMutation{OwnerID: 1, RequestKey: "same-key"}).Error == nil {
		t.Fatal("duplicate request allowed")
	}
}
