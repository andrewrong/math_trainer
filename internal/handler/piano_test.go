package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"mathtrainer/internal/config"
	appdb "mathtrainer/internal/db"
)

func newPianoTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := appdb.Open(filepath.Join(t.TempDir(), "piano.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	router := gin.New()
	NewPiano(database, config.ReadingConfig{ContentDir: filepath.Join(t.TempDir(), "sources")}).Register(router.Group("/api/v1"))
	return router
}

func pianoJSON(t *testing.T, router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestPianoWeeklyPlanPrioritisesStuckTask(t *testing.T) {
	router := newPianoTestRouter(t)
	created := pianoJSON(t, router, http.MethodPost, "/api/v1/piano/weeks", `{"label":"Week of 21 Sep","lessonDate":"2026-09-21","teacherNote":"Keep the left hand even."}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create week status=%d body=%s", created.Code, created.Body.String())
	}
	var week struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &week); err != nil {
		t.Fatal(err)
	}
	task := pianoJSON(t, router, http.MethodPost, "/api/v1/piano/weeks/1/tasks", `{"kind":"piece","title":"Minuet","rangeLabel":"p. 8, bars 1-16","goal":"Keep the pulse steady","priority":2,"status":"active"}`)
	if task.Code != http.StatusCreated {
		t.Fatalf("add task status=%d body=%s", task.Code, task.Body.String())
	}
	var savedTask struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(task.Body.Bytes(), &savedTask); err != nil {
		t.Fatal(err)
	}
	saved := pianoJSON(t, router, http.MethodPost, "/api/v1/piano/weeks/1/sessions", `{"taskId":`+jsonNumber(savedTask.ID)+`,"minutes":30,"selfStatus":"stuck","difficultyNote":"The left hand loses the pulse."}`)
	if saved.Code != http.StatusCreated {
		t.Fatalf("save session status=%d body=%s", saved.Code, saved.Body.String())
	}
	today := pianoJSON(t, router, http.MethodGet, "/api/v1/piano/today", "")
	if today.Code != http.StatusOK || !strings.Contains(today.Body.String(), `"title":"Minuet"`) || !strings.Contains(today.Body.String(), `"priority":3`) {
		t.Fatalf("today status=%d body=%s", today.Code, today.Body.String())
	}
}

func TestPianoRejectsInvalidTeacherTask(t *testing.T) {
	router := newPianoTestRouter(t)
	badWeek := pianoJSON(t, router, http.MethodPost, "/api/v1/piano/weeks", `{}`)
	if badWeek.Code != http.StatusBadRequest {
		t.Fatalf("empty week status=%d body=%s", badWeek.Code, badWeek.Body.String())
	}
	pianoJSON(t, router, http.MethodPost, "/api/v1/piano/weeks", `{"label":"Week"}`)
	badTask := pianoJSON(t, router, http.MethodPost, "/api/v1/piano/weeks/1/tasks", `{"kind":"made-up","title":"No"}`)
	if badTask.Code != http.StatusBadRequest {
		t.Fatalf("invalid task status=%d body=%s", badTask.Code, badTask.Body.String())
	}
	reflection := pianoJSON(t, router, http.MethodPost, "/api/v1/piano/weeks/1/sessions", `{"minutes":25,"selfStatus":"practising","difficultyNote":"I need to add the task after class."}`)
	if reflection.Code != http.StatusCreated {
		t.Fatalf("reflection without task status=%d body=%s", reflection.Code, reflection.Body.String())
	}
}
