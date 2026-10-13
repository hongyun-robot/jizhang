package service

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"jizhang/internal/liability"
	"jizhang/internal/logger"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

// 周期记账模板业务错误（specs/recurring-entries）。
var (
	ErrRuleNotFound        = errors.New("周期模板不存在")
	ErrRuleForbidden       = errors.New("无权管理周期模板")
	ErrRuleFreqInvalid     = errors.New("频率仅支持 daily/weekly/monthly/yearly")
	ErrRuleAmountInvalid   = errors.New("金额必须大于 0 且不超过 9999999999.99")
	ErrRuleDateInvalid     = errors.New("首次生成日期无效")
	ErrRuleAccountInvalid  = errors.New("账户无效或已停用")
	ErrRuleCategoryInvalid = errors.New("分类无效：须存在且为可直接记账的末级分类")
	ErrRuleStatusInvalid   = errors.New("状态仅支持 enabled/paused")
	ErrRuleCatchUpTooLong  = errors.New("单次补跑超过 1000 期，请调整首次生成日期")
)

const dayFmt = "2006-01-02"

// 单轮单模板补跑上限：防呆（如把首次生成日期设到二十年前），超出报错留待人工调整。
const maxCatchUpPerRun = 1000

// RuleStore 模板仓储依赖。
type RuleStore interface {
	List() ([]model.RecurringRule, error)
	ListByCreator(creatorID int64) ([]model.RecurringRule, error)
	ListDueBefore(beforeExclusive string) ([]model.RecurringRule, error)
	GetByID(id int64) (*model.RecurringRule, error)
	Create(m *model.RecurringRule) error
	Update(m *model.RecurringRule) error
	Advance(id int64, nextRun time.Time) error
	Delete(id int64) error
}

// RuleDateStore 生成幂等检查（transactions 表唯一约束 (rule_id, entry_date) 的查询面）。
type RuleDateStore interface {
	ExistsByRuleDate(ruleID int64, date time.Time) (bool, error)
}

// RuleDeps 周期记账服务的外部依赖。
type RuleDeps struct {
	Rules    RuleStore
	Dates    RuleDateStore
	Txns     *TransactionService
	Accounts *repository.AccountRepo
	Cats     *repository.CategoryRepo
	// Notifs 可选：懒补齐生成后落一条「已补记」提醒（specs/notifications，任务 10.1）。
	Notifs *repository.NotificationRepo
	// Users 列表附创建人姓名（specs/recurring-entries「按创建者归属隔离」）。
	Users *repository.UserRepo
}

// RecurringService 周期记账：模板 CRUD + 到期生成（任务 9.1–9.3）。
type RecurringService struct {
	deps RuleDeps
	// mu 串行化 EnsureDue（cron 每 5 分钟与前端懒补齐并发时的进程内闸门；
	// 数据库唯一约束 (rule_id, entry_date) 是多实例下的最终兜底）。
	mu sync.Mutex
}

func NewRecurringService(deps RuleDeps) *RecurringService {
	return &RecurringService{deps: deps}
}

// RuleInput 模板创建/修改入参。
type RuleInput struct {
	Amount     decimal.Decimal
	Freq       model.RuleFreq
	AccountID  int64
	CategoryID int64
	Note       string
	StartDate  time.Time
}

// validate 校验模板入参；分类须为末级（生成流水可直接挂账），收支类型不限。
func (s *RecurringService) validate(in *RuleInput) error {
	if in.Amount.IsNegative() || in.Amount.IsZero() ||
		in.Amount.GreaterThan(maxAmount) || !in.Amount.Round(2).Equal(in.Amount) {
		return ErrRuleAmountInvalid
	}
	switch in.Freq {
	case model.FreqDaily, model.FreqWeekly, model.FreqMonthly, model.FreqYearly:
	default:
		return ErrRuleFreqInvalid
	}
	if in.StartDate.IsZero() || in.StartDate.Year() < 2000 || in.StartDate.Year() > 2100 {
		return ErrRuleDateInvalid
	}
	acc, err := s.deps.Accounts.GetByID(in.AccountID)
	if err != nil {
		return err
	}
	if acc == nil || acc.Status != model.StatusActive {
		return ErrRuleAccountInvalid
	}
	if acc.IsFund() {
		return ErrRuleAccountInvalid
	}
	cat, err := s.deps.Cats.GetByID(in.CategoryID)
	if err != nil {
		return err
	}
	if cat == nil {
		return ErrRuleCategoryInvalid
	}
	n, err := s.deps.Cats.CountChildren(cat.ID)
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrRuleCategoryInvalid
	}
	return nil
}

// RuleView 模板 + 创建人姓名（GET /recurring 列表项，specs/recurring-entries）。
type RuleView struct {
	model.RecurringRule
	CreatedByName string `json:"created_by_name"`
}

// List 模板列表（specs/recurring-entries「模板按创建者归属隔离」）：
// 管理员见全部，其余角色仅见自己创建的（只读成员天然为空）；
// 附创建人姓名供管理员区分谁建的（一次取全表用户名建映射，避免 N+1）。
func (s *RecurringService) List(actor *model.User) ([]RuleView, error) {
	if !CanView(actor) {
		return nil, ErrRuleForbidden
	}
	var (
		rules []model.RecurringRule
		err   error
	)
	if CanManageSettings(actor) {
		rules, err = s.deps.Rules.List()
	} else {
		rules, err = s.deps.Rules.ListByCreator(actor.ID)
	}
	if err != nil {
		return nil, err
	}
	views := make([]RuleView, 0, len(rules))
	if len(rules) == 0 {
		return views, nil
	}
	names := map[int64]string{}
	if s.deps.Users != nil {
		users, uErr := s.deps.Users.List()
		if uErr != nil {
			return nil, uErr
		}
		for _, u := range users {
			names[u.ID] = u.Username
		}
	}
	for _, r := range rules {
		if e := checkAccountOwnership(s.deps.Accounts.DB(), actor, r.AccountID); e != nil {
			if r.CreatedBy == actor.ID && r.BlockedReason != "" {
				r.AccountID = 0
				views = append(views, RuleView{RecurringRule: r, CreatedByName: names[r.CreatedBy]})
			}
			continue
		}
		if e := checkCategoryOwnership(s.deps.Accounts.DB(), actor, r.CategoryID); e != nil {
			continue
		}
		views = append(views, RuleView{RecurringRule: r, CreatedByName: names[r.CreatedBy]})
	}
	return views, nil
}

// Create 创建模板：next_run_date = start_date，从首次生成日起算。
// 创建资格同记账权限（specs/recurring-entries：管理员/记账员/普通成员可建，只读不可）。
func (s *RecurringService) Create(actor *model.User, in RuleInput) (*model.RecurringRule, error) {
	if !CanManageRuleCreate(actor) {
		return nil, ErrRuleForbidden
	}
	if err := s.validate(&in); err != nil {
		return nil, err
	}
	if e := checkAccountOwnership(s.deps.Accounts.DB(), actor, in.AccountID); e != nil {
		return nil, ErrRuleForbidden
	}
	if e := checkCategoryOwnership(s.deps.Accounts.DB(), actor, in.CategoryID); e != nil {
		return nil, ErrRuleForbidden
	}
	r := &model.RecurringRule{
		Amount:      in.Amount,
		Freq:        in.Freq,
		AccountID:   in.AccountID,
		CategoryID:  in.CategoryID,
		Note:        in.Note,
		StartDate:   in.StartDate,
		NextRunDate: in.StartDate,
		Status:      model.RuleEnabled,
		CreatedBy:   actor.ID,
	}
	if err := s.deps.Rules.Create(r); err != nil {
		return nil, err
	}
	return r, nil
}

// Update 修改模板（specs/recurring-entries「按创建者归属隔离」：管理员可动全部，
// 记账员/普通成员仅动自己创建的，越权 403）。
// 频率或首次生成日期变化时 next_run_date 回到 start_date 重新走查整条序列——
// 已生成的归属日幂等跳过、漏掉的补上；只改金额/账户/分类/备注不动节奏。
func (s *RecurringService) Update(actor *model.User, id int64, in RuleInput) (*model.RecurringRule, error) {
	r, err := s.deps.Rules.GetByID(id)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrRuleNotFound
	}
	if !CanManageRule(actor, r) {
		return nil, ErrRuleForbidden
	}
	if e := checkAccountOwnership(s.deps.Accounts.DB(), actor, in.AccountID); e != nil {
		return nil, ErrRuleForbidden
	}
	if r.BlockedReason == "" || r.CreatedBy != actor.ID {
		if e := checkAccountOwnership(s.deps.Accounts.DB(), actor, r.AccountID); e != nil {
			return nil, ErrRuleForbidden
		}
		if e := checkCategoryOwnership(s.deps.Accounts.DB(), actor, r.CategoryID); e != nil {
			return nil, ErrRuleForbidden
		}
	}
	if e := checkCategoryOwnership(s.deps.Accounts.DB(), actor, in.CategoryID); e != nil {
		return nil, ErrRuleForbidden
	}
	if err := s.validate(&in); err != nil {
		return nil, err
	}
	scheduleChanged := r.Freq != in.Freq || r.StartDate.Format(dayFmt) != in.StartDate.Format(dayFmt)
	r.Amount = in.Amount
	r.Freq = in.Freq
	r.AccountID = in.AccountID
	r.CategoryID = in.CategoryID
	r.Note = in.Note
	r.BlockedReason = ""
	r.StartDate = in.StartDate
	if scheduleChanged {
		r.NextRunDate = in.StartDate
	}
	if err := s.deps.Rules.Update(r); err != nil {
		return nil, err
	}
	return r, nil
}

// SetStatus 启用/暂停（归属校验同 Update）。暂停不改 next_run_date：暂停期间到期的期次
// 在重新启用后按「不重不漏」补齐（生成循环只认 next_run_date 与已存在归属日）。
func (s *RecurringService) SetStatus(actor *model.User, id int64, status model.RuleStatus) error {
	r, err := s.deps.Rules.GetByID(id)
	if err != nil {
		return err
	}
	if r == nil {
		return ErrRuleNotFound
	}
	if !CanManageRule(actor, r) {
		return ErrRuleForbidden
	}
	if e := checkAccountOwnership(s.deps.Accounts.DB(), actor, r.AccountID); e != nil {
		return ErrRuleForbidden
	}
	if status != model.RuleEnabled && status != model.RulePaused {
		return ErrRuleStatusInvalid
	}
	if e := checkCategoryOwnership(s.deps.Accounts.DB(), actor, r.CategoryID); e != nil {
		return ErrRuleForbidden
	}
	r.Status = status
	return s.deps.Rules.Update(r)
}

// Delete 删除模板（归属校验同 Update）；已生成流水保留，仅模板不再产生后续期次。
func (s *RecurringService) Delete(actor *model.User, id int64) error {
	r, err := s.deps.Rules.GetByID(id)
	if err != nil {
		return err
	}
	if r == nil {
		return ErrRuleNotFound
	}
	if !CanManageRule(actor, r) {
		return ErrRuleForbidden
	}
	if r.BlockedReason == "" || r.CreatedBy != actor.ID {
		if e := checkAccountOwnership(s.deps.Accounts.DB(), actor, r.AccountID); e != nil {
			return ErrRuleForbidden
		}
		if e := checkCategoryOwnership(s.deps.Accounts.DB(), actor, r.CategoryID); e != nil {
			return ErrRuleForbidden
		}
	}
	return s.deps.Rules.Delete(id)
}

// EnsureDue 生成所有到期未生成的期次（cron 与前端懒补齐共用；幂等可重复调用）。
// 返回本轮新生成笔数。同一时刻只有一处在跑（mu），重复触发由唯一约束兜底。
func (s *RecurringService) EnsureDue(now time.Time) (int, error) {
	return s.ensureDue(now, false)
}
func (s *RecurringService) ensureDue(now time.Time, notify bool) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 半开区间截止到明天，覆盖「今天到期」
	rules, err := s.deps.Rules.ListDueBefore(now.AddDate(0, 0, 1).Format(dayFmt))
	if err != nil {
		return 0, err
	}
	total := 0
	generatedByOwner := map[int64]int{}
	var firstErr error
	for i := range rules {
		n, err := s.runRule(&rules[i], now)
		total += n
		generatedByOwner[rules[i].CreatedBy] += n
		if err != nil && firstErr == nil {
			// 一个模板的硬错误（如账户被停用）不阻塞其余模板；它的进度停在断点下轮重试
			firstErr = err
		}
	}
	if notify && s.deps.Notifs != nil {
		for owner, n := range generatedByOwner {
			if n == 0 {
				continue
			}
			payload, e := json.Marshal(RecurringPatchPayload{Generated: n})
			if e == nil {
				id := owner
				if e = s.deps.Notifs.Create(&model.Notification{OwnerID: &id, Kind: model.NotifRecurringPatched, Payload: string(payload)}); e != nil {
					logger.L().Warn("recurring patch notify failed", zap.Error(e))
				}
			}
		}
	}
	return total, firstErr
}

// RecurringPatchPayload 懒补齐提醒内容（spec：告知已补记，供用户核对）。
type RecurringPatchPayload struct {
	Generated int `json:"generated"`
}

// EnsureDueLazy 前端打开应用时的懒补齐（任务 9.3 / 10.1）：
// 与 cron 共用 EnsureDue 幂等生成；本轮有新生成时补一条「已补记」提醒。
// 正常到期由 cron 生成不提醒（非异常），只有懒补齐触发才提示核对。
// 提醒是旁路副作用：落库失败只记日志，不把已成功的生成报成失败。
func (s *RecurringService) EnsureDueLazy(now time.Time) (int, error) {
	return s.ensureDue(now, true)
}

// runRule 逐期推进一个模板直到下一期晚于今天；无论中途成败都把推进量落库（断点续跑）。
func (s *RecurringService) runRule(r *model.RecurringRule, now time.Time) (generated int, err error) {
	today := now.Format(dayFmt)
	origin := r.NextRunDate
	defer func() {
		if !r.NextRunDate.After(origin) {
			return
		}
		if advErr := s.deps.Rules.Advance(r.ID, r.NextRunDate); advErr != nil && err == nil {
			err = advErr
		}
	}()
	for steps := 0; r.NextRunDate.Format(dayFmt) <= today; steps++ {
		if steps >= maxCatchUpPerRun {
			return generated, ErrRuleCatchUpTooLong
		}
		done, oErr := s.runOccurrence(r)
		if r.Status == model.RulePaused {
			return generated, oErr
		}
		if done {
			generated++
		}
		if oErr != nil {
			return generated, oErr
		}
	}
	return generated, nil
}

// runOccurrence 处理一期：已存在（重复触发）幂等跳过，否则生成；成功与否都推进节奏。
func (s *RecurringService) runOccurrence(r *model.RecurringRule) (bool, error) {
	actor := &model.User{ID: r.CreatedBy}
	if e := checkAccountOwnership(s.deps.Accounts.DB(), actor, r.AccountID); e != nil {
		r.Status = model.RulePaused
		r.BlockedReason = "账户归属不匹配，请选择自己的账户"
		return false, s.deps.Rules.Update(r)
	}
	if e := checkCategoryOwnership(s.deps.Accounts.DB(), actor, r.CategoryID); e != nil {
		r.Status = model.RulePaused
		r.BlockedReason = "分类归属不匹配，请选择自己的分类"
		return false, s.deps.Rules.Update(r)
	}
	date := r.NextRunDate
	next := NextRunAfter(date, r.StartDate, r.Freq)

	exists, err := s.deps.Dates.ExistsByRuleDate(r.ID, date)
	if err != nil {
		return false, err
	}
	if exists {
		r.NextRunDate = next
		return false, nil
	}
	if _, err := s.deps.Txns.CreateFromRule(r, date); err != nil {
		if errors.Is(err, ErrTxnClosedPeriod) || isDupErr(err) {
			// 封账硬锁优先：跳过该期次不回写历史；唯一约束冲突 = 另一路已生成，均推进不重跑
			r.NextRunDate = next
			return false, nil
		}
		if errors.Is(err, liability.ErrConflict) {
			r.BlockedReason = err.Error()
			if updateErr := s.deps.Rules.Update(r); updateErr != nil {
				return false, errors.Join(err, updateErr)
			}
		}
		// 账户停用等硬错误：停在本期不推进，下轮 cron 重试（不静默吞掉）
		return false, err
	}
	if r.BlockedReason != "" {
		r.BlockedReason = ""
		if err := s.deps.Rules.Update(r); err != nil {
			return false, err
		}
	}
	r.NextRunDate = next
	return true, nil
}

// NextRunAfter 计算下一期日期（纯函数，任务 9.1 单测覆盖四种频率）：
// 每日 +1 天、每周 +7 天；
// 每月锚定首次生成日的日号（短月钳到月末，避免从 31 日漂移成 28 日）；
// 每年锚定月-日（2 月 29 日平年钳 28、闰年自然回到 29）。
func NextRunAfter(current, start time.Time, freq model.RuleFreq) time.Time {
	switch freq {
	case model.FreqDaily:
		return current.AddDate(0, 0, 1)
	case model.FreqWeekly:
		return current.AddDate(0, 0, 7)
	case model.FreqMonthly:
		y, m := current.Year(), int(current.Month())+1
		if m > 12 {
			y++
			m = 1
		}
		return time.Date(y, time.Month(m), min(start.Day(), daysIn(y, time.Month(m))),
			0, 0, 0, 0, current.Location())
	case model.FreqYearly:
		y := current.Year() + 1
		return time.Date(y, start.Month(), min(start.Day(), daysIn(y, start.Month())),
			0, 0, 0, 0, current.Location())
	default:
		return current
	}
}

// daysIn 月份天数（用下月 0 日归一取当月最后一天）。
func daysIn(y int, m time.Month) int {
	return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// isDupErr 唯一约束冲突：MySQL "Duplicate entry" / SQLite "UNIQUE constraint failed"。
func isDupErr(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "Duplicate") || strings.Contains(msg, "UNIQUE constraint failed")
}
