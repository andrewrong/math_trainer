package handler

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"

	"mathtrainer/internal/ai"
	"mathtrainer/internal/model"
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
	if h.ai != nil {
		if enriched, err := h.enrichItems(c, items); err == nil && len(enriched) > 0 {
			items = mergeChineseItems(items, enriched)
		}
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

func (h *ChineseHandler) enrichItems(ctx context.Context, items []model.ChineseItem) ([]model.ChineseItem, error) {
	raw, _ := json.Marshal(items)
	prompt := `Return ONLY a JSON array. For each simplified Chinese item, fill missing pinyin, tone, meaning, words, exampleSentence, pinyinHint, shapeHint. Keep child-friendly wording. Input: ` + string(raw)
	text, err := h.ai.GenerateText(ctx, ai.TextRequest{
		SystemPrompt:     "You enrich simplified Chinese learning items for a 9-year-old heritage speaker. Return only JSON.",
		UserPrompt:       prompt,
		MaxTokens:        8192,
		ResponseMIMEType: "application/json",
	})
	if err != nil {
		return nil, err
	}
	var out []model.ChineseItem
	if err := json.Unmarshal([]byte(extractJSONArray(text)), &out); err != nil {
		return nil, err
	}
	return normalizeChineseItems(out), nil
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

func extractJSONArray(s string) string {
	start := strings.Index(s, "[")
	end := strings.LastIndex(s, "]")
	if start < 0 || end < start {
		return "[]"
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
