package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"mathtrainer/internal/ai"
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

	// API routes
	api := r.Group("/api/v1")
	handler.NewMath(database).Register(api)
	handler.NewEnglish(database, aiChain).Register(api)
	handler.NewEnglishCurriculum(database).Register(api)
	handler.NewCurriculum(database, aiChain).Register(api)
	handler.NewChinese(database, aiChain).Register(api)
	handler.NewTTS().Register(api)
	handler.NewPiano(database).Register(api)
	handler.NewReading(database, aiChain, cfg.Reading).Register(api)

	// Health check
	api.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true, "ai": aiChain.Available()})
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
