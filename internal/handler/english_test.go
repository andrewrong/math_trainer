package handler

import (
	"context"
	"encoding/json"
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

type overlappingStoryProvider struct{}

func (overlappingStoryProvider) Name() string         { return "overlapping-story-test" }
func (overlappingStoryProvider) SupportsVision() bool { return false }
func (overlappingStoryProvider) GenerateWithVision(context.Context, ai.VisionRequest) (string, error) {
	return "", nil
}
func (overlappingStoryProvider) GenerateText(context.Context, ai.TextRequest) (string, error) {
	return `{"words":[{"word":"horrible"},{"word":"continue"}],"story":"The explorer wrote about a horrible storm. The crew decided to continue their voyage.","blanks":[{"word":"horrible","sentence":"The explorer wrote about a ____ storm. The crew decided to continue their voyage."},{"word":"continue","sentence":"The explorer wrote about a horrible storm. The crew decided to ____ their voyage."}]}`, nil
}

func TestGenerateWeekContentDerivesDistinctBlanksFromTheStory(t *testing.T) {
	h := NewEnglish(nil, ai.NewChain([]ai.Provider{overlappingStoryProvider{}}))

	content, err := h.generateWeekContent(context.Background(), []string{"horrible", "continue"})
	if err != nil {
		t.Fatal(err)
	}
	if len(content.Blanks) != 2 {
		t.Fatalf("blanks = %#v, want one blank for each word", content.Blanks)
	}
	if content.Blanks[0].Sentence != "The explorer wrote about a ____ storm." {
		t.Fatalf("first blank must be derived from its own source sentence, got %q", content.Blanks[0].Sentence)
	}
	if content.Blanks[1].Sentence != "The crew decided to ____ their voyage." {
		t.Fatalf("second blank must be derived from its own source sentence, got %q", content.Blanks[1].Sentence)
	}
}

func TestBuildStoryBlanksRejectsTwoWeeklyWordsInOneSentence(t *testing.T) {
	_, err := buildStoryBlanks(
		"The explorer wrote about a horrible storm and decided to continue the voyage.",
		[]string{"horrible", "continue"},
	)
	if err == nil || !strings.Contains(err.Error(), "more than one weekly word") {
		t.Fatalf("expected an overlapping-story validation error, got %v", err)
	}
}

func TestRegenerateWeekStoryKeepsExistingWordCards(t *testing.T) {
	database, err := appdb.Open(filepath.Join(t.TempDir(), "english.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	gin.SetMode(gin.TestMode)
	h := NewEnglish(database, ai.NewChain([]ai.Provider{overlappingStoryProvider{}}))
	week := &model.WeekData{
		WeekID: "2026-W39", Label: "Current vocabulary", CreatedAt: time.Now().UTC(),
		Words: []model.WordEntry{{Word: "horrible", Definition: "Very unpleasant."}, {Word: "continue", Definition: "Keep going."}},
		Story: "Old story", Blanks: []model.BlankEntry{{Word: "horrible", Sentence: "Old ____ story."}},
	}
	if err := h.saveWeek(week); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	h.Register(router.Group("/api/v1"))

	refreshed := readingJSONRequest(t, router, http.MethodPost, "/api/v1/english/weeks/2026-W39/story/regenerate", "")
	if refreshed.Code != http.StatusOK || !containsAll(refreshed.Body.String(), `"word":"horrible"`, `"definition":"Very unpleasant."`, `"sentence":"The explorer wrote about a ____ storm."`) {
		t.Fatalf("story refresh must preserve word cards and replace only the checked story: status=%d body=%s", refreshed.Code, refreshed.Body.String())
	}
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

func TestParentFridaySpellingMistakesStaySeparateAndCanBeCleared(t *testing.T) {
	database, err := appdb.Open(filepath.Join(t.TempDir(), "english.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	gin.SetMode(gin.TestMode)
	h := NewEnglish(database, nil)
	week := &model.WeekData{WeekID: "2026-W37", Label: "September vocabulary", CreatedAt: time.Now().UTC(), Words: []model.WordEntry{{Word: "procedure"}, {Word: "calendar"}}}
	if err := h.saveWeek(week); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	h.Register(router.Group("/api/v1"))

	saved := readingJSONRequest(t, router, http.MethodPut, "/api/v1/english/weeks/2026-W37/friday-spelling", `{"mistakes":["procedure"]}`)
	if saved.Code != http.StatusOK || !containsAll(saved.Body.String(), `"mistakes":["procedure"]`) {
		t.Fatalf("save Friday mistakes status=%d body=%s", saved.Code, saved.Body.String())
	}
	loaded := readingJSONRequest(t, router, http.MethodGet, "/api/v1/english/weeks/2026-W37", "")
	if loaded.Code != http.StatusOK || !containsAll(loaded.Body.String(), `"fridayMistakes":["procedure"]`) {
		t.Fatalf("Friday mistakes were not loaded: status=%d body=%s", loaded.Code, loaded.Body.String())
	}
	cleared := readingJSONRequest(t, router, http.MethodPut, "/api/v1/english/weeks/2026-W37/friday-spelling", `{"mistakes":[]}`)
	if cleared.Code != http.StatusOK || !containsAll(cleared.Body.String(), `"mistakes":[]`) {
		t.Fatalf("clear Friday mistakes status=%d body=%s", cleared.Code, cleared.Body.String())
	}
	loaded = readingJSONRequest(t, router, http.MethodGet, "/api/v1/english/weeks/2026-W37", "")
	if loaded.Code != http.StatusOK || !containsAll(loaded.Body.String(), `"fridayMistakes":[]`) {
		t.Fatalf("Friday mistakes were not cleared: status=%d body=%s", loaded.Code, loaded.Body.String())
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

// ─── Sentence check ───────────────────────────────────────────────────────────

type recordingSentenceProvider struct {
	reply  string
	prompt string
}

func (*recordingSentenceProvider) Name() string         { return "sentence-test" }
func (*recordingSentenceProvider) SupportsVision() bool { return false }
func (*recordingSentenceProvider) GenerateWithVision(context.Context, ai.VisionRequest) (string, error) {
	return "", nil
}
func (p *recordingSentenceProvider) GenerateText(_ context.Context, req ai.TextRequest) (string, error) {
	p.prompt = req.UserPrompt
	return p.reply, nil
}

func postSentenceCheck(t *testing.T, reply, sentence string) (*recordingSentenceProvider, model.SentenceCheckResp) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	p := &recordingSentenceProvider{reply: reply}
	h := NewEnglish(nil, ai.NewChain([]ai.Provider{p}))
	router := gin.New()
	h.Register(router.Group("/api/v1"))
	body, err := json.Marshal(map[string]string{"word": "like", "sentence": sentence})
	if err != nil {
		t.Fatal(err)
	}
	rec := readingJSONRequest(t, router, http.MethodPost, "/api/v1/english/sentence-check", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp model.SentenceCheckResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return p, resp
}

func TestNormalizeSentence(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, in, want string }{
		{"curly apostrophe", "I don’t", "I don't"},
		{"curly double quotes", "“hi”", `"hi"`},
		{"left single quote", "‘hi’", "'hi'"},
		{"nbsp and doubled spaces", "  I\u00a0like \u00a0 cats.  ", "I like cats."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeSentence(tc.in); got != tc.want {
				t.Fatalf("normalizeSentence(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCheckSentenceSendsStraightQuotes(t *testing.T) {
	t.Parallel()
	p, _ := postSentenceCheck(t, `{"ok":true,"feedback":"Nice."}`, "I like it, but I don’t.")
	if !strings.Contains(p.prompt, "don't") || strings.Contains(p.prompt, "’") {
		t.Fatalf("prompt must hold the straight quote only: %s", p.prompt)
	}
}

func TestCheckSentenceAcceptsSentenceEqualToCorrection(t *testing.T) {
	t.Parallel()
	t.Run("equal after normalisation", func(t *testing.T) {
		t.Parallel()
		_, resp := postSentenceCheck(t,
			`{"ok":false,"feedback":"Capitalise I.","corrected":"I like it, but I don't."}`,
			"I like it, but I don’t.")
		if !resp.OK || resp.Corrected != "" || resp.Feedback == "" {
			t.Fatalf("resp = %+v, want ok with empty corrected and praise", resp)
		}
	})
	t.Run("differs by letter case", func(t *testing.T) {
		t.Parallel()
		_, resp := postSentenceCheck(t,
			`{"ok":false,"feedback":"Capitalise I.","corrected":"I like cats."}`,
			"i like cats.")
		if resp.OK || resp.Corrected != "I like cats." {
			t.Fatalf("resp = %+v, want not ok with the correction", resp)
		}
	})
}

func TestFixLowerL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
		found    bool
	}{
		{"l sometimes like cats.", "I sometimes like cats.", true},
		{"Today l and my dad l sing.", "Today I and my dad I sing.", true},
		{"l'm happy.", "I'm happy.", true},
		{"I like lemons and will.", "I like lemons and will.", false},
		{"I like cats.", "I like cats.", false},
		{"", "", false},
	}
	for _, tc := range tests {
		got, found := fixLowerL(tc.in)
		if got != tc.want || found != tc.found {
			t.Errorf("fixLowerL(%q) = %q, %v; want %q, %v", tc.in, got, found, tc.want, tc.found)
		}
	}
}

func TestCheckSentenceNamesLowercaseL(t *testing.T) {
	t.Parallel()
	p, resp := postSentenceCheck(t, `{"ok":true,"feedback":"Nice."}`, "l sometimes like to wrestle.")
	if !strings.Contains(p.prompt, "I sometimes like to wrestle.") {
		t.Fatalf("prompt must hold the fixed sentence: %s", p.prompt)
	}
	if resp.OK ||
		resp.Feedback != "The letter you typed is a lowercase l, not a capital I. Please type a capital I." ||
		resp.Corrected != "I sometimes like to wrestle." {
		t.Fatalf("resp = %+v", resp)
	}
}
