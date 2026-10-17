package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/robfig/cron/v3"

	"jizhang/internal/cache"
	"jizhang/internal/ccb"
	"jizhang/internal/cmbcwm"
	"jizhang/internal/config"
	"jizhang/internal/database"
	"jizhang/internal/fuyao"
	"jizhang/internal/liability"
	"jizhang/internal/logger"
	"jizhang/internal/repository"
	"jizhang/internal/router"
	"jizhang/internal/seed"
	"jizhang/internal/service"
	"jizhang/internal/suyin"
	"jizhang/internal/wealthdata"

	"go.uber.org/zap"
)

func main() {
	// 本地开发：存在 .env 则加载（缺省忽略，生产走容器环境变量）；
	// 兼容在仓库根或 backend/ 目录下启动两种方式
	if _, err := os.Stat(".env"); err == nil {
		_ = godotenv.Load(".env")
	} else {
		_ = godotenv.Load("../.env")
	}

	logger.Init()
	defer logger.Sync()

	cfg := config.Load()
	db := database.Init(cfg)
	if err := liability.CheckRepaymentCutover(db); err != nil {
		logger.L().Fatal("liability repayment cutover blocked", zap.Error(err))
	}
	if err := seed.Admin(db, cfg); err != nil {
		logger.L().Fatal("seed admin failed", zap.Error(err))
	}
	rdb := cache.Init(cfg)

	userRepo := repository.NewUserRepo(db)
	authSvc := service.NewAuthService(
		userRepo,
		rdb,
		cfg.JWTSecret,
		time.Duration(cfg.AccessTTLMin)*time.Minute,
		time.Duration(cfg.RefreshTTLHours)*time.Hour,
		cfg.CookieSecure,
		cfg.CookiePath,
	)
	userSvc := service.NewUserService(userRepo, authSvc)
	accountRepo := repository.NewAccountRepo(db)
	accountSvc := service.NewAccountService(accountRepo)
	categorySvc := service.NewCategoryService(repository.NewCategoryRepo(db))
	periodSvc := service.NewPeriodService(repository.NewSettingRepo(db))
	txnSvc := service.NewTransactionService(service.TxnDeps{
		Txns:     repository.NewTransactionRepo(db),
		Accounts: accountRepo,
		Cats:     repository.NewCategoryRepo(db),
		Closings: repository.NewClosingRepo(db),
		Periods:  periodSvc,
	})
	statsSvc := service.NewStatsService(
		repository.NewStatsRepo(db),
		accountRepo,
		repository.NewCategoryRepo(db),
		periodSvc,
		cache.NewStatsCache(rdb),
	)
	fundSvc := service.NewFundService(db, txnSvc, fuyao.New(cfg.FuyaoAPIKey))
	fundSvc.BindAccounts(accountRepo)
	fundSvc.OnWrite = statsSvc.InvalidateAll
	goldSvc := service.NewGoldService(db, txnSvc, ccb.New())
	goldSvc.BindAccounts(accountRepo)
	goldSvc.OnWrite = statsSvc.InvalidateAll
	wealthMarket := wealthdata.NewRouter(map[string]wealthdata.Source{
		"suyin":  suyin.New(cfg.WealthCollectorURL, cfg.WealthCollectorKey),
		"cmbcwm": cmbcwm.New(),
	})
	wealthSvc := service.NewWealthService(db, txnSvc, wealthMarket)
	wealthSvc.BindAccounts(accountRepo)
	wealthSvc.OnWrite = statsSvc.InvalidateOwner
	closingSvc := service.NewClosingService(service.ClosingDeps{
		Closings: repository.NewClosingRepo(db),
		Audits:   repository.NewAuditRepo(db),
		Periods:  periodSvc,
		Stats:    repository.NewStatsRepo(db),
	})
	budgetSvc := service.NewBudgetService(service.BudgetDeps{
		Budgets: repository.NewBudgetRepo(db),
		Stats:   repository.NewStatsRepo(db),
		Cats:    repository.NewCategoryRepo(db),
		Notifs:  repository.NewNotificationRepo(db),
		Periods: periodSvc,
	})
	recurringSvc := service.NewRecurringService(service.RuleDeps{
		Rules:    repository.NewRecurringRepo(db),
		Dates:    repository.NewTransactionRepo(db),
		Txns:     txnSvc,
		Accounts: accountRepo,
		Cats:     repository.NewCategoryRepo(db),
		Notifs:   repository.NewNotificationRepo(db),
		Users:    userRepo,
	})
	notifSvc := service.NewNotificationService(service.NotifDeps{
		Notifs:   repository.NewNotificationRepo(db),
		Accounts: accountRepo,
	})
	voucherSvc := service.NewVoucherService(db, txnSvc)
	liabilitySvc := service.NewLiabilityService(db, txnSvc)
	liabilitySvc.BeforeEnsure = func(now time.Time) error {
		if _, e := recurringSvc.EnsureDue(now); e != nil {
			return e
		}
		_, e := voucherSvc.EnsureDue(now)
		return e
	}
	// 写操作主动失效统计缓存（30 秒缓存的写后一致性）
	txnSvc.OnWrite = statsSvc.InvalidateAll
	accountSvc.OnWrite = statsSvc.InvalidateAll
	categorySvc.OnWrite = statsSvc.InvalidateAll
	// 流水写入后评估预算超支（提醒是写成功后的旁路副作用，失败只记日志不回滚）
	txnSvc.OnWritePeriod = func(keys ...string) {
		for _, k := range keys {
			if err := budgetSvc.Evaluate(k); err != nil {
				logger.L().Warn("budget evaluate failed", zap.String("period_key", k), zap.Error(err))
			}
		}
	}

	r := router.New(cfg.CORSOrigins, router.Deps{
		AuthSvc:      authSvc,
		UserSvc:      userSvc,
		AccountSvc:   accountSvc,
		CategorySvc:  categorySvc,
		TxnSvc:       txnSvc,
		StatsSvc:     statsSvc,
		PeriodSvc:    periodSvc,
		ClosingSvc:   closingSvc,
		BudgetSvc:    budgetSvc,
		RecurringSvc: recurringSvc,
		NotifSvc:     notifSvc,
		VoucherSvc:   voucherSvc,
		LiabilitySvc: liabilitySvc,
		FundSvc:      fundSvc,
		GoldSvc:      goldSvc,
		WealthSvc:    wealthSvc,

		BackupStatusFile: cfg.BackupStatusFile,
	})

	// 周期记账：进程内 cron 每 5 分钟生成到期流水（设计 D7；
	// 前端每次打开应用还会调 POST /recurring/ensure 懒补齐，两者共用幂等生成）
	recurringCron := cron.New(cron.WithLocation(fuyao.Shanghai))
	fundCtx, stopFundSync := context.WithCancel(context.Background())
	defer stopFundSync()
	ensureFunds := func() {
		ctx, cancel := context.WithTimeout(fundCtx, 2*time.Minute)
		defer cancel()
		result := fundSvc.Ensure(ctx)
		if result.Failed > 0 {
			logger.L().Warn("fund quote sync incomplete", zap.Int("failed", result.Failed))
		}
	}
	for _, schedule := range []string{service.FundEveningCron, service.FundLastEveningCron, service.FundMorningCron} {
		if _, err := recurringCron.AddFunc(schedule, ensureFunds); err != nil {
			logger.L().Fatal("fund cron schedule failed", zap.Error(err))
		}
	}
	go ensureFunds()
	ensureGold := func() {
		ctx, cancel := context.WithTimeout(fundCtx, 2*time.Minute)
		defer cancel()
		if err := goldSvc.Ensure(ctx); err != nil {
			logger.L().Warn("gold quote/valuation sync incomplete", zap.Error(err))
		}
	}
	if _, err := recurringCron.AddFunc(service.GoldHourlyCron, ensureGold); err != nil {
		logger.L().Fatal("gold cron schedule failed", zap.Error(err))
	}
	go ensureGold()
	accountSvc.OnGoldCreated = func() { go ensureGold() }
	ensureWealth := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := wealthSvc.Ensure(ctx); err != nil {
			logger.L().Warn("wealth quote/valuation sync incomplete", zap.Error(err))
		}
	}
	if _, err := recurringCron.AddFunc("@every 30s", ensureWealth); err != nil {
		logger.L().Fatal("wealth schedule failed", zap.Error(err))
	}
	go ensureWealth()
	ensureVouchers := func() {
		result, err := voucherSvc.EnsureDue(time.Now())
		if err != nil {
			logger.L().Warn("voucher ensure failed", zap.Error(err))
			return
		}
		if result.Generated > 0 || result.Failed > 0 {
			logger.L().Info("voucher ensure", zap.Int("generated", result.Generated), zap.Int("failed", result.Failed))
		}
	}
	if _, err := recurringCron.AddFunc("@every 1m", ensureVouchers); err != nil {
		logger.L().Fatal("voucher cron schedule failed", zap.Error(err))
	}
	ensureVouchers()
	ensureLiabilities := func() {
		if _, err := liabilitySvc.EnsureDue(time.Now()); err != nil {
			logger.L().Warn("liability ensure failed", zap.Error(err))
		}
	}
	ensureLiabilities()
	if _, err := recurringCron.AddFunc("@every 5m", func() {
		n, err := recurringSvc.EnsureDue(time.Now())
		if err != nil {
			logger.L().Warn("recurring ensure failed", zap.Int("generated", n), zap.Error(err))
			return
		}
		if n > 0 {
			logger.L().Info("recurring generated", zap.Int("count", n))
		}
		ensureVouchers()
		ensureLiabilities()
	}); err != nil {
		logger.L().Fatal("recurring cron schedule failed", zap.Error(err))
	}
	// 每日 08:00 评估还款日提醒与预算超支（设计 D7；重复评估由 dedup_key 去重）
	if _, err := recurringCron.AddFunc("0 8 * * *", func() {
		ensureVouchers()
		ensureLiabilities()
		if err := notifSvc.EvaluateRepayment(time.Now()); err != nil {
			logger.L().Warn("repayment evaluate failed", zap.Error(err))
		}
		if err := budgetSvc.Evaluate(periodSvc.CurrentKey(time.Now())); err != nil {
			logger.L().Warn("budget evaluate failed", zap.Error(err))
		}
	}); err != nil {
		logger.L().Fatal("daily reminder cron schedule failed", zap.Error(err))
	}
	recurringCron.Start()
	defer recurringCron.Stop()

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.L().Info("server starting", zap.String("port", cfg.Port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.L().Fatal("server failed", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	logger.L().Info("server stopped")
}
