package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"mathtrainer/internal/db"
)

func TestMathFoundationsRecordsEvidenceAndReportsReadiness(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := db.Open(t.TempDir() + "/foundations.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	router := gin.New()
	NewMathFoundations(database).Register(router.Group("/api/v1"))

	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	initial := request(http.MethodGet, "/api/v1/math/foundations/overview", "")
	if initial.Code != http.StatusOK || !strings.Contains(initial.Body.String(), `"status":"not_checked"`) {
		t.Fatalf("unexpected overview: %d %s", initial.Code, initial.Body.String())
	}

	for i := 0; i < 5; i++ {
		body := `{"skillKey":"even-odd-pairing","itemKey":"q` + string(rune('a'+i)) + `","setKey":"check-1","mode":"check","correct":true}`
		if saved := request(http.MethodPost, "/api/v1/math/foundations/attempts", body); saved.Code != http.StatusOK {
			t.Fatalf("save attempt: %d %s", saved.Code, saved.Body.String())
		}
	}
	if _, err := database.Exec(`UPDATE math_foundation_attempts SET created_at='2026-09-20 10:00:00' WHERE set_key='check-1'`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		body := `{"skillKey":"even-odd-pairing","itemKey":"r` + string(rune('a'+i)) + `","setKey":"check-2","mode":"check","correct":true}`
		request(http.MethodPost, "/api/v1/math/foundations/attempts", body)
	}
	if _, err := database.Exec(`UPDATE math_foundation_attempts SET created_at='2026-09-22 10:00:00' WHERE set_key='check-2'`); err != nil {
		t.Fatal(err)
	}
	ready := request(http.MethodGet, "/api/v1/math/foundations/overview", "")
	if ready.Code != http.StatusOK || !strings.Contains(ready.Body.String(), `"status":"bridge_ready"`) {
		t.Fatalf("expected bridge ready: %d %s", ready.Code, ready.Body.String())
	}

	check := request(http.MethodPost, "/api/v1/math/foundations/paper-checks", `{"skillKey":"even-odd-pairing","completed":true,"note":"explained the leftover"}`)
	if check.Code != http.StatusOK {
		t.Fatalf("save paper check: %d %s", check.Code, check.Body.String())
	}
}
