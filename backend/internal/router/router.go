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
			accounts.POST("", middleware.RequireRoles(model.RoleAdmin), accth.Create)
			accounts.PUT("/:id", middleware.RequireRoles(model.RoleAdmin), accth.Update)
			accounts.DELETE("/:id", middleware.RequireRoles(model.RoleAdmin), accth.Delete)
		}

		// 分类：读全部角色，写仅管理员
		cath := handler.NewCategory(d.CategorySvc)
		categories := v1.Group("/categories", middleware.Auth(d.AuthSvc))
		{
			categories.GET("", cath.Tree)
			categories.POST("", middleware.RequireRoles(model.RoleAdmin), cath.Create)
			categories.PUT("/:id", middleware.RequireRoles(model.RoleAdmin), cath.Update)
			categories.DELETE("/:id", middleware.RequireRoles(model.RoleAdmin), cath.Delete)
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

		// 预算：读全部角色，写仅管理员（specs/budgets 8.1）
		bh := handler.NewBudget(d.BudgetSvc)
		budgets := v1.Group("/budgets", middleware.Auth(d.AuthSvc))
		{
			budgets.GET("", bh.Get)
			budgets.PUT("/:period_key", middleware.RequireRoles(model.RoleAdmin), bh.Set)
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
