package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	appdb "mathtrainer/internal/db"
)

func TestResolveMainLessonUsesParentSelection(t *testing.T) {
	options := []mainLessonTopic{
		{TopicCode: "P4-WHOLE-100000", Status: "Not Started"},
		{TopicCode: "P4-FRACTIONS-MIXED", Status: "Learning"},
	}
	got, reason := resolveMainLesson(options, "manual", "P4-FRACTIONS-MIXED")
	if got.TopicCode != "P4-FRACTIONS-MIXED" {
		t.Fatalf("resolved topic=%q, want parent selection", got.TopicCode)
	}
	if reason != "Pinned by parent for this week." {
		t.Fatalf("reason=%q", reason)
	}
}

func TestResolveMainLessonAutoPrioritisesActiveLearning(t *testing.T) {
	options := []mainLessonTopic{
		{TopicCode: "P4-WHOLE-100000", Status: "Not Started"},
		{TopicCode: "P4-FRACTIONS-MIXED", Status: "Review"},
		{TopicCode: "P4-DECIMALS", Status: "Learning"},
	}
	got, _ := resolveMainLesson(options, "auto", "P4-WHOLE-100000")
	if got.TopicCode != "P4-DECIMALS" {
		t.Fatalf("resolved topic=%q, want active learning topic", got.TopicCode)
	}
}

func TestMainLessonAPIStoresAndReturnsParentChoice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := appdb.Open(filepath.Join(t.TempDir(), "learning-plan.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	router := gin.New()
	api := router.Group("/api/v1")
	NewCurriculum(database, nil).Register(api)

	body := bytes.NewBufferString(`{"mode":"manual","topicCode":"P4-DECIMALS"}`)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/learning-plan/main-lesson", body)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", response.Code, response.Body.String())
	}

	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/learning-plan/main-lesson", nil)
	getResponse := httptest.NewRecorder()
	router.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", getResponse.Code, getResponse.Body.String())
	}
	var got mainLessonResponse
	if err := json.Unmarshal(getResponse.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Mode != "manual" || got.ResolvedTopic.TopicCode != "P4-DECIMALS" || len(got.Options) != 11 {
		t.Fatalf("saved response=%+v", got)
	}
}
