package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/shopspring/decimal"

	"jizhang/internal/cache"
	"jizhang/internal/logger"
	"jizhang/internal/model"
	"jizhang/internal/repository"

	"go.uber.org/zap"
)

// StatsCacheTTL 统计缓存时长（statistics spec：约 30 秒）。
const StatsCacheTTL = 30 * time.Second

// maxStatsDays 自由区间上限，防止超长区间逐日推演/零填充过载。
const maxStatsDays = 400

// 统计业务错误。
var (
	ErrInvalidStatsRange  = errors.New("统计区间无效：需为 YYYY-MM-DD、起不晚于止、跨度不超过 400 天")
	ErrInvalidGranularity = errors.New("趋势粒度须为 day/week/month")
	ErrInvalidStatsYear   = errors.New("年份须在 2000-2100 之间")
)

// StatsService 统计查询：分类占比/趋势/环比/账期汇总/资产走势。
// 全部走同一份区间解析与缓存（写操作经 InvalidateAll 主动失效）。
type StatsService struct {
	repo    *repository.StatsRepo
	accts   *repository.AccountRepo
	cats    *repository.CategoryRepo
	periods *PeriodService
	cache   cache.StatsCache
	ownerID int64
}

func (s *StatsService) ForUser(actor *model.User) *StatsService {
	copy := *s
	if !CanView(actor) {
		copy.ownerID = -1
		return &copy
	}
	if actor != nil {
		copy.ownerID = actor.ID
		copy.repo = s.repo.ForOwner(actor.ID)
		copy.accts = s.accts.ForOwner(actor.ID)
		copy.cats = s.cats.ForOwner(actor.ID)
	}
	return &copy
}

// NewStatsService 构造统计服务（cache 可为 nil，测试环境降级为无缓存）。
func NewStatsService(
	repo *repository.StatsRepo,
	accts *repository.AccountRepo,
	cats *repository.CategoryRepo,
	periods *PeriodService,
	c cache.StatsCache,
) *StatsService {
	return &StatsService{repo: repo, accts: accts, cats: cats, periods: periods, cache: c}
}

// InvalidateAll 写操作后主动失效全部统计缓存（stats:* 前缀）。
func (s *StatsService) InvalidateAll() {
	if s.cache == nil {
		return
	}
	if err := s.cache.DelByPrefix("stats:"); err != nil {
		logger.L().Warn("invalidate stats cache failed", zap.Error(err))
	}
}

// resolveRange 解析自由区间：两者皆空默认当前账期（含端点闭区间）。
func (s *StatsService) resolveRange(start, end string) (time.Time, time.Time, error) {
	if start == "" && end == "" {
		st, en, ok := s.periods.Range(s.periods.CurrentKey(time.Now()))
		if !ok {
			return time.Time{}, time.Time{}, ErrInvalidStatsRange
		}
		return st, en.AddDate(0, 0, -1), nil // Range 为半开区间，转闭区间
	}
	st, err1 := time.ParseInLocation("2006-01-02", start, time.Local)
	en, err2 := time.ParseInLocation("2006-01-02", end, time.Local)
	if err1 != nil || err2 != nil || en.Before(st) {
		return time.Time{}, time.Time{}, ErrInvalidStatsRange
	}
	if int(en.Sub(st).Hours()/24)+1 > maxStatsDays {
		return time.Time{}, time.Time{}, ErrInvalidStatsRange
	}
	return st, en, nil
}

// cached 30 秒缓存读穿：命中返回原始 JSON，未命中构建后写缓存。
func (s *StatsService) cached(key string, build func() (any, error)) (json.RawMessage, bool, error) {
	if s.ownerID < 0 {
		return nil, false, ErrTxnForbidden
	}
	if s.ownerID > 0 {
		key = fmt.Sprintf("stats:user:%d:%s", s.ownerID, key)
	}
	if s.cache != nil {
		if v, ok, err := s.cache.Get(key); err == nil && ok {
			return json.RawMessage(v), true, nil
		}
	}
	data, err := build()
	if err != nil {
		return nil, false, err
	}
	b, err := json.Marshal(data)
	if err != nil {
		return nil, false, err
	}
	if s.cache != nil {
		_ = s.cache.Set(key, string(b), StatsCacheTTL)
	}
	return b, false, nil
}

// ---------- 分类占比 ----------

type shareItem struct {
	ID      int64           `json:"id"`
	Name    string          `json:"name"`
	Amount  decimal.Decimal `json:"amount"`
	Percent float64         `json:"percent"`
}

type shareGroup struct {
	Total decimal.Decimal `json:"total"`
	Items []shareItem     `json:"items"`
}

type shareResponse struct {
	Start   string     `json:"start"`
	End     string     `json:"end"`
	Expense shareGroup `json:"expense"`
	Income  shareGroup `json:"income"`
}

// CategoryShare 区间内收支按一级分类占比（子分类上卷；各项合计=区间总额）。
func (s *StatsService) CategoryShare(start, end string) (json.RawMessage, bool, error) {
	st, en, err := s.resolveRange(start, end)
	if err != nil {
		return nil, false, err
	}
	key := fmt.Sprintf("stats:category:%s:%s", st.Format("2006-01-02"), en.Format("2006-01-02"))
	return s.cached(key, func() (any, error) {
		rows, err := s.repo.CategoryTotals(st, en)
		if err != nil {
			return nil, err
		}
		cats, err := s.cats.List()
		if err != nil {
			return nil, err
		}
		nameByID := make(map[int64]string, len(cats))
		for _, c := range cats {
			nameByID[c.ID] = c.Name
		}

		exp := shareGroup{Items: []shareItem{}}
		inc := shareGroup{Items: []shareItem{}}
		for _, row := range rows {
			g := &exp
			if row.Kind == model.TxnIncome {
				g = &inc
			}
			name := nameByID[row.RootID]
			if row.RootID == 0 || name == "" {
				name = "未分类"
			}
			g.Total = g.Total.Add(row.Total)
			g.Items = append(g.Items, shareItem{ID: row.RootID, Name: name, Amount: row.Total})
		}
		exp.finalize()
		inc.finalize()
		return shareResponse{
			Start: st.Format("2006-01-02"), End: en.Format("2006-01-02"),
			Expense: exp, Income: inc,
		}, nil
	})
}

// finalize 金额倒序并计算占比。
func (g *shareGroup) finalize() {
	sort.SliceStable(g.Items, func(i, j int) bool {
		return g.Items[i].Amount.GreaterThan(g.Items[j].Amount)
	})
	if g.Total.IsZero() {
		return
	}
	for i := range g.Items {
		p := g.Items[i].Amount.Div(g.Total).Mul(decimal.NewFromInt(100)).Round(1)
		f, _ := p.Float64()
		g.Items[i].Percent = f
	}
}

// ---------- 收支趋势 ----------

// TrendPoint 趋势图一个数据点。
type TrendPoint struct {
	Key     string          `json:"key"`
	Label   string          `json:"label"`
	Income  decimal.Decimal `json:"income"`
	Expense decimal.Decimal `json:"expense"`
	Balance decimal.Decimal `json:"balance"`
}

type trendResponse struct {
	Granularity string       `json:"granularity"`
	Start       string       `json:"start"`
	End         string       `json:"end"`
	Points      []TrendPoint `json:"points"`
}

// Trend 区间内按 day/week/month 粒度的收入/支出/结余趋势。
func (s *StatsService) Trend(start, end, granularity string) (json.RawMessage, bool, error) {
	switch granularity {
	case "", "day", "week", "month":
	default:
		return nil, false, ErrInvalidGranularity
	}
	if granularity == "" {
		granularity = "day"
	}
	st, en, err := s.resolveRange(start, end)
	if err != nil {
		return nil, false, err
	}
	key := fmt.Sprintf("stats:trend:%s:%s:%s", st.Format("2006-01-02"), en.Format("2006-01-02"), granularity)
	return s.cached(key, func() (any, error) {
		daily, err := s.repo.DailyTotals(st, en)
		if err != nil {
			return nil, err
		}
		return trendResponse{
			Granularity: granularity,
			Start:       st.Format("2006-01-02"),
			End:         en.Format("2006-01-02"),
			Points:      bucketDaily(daily, st, en, granularity),
		}, nil
	})
}

// bucketDaily 将逐日聚合按粒度分桶并零填充（纯函数，供单测）。
// week 按周一对齐（首尾桶可为不完整周）；month 按自然月。
func bucketDaily(rows []repository.DailyRow, start, end time.Time, granularity string) []TrendPoint {
	lookup := make(map[string]repository.DailyRow, len(rows))
	for _, r := range rows {
		lookup[r.D.Format("2006-01-02")] = r
	}
	sum := func(from, to time.Time) (decimal.Decimal, decimal.Decimal) {
		var inc, exp decimal.Decimal
		for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
			if row, ok := lookup[d.Format("2006-01-02")]; ok {
				inc = inc.Add(row.Income)
				exp = exp.Add(row.Expense)
			}
		}
		return inc, exp
	}
	mk := func(key, label string, inc, exp decimal.Decimal) TrendPoint {
		return TrendPoint{Key: key, Label: label, Income: inc, Expense: exp, Balance: inc.Sub(exp)}
	}

	points := []TrendPoint{}
	switch granularity {
	case "day":
		for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
			inc, exp := sum(d, d)
			points = append(points, mk(d.Format("2006-01-02"), d.Format("01-02"), inc, exp))
		}
	case "week":
		for bs := mondayOf(start); !bs.After(end); bs = bs.AddDate(0, 0, 7) {
			from := bs
			if from.Before(start) {
				from = start
			}
			to := bs.AddDate(0, 0, 6)
			if to.After(end) {
				to = end
			}
			y, w := bs.ISOWeek()
			inc, exp := sum(from, to)
			points = append(points, mk(bs.Format("2006-01-02"), fmt.Sprintf("%d-W%02d", y, w), inc, exp))
		}
	case "month":
		for ms := firstOfMonth(start); !ms.After(end); ms = ms.AddDate(0, 1, 0) {
			from := ms
			if from.Before(start) {
				from = start
			}
			to := ms.AddDate(0, 1, 0).AddDate(0, 0, -1)
			if to.After(end) {
				to = end
			}
			inc, exp := sum(from, to)
			label := fmt.Sprintf("%d年%d月", ms.Year(), int(ms.Month()))
			points = append(points, mk(ms.Format("2006-01"), label, inc, exp))
		}
	}
	return points
}

func mondayOf(t time.Time) time.Time {
	offset := (int(t.Weekday()) + 6) % 7
	return t.AddDate(0, 0, -offset)
}

func firstOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}

// ---------- 本期环比 ----------

// PrevMonthWindow 上一同比窗口：各端点整体前移一个月，目标月天数不足时
// 按目标月末收敛（如 3/31 → 2/28）。覆盖 statistics spec 场景：
// 2026-09-26~10-25 → 2026-08-26~09-25（账期口径自然成立）。
func PrevMonthWindow(start, end time.Time) (time.Time, time.Time) {
	return shiftMonth(start, -1), shiftMonth(end, -1)
}

func shiftMonth(t time.Time, delta int) time.Time {
	y, m, d := t.Date()
	first := time.Date(y, m+time.Month(delta), 1, 0, 0, 0, 0, t.Location())
	lastDay := time.Date(first.Year(), first.Month()+1, 0, 0, 0, 0, 0, t.Location()).Day()
	if d > lastDay {
		d = lastDay
	}
	return time.Date(first.Year(), first.Month(), d, 0, 0, 0, 0, t.Location())
}

type statsTotals struct {
	Income  decimal.Decimal `json:"income"`
	Expense decimal.Decimal `json:"expense"`
	Balance decimal.Decimal `json:"balance"`
}

type compareChange struct {
	IncomePercent  *float64 `json:"income_percent"`
	ExpensePercent *float64 `json:"expense_percent"`
	BalancePercent *float64 `json:"balance_percent"`
}

type compareResponse struct {
	Start         string        `json:"start"`
	End           string        `json:"end"`
	PreviousStart string        `json:"previous_start"`
	PreviousEnd   string        `json:"previous_end"`
	Current       statsTotals   `json:"current"`
	Previous      statsTotals   `json:"previous"`
	Change        compareChange `json:"change"`
}

// Compare 当前区间与上一同比窗口（前移一月）的收支结余及增减幅度。
// 上期为 0 时对应幅度为 null（前端显示“—”）。
func (s *StatsService) Compare(start, end string) (json.RawMessage, bool, error) {
	st, en, err := s.resolveRange(start, end)
	if err != nil {
		return nil, false, err
	}
	pst, pen := PrevMonthWindow(st, en)
	key := fmt.Sprintf("stats:compare:%s:%s", st.Format("2006-01-02"), en.Format("2006-01-02"))
	return s.cached(key, func() (any, error) {
		cur, err := s.totalsFor(st, en)
		if err != nil {
			return nil, err
		}
		prev, err := s.totalsFor(pst, pen)
		if err != nil {
			return nil, err
		}
		return compareResponse{
			Start:         st.Format("2006-01-02"),
			End:           en.Format("2006-01-02"),
			PreviousStart: pst.Format("2006-01-02"),
			PreviousEnd:   pen.Format("2006-01-02"),
			Current:       cur,
			Previous:      prev,
			Change: compareChange{
				IncomePercent:  percentOf(cur.Income, prev.Income),
				ExpensePercent: percentOf(cur.Expense, prev.Expense),
				BalancePercent: percentOf(cur.Balance, prev.Balance),
			},
		}, nil
	})
}

func (s *StatsService) totalsFor(start, end time.Time) (statsTotals, error) {
	rows, err := s.repo.DailyTotals(start, end)
	if err != nil {
		return statsTotals{}, err
	}
	var t statsTotals
	for _, r := range rows {
		t.Income = t.Income.Add(r.Income)
		t.Expense = t.Expense.Add(r.Expense)
	}
	t.Balance = t.Income.Sub(t.Expense)
	return t, nil
}

// percentOf 增减幅度（%），上期为 0 返回 nil。
func percentOf(cur, prev decimal.Decimal) *float64 {
	if prev.IsZero() {
		return nil
	}
	p := cur.Sub(prev).Div(prev.Abs()).Mul(decimal.NewFromInt(100)).Round(1)
	f, _ := p.Float64()
	return &f
}

// ---------- 账期月度汇总 ----------

type periodItem struct {
	PeriodKey string          `json:"period_key"`
	Income    decimal.Decimal `json:"income"`
	Expense   decimal.Decimal `json:"expense"`
	Balance   decimal.Decimal `json:"balance"`
}

type periodSummaryResponse struct {
	Year    int          `json:"year"`
	Periods []periodItem `json:"periods"`
}

// PeriodSummary 指定年份各账期的收入/支出/结余（当年截到本期）。
// yearStr 为空取当前年。
func (s *StatsService) PeriodSummary(yearStr string) (json.RawMessage, bool, error) {
	year := time.Now().Year()
	if yearStr != "" {
		y, err := strconv.Atoi(yearStr)
		if err != nil || y < 2000 || y > 2100 {
			return nil, false, ErrInvalidStatsYear
		}
		year = y
	}
	key := fmt.Sprintf("stats:periods:%d", year)
	return s.cached(key, func() (any, error) {
		rows, err := s.repo.PeriodTotals(year)
		if err != nil {
			return nil, err
		}
		byKey := make(map[string]repository.PeriodRow, len(rows))
		for _, r := range rows {
			byKey[r.PeriodKey] = r
		}
		lastMonth := time.Month(12)
		if year == time.Now().Year() {
			lastMonth = time.Now().Month()
		}
		items := make([]periodItem, 0, 12)
		for m := time.Month(1); m <= lastMonth; m++ {
			pk := fmt.Sprintf("%04d-%02d", year, int(m))
			row := byKey[pk]
			bal := row.Income.Sub(row.Expense)
			items = append(items, periodItem{PeriodKey: pk, Income: row.Income, Expense: row.Expense, Balance: bal})
		}
		return periodSummaryResponse{Year: year, Periods: items}, nil
	})
}

// ---------- 资产走势 ----------

type assetAcct struct {
	ID         int64             `json:"account_id"`
	Name       string            `json:"name"`
	Type       model.AccountType `json:"type"`
	Balance    *decimal.Decimal  `json:"balance"`
	AssetClass string            `json:"asset_class,omitempty"`
	NAVDate    string            `json:"nav_date,omitempty"`
}

type assetPoint struct {
	Date     string           `json:"date"`
	Total    *decimal.Decimal `json:"total"`
	Accounts []assetAcct      `json:"accounts"`
}

type assetResponse struct {
	Start       string           `json:"start"`
	End         string           `json:"end"`
	Points      []assetPoint     `json:"points"`
	EndingTotal *decimal.Decimal `json:"ending_total"`
}

// Assets 逐日总资产与各账户余额（负债以负值贡献）。
// 期末（区间最后一天）总值与账户余额聚合接口同口径。
func (s *StatsService) Assets(start, end string) (json.RawMessage, bool, error) {
	st, en, err := s.resolveRange(start, end)
	if err != nil {
		return nil, false, err
	}
	key := fmt.Sprintf("stats:assets:%s:%s", st.Format("2006-01-02"), en.Format("2006-01-02"))
	return s.cached(key, func() (any, error) {
		accounts, err := s.accts.List()
		if err != nil {
			return nil, err
		}
		if s.ownerID > 0 {
			visible := []model.Account{}
			for _, a := range accounts {
				if a.OwnerID != nil && *a.OwnerID == s.ownerID {
					visible = append(visible, a)
				}
			}
			accounts = visible
		}
		nets, err := s.repo.DailyAccountNets(en)
		if err != nil {
			return nil, err
		}
		fundIDs := []int64{}
		for _, account := range accounts {
			if account.IsFund() {
				fundIDs = append(fundIDs, account.ID)
			}
		}
		fundValues := map[string]map[int64]repository.FundAccountValue{}
		if len(fundIDs) > 0 {
			if s.accts.FundValues == nil {
				return nil, ErrFundPrice
			}
			dates := []time.Time{}
			for d := st; !d.After(en); d = d.AddDate(0, 0, 1) {
				dates = append(dates, d)
			}
			fundValues, err = s.accts.FundValues(s.accts.DB(), fundIDs, dates)
			if err != nil {
				return nil, err
			}
		}

		goldIDs := []int64{}
		for _, a := range accounts {
			if a.IsGold() {
				goldIDs = append(goldIDs, a.ID)
			}
		}
		if len(goldIDs) > 0 {
			if s.accts.GoldValues == nil {
				return nil, ErrGoldInvalid
			}
			dates := []time.Time{}
			for d := st; !d.After(en); d = d.AddDate(0, 0, 1) {
				dates = append(dates, d)
			}
			goldValues, e := s.accts.GoldValues(s.accts.DB(), goldIDs, dates)
			if e != nil {
				return nil, e
			}
			for day, rows := range goldValues {
				if fundValues[day] == nil {
					fundValues[day] = map[int64]repository.FundAccountValue{}
				}
				for id, v := range rows {
					fundValues[day][id] = v
				}
			}
		}
		balance := make(map[int64]decimal.Decimal, len(accounts))
		for _, a := range accounts {
			balance[a.ID] = a.OpeningBalance
		}
		dayNets := map[string]map[int64]decimal.Decimal{}
		for _, n := range nets {
			if _, ok := balance[n.Aid]; !ok {
				balance[n.Aid] = decimal.Zero
			}
			if n.D.Before(st) {
				balance[n.Aid] = balance[n.Aid].Add(n.Net) // 区间前净额计入初始
				continue
			}
			dk := n.D.Format("2006-01-02")
			if dayNets[dk] == nil {
				dayNets[dk] = map[int64]decimal.Decimal{}
			}
			dayNets[dk][n.Aid] = dayNets[dk][n.Aid].Add(n.Net)
		}

		points := make([]assetPoint, 0, int(en.Sub(st).Hours()/24)+1)
		var ending *decimal.Decimal
		for d := st; !d.After(en); d = d.AddDate(0, 0, 1) {
			dk := d.Format("2006-01-02")
			if m := dayNets[dk]; m != nil {
				for aid, net := range m {
					balance[aid] = balance[aid].Add(net)
				}
			}
			total := decimal.Zero
			complete := true
			acctOut := make([]assetAcct, 0, len(accounts))
			for _, a := range accounts {
				b := balance[a.ID]
				known := true
				navDate := ""
				if a.IsInvestment() {
					value := fundValues[dk][a.ID]
					b = value.Value
					known = value.Complete
					navDate = value.NAVDate
				}
				total = total.Add(b)
				var shown *decimal.Decimal
				if known {
					shown = decimalPointer(b.Round(2))
				} else {
					complete = false
				}
				acctOut = append(acctOut, assetAcct{ID: a.ID, Name: a.Name, Type: a.Type, Balance: shown, AssetClass: a.AssetClass, NAVDate: navDate})
			}
			ending = nil
			if complete {
				ending = decimalPointer(total.Round(2))
			}
			points = append(points, assetPoint{Date: dk, Total: ending, Accounts: acctOut})
		}
		return assetResponse{
			Start: st.Format("2006-01-02"), End: en.Format("2006-01-02"),
			Points: points, EndingTotal: ending,
		}, nil
	})
}
