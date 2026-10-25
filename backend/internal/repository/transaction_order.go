package repository

import (
	"gorm.io/gorm"
	"jizhang/internal/model"
)

type TransactionOrder struct {
	db    *gorm.DB
	owner int64
}

func NewTransactionOrder(db *gorm.DB, owner int64) *TransactionOrder {
	return &TransactionOrder{db: db, owner: owner}
}

func (r *TransactionOrder) Requested(ids []int64) ([]int64, error) {
	var found []int64
	err := r.db.Model(&model.Transaction{}).Where("id IN ?", ids).Pluck("id", &found).Error
	return found, err
}

func (r *TransactionOrder) IDs(f TxnFilter) ([]int64, error) {
	f.PersonalOwnerID = r.owner
	f.CustomOrder = true
	var ids []int64
	q := transactionOrderQuery(applyFilter(r.db.Model(&model.Transaction{}), f), f)
	err := q.Select("transactions.id").Pluck("transactions.id", &ids).Error
	return ids, err
}

func (r *TransactionOrder) Save(ids []int64) error {
	if err := r.db.Where("owner_id = ?", r.owner).Delete(&model.TransactionDisplayOrder{}).Error; err != nil {
		return err
	}
	rows := make([]model.TransactionDisplayOrder, len(ids))
	for i, id := range ids {
		rows[i] = model.TransactionDisplayOrder{OwnerID: r.owner, TransactionID: id, Position: int64(i)}
	}
	return r.db.CreateInBatches(rows, 500).Error
}

func (r *TransactionOrder) Request(key string) (*model.CatalogOrderRequest, error) {
	return (&CatalogOrder{db: r.db, owner: r.owner, table: "transactions"}).Request(key)
}

func (r *TransactionOrder) Remember(key, digest string) error {
	return (&CatalogOrder{db: r.db, owner: r.owner, table: "transactions"}).Remember(key, digest)
}
