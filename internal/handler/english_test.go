package handler

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"mathtrainer/internal/ai"
	appdb "mathtrainer/internal/db"
	"mathtrainer/internal/model"
)

type englishTestProvider struct{}

func (englishTestProvider) Name() string         { return "english-test" }
func (englishTestProvider) SupportsVision() bool { return false }
func (englishTestProvider) GenerateWithVision(context.Context, ai.VisionRequest) (string, error) {
	return "", nil
}
func (englishTestProvider) GenerateText(context.Context, ai.TextRequest) (string, error) {
	return `{"words":[{"word":"elite","syllables":"e·lite","pos":"noun","phonicsTip":"Remember the long i sound.","definition":"An elite group is a small group with special skill.","example":"The elite team practiced.","examples":["The elite team practiced.","An elite runner trained.","They joined an elite group."]}],"story":"The elite team practiced after school.","blanks":[{"word":"elite","sentence":"The ____ team practiced after school."}]}`, nil
}

func TestSaveWeekKeepsSeparateSetsCreatedInTheSameWeek(t *testing.T) {
	database, err := appdb.Open(filepath.Join(t.TempDir(), "english.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	h := NewEnglish(database, nil)
	created := time.Date(2026, time.September, 7, 10, 0, 0, 0, time.UTC)
	first := &model.WeekData{WeekID: "2026-W37", Label: "Week of Sep 7, 2026", CreatedAt: created, Words: []model.WordEntry{{Word: "first"}}}
	second := &model.WeekData{WeekID: "2026-W37", Label: "Week of Sep 7, 2026", CreatedAt: created.Add(time.Minute), Words: []model.WordEntry{{Word: "second"}}}

	if err := h.saveWeek(first); err != nil {
		t.Fatal(err)
	}
	if err := h.saveWeek(second); err != nil {
		t.Fatal(err)
	}

	rows, err := database.QueryContext(context.Background(), `SELECT week_id, label FROM english_weeks ORDER BY created_at`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids, labels []string
	for rows.Next() {
		var id, label string
		if err := rows.Scan(&id, &label); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		labels = append(labels, label)
	}
	if len(ids) != 2 {
		t.Fatalf("saved sets = %d, want 2; ids=%v", len(ids), ids)
	}
	if ids[0] == ids[1] || labels[0] == labels[1] {
		t.Fatalf("same-week sets must have distinct ids and labels: ids=%v labels=%v", ids, labels)
	}
}

func TestRenameWeekPreservesStableWeekID(t *testing.T) {
	database, err := appdb.Open(filepath.Join(t.TempDir(), "english.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	gin.SetMode(gin.TestMode)
	h := NewEnglish(database, nil)
	week := &model.WeekData{WeekID: "2026-W37", Label: "Week of Sep 7, 2026", CreatedAt: time.Now().UTC(), Words: []model.WordEntry{{Word: "procedure"}}}
	if err := h.saveWeek(week); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	h.Register(router.Group("/api/v1"))

	renamed := readingJSONRequest(t, router, http.MethodPatch, "/api/v1/english/weeks/2026-W37", `{"label":"Academic words · September"}`)
	if renamed.Code != http.StatusOK {
		t.Fatalf("rename status=%d body=%s", renamed.Code, renamed.Body.String())
	}
	loaded := readingJSONRequest(t, router, http.MethodGet, "/api/v1/english/weeks/2026-W37", "")
	if loaded.Code != http.StatusOK || !containsAll(loaded.Body.String(), `"weekId":"2026-W37"`, `"label":"Academic words · September"`, `"procedure"`) {
		t.Fatalf("renamed week was not preserved: status=%d body=%s", loaded.Code, loaded.Body.String())
	}
}

func TestUpdateWeekWordsKeepsIdentityAndDropsRemovedScores(t *testing.T) {
	database, err := appdb.Open(filepath.Join(t.TempDir(), "english.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	gin.SetMode(gin.TestMode)
	h := NewEnglish(database, ai.NewChain([]ai.Provider{englishTestProvider{}}))
	week := &model.WeekData{WeekID: "2026-W37", Label: "September vocabulary", CreatedAt: time.Now().UTC(), Words: []model.WordEntry{{Word: "procedure"}}}
	if err := h.saveWeek(week); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO spell_scores(week_id,word,correct,wrong) VALUES(?,?,?,?)`, week.WeekID, "procedure", 3, 1); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	h.Register(router.Group("/api/v1"))

	updated := readingJSONRequest(t, router, http.MethodPatch, "/api/v1/english/weeks/2026-W37/words", `{"words":["elite"]}`)
	if updated.Code != http.StatusOK || !containsAll(updated.Body.String(), `"weekId":"2026-W37"`, `"label":"September vocabulary"`, `"word":"elite"`) {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}
	var scores int
	if err := database.QueryRow(`SELECT COUNT(*) FROM spell_scores WHERE week_id=?`, week.WeekID).Scan(&scores); err != nil {
		t.Fatal(err)
	}
	if scores != 0 {
		t.Fatalf("removed word scores should be deleted, got %d", scores)
	}
}

func TestParentCanConfirmBritishIPAWithoutChangingTheWord(t *testing.T) {
	database, err := appdb.Open(filepath.Join(t.TempDir(), "english.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	gin.SetMode(gin.TestMode)
	h := NewEnglish(database, nil)
	router := gin.New()
	h.Register(router.Group("/api/v1"))

	saved := readingJSONRequest(t, router, http.MethodPut, "/api/v1/english/pronunciations/procedure", `{"ipa":"pɹəˈsiːdʒə"}`)
	if saved.Code != http.StatusOK || !containsAll(saved.Body.String(), `"word":"procedure"`, `"locale":"en-GB"`, `"verified":true`) {
		t.Fatalf("save pronunciation status=%d body=%s", saved.Code, saved.Body.String())
	}
	lookup := readingJSONRequest(t, router, http.MethodPost, "/api/v1/english/pronunciations/lookup", `{"words":["procedure"]}`)
	if lookup.Code != http.StatusOK || !containsAll(lookup.Body.String(), `"ipa":"pɹəˈsiːdʒə"`, `"source":"parent_verified"`) {
		t.Fatalf("lookup pronunciation status=%d body=%s", lookup.Code, lookup.Body.String())
	}
}

func containsAll(s string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(s, part) {
			return false
		}
	}
	return true
}
