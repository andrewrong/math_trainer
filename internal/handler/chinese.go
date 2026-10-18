package handler

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"mathtrainer/internal/ai"
	"mathtrainer/internal/model"
	"mathtrainer/internal/pinyin"
)

type ChineseHandler struct {
	db *sql.DB
	ai *ai.Chain
}

func NewChinese(db *sql.DB, chain *ai.Chain) *ChineseHandler {
	return &ChineseHandler{db: db, ai: chain}
}

func (h *ChineseHandler) Register(r gin.IRouter) {
	r.GET("/chinese/weeks", h.listWeeks)
	r.POST("/chinese/weeks", h.createWeek)
	r.GET("/chinese/context-stories/:setKey", h.getContextStory)
	r.POST("/chinese/context-stories/:setKey", h.regenerateContextStory)
	r.GET("/chinese/review/due", h.reviewDue)
	r.PUT("/chinese/review", h.updateReview)
	r.GET("/chinese/weeks/:weekId/review-rounds", h.reviewRounds)
	r.POST("/chinese/weeks/:weekId/review-rounds/:roundIndex/start", h.startReviewRound)
	r.PUT("/chinese/weeks/:weekId/review-rounds/:roundIndex", h.saveReviewRound)
	r.GET("/chinese/wrongs", h.wrongs)
	r.POST("/chinese/wrongs/review", h.wrongReview)
}

func (h *ChineseHandler) listWeeks(c *gin.Context) {
	rows, err := h.db.QueryContext(c, `SELECT week_id,label,created_at FROM chinese_weeks ORDER BY created_at DESC`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	weeks := []model.ChineseWeek{}
	for rows.Next() {
		var w model.ChineseWeek
		if err := rows.Scan(&w.WeekID, &w.Label, &w.CreatedAt); err == nil {
			weeks = append(weeks, w)
		}
	}
	rows.Close()
	for i := range weeks {
		weeks[i].Items = h.loadItems(c, weeks[i].WeekID)
		h.attachChineseStats(c, weeks[i].Items)
	}
	c.JSON(http.StatusOK, gin.H{"weeks": weeks})
}

func (h *ChineseHandler) createWeek(c *gin.Context) {
	var req model.CreateChineseWeekReq
	if err := c.ShouldBindJSON(&req); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "CreateChineseWeekReq.Items") && strings.Contains(msg, "'max'") {
			msg = "too many Chinese items in one week; please keep it under 200"
		}
		c.JSON(http.StatusBadRequest, model.APIError{Error: msg})
		return
	}
	items := normalizeChineseItems(req.Items)
	if len(items) == 0 {
		c.JSON(http.StatusBadRequest, model.APIError{Error: "no valid Chinese items; use 1-8 Chinese characters in the first column"})
		return
	}
	// Pinyin comes from the local data. For a word of several characters the
	// AI may know a better reading, so its local reading is only a fallback.
	fallback := map[int][2]string{}
	for i := range items {
		if items[i].Pinyin != "" {
			continue
		}
		text, tone, ok := pinyin.Read(items[i].Character)
		if !ok {
			continue
		}
		items[i].Pinyin, items[i].Tone = text, tone
		if utf8.RuneCountInString(items[i].Character) > 1 {
			fallback[i] = [2]string{text, tone}
			items[i].Pinyin, items[i].Tone = "", ""
		}
	}
	var enrichErr *enrichError
	if h.ai != nil {
		enriched, err := h.enrichItems(c, items)
		if err != nil {
			if !errors.As(err, &enrichErr) {
				enrichErr = &enrichError{kind: enrichProviderError}
			}
			log.Printf("[chinese] enrichment failed: provider=%s kind=%s%s", enrichErr.providerName(), enrichErr.kind, enrichErr.detail())
		} else if len(enriched) > 0 {
			items = mergeChineseItems(items, enriched)
		}
	}
	for i, fb := range fallback {
		if items[i].Pinyin == "" {
			items[i].Pinyin, items[i].Tone = fb[0], fb[1]
		}
	}
	if missing := chineseItemsMissingPinyin(items); len(missing) > 0 {
		msg := "could not prepare pronunciation for " + strings.Join(missing, "、") + "; this weekly set was not saved."
		if enrichErr != nil {
			msg += " " + enrichErr.userStep() + "."
		}
		c.JSON(http.StatusUnprocessableEntity, model.APIError{Error: msg + " Add pinyin with tone marks or retry after checking the AI service."})
		return
	}
	weekID := fmt.Sprintf("zh-%d", time.Now().UnixNano())
	tx, err := h.db.BeginTx(c, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(c, `INSERT INTO chinese_weeks(week_id,label) VALUES(?,?)`, weekID, req.Label); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	for _, it := range items {
		words, _ := json.Marshal(it.Words)
		if _, err := tx.ExecContext(c, `
			INSERT INTO chinese_items(week_id,character,pinyin,tone,meaning,words_json,example_sentence,pinyin_hint,shape_hint)
			VALUES(?,?,?,?,?,?,?,?,?)`,
			weekID, it.Character, it.Pinyin, it.Tone, it.Meaning, string(words), it.ExampleSentence, it.PinyinHint, it.ShapeHint); err != nil {
			c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.ChineseWeek{WeekID: weekID, Label: req.Label, CreatedAt: time.Now(), Items: items})
}

func (h *ChineseHandler) getContextStory(c *gin.Context) {
	story, err := h.loadContextStory(c, c.Param("setKey"))
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, model.APIError{Error: "no context story yet"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, story)
}

// regenerateContextStory creates one short, child-readable cloze story for a
// weekly set. It does not change any parent-entered word data or review marks.
func (h *ChineseHandler) regenerateContextStory(c *gin.Context) {
	setKey := c.Param("setKey")
	items := h.loadSetItems(c, setKey)
	if len(items) == 0 {
		c.JSON(http.StatusNotFound, model.APIError{Error: "weekly Chinese set not found"})
		return
	}
	story, err := h.generateContextStory(c, setKey, selectChineseContextItems(items, 8))
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, model.APIError{Error: err.Error()})
		return
	}
	storyJSON, err := json.Marshal(story.Sentences)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	blanksJSON, err := json.Marshal(story.Blanks)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	if _, err := h.db.ExecContext(c, `
		INSERT INTO chinese_context_stories(set_key,story_json,blanks_json,created_at)
		VALUES(?,?,?,?)
		ON CONFLICT(set_key) DO UPDATE SET story_json=excluded.story_json, blanks_json=excluded.blanks_json, created_at=excluded.created_at`,
		setKey, string(storyJSON), string(blanksJSON), story.CreatedAt); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, story)
}

// A short story is more useful than an exhausting paragraph. Large imported
// weekly sets are therefore sampled into calm, focused groups, with known weak
// characters first. A later New story can provide a fresh narrative context.
func selectChineseContextItems(items []model.ChineseItem, limit int) []model.ChineseItem {
	if len(items) <= limit {
		return items
	}
	picked := append([]model.ChineseItem(nil), items...)
	sort.SliceStable(picked, func(i, j int) bool {
		if picked[i].WrongCount != picked[j].WrongCount {
			return picked[i].WrongCount > picked[j].WrongCount
		}
		return picked[i].ReviewCount < picked[j].ReviewCount
	})
	return picked[:limit]
}

func (h *ChineseHandler) loadContextStory(ctx context.Context, setKey string) (*model.ChineseContextStory, error) {
	var story model.ChineseContextStory
	var sentencesJSON, blanksJSON string
	if err := h.db.QueryRowContext(ctx, `SELECT story_json,blanks_json,created_at FROM chinese_context_stories WHERE set_key=?`, setKey).
		Scan(&sentencesJSON, &blanksJSON, &story.CreatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(sentencesJSON), &story.Sentences); err != nil {
		return nil, fmt.Errorf("read saved context story: %w", err)
	}
	if err := json.Unmarshal([]byte(blanksJSON), &story.Blanks); err != nil {
		return nil, fmt.Errorf("read saved context blanks: %w", err)
	}
	story.SetKey = setKey
	return &story, nil
}

type generatedChineseContextStory struct {
	Sentences []model.ChineseContextSentence `json:"sentences"`
}

func (h *ChineseHandler) generateContextStory(ctx context.Context, setKey string, items []model.ChineseItem) (*model.ChineseContextStory, error) {
	if h.ai == nil {
		return nil, fmt.Errorf("AI story generation is not configured")
	}
	targets := make([]model.ChineseItem, 0, len(items))
	for _, item := range items {
		targets = append(targets, model.ChineseItem{Character: item.Character, Pinyin: item.Pinyin, Meaning: item.Meaning, Words: item.Words})
	}
	rawTargets, _ := json.Marshal(targets)
	prompt := fmt.Sprintf(`You write very simple, natural Chinese context stories for a child learning a weekly word list.

Target items (use the exact character or word in each "character" field): %s

Return ONLY one valid JSON object:
{"sentences":[{"text":"...","annotations":[{"word":"...","pinyin":"..."}]}]}

Rules:
- Make one connected, everyday story (school, home, playground, or family).
- Use EVERY target item exactly once, and use NO target item more than once.
- Put at most ONE target item in each sentence. There should be one short sentence for each target item; extra linking sentences are optional.
- Each sentence must be short, concrete, and easy for an early primary-school reader (usually 8–20 Chinese characters before punctuation). Do not use abstract language or a complicated plot.
- Add annotations only for genuinely less-common supporting words that a child may not know. Each annotation has the exact word in text and its pinyin with tone marks, e.g. {"word":"操场","pinyin":"cāo chǎng"}. Do not annotate the target items or very basic words.
- End every sentence with Chinese punctuation (。！？).
- Do not include blanks in text; the application creates them after checking the story.`, string(rawTargets))

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		text, err := h.ai.GenerateText(ctx, ai.TextRequest{UserPrompt: prompt, MaxTokens: 8192, ResponseMIMEType: "application/json"})
		if err != nil {
			lastErr = err
			continue
		}
		generated, err := parseGeneratedChineseContextStory(text)
		if err != nil {
			lastErr = err
			continue
		}
		blanks, err := validateChineseContextStory(generated.Sentences, items)
		if err != nil {
			lastErr = err
			continue
		}
		if err := h.reviewChineseContextStory(ctx, generated.Sentences, targets); err != nil {
			lastErr = err
			continue
		}
		return &model.ChineseContextStory{SetKey: setKey, Sentences: generated.Sentences, Blanks: blanks, CreatedAt: time.Now().UTC()}, nil
	}
	return nil, fmt.Errorf("could not generate a checked Chinese context story after two attempts: %w", lastErr)
}

// reviewChineseContextStory is a semantic quality gate. The deterministic
// validator protects the exercise mechanics; this pass protects reading level,
// naturalness, and whether pinyin was used as a helpful scaffold rather than
// noise. It never edits learner-facing content — a rejected story is rewritten.
func (h *ChineseHandler) reviewChineseContextStory(ctx context.Context, sentences []model.ChineseContextSentence, targets []model.ChineseItem) error {
	storyJSON, _ := json.Marshal(sentences)
	targetJSON, _ := json.Marshal(targets)
	prompt := fmt.Sprintf(`You are an independent quality checker for a Chinese context story for a 7–9-year-old child.

Weekly target items: %s
Proposed story: %s

Return ONLY valid JSON: {"approved":true,"reason":"..."}

Approve ONLY if every condition is true:
- It is one coherent, everyday mini-story a child can follow without adult explanation.
- Sentences use concrete, age-appropriate language. No abstract ideas, frightening events, sarcasm, or complicated plot twists.
- Every target item appears exactly once, with at most one target item in each sentence.
- The supporting words are mostly common early-primary Chinese. Any clearly less-common supporting word is annotated with accurate tone-marked pinyin; basic words and target items are not annotated.
- The story is pleasant, emotionally safe, and useful for understanding each target in context.

If any condition fails, set approved to false and name the single most important reason.`, string(targetJSON), string(storyJSON))
	text, err := h.ai.GenerateText(ctx, ai.TextRequest{UserPrompt: prompt, MaxTokens: 2048, ResponseMIMEType: "application/json"})
	if err != nil {
		return fmt.Errorf("review Chinese context story: %w", err)
	}
	text = strings.TrimSpace(text)
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return fmt.Errorf("context story review returned no JSON")
	}
	var review struct {
		Approved bool   `json:"approved"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &review); err != nil {
		return fmt.Errorf("parse context story review: %w", err)
	}
	if !review.Approved {
		reason := strings.TrimSpace(review.Reason)
		if reason == "" {
			reason = "the story was not suitable for the intended reading level"
		}
		return fmt.Errorf("context story review rejected it: %s", reason)
	}
	return nil
}

func parseGeneratedChineseContextStory(text string) (*generatedChineseContextStory, error) {
	text = strings.TrimSpace(text)
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return nil, fmt.Errorf("no JSON object in AI context story")
	}
	var generated generatedChineseContextStory
	if err := json.Unmarshal([]byte(text[start:end+1]), &generated); err != nil {
		return nil, fmt.Errorf("parse AI context story: %w", err)
	}
	return &generated, nil
}

var chineseContextSentencePattern = regexp.MustCompile(`[。！？]$`)

// validateChineseContextStory is the safety gate between the generative model
// and the learner. Its derived blanks prevent duplicate or overlapping prompts.
func validateChineseContextStory(sentences []model.ChineseContextSentence, items []model.ChineseItem) ([]model.ChineseContextBlank, error) {
	if len(sentences) < len(items) || len(sentences) > len(items)+3 {
		return nil, fmt.Errorf("story needs one short sentence per weekly item")
	}
	seenSentences := map[string]bool{}
	for i := range sentences {
		sentences[i].Text = strings.TrimSpace(sentences[i].Text)
		if sentences[i].Text == "" || !chineseContextSentencePattern.MatchString(sentences[i].Text) {
			return nil, fmt.Errorf("story sentence %d is incomplete", i+1)
		}
		if chineseContextReadingLength(sentences[i].Text) > 26 {
			return nil, fmt.Errorf("story sentence %d is too long for this reader", i+1)
		}
		if seenSentences[sentences[i].Text] {
			return nil, fmt.Errorf("story repeats sentence %d", i+1)
		}
		seenSentences[sentences[i].Text] = true
		for _, annotation := range sentences[i].Annotations {
			if strings.TrimSpace(annotation.Word) == "" || strings.TrimSpace(annotation.Pinyin) == "" || strings.Count(sentences[i].Text, annotation.Word) != 1 {
				return nil, fmt.Errorf("story sentence %d has an invalid pinyin annotation", i+1)
			}
			for _, item := range items {
				if strings.Contains(annotation.Word, item.Character) {
					return nil, fmt.Errorf("story sentence %d annotates a weekly target", i+1)
				}
			}
		}
	}

	blanks := make([]model.ChineseContextBlank, 0, len(items))
	for _, item := range items {
		target := strings.TrimSpace(item.Character)
		if target == "" {
			return nil, fmt.Errorf("weekly set has an empty target")
		}
		count, sentenceIndex := 0, -1
		for i, sentence := range sentences {
			occurrences := strings.Count(sentence.Text, target)
			if occurrences > 0 {
				count += occurrences
				sentenceIndex = i
			}
		}
		if count != 1 {
			return nil, fmt.Errorf("story must use %q exactly once", target)
		}
		blanks = append(blanks, model.ChineseContextBlank{Character: target, SentenceIndex: sentenceIndex, Sentence: strings.Replace(sentences[sentenceIndex].Text, target, "____", 1)})
	}
	for i, sentence := range sentences {
		targetsInSentence := 0
		for _, item := range items {
			if strings.Contains(sentence.Text, item.Character) {
				targetsInSentence++
			}
		}
		if targetsInSentence > 1 {
			return nil, fmt.Errorf("story sentence %d contains more than one weekly item", i+1)
		}
	}
	return blanks, nil
}

func chineseContextReadingLength(text string) int {
	count := 0
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			count++
		}
	}
	return count
}

func chineseItemsMissingPinyin(items []model.ChineseItem) []string {
	missing := make([]string, 0)
	for _, item := range items {
		if strings.TrimSpace(item.Pinyin) == "" {
			missing = append(missing, item.Character)
		}
	}
	return missing
}

func (h *ChineseHandler) reviewDue(c *gin.Context) {
	items := h.loadReviewItems(c, 80)
	h.attachChineseStats(c, items)
	sortChineseDue(items)
	if len(items) > 20 {
		items = items[:20]
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *ChineseHandler) updateReview(c *gin.Context) {
	var req model.UpdateChineseReviewReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	if req.Item != nil {
		req.Item.Character = req.Character
		if err := h.saveChineseReviewCatalog(c, *req.Item); err != nil {
			c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
			return
		}
	}
	pinyinWrong, recognitionWrong := 0, 0
	if !req.Correct {
		if req.Kind == "pinyin" {
			pinyinWrong = 1
		} else if req.Kind == "recognition" {
			recognitionWrong = 1
		}
	}
	wrong := 0
	if !req.Correct {
		wrong = 1
	}
	if _, err := h.db.ExecContext(c, `
		INSERT INTO chinese_review_stats(character,wrong_count,review_count,pinyin_wrong,recognition_wrong,last_reviewed_at)
		VALUES(?,?,?,?,?,datetime('now'))
		ON CONFLICT(character) DO UPDATE SET
			wrong_count=wrong_count+excluded.wrong_count,
			review_count=review_count+1,
			pinyin_wrong=pinyin_wrong+excluded.pinyin_wrong,
			recognition_wrong=recognition_wrong+excluded.recognition_wrong,
			last_reviewed_at=datetime('now')`,
		req.Character, wrong, 1, pinyinWrong, recognitionWrong); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	if !req.Correct {
		if _, err := h.db.ExecContext(c, `
			INSERT INTO chinese_review_kind_stats(character,kind,wrong_count,last_reviewed_at)
			VALUES(?,?,1,datetime('now'))
			ON CONFLICT(character,kind) DO UPDATE SET
				wrong_count=wrong_count+1,
				last_reviewed_at=datetime('now')`,
			req.Character, req.Kind); err != nil {
			c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, model.OKResponse{OK: true})
}

func (h *ChineseHandler) saveChineseReviewCatalog(ctx context.Context, item model.ChineseItem) error {
	words, _ := json.Marshal(item.Words)
	_, err := h.db.ExecContext(ctx, `
		INSERT INTO chinese_review_catalog(character,pinyin,tone,meaning,words_json,example_sentence,pinyin_hint,shape_hint,updated_at)
		VALUES(?,?,?,?,?,?,?,?,datetime('now'))
		ON CONFLICT(character) DO UPDATE SET
			pinyin=excluded.pinyin,
			tone=excluded.tone,
			meaning=excluded.meaning,
			words_json=excluded.words_json,
			example_sentence=excluded.example_sentence,
			pinyin_hint=excluded.pinyin_hint,
			shape_hint=excluded.shape_hint,
			updated_at=datetime('now')`,
		item.Character, item.Pinyin, item.Tone, item.Meaning, string(words), item.ExampleSentence, item.PinyinHint, item.ShapeHint)
	return err
}

func (h *ChineseHandler) reviewRounds(c *gin.Context) {
	key := c.Param("weekId")
	items := h.loadSetItems(c, key)
	saved := h.loadRoundStats(c, key)
	rounds := []gin.H{}
	for i := 0; i < len(items); i += 20 {
		idx := i/20 + 1
		end := i + 20
		if end > len(items) {
			end = len(items)
		}
		r := gin.H{
			"roundIndex": idx,
			"startPos":   i + 1,
			"endPos":     end,
			"total":      end - i,
			"correct":    0,
			"wrong":      0,
		}
		if st, ok := saved[idx]; ok {
			r["correct"] = st.Correct
			r["wrong"] = st.Wrong
			r["completedAt"] = st.CompletedAt
		}
		rounds = append(rounds, r)
	}
	c.JSON(http.StatusOK, gin.H{"rounds": rounds, "total": len(items)})
}

func (h *ChineseHandler) startReviewRound(c *gin.Context) {
	idx, _ := strconv.Atoi(c.Param("roundIndex"))
	if idx < 1 {
		idx = 1
	}
	items := h.loadSetItems(c, c.Param("weekId"))
	start := (idx - 1) * 20
	if start >= len(items) {
		c.JSON(http.StatusOK, gin.H{"items": []model.ChineseItem{}})
		return
	}
	end := start + 20
	if end > len(items) {
		end = len(items)
	}
	c.JSON(http.StatusOK, gin.H{"items": items[start:end], "startPos": start + 1, "endPos": end})
}

func (h *ChineseHandler) saveReviewRound(c *gin.Context) {
	idx, _ := strconv.Atoi(c.Param("roundIndex"))
	if idx < 1 {
		idx = 1
	}
	var req model.SaveChineseReviewRoundReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	if _, err := h.db.ExecContext(c, `
		INSERT INTO chinese_review_rounds(week_id,round_index,start_pos,end_pos,total,correct,wrong,completed_at)
		VALUES(?,?,?,?,?,?,?,datetime('now'))
		ON CONFLICT(week_id,round_index) DO UPDATE SET
			start_pos=excluded.start_pos,
			end_pos=excluded.end_pos,
			total=excluded.total,
			correct=excluded.correct,
			wrong=excluded.wrong,
			completed_at=datetime('now')`,
		c.Param("weekId"), idx, req.StartPos, req.EndPos, req.Total, req.Correct, req.Wrong); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.OKResponse{OK: true})
}

func (h *ChineseHandler) wrongs(c *gin.Context) {
	items := h.loadReviewItems(c, 10000)
	h.attachChineseStats(c, items)
	out := []model.ChineseItem{}
	for _, it := range items {
		if it.WrongCount > 0 {
			out = append(out, it)
		}
	}
	sortChineseDue(out)
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (h *ChineseHandler) wrongReview(c *gin.Context) {
	items := h.loadReviewItems(c, 10000)
	h.attachChineseStats(c, items)
	out := []model.ChineseItem{}
	for _, it := range items {
		if it.WrongCount > 0 {
			out = append(out, it)
		}
	}
	sortChineseDue(out)
	if len(out) > 20 {
		out = out[:20]
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (h *ChineseHandler) loadItems(ctx context.Context, weekID string) []model.ChineseItem {
	rows, err := h.db.QueryContext(ctx, `
		SELECT id,character,pinyin,tone,meaning,words_json,example_sentence,pinyin_hint,shape_hint
		FROM chinese_items WHERE week_id=? ORDER BY id`, weekID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []model.ChineseItem{}
	for rows.Next() {
		var it model.ChineseItem
		var words string
		if err := rows.Scan(&it.ID, &it.Character, &it.Pinyin, &it.Tone, &it.Meaning, &words, &it.ExampleSentence, &it.PinyinHint, &it.ShapeHint); err == nil {
			_ = json.Unmarshal([]byte(words), &it.Words)
			out = append(out, it)
		}
	}
	return out
}

func (h *ChineseHandler) loadRecentItems(ctx context.Context, limit int) []model.ChineseItem {
	rows, err := h.db.QueryContext(ctx, `
		SELECT id,character,pinyin,tone,meaning,words_json,example_sentence,pinyin_hint,shape_hint
		FROM chinese_items ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []model.ChineseItem{}
	seen := map[string]bool{}
	for rows.Next() {
		var it model.ChineseItem
		var words string
		if err := rows.Scan(&it.ID, &it.Character, &it.Pinyin, &it.Tone, &it.Meaning, &words, &it.ExampleSentence, &it.PinyinHint, &it.ShapeHint); err == nil && !seen[it.Character] {
			seen[it.Character] = true
			_ = json.Unmarshal([]byte(words), &it.Words)
			out = append(out, it)
		}
	}
	return out
}

func (h *ChineseHandler) loadReviewItems(ctx context.Context, limit int) []model.ChineseItem {
	items := h.loadRecentItems(ctx, limit)
	seen := map[string]bool{}
	for _, item := range items {
		seen[item.Character] = true
	}
	rows, err := h.db.QueryContext(ctx, `
		SELECT character,pinyin,tone,meaning,words_json,example_sentence,pinyin_hint,shape_hint
		FROM chinese_review_catalog ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return items
	}
	defer rows.Close()
	for rows.Next() {
		var item model.ChineseItem
		var words string
		if err := rows.Scan(&item.Character, &item.Pinyin, &item.Tone, &item.Meaning, &words, &item.ExampleSentence, &item.PinyinHint, &item.ShapeHint); err != nil || seen[item.Character] {
			continue
		}
		_ = json.Unmarshal([]byte(words), &item.Words)
		seen[item.Character] = true
		items = append(items, item)
	}
	return items
}

type chineseStat struct {
	WrongCount       int
	ReviewCount      int
	PinyinWrong      int
	RecognitionWrong int
	MeaningWrong     int
	WordWrong        int
	SentenceWrong    int
	ToneWrong        int
	WritingWrong     int
	LastReviewedAt   string
}

type roundStat struct {
	Correct     int
	Wrong       int
	CompletedAt string
}

func (h *ChineseHandler) loadChineseStats(ctx context.Context) map[string]chineseStat {
	rows, err := h.db.QueryContext(ctx, `SELECT character,wrong_count,review_count,pinyin_wrong,recognition_wrong,COALESCE(last_reviewed_at,'') FROM chinese_review_stats`)
	if err != nil {
		return map[string]chineseStat{}
	}
	defer rows.Close()
	out := map[string]chineseStat{}
	for rows.Next() {
		var ch string
		var st chineseStat
		if err := rows.Scan(&ch, &st.WrongCount, &st.ReviewCount, &st.PinyinWrong, &st.RecognitionWrong, &st.LastReviewedAt); err == nil {
			out[ch] = st
		}
	}
	krows, err := h.db.QueryContext(ctx, `SELECT character,kind,wrong_count FROM chinese_review_kind_stats`)
	if err != nil {
		return out
	}
	defer krows.Close()
	for krows.Next() {
		var ch, kind string
		var n int
		if err := krows.Scan(&ch, &kind, &n); err != nil {
			continue
		}
		st := out[ch]
		switch kind {
		case "tone":
			st.ToneWrong += n
		case "writing":
			st.WritingWrong += n
		case "meaning":
			st.MeaningWrong += n
		case "word":
			st.WordWrong += n
		case "sentence":
			st.SentenceWrong += n
		}
		out[ch] = st
	}
	return out
}

func (h *ChineseHandler) attachChineseStats(ctx context.Context, items []model.ChineseItem) {
	stats := h.loadChineseStats(ctx)
	for i := range items {
		if st, ok := stats[items[i].Character]; ok {
			items[i].WrongCount = st.WrongCount
			items[i].ReviewCount = st.ReviewCount
			items[i].PinyinWrong = st.PinyinWrong
			items[i].RecognitionWrong = st.RecognitionWrong
			items[i].MeaningWrong = st.MeaningWrong
			items[i].WordWrong = st.WordWrong
			items[i].SentenceWrong = st.SentenceWrong
			items[i].ToneWrong = st.ToneWrong
			items[i].WritingWrong = st.WritingWrong
			items[i].LastReviewedAt = st.LastReviewedAt
			items[i].WeakKind = weakestChineseKind(st)
		}
	}
}

func (h *ChineseHandler) loadSetItems(ctx context.Context, key string) []model.ChineseItem {
	if strings.HasPrefix(key, "label:") {
		labelBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(key, "label:"))
		if err != nil {
			return nil
		}
		rows, err := h.db.QueryContext(ctx, `
			SELECT i.id,i.character,i.pinyin,i.tone,i.meaning,i.words_json,i.example_sentence,i.pinyin_hint,i.shape_hint
			FROM chinese_weeks w JOIN chinese_items i ON i.week_id=w.week_id
			WHERE w.label=?
			ORDER BY w.created_at,i.id`, string(labelBytes))
		if err != nil {
			return nil
		}
		defer rows.Close()
		items := scanChineseItems(rows)
		h.attachChineseStats(ctx, items)
		return items
	}
	items := h.loadItems(ctx, key)
	h.attachChineseStats(ctx, items)
	return items
}

func scanChineseItems(rows *sql.Rows) []model.ChineseItem {
	out := []model.ChineseItem{}
	seen := map[string]bool{}
	for rows.Next() {
		var it model.ChineseItem
		var words string
		if err := rows.Scan(&it.ID, &it.Character, &it.Pinyin, &it.Tone, &it.Meaning, &words, &it.ExampleSentence, &it.PinyinHint, &it.ShapeHint); err == nil && !seen[it.Character] {
			seen[it.Character] = true
			_ = json.Unmarshal([]byte(words), &it.Words)
			out = append(out, it)
		}
	}
	return out
}

func (h *ChineseHandler) loadRoundStats(ctx context.Context, key string) map[int]roundStat {
	rows, err := h.db.QueryContext(ctx, `SELECT round_index,correct,wrong,COALESCE(completed_at,'') FROM chinese_review_rounds WHERE week_id=?`, key)
	if err != nil {
		return map[int]roundStat{}
	}
	defer rows.Close()
	out := map[int]roundStat{}
	for rows.Next() {
		var idx int
		var st roundStat
		if err := rows.Scan(&idx, &st.Correct, &st.Wrong, &st.CompletedAt); err == nil {
			out[idx] = st
		}
	}
	return out
}

func weakestChineseKind(st chineseStat) string {
	best, name := st.PinyinWrong, "拼音"
	if st.RecognitionWrong > best {
		best, name = st.RecognitionWrong, "认字"
	}
	if st.MeaningWrong > best {
		best, name = st.MeaningWrong, "意思"
	}
	if st.WordWrong > best {
		best, name = st.WordWrong, "组词"
	}
	if st.SentenceWrong > best {
		best, name = st.SentenceWrong, "句子"
	}
	if st.ToneWrong > best {
		best, name = st.ToneWrong, "声调"
	}
	if st.WritingWrong > best {
		name = "写字"
	}
	return name
}

const (
	enrichProviderError = "provider_error"
	enrichNoJSON        = "no_json"
	enrichParseError    = "parse_error"
)

// enrichError says which step of enrichment failed. It never holds item or
// reply content, so it is safe to log.
type enrichError struct {
	kind     string
	provider string
	parseErr string
	replyLen int
}

func (e *enrichError) Error() string { return "chinese enrichment failed: " + e.kind }

func (e *enrichError) providerName() string {
	if e.provider == "" {
		return "unknown"
	}
	return e.provider
}

func (e *enrichError) detail() string {
	if e.kind != enrichParseError {
		return ""
	}
	return fmt.Sprintf(" error=%q reply_len=%d", e.parseErr, e.replyLen)
}

func (e *enrichError) userStep() string {
	switch e.kind {
	case enrichProviderError:
		return "The AI service did not answer"
	default:
		return "The AI reply could not be read"
	}
}

// enrichedChineseItem mirrors model.ChineseItem but takes tone as a number or a string.
type enrichedChineseItem struct {
	model.ChineseItem
	Tone json.RawMessage `json:"tone"`
}

func (it enrichedChineseItem) item() model.ChineseItem {
	out := it.ChineseItem
	var s string
	if json.Unmarshal(it.Tone, &s) == nil {
		out.Tone = s
	} else if string(it.Tone) != "null" && len(it.Tone) > 0 {
		out.Tone = string(it.Tone)
	}
	return out
}

// describeJSONError names a decode failure without the input values that
// json's own error text can carry (for example an overflowing number).
func describeJSONError(err error) string {
	var typeErr *json.UnmarshalTypeError
	var syntaxErr *json.SyntaxError
	switch {
	case errors.As(err, &typeErr):
		kind, _, _ := strings.Cut(typeErr.Value, " ")
		return fmt.Sprintf("cannot unmarshal %s into field %s of type %s", kind, typeErr.Field, typeErr.Type)
	case errors.As(err, &syntaxErr):
		return fmt.Sprintf("invalid JSON syntax at offset %d", syntaxErr.Offset)
	default:
		return "unreadable JSON"
	}
}

func (h *ChineseHandler) enrichItems(ctx context.Context, items []model.ChineseItem) ([]model.ChineseItem, error) {
	raw, _ := json.Marshal(items)
	prompt := `Return ONLY a JSON array. For each simplified Chinese item, fill missing meaning, words, exampleSentence, pinyinHint, shapeHint. For an item of two or more characters with empty pinyin, also give its pinyin with tone marks, separated by spaces (use the reading of the word, not of each character alone); never invent pinyin for a single character. Keep child-friendly wording. Input: ` + string(raw)
	res, err := h.ai.GenerateTextDetailed(ctx, ai.TextRequest{
		SystemPrompt:     "You enrich simplified Chinese learning items for a 9-year-old heritage speaker. Return only JSON.",
		UserPrompt:       prompt,
		MaxTokens:        8192,
		ResponseMIMEType: "application/json",
	})
	if err != nil {
		// A failed chain returns no provider name; name the ones it tried.
		return nil, &enrichError{kind: enrichProviderError, provider: strings.Join(h.ai.Available(), ",")}
	}
	body := extractJSONValue(res.Text)
	if body == "" {
		return nil, &enrichError{kind: enrichNoJSON, provider: res.Provider}
	}
	var decoded []enrichedChineseItem
	if body[0] == '{' {
		var one enrichedChineseItem
		if err := json.Unmarshal([]byte(body), &one); err != nil {
			return nil, &enrichError{kind: enrichParseError, provider: res.Provider, parseErr: describeJSONError(err), replyLen: len(res.Text)}
		}
		decoded = []enrichedChineseItem{one}
	} else if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		return nil, &enrichError{kind: enrichParseError, provider: res.Provider, parseErr: describeJSONError(err), replyLen: len(res.Text)}
	}
	out := make([]model.ChineseItem, len(decoded))
	for i, d := range decoded {
		out[i] = d.item()
	}
	out = normalizeChineseItems(out)
	return out, nil
}

func normalizeChineseItems(items []model.ChineseItem) []model.ChineseItem {
	out := []model.ChineseItem{}
	seen := map[string]bool{}
	for _, it := range items {
		it.Character = strings.TrimSpace(it.Character)
		if !validChineseEntry(it.Character) || seen[it.Character] {
			continue
		}
		seen[it.Character] = true
		it.Pinyin = strings.TrimSpace(it.Pinyin)
		it.Tone = strings.TrimSpace(it.Tone)
		it.Meaning = strings.TrimSpace(it.Meaning)
		it.ExampleSentence = strings.TrimSpace(it.ExampleSentence)
		it.PinyinHint = strings.TrimSpace(it.PinyinHint)
		it.ShapeHint = strings.TrimSpace(it.ShapeHint)
		for i := range it.Words {
			it.Words[i] = strings.TrimSpace(it.Words[i])
		}
		out = append(out, it)
	}
	return out
}

func validChineseEntry(s string) bool {
	n := 0
	for _, r := range s {
		if !unicode.Is(unicode.Han, r) {
			return false
		}
		n++
	}
	return n >= 1 && n <= 8
}

func mergeChineseItems(base, enriched []model.ChineseItem) []model.ChineseItem {
	byChar := map[string]model.ChineseItem{}
	for _, it := range enriched {
		byChar[it.Character] = it
	}
	for i, it := range base {
		en, ok := byChar[it.Character]
		if !ok {
			continue
		}
		if it.Pinyin == "" {
			base[i].Pinyin = en.Pinyin
		}
		if it.Tone == "" {
			base[i].Tone = en.Tone
		}
		if it.Meaning == "" {
			base[i].Meaning = en.Meaning
		}
		if len(it.Words) == 0 {
			base[i].Words = en.Words
		}
		if it.ExampleSentence == "" {
			base[i].ExampleSentence = en.ExampleSentence
		}
		if it.PinyinHint == "" {
			base[i].PinyinHint = en.PinyinHint
		}
		if it.ShapeHint == "" {
			base[i].ShapeHint = en.ShapeHint
		}
	}
	return base
}

// extractJSONValue returns the outermost JSON array or object in s, which may
// be wrapped in a Markdown code fence or other text; "" when there is none.
func extractJSONValue(s string) string {
	start := strings.IndexAny(s, "[{")
	if start < 0 {
		return ""
	}
	closer := "]"
	if s[start] == '{' {
		closer = "}"
	}
	end := strings.LastIndex(s, closer)
	if end < start {
		return ""
	}
	return s[start : end+1]
}

func sortChineseDue(items []model.ChineseItem) {
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if chineseDueScore(items[j]) > chineseDueScore(items[i]) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

func chineseDueScore(it model.ChineseItem) int {
	score := it.WrongCount*10 - it.ReviewCount
	if it.LastReviewedAt == "" {
		score += 5
	}
	return score
}
