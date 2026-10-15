package repository

import (
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"jizhang/internal/model"
)

// CatalogOrder scopes metadata writes to a code-selected personal directory.
// Neither the table nor the owner can be supplied by an HTTP client.
type CatalogOrder struct {
	db     *gorm.DB
	table  string
	owner  int64
	kind   model.CategoryKind
	parent *int64
}

type CatalogOrderRow struct {
	ID       int64
	OwnerID  *int64
	Sort     int
	Kind     model.CategoryKind
	ParentID *int64
}

func CategoryOrder(db *gorm.DB, owner int64, kind model.CategoryKind, parent *int64) *CatalogOrder {
	return &CatalogOrder{db: db, table: "categories", owner: owner, kind: kind, parent: parent}
}
func AccountOrder(db *gorm.DB, owner int64) *CatalogOrder {
	return &CatalogOrder{db: db, table: "accounts", owner: owner}
}
func PlatformOrder(db *gorm.DB, owner int64) *CatalogOrder {
	return &CatalogOrder{db: db, table: "voucher_platforms", owner: owner}
}
func (s *CatalogOrder) query() *gorm.DB {
	q := s.db.Table(s.table).Where("owner_id = ?", s.owner)
	if s.kind != "" {
		q = q.Where("kind = ?", s.kind)
		if s.parent == nil {
			q = q.Where("parent_id IS NULL")
		} else {
			q = q.Where("parent_id = ?", *s.parent)
		}
	}
	return q
}
func (s *CatalogOrder) Requested(ids []int64) ([]CatalogOrderRow, error) {
	rows := []CatalogOrderRow{}
	if len(ids) == 0 {
		return rows, nil
	}
	columns := "id, owner_id, sort"
	if s.kind != "" {
		columns += ", kind, parent_id"
	}
	err := s.db.Table(s.table).Select(columns).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", ids).Order("id").Find(&rows).Error
	return rows, err
}
func (s *CatalogOrder) IDs() ([]int64, error) {
	ids := []int64{}
	err := s.query().Clauses(clause.Locking{Strength: "UPDATE"}).Order("sort, id").Pluck("id", &ids).Error
	return ids, err
}
func (s *CatalogOrder) NextPosition() (int, error) {
	var next int
	err := s.query().Select("COALESCE(MAX(sort), -1) + 1").Scan(&next).Error
	return next, err
}
func (s *CatalogOrder) Save(ids []int64) error {
	for position, id := range ids {
		if err := s.query().Where("id = ?", id).Update("sort", position).Error; err != nil {
			return err
		}
	}
	return nil
}
func (s *CatalogOrder) Matches(row CatalogOrderRow) bool {
	if s.kind == "" {
		return true
	}
	if row.Kind != s.kind {
		return false
	}
	if s.parent == nil {
		return row.ParentID == nil
	}
	return row.ParentID != nil && *row.ParentID == *s.parent
}

func (s *CatalogOrder) RequestScope() string {
	if s.kind == "" {
		return s.table
	}
	parent := int64(0)
	if s.parent != nil {
		parent = *s.parent
	}
	return fmt.Sprintf("categories:%s:%d", s.kind, parent)
}
func (s *CatalogOrder) User() (model.User, error) {
	var user model.User
	err := s.db.First(&user, s.owner).Error
	return user, err
}
func (s *CatalogOrder) Request(key string) (*model.CatalogOrderRequest, error) {
	var request model.CatalogOrderRequest
	err := s.db.Where("owner_id = ? AND scope = ? AND request_key = ?", s.owner, s.RequestScope(), key).First(&request).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &request, err
}
func (s *CatalogOrder) Remember(key, digest string) error {
	return s.db.Create(&model.CatalogOrderRequest{OwnerID: s.owner, Scope: s.RequestScope(), RequestKey: key, Digest: digest}).Error
}
