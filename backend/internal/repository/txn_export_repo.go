package repository

import (
	"context"
	"database/sql"

	"gorm.io/gorm"
	"jizhang/internal/model"
)

// TxnExportSnapshot contains only persisted facts read in one database view.
type TxnExportSnapshot struct {
	Rows       []model.Transaction
	Accounts   []model.Account
	Categories []model.Category
	Related    map[int64]bool
	References map[string]map[int64]TxnExportReference
}

type TxnExportReference struct {
	ID            int64
	AccountID     int64
	CashAccountID *int64
	TxnID         *int64
	VoucherID     int64
	Code          string
}

type TxnExportRepo struct{ db *gorm.DB }

func NewTxnExportRepo(db *gorm.DB) *TxnExportRepo { return &TxnExportRepo{db: db} }

// ReadXLSX stops before metadata/association reads when the extra row proves
// overflow. It never changes the unbounded CSV query contract.
func (r *TxnExportRepo) ReadXLSX(ctx context.Context, f TxnFilter, limit int) (*TxnExportSnapshot, error) {
	out := &TxnExportSnapshot{Related: map[int64]bool{}, References: map[string]map[int64]TxnExportReference{}}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := applyFilter(tx.Model(&model.Transaction{}), f)
		if err := transactionOrderQuery(q, f).Limit(limit + 1).Find(&out.Rows).Error; err != nil {
			return err
		}
		if len(out.Rows) > limit {
			return nil
		}
		if err := tx.Where("owner_id = ?", f.PersonalOwnerID).Find(&out.Accounts).Error; err != nil {
			return err
		}
		if err := tx.Where("owner_id = ?", f.PersonalOwnerID).Find(&out.Categories).Error; err != nil {
			return err
		}
		ids := map[string][]int64{}
		for _, t := range out.Rows {
			for kind, id := range map[string]*int64{"related": t.RelatedTxnID, "voucher": t.VoucherEventID, "fund": t.FundEventID, "gold": t.GoldEventID, "wealth": t.WealthEventID, "yield": t.WealthCashYieldID} {
				if id != nil {
					ids[kind] = append(ids[kind], *id)
				}
			}
		}
		for kind, all := range ids {
			seen := map[int64]bool{}
			unique := make([]int64, 0, len(all))
			for _, id := range all {
				if !seen[id] {
					unique = append(unique, id)
					seen[id] = true
				}
			}
			out.References[kind] = map[int64]TxnExportReference{}
			for start := 0; start < len(unique); start += 500 {
				batch := unique[start:min(start+500, len(unique))]
				if kind == "related" {
					relatedFilter := TxnFilter{PersonalOwnerID: f.PersonalOwnerID, LegacyTransfers: f.LegacyTransfers}
					var rows []model.Transaction
					if err := applyFilter(tx.Model(&model.Transaction{}), relatedFilter).Select("transactions.id").Where("transactions.id IN ?", batch).Find(&rows).Error; err != nil {
						return err
					}
					for _, row := range rows {
						out.Related[row.ID] = true
					}
					continue
				}
				rows, err := readExportReferences(tx, kind, batch, f.PersonalOwnerID)
				if err != nil {
					return err
				}
				for _, row := range rows {
					out.References[kind][row.ID] = row
				}
			}
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return out, err
}

func readExportReferences(tx *gorm.DB, kind string, ids []int64, owner int64) ([]TxnExportReference, error) {
	var q *gorm.DB
	switch kind {
	case "voucher":
		q = tx.Table("voucher_events e").Select("e.id,e.account_id,e.voucher_id").Joins("JOIN vouchers v ON v.id=e.voucher_id").
			Joins("JOIN accounts a ON a.id=e.account_id").Joins("JOIN accounts va ON va.id=v.account_id").
			Where("a.owner_id=? AND va.owner_id=? AND e.voided_at IS NULL AND v.deleted_at IS NULL", owner, owner)
	case "fund":
		q = tx.Table("fund_events e").Select("e.id,e.account_id,e.cash_account_id,i.ths_code AS code").
			Joins("JOIN fund_positions p ON p.id=e.position_id AND p.owner_id=e.owner_id AND p.account_id=e.account_id").
			Joins("JOIN fund_instruments i ON i.id=p.instrument_id").Where("e.owner_id=? AND e.voided_at IS NULL", owner)
	case "gold":
		q = tx.Table("gold_events e").Select("e.id,e.account_id,e.cash_account_id,p.product_key AS code").
			Joins("JOIN gold_positions p ON p.account_id=e.account_id AND p.owner_id=e.owner_id").Where("e.owner_id=? AND e.voided_at IS NULL", owner)
	case "wealth":
		q = tx.Table("wealth_events e").Select("e.id,e.account_id,e.cash_account_id,i.code").
			Joins("JOIN wealth_positions p ON p.id=e.position_id AND p.owner_id=e.owner_id AND p.account_id=e.account_id").
			Joins("JOIN wealth_products i ON i.id=p.product_id").Where("e.owner_id=? AND e.voided_at IS NULL", owner)
	case "yield":
		q = tx.Table("wealth_cash_yields e").Select("e.id,e.account_id,e.txn_id,i.code").
			Joins("JOIN wealth_products i ON i.id=e.product_id").Where("e.owner_id=?", owner)
	}
	var rows []TxnExportReference
	err := q.Where("e.id IN ?", ids).Scan(&rows).Error
	return rows, err
}
