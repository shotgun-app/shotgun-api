package main

import (
	"log"

	"github.com/gin-gonic/gin"

	"shotgun-api/config"
	"shotgun-api/handlers"
	"shotgun-api/middleware"
)

func main() {
	cfg := config.Load()

	if err := setupRouter(cfg).Run(":" + cfg.Port); err != nil {
		log.Fatalf("server: %v", err)
	}
}

// setupRouter registers middleware and routes. It is separate from main so tests can use it without starting a server
func setupRouter(cfg *config.Config) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	r.Use(middleware.CORS(cfg.AllowedOrigin))

	r.GET("/ping", handlers.Ping)

	return r
}
