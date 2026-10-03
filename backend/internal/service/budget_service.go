package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"jizhang/internal/model"
	"jizhang/internal/repository"
)

// 预算业务错误（specs/budgets 任务 8.1）。
var (
	ErrBudgetInvalidPeriod   = errors.New("账期格式无效")
	ErrBudgetCategoryInvalid = errors.New("分类预算须为存在的支出一级分类")
	ErrBudgetAmountInvalid   = errors.New("预算金额须为 0-9999999999.99 的数字（0 表示清除）")
)

// BudgetDeps 预算服务依赖。
type BudgetDeps struct {
	Budgets *repository.BudgetRepo
	Stats   *repository.StatsRepo
	Cats    *repository.CategoryRepo
	Notifs  *repository.NotificationRepo
	Periods *PeriodService
}

// BudgetService 预算配置、消耗进度与超支提醒（specs/budgets）。
type BudgetService struct {
	deps BudgetDeps
}

func NewBudgetService(d BudgetDeps) *BudgetService { return &BudgetService{deps: d} }

// BudgetProgress 预算进度：进度条数据源，超支标红 + 超出金额。
type BudgetProgress struct {
	Budget     *string `json:"budget"`      // nil = 未设置预算
	Consumed   string  `json:"consumed"`    // 消耗合计（仅支出）
	Percent    float64 `json:"percent"`     // 0-100，超支时封顶（超支由 over/over_amount 表达）
	Over       bool    `json:"over"`        // 消耗 > 预算
	OverAmount string  `json:"over_amount"` // 超出金额，未超支为 0.00
}

// BudgetCategoryRow 一级分类预算行（含未设置的，配置页直接回填）。
type BudgetCategoryRow struct {
	CategoryID   int64  `json:"category_id"`
	CategoryName string `json:"category_name"`
	BudgetProgress
}

// BudgetView GET /budgets 响应：配置与进度一体。
type BudgetView struct {
	PeriodKey  string              `json:"period_key"`
	Total      BudgetProgress      `json:"total"`
	Categories []BudgetCategoryRow `json:"categories"` // 全部一级支出分类
}

// calcProgress 计算预算进度（纯函数，单测覆盖）。
// 超支判定 consumed > budget；percent 供进度条使用，封顶 100。
func calcProgress(budget *decimal.Decimal, consumed decimal.Decimal) BudgetProgress {
	p := BudgetProgress{
		Consumed:   consumed.StringFixed(2),
		OverAmount: "0.00",
	}
	if budget == nil {
		return p
	}
	b := *budget
	s := b.StringFixed(2)
	p.Budget = &s
	if consumed.GreaterThan(b) {
		p.Over = true
		p.OverAmount = consumed.Sub(b).StringFixed(2)
	}
	switch {
	case b.IsPositive():
		pct := consumed.Div(b).Mul(decimal.NewFromInt(100)).Round(2)
		if pct.GreaterThan(decimal.NewFromInt(100)) {
			pct = decimal.NewFromInt(100)
		}
		if pct.IsNegative() {
			pct = decimal.Zero
		}
		p.Percent, _ = pct.Float64()
	case consumed.IsPositive(): // 预算 0 且有消耗
		p.Percent = 100
	}
	return p
}

// CurrentKey 当前账期键（GET /budgets 未带 period_key 时的默认值）。
func (s *BudgetService) CurrentKey() string {
	return s.deps.Periods.CurrentKey(time.Now())
}

// Get 某账期预算视图（全部角色可读）：
// 总预算进度 + 每个一级支出分类一行（配置回填与进度展示同一个数据源）。
func (s *BudgetService) Get(periodKey string) (*BudgetView, error) {
	if _, _, ok := s.deps.Periods.Range(periodKey); !ok {
		return nil, ErrBudgetInvalidPeriod
	}
	bs, err := s.deps.Budgets.List(periodKey)
	if err != nil {
		return nil, err
	}
	sum, err := s.deps.Stats.SumByPeriod(periodKey)
	if err != nil {
		return nil, err
	}
	byRoot, err := s.deps.Budgets.ConsumptionByRoot(periodKey)
	if err != nil {
		return nil, err
	}
	cats, err := s.deps.Cats.List()
	if err != nil {
		return nil, err
	}

	var totalBudget *decimal.Decimal
	budgetsByRoot := make(map[int64]decimal.Decimal, len(bs))
	for _, b := range bs {
		if b.CategoryID == nil {
			v := b.Amount
			totalBudget = &v
		} else {
			budgetsByRoot[*b.CategoryID] = b.Amount
		}
	}

	view := &BudgetView{
		PeriodKey:  periodKey,
		Total:      calcProgress(totalBudget, sum.Expense),
		Categories: make([]BudgetCategoryRow, 0, 8),
	}
	for _, c := range cats {
		if c.Kind != model.KindExpense || c.ParentID != nil {
			continue // 只列一级支出分类
		}
		var budget *decimal.Decimal
		if v, ok := budgetsByRoot[c.ID]; ok {
			budget = &v
		}
		row := BudgetCategoryRow{CategoryID: c.ID, CategoryName: c.Name}
		row.BudgetProgress = calcProgress(budget, byRoot[c.ID])
		view.Categories = append(view.Categories, row)
	}
	return view, nil
}

// BudgetSetItem 分类预算项（amount 为 nil/空/0 表示清除该分类预算）。
type BudgetSetItem struct {
	CategoryID int64   `json:"category_id"`
	Amount     *string `json:"amount"`
}

// BudgetSetInput 整单提交的预算配置。
type BudgetSetInput struct {
	Total *string         `json:"total"`
	Items []BudgetSetItem `json:"items"`
}

// parseAmount 解析预算金额：返回 (值, 是否保留, 错误)。0 与空串/nil 视为清除。
func parseAmount(s *string) (decimal.Decimal, bool, error) {
	if s == nil || *s == "" {
		return decimal.Zero, false, nil
	}
	v, err := decimal.NewFromString(*s)
	if err != nil || v.IsNegative() || v.GreaterThan(maxAmount) || !v.Round(2).Equal(v) {
		return decimal.Zero, false, ErrBudgetAmountInvalid
	}
	if v.IsZero() {
		return decimal.Zero, false, nil
	}
	return v, true, nil
}

// Set 保存预算配置（仅管理员，路由门禁）：整期重建，随后立即评估超支——
// 把预算改到低于既有消耗时当场提醒（spec「首次超支」不限于记账触发）。
func (s *BudgetService) Set(periodKey string, in BudgetSetInput) error {
	if _, _, ok := s.deps.Periods.Range(periodKey); !ok {
		return ErrBudgetInvalidPeriod
	}
	rows := make([]model.Budget, 0, len(in.Items)+1)
	if v, keep, err := parseAmount(in.Total); err != nil {
		return err
	} else if keep {
		rows = append(rows, model.Budget{PeriodKey: periodKey, Amount: v})
	}
	seen := make(map[int64]bool, len(in.Items))
	for _, it := range in.Items {
		if seen[it.CategoryID] {
			continue // 同分类重复提交取首项
		}
		seen[it.CategoryID] = true
		c, err := s.deps.Cats.GetByID(it.CategoryID)
		if err != nil {
			return err
		}
		if c == nil || c.Kind != model.KindExpense || c.ParentID != nil {
			return ErrBudgetCategoryInvalid
		}
		v, keep, err := parseAmount(it.Amount)
		if err != nil {
			return err
		}
		if !keep {
			continue // 0 / 空 = 清除该分类预算
		}
		cid := it.CategoryID
		rows = append(rows, model.Budget{PeriodKey: periodKey, CategoryID: &cid, Amount: v})
	}
	if err := s.deps.Budgets.Replace(periodKey, rows); err != nil {
		return err
	}
	return s.Evaluate(periodKey)
}

// BudgetNotifyPayload 超支提醒内容（spec：含分类名与超出金额）。
type BudgetNotifyPayload struct {
	DedupKey     string `json:"dedup_key"`
	PeriodKey    string `json:"period_key"`
	Scope        string `json:"scope"` // total | category
	CategoryID   int64  `json:"category_id"`
	CategoryName string `json:"category_name"` // 总预算为「总预算」
	Budget       string `json:"budget"`
	Consumed     string `json:"consumed"`
	OverAmount   string `json:"over_amount"`
}

// Evaluate 评估某账期预算，对「首次超支且本账期内未曾提醒过」的预算生成一条站内提醒。
// 同账期同预算用 payload.dedup_key 去重（任务 8.2）。
func (s *BudgetService) Evaluate(periodKey string) error {
	bs, err := s.deps.Budgets.List(periodKey)
	if err != nil {
		return err
	}
	if len(bs) == 0 {
		return nil
	}
	sum, err := s.deps.Stats.SumByPeriod(periodKey)
	if err != nil {
		return err
	}
	byRoot, err := s.deps.Budgets.ConsumptionByRoot(periodKey)
	if err != nil {
		return err
	}
	notifs, err := s.deps.Notifs.List(model.NotifBudgetOverdue)
	if err != nil {
		return err
	}
	existing := make(map[string]bool, len(notifs))
	for _, n := range notifs {
		var p BudgetNotifyPayload
		if json.Unmarshal([]byte(n.Payload), &p) == nil && p.DedupKey != "" {
			existing[p.DedupKey] = true
		}
	}

	for _, b := range bs {
		var (
			consumed decimal.Decimal
			dedupKey string
			name     = "总预算"
			scope    = "total"
			catID    int64
		)
		if b.CategoryID == nil {
			consumed = sum.Expense
			dedupKey = fmt.Sprintf("budget_overdue:%s:total", periodKey)
		} else {
			catID = *b.CategoryID
			consumed = byRoot[catID]
			dedupKey = fmt.Sprintf("budget_overdue:%s:cat:%d", periodKey, catID)
			scope = "category"
			c, err := s.deps.Cats.GetByID(catID)
			if err != nil {
				return err
			}
			if c != nil {
				name = c.Name
			}
		}
		if !consumed.GreaterThan(b.Amount) || existing[dedupKey] {
			continue
		}
		payload, err := json.Marshal(BudgetNotifyPayload{
			DedupKey:     dedupKey,
			PeriodKey:    periodKey,
			Scope:        scope,
			CategoryID:   catID,
			CategoryName: name,
			Budget:       b.Amount.StringFixed(2),
			Consumed:     consumed.StringFixed(2),
			OverAmount:   consumed.Sub(b.Amount).StringFixed(2),
		})
		if err != nil {
			return err
		}
		if err := s.deps.Notifs.Create(&model.Notification{
			Kind:    model.NotifBudgetOverdue,
			Payload: string(payload),
		}); err != nil {
			return err
		}
		existing[dedupKey] = true
	}
	return nil
}
