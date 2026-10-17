package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"jizhang/internal/model"
	"jizhang/internal/suyin"
)

func qualificationFixture() map[string]any {
	start := time.Date(2026, 10, 9, 0, 0, 0, 0, suyin.Shanghai)
	checks := []map[string]any{}
	addCheck := func(id string) {
		checks = append(checks, map[string]any{"id": id, "state": "succeeded", "count": 265, "sha256": strings.Repeat("a", 64), "public_envelope": "public-test.json"})
	}
	addCheck("cold-start")
	addCheck("restart-fresh-context")
	slots := []map[string]any{}
	for day := 0; day < 3; day++ {
		for _, hour := range []int{8, 12, 18, 22} {
			due := start.AddDate(0, 0, day).Add(time.Duration(hour) * time.Hour)
			id := due.Format("2006-01-02") + fmt.Sprintf("/%d", hour)
			addCheck(id)
			slots = append(slots, map[string]any{"id": id, "due_at": due, "started_at": due.Add(time.Second), "finished_at": due.Add(15 * time.Second), "state": "succeeded", "check_id": id})
		}
	}
	return map[string]any{"product": "J07241", "source_version": "test-fixture", "status": "passed_sampling", "completed_at": start.AddDate(0, 0, 3), "proofs": map[string]any{"cold_start": true, "process_restart": true, "fresh_anonymous_history": true, "new_disclosure_or_revision": map[string]any{"check": slots[0]["id"], "dates": []string{"2026-10-09"}}}, "slots": slots, "checks": checks}
}
func TestWealthAdmissionRequiresCompleteRealTimeManifestAndExplicitOperatorWrite(t *testing.T) {
	now := fday("2026-10-16")
	good, _ := json.Marshal(qualificationFixture())
	if hash, err := WealthAdmissionEvidence(good, now); err != nil || len(hash) != 64 {
		t.Fatal(hash, err)
	}
	for _, mutate := range []func(map[string]any){func(e map[string]any) { e["status"] = "running" }, func(e map[string]any) { e["slots"] = e["slots"].([]map[string]any)[:11] }, func(e map[string]any) { e["proofs"].(map[string]any)["new_disclosure_or_revision"] = nil }, func(e map[string]any) { e["proofs"].(map[string]any)["process_restart"] = false }, func(e map[string]any) { e["slots"].([]map[string]any)[2]["state"] = "missed" }, func(e map[string]any) { e["slots"].([]map[string]any)[0]["started_at"] = now }, func(e map[string]any) { e["slots"].([]map[string]any)[1]["check_id"] = "cold-start" }} {
		fixture := qualificationFixture()
		mutate(fixture)
		raw, _ := json.Marshal(fixture)
		if _, err := WealthAdmissionEvidence(raw, now); err == nil {
			t.Fatal("incomplete admission evidence accepted", fixture)
		}
	}
	e := newWealthEnv(t)
	mustOK(t, e.db.Model(&model.WealthProduct{}).Where("id = ?", 1).Updates(map[string]any{"admission_status": "pending_validation", "evidence_version": ""}).Error)
	catalog, err := e.svc.Catalog(e.users[0])
	mustOK(t, err)
	if len(catalog) != 0 {
		t.Fatal(catalog)
	}
	_, err = e.svc.CreateOpening(context.Background(), e.users[0], e.wealths[0], wealthOpening("1", "1", "2026-10-09"))
	if !errors.Is(err, ErrWealthCode) {
		t.Fatal(err)
	}
	if err = e.svc.SetAdmission("J07241", "enabled", nil); err == nil {
		t.Fatal("empty proof enabled product")
	}
	mustOK(t, e.svc.SetAdmission("J07241", "enabled", good))
	r := e.opening(t, 0, "100", "1", "2026-10-09")
	mustOK(t, e.svc.SetAdmission("J07241", "retired", nil))
	_, err = e.svc.AmendOpening(context.Background(), e.users[0], r.PositionID, wealthOpening("101", "1", "2026-10-09"))
	mustOK(t, err)
	_, err = e.svc.Operate(context.Background(), e.users[0], r.PositionID, WealthInput{RequestKey: "retired-buy", Kind: "buy", Shares: "1", Amount: "1", TradeNAV: "1", CashDate: "2026-10-10", EffectiveDate: "2026-10-10", CashAccountID: e.banks[0]})
	if !errors.Is(err, ErrWealthCode) {
		t.Fatal(err)
	}
	_, err = e.svc.Operate(context.Background(), e.users[0], r.PositionID, WealthInput{RequestKey: "retired-disposal", Kind: "redeem", Shares: "1", Amount: "1", EffectiveDate: "2026-10-10"})
	mustOK(t, err)
}
