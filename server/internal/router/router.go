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
	authed.Use(middleware.Auth(cfg.JWT.Secret))
	authed.Use(middleware.OperationLogger(db))
	authed.GET("/auth/me", authHandler.Me)
	authed.GET("/dashboard/overview", accounthandler.NewDashboardHandler(db).Overview)

	domainAccountHandler := domainhandler.NewDomainAccountHandler(db)
	registerResource(authed, "/domain-accounts", domainAccountHandler)
	authed.POST("/domain-accounts/:id/test", domainAccountHandler.Test)
	sslAccountHandler := certhandler.NewSSLAccountHandler(db)
	registerResource(authed, "/ssl-accounts", sslAccountHandler)
	authed.GET("/ssl-accounts/:id/certificates", sslAccountHandler.Certificates)
	authed.POST("/ssl-accounts/:id/certificates/import", sslAccountHandler.ImportCertificates)
	authed.POST("/ssl-accounts/:id/certificates/:certificateId/import", sslAccountHandler.ImportCertificate)
	deployAccountHandler := deployhandler.NewDeployAccountHandler(db)
	registerResource(authed, "/deploy-accounts", deployAccountHandler)
	authed.POST("/deploy-accounts/:id/test", deployAccountHandler.Test)
	authed.GET("/deploy-accounts/:id/sites", deployAccountHandler.Sites)
	domainHandler := domainhandler.NewDomainHandler(db)
	registerResource(authed, "/domains", domainHandler)
	authed.GET("/domain-accounts/:id/provider-domains", domainHandler.ProviderDomains)
	authed.POST("/domains/:id/refresh-expires", domainHandler.RefreshExpires)
	authed.GET("/domains/:id/whois", domainHandler.Whois)
	authed.GET("/domains/:id/records", domainHandler.Records)
	authed.GET("/domains/:id/record-lines", domainHandler.RecordLines)
	authed.POST("/domains/:id/records", domainHandler.CreateRecord)
	authed.PUT("/domains/:id/records/:recordId", domainHandler.UpdateRecord)
	authed.DELETE("/domains/:id/records/:recordId", domainHandler.DeleteRecord)
	certificateHandler := certhandler.NewCertificateHandler(db)
	registerResource(authed, "/certificates", certificateHandler)
	authed.POST("/certificates/:id/submit", certificateHandler.Submit)
	authed.POST("/certificates/:id/revoke", certificateHandler.Revoke)
	authed.POST("/certificates/:id/deploy", certificateHandler.Deploy)
	authed.GET("/certificates/:id/detail", certificateHandler.Detail)
	autoTaskHandler := certhandler.NewAutoTaskHandler(db)
	registerResource(authed, "/tasks", autoTaskHandler)
	authed.POST("/tasks/:id/run", autoTaskHandler.Run)
	registerResource(authed, "/task-logs", handler.NewResourceHandler[model.TaskLog](db))
	registerResource(authed, "/users", accounthandler.NewUserHandler(db))
	registerResource(authed, "/logs", handler.NewResourceHandler[model.OperationLog](db))
	registerResource(authed, "/login-logs", handler.NewResourceHandler[model.LoginLog](db))
	registerResource(authed, "/system-logs", handler.NewResourceHandler[model.SystemLog](db))
	registerResource(authed, "/domain-account-logs", handler.NewResourceHandler[model.DomainAccountLog](db))
	registerResource(authed, "/domain-record-logs", domainhandler.NewDomainRecordLogHandler(db))
	registerResource(authed, "/certificate-logs", certhandler.NewCertificateLogHandler(db))
	registerResource(authed, "/certificate-deploy-logs", handler.NewResourceHandler[model.CertificateDeployLog](db))
	registerResource(authed, "/system-settings", handler.NewResourceHandler[model.SystemSetting](db))
	systemTaskHandler := settingshandler.NewSystemTaskHandler(db)
	settingshandler.StartLogCleanupWorker(db)
	authed.GET("/system-tasks", systemTaskHandler.List)
	authed.PUT("/system-tasks/:key", systemTaskHandler.Update)
	logCleanupHandler := settingshandler.NewLogCleanupHandler(db)
	authed.POST("/log-cleanup", logCleanupHandler.Run)
	proxySettingHandler := settingshandler.NewProxySettingHandler(db)
	registerResource(authed, "/proxy-settings", proxySettingHandler)
	authed.POST("/proxy-settings/:id/test", proxySettingHandler.Test)
	notificationSettingHandler := settingshandler.NewNotificationSettingHandler(db)
	authed.GET("/notification-rules", notificationSettingHandler.Rules)
	authed.PUT("/notification-rules", notificationSettingHandler.SaveRules)
	registerResource(authed, "/notification-settings", notificationSettingHandler)
	authed.POST("/notification-settings/:id/test", notificationSettingHandler.Test)
	registerResource(authed, "/notification-logs", handler.NewResourceHandler[model.NotificationLog](db))

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

func registerResource(group *gin.RouterGroup, path string, h resource) {
	g := group.Group(path)
	g.GET("", h.List)
	g.GET("/:id", h.Get)
	g.POST("", h.Create)
	g.PUT("/:id", h.Update)
	g.DELETE("/:id", h.Delete)
}
