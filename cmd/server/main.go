package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"mathtrainer/internal/ai"
	"mathtrainer/internal/config"
	"mathtrainer/internal/db"
	"mathtrainer/internal/handler"
	"mathtrainer/internal/middleware"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config.yaml")
	flag.Parse()

	// Load config
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// Open database
	database, err := db.Open(cfg.DB.Path)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer database.Close()
	log.Printf("database: %s", cfg.DB.Path)

	// Build AI provider chain
	aiChain := ai.BuildChain(cfg.AI)
	log.Printf("AI providers: %v", aiChain.Available())

	// Create Gin router
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.Use(middleware.CORS())

	// API routes
	api := r.Group("/api/v1")
	handler.NewMath(database).Register(api)
	handler.NewEnglish(database, aiChain).Register(api)

	// Health check
	api.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true, "ai": aiChain.Available()})
	})

	// Serve frontend — all non-API routes serve web/index.html
	r.Static("/static", "./web/static")
	r.StaticFile("/favicon.ico", "./web/favicon.ico")
	r.NoRoute(func(c *gin.Context) {
		c.File("./web/index.html")
	})

	addr := cfg.Server.Addr()
	fmt.Printf("\n  Math Trainer running on http://%s\n\n", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("server: %v", err)
	}
}
