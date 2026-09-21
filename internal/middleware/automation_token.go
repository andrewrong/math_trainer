package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// AutomationToken protects the agent-facing API with a single bearer token.
// An empty configured token disables the interface rather than accidentally
// exposing a second unauthenticated API surface.
func AutomationToken(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.TrimSpace(token) == "" {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error": "automation API is disabled; configure automation.api_token",
			})
			return
		}

		provided := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
		if !secureTokenEqual(provided, token) {
			c.Header("WWW-Authenticate", `Bearer realm="learning-compass-agent"`)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or missing bearer token"})
			return
		}
		c.Next()
	}
}

func secureTokenEqual(provided, expected string) bool {
	if len(provided) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}
