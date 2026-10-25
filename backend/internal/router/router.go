package router

import (
	"github.com/gin-gonic/gin"

	"jizhang/internal/backup"
	"jizhang/internal/handler"
	"jizhang/internal/middleware"
	"jizhang/internal/model"
	"jizhang/internal/response"
	"jizhang/internal/service"
)

// Deps 路由依赖注入。
type Deps struct {
	AuthSvc      *service.AuthService
	UserSvc      *service.UserService
	AccountSvc   *service.AccountService
	CategorySvc  *service.CategoryService
	TxnSvc       *service.TransactionService
	StatsSvc     *service.StatsService
	PeriodSvc    *service.PeriodService
	ClosingSvc   *service.ClosingService
	BudgetSvc    *service.BudgetService
	RecurringSvc *service.RecurringService
	NotifSvc     *service.NotificationService
	VoucherSvc   *service.VoucherService
	LiabilitySvc *service.LiabilityService
	FundSvc      *service.FundService
	GoldSvc      *service.GoldService
	WealthSvc    *service.WealthService
	// BackupStatusFile 备份状态文件路径（health 暴露最近备份时间，任务 11.3）。
	BackupStatusFile string
}

// New 构建路由树。
func New(corsOrigins []string, d Deps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(middleware.CORS(corsOrigins))
	r.Use(gin.Logger(), gin.Recovery())

	v1 := r.Group("/api/v1")
	{
		v1.GET("/health", func(c *gin.Context) {
			response.OK(c, gin.H{
				"status": "ok",
				"backup": backup.Read(d.BackupStatusFile),
			})
		})

		ah := handler.NewAuth(d.AuthSvc)
		auth := v1.Group("/auth")
		{
			auth.POST("/login", ah.Login)
			auth.POST("/refresh", ah.Refresh)
			auth.POST("/logout", ah.Logout)
			auth.GET("/me", middleware.Auth(d.AuthSvc), ah.Me)
		}

		uh := handler.NewUser(d.UserSvc)
		users := v1.Group("/users", middleware.Auth(d.AuthSvc), middleware.RequireRoles(model.RoleAdmin))
		{
			users.GET("", uh.List)
			users.POST("", uh.Create)
			users.PATCH("/:id/role", uh.SetRole)
			users.PATCH("/:id/status", uh.SetStatus)
			users.POST("/:id/password", uh.ResetPassword)
		}

		// 账户：读全部角色，写仅管理员（specs/identity/access-control）
		accth := handler.NewAccount(d.AccountSvc)
		accounts := v1.Group("/accounts", middleware.Auth(d.AuthSvc))
		{
			accounts.GET("", accth.List)
			accounts.GET("/liability-types", accth.LiabilityTypes)
			accounts.POST("", accth.Create)
			accounts.PUT("/reorder", accth.Reorder)
			accounts.PUT("/:id", accth.Update)
			accounts.DELETE("/:id", accth.Delete)
		}

		if d.GoldSvc != nil {
			gh := handler.NewGold(d.GoldSvc)
			gold := v1.Group("/gold", middleware.Auth(d.AuthSvc))
			gold.GET("/quote", gh.Quote)
			gold.POST("/quote/refresh", gh.Refresh)
			gold.GET("/accounts", gh.Accounts)
			gold.GET("/accounts/:id", gh.Account)
			gold.POST("/accounts/:id/opening", gh.Write(true, false))
			gold.PUT("/accounts/:id/opening", gh.Write(true, true))
			gold.POST("/accounts/:id/events", gh.Write(false, false))
			gold.GET("/accounts/:id/events", gh.Events)
			gold.POST("/accounts/:id/events/:eventId/undo", gh.Undo)
			gold.GET("/accounts/:id/valuations", gh.Valuations)
			gold.GET("/accounts/:id/audits", gh.Audits)
		}
		if d.FundSvc != nil {
			fh := handler.NewFund(d.FundSvc)
			funds := v1.Group("/funds", middleware.Auth(d.AuthSvc))
			funds.GET("/instruments/search", fh.Search)
			funds.GET("/accounts", fh.Accounts)
			funds.GET("/accounts/:id", fh.Account)
			funds.POST("/accounts/:id/positions", fh.Opening)
			funds.POST("/accounts/:id/events", fh.FirstBuy)
			funds.POST("/accounts/:id/refresh", fh.Refresh)
			funds.GET("/positions/:id", fh.Position)
			funds.PUT("/positions/:id", fh.Amend)
			funds.GET("/positions/:id/events", fh.Events)
			funds.POST("/positions/:id/events", fh.Operate)
			funds.POST("/positions/:id/events/:eventId/undo", fh.Undo)
			funds.GET("/positions/:id/valuations", fh.Valuations)
			funds.GET("/positions/:id/dividends", fh.Dividends)
			funds.GET("/positions/:id/audits", fh.Audits)
		}
		if d.WealthSvc != nil {
			fh := handler.NewWealth(d.WealthSvc)
			wealth := v1.Group("/wealth", middleware.Auth(d.AuthSvc))
			wealth.GET("/products", fh.Catalog)
			wealth.GET("/cash-accounts", fh.CashAccounts)
			wealth.GET("/cash-accounts/:id", fh.CashAccount)
			wealth.PUT("/cash-accounts/:id", fh.ConfigureCash)
			wealth.POST("/cash-accounts/:id/refresh", fh.RefreshCash)
			wealth.GET("/cash-yields/:id", fh.CashYield)
			wealth.PUT("/cash-yields/:id", fh.EditCashYield)
			wealth.GET("/products/:id/navs", fh.Quotes)
			wealth.GET("/products/:id/manual-navs", fh.ManualNAVs)
			wealth.POST("/products/:id/manual-navs", fh.WriteManual)
			wealth.GET("/products/:id/manual-audits", fh.ManualAudits)
			wealth.POST("/products/:id/refresh", fh.RefreshProduct)
			wealth.GET("/accounts", fh.Accounts)
			wealth.GET("/accounts/:id", fh.Account)
			wealth.POST("/accounts/:id/positions", fh.Opening)
			wealth.POST("/accounts/:id/events", fh.FirstBuy)
			wealth.POST("/accounts/:id/orders", fh.Subscribe)
			wealth.POST("/accounts/:id/refresh", fh.Refresh)
			wealth.GET("/positions/:id", fh.Position)
			wealth.PUT("/positions/:id", fh.Amend)
			wealth.GET("/positions/:id/events", fh.Events)
			wealth.POST("/positions/:id/events", fh.Operate)
			wealth.POST("/positions/:id/order-preview", fh.OrderPreview)
			wealth.POST("/positions/:id/events/:eventId/undo", fh.Undo)
			wealth.GET("/positions/:id/valuations", fh.Valuations)
			wealth.GET("/positions/:id/audits", fh.Audits)
		}
		// 分类：读全部角色，写仅管理员
		if d.LiabilitySvc != nil {
			lh := handler.NewLiability(d.LiabilitySvc)
			accounts.GET("/:id/liability", lh.Detail)
			accounts.GET("/:id/statements", lh.Bills)
			accounts.PUT("/:id/liability-settings", lh.Settings)
			v1.GET("/statements/:id", middleware.Auth(d.AuthSvc), lh.Bill)
			v1.POST("/statements/:id/reconcile", middleware.Auth(d.AuthSvc), lh.Reconcile)
			liabilities := v1.Group("/liabilities", middleware.Auth(d.AuthSvc))
			liabilities.GET("", lh.List)
			liabilities.POST("/ensure", lh.Ensure)
			liabilities.POST("/repayments", lh.Repay)
			liabilities.POST("/repayments/preview", lh.OrdinaryRepaymentPreview)
			liabilities.PUT("/repayments/:id/allocations", lh.Allocate)
			liabilities.POST("/installments", lh.CreatePlan)
			liabilities.GET("/installments/:id", lh.Plan)
			liabilities.PATCH("/installments/:id", lh.UpdatePlan)
			liabilities.POST("/installments/:id/refund-adjustments", lh.RefundPlan)
			liabilities.POST("/installments/:id/settlement-preview", lh.Preview)
			liabilities.POST("/installments/:id/settle", lh.Settle)
			liabilities.POST("/installments/:id/repayment-preview", lh.RepaymentPreview)
			liabilities.POST("/installments/:id/repay", lh.RepayInstallment)
			liabilities.POST("/operations/:id/reverse", lh.Reverse)
		}
		cath := handler.NewCategory(d.CategorySvc)
		categories := v1.Group("/categories", middleware.Auth(d.AuthSvc))
		{
			categories.GET("", cath.Tree)
			categories.POST("", cath.Create)
			categories.PUT("/reorder", cath.Reorder)
			categories.PUT("/:id", cath.Update)
			categories.DELETE("/:id", cath.Delete)
		}

		// 流水：读全部角色；建改删按授权矩阵在 service 内细判
		txnh := handler.NewTxn(d.TxnSvc)
		txns := v1.Group("/transactions", middleware.Auth(d.AuthSvc))
		{
			txns.GET("", txnh.List)
			txns.GET("/export", txnh.Export)
			txns.POST("", txnh.Create)
			txns.PUT("/:id", txnh.Update)
			txns.DELETE("/:id", txnh.Delete)
		}

		// 统计：区间查询全部角色可读；30 秒缓存，写操作主动失效
		vh := handler.NewVoucher(d.VoucherSvc)
		// 个人名称目录不是账务写入，所有已登录角色均可管理本人条目。
		platforms := v1.Group("/voucher-platforms", middleware.Auth(d.AuthSvc))
		platforms.GET("", vh.ListPlatforms)
		platforms.POST("", vh.CreatePlatform)
		platforms.PUT("/reorder", vh.ReorderPlatforms)
		platforms.PUT("/:id", vh.RenamePlatform)
		platforms.DELETE("/:id", vh.DeletePlatform)
		vouchers := v1.Group("/vouchers", middleware.Auth(d.AuthSvc))
		{
			vouchers.GET("", vh.List)
			vouchers.GET("/:id", vh.Detail)
			writers := vouchers.Group("", middleware.RequireRoles(model.RoleAdmin, model.RoleRecorder, model.RoleMember))
			writers.POST("", vh.Create)
			writers.POST("/ensure", vh.Ensure)
			writers.PUT("/:id", vh.Update)
			writers.DELETE("/:id", vh.Delete)
			writers.POST("/:id/redeem", vh.Operate(model.VoucherRedeem))
			writers.POST("/:id/refund", vh.Operate(model.VoucherRefund))
			writers.POST("/:id/writeoff", vh.Operate(model.VoucherWriteoff))
			writers.POST("/:id/events/:eventId/undo", vh.Undo)
		}

		stath := handler.NewStats(d.StatsSvc)
		stats := v1.Group("/stats", middleware.Auth(d.AuthSvc))
		{
			stats.GET("/category", stath.CategoryShare)
			stats.GET("/trend", stath.Trend)
			stats.GET("/compare", stath.Compare)
			stats.GET("/periods", stath.PeriodSummary)
			stats.GET("/assets", stath.Assets)
		}

		// 账期设置：读全部角色，写仅管理员（specs/billing-periods 7.1）
		seth := handler.NewSetting(d.PeriodSvc)
		settings := v1.Group("/settings", middleware.Auth(d.AuthSvc))
		{
			settings.GET("", seth.Get)
			settings.PUT("", middleware.RequireRoles(model.RoleAdmin), seth.Set)
		}

		// 封账：预览/列表全部角色可读，封/解封仅管理员（specs/billing-periods 7.2）
		clh := handler.NewClosing(d.ClosingSvc)
		closings := v1.Group("/closings", middleware.Auth(d.AuthSvc))
		{
			closings.GET("", clh.List)
			closings.GET("/preview", clh.Preview)
			closings.POST("", middleware.RequireRoles(model.RoleAdmin), clh.Close)
			closings.DELETE("/:period_key", middleware.RequireRoles(model.RoleAdmin), clh.Reopen)
		}

		// 审计日志：仅管理员查看
		audits := v1.Group("/audit-logs", middleware.Auth(d.AuthSvc), middleware.RequireRoles(model.RoleAdmin))
		{
			audits.GET("", clh.AuditList)
		}

		// 预算：全部登录角色管理本人预算，服务层按认证账号隔离。
		bh := handler.NewBudget(d.BudgetSvc)
		budgets := v1.Group("/budgets", middleware.Auth(d.AuthSvc))
		{
			budgets.GET("", bh.Get)
			budgets.PUT("/:period_key", bh.Set)
		}

		// 周期记账：列表读全部登录角色（作用域按创建者归属，管理员全量）；
		// 写仅三档可记账角色，归属细校验在 service —— specs/recurring-entries
		//「按创建者归属隔离」；ensure 懒补齐任意登录角色可触发（生成本身幂等）
		rh := handler.NewRecurring(d.RecurringSvc)
		recurring := v1.Group("/recurring", middleware.Auth(d.AuthSvc))
		{
			recurring.GET("", rh.List)
			recurring.POST("",
				middleware.RequireRoles(model.RoleAdmin, model.RoleRecorder, model.RoleMember), rh.Create)
			recurring.PUT("/:id",
				middleware.RequireRoles(model.RoleAdmin, model.RoleRecorder, model.RoleMember), rh.Update)
			recurring.PATCH("/:id/status",
				middleware.RequireRoles(model.RoleAdmin, model.RoleRecorder, model.RoleMember), rh.SetStatus)
			recurring.DELETE("/:id",
				middleware.RequireRoles(model.RoleAdmin, model.RoleRecorder, model.RoleMember), rh.Delete)
			recurring.POST("/ensure", rh.Ensure)
		}

		// 提醒：任意登录角色可读可标记已读（全局共享一条账本；specs/notifications 10.2）
		nh := handler.NewNotification(d.NotifSvc)
		notifs := v1.Group("/notifications", middleware.Auth(d.AuthSvc))
		{
			notifs.GET("", nh.List)
			notifs.PATCH("/:id", nh.MarkRead)
			notifs.POST("/read-all", nh.MarkAllRead)
		}
	}
	return r
}
