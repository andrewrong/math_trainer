package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"mathtrainer/internal/ai"
	"mathtrainer/internal/model"
	"mathtrainer/internal/pronunciation"
)

type EnglishHandler struct {
	db             *sql.DB
	ai             *ai.Chain
	pronunciations *pronunciation.Service
}

func NewEnglish(db *sql.DB, chain *ai.Chain) *EnglishHandler {
	return &EnglishHandler{db: db, ai: chain, pronunciations: pronunciation.New(db)}
}

func (h *EnglishHandler) Register(r gin.IRouter) {
	r.GET("/english/weeks", h.listWeeks)
	r.GET("/english/weeks/:weekId", h.getWeek)
	r.PATCH("/english/weeks/:weekId", h.renameWeek)
	r.PATCH("/english/weeks/:weekId/words", h.updateWeekWords)
	r.POST("/english/pronunciations/lookup", h.lookupPronunciations)
	r.PUT("/english/pronunciations/:word", h.savePronunciation)
	r.POST("/english/pronunciations/audio", h.speakEnglish)
	r.POST("/english/weeks/generate/text", h.generateFromText)
	r.POST("/english/weeks/generate/image", h.generateFromImage)
	r.PUT("/english/weeks/:weekId/spell", h.updateSpell)
	r.PUT("/english/weeks/:weekId/friday-spelling", h.updateFridaySpelling)
	r.POST("/english/sentence-check", h.checkSentence)
	r.GET("/english/diagnostics", h.listDiagnostics)
	r.GET("/english/diagnostics/latest", h.latestDiagnostic)
	r.POST("/english/diagnostics", h.saveDiagnostic)
}

func (h *EnglishHandler) lookupPronunciations(c *gin.Context) {
	var req model.LookupEnglishPronunciationsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	records, err := h.pronunciations.LookupOrGenerate(c, req.Words)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"pronunciations": records})
}

func (h *EnglishHandler) savePronunciation(c *gin.Context) {
	var req model.SaveEnglishPronunciationReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	record, err := h.pronunciations.SaveParentVerified(c, c.Param("word"), req.IPA)
	if err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, record)
}

func (h *EnglishHandler) speakEnglish(c *gin.Context) {
	var req model.EnglishSpeechReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	url, cached, err := h.pronunciations.SynthesizeBritishSpeech(c, req.Text)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"url": url, "cached": cached, "locale": "en-GB", "source": "macos_tts"})
}

// ─── List weeks ───────────────────────────────────────────────────────────────

func (h *EnglishHandler) listWeeks(c *gin.Context) {
	rows, err := h.db.QueryContext(c, `
		SELECT w.week_id, w.label, w.created_at, w.words_json
		FROM english_weeks w ORDER BY w.created_at DESC`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}

	// Scan all rows before closing — avoids holding the connection open
	// while loadSpellScores tries to acquire a second connection (deadlock with MaxOpenConns=1).
	var summaries []model.WeekSummary
	for rows.Next() {
		var s model.WeekSummary
		var tsStr, wordsJSON string
		if err := rows.Scan(&s.WeekID, &s.Label, &tsStr, &wordsJSON); err != nil {
			continue
		}
		s.CreatedAt = parseTime(tsStr)
		var words []model.WordEntry
		json.Unmarshal([]byte(wordsJSON), &words)
		s.WordCount = len(words)
		for _, w := range words {
			s.Words = append(s.Words, w.Word)
		}
		summaries = append(summaries, s)
	}
	rows.Close()

	// Now safe to query spell scores — outer rows are closed
	for i := range summaries {
		summaries[i].SpellScores = h.loadSpellScores(c, summaries[i].WeekID)
		summaries[i].FridayMistakes = h.loadFridayMistakes(c, summaries[i].WeekID)
	}
	if summaries == nil {
		summaries = []model.WeekSummary{}
	}
	c.JSON(http.StatusOK, gin.H{"weeks": summaries})
}

// ─── Get single week ──────────────────────────────────────────────────────────

func (h *EnglishHandler) getWeek(c *gin.Context) {
	weekID := c.Param("weekId")
	week, err := h.loadWeek(c, weekID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, model.APIError{Error: "week not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, week)
}

// renameWeek changes the learner-facing title without changing the stable
// week ID, so word cards, spelling progress, and stories retain their links.
func (h *EnglishHandler) renameWeek(c *gin.Context) {
	var req model.RenameEnglishWeekReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	label := strings.TrimSpace(req.Label)
	if label == "" {
		c.JSON(http.StatusBadRequest, model.APIError{Error: "name is required"})
		return
	}
	result, err := h.db.ExecContext(c, `UPDATE english_weeks SET label=? WHERE week_id=?`, label, c.Param("weekId"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		c.JSON(http.StatusNotFound, model.APIError{Error: "week not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"weekId": c.Param("weekId"), "label": label})
}

// updateWeekWords refreshes generated study content but retains the stable
// week ID and label. Spell scores remain only for words that stay in the list.
func (h *EnglishHandler) updateWeekWords(c *gin.Context) {
	var req model.UpdateEnglishWeekWordsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	words := cleanWordList(req.Words)
	if len(words) == 0 || len(words) > 20 {
		c.JSON(http.StatusBadRequest, model.APIError{Error: "enter between 1 and 20 distinct words"})
		return
	}
	weekID := c.Param("weekId")
	if _, err := h.loadWeek(c, weekID); err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, model.APIError{Error: "week not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	content, err := h.generateWeekContent(c.Request.Context(), words)
	if err != nil {
		log.Printf("[english] refresh week %s failed: %v", weekID, err)
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	if err := h.replaceWeekContent(c, weekID, content); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	week, err := h.loadWeek(c, weekID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, week)
}

// ─── Generate from manual word list ──────────────────────────────────────────

func (h *EnglishHandler) generateFromText(c *gin.Context) {
	var req model.GenerateTextReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	week, err := h.generateWeekData(c.Request.Context(), req.Words)
	if err != nil {
		log.Printf("[english] generate from text failed: %v", err)
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, week)
}

// ─── Generate from image upload ───────────────────────────────────────────────

func (h *EnglishHandler) generateFromImage(c *gin.Context) {
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: "failed to parse form"})
		return
	}
	file, header, err := c.Request.FormFile("image")
	if err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: "image field required"})
		return
	}
	defer file.Close()

	imgData, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: "failed to read image"})
		return
	}

	// Detect MIME type from filename or sniff bytes
	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = detectMIME(imgData, header.Filename)
	}

	// Step 1: extract words from image
	words, err := h.extractWordsFromImage(c.Request.Context(), imgData, mimeType)
	if err != nil {
		log.Printf("[english] word extraction failed: filename=%q mime=%q bytes=%d err=%v", header.Filename, mimeType, len(imgData), err)
		c.JSON(http.StatusInternalServerError, model.APIError{Error: "word extraction failed: " + err.Error()})
		return
	}
	log.Printf("[english] extracted %d words from image %q: %v", len(words), header.Filename, words)

	// Step 2: generate full week data
	week, err := h.generateWeekData(c.Request.Context(), words)
	if err != nil {
		log.Printf("[english] week data generation failed: words=%v err=%v", words, err)
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, week)
}

// ─── Update spell score ───────────────────────────────────────────────────────

func (h *EnglishHandler) updateSpell(c *gin.Context) {
	weekID := c.Param("weekId")
	var req model.UpdateSpellReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	_, err := h.db.ExecContext(c, `
		INSERT INTO spell_scores(week_id, word, correct, wrong) VALUES(?,?,?,?)
		ON CONFLICT(week_id, word) DO UPDATE SET
			correct = correct + excluded.correct,
			wrong   = wrong   + excluded.wrong`,
		weekID, req.Word, req.Correct, req.Wrong)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.OKResponse{OK: true})
}

// updateFridaySpelling replaces the parent-confirmed paper-test mistakes for a
// week. A parent can submit an empty list once every word has been reviewed.
func (h *EnglishHandler) updateFridaySpelling(c *gin.Context) {
	weekID := c.Param("weekId")
	var req model.UpdateFridaySpellingReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	week, err := h.loadWeek(c, weekID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, model.APIError{Error: "week not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	allowed := make(map[string]string, len(week.Words))
	for _, item := range week.Words {
		word := strings.TrimSpace(item.Word)
		if word != "" {
			allowed[strings.ToLower(word)] = word
		}
	}
	mistakes := make([]string, 0, len(req.Mistakes))
	seen := map[string]bool{}
	for _, raw := range req.Mistakes {
		key := strings.ToLower(strings.TrimSpace(raw))
		word, ok := allowed[key]
		if !ok {
			c.JSON(http.StatusBadRequest, model.APIError{Error: "each Friday mistake must belong to this vocabulary list"})
			return
		}
		if !seen[key] {
			seen[key] = true
			mistakes = append(mistakes, word)
		}
	}
	tx, err := h.db.BeginTx(c, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(c, `DELETE FROM english_friday_spelling_mistakes WHERE week_id=?`, weekID); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	for _, word := range mistakes {
		if _, err := tx.ExecContext(c, `INSERT INTO english_friday_spelling_mistakes(week_id,word) VALUES(?,?)`, weekID, word); err != nil {
			c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "mistakes": mistakes})
}

// ─── Sentence grammar check ───────────────────────────────────────────────────

func (h *EnglishHandler) checkSentence(c *gin.Context) {
	var req model.SentenceCheckReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	wordJSON, _ := json.Marshal(req.Word)
	sentenceJSON, _ := json.Marshal(req.Sentence)
	system := `You are a primary-school English teacher. Return valid JSON only. Do not use markdown. Do not add text outside JSON.`
	prompt := fmt.Sprintf(`Check a child's sentence.

Target word: %s
Sentence: %s

Decide if the target word is used naturally and if grammar, tense, articles, plurals, word order, capitalization, and punctuation are okay.

Return exactly one compact JSON object:
{"ok":true,"feedback":"One short child-friendly sentence."}
or
{"ok":false,"feedback":"One short child-friendly sentence explaining the biggest problem.","corrected":"One natural corrected sentence."}

Rules:
- feedback must be under 22 words.
- corrected is required when ok is false.
- corrected must include the target word.
- no bullets, no markdown, no explanation outside JSON.`, wordJSON, sentenceJSON)

	result, err := h.ai.GenerateText(c.Request.Context(), ai.TextRequest{
		SystemPrompt:     system,
		UserPrompt:       prompt,
		MaxTokens:        700,
		ResponseMIMEType: "application/json",
	})
	if err != nil {
		log.Printf("[checkSentence] AI error: %v", err)
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	resp, parseErr := parseSentenceCheckResp(result)
	if parseErr != nil {
		log.Printf("[checkSentence] invalid AI response, retrying: %v | raw: %q", parseErr, result)
		retryPrompt := prompt + "\n\nYour previous answer was invalid JSON. Return ONLY the JSON object now."
		result, err = h.ai.GenerateText(c.Request.Context(), ai.TextRequest{
			SystemPrompt:     system,
			UserPrompt:       retryPrompt,
			MaxTokens:        700,
			ResponseMIMEType: "application/json",
		})
		if err != nil {
			log.Printf("[checkSentence] AI retry error: %v", err)
			c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
			return
		}
		resp, parseErr = parseSentenceCheckResp(result)
		if parseErr != nil {
			log.Printf("[checkSentence] invalid AI response after retry: %v | raw: %q", parseErr, result)
			c.JSON(http.StatusOK, model.SentenceCheckResp{
				OK:        false,
				Feedback:  "I could not check this clearly. Please try again.",
				Corrected: req.Sentence,
			})
			return
		}
	}
	c.JSON(http.StatusOK, resp)
}

func parseSentenceCheckResp(raw string) (model.SentenceCheckResp, error) {
	raw = strings.TrimSpace(raw)
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start == -1 || end == -1 || end <= start {
		return model.SentenceCheckResp{}, fmt.Errorf("missing JSON object")
	}
	var resp model.SentenceCheckResp
	if err := json.Unmarshal([]byte(raw[start:end+1]), &resp); err != nil {
		return model.SentenceCheckResp{}, err
	}
	resp.Feedback = strings.TrimSpace(resp.Feedback)
	resp.Corrected = strings.TrimSpace(resp.Corrected)
	if resp.Feedback == "" {
		return model.SentenceCheckResp{}, fmt.Errorf("empty feedback")
	}
	if !resp.OK && resp.Corrected == "" {
		return model.SentenceCheckResp{}, fmt.Errorf("missing corrected sentence")
	}
	return resp, nil
}

// ─── English diagnostic reports ───────────────────────────────────────────────

func (h *EnglishHandler) listDiagnostics(c *gin.Context) {
	rows, err := h.db.QueryContext(c, `
		SELECT id, created_at, grade_level, report_json
		FROM english_diagnostics ORDER BY created_at DESC LIMIT 20`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	defer rows.Close()
	out := []model.EnglishDiagnostic{}
	for rows.Next() {
		var d model.EnglishDiagnostic
		var ts, raw string
		if err := rows.Scan(&d.ID, &ts, &d.GradeLevel, &raw); err != nil {
			continue
		}
		d.CreatedAt = parseTime(ts)
		d.Report = json.RawMessage(raw)
		out = append(out, d)
	}
	c.JSON(http.StatusOK, gin.H{"diagnostics": out})
}

func (h *EnglishHandler) latestDiagnostic(c *gin.Context) {
	var d model.EnglishDiagnostic
	var ts, raw string
	err := h.db.QueryRowContext(c, `
		SELECT id, created_at, grade_level, report_json
		FROM english_diagnostics ORDER BY created_at DESC LIMIT 1`).
		Scan(&d.ID, &ts, &d.GradeLevel, &raw)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusOK, gin.H{"diagnostic": nil})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	d.CreatedAt = parseTime(ts)
	d.Report = json.RawMessage(raw)
	c.JSON(http.StatusOK, gin.H{"diagnostic": d})
}

func (h *EnglishHandler) saveDiagnostic(c *gin.Context) {
	var req model.SaveEnglishDiagnosticReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	if strings.TrimSpace(req.GradeLevel) == "" {
		req.GradeLevel = "Grade 4"
	}

	report := map[string]any{}
	if err := json.Unmarshal(req.Report, &report); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: "report must be a JSON object"})
		return
	}
	report["gradeLevel"] = req.GradeLevel
	report["savedAt"] = time.Now().UTC().Format(time.RFC3339)
	report["ai"] = h.enrichDiagnosticWithAI(c.Request.Context(), report)

	reportJSON, _ := json.Marshal(report)
	res, err := h.db.ExecContext(c, `
		INSERT INTO english_diagnostics(grade_level, report_json)
		VALUES(?,?)`, req.GradeLevel, string(reportJSON))
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(http.StatusOK, gin.H{"id": id, "report": json.RawMessage(reportJSON)})
}

func (h *EnglishHandler) enrichDiagnosticWithAI(ctx context.Context, report map[string]any) map[string]any {
	fallback := fallbackEnglishDiagnosticAdvice(report)
	writing, _ := report["writingSample"].(string)
	if strings.TrimSpace(writing) == "" || h.ai == nil {
		return fallback
	}
	payload, _ := json.Marshal(report)
	system := `You are an experienced Grade 4 international-school English teacher. Return valid JSON only.`
	prompt := fmt.Sprintf(`Review this Grade 4 English diagnostic report and the child's writing sample.

Report JSON:
%s

Return exactly one compact JSON object with this shape:
{
  "writingRubric": {
    "ideas": {"score": 1, "note": "short note"},
    "organization": {"score": 1, "note": "short note"},
    "sentenceStructure": {"score": 1, "note": "short note"},
    "grammar": {"score": 1, "note": "short note"},
    "vocabulary": {"score": 1, "note": "short note"},
    "spelling": {"score": 1, "note": "short note"}
  },
  "summary": "2-3 sentence parent-friendly summary.",
  "mainBlocker": "the most important blocker",
  "fourWeekPlan": [
    {"week": 1, "focus": "Inference + Evidence", "routine": "specific 20-minute routine", "success": "observable success criterion"}
  ],
  "parentCoaching": ["one concrete thing to say or ask", "one concrete thing to avoid"]
}

Rules:
- Scores are 1 to 4.
- Be specific and practical.
- Do not shame the child.
- The plan must choose 1-2 focus areas per week based on the report, not generic advice.
- Return only JSON.`, string(payload))
	result, err := h.ai.GenerateText(ctx, ai.TextRequest{
		SystemPrompt:     system,
		UserPrompt:       prompt,
		MaxTokens:        2500,
		ResponseMIMEType: "application/json",
	})
	if err != nil {
		log.Printf("[english diagnostic] AI enrichment failed: %v", err)
		return fallback
	}
	start := strings.Index(result, "{")
	end := strings.LastIndex(result, "}")
	if start == -1 || end <= start {
		log.Printf("[english diagnostic] AI response missing JSON: %q", result)
		return fallback
	}
	aiReport := map[string]any{}
	if err := json.Unmarshal([]byte(result[start:end+1]), &aiReport); err != nil {
		log.Printf("[english diagnostic] AI response parse failed: %v | raw: %q", err, result)
		return fallback
	}
	return aiReport
}

func fallbackEnglishDiagnosticAdvice(report map[string]any) map[string]any {
	main := "academic reading and structured writing"
	if weak, ok := report["weakSkills"].([]any); ok && len(weak) > 0 {
		if first, ok := weak[0].(map[string]any); ok {
			if name, ok := first["name"].(string); ok && name != "" {
				main = name
			}
		}
	}
	return map[string]any{
		"writingRubric": map[string]any{
			"ideas":             map[string]any{"score": 2, "note": "Use the writing sample to add clearer details."},
			"organization":      map[string]any{"score": 2, "note": "Use a topic sentence, details, and a closing sentence."},
			"sentenceStructure": map[string]any{"score": 2, "note": "Practise full sentences with because, but, and so."},
			"grammar":           map[string]any{"score": 2, "note": "Review one grammar pattern at a time."},
			"vocabulary":        map[string]any{"score": 2, "note": "Use more precise school words and evidence words."},
			"spelling":          map[string]any{"score": 2, "note": "Use weekly phonics and spelling patterns."},
		},
		"summary":     "The diagnostic was saved. Use the section profile to choose one reading focus and one writing focus for the next week.",
		"mainBlocker": main,
		"fourWeekPlan": []map[string]any{
			{"week": 1, "focus": "Inference + Evidence", "routine": "Read a short passage and answer: What happened? Why? How do you know?", "success": "She can point to a sentence as evidence."},
			{"week": 2, "focus": "Sentence Expansion", "routine": "Write 4 sentences and add why, when, where, or one detail.", "success": "Most answers are full sentences."},
			{"week": 3, "focus": "Paragraph Structure", "routine": "Use topic sentence, two details, example, closing sentence.", "success": "The paragraph stays on one main idea."},
			{"week": 4, "focus": "Accuracy Focus", "routine": "Choose one pattern: tense, articles, plurals, prepositions, punctuation, or spelling.", "success": "She can find and fix that pattern in her own writing."},
		},
		"parentCoaching": []string{
			"Ask: Can you say that in a full sentence?",
			"Ask: Which sentence gives you the clue?",
			"Avoid correcting every mistake at once; choose one focus.",
		},
	}
}

// ─── AI generation helpers ────────────────────────────────────────────────────

func (h *EnglishHandler) extractWordsFromImage(ctx context.Context, imgData []byte, mimeType string) ([]string, error) {
	prompt := `This is a child's school vocabulary word list. Extract all the English vocabulary words from this image.
Return ONLY a complete JSON array of strings, all lowercase, nothing else.
The array must start with [ and end with ]. Do not stop halfway.
Example: ["adventure","beautiful","calendar"]`
	result, err := h.ai.GenerateWithVision(ctx, ai.VisionRequest{
		UserPrompt:       prompt,
		ImageData:        imgData,
		MimeType:         mimeType,
		MaxTokens:        1000,
		ResponseMIMEType: "application/json",
	})
	if err != nil {
		return nil, err
	}
	words, parseErr := parseWordListResponse(result)
	if parseErr == nil {
		return words, nil
	}
	log.Printf("[english] invalid word extraction response, retrying: %v | raw: %q", parseErr, result)

	retryResult, err := h.ai.GenerateWithVision(ctx, ai.VisionRequest{
		UserPrompt:       prompt + "\n\nYour previous answer was incomplete. Return the complete JSON array only.",
		ImageData:        imgData,
		MimeType:         mimeType,
		MaxTokens:        1000,
		ResponseMIMEType: "application/json",
	})
	if err != nil {
		return nil, err
	}
	words, parseErr = parseWordListResponse(retryResult)
	if parseErr == nil {
		return words, nil
	}

	// Last-resort salvage for truncated output like:
	// ["comprehension", "extension", "expansion"
	words = extractQuotedWords(result)
	if len(words) > 0 {
		log.Printf("[english] salvaged %d words from truncated response: %v", len(words), words)
		return words, nil
	}
	return nil, parseErr
}

func parseWordListResponse(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no JSON array found in response: %q", raw)
	}
	var words []string
	if err := json.Unmarshal([]byte(raw[start:end+1]), &words); err != nil {
		return nil, fmt.Errorf("parse words: %w", err)
	}
	words = cleanWordList(words)
	if len(words) == 0 {
		return nil, fmt.Errorf("empty word list")
	}
	return words, nil
}

func extractQuotedWords(raw string) []string {
	matches := regexp.MustCompile(`"([A-Za-z][A-Za-z' -]*)"`).FindAllStringSubmatch(raw, -1)
	words := make([]string, 0, len(matches))
	for _, m := range matches {
		words = append(words, m[1])
	}
	return cleanWordList(words)
}

func cleanWordList(words []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.ToLower(strings.TrimSpace(w))
		w = strings.Trim(w, ".,;:!?()[]{}\"'")
		if w == "" || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	return out
}

type generatedEnglishWeekContent struct {
	Words  []model.WordEntry
	Story  string
	Blanks []model.BlankEntry
}

func (h *EnglishHandler) generateWeekContent(ctx context.Context, words []string) (*generatedEnglishWeekContent, error) {
	prompt := fmt.Sprintf(`You are helping a Grade 3-4 student (age 8-10) learn their weekly vocabulary words.

Given these %d words: %s

Return a JSON object with this EXACT structure (no extra text, just valid JSON):
{
  "words": [
    {
      "word": "adventure",
      "syllables": "ad·ven·ture",
      "pos": "noun",
      "phonicsTip": "The ending 'ture' sounds like 'cher' — same as in 'picture' and 'nature'. Remember: t-u-r-e = cher sound.",
      "definition": "An adventure is an exciting experience where you explore somewhere new or face a surprise challenge. It makes you feel brave and curious. Adventures can be big (like a trip) or small (like exploring your backyard).",
      "example": "Climbing the big oak tree in the backyard felt like a real adventure.",
      "examples": [
        "Climbing the big oak tree in the backyard felt like a real adventure.",
        "Max packed his bag and set off on an adventure through the forest.",
        "Reading a great book can feel like going on an adventure without leaving your chair."
      ]
    }
  ],
  "story": "A complete engaging story of about 150 words using ALL the vocabulary words naturally. Use a character named Lily or Max.",
  "blanks": [
    {
      "word": "adventure",
      "sentence": "One sunny morning, Lily went on an ____ in the forest."
    }
  ]
}

Rules:
- phonicsTip: explain the trickiest part to spell using simple phonics rules. Compare to similar common words.
- definition: write 2-3 rich but simple sentences for a child. Sentence 1 = clear meaning. Sentence 2 = what the word feels like / when people use it. Sentence 3 = real-life child-friendly example or contrast if helpful.
- Avoid vague dictionary language like "something" or "in a way" unless necessary. Be concrete.
- example: the first entry of examples (for compatibility).
- examples: exactly 3 short, natural, relatable sentences for a child, each in a DIFFERENT context (home, school, friends, feelings, outdoors, etc.).
- story: use ALL %d words naturally. About 150 words. Make the story understandable without needing the original word list.
- blanks: one entry per word. Show the sentence from the story with that word replaced by ____.
- Each blank sentence should give enough context that a child can reason from meaning, not only from letter shape.
- Return ONLY valid JSON.`,
		len(words), strings.Join(words, ", "), len(words))

	result, err := h.ai.GenerateText(ctx, ai.TextRequest{
		UserPrompt:       prompt,
		MaxTokens:        65536,
		ResponseMIMEType: "application/json",
	})
	if err != nil {
		return nil, err
	}

	// Extract JSON object from response
	result = strings.TrimSpace(result)
	start := strings.Index(result, "{")
	end := strings.LastIndex(result, "}")
	if start == -1 || end == -1 {
		return nil, fmt.Errorf("no JSON object in AI response")
	}
	jsonStr := result[start : end+1]

	var generated struct {
		Words  []model.WordEntry  `json:"words"`
		Story  string             `json:"story"`
		Blanks []model.BlankEntry `json:"blanks"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &generated); err != nil {
		return nil, fmt.Errorf("parse AI response: %w", err)
	}
	return &generatedEnglishWeekContent{Words: generated.Words, Story: generated.Story, Blanks: generated.Blanks}, nil
}

func (h *EnglishHandler) generateWeekData(ctx context.Context, words []string) (*model.WeekData, error) {
	content, err := h.generateWeekContent(ctx, words)
	if err != nil {
		return nil, err
	}
	weekID := weekIDFromNow()
	week := &model.WeekData{
		WeekID:      weekID,
		Label:       weekLabelFromNow(),
		CreatedAt:   time.Now().UTC(),
		Words:       content.Words,
		Story:       content.Story,
		Blanks:      content.Blanks,
		SpellScores: map[string]model.SpellScore{},
	}

	// Persist to DB
	if err := h.saveWeek(week); err != nil {
		return nil, fmt.Errorf("save week: %w", err)
	}
	return week, nil
}

func (h *EnglishHandler) replaceWeekContent(ctx context.Context, weekID string, content *generatedEnglishWeekContent) error {
	wordsJSON, err := json.Marshal(content.Words)
	if err != nil {
		return err
	}
	blanksJSON, err := json.Marshal(content.Blanks)
	if err != nil {
		return err
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE english_weeks SET words_json=?,story=?,blanks_json=? WHERE week_id=?`, string(wordsJSON), content.Story, string(blanksJSON), weekID); err != nil {
		return err
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(content.Words)), ",")
	args := make([]any, 0, len(content.Words)+1)
	args = append(args, weekID)
	for _, word := range content.Words {
		args = append(args, word.Word)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM spell_scores WHERE week_id=? AND word NOT IN (`+placeholders+`)`, args...); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM english_friday_spelling_mistakes WHERE week_id=? AND word NOT IN (`+placeholders+`)`, args...); err != nil {
		return err
	}
	return tx.Commit()
}

// ─── DB helpers ───────────────────────────────────────────────────────────────

func (h *EnglishHandler) saveWeek(w *model.WeekData) error {
	wordsJSON, _ := json.Marshal(w.Words)
	blanksJSON, _ := json.Marshal(w.Blanks)
	baseID, baseLabel := w.WeekID, w.Label
	for setNumber := 1; setNumber <= 1000; setNumber++ {
		weekID, label := baseID, baseLabel
		if setNumber > 1 {
			weekID = fmt.Sprintf("%s-%d", baseID, setNumber)
			label = fmt.Sprintf("%s · Set %d", baseLabel, setNumber)
		}
		res, err := h.db.Exec(`
			INSERT INTO english_weeks(week_id, label, created_at, words_json, story, blanks_json)
			VALUES(?,?,?,?,?,?)
			ON CONFLICT(week_id) DO NOTHING`,
			weekID, label, w.CreatedAt.Format(time.RFC3339),
			string(wordsJSON), w.Story, string(blanksJSON))
		if err != nil {
			return err
		}
		if inserted, _ := res.RowsAffected(); inserted == 1 {
			w.WeekID, w.Label = weekID, label
			return nil
		}
	}
	return fmt.Errorf("could not allocate a distinct English vocabulary set for %s", baseLabel)
}

func (h *EnglishHandler) loadWeek(ctx context.Context, weekID string) (*model.WeekData, error) {
	var w model.WeekData
	var tsStr, wordsJSON, blanksJSON string
	err := h.db.QueryRowContext(ctx, `
		SELECT week_id, label, created_at, words_json, story, blanks_json
		FROM english_weeks WHERE week_id=?`, weekID).
		Scan(&w.WeekID, &w.Label, &tsStr, &wordsJSON, &w.Story, &blanksJSON)
	if err != nil {
		return nil, err
	}
	w.CreatedAt = parseTime(tsStr)
	json.Unmarshal([]byte(wordsJSON), &w.Words)
	json.Unmarshal([]byte(blanksJSON), &w.Blanks)
	w.SpellScores = h.loadSpellScores(ctx, weekID)
	w.FridayMistakes = h.loadFridayMistakes(ctx, weekID)
	return &w, nil
}

func (h *EnglishHandler) loadSpellScores(ctx context.Context, weekID string) map[string]model.SpellScore {
	scores := map[string]model.SpellScore{}
	rows, err := h.db.QueryContext(ctx, `SELECT word, correct, wrong FROM spell_scores WHERE week_id=?`, weekID)
	if err != nil {
		return scores
	}
	defer rows.Close()
	for rows.Next() {
		var word string
		var s model.SpellScore
		if rows.Scan(&word, &s.Correct, &s.Wrong) == nil {
			scores[word] = s
		}
	}
	return scores
}

func (h *EnglishHandler) loadFridayMistakes(ctx context.Context, weekID string) []string {
	words := []string{}
	rows, err := h.db.QueryContext(ctx, `SELECT word FROM english_friday_spelling_mistakes WHERE week_id=? ORDER BY updated_at, word`, weekID)
	if err != nil {
		return words
	}
	defer rows.Close()
	for rows.Next() {
		var word string
		if rows.Scan(&word) == nil {
			words = append(words, word)
		}
	}
	return words
}

// ─── Time helpers ─────────────────────────────────────────────────────────────

func weekIDFromNow() string {
	t := time.Now()
	_, week := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", t.Year(), week)
}

func weekLabelFromNow() string {
	return "Week of " + time.Now().Format("Jan 2, 2006")
}

func parseTime(s string) time.Time {
	formats := []string{time.RFC3339, "2006-01-02T15:04:05Z", "2006-01-02 15:04:05"}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// ─── MIME detection ───────────────────────────────────────────────────────────

func detectMIME(data []byte, filename string) string {
	if len(data) >= 3 {
		switch {
		case data[0] == 0xFF && data[1] == 0xD8:
			return "image/jpeg"
		case data[0] == 0x89 && data[1] == 'P' && data[2] == 'N':
			return "image/png"
		case string(data[:4]) == "RIFF":
			return "image/webp"
		}
	}
	lower := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(lower, ".png"):
		return "image/png"
	case strings.HasSuffix(lower, ".webp"):
		return "image/webp"
	}
	return "image/jpeg"
}
