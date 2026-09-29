package main

import (
	"context"
	"log"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"shotgun-api/config"
	"shotgun-api/handlers"
	"shotgun-api/middleware"
)

func main() {
	cfg := config.Load()

	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()

	if err := setupRouter(cfg, pool).Run(":" + cfg.Port); err != nil {
		log.Fatalf("server: %v", err)
	}
}

// setupRouter registers middleware and routes. It is separate from main so tests can use it without starting a server.
// pool may be nil in tests that only hit routes without database access
func setupRouter(cfg *config.Config, pool *pgxpool.Pool) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	r.Use(middleware.CORS(cfg.AllowedOrigin))

	r.GET("/ping", handlers.Ping)

	auth := &handlers.Auth{DB: pool, Cfg: cfg}
	r.POST("/auth/register", auth.Register)
	r.POST("/auth/login", auth.Login)
	r.POST("/auth/logout", auth.Logout)
	me := r.Group("/auth/me", auth.RequireAuth)
	me.GET("", auth.Me)
	me.PATCH("", auth.UpdateMe)
	me.DELETE("", auth.DeleteMe)

	return r
}
