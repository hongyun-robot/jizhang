package handler

import (
	"errors"

	"github.com/gin-gonic/gin"

	"jizhang/internal/middleware"
	"jizhang/internal/model"
	"jizhang/internal/response"
	"jizhang/internal/service"
)

type categoryHandler struct {
	svc *service.CategoryService
}

// NewCategory 分类处理器。
func NewCategory(svc *service.CategoryService) *categoryHandler {
	return &categoryHandler{svc: svc}
}

func mapCategoryErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrTxnForbidden):
		response.Forbidden(c, "只能管理自己的分类")
	case errors.Is(err, service.ErrCategoryNotFound):
		response.NotFound(c, err.Error())
	case errors.Is(err, service.ErrCategoryHasChildren),
		errors.Is(err,service.ErrCategoryHasReferences),
		errors.Is(err, service.ErrCategoryHasTransactions):
		response.Conflict(c, err.Error())
	case errors.Is(err, service.ErrInvalidCategory),
		errors.Is(err, service.ErrCategoryParentInvalid):
		response.Invalid(c, err.Error())
	default:
		response.Internal(c, "操作失败")
	}
}

// Tree GET /api/v1/categories（全部角色可读，两级树）
func (h *categoryHandler) Tree(c *gin.Context) {
	tree, err := h.svc.TreeFor(middleware.CurrentUser(c))
	if err != nil {
		response.Internal(c, "查询失败")
		return
	}
	response.OK(c, tree)
}

type categoryReq struct {
	Kind     model.CategoryKind `json:"kind" binding:"required"`
	Name     string             `json:"name" binding:"required,max=64"`
	ParentID *int64             `json:"parent_id"`
	Sort     *int               `json:"sort"`
}

// Create POST /api/v1/categories（管理员）
func (h *categoryHandler) Create(c *gin.Context) {
	var req categoryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Invalid(c, "类型与名称必填")
		return
	}
	in := service.CreateCategoryInput{
		Kind:     req.Kind,
		Name:     req.Name,
		ParentID: req.ParentID,
	}
	if req.Sort != nil {
		in.Sort = *req.Sort
	}
	cat, err := h.svc.CreateFor(middleware.CurrentUser(c), in)
	if err != nil {
		mapCategoryErr(c, err)
		return
	}
	response.OK(c, cat)
}

type categoryUpdateReq struct {
	Name string `json:"name" binding:"required,max=64"`
	Sort *int   `json:"sort"`
}

// Update PUT /api/v1/categories/:id（管理员：重命名/排序）
func (h *categoryHandler) Update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req categoryUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Invalid(c, "名称必填")
		return
	}
	sort := 0
	if req.Sort != nil {
		sort = *req.Sort
	}
	cat, err := h.svc.UpdateFor(middleware.CurrentUser(c), id, req.Name, sort)
	if err != nil {
		mapCategoryErr(c, err)
		return
	}
	response.OK(c, cat)
}

// Delete DELETE /api/v1/categories/:id（管理员，有子分类或被流水引用则 409）
func (h *categoryHandler) Delete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteFor(middleware.CurrentUser(c), id); err != nil {
		mapCategoryErr(c, err)
		return
	}
	response.OK(c, nil)
}
