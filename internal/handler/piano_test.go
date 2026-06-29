package handler

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

func TestPianoAssignmentsCanBeSavedAndListed(t *testing.T) {
	db := newPianoTestDB(t)
	r := gin.New()
	api := r.Group("/api/v1")
	NewPiano(db).Register(api)

	body := `{"title":"Minuet","section":"p. 12","goal":"right hand","dueDate":"2026-06-30","status":"Hands Separate"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/piano/assignments", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/piano/assignments", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Minuet") {
		t.Fatalf("saved assignment missing from response: %s", w.Body.String())
	}
}

func newPianoTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`
CREATE TABLE piano_assignments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	title TEXT NOT NULL,
	section TEXT,
	goal TEXT,
	due_date TEXT,
	status TEXT NOT NULL DEFAULT 'Not Started',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}
