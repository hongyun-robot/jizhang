package router_test

import (
	"context"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"jizhang/internal/cache"
	"jizhang/internal/ccb"
	"jizhang/internal/fuyao"
	"jizhang/internal/liability"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"jizhang/internal/router"
	"jizhang/internal/service"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// 浏览器验收使用内存账本和临时 Redis，启动/停止都只影响这个测试进程。
func TestVoucherBrowserServer(t *testing.T) {
	if os.Getenv("VOUCHER_BROWSER_SERVER") != "1" {
		t.Skip("explicit browser fixture only")
	}
	runBrowserServer(t, false)
}

func TestLiabilityBrowserServer(t *testing.T) {
	if os.Getenv("LIABILITY_BROWSER_SERVER") != "1" {
		t.Skip("explicit isolated liability browser fixture only")
	}
	runBrowserServer(t, true)
}

func runBrowserServer(t *testing.T, liabilityFixture bool) {
	fundFixture := os.Getenv("FUND_BROWSER_SERVER") == "1"
	goldFixture := os.Getenv("GOLD_BROWSER_SERVER") == "1"
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	defer pool.Close()
	if err := db.AutoMigrate(&model.CatalogOrderRequest{}, &model.User{}, &model.Setting{}, &model.Account{}, &model.Category{}, &model.Transaction{}, &model.Budget{}, &model.RecurringRule{}, &model.PeriodClosing{}, &model.AuditLog{}, &model.Notification{}, &model.Voucher{}, &model.VoucherEvent{}, &model.VoucherPlatform{}); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.LiabilityModels()...); err != nil {
		t.Fatal(err)
	}
	if fundFixture {
		if err := db.AutoMigrate(&model.FundInstrument{}, &model.FundPosition{}, &model.FundEvent{}, &model.FundMutation{}, &model.FundNAV{}, &model.FundNAVRevision{}, &model.FundValuation{}, &model.FundAudit{}, &model.FundDividend{}); err != nil {
			t.Fatal(err)
		}
	}
	if goldFixture {
		if err := db.AutoMigrate(&model.GoldPosition{}, &model.GoldEvent{}, &model.GoldMutation{}, &model.GoldAudit{}, &model.GoldQuoteObservation{}, &model.GoldQuoteSync{}, &model.GoldValuation{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&[]model.LiabilityAccountType{{Code: "credit_card", Name: "信用卡", DefaultAccount: true}, {Code: "huabei", Name: "花呗", Sort: 1, DefaultAccount: true}, {Code: "jd_baitiao", Name: "京东白条", Sort: 2, DefaultAccount: true}, {Code: "douyin_monthly", Name: "抖音月付", Sort: 3, DefaultAccount: true}, {Code: "meituan_monthly", Name: "美团月付", Sort: 4, DefaultAccount: true}, {Code: "other_consumer", Name: "其他消费信用", Sort: 5}, {Code: "test_monthly", Name: "测试月付", Sort: 6}}).Error; err != nil {
		t.Fatal(err)
	}
	password, err := bcrypt.GenerateFromPassword([]byte("voucher-test-only"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []model.User{{Username: "voucher-admin", Role: model.RoleAdmin, Status: model.StatusActive, PasswordHash: string(password)}, {Username: "voucher-viewer", Role: model.RoleViewer, Status: model.StatusActive, PasswordHash: string(password)}, {Username: "voucher-member", Role: model.RoleMember, Status: model.StatusActive, PasswordHash: string(password)}, {Username: "voucher-recorder", Role: model.RoleRecorder, Status: model.StatusActive, PasswordHash: string(password)}} {
		if err := db.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
	}
	for owner := int64(1); owner <= 4; owner++ {
		account := model.Account{OwnerID: &owner, Name: "测试现金", Type: model.AccountAsset, Status: model.StatusActive, OpeningBalance: decimal.NewFromInt(1000)}
		category := model.Category{OwnerID: &owner, Name: "测试餐饮", Kind: model.KindExpense}
		if err := db.Create(&account).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&category).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.AutoMigrate(&model.AccountCreateRequest{}, &model.AccountTemplate{}, &model.CategoryTemplate{}, &model.VoucherPlatformTemplate{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"支付宝", "微信", "淘宝", "天猫", "京东", "拼多多", "抖音", "美团", "饿了么"} {
		if err := db.Create(&model.VoucherPlatformTemplate{Name: name}).Error; err != nil {
			t.Fatal(err)
		}
	}
	rds := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: rds.Addr()})
	defer client.Close()
	users := repository.NewUserRepo(db)
	accounts := repository.NewAccountRepo(db)
	cats := repository.NewCategoryRepo(db)
	periods := service.NewPeriodService(repository.NewSettingRepo(db))
	statsRepo := repository.NewStatsRepo(db)
	closings := repository.NewClosingRepo(db)
	notifs := repository.NewNotificationRepo(db)
	auth := service.NewAuthService(users, client, "voucher-browser-fixture-only", 30*time.Minute, 24*time.Hour, false, "/api/v1/auth")
	txn := service.NewTransactionService(service.TxnDeps{Txns: repository.NewTransactionRepo(db), Accounts: accounts, Cats: cats, Closings: closings, Periods: periods})
	stats := service.NewStatsService(statsRepo, accounts, cats, periods, cache.NewStatsCache(client))
	var gold *service.GoldService
	var goldMarket *goldBrowserMarket
	var funds *service.FundService
	var fundMarket *fundBrowserMarket
	budgets := service.NewBudgetService(service.BudgetDeps{Budgets: repository.NewBudgetRepo(db), Stats: statsRepo, Cats: cats, Notifs: notifs, Periods: periods})
	txn.OnWrite = stats.InvalidateAll
	liab := service.NewLiabilityService(db, txn)
	var browserTime atomic.Int64
	initialDate, _ := liability.Parse("2026-11-01")
	browserTime.Store(initialDate.Unix())
	if fundFixture {
		date := time.Date(2026, 10, 13, 19, 0, 0, 0, fuyao.Shanghai)
		browserTime.Store(date.Unix())
		fundMarket = newFundBrowserMarket()
		funds = service.NewFundService(db, txn, fundMarket)
		funds.Now = func() time.Time { return time.Unix(browserTime.Load(), 0).In(fuyao.Shanghai) }
		funds.BindAccounts(accounts)
		funds.OnWrite = stats.InvalidateAll
	}
	if goldFixture {
		browserTime.Store(time.Now().Unix())
		goldMarket = &goldBrowserMarket{now: func() time.Time { return time.Unix(browserTime.Load(), 0).In(ccb.Shanghai) }}
		gold = service.NewGoldService(db, txn, goldMarket)
		gold.Now = goldMarket.now
		gold.BindAccounts(accounts)
		gold.OnWrite = stats.InvalidateAll
	}
	if liabilityFixture {
		liab.Now = func() time.Time { return time.Unix(browserTime.Load(), 0).In(liability.Location) }
	}
	txn.OnWritePeriod = func(keys ...string) {
		for _, key := range keys {
			if err := budgets.Evaluate(key); err != nil {
				t.Error(err)
			}
		}
	}
	r := router.New([]string{"http://127.0.0.1:5601", "http://localhost:5601", "http://127.0.0.1:5602", "http://127.0.0.1:5603", "http://127.0.0.1:5612"}, router.Deps{AuthSvc: auth, UserSvc: service.NewUserService(users, auth), AccountSvc: service.NewAccountService(accounts), CategorySvc: service.NewCategoryService(cats), TxnSvc: txn, StatsSvc: stats, PeriodSvc: periods, ClosingSvc: service.NewClosingService(service.ClosingDeps{Closings: closings, Audits: repository.NewAuditRepo(db), Periods: periods, Stats: statsRepo}), BudgetSvc: budgets, RecurringSvc: service.NewRecurringService(service.RuleDeps{Rules: repository.NewRecurringRepo(db), Dates: repository.NewTransactionRepo(db), Txns: txn, Accounts: accounts, Cats: cats, Notifs: notifs, Users: users}), NotifSvc: service.NewNotificationService(service.NotifDeps{Notifs: notifs, Accounts: accounts}), VoucherSvc: service.NewVoucherService(db, txn), LiabilitySvc: liab, FundSvc: funds, GoldSvc: gold})
	stop := make(chan struct{}, 1)
	r.GET("/api/v1/__test__/info", func(c *gin.Context) {
		name := "voucher-browser"
		if liabilityFixture {
			name = "liability-browser"
		}
		if fundFixture {
			name = "fund-browser"
		}
		if goldFixture {
			name = "gold-browser"
		}
		c.JSON(http.StatusOK, gin.H{"fixture": name})
	})
	if goldFixture {
		r.POST("/api/v1/__test__/gold-quote", func(c *gin.Context) {
			var in struct {
				Buy     string `json:"buy"`
				Sell    string `json:"sell"`
				Failure bool   `json:"failure"`
				Advance int64  `json:"advance"`
			}
			if c.ShouldBindJSON(&in) != nil {
				c.Status(400)
				return
			}
			goldMarket.mu.Lock()
			goldMarket.buy = in.Buy
			goldMarket.sell = in.Sell
			goldMarket.failure = in.Failure
			goldMarket.mu.Unlock()
			if in.Advance == 0 {
				in.Advance = 31
			}
			browserTime.Add(in.Advance)
			c.JSON(200, gin.H{"calls": goldMarket.calls.Load()})
		})
		r.GET("/api/v1/__test__/gold-calls", func(c *gin.Context) { c.JSON(200, gin.H{"calls": goldMarket.calls.Load()}) })
	}
	if fundFixture {
		r.POST("/api/v1/__test__/fund-quote", func(c *gin.Context) {
			var input struct {
				Date    string `json:"date"`
				NAV     string `json:"nav"`
				Failure bool   `json:"failure"`
			}
			if c.ShouldBindJSON(&input) != nil {
				c.Status(400)
				return
			}
			date, e := liability.Parse(input.Date)
			if e != nil {
				c.Status(400)
				return
			}
			value, e := decimal.NewFromString(input.NAV)
			if e != nil || !value.IsPositive() {
				c.Status(400)
				return
			}
			fundMarket.mu.Lock()
			found := false
			for i := range fundMarket.quotes {
				if fundMarket.quotes[i].Date.Format("2006-01-02") == date.Format("2006-01-02") {
					fundMarket.quotes[i] = fuyao.NAV{Date: date, Value: value}
					found = true
					break
				}
			}
			if !found {
				fundMarket.quotes = append(fundMarket.quotes, fuyao.NAV{Date: date, Value: value})
			}
			fundMarket.failure = nil
			if input.Failure {
				fundMarket.failure = fuyao.ErrRateLimited
			}
			fundMarket.mu.Unlock()
			browserTime.Add(1800)
			c.JSON(200, gin.H{"queued": true})
		})
		r.POST("/api/v1/__test__/fund-date", func(c *gin.Context) {
			var input struct {
				Date string `json:"date"`
			}
			if c.ShouldBindJSON(&input) != nil {
				c.Status(400)
				return
			}
			date, e := liability.Parse(input.Date)
			if e != nil {
				c.Status(400)
				return
			}
			browserTime.Store(date.Unix())
			c.JSON(200, gin.H{"date": input.Date})
		})
	}
	if liabilityFixture {
		r.POST("/api/v1/__test__/date", func(c *gin.Context) {
			var in struct {
				Date string `json:"date"`
			}
			if c.ShouldBindJSON(&in) != nil {
				c.Status(400)
				return
			}
			date, e := liability.Parse(in.Date)
			if e != nil {
				c.Status(400)
				return
			}
			browserTime.Store(date.Unix())
			_, e = liab.EnsureDue(date)
			if e != nil {
				c.JSON(500, gin.H{"error": e.Error()})
				return
			}
			c.JSON(200, gin.H{"date": in.Date})
		})
	}
	r.POST("/__test__/stop", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		select {
		case stop <- struct{}{}:
		default:
		}
	})
	address := "127.0.0.1:18084"
	if liabilityFixture {
		address = "127.0.0.1:18085"
	}
	if fundFixture {
		address = "127.0.0.1:18086"
	}
	if goldFixture {
		address = "127.0.0.1:18087"
	}
	if configured := os.Getenv("BROWSER_FIXTURE_ADDR"); configured != "" {
		host, _, err := net.SplitHostPort(configured)
		if err != nil || (host != "127.0.0.1" && host != "localhost" && host != "::1") {
			t.Fatal("BROWSER_FIXTURE_ADDR must use a loopback address")
		}
		address = configured
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: r, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			t.Error(err)
		}
	}()
	t.Log("BROWSER_READY http://" + address)
	if fundFixture {
		workerCtx, cancelWorker := context.WithCancel(context.Background())
		defer cancelWorker()
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					funds.Ensure(workerCtx)
				case <-workerCtx.Done():
					return
				}
			}
		}()
	}
	lifetime := 20 * time.Minute
	if raw := os.Getenv("BROWSER_FIXTURE_TTL"); raw != "" {
		configured, e := time.ParseDuration(raw)
		if e != nil || configured < time.Minute || configured > 4*time.Hour {
			t.Fatal("BROWSER_FIXTURE_TTL must be between 1m and 4h")
		}
		lifetime = configured
	}
	select {
	case <-stop:
	case <-time.After(lifetime):
		t.Logf("browser fixture auto-stopped after %s", lifetime)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Error(err)
	}
}
