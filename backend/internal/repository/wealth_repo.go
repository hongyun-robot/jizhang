package repository

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"jizhang/internal/model"
	"jizhang/internal/wealthdata"
)

type WealthRepo struct{ db *gorm.DB }

func NewWealthRepo(db *gorm.DB) *WealthRepo      { return &WealthRepo{db: db} }
func (r *WealthRepo) DB() *gorm.DB               { return r.db }
func (r *WealthRepo) At(db *gorm.DB) *WealthRepo { return NewWealthRepo(db) }

func (r *WealthRepo) Catalog() ([]model.WealthProduct, error) {
	rows := []model.WealthProduct{}
	err := r.db.Where("admission_status = ?", "enabled").Order("issuer,code,id").Find(&rows).Error
	return rows, err
}
func (r *WealthRepo) Product(id int64, lock bool) (*model.WealthProduct, error) {
	var row model.WealthProduct
	q := r.db
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := q.First(&row, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}
func (r *WealthRepo) ProductForCode(code string) (*model.WealthProduct, error) {
	identity, ok := wealthdata.Catalog(code)
	if !ok {
		return nil, nil
	}
	var row model.WealthProduct
	err := r.db.Where("issuer = ? AND registration_code = ? AND code = ? AND currency = ? AND source_adapter = ? AND source_code = ? AND valuation_basis = ?", identity.Issuer, identity.RegistrationCode, identity.Code, identity.Currency, identity.Adapter, identity.SourceCode, "cny_wealth_unit_nav").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}
func (r *WealthRepo) Accounts(owner int64) ([]model.Account, error) {
	rows := []model.Account{}
	err := r.db.Where("owner_id = ? AND type = ? AND asset_class = ?", owner, model.AccountAsset, "wealth").Order("sort,id").Find(&rows).Error
	return rows, err
}
func (r *WealthRepo) Position(owner, id int64, lock bool) (*model.WealthPosition, error) {
	var row model.WealthPosition
	q := r.db.Where("owner_id = ?", owner)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := q.First(&row, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}
func (r *WealthRepo) PositionForCode(owner, account, product int64) (*model.WealthPosition, error) {
	var row model.WealthPosition
	err := r.db.Where("owner_id = ? AND account_id = ? AND product_id = ?", owner, account, product).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}
func (r *WealthRepo) Mutation(owner int64, key string) (*model.WealthMutation, error) {
	var row model.WealthMutation
	err := r.db.Where("owner_id = ? AND request_key = ?", owner, key).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}
func (r *WealthRepo) Events(owner, position int64) ([]model.WealthEvent, error) {
	rows := []model.WealthEvent{}
	err := r.db.Where("owner_id = ? AND position_id = ?", owner, position).Order("effective_date,id").Find(&rows).Error
	return rows, err
}
func (r *WealthRepo) Audits(owner, position int64) ([]model.WealthAudit, error) {
	rows := []model.WealthAudit{}
	err := r.db.Where("owner_id = ? AND position_id = ?", owner, position).Order("id DESC").Find(&rows).Error
	return rows, err
}
func (r *WealthRepo) NAVs(product int64) ([]model.WealthNAV, error) {
	rows := []model.WealthNAV{}
	err := r.db.Where("product_id = ?", product).Order("nav_date").Find(&rows).Error
	return rows, err
}
func (r *WealthRepo) ManualNAVs(owner, product int64) ([]model.WealthManualNAV, error) {
	rows := []model.WealthManualNAV{}
	err := r.db.Where("owner_id = ? AND product_id = ?", owner, product).Order("nav_date").Find(&rows).Error
	return rows, err
}
func (r *WealthRepo) Valuations(owner, position int64, start, end time.Time) ([]model.WealthValuation, error) {
	rows := []model.WealthValuation{}
	q := r.db.Table("wealth_valuations v").Select("v.*").Joins("JOIN wealth_positions p ON p.id=v.position_id").Where("p.owner_id = ? AND p.id = ?", owner, position)
	if !start.IsZero() {
		q = q.Where("v.nav_date >= ?", start.Format("2006-01-02"))
	}
	if !end.IsZero() {
		q = q.Where("v.nav_date < ?", end.AddDate(0, 0, 1).Format("2006-01-02"))
	}
	err := q.Order("v.nav_date DESC").Limit(5000).Scan(&rows).Error
	return rows, err
}

type WealthData struct {
	Positions  []model.WealthPosition
	Products   []model.WealthProduct
	Events     []model.WealthEvent
	NAVs       []model.WealthNAV
	ManualNAVs []model.WealthManualNAV
	Series     []model.WealthSeries
}

// Account IDs must already be scoped; all personal joins also constrain the stored
// owner. A public product match can never broaden personal overlay visibility.
func (r *WealthRepo) LoadAccounts(ids []int64) (WealthData, error) {
	data := WealthData{Positions: []model.WealthPosition{}, Products: []model.WealthProduct{}, Events: []model.WealthEvent{}, NAVs: []model.WealthNAV{}, ManualNAVs: []model.WealthManualNAV{}, Series: []model.WealthSeries{}}
	if len(ids) == 0 {
		return data, nil
	}
	err := r.db.Table("wealth_positions p").Select("p.*").Joins("JOIN accounts a ON a.id=p.account_id AND a.owner_id=p.owner_id").Where("a.id IN ? AND a.type = ? AND a.asset_class = ?", ids, model.AccountAsset, "wealth").Order("p.id").Scan(&data.Positions).Error
	if err != nil || len(data.Positions) == 0 {
		return data, err
	}
	positions, products := []int64{}, []int64{}
	for _, p := range data.Positions {
		positions = append(positions, p.ID)
		products = append(products, p.ProductID)
	}
	if err = r.db.Where("id IN ?", products).Find(&data.Products).Error; err != nil {
		return data, err
	}
	if err = r.db.Table("wealth_events e").Select("e.*").Joins("JOIN wealth_positions p ON p.id=e.position_id AND p.owner_id=e.owner_id AND p.account_id=e.account_id").Where("p.id IN ?", positions).Order("e.effective_date,e.id").Scan(&data.Events).Error; err != nil {
		return data, err
	}
	if err = r.db.Where("product_id IN ?", products).Order("nav_date").Find(&data.NAVs).Error; err != nil {
		return data, err
	}
	personal := r.db.Table("wealth_positions p").Select("1").Where("p.id IN ? AND p.owner_id=m.owner_id AND p.product_id=m.product_id", positions)
	if err = r.db.Table("wealth_manual_navs m").Select("m.*").Where("EXISTS (?)", personal).Order("m.nav_date").Scan(&data.ManualNAVs).Error; err != nil {
		return data, err
	}
	err = r.db.Table("wealth_series m").Select("m.*").Where("EXISTS (?)", personal).Scan(&data.Series).Error
	return data, err
}

// Product is locked before positions throughout quote/overlay/rebuild writes.
// Financial LedgerWrite is the outer lock whenever money is affected.
func (r *WealthRepo) BumpSeries(owner, product int64) error {
	row := model.WealthSeries{OwnerID: owner, ProductID: product, Version: 1}
	if err := r.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "owner_id"}, {Name: "product_id"}}, DoUpdates: clause.Assignments(map[string]any{"version": gorm.Expr("version + 1")})}).Create(&row).Error; err != nil {
		return err
	}
	return r.db.Model(&model.WealthPosition{}).Where("owner_id = ? AND product_id = ?", owner, product).Update("rebuild_pending", true).Error
}

type WealthCashLink struct {
	EventID    int64
	PositionID int64
	Code       string
}

func (r *WealthRepo) CashLinks(ids []int64) ([]WealthCashLink, error) {
	rows := []WealthCashLink{}
	if len(ids) == 0 {
		return rows, nil
	}
	err := r.db.Table("wealth_events e").Select("e.id AS event_id,e.position_id,i.code").Joins("JOIN wealth_positions p ON p.id=e.position_id AND p.owner_id=e.owner_id AND p.account_id=e.account_id").Joins("JOIN wealth_products i ON i.id=p.product_id").Where("e.id IN ?", ids).Scan(&rows).Error
	return rows, err
}
