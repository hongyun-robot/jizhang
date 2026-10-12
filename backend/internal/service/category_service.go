package service

import (
	"errors"
	"sort"

	"jizhang/internal/model"
	"jizhang/internal/repository"
)

// 分类业务错误。
var (
	ErrCategoryNotFound        = errors.New("分类不存在")
	ErrCategoryHasChildren     = errors.New("该分类还有子分类，不能删除")
	ErrCategoryHasTransactions = errors.New("该分类仍被流水引用，不能删除")
	ErrCategoryHasReferences   = errors.New("该分类仍被业务配置引用，不能删除")
	ErrInvalidCategory         = errors.New("分类参数无效")
	ErrCategoryParentInvalid   = errors.New("父分类无效（只能挂在一级分类下）")
)

// CategoryService 分类管理（两级，收支各一套）。
type CategoryService struct {
	repo *repository.CategoryRepo
	// OnWrite 写成功后的钩子（用于统计缓存主动失效）。
	OnWrite func()
}

func (s *CategoryService) fireWrite() {
	if s.OnWrite != nil {
		s.OnWrite()
	}
}

func NewCategoryService(repo *repository.CategoryRepo) *CategoryService {
	return &CategoryService{repo: repo}
}

// Tree 返回按 kind 分组的两级分类树（一级 → 子分类）。
// 返回顺序固定：支出树、收入树。
func (s *CategoryService) Tree() ([]*model.CategoryTreeNode, error) {
	list, err := s.repo.List()
	if err != nil {
		return nil, err
	}
	order := []model.CategoryKind{model.KindExpense, model.KindIncome}
	roots := map[model.CategoryKind]*model.CategoryTreeNode{}
	for _, k := range order {
		roots[k] = &model.CategoryTreeNode{}
	}

	// 第一遍：一级分类
	for i := range list {
		c := list[i]
		if c.ParentID == nil {
			roots[c.Kind].Children = append(roots[c.Kind].Children, &model.CategoryTreeNode{Category: c})
		}
	}
	// 第二遍：子分类挂到对应一级下
	for i := range list {
		c := list[i]
		if c.ParentID == nil {
			continue
		}
		for _, root := range roots[c.Kind].Children {
			if root.ID == *c.ParentID {
				root.Children = append(root.Children, &model.CategoryTreeNode{Category: c})
				break
			}
		}
	}

	out := make([]*model.CategoryTreeNode, 0, 2)
	for _, k := range order {
		rootList := roots[k].Children
		sort.SliceStable(rootList, func(i, j int) bool {
			if rootList[i].Sort != rootList[j].Sort {
				return rootList[i].Sort < rootList[j].Sort
			}
			return rootList[i].ID < rootList[j].ID
		})
		for _, r := range rootList {
			sort.SliceStable(r.Children, func(i, j int) bool {
				if r.Children[i].Sort != r.Children[j].Sort {
					return r.Children[i].Sort < r.Children[j].Sort
				}
				return r.Children[i].ID < r.Children[j].ID
			})
		}
		out = append(out, roots[k])
	}
	return out, nil
}

// CreateInput 建分类入参：ParentID 为空 = 一级。
type CreateCategoryInput struct {
	OwnerID  int64
	Kind     model.CategoryKind
	Name     string
	ParentID *int64
	Sort     int
}

// Create 新建分类（最多两级：只能挂在一级分类下）。
func (s *CategoryService) Create(in CreateCategoryInput) (*model.Category, error) {
	if in.Kind != model.KindExpense && in.Kind != model.KindIncome {
		return nil, ErrInvalidCategory
	}
	if in.Name == "" {
		return nil, ErrInvalidCategory
	}
	if in.ParentID != nil {
		if *in.ParentID <= 0 {
			return nil, ErrCategoryParentInvalid
		}
		parent, err := s.repo.GetByID(*in.ParentID)
		if err != nil {
			return nil, err
		}
		if parent == nil || parent.ParentID != nil || parent.Kind != in.Kind {
			return nil, ErrCategoryParentInvalid
		}
	}
	c := &model.Category{
		ParentID: in.ParentID,
		Kind:     in.Kind,
		Name:     in.Name,
		Sort:     in.Sort,
	}
	if in.OwnerID > 0 {
		c.OwnerID = &in.OwnerID
	}
	if err := s.repo.Create(c); err != nil {
		return nil, err
	}
	s.fireWrite()
	return c, nil
}

// Update 重命名/排序。
func (s *CategoryService) Update(id int64, name string, sort int) (*model.Category, error) {
	c, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, ErrCategoryNotFound
	}
	if name != "" {
		c.Name = name
	}
	c.Sort = sort
	if err := s.repo.Update(c); err != nil {
		return nil, err
	}
	s.fireWrite()
	return c, nil
}

// Delete 删除分类：有子分类或被流水引用时拒绝。
func (s *CategoryService) Delete(id int64) error {
	c, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	if c == nil {
		return ErrCategoryNotFound
	}
	nChildren, err := s.repo.CountChildren(id)
	if err != nil {
		return err
	}
	if nChildren > 0 {
		return ErrCategoryHasChildren
	}
	nTxn, err := s.repo.CountTransactions(id)
	if err != nil {
		return err
	}
	if nTxn > 0 {
		return ErrCategoryHasTransactions
	}
	if err := s.repo.Delete(id); err != nil {
		return err
	}
	s.fireWrite()
	return nil
}
