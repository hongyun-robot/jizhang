package model

import "time"

// CategoryKind 分类归属的收支类型（收支各一套树）。
type CategoryKind string

const (
	KindExpense CategoryKind = "expense"
	KindIncome  CategoryKind = "income"
)

// Category 两级分类：parent_id 为空即一级分类。
type Category struct {
	ID        int64        `gorm:"primaryKey" json:"id"`
	ParentID  *int64       `json:"parent_id"`
	Kind      CategoryKind `json:"kind"`
	Name      string       `gorm:"size:64" json:"name"`
	Sort      int          `json:"sort"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

func (Category) TableName() string { return "categories" }

// TreeNode 树形返回结构。
type CategoryTreeNode struct {
	Category
	Children []*CategoryTreeNode `json:"children,omitempty"`
}
