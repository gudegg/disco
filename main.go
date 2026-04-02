package main

import (
	"config-center/handlers"
	"config-center/middleware"
	"fmt"
	"log"
	"time"

	"github.com/gin-gonic/gin"
)

func main() {
	// 加载配置
	cfg, err := LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 初始化数据库
	if err := InitDB(cfg.Database); err != nil {
		log.Fatalf("Failed to init database: %v", err)
	}

	// 设置 JWT 密钥
	middleware.SetJWTSecret(cfg.JWT.Secret)

	// 初始化 SSE 管理器
	sseManager := handlers.NewSSEManagerImpl()
	handlers.SetGlobalSSEManager(sseManager)

	// 启动心跳
	startHeartbeat(sseManager)

	// 创建处理器
	authHandler := handlers.NewAuthHandler(GetDB(), cfg.JWT.Secret, cfg.JWT.Expires)
	serviceHandler := handlers.NewServiceHandler(GetDB())
	configHandler := handlers.NewConfigHandler(GetDB(), sseManager)
	sseHandler := handlers.NewSSEHandler(sseManager)
	tokenHandler := handlers.NewTokenHandler(GetDB())

	// 设置全局 TokenHandler
	handlers.SetGlobalTokenHandler(tokenHandler)

	// 创建 Gin 路由
	r := gin.Default()

	// 静态文件
	r.Static("/static", "./static")
	r.StaticFile("/", "./static/index.html")

	// API 路由
	api := r.Group("/api")
	{
		// 认证（无需鉴权）
		api.POST("/auth/login", authHandler.Login)
		api.POST("/auth/register", authHandler.Register)

		// 需要鉴权的路由
		authorized := api.Group("", middleware.AuthMiddleware())
		{
			// 服务管理
			authorized.GET("/services", serviceHandler.List)
			authorized.POST("/services", serviceHandler.Create)
			authorized.DELETE("/services/:id", serviceHandler.Delete)

			// 配置管理
			authorized.GET("/configs/:service/:env", configHandler.List)
			authorized.POST("/configs", configHandler.Create)
			authorized.PUT("/configs/:id", configHandler.Update)
			authorized.DELETE("/configs/:id", configHandler.Delete)
			authorized.GET("/configs/:service/envs", configHandler.GetEnvs)

			// Token 管理
			authorized.GET("/tokens/:service_id/:env", tokenHandler.GetOrCreateToken)
			authorized.POST("/tokens/:service_id/:env/regenerate", tokenHandler.RegenerateToken)
			authorized.DELETE("/tokens/:service_id/:env", tokenHandler.DeleteToken)

			// 修改密码
			authorized.POST("/auth/change-password", authHandler.ChangePassword)
		}

		// 客户端接口（无需鉴权）
		api.GET("/client/configs/:service/:env", configHandler.GetServiceConfig)
	}

	// SSE 接口
	r.GET("/sse/configs", sseHandler.HandleSSE)

	// 启动服务器
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("Server starting on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// startHeartbeat 启动心跳
func startHeartbeat(mgr *handlers.SSEManagerImpl) {
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		for range ticker.C {
			mgr.BroadcastHeartbeat()
		}
	}()
}
