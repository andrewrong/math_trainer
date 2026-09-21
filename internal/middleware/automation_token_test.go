package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAutomationToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		token      string
		authority  string
		wantStatus int
	}{
		{name: "disabled", token: "", authority: "Bearer test-token", wantStatus: http.StatusServiceUnavailable},
		{name: "missing", token: "test-token", wantStatus: http.StatusUnauthorized},
		{name: "wrong", token: "test-token", authority: "Bearer wrong-token", wantStatus: http.StatusUnauthorized},
		{name: "accepted", token: "test-token", authority: "Bearer test-token", wantStatus: http.StatusNoContent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(AutomationToken(tt.token))
			r.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.authority != "" {
				req.Header.Set("Authorization", tt.authority)
			}
			res := httptest.NewRecorder()
			r.ServeHTTP(res, req)
			if res.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", res.Code, tt.wantStatus)
			}
		})
	}
}
