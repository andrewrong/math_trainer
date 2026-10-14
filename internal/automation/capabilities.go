package automation

// Capability is a stable discovery record for an operation exposed to an AI
// control plane. The operation paths are relative to /api/agent/v1.
type Capability struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Operations  []string `json:"operations"`
}

// Capabilities intentionally groups the existing learning-record operations
// instead of inventing a shallow wrapper for every individual route.
func Capabilities() []Capability {
	return []Capability{
		{Name: "math", Description: "Record practice, inspect mistakes, mastered facts, and challenge weak spots.", Operations: []string{"GET /math/sessions", "POST /math/sessions", "DELETE /math/sessions", "GET /math/wrongs", "PUT /math/wrongs", "GET /math/mastered", "PUT /math/mastered/{factKey}", "GET /math/challenge/weak", "PUT /math/challenge/weak", "DELETE /math/challenge/weak/{key}"}},
		{Name: "math_foundations", Description: "Guide and record prerequisite repair for even/odd and fractions before P4 work.", Operations: []string{"GET /math/foundations/overview", "POST /math/foundations/attempts", "POST /math/foundations/paper-checks"}},
		{Name: "english", Description: "Manage weekly vocabulary, checked context stories, spelling evidence, pronunciation, diagnostics, and sentence feedback.", Operations: []string{"GET /english/weeks", "GET /english/weeks/{weekId}", "PATCH /english/weeks/{weekId}", "PATCH /english/weeks/{weekId}/words", "POST /english/weeks/{weekId}/story/regenerate", "POST /english/pronunciations/lookup", "PUT /english/pronunciations/{word}", "POST /english/pronunciations/audio", "POST /english/weeks/generate/text", "POST /english/weeks/generate/image", "PUT /english/weeks/{weekId}/spell", "PUT /english/weeks/{weekId}/friday-spelling", "POST /english/sentence-check", "GET /english/diagnostics", "GET /english/diagnostics/latest", "POST /english/diagnostics"}},
		{Name: "chinese", Description: "Manage Chinese weekly words, checked context-story cloze practice, due review, review rounds, and error recovery.", Operations: []string{"GET /chinese/weeks", "POST /chinese/weeks", "GET /chinese/context-stories/{setKey}", "POST /chinese/context-stories/{setKey}", "GET /chinese/review/due", "PUT /chinese/review", "GET /chinese/weeks/{weekId}/review-rounds", "POST /chinese/weeks/{weekId}/review-rounds/{roundIndex}/start", "PUT /chinese/weeks/{weekId}/review-rounds/{roundIndex}", "GET /chinese/wrongs", "POST /chinese/wrongs/review"}},
		{Name: "curriculum", Description: "Read learning maps and artifacts, save progress, import sources, generate artifacts, and choose the main lesson.", Operations: []string{"GET /curriculum/topics", "GET /curriculum/topics/{code}", "GET /curriculum/topics/{code}/artifacts", "PUT /curriculum/topics/{code}/progress", "POST /curriculum/textbook-map/import", "POST /curriculum/artifacts/generate", "GET /curriculum/path", "GET /learning-plan/main-lesson", "PUT /learning-plan/main-lesson"}},
		{Name: "english_curriculum", Description: "Read English units and resources, save progress, import verified contents, and generate artifacts.", Operations: []string{"GET /english-curriculum/units", "GET /english-curriculum/resources", "GET /english-curriculum/units/{unitId}", "GET /english-curriculum/units/{unitId}/artifacts", "PUT /english-curriculum/units/{unitId}/progress", "POST /english-curriculum/contents/import", "POST /english-curriculum/artifacts/generate"}},
		{Name: "reading", Description: "Track profiles, books, source assets, sessions, evidence-based thinking checks, and weekly reflection.", Operations: []string{"GET /reading/profiles", "PATCH /reading/profiles/{profileId}", "GET /reading/books", "POST /reading/books", "POST /reading/books/import", "GET /reading/books/{bookId}", "POST /reading/books/recognize", "POST /reading/books/{bookId}/assets", "GET /reading/assets/{assetId}/image", "PUT /reading/books/{bookId}/page-map", "POST /reading/library/scan", "GET /reading/sessions", "POST /reading/sessions", "POST /reading/checks", "POST /reading/checks/{checkId}/respond", "PUT /reading/checks/{checkId}/confirm", "GET /reading/weekly-review", "PUT /reading/weekly-review"}},
		{Name: "piano", Description: "Track teacher assignments, source files, focused home practice, and lesson feedback without interpreting notation.", Operations: []string{"GET /piano/weeks", "POST /piano/weeks", "GET /piano/weeks/{weekId}", "POST /piano/weeks/{weekId}/tasks", "PATCH /piano/tasks/{taskId}", "POST /piano/weeks/{weekId}/assets", "GET /piano/assets/{assetId}", "POST /piano/weeks/{weekId}/sessions", "POST /piano/weeks/{weekId}/feedback", "GET /piano/today"}},
		{Name: "speech", Description: "Generate Chinese or English practice audio.", Operations: []string{"POST /tts/chinese", "POST /english/pronunciations/audio"}},
	}
}
