package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"jizhang/internal/wealthdata"
)

func (s *WealthService) SavePublicNAVs(productID int64, input []wealthdata.NAV, lease ...string) (bool, error) {
	product, err := s.repo.Product(productID, false)
	if err != nil {
		return false, err
	}
	if product == nil {
		return false, ErrWealthCode
	}
	if len(input) == 0 || len(input) > 9999 {
		return false, ErrWealthInvalid
	}
	rows := make([]model.WealthNAV, 0, len(input))
	dates := map[string]model.WealthNAV{}
	for _, n := range input {
		date := wealthDay(n.Date)
		key := date.Format("2006-01-02")
		if date.Before(wealthDay(product.InceptionDate)) || date.After(wealthToday(s.Now())) || !n.Value.IsPositive() || n.Value.GreaterThan(maxAmount) || !n.Value.Equal(n.Value.Round(8)) || n.SourceCode != product.SourceCode || n.FetchedAt.IsZero() || n.FetchedAt.After(s.Now().Add(5*time.Minute)) {
			return false, ErrWealthInvalid
		}
		if n.TotalValue != nil && (!n.TotalValue.IsPositive() || !n.TotalValue.Equal(n.TotalValue.Round(8)) || n.TotalValue.GreaterThan(maxAmount)) {
			return false, ErrWealthInvalid
		}
		row := model.WealthNAV{ProductID: productID, NAVDate: date, UnitNAV: n.Value, TotalNAV: n.TotalValue, FetchedAt: n.FetchedAt}
		if old, ok := dates[key]; ok {
			a, _ := json.Marshal(old)
			b, _ := json.Marshal(row)
			if string(a) != string(b) {
				return false, ErrWealthInvalid
			}
			continue
		}
		dates[key] = row
		rows = append(rows, row)
	}
	owners, changed, err := s.repo.SaveNAVBatch(productID, rows, s.Now(), lease...)
	if err != nil {
		return false, err
	}
	if changed {
		for _, owner := range owners {
			s.changed(owner)
		}
		s.rebuildProduct(0, productID)
	}
	return changed, nil
}
func (s *WealthService) rebuildProduct(owner, product int64) {
	rows, err := s.repo.PositionsForProduct(owner, product)
	if err != nil {
		return
	}
	for _, p := range rows {
		_ = s.RebuildPosition(p.ID)
	} // pending remains durable on a derived failure
}

type WealthManualInput struct {
	RequestKey       string `json:"request_key"`
	NAVDate          string `json:"nav_date"`
	UnitNAV          string `json:"unit_nav"`
	SourceNote       string `json:"source_note"`
	ExpectedRevision int64  `json:"expected_revision"`
	Withdraw         bool   `json:"withdraw"`
}
type WealthManualReceipt struct {
	ID        int64 `json:"id"`
	ProductID int64 `json:"product_id"`
	Revision  int64 `json:"revision"`
}
type WealthManualView struct {
	model.WealthManualNAV
	AutomaticReplaced bool `json:"automatic_replaced"`
	Effective         bool `json:"effective"`
}

func (s *WealthService) registeredProduct(actor *model.User, id int64) (*model.WealthProduct, error) {
	if err := wealthActor(actor); err != nil {
		return nil, err
	}
	p, err := s.repo.Product(id, false)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrWealthCode
	}
	has, err := s.repo.HasProduct(actor.ID, id)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrWealthUnregistered
	}
	return p, nil
}
func (s *WealthService) ManualNAVs(actor *model.User, product int64) ([]WealthManualView, error) {
	if _, err := s.registeredProduct(actor, product); err != nil {
		return nil, err
	}
	manuals, err := s.repo.ManualNAVs(actor.ID, product)
	if err != nil {
		return nil, err
	}
	navs, err := s.repo.NAVs(product)
	if err != nil {
		return nil, err
	}
	automatic := map[string]bool{}
	for _, n := range navs {
		automatic[wealthDay(n.NAVDate).Format("2006-01-02")] = true
	}
	rows := []WealthManualView{}
	for _, n := range manuals {
		replaced := automatic[wealthDay(n.NAVDate).Format("2006-01-02")]
		rows = append(rows, WealthManualView{n, replaced, !replaced && !n.Withdrawn})
	}
	return rows, nil
}
func (s *WealthService) ManualAudits(actor *model.User, product int64) ([]model.WealthManualAudit, error) {
	if _, err := s.registeredProduct(actor, product); err != nil {
		return nil, err
	}
	return s.repo.ManualAudits(actor.ID, product)
}
func (s *WealthService) EffectiveNAVs(actor *model.User, product int64) ([]model.WealthQuote, error) {
	if err := wealthActor(actor); err != nil {
		return nil, err
	}
	p, err := s.repo.Product(product, false)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrWealthCode
	}
	has, err := s.repo.HasProduct(actor.ID, product)
	if err != nil {
		return nil, err
	}
	if !has && p.AdmissionStatus != "enabled" {
		return nil, ErrWealthUnregistered
	}
	navs, err := s.repo.NAVs(product)
	if err != nil {
		return nil, err
	}
	manuals, err := s.repo.ManualNAVs(actor.ID, product)
	if err != nil {
		return nil, err
	}
	return wealthQuotes(repository.WealthData{NAVs: navs, ManualNAVs: manuals}, actor.ID, product), nil
}

func (s *WealthService) WriteManualNAV(actor *model.User, product int64, in WealthManualInput) (WealthManualReceipt, error) {
	if err := wealthActor(actor); err != nil {
		return WealthManualReceipt{}, err
	}
	in.RequestKey = strings.TrimSpace(in.RequestKey)
	in.SourceNote = strings.TrimSpace(in.SourceNote)
	if in.RequestKey == "" || len(in.RequestKey) > 128 || in.ExpectedRevision < 0 || utf8.RuneCountInString(in.SourceNote) < 1 || utf8.RuneCountInString(in.SourceNote) > 255 {
		return WealthManualReceipt{}, ErrWealthInvalid
	}
	date, err := wealthDate(in.NAVDate, s.Now())
	if err != nil {
		return WealthManualReceipt{}, err
	}
	price, err := wealthNumber(in.UnitNAV, in.Withdraw, 8)
	if err != nil {
		return WealthManualReceipt{}, err
	}
	in.UnitNAV = price.String()
	payload := in
	payload.RequestKey = ""
	body, _ := json.Marshal(struct {
		Product int64
		Input   WealthManualInput
	}{product, payload})
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	var result WealthManualReceipt
	wrote := false
	err = s.repo.DB().Transaction(func(tx *gorm.DB) error {
		r := s.repo.At(tx)
		p, err := r.Product(product, true)
		if err != nil {
			return err
		}
		if p == nil {
			return ErrWealthCode
		}
		if date.Before(wealthDay(p.InceptionDate)) {
			return ErrWealthInvalid
		}
		has, err := r.HasProduct(actor.ID, product)
		if err != nil {
			return err
		}
		if !has {
			return ErrWealthUnregistered
		}
		prior, err := r.Mutation(actor.ID, in.RequestKey)
		if err != nil {
			return err
		}
		if prior != nil {
			if prior.PayloadHash != hash || prior.Action != "manual_nav" {
				return ErrWealthConflict
			}
			return json.Unmarshal([]byte(prior.ResultJSON), &result)
		}
		automatic, err := r.AutomaticAt(product, date)
		if err != nil {
			return err
		}
		if automatic != nil {
			return ErrWealthAutomatic
		}
		old, err := r.ManualAt(actor.ID, product, date)
		if err != nil {
			return err
		}
		if old == nil && (in.ExpectedRevision != 0 || in.Withdraw) {
			return ErrWealthConflict
		}
		if old != nil && old.Revision != in.ExpectedRevision {
			return ErrWealthConflict
		}
		before := "null"
		revision := int64(1)
		id := int64(0)
		if old != nil {
			b, _ := json.Marshal(old)
			before = string(b)
			revision = old.Revision + 1
			id = old.ID
			if in.Withdraw {
				if old.Withdrawn {
					return ErrWealthConflict
				}
				price = old.UnitNAV
			}
		}
		row := model.WealthManualNAV{ID: id, OwnerID: actor.ID, ProductID: product, NAVDate: date, UnitNAV: price, Revision: revision, SourceNote: in.SourceNote, Withdrawn: in.Withdraw, UpdatedAt: s.Now()}
		if old != nil {
			row.CreatedAt = old.CreatedAt
		}
		if err = tx.Save(&row).Error; err != nil {
			return err
		}
		after, _ := json.Marshal(row)
		action := "create"
		if old != nil {
			action = "amend"
		}
		if in.Withdraw {
			action = "withdraw"
		}
		if err = tx.Create(&model.WealthManualAudit{OwnerID: actor.ID, ProductID: product, ManualNAVID: row.ID, ActorID: actor.ID, Action: action, BeforeJSON: before, AfterJSON: string(after), CreatedAt: s.Now()}).Error; err != nil {
			return err
		}
		if err = r.BumpSeries(actor.ID, product); err != nil {
			return err
		}
		result = WealthManualReceipt{row.ID, product, revision}
		receipt, _ := json.Marshal(result)
		if err = tx.Create(&model.WealthMutation{OwnerID: actor.ID, RequestKey: in.RequestKey, PayloadHash: hash, Action: "manual_nav", ResultJSON: string(receipt), CreatedAt: s.Now()}).Error; err != nil {
			return err
		}
		wrote = true
		return nil
	})
	if err == nil && wrote {
		s.changed(actor.ID)
		s.rebuildProduct(actor.ID, product)
	}
	return result, err
}
