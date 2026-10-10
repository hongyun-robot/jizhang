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
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"jizhang/internal/router"
	"jizhang/internal/service"
	"net"
	"net/http"
	"os"
	"testing"
	"time"
)

// 浏览器验收使用内存账本和临时 Redis，启动/停止都只影响这个测试进程。
func TestVoucherBrowserServer(t *testing.T) {
	if os.Getenv("VOUCHER_BROWSER_SERVER") != "1" {
		t.Skip("explicit browser fixture only")
	}
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
	if err := db.AutoMigrate(&model.User{}, &model.Setting{}, &model.Account{}, &model.Category{}, &model.Transaction{}, &model.Budget{}, &model.RecurringRule{}, &model.PeriodClosing{}, &model.AuditLog{}, &model.Notification{}, &model.Voucher{}, &model.VoucherEvent{}, &model.VoucherPlatform{}); err != nil {
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
	account := model.Account{Name: "测试现金", Type: model.AccountAsset, Status: model.StatusActive, OpeningBalance: decimal.NewFromInt(1000)}
	category := model.Category{Name: "测试餐饮", Kind: model.KindExpense}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&category).Error; err != nil {
		t.Fatal(err)
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
	budgets := service.NewBudgetService(service.BudgetDeps{Budgets: repository.NewBudgetRepo(db), Stats: statsRepo, Cats: cats, Notifs: notifs, Periods: periods})
	txn.OnWrite = stats.InvalidateAll
	txn.OnWritePeriod = func(keys ...string) {
		for _, key := range keys {
			if err := budgets.Evaluate(key); err != nil {
				t.Error(err)
			}
		}
	}
	r := router.New([]string{"http://127.0.0.1:5601", "http://localhost:5601"}, router.Deps{AuthSvc: auth, UserSvc: service.NewUserService(users, auth), AccountSvc: service.NewAccountService(accounts), CategorySvc: service.NewCategoryService(cats), TxnSvc: txn, StatsSvc: stats, PeriodSvc: periods, ClosingSvc: service.NewClosingService(service.ClosingDeps{Closings: closings, Audits: repository.NewAuditRepo(db), Periods: periods, Stats: statsRepo}), BudgetSvc: budgets, RecurringSvc: service.NewRecurringService(service.RuleDeps{Rules: repository.NewRecurringRepo(db), Dates: repository.NewTransactionRepo(db), Txns: txn, Accounts: accounts, Cats: cats, Notifs: notifs, Users: users}), NotifSvc: service.NewNotificationService(service.NotifDeps{Notifs: notifs, Accounts: accounts}), VoucherSvc: service.NewVoucherService(db, txn)})
	stop := make(chan struct{}, 1)
	r.GET("/api/v1/__test__/info", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"fixture": "voucher-browser"}) })
	r.POST("/__test__/stop", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		select {
		case stop <- struct{}{}:
		default:
		}
	})
	listener, err := net.Listen("tcp", "127.0.0.1:18084")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: r, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			t.Error(err)
		}
	}()
	t.Log("VOUCHER_BROWSER_READY http://127.0.0.1:18084")
	select {
	case <-stop:
	case <-time.After(20 * time.Minute):
		t.Log("browser fixture auto-stopped after 20 minutes")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Error(err)
	}
}
