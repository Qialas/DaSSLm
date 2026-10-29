package router

import (
	"dasslm/server/internal/config"
	"dasslm/server/internal/handler"
	accounthandler "dasslm/server/internal/handler/account"
	certhandler "dasslm/server/internal/handler/certificate"
	deployhandler "dasslm/server/internal/handler/deploy"
	domainhandler "dasslm/server/internal/handler/domain"
	settingshandler "dasslm/server/internal/handler/settings"
	"dasslm/server/internal/middleware"
	"dasslm/server/internal/model"
	"dasslm/server/internal/web"
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func New(cfg *config.Config, db *gorm.DB) *gin.Engine {
	gin.SetMode(cfg.Server.Mode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173", "http://127.0.0.1:5173"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
	}))

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	api := r.Group("/api")
	authHandler := accounthandler.NewAuthHandler(db, cfg)
	api.POST("/auth/login", authHandler.Login)

	authed := api.Group("")
	authed.Use(middleware.Auth(db, cfg.JWT.Secret))
	authed.Use(middleware.NumericParams("id", "recordId"))
	authed.Use(middleware.OperationLogger(db))
	// 操作员只能做日常运维（域名、解析、证书、自动任务）并查看账号与日志；
	// 账号增删改、系统设置、用户、通知、日志写入/清理仅管理员可用。
	admin := authed.Group("")
	admin.Use(middleware.RequireAdmin())
	authed.GET("/auth/me", authHandler.Me)
	authed.PUT("/auth/password", authHandler.ChangePassword)
	authed.GET("/dashboard/overview", accounthandler.NewDashboardHandler(db).Overview)

	domainAccountHandler := domainhandler.NewDomainAccountHandler(db)
	registerResource(authed, admin, "/domain-accounts", domainAccountHandler)
	admin.POST("/domain-accounts/:id/test", domainAccountHandler.Test)
	sslAccountHandler := certhandler.NewSSLAccountHandler(db)
	registerResource(authed, admin, "/ssl-accounts", sslAccountHandler)
	authed.GET("/ssl-accounts/:id/certificates", sslAccountHandler.Certificates)
	authed.POST("/ssl-accounts/:id/certificates/import", sslAccountHandler.ImportCertificates)
	authed.POST("/ssl-accounts/:id/certificates/:certificateId/import", sslAccountHandler.ImportCertificate)
	deployAccountHandler := deployhandler.NewDeployAccountHandler(db)
	registerResource(authed, admin, "/deploy-accounts", deployAccountHandler)
	admin.POST("/deploy-accounts/:id/test", deployAccountHandler.Test)
	authed.GET("/deploy-accounts/:id/sites", deployAccountHandler.Sites)
	domainHandler := domainhandler.NewDomainHandler(db)
	registerResource(authed, authed, "/domains", domainHandler)
	authed.GET("/domain-accounts/:id/provider-domains", domainHandler.ProviderDomains)
	authed.POST("/domains/:id/refresh-expires", domainHandler.RefreshExpires)
	authed.GET("/domains/:id/whois", domainHandler.Whois)
	authed.GET("/domains/:id/records", domainHandler.Records)
	authed.GET("/domains/:id/record-lines", domainHandler.RecordLines)
	authed.POST("/domains/:id/records", domainHandler.CreateRecord)
	authed.PUT("/domains/:id/records/:recordId", domainHandler.UpdateRecord)
	authed.DELETE("/domains/:id/records/:recordId", domainHandler.DeleteRecord)
	certificateHandler := certhandler.NewCertificateHandler(db)
	registerResource(authed, authed, "/certificates", certificateHandler)
	authed.POST("/certificates/:id/submit", certificateHandler.Submit)
	authed.POST("/certificates/:id/revoke", certificateHandler.Revoke)
	authed.POST("/certificates/:id/reset", certificateHandler.Reset)
	authed.POST("/certificates/:id/deploy", certificateHandler.Deploy)
	authed.GET("/certificates/:id/detail", certificateHandler.Detail)
	autoTaskHandler := certhandler.NewAutoTaskHandler(db)
	registerResource(authed, authed, "/tasks", autoTaskHandler)
	authed.POST("/tasks/:id/run", autoTaskHandler.Run)
	registerResource(authed, admin, "/task-logs", handler.NewResourceHandler[model.TaskLog](db))
	registerResource(admin, admin, "/users", accounthandler.NewUserHandler(db))
	registerResource(authed, admin, "/logs", handler.NewResourceHandler[model.OperationLog](db))
	registerResource(authed, admin, "/login-logs", handler.NewResourceHandler[model.LoginLog](db))
	registerResource(authed, admin, "/system-logs", handler.NewResourceHandler[model.SystemLog](db))
	registerResource(authed, admin, "/domain-account-logs", handler.NewResourceHandler[model.DomainAccountLog](db))
	registerResource(authed, admin, "/domain-record-logs", domainhandler.NewDomainRecordLogHandler(db))
	registerResource(authed, admin, "/certificate-logs", certhandler.NewCertificateLogHandler(db))
	registerResource(authed, admin, "/certificate-deploy-logs", handler.NewResourceHandler[model.CertificateDeployLog](db))
	registerResource(admin, admin, "/system-settings", handler.NewResourceHandler[model.SystemSetting](db))
	systemTaskHandler := settingshandler.NewSystemTaskHandler(db)
	settingshandler.StartLogCleanupWorker(db)
	admin.GET("/system-tasks", systemTaskHandler.List)
	admin.PUT("/system-tasks/:key", systemTaskHandler.Update)
	logCleanupHandler := settingshandler.NewLogCleanupHandler(db)
	admin.POST("/log-cleanup", logCleanupHandler.Run)
	proxySettingHandler := settingshandler.NewProxySettingHandler(db)
	registerResource(admin, admin, "/proxy-settings", proxySettingHandler)
	admin.POST("/proxy-settings/:id/test", proxySettingHandler.Test)
	notificationSettingHandler := settingshandler.NewNotificationSettingHandler(db)
	admin.GET("/notification-rules", notificationSettingHandler.Rules)
	admin.PUT("/notification-rules", notificationSettingHandler.SaveRules)
	registerResource(admin, admin, "/notification-settings", notificationSettingHandler)
	admin.POST("/notification-settings/:id/test", notificationSettingHandler.Test)
	registerResource(authed, admin, "/notification-logs", handler.NewResourceHandler[model.NotificationLog](db))

	web.Register(r)

	return r
}

type resource interface {
	List(*gin.Context)
	Get(*gin.Context)
	Create(*gin.Context)
	Update(*gin.Context)
	Delete(*gin.Context)
}

// registerResource 注册增删改查路由：查询挂在 read 分组，新增/修改/删除挂在 write 分组。
func registerResource(read, write *gin.RouterGroup, path string, h resource) {
	r := read.Group(path)
	r.GET("", h.List)
	r.GET("/:id", h.Get)
	w := write.Group(path)
	w.POST("", h.Create)
	w.PUT("/:id", h.Update)
	w.DELETE("/:id", h.Delete)
}
