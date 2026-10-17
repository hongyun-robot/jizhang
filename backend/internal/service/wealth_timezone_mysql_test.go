package service

import (
	"errors"
	"os"
	"testing"

	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"jizhang/internal/model"
	"jizhang/internal/suyin"
)

// Production uses loc=Local with Asia/Shanghai. UTC-only integration fixtures
// cannot expose a SQL DATE compared against an 08:00 driver timestamp.
func TestWealthMySQLShanghaiRepeatedNAVAndSameDayOverlay(t *testing.T) {
	raw := os.Getenv("WEALTH_MYSQL_DSN")
	if raw == "" {
		t.Skip("set WEALTH_MYSQL_DSN for isolated real MySQL test")
	}
	cfg, err := driver.ParseDSN(raw)
	mustOK(t, err)
	cfg.Loc = suyin.Shanghai
	t.Setenv("WEALTH_MYSQL_DSN", cfg.FormatDSN())
	e := newWealthMySQLEnv(t)
	p := e.opening(t, 0, "100", "1", "2026-10-09")
	quote := []suyin.NAV{{Date: fday("2026-10-15"), Value: dec("1.02142"), FetchedAt: e.now, SourceCode: "Y0408Ux"}}
	changed, err := e.svc.SavePublicNAVs(1, quote)
	mustOK(t, err)
	if !changed {
		t.Fatal("initial batch not saved")
	}
	changed, err = e.svc.SavePublicNAVs(1, quote)
	mustOK(t, err)
	if changed {
		t.Fatal("unchanged batch revised")
	}
	manual := WealthManualInput{RequestKey: uuid.NewString(), NAVDate: "2026-10-15", UnitNAV: "1.01", SourceNote: "test"}
	if _, err = e.svc.WriteManualNAV(e.users[0], 1, manual); !errors.Is(err, ErrWealthAutomatic) {
		t.Fatal("same-day automatic NAV missed", err)
	}
	manual.NAVDate = "2026-10-14"
	manual.RequestKey = uuid.NewString()
	first, err := e.svc.WriteManualNAV(e.users[0], 1, manual)
	mustOK(t, err)
	manual.RequestKey = uuid.NewString()
	manual.UnitNAV = "1.02"
	manual.ExpectedRevision = 1
	second, err := e.svc.WriteManualNAV(e.users[0], 1, manual)
	mustOK(t, err)
	if first.ID != second.ID {
		t.Fatal("same-day manual overlay duplicated")
	}
	mustOK(t, e.svc.RebuildPosition(p.PositionID))
	vals, err := e.svc.Valuations(e.users[0], p.PositionID, "2026-10-15", "2026-10-15")
	mustOK(t, err)
	if len(vals) != 1 {
		t.Fatal("inclusive date history lost", len(vals))
	}
	var count int64
	mustOK(t, e.db.Model(&model.WealthNAV{}).Count(&count).Error)
	if count != 1 {
		t.Fatal(count)
	}
}
