package main

import (
	"database/sql"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"mathtrainer/internal/ai"
	"mathtrainer/internal/automation"
	"mathtrainer/internal/config"
	"mathtrainer/internal/db"
	"mathtrainer/internal/handler"
	"mathtrainer/internal/middleware"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config.yaml")
	logPath := flag.String("log", "server.log", "path to log file")
	flag.Parse()

	// Set up logging to file + stdout
	logFile, err := os.OpenFile(*logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("open log file: %v", err)
	}
	defer logFile.Close()
	log.SetOutput(io.MultiWriter(os.Stdout, logFile))
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Printf("logging to %s", *logPath)

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
	r.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
		c.Next()
	})

	// Local browser API. It intentionally remains unauthenticated for the
	// family's existing LAN-based application.
	api := r.Group("/api/v1")
	registerLearningRoutes(api, database, aiChain, cfg.Reading)
	api.GET("/health", healthHandler(aiChain, false))

	// Agent API is an authenticated mirror of the learning record interface.
	// Keeping its own prefix lets a control plane evolve independently from the
	// browser without duplicating domain logic in new handlers.
	agentAPI := r.Group("/api/agent/v1", middleware.AutomationToken(cfg.Automation.APIToken))
	registerLearningRoutes(agentAPI, database, aiChain, cfg.Reading)
	agentAPI.GET("/health", healthHandler(aiChain, true))
	agentAPI.GET("/capabilities", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"version":        "2026-09-29",
			"authentication": "Bearer token",
			"capabilities":   automation.Capabilities(),
		})
	})

	// Serve frontend — all non-API routes serve web/index.html
	r.Static("/audio", "./web/audio")
	r.Static("/static", "./web/static")
	r.StaticFile("/favicon.ico", "./web/favicon.ico")
	r.GET("/refresh", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<meta http-equiv="Cache-Control" content="no-store, no-cache, must-revalidate, max-age=0">
<meta http-equiv="Pragma" content="no-cache">
<meta http-equiv="Expires" content="0">
<title>Refreshing...</title>
</head>
<body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;padding:32px;">
<h2>Refreshing latest app...</h2>
<p>This keeps saved learning data and reloads the newest page.</p>
<script>
(async function(){
  try {
    if ('serviceWorker' in navigator) {
      const regs = await navigator.serviceWorker.getRegistrations();
      await Promise.all(regs.map(r => r.unregister()));
    }
    if ('caches' in window) {
      const keys = await caches.keys();
      await Promise.all(keys.map(k => caches.delete(k)));
    }
  } catch (e) {
    console.warn('refresh cleanup skipped:', e);
  }
  location.replace('/?v=' + Date.now());
})();
</script>
</body>
</html>`))
	})
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "api route not found"})
			return
		}
		if strings.HasPrefix(c.Request.URL.Path, "/audio/") {
			c.Status(http.StatusNotFound)
			return
		}
		html, err := os.ReadFile("./web/index.html")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read index.html"})
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", html)
	})

	addr := cfg.Server.Addr()
	fmt.Printf("\n  Math Trainer running on http://%s\n\n", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func registerLearningRoutes(r gin.IRouter, database *sql.DB, aiChain *ai.Chain, reading config.ReadingConfig) {
	handler.NewMath(database).Register(r)
	handler.NewMathFoundations(database).Register(r)
	handler.NewEnglish(database, aiChain).Register(r)
	handler.NewEnglishCurriculum(database).Register(r)
	handler.NewCurriculum(database, aiChain).Register(r)
	handler.NewChinese(database, aiChain).Register(r)
	handler.NewTTS().Register(r)
	handler.NewPiano(database, reading).Register(r)
	handler.NewReading(database, aiChain, reading).Register(r)
}

func healthHandler(aiChain *ai.Chain, agent bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true, "ai": aiChain.Available(), "agentAPI": agent})
	}
}
