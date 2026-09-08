package handler

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"

	"mathtrainer/internal/model"
)

func TestEnglishCurriculumGrade4UnitsAndArtifacts(t *testing.T) {
	db := newEnglishCurriculumTestDB(t)
	r := gin.New()
	api := r.Group("/api/v1")
	NewEnglishCurriculum(db).Register(api)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/english-curriculum/units?grade=4", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("units status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"Fantastic Animals", "Our Trip to Thailand", "Great Olympians"} {
		if !strings.Contains(body, want) {
			t.Fatalf("units response missing %q: %s", want, body)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/english-curriculum/units/G4-U10/artifacts", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("artifacts status=%d body=%s", w.Code, w.Body.String())
	}
	body = w.Body.String()
	for _, want := range []string{"Wonders of Science", "sourceObjectives", "answer why questions", "sourcePdfRefs", "sourceLessonCards", "Unit 10B", "Text type", "readingModel", "vocabularyWords", "languagePattern", "writingScaffold", "miniPractice", "why ice melts"} {
		if !strings.Contains(body, want) {
			t.Fatalf("artifact response missing %q: %s", want, body)
		}
	}
}

func TestEnglishCurriculumSupportAndPreviewUnits(t *testing.T) {
	db := newEnglishCurriculumTestDB(t)
	r := gin.New()
	api := r.Group("/api/v1")
	NewEnglishCurriculum(db).Register(api)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/english-curriculum/units?grade=3", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("grade 3 units status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"G3-S1", "G3 Support", "Literal Retell and Full Sentences", "Evidence Starter Questions", "Paragraph Basics"} {
		if !strings.Contains(body, want) {
			t.Fatalf("grade 3 support units missing %q: %s", want, body)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/english-curriculum/units/G3-S2/artifacts", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("grade 3 artifact status=%d body=%s", w.Code, w.Body.String())
	}
	body = w.Body.String()
	for _, want := range []string{"Grade 3 support: find the clue", "I think ___ because the text says ___", "answer one why/how question with a text clue", "not verified in local Grade 3 PDF folder"} {
		if !strings.Contains(body, want) {
			t.Fatalf("grade 3 artifact missing %q: %s", want, body)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/english-curriculum/units?grade=5", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("grade 5 units status=%d body=%s", w.Code, w.Body.String())
	}
	body = w.Body.String()
	for _, want := range []string{"G5-P1", "G5 Preview", "Longer Inference with Evidence", "Multi-Paragraph Transition"} {
		if !strings.Contains(body, want) {
			t.Fatalf("grade 5 preview units missing %q: %s", want, body)
		}
	}
}

func TestEnglishCurriculumResourceCoverage(t *testing.T) {
	db := newEnglishCurriculumTestDB(t)
	r := gin.New()
	api := r.Group("/api/v1")
	NewEnglishCurriculum(db).Register(api)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/english-curriculum/resources", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("resources status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"resourceCoverage", "新加坡4A", "missingParts", "Unit 1B", "新加坡4B", "complete", "Grade 5", "not available", "teachingRoadmap", "Current Grade 4 spine", "Grade 3 prerequisite repair", "Grade 5 transition", "manual contents import", "repairBlocks", "Reading evidence", "Language pattern transfer", "Writing structure", "diagnosticBridge", "Reading Evidence", "Vocabulary Use", "Grammar in Context", "G4-U10", "skillSpine", "Reading", "grade4Now", "homeLoop", "sourceAccess", "projectPath", "resources/english"} {
		if !strings.Contains(body, want) {
			t.Fatalf("resource coverage missing %q: %s", want, body)
		}
	}
}

func TestEnglishCurriculumDeepArtifactsForEveryGrade4Unit(t *testing.T) {
	for _, u := range englishCurriculumSeedUnits() {
		pack := englishCurriculumUnitPack(u)
		for _, key := range []string{"readingModel", "vocabularyWords", "languagePattern", "writingScaffold", "miniPractice"} {
			if pack[key] == nil {
				t.Fatalf("%s missing %s", u.UnitID, key)
			}
		}
		weeklyPlan, ok := pack["weeklyPlan"].([]gin.H)
		if !ok || len(weeklyPlan) < 5 {
			t.Fatalf("%s weeklyPlan too shallow: %#v", u.UnitID, pack["weeklyPlan"])
		}
		lessonPractice, ok := pack["lessonPractice"].([]gin.H)
		if !ok || len(lessonPractice) != 4 {
			t.Fatalf("%s lessonPractice must cover A-D: %#v", u.UnitID, pack["lessonPractice"])
		}
		for i, wantPart := range []string{"A", "B", "C", "D"} {
			if lessonPractice[i]["part"] != wantPart {
				t.Fatalf("%s lessonPractice[%d] part=%#v, want %s", u.UnitID, i, lessonPractice[i]["part"], wantPart)
			}
			if tasks, ok := lessonPractice[i]["tasks"].([]gin.H); !ok || len(tasks) < 2 {
				t.Fatalf("%s lessonPractice[%d] needs at least two tasks: %#v", u.UnitID, i, lessonPractice[i])
			}
		}
		checkpointRubric, ok := pack["checkpointRubric"].([]gin.H)
		if !ok || len(checkpointRubric) != 4 {
			t.Fatalf("%s checkpointRubric must cover four skill domains: %#v", u.UnitID, pack["checkpointRubric"])
		}
		for _, row := range checkpointRubric {
			if row["domain"] == "" || row["ready"] == "" || row["needsPractice"] == "" {
				t.Fatalf("%s checkpointRubric row too shallow: %#v", u.UnitID, row)
			}
		}
		reviewPrompts, ok := pack["reviewPrompts"].([]gin.H)
		if !ok || len(reviewPrompts) < 3 {
			t.Fatalf("%s reviewPrompts too shallow: %#v", u.UnitID, pack["reviewPrompts"])
		}
		tabs, ok := pack["tabs"].(gin.H)
		if !ok {
			t.Fatalf("%s missing tabs: %#v", u.UnitID, pack["tabs"])
		}
		for _, key := range []string{"overview", "roadmap", "plan", "reading", "vocabulary", "language", "writing", "practice", "checkpoint", "parent", "source"} {
			if tabs[key] == nil {
				t.Fatalf("%s missing tab %s: %#v", u.UnitID, key, tabs)
			}
		}
		practiceTab, ok := tabs["practice"].(gin.H)
		if !ok {
			t.Fatalf("%s practice tab has type %T", u.UnitID, tabs["practice"])
		}
		if questions, ok := practiceTab["questions"].([]gin.H); !ok || len(questions) < 8 {
			t.Fatalf("%s practice tab questions too shallow: %#v", u.UnitID, practiceTab["questions"])
		}
		roadmap, ok := pack["skillRoadmap"].(gin.H)
		if !ok {
			t.Fatalf("%s missing skillRoadmap: %#v", u.UnitID, pack["skillRoadmap"])
		}
		bridge, ok := roadmap["bridge"].([]gin.H)
		if !ok || len(bridge) < 3 {
			t.Fatalf("%s skillRoadmap bridge too shallow: %#v", u.UnitID, roadmap["bridge"])
		}
		for i, wantStage := range []string{"Before this", "This unit", "Next level"} {
			if bridge[i]["stage"] != wantStage || bridge[i]["studentCan"] == "" || bridge[i]["homeCheck"] == "" {
				t.Fatalf("%s skillRoadmap bridge[%d] invalid: %#v", u.UnitID, i, bridge[i])
			}
		}
		focusCycle, ok := roadmap["focusCycle"].([]gin.H)
		if !ok || len(focusCycle) < 4 {
			t.Fatalf("%s skillRoadmap focusCycle too shallow: %#v", u.UnitID, roadmap["focusCycle"])
		}
		objectives, ok := pack["sourceObjectives"].([]string)
		if !ok || len(objectives) < 4 {
			t.Fatalf("%s sourceObjectives too shallow: %#v", u.UnitID, pack["sourceObjectives"])
		}
		refs, ok := pack["sourcePdfRefs"].([]gin.H)
		if !ok || len(refs) == 0 {
			t.Fatalf("%s sourcePdfRefs missing: %#v", u.UnitID, pack["sourcePdfRefs"])
		}
		cards, ok := pack["sourceLessonCards"].([]gin.H)
		if !ok || len(cards) != 4 {
			t.Fatalf("%s sourceLessonCards must cover A-D: %#v", u.UnitID, pack["sourceLessonCards"])
		}
		for i, wantPart := range []string{"A", "B", "C", "D"} {
			if cards[i]["part"] != wantPart {
				t.Fatalf("%s sourceLessonCards[%d] part=%#v, want %s", u.UnitID, i, cards[i]["part"], wantPart)
			}
			if _, ok := cards[i]["sourceAvailable"].(bool); !ok {
				t.Fatalf("%s sourceLessonCards[%d] missing sourceAvailable: %#v", u.UnitID, i, cards[i])
			}
		}
		vocab, ok := pack["vocabularyWords"].([]gin.H)
		if !ok || len(vocab) < 4 {
			t.Fatalf("%s vocabularyWords too shallow: %#v", u.UnitID, pack["vocabularyWords"])
		}
		practice, ok := pack["miniPractice"].([]gin.H)
		if !ok || len(practice) < 2 {
			t.Fatalf("%s miniPractice too shallow: %#v", u.UnitID, pack["miniPractice"])
		}
		skillPractice, ok := pack["skillPractice"].([]gin.H)
		if !ok || len(skillPractice) < 4 {
			t.Fatalf("%s skillPractice must cover four domains: %#v", u.UnitID, pack["skillPractice"])
		}
		seenDomains := map[string]bool{}
		domainCounts := map[string]int{}
		for _, q := range skillPractice {
			domain, _ := q["domain"].(string)
			seenDomains[domain] = true
			domainCounts[domain]++
			if q["prompt"] == "" || q["explain"] == "" {
				t.Fatalf("%s skillPractice question too shallow: %#v", u.UnitID, q)
			}
		}
		for _, domain := range []string{"Reading", "Vocabulary", "Language", "Writing"} {
			if !seenDomains[domain] {
				t.Fatalf("%s skillPractice missing %s: %#v", u.UnitID, domain, skillPractice)
			}
			if domainCounts[domain] < 2 {
				t.Fatalf("%s skillPractice needs at least two %s questions: %#v", u.UnitID, domain, skillPractice)
			}
		}
	}
}

func TestEnglishCurriculumSourceLessonCardsAreUnitSpecific(t *testing.T) {
	wantKeywords := map[string][]string{
		"G4-U1":  {"animal", "report"},
		"G4-U2":  {"diary", "special"},
		"G4-U3":  {"fossil", "dinosaur"},
		"G4-U4":  {"poem", "narrative"},
		"G4-U5":  {"experiment", "instruction"},
		"G4-U6":  {"postcard", "email"},
		"G4-U7":  {"joke", "funny"},
		"G4-U8":  {"narrative", "novel"},
		"G4-U9":  {"review", "opinion"},
		"G4-U10": {"science", "explanation"},
		"G4-U11": {"newspaper", "olympic"},
	}

	for _, u := range englishCurriculumSeedUnits() {
		if !strings.HasPrefix(u.UnitID, "G4-") {
			continue
		}
		cards := englishSpecificLessonCards(u.UnitID)
		if len(cards) != 4 {
			t.Fatalf("%s must have four source-specific cards, got %#v", u.UnitID, cards)
		}
		combined := strings.ToLower(cardsToText(cards))
		for _, want := range wantKeywords[u.UnitID] {
			if !strings.Contains(combined, want) {
				t.Fatalf("%s source cards should mention %q: %s", u.UnitID, want, combined)
			}
		}
	}
}

func TestEnglishCurriculumSourcePDFAvailabilityMatchesLocalGrade4Set(t *testing.T) {
	unit1 := modelEnglishUnitForTest("G4-U1")
	refs := englishSourcePDFRefs(unit1)
	if len(refs) != 4 {
		t.Fatalf("G4-U1 refs len=%d, want 4", len(refs))
	}
	if refs[1]["part"] != "B" || refs[1]["available"] != false {
		t.Fatalf("G4-U1 part B should be marked missing: %#v", refs[1])
	}
	if !strings.Contains(refs[1]["status"].(string), "not found") {
		t.Fatalf("G4-U1 part B status should explain missing local file: %#v", refs[1])
	}

	unit10 := modelEnglishUnitForTest("G4-U10")
	refs = englishSourcePDFRefs(unit10)
	if refs[1]["part"] != "B" || refs[1]["available"] != true {
		t.Fatalf("G4-U10 part B should be marked available: %#v", refs[1])
	}

	pack := englishCurriculumUnitPack(unit1)
	cards := pack["sourceLessonCards"].([]gin.H)
	if cards[1]["part"] != "B" || cards[1]["sourceAvailable"] != false {
		t.Fatalf("G4-U1 lesson card B should inherit missing source status: %#v", cards[1])
	}
	if !strings.Contains(cards[1]["sourceStatus"].(string), "missing") {
		t.Fatalf("G4-U1 lesson card B should explain missing source: %#v", cards[1])
	}
}

func modelEnglishUnitForTest(id string) model.EnglishCurriculumUnit {
	for _, u := range englishCurriculumSeedUnits() {
		if u.UnitID == id {
			return u
		}
	}
	return model.EnglishCurriculumUnit{UnitID: id}
}

func TestEnglishCurriculumProgressRoundTrip(t *testing.T) {
	db := newEnglishCurriculumTestDB(t)
	r := gin.New()
	api := r.Group("/api/v1")
	NewEnglishCurriculum(db).Register(api)

	body := `{"status":"Review","readingScore":80,"vocabularyScore":70,"languageScore":60,"writingScore":50}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/english-curriculum/units/G4-U10/progress", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("progress update status=%d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/english-curriculum/units?grade=4", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("units status=%d body=%s", w.Code, w.Body.String())
	}
	resp := w.Body.String()
	for _, want := range []string{`"unitId":"G4-U10"`, `"status":"Review"`, `"readingScore":80`, `"vocabularyScore":70`, `"languageScore":60`, `"writingScore":50`} {
		if !strings.Contains(resp, want) {
			t.Fatalf("units response missing progress %q: %s", want, resp)
		}
	}
}

func TestEnglishCurriculumManualContentsImport(t *testing.T) {
	db := newEnglishCurriculumTestDB(t)
	r := gin.New()
	api := r.Group("/api/v1")
	NewEnglishCurriculum(db).Register(api)

	body := `{"grade":"5","book":"5A","lines":"CONTENTS\n1 New Worlds ........ 2\n2 | Persuade Me | 20\nUnit 3 Reading the Sky 38"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/english-curriculum/contents/import", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/english-curriculum/units?grade=5", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("grade 5 units status=%d body=%s", w.Code, w.Body.String())
	}
	resp := w.Body.String()
	for _, want := range []string{`"unitId":"G5-U1"`, `"title":"New Worlds"`, `"unitId":"G5-U2"`, `"title":"Persuade Me"`, `"unitId":"G5-U3"`, `"title":"Reading the Sky"`} {
		if !strings.Contains(resp, want) {
			t.Fatalf("imported units response missing %q: %s", want, resp)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/english-curriculum/units/G5-U1/artifacts", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("imported artifact status=%d body=%s", w.Code, w.Body.String())
	}
	resp = w.Body.String()
	for _, want := range []string{`"artifactVersion":"` + englishCurriculumArtifactVersion + `"`, `"grade":"5"`, `"book":"5A"`, "Use New Worlds to build Grade 5", "read Grade 5 Unit 1", "build unit vocabulary", "complete the unit output", "Preview: New Worlds", "Reading: New Worlds", "Vocabulary and language: New Worlds", "Output: New Worlds", "For New Worlds", "In New Worlds", "Preview New Worlds", "3 predictions about New Worlds", "Use \\\"New Worlds\\\" to predict", "Read a short section from \\\"New Worlds\\\"", "Pick two useful words from \\\"New Worlds\\\"", "Explain the \\\"New Worlds\\\" writing idea", "Write one short response for \\\"New Worlds\\\"", "not verified in local Grade 5 PDF folder"} {
		if !strings.Contains(resp, want) {
			t.Fatalf("imported artifact missing %q: %s", want, resp)
		}
	}
	if strings.Contains(resp, "Grade 4 English unit") || strings.Contains(resp, "same Grade 4 loop") {
		t.Fatalf("imported Grade 5 artifact leaked Grade 4 fallback text: %s", resp)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/english-curriculum/contents/import", strings.NewReader(`{"grade":"5","book":"5A","lines":"bad line"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad import status=%d body=%s", w.Code, w.Body.String())
	}
}

func cardsToText(cards []gin.H) string {
	var b strings.Builder
	for _, card := range cards {
		for _, key := range []string{"title", "objective", "purpose"} {
			if v, ok := card[key].(string); ok {
				b.WriteString(v)
				b.WriteByte(' ')
			}
		}
		if prompts, ok := card["coachPrompts"].([]string); ok {
			b.WriteString(strings.Join(prompts, " "))
			b.WriteByte(' ')
		}
	}
	return b.String()
}

func TestEnglishCurriculumUnitsDoesNotDeadlockWithSingleSQLiteConnection(t *testing.T) {
	db := newEnglishCurriculumTestDB(t)
	db.SetMaxOpenConns(1)
	r := gin.New()
	api := r.Group("/api/v1")
	NewEnglishCurriculum(db).Register(api)

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/english-curriculum/units?grade=4", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		done <- w
	}()

	select {
	case w := <-done:
		if w.Code != http.StatusOK {
			t.Fatalf("units status=%d body=%s", w.Code, w.Body.String())
		}
	case <-time.After(time.Second):
		db.Close()
		t.Fatal("english curriculum units request deadlocked with MaxOpenConns=1")
	}
}

func newEnglishCurriculumTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`
CREATE TABLE english_curriculum_units (
	unit_id TEXT PRIMARY KEY,
	grade TEXT NOT NULL,
	book TEXT NOT NULL,
	unit_no INTEGER NOT NULL,
	title TEXT NOT NULL,
	page_start INTEGER NOT NULL DEFAULT 0,
	sort_order INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE english_curriculum_progress (
	unit_id TEXT PRIMARY KEY,
	status TEXT NOT NULL DEFAULT 'Not Started',
	reading_score INTEGER NOT NULL DEFAULT 0,
	vocabulary_score INTEGER NOT NULL DEFAULT 0,
	language_score INTEGER NOT NULL DEFAULT 0,
	writing_score INTEGER NOT NULL DEFAULT 0,
	updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE english_curriculum_lessons (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	unit_id TEXT NOT NULL,
	lesson_code TEXT NOT NULL,
	title TEXT NOT NULL,
	skill_focus TEXT NOT NULL DEFAULT '',
	source_ref TEXT NOT NULL DEFAULT '',
	sort_order INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE english_curriculum_artifacts (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	unit_id TEXT NOT NULL,
	artifact_type TEXT NOT NULL,
	content_json TEXT NOT NULL DEFAULT '{}',
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}
