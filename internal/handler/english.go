package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"mathtrainer/internal/ai"
	"mathtrainer/internal/model"
)

type EnglishHandler struct {
	db *sql.DB
	ai *ai.Chain
}

func NewEnglish(db *sql.DB, chain *ai.Chain) *EnglishHandler {
	return &EnglishHandler{db: db, ai: chain}
}

func (h *EnglishHandler) Register(r gin.IRouter) {
	r.GET("/english/weeks", h.listWeeks)
	r.GET("/english/weeks/:weekId", h.getWeek)
	r.POST("/english/weeks/generate/text", h.generateFromText)
	r.POST("/english/weeks/generate/image", h.generateFromImage)
	r.PUT("/english/weeks/:weekId/spell", h.updateSpell)
	r.POST("/english/sentence-check", h.checkSentence)
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

// ─── Generate from manual word list ──────────────────────────────────────────

func (h *EnglishHandler) generateFromText(c *gin.Context) {
	var req model.GenerateTextReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	week, err := h.generateWeekData(c.Request.Context(), req.Words)
	if err != nil {
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
		c.JSON(http.StatusInternalServerError, model.APIError{Error: "word extraction failed: " + err.Error()})
		return
	}

	// Step 2: generate full week data
	week, err := h.generateWeekData(c.Request.Context(), words)
	if err != nil {
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

// ─── Sentence grammar check ───────────────────────────────────────────────────

func (h *EnglishHandler) checkSentence(c *gin.Context) {
	var req model.SentenceCheckReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	prompt := fmt.Sprintf(`You are a friendly English teacher helping a child (age 8-10).

The child was asked to write a sentence using the word "%s".
Their sentence: "%s"

Check:
1. Is "%s" used correctly in context?
2. Are there any grammar mistakes?

Reply with ONLY a JSON object (no extra text):
{"ok": true, "feedback": "Great sentence! You used 'convenience' perfectly."}
OR if there are issues:
{"ok": false, "feedback": "Almost! 'went' should be 'goes' here.", "corrected": "The store is open for convenience."}

Rules:
- feedback: 1 short sentence, encouraging tone, mention what's good or what to fix
- corrected: only include if there is a grammar error; give a natural corrected version
- If the word is missing, ok=false and say so kindly`, req.Word, req.Sentence, req.Word)

	result, err := h.ai.GenerateText(c.Request.Context(), ai.TextRequest{
		UserPrompt: prompt,
		MaxTokens:  200,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	result = strings.TrimSpace(result)
	start := strings.Index(result, "{")
	end := strings.LastIndex(result, "}")
	if start == -1 || end == -1 {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: "unexpected AI response"})
		return
	}
	var resp model.SentenceCheckResp
	if err := json.Unmarshal([]byte(result[start:end+1]), &resp); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: "parse error"})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// ─── AI generation helpers ────────────────────────────────────────────────────

func (h *EnglishHandler) extractWordsFromImage(ctx context.Context, imgData []byte, mimeType string) ([]string, error) {
	result, err := h.ai.GenerateWithVision(ctx, ai.VisionRequest{
		UserPrompt: `This is a child's school vocabulary word list. Extract all the English vocabulary words from this image.
Return ONLY a JSON array of the words in lowercase, nothing else.
Example: ["adventure","beautiful","calendar"]`,
		ImageData: imgData,
		MimeType:  mimeType,
		MaxTokens: 300,
	})
	if err != nil {
		return nil, err
	}
	result = strings.TrimSpace(result)
	// Find JSON array in response
	start := strings.Index(result, "[")
	end := strings.LastIndex(result, "]")
	if start == -1 || end == -1 {
		return nil, fmt.Errorf("no JSON array found in response: %q", result)
	}
	var words []string
	if err := json.Unmarshal([]byte(result[start:end+1]), &words); err != nil {
		return nil, fmt.Errorf("parse words: %w", err)
	}
	return words, nil
}

func (h *EnglishHandler) generateWeekData(ctx context.Context, words []string) (*model.WeekData, error) {
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
      "definition": "An exciting trip or experience where something surprising might happen.",
      "example": "Climbing the big oak tree in the backyard felt like a real adventure."
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
- definition: simple, 1-2 sentences, grade 3 vocabulary.
- example: fun and relatable for a child.
- story: use ALL %d words naturally. About 150 words.
- blanks: one entry per word. Show the sentence from the story with that word replaced by ____.
- Return ONLY valid JSON.`,
		len(words), strings.Join(words, ", "), len(words))

	result, err := h.ai.GenerateText(ctx, ai.TextRequest{
		UserPrompt: prompt,
		MaxTokens:  4096,
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

	weekID := weekIDFromNow()
	week := &model.WeekData{
		WeekID:      weekID,
		Label:       weekLabelFromNow(),
		CreatedAt:   time.Now().UTC(),
		Words:       generated.Words,
		Story:       generated.Story,
		Blanks:      generated.Blanks,
		SpellScores: map[string]model.SpellScore{},
	}

	// Persist to DB
	if err := h.saveWeek(week); err != nil {
		return nil, fmt.Errorf("save week: %w", err)
	}
	return week, nil
}

// ─── DB helpers ───────────────────────────────────────────────────────────────

func (h *EnglishHandler) saveWeek(w *model.WeekData) error {
	wordsJSON, _ := json.Marshal(w.Words)
	blanksJSON, _ := json.Marshal(w.Blanks)
	_, err := h.db.Exec(`
		INSERT INTO english_weeks(week_id, label, created_at, words_json, story, blanks_json)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(week_id) DO UPDATE SET
			label=excluded.label,
			words_json=excluded.words_json,
			story=excluded.story,
			blanks_json=excluded.blanks_json`,
		w.WeekID, w.Label, w.CreatedAt.Format(time.RFC3339),
		string(wordsJSON), w.Story, string(blanksJSON))
	return err
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
