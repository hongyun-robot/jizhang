package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	"gorm.io/gorm"
	"jizhang/internal/wealthdata"
)

// Admission is an operator command, never a user API or automatic sync side effect.
// A successful sampling manifest is necessary; functional release acceptance
// must still be reviewed by the operator before running the command.
func WealthAdmissionEvidence(raw []byte, now time.Time, expectedCode ...string) (string, error) {
	var evidence struct {
		Product       string    `json:"product"`
		Status        string    `json:"status"`
		SourceVersion string    `json:"source_version"`
		CompletedAt   time.Time `json:"completed_at"`
		Proofs        struct {
			Cold       bool            `json:"cold_start"`
			Restart    bool            `json:"process_restart"`
			History    bool            `json:"fresh_anonymous_history"`
			Disclosure json.RawMessage `json:"new_disclosure_or_revision"`
		} `json:"proofs"`
		Slots []struct {
			ID       string    `json:"id"`
			Due      time.Time `json:"due_at"`
			Started  time.Time `json:"started_at"`
			Finished time.Time `json:"finished_at"`
			State    string    `json:"state"`
			CheckID  string    `json:"check_id"`
		} `json:"slots"`
		Checks []struct {
			ID       string `json:"id"`
			State    string `json:"state"`
			Count    int    `json:"count"`
			Hash     string `json:"sha256"`
			Envelope string `json:"public_envelope"`
		} `json:"checks"`
	}
	if len(raw) == 0 || len(raw) > 4*1024*1024 || json.Unmarshal(raw, &evidence) != nil || evidence.Status != "passed_sampling" || evidence.SourceVersion == "" || evidence.CompletedAt.IsZero() || evidence.CompletedAt.After(now) || !evidence.Proofs.Cold || !evidence.Proofs.Restart || !evidence.Proofs.History || len(evidence.Proofs.Disclosure) == 0 || string(evidence.Proofs.Disclosure) == "false" || len(evidence.Slots) != 12 {
		return "", ErrWealthCode
	}
	if _, ok := wealthdata.Catalog(evidence.Product); !ok {
		return "", ErrWealthCode
	}
	if len(expectedCode) > 0 {
		code, err := wealthCanonicalCode(expectedCode[0])
		if err != nil || evidence.Product != code {
			return "", ErrWealthCode
		}
	}
	checks := map[string]bool{}
	for _, c := range evidence.Checks {
		if _, exists := checks[c.ID]; exists || c.State != "succeeded" {
			return "", ErrWealthCode
		}
		checks[c.ID] = c.State == "succeeded" && c.Count > 0 && len(c.Hash) == 64 && c.Envelope != ""
	}
	var disclosure struct {
		Check string   `json:"check"`
		Dates []string `json:"dates"`
	}
	if json.Unmarshal(evidence.Proofs.Disclosure, &disclosure) != nil || !checks[disclosure.Check] || len(disclosure.Dates) == 0 {
		return "", ErrWealthCode
	}
	if !checks["cold-start"] || !checks["restart-fresh-context"] {
		return "", ErrWealthCode
	}
	first := evidence.Slots[0].Due.In(wealthdata.Shanghai)
	day := time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, wealthdata.Shanghai)
	weekday := false
	for i, slot := range evidence.Slots {
		due := day.AddDate(0, 0, i/4).Add(time.Duration([]int{8, 12, 18, 22}[i%4]) * time.Hour)
		if slot.ID != due.Format("2006-01-02")+"/"+strconv.Itoa(due.Hour()) || slot.CheckID != slot.ID {
			return "", ErrWealthCode
		}
		if !slot.Due.Equal(due) || slot.State != "succeeded" || !checks[slot.CheckID] || slot.Started.Before(due) || slot.Started.After(due.Add(5*time.Minute)) || slot.Finished.Before(slot.Started) || slot.Finished.After(now) {
			return "", ErrWealthCode
		}
		if due.Weekday() != time.Saturday && due.Weekday() != time.Sunday {
			weekday = true
		}
	}
	if !weekday {
		return "", ErrWealthCode
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func (s *WealthService) SetAdmission(code, status string, evidence []byte) error {
	if status != "enabled" && status != "retired" && status != "pending_validation" {
		return ErrWealthCode
	}
	version := ""
	var err error
	if status == "enabled" {
		version, err = WealthAdmissionEvidence(evidence, s.Now(), code)
		if err != nil {
			return err
		}
	}
	p, err := s.product(code)
	if err != nil {
		return err
	}
	return s.repo.DB().Transaction(func(tx *gorm.DB) error {
		row, err := s.repo.At(tx).Product(p.ID, true)
		if err != nil {
			return err
		}
		updates := map[string]any{"admission_status": status}
		if version != "" {
			updates["evidence_version"] = version
		}
		return tx.Model(row).Updates(updates).Error
	})
}
