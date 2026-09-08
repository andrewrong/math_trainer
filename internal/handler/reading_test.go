package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"mathtrainer/internal/ai"
	"mathtrainer/internal/config"
	appdb "mathtrainer/internal/db"
)

type readingTestProvider struct{}

func (readingTestProvider) Name() string         { return "reading-test" }
func (readingTestProvider) SupportsVision() bool { return true }
func (readingTestProvider) GenerateWithVision(context.Context, ai.VisionRequest) (string, error) {
	return `{"title":"The Wild Robot","author":"Peter Brown","language":"en","genre":"fiction","confidence":0.98}`, nil
}
func (readingTestProvider) GenerateText(_ context.Context, req ai.TextRequest) (string, error) {
	if strings.Contains(req.UserPrompt, "OCR text:") {
		return `{"title":"Matilda","author":"Roald Dahl","isbn":"9780142410370","publisher":"Puffin","language":"en","genre":"fiction"}`, nil
	}
	if strings.Contains(req.UserPrompt, "RESPONSES:") {
		return `{"feedback":[{"skill":"Evidence & Inference","affirmation":"You connected the event to the character's choice.","hint":"Add the detail that shows why the choice mattered.","suggestedState":"developing"}]}`, nil
	}
	return `{"questions":[{"skill":"Evidence & Inference","question":"Why does Roz decide to help, and which detail supports your answer?","difficulty":2,"expectedIdeas":["Roz notices a need"],"hint":"Look at what Roz sees before the decision."}]}`, nil
}

func newReadingTestRouter(t *testing.T, cfg config.ReadingConfig, chain *ai.Chain) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dbPath := filepath.Join(t.TempDir(), "reading.db")
	database, err := appdb.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if cfg.ContentDir == "" {
		cfg.ContentDir = filepath.Join(t.TempDir(), "reading-files")
	}
	router := gin.New()
	api := router.Group("/api/v1")
	NewReading(database, chain, cfg).Register(api)
	return router, dbPath
}

func readingJSONRequest(t *testing.T, router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func TestReadingJournalProfileBookSessionAndWeeklyLoop(t *testing.T) {
	router, _ := newReadingTestRouter(t, config.ReadingConfig{}, nil)

	profiles := readingJSONRequest(t, router, http.MethodGet, "/api/v1/reading/profiles", "")
	if profiles.Code != http.StatusOK || !strings.Contains(profiles.Body.String(), `"profileId":"learner"`) || !strings.Contains(profiles.Body.String(), `"profileId":"me"`) {
		t.Fatalf("profiles status=%d body=%s", profiles.Code, profiles.Body.String())
	}

	book := readingJSONRequest(t, router, http.MethodPost, "/api/v1/reading/books", `{"title":"Charlotte's Web","author":"E. B. White","genre":"fiction","language":"en"}`)
	if book.Code != http.StatusCreated {
		t.Fatalf("create book status=%d body=%s", book.Code, book.Body.String())
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(book.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	session := readingJSONRequest(t, router, http.MethodPost, "/api/v1/reading/sessions", `{"profileId":"learner","bookId":`+jsonNumber(created.ID)+`,"sessionDate":"2026-08-24","chapter":"Chapter 1","pageFrom":1,"pageTo":12,"percent":10,"minutes":20,"reaction":"Interesting"}`)
	if session.Code != http.StatusCreated {
		t.Fatalf("create session status=%d body=%s", session.Code, session.Body.String())
	}

	books := readingJSONRequest(t, router, http.MethodGet, "/api/v1/reading/books?profileId=learner", "")
	if books.Code != http.StatusOK || !strings.Contains(books.Body.String(), `"chapter":"Chapter 1"`) || !strings.Contains(books.Body.String(), `"percent":10`) {
		t.Fatalf("books status=%d body=%s", books.Code, books.Body.String())
	}

	review := readingJSONRequest(t, router, http.MethodPut, "/api/v1/reading/weekly-review", `{"profileId":"learner","weekStart":"2026-08-24","reflection":"She found the main idea.","nextGoal":"Use one detail as evidence.","reviewer":"Parent"}`)
	if review.Code != http.StatusOK {
		t.Fatalf("weekly review status=%d body=%s", review.Code, review.Body.String())
	}
	week := readingJSONRequest(t, router, http.MethodGet, "/api/v1/reading/weekly-review?profileId=learner&weekStart=2026-08-24", "")
	if week.Code != http.StatusOK || !strings.Contains(week.Body.String(), `"daysRead":1`) || !strings.Contains(week.Body.String(), `"nextGoal":"Use one detail as evidence."`) {
		t.Fatalf("week status=%d body=%s", week.Code, week.Body.String())
	}
}

func TestReadingEPUBCreatesGroundedQuestionAndFeedback(t *testing.T) {
	chain := ai.NewChain([]ai.Provider{readingTestProvider{}})
	router, _ := newReadingTestRouter(t, config.ReadingConfig{}, chain)
	book := readingJSONRequest(t, router, http.MethodPost, "/api/v1/reading/books", `{"title":"The Wild Robot","author":"Peter Brown","genre":"fiction","language":"en"}`)
	var created struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(book.Body.Bytes(), &created)

	epub := buildReadingTestEPUB(t)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "wild-robot.epub")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(epub); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reading/books/"+jsonNumber(created.ID)+"/assets?assetType=ebook", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	upload := httptest.NewRecorder()
	router.ServeHTTP(upload, req)
	if upload.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", upload.Code, upload.Body.String())
	}

	check := readingJSONRequest(t, router, http.MethodPost, "/api/v1/reading/checks", `{"profileId":"learner","bookId":`+jsonNumber(created.ID)+`,"tier":"session","chapter":"Arrival"}`)
	if check.Code != http.StatusCreated || !strings.Contains(check.Body.String(), `"grounded":true`) || !strings.Contains(check.Body.String(), `"providerName":"reading-test"`) {
		t.Fatalf("check status=%d body=%s", check.Code, check.Body.String())
	}
	var createdCheck struct {
		Check struct {
			ID int64 `json:"id"`
		} `json:"check"`
	}
	_ = json.Unmarshal(check.Body.Bytes(), &createdCheck)
	feedback := readingJSONRequest(t, router, http.MethodPost, "/api/v1/reading/checks/"+jsonNumber(createdCheck.Check.ID)+"/respond", `{"responses":["Roz helps because she notices the animals need protection."],"responseMode":"typed"}`)
	if feedback.Code != http.StatusOK || !strings.Contains(feedback.Body.String(), `"suggestedState":"developing"`) {
		t.Fatalf("feedback status=%d body=%s", feedback.Code, feedback.Body.String())
	}
}

func TestReadingImportEPUBCreatesBookAndIndex(t *testing.T) {
	chain := ai.NewChain([]ai.Provider{readingTestProvider{}})
	router, _ := newReadingTestRouter(t, config.ReadingConfig{}, chain)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "wild-robot.epub")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(buildReadingTestEPUB(t)); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reading/books/import", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	imported := httptest.NewRecorder()
	router.ServeHTTP(imported, req)
	if imported.Code != http.StatusCreated || !strings.Contains(imported.Body.String(), `"title":"The Wild Robot"`) || !strings.Contains(imported.Body.String(), `"author":"Peter Brown"`) || !strings.Contains(imported.Body.String(), `"status":"ready"`) {
		t.Fatalf("import status=%d body=%s", imported.Code, imported.Body.String())
	}
	books := readingJSONRequest(t, router, http.MethodGet, "/api/v1/reading/books?profileId=learner", "")
	if books.Code != http.StatusOK || !strings.Contains(books.Body.String(), `"title":"The Wild Robot"`) || !strings.Contains(books.Body.String(), `"assetStatus":"ready"`) {
		t.Fatalf("books status=%d body=%s", books.Code, books.Body.String())
	}
	detail := readingJSONRequest(t, router, http.MethodGet, "/api/v1/reading/books/1?profileId=learner", "")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"sections":[`) || !strings.Contains(detail.Body.String(), `"title":"Arrival"`) || strings.Contains(detail.Body.String(), `Arrival, The Wild Robot`) {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}
}

func TestReadingOCRHTTPAdapterNormalisesCandidate(t *testing.T) {
	requests := 0
	ocr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/v1/ocr/image" {
			http.Error(w, "wrong OCR route", http.StatusNotFound)
			return
		}
		if err := r.ParseMultipartForm(2 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if _, _, err := r.FormFile("file"); err != nil {
			http.Error(w, "file was not forwarded to OCR service", http.StatusBadRequest)
			return
		}
		if r.FormValue("engine") != "auto" {
			http.Error(w, "engine was not forwarded", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"markdown":"# MATILDA\nRoald Dahl\nISBN 978-0-14-241037-0","engine":"paddle","pages":1,"duration_ms":1200}`)
	}))
	defer ocr.Close()
	chain := ai.NewChain([]ai.Provider{readingTestProvider{}})
	router, _ := newReadingTestRouter(t, config.ReadingConfig{OCR: config.OCRConfig{BaseURL: ocr.URL, Engine: "auto"}}, chain)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("cover", "cover.jpg")
	part.Write([]byte("fake jpeg bytes"))
	part, _ = writer.CreateFormFile("copyright", "copyright.jpg")
	part.Write([]byte("fake copyright bytes"))
	writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reading/books/recognize", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"title":"Matilda"`) || !strings.Contains(recorder.Body.String(), `"author":"Roald Dahl"`) || requests != 2 {
		t.Fatalf("recognize status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestReadingOCRFallsBackToLegacyRouteOn404(t *testing.T) {
	versioned, legacy := 0, 0
	ocr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/ocr/image":
			versioned++
			http.NotFound(w, r)
		case "/ocr/image":
			legacy++
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"markdown":"# MATILDA\nRoald Dahl","engine":"glm","pages":1,"duration_ms":900}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ocr.Close()
	chain := ai.NewChain([]ai.Provider{readingTestProvider{}})
	router, _ := newReadingTestRouter(t, config.ReadingConfig{OCR: config.OCRConfig{BaseURL: ocr.URL}}, chain)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("cover", "cover.jpg")
	part.Write([]byte("fake jpeg bytes"))
	writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reading/books/recognize", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK || versioned != 1 || legacy != 1 {
		t.Fatalf("recognize status=%d versioned=%d legacy=%d body=%s", recorder.Code, versioned, legacy, recorder.Body.String())
	}
}

func buildReadingTestEPUB(t *testing.T) []byte {
	t.Helper()
	var body bytes.Buffer
	writer := zip.NewWriter(&body)
	files := map[string]string{
		"META-INF/container.xml": `<?xml version="1.0"?><container><rootfiles><rootfile full-path="OPS/content.opf"/></rootfiles></container>`,
		"OPS/content.opf":        `<?xml version="1.0"?><package xmlns:dc="http://purl.org/dc/elements/1.1/"><metadata><dc:title id="main-title">The Wild Robot</dc:title><meta property="title-type" refines="#main-title">main</meta><dc:title id="subtitle">A Story of Survival</dc:title><meta property="title-type" refines="#subtitle">subtitle</meta><dc:creator>Peter Brown</dc:creator><dc:language>en</dc:language></metadata><manifest><item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="ch1"/></spine></package>`,
		"OPS/chapter1.xhtml":     `<html><head><title>Arrival, The Wild Robot</title></head><body><h1>Arrival</h1><p>Roz opened her eyes on a wild island. She watched the animals carefully and decided to learn how they lived. When a storm threatened a nest, Roz chose to help.</p></body></html>`,
	}
	for name, content := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes()
}

func jsonNumber(value int64) string {
	return strings.TrimSpace(string(mustJSON(value)))
}

func mustJSON(value any) []byte {
	data, _ := json.Marshal(value)
	return data
}

func TestParseReadingPDFReportsUnavailableText(t *testing.T) {
	if _, err := os.Stat("/opt/homebrew/bin/pdftotext"); err != nil {
		t.Skip("pdftotext is not installed on this machine")
	}
}
