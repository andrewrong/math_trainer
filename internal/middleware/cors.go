package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CORS allows requests from any origin on the local network.
// Since this app runs on the LAN and has no authentication, a permissive
// CORS policy is intentional.
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Accept")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
