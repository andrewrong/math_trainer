package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"

	"mathtrainer/internal/ai"
	"mathtrainer/internal/model"
)

type CurriculumHandler struct {
	db *sql.DB
	ai *ai.Chain
}

const (
	p3ArtifactVersion = "p3-mypals-20260807-v2"
	p4ArtifactVersion = "p4-mypals-20260809-v66"
	p5ArtifactVersion = "p5-mypals-20260808-v1"
)

func NewCurriculum(db *sql.DB, chain *ai.Chain) *CurriculumHandler {
	return &CurriculumHandler{db: db, ai: chain}
}

func (h *CurriculumHandler) Register(r gin.IRouter) {
	r.GET("/curriculum/topics", h.listTopics)
	r.GET("/curriculum/topics/:code", h.getTopic)
	r.GET("/curriculum/topics/:code/artifacts", h.getArtifacts)
	r.PUT("/curriculum/topics/:code/progress", h.updateProgress)
	r.POST("/curriculum/textbook-map/import", h.importTextbookMap)
	r.POST("/curriculum/artifacts/generate", h.generateArtifacts)
	r.GET("/curriculum/path", h.curriculumPath)
	r.GET("/learning-plan/main-lesson", h.getMainLesson)
	r.PUT("/learning-plan/main-lesson", h.updateMainLesson)
}

type mainLessonTopic struct {
	TopicCode string `json:"topicCode"`
	Title     string `json:"title"`
	Grade     string `json:"grade"`
	Strand    string `json:"strand"`
	Status    string `json:"status"`
}

type mainLessonResponse struct {
	Mode              string            `json:"mode"`
	SelectedTopicCode string            `json:"selectedTopicCode"`
	ResolvedTopic     mainLessonTopic   `json:"resolvedTopic"`
	Reason            string            `json:"reason"`
	Options           []mainLessonTopic `json:"options"`
}

type updateMainLessonRequest struct {
	Mode      string `json:"mode" binding:"required"`
	TopicCode string `json:"topicCode"`
}

func (h *CurriculumHandler) getMainLesson(c *gin.Context) {
	response, err := h.mainLessonResponse(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *CurriculumHandler) updateMainLesson(c *gin.Context) {
	var req updateMainLessonRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	if req.Mode != "manual" && req.Mode != "auto" {
		c.JSON(http.StatusBadRequest, model.APIError{Error: "mode must be manual or auto"})
		return
	}
	if err := h.ensureSeed(c); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}

	code := strings.TrimSpace(req.TopicCode)
	if code == "" {
		if err := h.db.QueryRowContext(c, `SELECT main_lesson_topic_code FROM learning_plan_settings WHERE id=1`).Scan(&code); err != nil {
			code = "P4-FRACTIONS-MIXED"
		}
	}
	var grade string
	if err := h.db.QueryRowContext(c, `SELECT grade FROM curriculum_topics WHERE topic_code=?`, code).Scan(&grade); err == sql.ErrNoRows {
		c.JSON(http.StatusBadRequest, model.APIError{Error: "topic not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	} else if grade != "P4" {
		c.JSON(http.StatusBadRequest, model.APIError{Error: "main lesson must be a P4 topic"})
		return
	}
	if _, err := h.db.ExecContext(c, `
		INSERT INTO learning_plan_settings(id,main_lesson_mode,main_lesson_topic_code)
		VALUES(1,?,?)
		ON CONFLICT(id) DO UPDATE SET
			main_lesson_mode=excluded.main_lesson_mode,
			main_lesson_topic_code=excluded.main_lesson_topic_code,
			updated_at=datetime('now')`, req.Mode, code); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	response, err := h.mainLessonResponse(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *CurriculumHandler) mainLessonResponse(c *gin.Context) (mainLessonResponse, error) {
	if err := h.ensureSeed(c); err != nil {
		return mainLessonResponse{}, err
	}
	mode, selectedCode := "manual", "P4-FRACTIONS-MIXED"
	err := h.db.QueryRowContext(c, `SELECT main_lesson_mode,main_lesson_topic_code FROM learning_plan_settings WHERE id=1`).Scan(&mode, &selectedCode)
	if err != nil && err != sql.ErrNoRows {
		return mainLessonResponse{}, err
	}

	options, err := h.loadMainLessonOptions(c)
	if err != nil {
		return mainLessonResponse{}, err
	}
	resolved, reason := resolveMainLesson(options, mode, selectedCode)
	return mainLessonResponse{
		Mode:              mode,
		SelectedTopicCode: selectedCode,
		ResolvedTopic:     resolved,
		Reason:            reason,
		Options:           options,
	}, nil
}

func (h *CurriculumHandler) loadMainLessonOptions(c *gin.Context) ([]mainLessonTopic, error) {
	rows, err := h.db.QueryContext(c, `
		SELECT t.topic_code,t.title,t.grade,t.strand,COALESCE(p.status,'Not Started')
		FROM curriculum_topics t
		LEFT JOIN curriculum_progress p ON p.topic_code=t.topic_code
		WHERE t.grade='P4'
		ORDER BY t.sort_order`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	options := []mainLessonTopic{}
	for rows.Next() {
		var topic mainLessonTopic
		if err := rows.Scan(&topic.TopicCode, &topic.Title, &topic.Grade, &topic.Strand, &topic.Status); err != nil {
			return nil, err
		}
		options = append(options, topic)
	}
	return options, rows.Err()
}

func resolveMainLesson(options []mainLessonTopic, mode, selectedCode string) (mainLessonTopic, string) {
	if len(options) == 0 {
		return mainLessonTopic{}, "No P4 topics are available yet."
	}
	if mode == "manual" {
		for _, topic := range options {
			if topic.TopicCode == selectedCode {
				return topic, "Pinned by parent for this week."
			}
		}
		return options[0], "The saved topic was unavailable, so the first P4 topic was used."
	}

	statusPriority := map[string]int{"Learning": 0, "Practicing": 1, "Review": 2, "Not Started": 3, "Mastered": 4}
	best := options[0]
	bestPriority := statusPriority[best.Status]
	for _, topic := range options[1:] {
		priority, ok := statusPriority[topic.Status]
		if !ok {
			priority = 3
		}
		if priority < bestPriority {
			best, bestPriority = topic, priority
		}
	}
	reason := "Recommended from the first P4 topic not yet started."
	switch best.Status {
	case "Learning":
		reason = "Recommended because this topic is currently being learned."
	case "Practicing":
		reason = "Recommended because this topic needs more guided practice."
	case "Review":
		reason = "Recommended to keep a recently learned topic secure."
	case "Mastered":
		reason = "All recorded P4 topics are secure; this topic keeps learning warm."
	}
	return best, reason
}

func (h *CurriculumHandler) ensureSeed(c *gin.Context) error {
	tx, err := h.db.BeginTx(c, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range curriculumSeedTopics() {
		obj, _ := json.Marshal(t.Objectives)
		pre, _ := json.Marshal(t.Prerequisites)
		rel, _ := json.Marshal(t.Related)
		if _, err := tx.ExecContext(c, `
			INSERT INTO curriculum_topics
			(topic_code, grade, strand, sub_strand, title, objectives_json, prerequisites_json, related_json, sort_order)
			VALUES(?,?,?,?,?,?,?,?,?)
			ON CONFLICT(topic_code) DO UPDATE SET
				grade=excluded.grade,
				strand=excluded.strand,
				sub_strand=excluded.sub_strand,
				title=excluded.title,
				objectives_json=excluded.objectives_json,
				prerequisites_json=excluded.prerequisites_json,
				related_json=excluded.related_json,
				sort_order=excluded.sort_order`,
			t.Code, t.Grade, t.Strand, t.SubStrand, t.Title, string(obj), string(pre), string(rel), t.SortOrder); err != nil {
			return err
		}
	}
	for _, m := range curriculumSeedTextbookMappings() {
		var n int
		if err := tx.QueryRowContext(c, `SELECT COUNT(*) FROM curriculum_textbook_map
			WHERE topic_code=? AND book=? AND chapter_title=? AND source=?`,
			m.TopicCode, m.Book, m.ChapterTitle, m.Source).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		if _, err := tx.ExecContext(c, `
			INSERT INTO curriculum_textbook_map(topic_code,textbook_series,book,chapter_title,source,confidence)
			VALUES(?,?,?,?,?,?)`,
			m.TopicCode, "My Pals Are Here! Maths", m.Book, m.ChapterTitle, m.Source, m.Confidence); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (h *CurriculumHandler) listTopics(c *gin.Context) {
	if err := h.ensureSeed(c); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	grade := c.Query("grade")
	q := `SELECT topic_code, grade, strand, sub_strand, title, objectives_json, prerequisites_json, related_json, sort_order
		FROM curriculum_topics`
	args := []any{}
	if grade != "" {
		q += ` WHERE grade = ?`
		args = append(args, grade)
	}
	q += ` ORDER BY grade, sort_order`
	rows, err := h.db.QueryContext(c, q, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	defer rows.Close()
	topics := []model.CurriculumTopic{}
	for rows.Next() {
		t, err := scanCurriculumTopic(rows)
		if err == nil {
			topics = append(topics, t)
		}
	}
	rows.Close()
	for i := range topics {
		h.attachCurriculumMeta(c, &topics[i])
	}
	c.JSON(http.StatusOK, gin.H{"topics": topics})
}

func (h *CurriculumHandler) getTopic(c *gin.Context) {
	if err := h.ensureSeed(c); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	row := h.db.QueryRowContext(c, `SELECT topic_code, grade, strand, sub_strand, title, objectives_json, prerequisites_json, related_json, sort_order
		FROM curriculum_topics WHERE topic_code = ?`, c.Param("code"))
	t, err := scanCurriculumTopic(row)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, model.APIError{Error: "topic not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	h.attachCurriculumMeta(c, &t)
	c.JSON(http.StatusOK, t)
}

func (h *CurriculumHandler) getArtifacts(c *gin.Context) {
	if err := h.ensureSeed(c); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	code := c.Param("code")
	if !h.topicExists(c, code) {
		c.JSON(http.StatusNotFound, model.APIError{Error: "topic not found"})
		return
	}
	if err := h.ensureArtifactsForTopic(c, code); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	artifacts, err := h.loadArtifacts(c, code)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, artifacts)
}

func (h *CurriculumHandler) generateArtifacts(c *gin.Context) {
	if err := h.ensureSeed(c); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	grade := c.DefaultQuery("grade", "P4")
	if grade != "P3" && grade != "P4" && grade != "P5" {
		c.JSON(http.StatusBadRequest, model.APIError{Error: "only P3/P4/P5 artifact generation is supported"})
		return
	}
	n, err := h.generateArtifactsForGrade(c, grade)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "grade": grade, "topics": n})
}

func (h *CurriculumHandler) updateProgress(c *gin.Context) {
	var req model.UpdateCurriculumProgressReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	if !validCurriculumStatus(req.Status) {
		c.JSON(http.StatusBadRequest, model.APIError{Error: "invalid status"})
		return
	}
	code := c.Param("code")
	if _, err := h.db.ExecContext(c, `
		INSERT INTO curriculum_progress(topic_code,status,concept_score,accuracy_score,speed_score,transfer_score)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(topic_code) DO UPDATE SET
			status=excluded.status,
			concept_score=excluded.concept_score,
			accuracy_score=excluded.accuracy_score,
			speed_score=excluded.speed_score,
			transfer_score=excluded.transfer_score,
			updated_at=datetime('now')`,
		code, req.Status, clampScore(req.ConceptScore), clampScore(req.AccuracyScore), clampScore(req.SpeedScore), clampScore(req.TransferScore)); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.OKResponse{OK: true})
}

func (h *CurriculumHandler) importTextbookMap(c *gin.Context) {
	if err := h.ensureSeed(c); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	ct := c.GetHeader("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		h.importTextbookMapImage(c)
		return
	}
	var req model.ImportTextbookMapReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	saved, err := h.saveMappings(c, req.Book, req.Chapters, "manual")
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"mappings": saved})
}

func (h *CurriculumHandler) importTextbookMapImage(c *gin.Context) {
	if h.ai == nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: "AI chain not configured"})
		return
	}
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: "failed to parse form"})
		return
	}
	book := c.DefaultPostForm("book", "unmapped")
	file, header, err := c.Request.FormFile("image")
	if err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: "image field required"})
		return
	}
	defer file.Close()
	img, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: "failed to read image"})
		return
	}
	mime := header.Header.Get("Content-Type")
	if mime == "" {
		mime = detectMIME(img, header.Filename)
	}
	topicList := h.topicListForPrompt(c)
	prompt := fmt.Sprintf(`Read this My Pals Are Here! Maths table-of-contents image.
Return JSON only:
{"chapters":[{"chapterTitle":"...","topicCode":"P3-...","confidence":"ai-suggested"}]}

Use only these topic codes. If unsure, choose the closest code:
%s

Do not copy workbook questions or page text. Only chapter titles and syllabus topic codes.`, topicList)
	out, err := h.ai.GenerateWithVision(c.Request.Context(), ai.VisionRequest{
		SystemPrompt: "You map textbook chapter titles to Singapore primary mathematics syllabus topic codes. Return valid JSON only.",
		UserPrompt:   prompt,
		ImageData:    img,
		MimeType:     mime,
		MaxTokens:    4096,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	chapters, err := parseAIChapterMap(out)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	saved, err := h.saveMappings(c, book, chapters, "ai-image")
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"mappings": saved})
}

func (h *CurriculumHandler) curriculumPath(c *gin.Context) {
	grade := c.DefaultQuery("grade", "P4")
	c.Request.URL.RawQuery = "grade=" + grade
	h.listTopics(c)
}

func (h *CurriculumHandler) loadArtifacts(c *gin.Context, code string) (gin.H, error) {
	rows, err := h.db.QueryContext(c, `SELECT artifact_type, content_json FROM curriculum_artifacts WHERE topic_code=?`, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := gin.H{}
	for rows.Next() {
		var typ string
		var raw string
		if rows.Scan(&typ, &raw) != nil {
			continue
		}
		switch typ {
		case "topic_lesson_pack":
			out["topicLessonPack"] = json.RawMessage(raw)
		case "textbook_lesson_cards":
			out["textbookLessonCards"] = json.RawMessage(raw)
		}
	}
	return out, rows.Err()
}

func curriculumArtifactVersion(code string) string {
	switch code {
	case "P3-ADD-SUB", "P3-MULT-DIV":
		return p3ArtifactVersion
	}
	if strings.HasPrefix(code, "P4-") {
		return p4ArtifactVersion
	}
	if strings.HasPrefix(code, "P5-") {
		return p5ArtifactVersion
	}
	return ""
}

func (h *CurriculumHandler) ensureArtifactsForTopic(c *gin.Context, code string) error {
	var n int
	if err := h.db.QueryRowContext(c, `SELECT COUNT(*) FROM curriculum_artifacts WHERE topic_code=?`, code).Scan(&n); err != nil {
		return err
	}
	if version := curriculumArtifactVersion(code); version != "" {
		var fresh int
		like := `%"artifactVersion":"` + version + `"%`
		if err := h.db.QueryRowContext(c, `SELECT COUNT(*) FROM curriculum_artifacts
			WHERE topic_code=? AND artifact_type='topic_lesson_pack' AND content_json LIKE ?`, code, like).Scan(&fresh); err != nil {
			return err
		}
		if n >= 2 && fresh > 0 {
			return nil
		}
	} else if n >= 2 {
		return nil
	}
	t, err := h.topicByCode(c, code)
	if err != nil || (t.Grade != "P3" && t.Grade != "P4" && t.Grade != "P5") {
		return err
	}
	h.attachCurriculumMeta(c, &t)
	return h.saveGeneratedArtifacts(c, t)
}

func (h *CurriculumHandler) generateArtifactsForGrade(c *gin.Context, grade string) (int, error) {
	rows, err := h.db.QueryContext(c, `SELECT topic_code, grade, strand, sub_strand, title, objectives_json, prerequisites_json, related_json, sort_order
		FROM curriculum_topics WHERE grade=? ORDER BY sort_order`, grade)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	topics := []model.CurriculumTopic{}
	for rows.Next() {
		t, err := scanCurriculumTopic(rows)
		if err == nil {
			topics = append(topics, t)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for i := range topics {
		h.attachCurriculumMeta(c, &topics[i])
		if err := h.saveGeneratedArtifacts(c, topics[i]); err != nil {
			return i, err
		}
	}
	return len(topics), nil
}

func (h *CurriculumHandler) saveGeneratedArtifacts(c *gin.Context, t model.CurriculumTopic) error {
	// ponytail: local original generator; replace with AI enrichment if these templates become the ceiling.
	pack, _ := json.Marshal(buildTopicLessonPack(t))
	cards, _ := json.Marshal(gin.H{"cards": buildTextbookLessonCards(t)})
	tx, err := h.db.BeginTx(c, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(c, `DELETE FROM curriculum_artifacts WHERE topic_code=? AND artifact_type IN ('topic_lesson_pack','textbook_lesson_cards')`, t.TopicCode); err != nil {
		return err
	}
	if _, err := tx.ExecContext(c, `INSERT INTO curriculum_artifacts(topic_code,artifact_type,content_json) VALUES(?,?,?)`, t.TopicCode, "topic_lesson_pack", string(pack)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(c, `INSERT INTO curriculum_artifacts(topic_code,artifact_type,content_json) VALUES(?,?,?)`, t.TopicCode, "textbook_lesson_cards", string(cards)); err != nil {
		return err
	}
	return tx.Commit()
}

func (h *CurriculumHandler) topicByCode(c *gin.Context, code string) (model.CurriculumTopic, error) {
	row := h.db.QueryRowContext(c, `SELECT topic_code, grade, strand, sub_strand, title, objectives_json, prerequisites_json, related_json, sort_order
		FROM curriculum_topics WHERE topic_code=?`, code)
	return scanCurriculumTopic(row)
}

type seedTopic struct {
	Code          string
	Grade         string
	Strand        string
	SubStrand     string
	Title         string
	Objectives    []string
	Prerequisites []string
	Related       []string
	SortOrder     int
}

type seedTextbookMapping struct {
	TopicCode    string
	Book         string
	ChapterTitle string
	Source       string
	Confidence   string
}

type chapterImport struct {
	ChapterTitle string `json:"chapterTitle"`
	TopicCode    string `json:"topicCode"`
	Confidence   string `json:"confidence"`
}

type scanRow interface {
	Scan(dest ...any) error
}

func scanCurriculumTopic(row scanRow) (model.CurriculumTopic, error) {
	var t model.CurriculumTopic
	var obj, pre, rel string
	err := row.Scan(&t.TopicCode, &t.Grade, &t.Strand, &t.SubStrand, &t.Title, &obj, &pre, &rel, &t.SortOrder)
	t.Objectives = json.RawMessage(obj)
	t.Prerequisites = json.RawMessage(pre)
	t.Related = json.RawMessage(rel)
	return t, err
}

func (h *CurriculumHandler) attachCurriculumMeta(c *gin.Context, t *model.CurriculumTopic) {
	var p model.CurriculumProgress
	err := h.db.QueryRowContext(c, `SELECT topic_code,status,concept_score,accuracy_score,speed_score,transfer_score,updated_at
		FROM curriculum_progress WHERE topic_code=?`, t.TopicCode).
		Scan(&p.TopicCode, &p.Status, &p.ConceptScore, &p.AccuracyScore, &p.SpeedScore, &p.TransferScore, &p.UpdatedAt)
	if err == nil {
		t.Progress = &p
	}
	rows, err := h.db.QueryContext(c, `SELECT id, topic_code, textbook_series, book, chapter_title, source, confidence
		FROM curriculum_textbook_map WHERE topic_code=? ORDER BY book, chapter_title`, t.TopicCode)
	if err != nil {
		return
	}
	defer rows.Close()
	t.Mappings = []model.TextbookMapping{}
	for rows.Next() {
		var m model.TextbookMapping
		if rows.Scan(&m.ID, &m.TopicCode, &m.TextbookSeries, &m.Book, &m.ChapterTitle, &m.Source, &m.Confidence) == nil {
			t.Mappings = append(t.Mappings, m)
		}
	}
}

func (h *CurriculumHandler) saveMappings(c *gin.Context, book string, chapters []struct {
	ChapterTitle string `json:"chapterTitle" binding:"required"`
	TopicCode    string `json:"topicCode"`
	Confidence   string `json:"confidence"`
}, source string) ([]model.TextbookMapping, error) {
	out := []model.TextbookMapping{}
	for _, ch := range chapters {
		title := strings.TrimSpace(ch.ChapterTitle)
		if title == "" {
			continue
		}
		code := strings.TrimSpace(ch.TopicCode)
		if code == "" {
			code = h.guessTopicCode(c, title)
		}
		if !h.topicExists(c, code) {
			code = "UNMAPPED"
		}
		conf := ch.Confidence
		if conf == "" {
			conf = "confirmed"
		}
		res, err := h.db.ExecContext(c, `
			INSERT INTO curriculum_textbook_map(topic_code,textbook_series,book,chapter_title,source,confidence)
			VALUES(?,?,?,?,?,?)`,
			code, "My Pals Are Here! Maths", book, title, source, conf)
		if err != nil {
			return nil, err
		}
		id, _ := res.LastInsertId()
		out = append(out, model.TextbookMapping{ID: id, TopicCode: code, TextbookSeries: "My Pals Are Here! Maths", Book: book, ChapterTitle: title, Source: source, Confidence: conf})
	}
	return out, nil
}

func parseAIChapterMap(raw string) ([]struct {
	ChapterTitle string `json:"chapterTitle" binding:"required"`
	TopicCode    string `json:"topicCode"`
	Confidence   string `json:"confidence"`
}, error) {
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object found in AI response")
	}
	var parsed struct {
		Chapters []struct {
			ChapterTitle string `json:"chapterTitle" binding:"required"`
			TopicCode    string `json:"topicCode"`
			Confidence   string `json:"confidence"`
		} `json:"chapters"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &parsed); err != nil {
		return nil, err
	}
	return parsed.Chapters, nil
}

func (h *CurriculumHandler) topicListForPrompt(c *gin.Context) string {
	rows, err := h.db.QueryContext(c, `SELECT topic_code,title FROM curriculum_topics ORDER BY sort_order`)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var code, title string
		rows.Scan(&code, &title)
		b.WriteString(code)
		b.WriteString(" = ")
		b.WriteString(title)
		b.WriteString("\n")
	}
	return b.String()
}

func (h *CurriculumHandler) topicExists(c *gin.Context, code string) bool {
	if code == "UNMAPPED" {
		return true
	}
	var n int
	h.db.QueryRowContext(c, `SELECT COUNT(*) FROM curriculum_topics WHERE topic_code=?`, code).Scan(&n)
	return n > 0
}

func (h *CurriculumHandler) guessTopicCode(c *gin.Context, title string) string {
	s := strings.ToLower(title)
	rules := []struct{ re, code string }{
		{"100 ?000|hundred thousand|rounding", "P4-WHOLE-100000"},
		{"factor|multiple", "P4-FACTORS-MULTIPLES"},
		{"decimal", "P4-DECIMALS"},
		{"improper|mixed number", "P4-FRACTIONS-MIXED"},
		{"area|perimeter|composite", "P4-AREA-PERIMETER"},
		{"angle", "P4-GEOMETRY-ANGLES"},
		{"symmetry", "P4-GEOMETRY-SYMMETRY"},
		{"net|solid", "P4-GEOMETRY-NETS"},
		{"24-hour clock|seconds", "P4-TIME"},
		{"time|duration", "P3-TIME"},
		{"mass|weight|kg|gram|volume|litre|length", "P3-MEASUREMENT"},
		{"remainder", "P3-MULT-DIV"},
		{"10 ?000|ten thousand", "P3-WHOLE-10000"},
		{"bar graph|table|line graph|pie", "P4-STATISTICS"},
	}
	for _, r := range rules {
		if regexp.MustCompile(r.re).FindString(s) != "" {
			return r.code
		}
	}
	return "UNMAPPED"
}

func validCurriculumStatus(s string) bool {
	switch s {
	case "Not Started", "Learning", "Practicing", "Review", "Mastered":
		return true
	default:
		return false
	}
}

func clampScore(n int) int {
	if n < 0 {
		return 0
	}
	if n > 100 {
		return 100
	}
	return n
}

func buildTopicLessonPack(t model.CurriculumTopic) gin.H {
	code := t.TopicCode
	base := gin.H{
		"bigIdea":              "Mathematics is a way to explain a situation, not just a way to get an answer.",
		"plain":                "Read the situation, choose a model, work step by step, then check whether the answer makes sense.",
		"conceptModel":         gin.H{"visualKind": "generic", "title": "Model first", "description": "Use a picture, table, number line, or bar model before calculating."},
		"whyItWorks":           []string{"A model shows what the numbers mean.", "A method can be reused when memory is not enough.", "Checking catches answers that do not fit the story."},
		"methodSteps":          []string{"Say what is known.", "Say what is unknown.", "Choose a model.", "Calculate carefully.", "Explain why the answer makes sense."},
		"workedExamples":       []gin.H{ex("Model before answer", "If a problem feels confusing, draw what is happening first. The drawing tells you which operation fits.")},
		"commonMisconceptions": []gin.H{mis("Only looking for keywords", "Keywords can trick you. Use the story and model to decide the operation.")},
		"guidedPractice":       []gin.H{cq("Which is the best first step?", "Choose the thinking move.", []string{"Guess", "Draw or describe the model", "Copy an old answer", "Skip the question"}, "Draw or describe the model", "The model makes the meaning of the numbers visible.")},
		"miniTest":             []gin.H{cq("A strong answer should include...", "Choose the complete response.", []string{"Only a number", "A number and reasoning", "Only a drawing", "Only speed"}, "A number and reasoning", "Reasoning shows she understands the concept, not just the answer.")},
		"challenge":            []gin.H{cq("A word problem feels new. What should you do first?", "Use the concept, not memory.", []string{"Look for a keyword only", "Build a model of the situation", "Pick the biggest number", "Skip the explanation"}, "Build a model of the situation", "A model lets her transfer the concept to an unfamiliar problem.")},
		"review":               []string{"Explain one example out loud.", "Redo one similar question.", "Tell what mistake would be easy to make."},
		"parentCoachNotes":     []string{"家长只追问：你知道什么？要求什么？为什么用这个方法？", "不要急着纠正全部错误，先抓住一个关键概念。"},
	}
	if code == "P3-ADD-SUB" {
		pack := buildP3AdditionSubtractionPack(base)
		pack["artifactVersion"] = p3ArtifactVersion
		return pack
	}
	if code == "P3-MULT-DIV" {
		pack := buildP3MultiplicationDivisionPack(base)
		pack["artifactVersion"] = p3ArtifactVersion
		return pack
	}
	if strings.HasPrefix(code, "P4-") {
		if pack, ok := buildP4TopicLessonPack(code, base); ok {
			pack["artifactVersion"] = p4ArtifactVersion
			return pack
		}
	}
	if strings.HasPrefix(code, "P5-") {
		if pack, ok := buildP5TopicLessonPack(code, base); ok {
			pack["artifactVersion"] = p5ArtifactVersion
			return pack
		}
	}
	switch {
	case strings.Contains(code, "WHOLE"):
		return mergePack(base, "Whole numbers are built from place value.", "A large number is not a long string of digits. Each digit has a job based on its place.", "place",
			[]string{"The same digit can mean different amounts in different places.", "Comparing starts from the largest place because it has the greatest value.", "Rounding works by choosing a target place and checking the digit next door."},
			[]string{"Read each digit by place value.", "Compare from the left.", "For rounding, underline the target place.", "Look at the digit to the right: 5 or more rounds up."},
			[]gin.H{ex("Place value", "In 58,304, the 5 means 50,000, the 8 means 8,000, the 3 means 300, and the 4 means 4."), ex("Rounding", "58,304 to the nearest thousand is 58,000 because the hundreds digit is 3.")},
			[]gin.H{mis("Reading 50,000 as 5,000", "Ask: what place is the digit in? Ten-thousands means groups of 10,000."), mis("Rounding every digit", "Only one target place matters. Look only at the digit to its right.")},
			[]gin.H{cq("In 74,892, what is the value of 7?", "Find the place of 7.", []string{"7", "70", "7,000", "70,000"}, "70,000", "The 7 is in the ten-thousands place."), cq("Round 36,749 to the nearest thousand.", "Look at the hundreds digit.", []string{"36,000", "36,700", "37,000", "40,000"}, "37,000", "The hundreds digit is 7, so 36,749 rounds up to 37,000.")},
			[]gin.H{cq("Which number is 2,000 more than 48,375?", "Change only the thousands place.", []string{"48,575", "50,375", "68,375", "50,3750"}, "50,375", "Adding 2,000 changes 48 thousands to 50 thousands. The other places stay the same."), cq("What is the missing number: 12,500, 13,000, 13,500, ___?", "Find the pattern.", []string{"13,600", "14,000", "14,500", "15,000"}, "14,000", "The pattern adds 500 each time.")},
			cq("A stadium has 49,860 seats. About how many seats is this to the nearest thousand, and why?", "Use rounding plus explanation.", []string{"49,000 because 8 is large", "50,000 because 860 is closer to 1,000 than 0", "49,800 because only tens matter", "60,000 because 9 rounds up"}, "50,000 because 860 is closer to 1,000 than 0", "49,860 is 140 away from 50,000 but 860 away from 49,000, so 50,000 is the better estimate."))
	case strings.Contains(code, "ADD-SUB"):
		return mergePack(base, "Addition and subtraction are place-value changes.", "Keep ones with ones, tens with tens, and hundreds with hundreds. Regrouping is just trading between places.", "place",
			[]string{"Ten ones can be traded for one ten.", "Ten tens can be traded for one hundred.", "Subtraction with regrouping works because the total value stays the same."},
			[]string{"Line up place values.", "Start from ones.", "Regroup only when needed.", "Estimate to check the answer."},
			[]gin.H{ex("Addition regrouping", "368 + 457: 8 + 7 = 15, write 5 ones and carry 1 ten."), ex("Subtraction regrouping", "503 - 278: trade 1 hundred into 10 tens, then trade 1 ten into 10 ones.")},
			[]gin.H{mis("Digits not lined up", "Use place value columns. A tens digit cannot sit under a hundreds digit."), mis("Forgetting regrouped value", "Say the new place values out loud before subtracting.")},
			[]gin.H{cq("627 + 248 = ?", "Add by place value.", []string{"865", "875", "885", "975"}, "875", "7+8=15, carry 1. 2 tens + 4 tens + 1 = 7 tens. 6 hundreds + 2 hundreds = 8 hundreds."), cq("14 - 6 = ?", "Use count up or break 10.", []string{"6", "7", "8", "9"}, "8", "From 6 to 10 is 4, and from 10 to 14 is 4. 4 + 4 = 8.")},
			[]gin.H{cq("4,006 - 2,378 = ?", "Regroup across zeros.", []string{"1,628", "1,738", "2,628", "1,672"}, "1,628", "Regroup 4,006 so the ones and tens can subtract. 4006 - 2378 = 1628."), cq("Which estimate best checks 2,947 + 3,156?", "Round to thousands.", []string{"about 5,000", "about 6,000", "about 7,000", "about 9,000"}, "about 6,000", "2,947 is about 3,000 and 3,156 is about 3,000, so total is about 6,000.")},
			cq("Maya had 3,275 stickers. She bought 1,486 more and gave away 928. Which expression matches the story?", "Two-step add/sub story.", []string{"3,275 - 1,486 + 928", "3,275 + 1,486 - 928", "3,275 + 1,486 + 928", "3,275 - 1,486 - 928"}, "3,275 + 1,486 - 928", "Bought more means add. Gave away means subtract. The expression follows the story order."))
	case strings.Contains(code, "FOUR-OPERATIONS"):
		return mergePack(base, "Algorithms are organised place-value work.", "P4 multiplication and division are not new meanings. They are efficient written methods for equal groups, built from place value, partial products, regrouping, and checking.", "place",
			[]string{"A multi-digit multiplication splits a number by place value.", "Long division records repeated sharing or grouping one place at a time.", "Every algorithm step must be checkable by estimation or inverse operation."},
			[]string{"Estimate first so the answer has a sensible size.", "Break the number by place value.", "Work one place at a time and record regrouping.", "For division, divide, multiply, subtract, bring down.", "Check with inverse operation."},
			[]gin.H{ex("4-digit by 1-digit", "3,426 × 4 means 3,000×4 + 400×4 + 20×4 + 6×4. The written algorithm is a compact way to do those partial products."), ex("Long division", "846 ÷ 3: share 8 hundreds, then 24 tens, then 6 ones. The answer is 282.")},
			[]gin.H{mis("Writing digits without place value", "Say what each digit represents: 14 tens, 9 hundreds, not just 14 and 9."), mis("Skipping the division check", "Use divisor × quotient + remainder. If it does not rebuild the dividend, the algorithm has an error.")},
			[]gin.H{cq("236 × 4 = ?", "Use place-value multiplication.", []string{"824", "944", "1244", "914"}, "944", "4×6=24, write 4 carry 2 tens. 4×3 tens + 2 tens = 14 tens, write 4 carry 1 hundred. 4×2 hundreds + 1 hundred = 9 hundreds."), cq("846 ÷ 3 = ?", "Divide hundreds, tens, then ones.", []string{"262", "282", "292", "288"}, "282", "8 hundreds ÷3 gives 2 hundreds with 2 hundreds left. 24 tens ÷3 gives 8 tens. 6 ones ÷3 gives 2 ones.")},
			[]gin.H{cq("3,218 × 6 is closest to...", "Estimate before calculating.", []string{"1,800", "18,000", "19,000", "190,000"}, "19,000", "3,218 is about 3,200. 3,200 × 6 = 19,200, so about 19,000."), cq("Check 728 ÷ 4 = 182. Which check is correct?", "Use divisor × quotient.", []string{"182 × 4 = 728", "728 × 4 = 2912", "182 + 4 = 186", "728 - 182 = 546"}, "182 × 4 = 728", "Division can be checked by multiplication: quotient × divisor should rebuild the dividend.")},
			cq("A school orders 24 boxes with 36 pencils in each box. Which method shows the best P4 thinking?", "Use partial products, not guesswork.", []string{"24 + 36", "36 × 20 + 36 × 4", "36 ÷ 24", "24 - 36"}, "36 × 20 + 36 × 4", "Multiplying by 24 means 20 groups of 36 plus 4 groups of 36. This is the logic behind the written algorithm."))
	case strings.Contains(code, "MULT-DIV") || strings.Contains(code, "FACTORS-MULTIPLES"):
		return mergePack(base, "Multiplication and division are equal-group stories.", "When a formula is forgotten, rebuild the idea from groups: number of groups, size of each group, and total.", "division",
			[]string{"Multiplication combines equal groups.", "Division either shares equally or makes equal groups.", "Remainders are leftovers and must be smaller than the divisor."},
			[]string{"Decide: equal groups, sharing, or grouping.", "Turn division into missing multiplication.", "For remainders, check divisor × quotient + remainder.", "For word problems, decide what the leftover means."},
			[]gin.H{ex("Missing multiplication", "42 ÷ 7 means 7 × ? = 42, so the answer is 6."), ex("Remainder", "38 ÷ 5 = 7 r 3 because 5 × 7 + 3 = 38.")},
			[]gin.H{mis("Remainder too large", "If the remainder is equal to or larger than the divisor, make one more group."), mis("Mixing sharing and grouping", "Sharing asks how many each gets. Grouping asks how many groups can be made.")},
			[]gin.H{cq("46 ÷ 6 = ?", "The remainder must be smaller than 6.", []string{"7 r 4", "8 r 2", "6 r 10", "7 r 6"}, "7 r 4", "6 × 7 = 42 and 46 - 42 = 4."), cq("Which check is correct for 38 ÷ 5 = 7 r 3?", "Use divisor × quotient + remainder.", []string{"5 × 7 + 3 = 38", "7 × 3 + 5 = 26", "38 × 5 = 190", "5 + 7 + 3 = 15"}, "5 × 7 + 3 = 38", "The divisor is 5, quotient is 7, and remainder is 3.")},
			[]gin.H{cq("A class packs 187 books into boxes of 8. How many full boxes?", "Use quotient and interpret remainder.", []string{"22", "23", "23 r 3", "24"}, "23", "8 × 23 = 184, with 3 books left. Full boxes means 23."), cq("Which number is both a factor of 48 and a multiple of 6?", "Check both conditions.", []string{"5", "8", "12", "18"}, "12", "48 ÷ 12 = 4, so 12 is a factor of 48. 6 × 2 = 12, so it is also a multiple of 6.")},
			cq("225 pupils need buses. Each bus holds 40 pupils. How many buses are needed?", "Remainder changes the real answer.", []string{"5", "5 r 25", "6", "7"}, "6", "40 × 5 = 200, with 25 pupils left. They still need a bus, so 6 buses are needed."))
	case strings.Contains(code, "FRACTIONS"):
		return mergePack(base, "Fractions name equal parts of a whole or a set.", "The denominator tells the size of the parts. The numerator tells how many parts we have.", "fraction",
			[]string{"Fractions only work when the parts are equal.", "Equivalent fractions are the same amount cut into different numbers of pieces.", "Mixed numbers and improper fractions are two ways to describe the same value."},
			[]string{"Identify the whole.", "Check equal parts.", "Read denominator as part size.", "Use diagrams or number lines to compare.", "Convert mixed/improper forms when helpful."},
			[]gin.H{ex("Equivalent fractions", "1/2 = 2/4 because each half is split into two equal pieces."), ex("Improper fraction", "7/3 = 2 1/3 because 6 thirds make 2 wholes, with 1 third left.")},
			[]gin.H{mis("Comparing only numerators", "3/8 is not automatically bigger than 2/3. The pieces are different sizes."), mis("Unequal parts", "If parts are not equal, the fraction name is not valid.")},
			[]gin.H{cq("Which is equivalent to 1/2?", "Same amount, different pieces.", []string{"2/3", "2/4", "3/5", "4/6"}, "2/4", "Split each half into two equal parts and 1/2 becomes 2/4."), cq("5/4 is...", "4 fourths make one whole.", []string{"less than 1", "equal to 1", "1 1/4", "2 1/4"}, "1 1/4", "5 fourths = 4 fourths + 1 fourth.")},
			[]gin.H{cq("Which is greater?", "Use equivalent parts or a number line.", []string{"2/3", "3/6", "They are equal", "Cannot tell"}, "2/3", "3/6 = 1/2, and 2/3 is greater than 1/2."), cq("1 2/5 = ?", "Convert mixed number to improper fraction.", []string{"3/5", "5/7", "7/5", "12/5"}, "7/5", "1 whole is 5/5. 5/5 + 2/5 = 7/5.")},
			cq("A ribbon is 3/4 m. Sara uses 1/3 m. Which idea helps compare what is left?", "Different denominators need common-sized pieces.", []string{"Subtract 3 - 1 only", "Use common equal parts", "Compare only denominators", "Ignore the units"}, "Use common equal parts", "Fourth and third pieces are different sizes. Common equal parts make the subtraction meaningful."))
	case strings.Contains(code, "DECIMALS"):
		return mergePack(base, "Decimals extend place value to parts smaller than one.", "Tenths, hundredths, and thousandths are the same place-value idea continuing after the ones place.", "decimal",
			[]string{"0.6 means 6 tenths, not 6 ones.", "Adding zeros after a decimal can keep the same value.", "Decimal operations work best when decimal points are lined up."},
			[]string{"Name each decimal place.", "Use equivalent forms like 0.6 = 0.60.", "Line up decimal points.", "Estimate before calculating."},
			[]gin.H{ex("Place value", "3.472 means 3 ones, 4 tenths, 7 hundredths, and 2 thousandths."), ex("Compare", "2.5 = 2.50, so 2.50 is greater than 2.47.")},
			[]gin.H{mis("Thinking more digits means bigger", "0.9 is bigger than 0.12 because 9 tenths is bigger than 1 tenth."), mis("Not lining up decimal points", "Add by place value: tenths with tenths, hundredths with hundredths.")},
			[]gin.H{cq("0.6 + 0.25 = ?", "Write 0.6 as 0.60.", []string{"0.31", "0.65", "0.85", "0.925"}, "0.85", "0.60 + 0.25 = 0.85."), cq("What is the value of 7 in 3.472?", "Look at the second digit after the decimal.", []string{"7 ones", "7 tenths", "7 hundredths", "7 thousandths"}, "7 hundredths", "Tenths, hundredths, thousandths: the 7 is hundredths.")},
			[]gin.H{cq("Which is greatest?", "Add zeros to compare.", []string{"0.8", "0.75", "0.708", "0.079"}, "0.8", "0.8 = 0.800, which is greater than 0.750, 0.708, and 0.079."), cq("4.5 - 1.28 = ?", "Line up decimal points.", []string{"3.22", "3.32", "3.28", "2.22"}, "3.22", "4.50 - 1.28 = 3.22.")},
			cq("A bottle has 1.25 l of juice. Another has 0.8 l. About how much juice altogether?", "Estimate and use decimal place value.", []string{"about 1 l", "about 2 l", "about 3 l", "about 8 l"}, "about 2 l", "1.25 is about 1.3 and 0.8 is about 0.8; together about 2.1 l, so about 2 l."))
	case strings.Contains(code, "MONEY"):
		return mergePack(base, "Money uses decimal place value in a real situation.", "Dollars and cents are connected: 100 cents make 1 dollar, so careful regrouping matters.", "decimal",
			[]string{"$1 = 100 cents.", "Money addition and subtraction follow place value.", "Word problems need you to decide whether money is combined, spent, or compared."},
			[]string{"Convert if needed.", "Line up dollars and cents.", "Calculate.", "Check whether the answer should be more or less money."},
			[]gin.H{ex("Add money", "$3.45 + $2.80 = $6.25 because 45c + 80c = 125c = $1.25."), ex("Subtract money", "$10.00 - $3.75 = $6.25 after regrouping 1 dollar into 100 cents.")},
			[]gin.H{mis("Treating cents like whole dollars", "75 cents is $0.75, not $75."), mis("Forgetting to regroup cents", "100 cents can trade for 1 dollar.")},
			[]gin.H{cq("$4.50 + $2.75 = ?", "Add cents carefully.", []string{"$6.25", "$7.25", "$7.15", "$6.125"}, "$7.25", "50c + 75c = 125c = $1.25. $4 + $2 + $1.25 = $7.25."), cq("$10.00 - $3.40 = ?", "Regroup dollars to cents.", []string{"$6.60", "$7.60", "$6.40", "$13.40"}, "$6.60", "$10.00 - $3.00 = $7.00, then minus $0.40 = $6.60.")},
			[]gin.H{cq("A pen costs $1.85 and a notebook costs $3.70. Total?", "Line up dollars and cents.", []string{"$4.55", "$5.45", "$5.55", "$6.55"}, "$5.55", "$1.85 + $3.70 = $5.55."), cq("You pay $10 for something costing $6.35. Change?", "Subtract from $10.00.", []string{"$3.65", "$4.65", "$3.75", "$16.35"}, "$3.65", "$10.00 - $6.35 = $3.65.")},
			cq("Three items cost $2.40, $3.85, and $1.70. Which estimate is most reasonable?", "Estimate money before exact work.", []string{"about $5", "about $8", "about $12", "about $20"}, "about $8", "$2.40≈$2.50, $3.85≈$4, $1.70≈$1.50; total is about $8."))
	case strings.Contains(code, "MEASUREMENT"):
		return mergePack(base, "Measurement means choosing and converting units.", "Before calculating, make the units match. The number changes, but the amount stays the same.", "measure",
			[]string{"1 km = 1000 m, 1 kg = 1000 g, 1 l = 1000 ml.", "Compound units can be converted into one smaller unit.", "Word problems often mix units on purpose."},
			[]string{"Name the unit.", "Convert everything to one unit.", "Calculate.", "Convert back if useful.", "Write the unit in the answer."},
			[]gin.H{ex("Mass", "3 kg 250 g = 3250 g because 3 kg = 3000 g."), ex("Volume", "1 l 400 ml + 750 ml = 2150 ml = 2 l 150 ml.")},
			[]gin.H{mis("Adding unlike units", "Do not add 3 kg + 250 g as 253. Convert first."), mis("Dropping the unit", "A measurement answer without a unit is incomplete.")},
			[]gin.H{cq("2 kg 300 g = ___ g", "1 kg = 1000 g.", []string{"230 g", "2030 g", "2300 g", "3200 g"}, "2300 g", "2 kg = 2000 g, plus 300 g gives 2300 g."), cq("3 km 45 m = ___ m", "1 km = 1000 m.", []string{"345 m", "3045 m", "30045 m", "3450 m"}, "3045 m", "3 km = 3000 m, plus 45 m gives 3045 m.")},
			[]gin.H{cq("1 l 250 ml + 800 ml = ?", "Convert or add millilitres.", []string{"1 l 1050 ml", "2 l 50 ml", "2050 l", "1 l 950 ml"}, "2 l 50 ml", "250 ml + 800 ml = 1050 ml = 1 l 50 ml. Add the original 1 l."), cq("4500 g = ?", "1000 g = 1 kg.", []string{"4 kg 500 g", "45 kg", "450 kg", "4 g 500 kg"}, "4 kg 500 g", "4000 g is 4 kg, with 500 g left.")},
			cq("A route is 2 km 350 m. A second route is 1 km 780 m. How much longer is the first?", "Convert to metres first.", []string{"570 m", "630 m", "1 km 430 m", "4 km 130 m"}, "570 m", "2350 m - 1780 m = 570 m. Converting first prevents unit confusion."))
	case strings.Contains(code, "TIME"):
		return mergePack(base, "Time problems connect start, finish, and duration.", "If you know two parts, you can find the third. A timeline keeps the jumps clear.", "time",
			[]string{"Duration is elapsed time, not the number shown on the clock.", "Crossing noon or midnight changes am/pm.", "24-hour time removes am/pm but needs careful conversion."},
			[]string{"Write start, finish, duration.", "Draw a timeline.", "Jump to an easy time.", "Cross noon/midnight carefully.", "Check am/pm or 24-hour format."},
			[]gin.H{ex("Cross noon", "11:00 am + 1 h 30 min = 12:30 pm."), ex("Cross midnight", "11:00 pm + 1 h 30 min = 12:30 am.")},
			[]gin.H{mis("12 am and 12 pm confusion", "12 am is midnight. 12 pm is noon."), mis("Subtracting clock numbers only", "Use a timeline because 9:50 to 10:10 is 20 minutes, not 60.")},
			[]gin.H{cq("11:00 am + 1 h 30 min = ?", "Cross noon.", []string{"12:30 am", "12:30 pm", "1:30 pm", "10:30 am"}, "12:30 pm", "11:00 am + 1 hour is 12:00 pm, then +30 minutes is 12:30 pm."), cq("18:45 is...", "Convert from 24-hour time.", []string{"6:45 am", "6:45 pm", "8:45 pm", "18:45 am"}, "6:45 pm", "18 - 12 = 6, so 18:45 is 6:45 pm.")},
			[]gin.H{cq("9:20 am to 10:05 am is how long?", "Jump to 10:00 first.", []string{"35 min", "45 min", "55 min", "1 h 15 min"}, "45 min", "9:20 to 10:00 is 40 min, then 5 more min."), cq("12:15 am in 24-hour time is...", "After midnight is 00 hours.", []string{"00:15", "12:15", "24:15", "12:15 pm"}, "00:15", "12:15 am is 15 minutes after midnight.")},
			cq("A film starts at 10:45 pm and lasts 1 h 40 min. When does it end?", "Cross midnight carefully.", []string{"12:25 am", "12:25 pm", "11:85 pm", "1:25 am"}, "12:25 am", "10:45 pm + 1 h = 11:45 pm. Add 40 min to cross midnight: 12:25 am."))
	case strings.Contains(code, "AREA-PERIMETER"):
		return mergePack(base, "Area covers inside; perimeter goes around outside.", "The biggest win is knowing which measure the story asks for before using a formula.", "area",
			[]string{"Area counts square units inside a shape.", "Perimeter adds the outside boundary.", "Composite figures can be split into rectangles or traced around the edge."},
			[]string{"Ask: inside or outside?", "Draw or split the shape.", "Use area = length × breadth for rectangles.", "For perimeter, add only outside sides.", "Write square units for area."},
			[]gin.H{ex("Area", "A 6 cm by 4 cm rectangle has area 6 × 4 = 24 cm²."), ex("Perimeter", "The same rectangle has perimeter 6 + 4 + 6 + 4 = 20 cm.")},
			[]gin.H{mis("Using cm² for perimeter", "Perimeter is length around the edge, so use cm, not cm²."), mis("Adding inside lines for perimeter", "Only outside boundary counts.")},
			[]gin.H{cq("A rectangle is 8 cm by 3 cm. Area = ?", "Area covers inside.", []string{"11 cm²", "22 cm²", "24 cm²", "24 cm"}, "24 cm²", "Area = 8 × 3 = 24 cm²."), cq("Which asks for perimeter?", "Think around the outside.", []string{"Tiles covering a floor", "Fence around a garden", "Paint covering a wall", "Paper covering a book"}, "Fence around a garden", "A fence goes around the outside, so it asks for perimeter.")},
			[]gin.H{cq("A square has side 5 cm. Perimeter = ?", "Walk around 4 equal sides.", []string{"10 cm", "20 cm", "25 cm²", "25 cm"}, "20 cm", "5 + 5 + 5 + 5 = 20 cm."), cq("A 7 by 6 rectangle. Area = ?", "Multiply length by breadth.", []string{"13", "26", "42", "49"}, "42", "Area = 7 × 6 = 42 square units.")},
			cq("A 10 cm by 6 cm rectangle has a 3 cm by 2 cm corner cut out. What is the area left?", "Split or subtract areas.", []string{"54 cm²", "56 cm²", "60 cm²", "66 cm²"}, "54 cm²", "Full rectangle area is 10 × 6 = 60 cm². Cut-out area is 3 × 2 = 6 cm². 60 - 6 = 54 cm²."))
	case strings.Contains(code, "GEOMETRY"):
		return mergePack(base, "Geometry is about properties you can rely on.", "Do not just name the shape or line. Ask what is always true about it.", "generic",
			[]string{"A right angle is 90°.", "Parallel lines stay the same distance apart.", "Perpendicular lines meet at right angles.", "Shape properties let you reason even when a drawing is not exact."},
			[]string{"Name the object.", "List the properties.", "Use the property to decide or calculate.", "Check whether the drawing is exact or only a sketch."},
			[]gin.H{ex("Perpendicular lines", "Two lines are perpendicular if they meet at a right angle."), ex("Rectangle", "A rectangle has 4 right angles and opposite sides equal.")},
			[]gin.H{mis("Trusting a rough drawing", "Use the stated properties, not just how the picture looks."), mis("Parallel vs perpendicular", "Parallel never meet; perpendicular meet at 90°.")},
			[]gin.H{cq("Perpendicular lines meet at...", "Use the definition.", []string{"45°", "60°", "90°", "180°"}, "90°", "Perpendicular lines form a right angle."), cq("A rectangle must have...", "Use properties.", []string{"3 sides", "4 right angles", "all sides curved", "no parallel sides"}, "4 right angles", "Every rectangle has four right angles.")},
			[]gin.H{cq("Which pair of lines will never meet?", "Use the line property.", []string{"Perpendicular", "Parallel", "Curved", "Intersecting"}, "Parallel", "Parallel lines stay the same distance apart and never meet."), cq("An angle smaller than a right angle is...", "Right angle is 90°.", []string{"acute", "obtuse", "straight", "reflex"}, "acute", "An acute angle is smaller than 90°.")},
			cq("A path turns right, then turns right again. What total turn has it made?", "Two right angles.", []string{"45°", "90°", "180°", "360°"}, "180°", "One right turn is 90°. Two right turns make 180°, a half-turn."))
	case strings.Contains(code, "STATISTICS"):
		return mergePack(base, "Graphs turn data into something we can read and question.", "The graph is only useful if we read the title, labels, and scale before answering.", "generic",
			[]string{"A scale tells what each step means.", "Comparisons usually need subtraction.", "Totals need addition.", "The same data can look different with a different scale."},
			[]string{"Read the title.", "Read the axis labels.", "Check the scale.", "Find exact values.", "Answer with comparison, total, or trend."},
			[]gin.H{ex("Scale", "If 1 step means 5 pupils, a bar at 6 steps means 30 pupils."), ex("Difference", "If Class A has 28 books and Class B has 19, A has 9 more.")},
			[]gin.H{mis("Counting grid lines as 1", "Always check the scale first."), mis("Answering without units", "Say what the number measures: pupils, books, votes, etc.")},
			[]gin.H{cq("Scale: 1 step = 5. A bar reaches 6 steps. Value = ?", "Multiply scale by steps.", []string{"6", "11", "30", "60"}, "30", "6 × 5 = 30."), cq("Class A has 28 books. Class B has 19. How many more?", "Difference means subtract.", []string{"7", "9", "37", "47"}, "9", "28 - 19 = 9.")},
			[]gin.H{cq("A line graph goes from 12 to 20. Increase = ?", "Find the difference.", []string{"6", "8", "12", "32"}, "8", "20 - 12 = 8."), cq("What should you read before answering a graph question?", "Avoid scale mistakes.", []string{"Only the colours", "Title, labels, and scale", "Only the last bar", "Only the biggest number"}, "Title, labels, and scale", "Those tell what the graph means and how values are counted.")},
			cq("A graph scale is 1 square = 4 pupils. One bar is halfway between 6 and 7 squares. What value is shown?", "Use the scale carefully.", []string{"24", "26", "28", "30"}, "26", "6.5 squares × 4 pupils = 26 pupils."))
	default:
		return base
	}
}

func mergePack(base gin.H, big, plain, visual string, why, steps []string, examples, misconceptions, guided, mini []gin.H, challenge gin.H) gin.H {
	mini = curriculumDistinctMiniQuestions(visual, guided, mini)
	review := curriculumReviewTab(big, steps, misconceptions)
	parentNotes := curriculumParentCoachNotes(visual, base["parentCoachNotes"])
	base["bigIdea"] = big
	base["plain"] = plain
	base["conceptModel"] = gin.H{"visualKind": visual, "title": big, "description": plain}
	base["whyItWorks"] = why
	base["methodSteps"] = steps
	base["workedExamples"] = examples
	base["commonMisconceptions"] = misconceptions
	base["guidedPractice"] = guided
	base["miniTest"] = mini
	base["challenge"] = []gin.H{challenge}
	base["review"] = []string{"Explain the model in one sentence.", "Do 3 guided questions slowly.", "Do 1 word problem and say why the operation fits."}
	base["parentCoachNotes"] = parentNotes
	base["tabs"] = gin.H{
		"bigIdea":        gin.H{"title": big, "plain": plain, "whyItWorks": why},
		"visualModel":    gin.H{"visualKind": visual, "title": big, "description": plain},
		"workedExamples": examples,
		"practice":       guided,
		"miniTest":       mini,
		"challenge":      []gin.H{challenge},
		"parentNotes":    parentNotes,
		"review":         review,
	}
	return base
}

func curriculumParentCoachNotes(visual string, fallback any) []string {
	if notes, ok := fallback.([]string); ok && visual == "generic" {
		return notes
	}
	notes := map[string][]string{
		"place":         {"先问：这个数字里的每一位各代表多少？让她把数读成 expanded form，而不只是念出来。", "追问：如果把 0 拿掉，数值为什么会变？用 place value 解释，不用只说规则。"},
		"addSub":        {"先问：这一步在处理 ones、tens 还是 hundreds？不要只说“借一位”或“进一位”。", "让她把 72 说成 6 tens + 12 ones，解释 trade 后总数为什么没变；最后用反向运算检查。"},
		"factorRainbow": {"先问故事是“一个总数能否平均分组”，还是“两个循环什么时候再碰面”？前者是 factors，后者是 multiples。", "让她用 24 讲一个 factor pair；再用 6 和 8 的跳格讲 common multiple。", "故事题辅导画图：先让她画出袋子或标记，再列数、圈公共数；追问“答案应该比故事里的数大还是小？”（公因数不会比较小的总数大，公倍数不会比较大的数小）。"},
		"algorithm":     {"不要先看竖式结果。让她先说每一行代表什么，例如 326×20 不是 326×2。", "除法卡住时问：这一步在分 hundreds、tens 还是 ones？最后用乘法把答案还原检查。"},
		"division":      {"先问：这题是 sharing equally，还是 making groups？同一条除法式里，答案的意思可能不同。", "余数题固定追问：余数比 divisor 小吗？用 divisor × quotient + remainder 能还原总数吗？"},
		"fractionBars":  {"先问：这个题里的 whole 是什么？whole 不同，分数不能直接比较。", "让她选择模型：分数本身用 bar 或 number line；“of a set”先找一份，再取几份。"},
		"decimal":       {"每次 money 题最后只问三件事：单位是什么？小数点在哪里？是否有两位 cents？", "可以让她先估大约几 dollars，再算精确值；估算不对就先检查 decimal point。"},
		"areaComposite": {"让她先用手指比划：题目是 cover inside 还是 walk outside？不要一开始就说公式。", "复合图形周长先找缺失的 outside sides，再沿外边走一圈；内部切线不能加进去。"},
		"angle":         {"先问这题的 job：name、measure、draw、turn 还是 property？不同 job 需要不同证据。", "量角器题请她指出 vertex 和从哪条 0° 线起读；指南针题请她写 jumps，不数标签。"},
		"symmetry":      {"不要让她凭眼睛补图。先标 corners、endpoints、dots，再数到 mirror line 的格数。", "最后做两次检查：先看 outline，再看里面的小点、颜色或图案。"},
		"nets":          {"先问图片是什么 view：flat net、folded solid 还是 2D drawing。", "让她选一个 base，把邻面当作 hinge 折起来；检查每个 face slot 只被占一次。"},
		"statistics":    {"读图前固定问：这题要 exact value、compare、total 还是 trend？然后才看数字。", "要求完整 evidence sentence：从哪个值到哪个值，变化多少，单位是什么。"},
		"time":          {"先写 start、finish、duration 中已知的两项，再用 timeline 跳到容易的整点。", "跨 noon 或 midnight 时，让她先说 12 pm 是 noon、12 am 是 midnight，再写最终格式。"},
		"p5OrderOps":    {"先让她把 brackets、multiply/divide、add/subtract 分层标出来，再计算；不要从左到右乱算。", "大数题每一步都追问 place value：乘以 1000 是数字移动，还是 value 变成 1000 倍？"},
		"p5PercentRate": {"百分比先问：100% 是哪个 whole？再用 10%、1%、25%、50% 这些友好基准推。", "rate 题先写单位，例如 km/h 或 dollars per item；没有单位的数字很容易算对但理解错。"},
		"p5Ratio":       {"ratio 题先问：每一份代表多少？如果总量变了，份数关系有没有变？", "比较三个量时让她画三条 bar，先对齐份数，再找真实数量。"},
		"p5AreaVolume":  {"三角形面积不要先背公式，先从同底同高 rectangle 的一半推出来。", "volume 先让她数 unit cubes：一层多少个，有几层，再过渡到 length × breadth × height。"},
		"p5Geometry":    {"角度题先找规则来源：straight line、point、opposite angles、triangle sum，不能只猜。", "triangle/quadrilateral 题要求她写出证据，例如 isosceles equal sides imply equal base angles。"},
		"p5Average":     {"average 先用“重新平均分”的图像理解，不要一开始只背 total ÷ number。", "追问三件事：total 是多少？有几份？如果知道 average，怎样反推 total？"},
	}
	if result, ok := notes[visual]; ok {
		return result
	}
	if notes, ok := fallback.([]string); ok {
		return notes
	}
	return []string{"先让她说出模型或图形表示什么，再计算。", "一次只追问一个关键理由，不要求她同时修正所有错误。"}
}

// Keep the independent check genuinely independent when a topic originally
// carried a familiar practice question into its mini test.
func curriculumDistinctMiniQuestions(visual string, guided, mini []gin.H) []gin.H {
	seen := make(map[string]struct{}, len(guided))
	for _, q := range guided {
		prompt, _ := q["prompt"].(string)
		seen[prompt] = struct{}{}
	}
	for i, q := range mini {
		prompt, _ := q["prompt"].(string)
		if _, duplicate := seen[prompt]; !duplicate {
			continue
		}
		if visual == "angle" && prompt == "In ∠ABC, which point is the vertex?" {
			mini[i] = cq(
				"Which angle notation has Q as its vertex?",
				"The middle letter names the corner.",
				[]string{"∠PQR", "∠QPR", "∠PRQ", "∠RQP"},
				"∠PQR",
				"In ∠PQR, Q is the middle letter, so Q is the vertex.",
			)
		}
	}
	return mini
}

func curriculumReviewTab(big string, steps []string, misconceptions []gin.H) gin.H {
	checks := make([]gin.H, 0, len(misconceptions))
	for _, m := range misconceptions {
		mistake, _ := m["mistake"].(string)
		fix, _ := m["fix"].(string)
		if mistake == "" || fix == "" {
			continue
		}
		checks = append(checks, gin.H{
			"mistake": mistake,
			"check":   "Can she explain why this is wrong before calculating again?",
			"fix":     fix,
		})
	}
	if len(checks) == 0 {
		checks = []gin.H{
			{"mistake": "Answer without a model", "check": "Can she draw or describe the model first?", "fix": "Return to the visual model before doing the algorithm."},
			{"mistake": "Answer without units", "check": "Can she say what the number measures?", "fix": "Write the unit and the reason with the answer."},
			{"mistake": "Guessing the operation", "check": "Can she name the story type?", "fix": "Identify part-whole, comparison, equal groups, measurement, or data first."},
		}
	}
	if len(checks) > 3 {
		checks = checks[:3]
	}

	firstMove := "Explain the visual model in one sentence."
	if len(steps) > 0 {
		firstMove = steps[0]
	}
	return gin.H{
		"misconceptionChecks": checks,
		"masteryRubric": []gin.H{
			{"domain": "Concept", "ready": "Can explain the big idea in her own words.", "needsPractice": "Can do a familiar procedure but cannot explain why.", "nextMove": firstMove},
			{"domain": "Accuracy", "ready": "Gets most guided and mini-test questions correct.", "needsPractice": "Repeats the same mistake after seeing feedback.", "nextMove": "Redo only the missed type, then explain the correction."},
			{"domain": "Strategy", "ready": "Chooses a model or method before calculating.", "needsPractice": "Starts calculating without deciding what the problem is asking.", "nextMove": "Ask: What do we know? What are we finding? Which model fits?"},
			{"domain": "Transfer", "ready": "Can solve a word problem or challenge using the same idea.", "needsPractice": "Can answer direct questions but gets stuck when the wording changes.", "nextMove": "Use one new story context with the same numbers or same structure."},
		},
		"reviewPrompts": []gin.H{
			{"label": "Say the idea", "prompt": big, "lookFor": "A child-friendly explanation, not memorised wording."},
			{"label": "Find the mistake", "prompt": "Which mistake did you almost make today?", "lookFor": "She can name a specific misconception."},
			{"label": "Next focus", "prompt": "Which one thing should we practise next time?", "lookFor": "Pick concept, accuracy, strategy, or transfer."},
		},
	}
}

func buildP3AdditionSubtractionPack(base gin.H) gin.H {
	return mergePack(base,
		"Addition combines place-value parts; subtraction removes place-value parts without changing the total value.",
		"A written column is a place-value chart turned sideways. Add or subtract ones with ones, tens with tens, and hundreds with hundreds. Regrouping is a value trade, not a little number trick.",
		"addSub",
		[]string{
			"Each digit has a place: thousands, hundreds, tens, or ones.",
			"Addition combines like places before trading ten ones for one ten.",
			"Subtraction removes like places; if there are not enough ones, trade one ten for ten ones.",
			"A trade changes the form but not the value: 72 = 7 tens + 2 ones = 6 tens + 12 ones.",
			"Estimate first to notice an answer with the wrong size or missing digit.",
			"Addition can be checked with subtraction; subtraction can be checked with addition.",
			"In a word problem, decide whether the story is putting together, finding the difference, or taking away.",
		},
		[]string{
			"Line up digits by place value, not by the length of the number.",
			"Estimate with friendly numbers before the exact calculation.",
			"Start in the ones column and say the place name aloud.",
			"For addition, trade every 10 ones for 1 ten, every 10 tens for 1 hundred, and so on.",
			"For subtraction, trade one larger place only when the current column is too small.",
			"Record the trade clearly before subtracting.",
			"Move left one place at a time until every column is finished.",
			"Check an addition answer by subtracting one addend; check subtraction by adding the difference back.",
			"Read the story again and write what the final number means.",
		},
		[]gin.H{
			ex("Place-value addition", "46 + 27 is 4 tens + 6 ones plus 2 tens + 7 ones. Tens make 60 and ones make 13. Trade 13 ones as 1 ten + 3 ones: 60 + 13 = 73."),
			ex("Place-value subtraction", "72 - 38 starts with 2 ones minus 8 ones, which is impossible without a trade. Trade one ten: 72 becomes 6 tens + 12 ones. Then 12-8=4 and 60-30=30, so the answer is 34."),
			ex("Three-digit regrouping", "268 + 157: 8+7=15 ones, trade 1 ten; 6 tens + 5 tens + 1 ten = 12 tens, trade 1 hundred; 2 hundreds + 1 hundred + 1 hundred = 4 hundreds. Answer 425."),
			ex("Zero in a column", "503 - 178 needs more than one trade. Rename 503 as 4 hundreds + 9 tens + 13 ones. Then subtract ones, tens, and hundreds in order."),
			ex("Estimate check", "398 + 207 is about 400 + 200 = 600. An exact answer near 605 makes sense; 6,005 does not."),
			ex("Story decision", "A shop had 684 stickers and sold 257. The story takes away, so subtract. The answer tells how many stickers remain, not how many were sold."),
		},
		[]gin.H{
			mis("Lining up by the right edge only", "Numbers must align by place value. 47 and 306 line up as 047 and 306, not by guessing where to start."),
			mis("Carrying without a value", "The carried 1 in 46+27 means one ten, not one one."),
			mis("Borrowing without trading", "In subtraction, rename a ten as ten ones. The total stays equal even though the columns change."),
			mis("Skipping zeros", "A zero still holds a place. In 503, there are zero tens, so regrouping needs careful renaming."),
			mis("Choosing an operation from one keyword", "Use the story: are amounts joining, being removed, or compared?"),
			mis("No inverse check", "Use the opposite operation to see whether the original numbers rebuild."),
		},
		[]gin.H{
			cq("Which alignment is correct for 47 + 306?", "Keep hundreds, tens, and ones together.", []string{"47 under 306 by the right edge", "047 under 306", "470 under 306", "write 47 anywhere"}, "047 under 306", "047 has 0 hundreds, 4 tens, and 7 ones, so its places align with 306."),
			cq("46 + 27: why is 1 written in the tens column?", "Name the trade.", []string{"10 ones become 1 ten", "1 one disappears", "27 becomes 17", "tens become ones"}, "10 ones become 1 ten", "6+7 makes 13 ones. Ten of those ones trade for one ten."),
			cq("72 - 38 starts by renaming 72 as...", "Trade one ten for ten ones.", []string{"7 tens + 2 ones", "6 tens + 12 ones", "5 tens + 22 ones", "8 tens + 2 ones"}, "6 tens + 12 ones", "One ten is traded into ten ones, but 72 keeps the same value."),
			cq("268 + 157 = ?", "Regroup ones then tens.", []string{"315", "415", "425", "515"}, "425", "8+7=15; regroup 1 ten. 6+5+1=12 tens; regroup 1 hundred. 2+1+1=4 hundreds."),
			cq("503 - 178 needs which first idea?", "Do not skip the zero tens.", []string{"rename across the zero", "subtract 8 from 3 without trading", "add the numbers", "ignore the 0"}, "rename across the zero", "503 has no tens available, so a hundred is first traded into tens before one ten can become ones."),
			cq("Which estimate fits 398 + 207?", "Round to friendly hundreds.", []string{"about 60", "about 600", "about 6,000", "about 20,000"}, "about 600", "398 is about 400 and 207 is about 200."),
			cq("A library has 684 books and gives away 257. Which operation fits?", "Tell the story.", []string{"addition", "subtraction", "multiplication", "division"}, "subtraction", "Giving away removes books from the starting total."),
			cq("Which check fits 425 - 157 = 268?", "Use the inverse operation.", []string{"268 + 157 = 425", "425 + 157 = 268", "268 - 157 = 425", "425 ÷ 157 = 268"}, "268 + 157 = 425", "Add the difference back to the subtracted amount to rebuild the starting total."),
		},
		[]gin.H{
			cq("1,246 + 378 = ?", "Regroup by place value.", []string{"1,514", "1,624", "1,714", "1,524"}, "1,624", "6+8=14 ones, then tens: 4+7+1=12 tens, then hundreds: 2+3+1=6 hundreds."),
			cq("1,002 - 468 = ?", "Rename through zeros carefully.", []string{"534", "644", "544", "634"}, "534", "Rename 1 thousand as 9 hundreds, then one hundred as 9 tens, then one ten as 12 ones before subtracting."),
			cq("Which answer is reasonable for 2,891 + 1,104?", "Estimate first.", []string{"about 400", "about 4,000", "about 40,000", "about 1,800"}, "about 4,000", "2,891 is about 2,900 and 1,104 is about 1,100."),
			cq("A game score was 1,250. It increased by 375. New score?", "Join the amounts.", []string{"875", "1,525", "1,625", "1,725"}, "1,625", "An increase means add: 1,250+375=1,625."),
			cq("A tank held 950 L and now holds 386 L. How much water was used?", "Find the amount removed.", []string{"564 L", "1,336 L", "664 L", "574 L"}, "564 L", "950-386=564. The unit L stays with the answer."),
			cq("Why is 600 - 278 not 478?", "Use place-value trade.", []string{"you cannot subtract 8 ones from 0 ones without regrouping", "because subtraction always adds", "because 600 has no zeros", "because 278 is bigger"}, "you cannot subtract 8 ones from 0 ones without regrouping", "Regroup hundreds into tens and then tens into ones before subtracting."),
			cq("Which result checks 1,002 - 468 = 534?", "Rebuild the start.", []string{"534 + 468 = 1,002", "1,002 + 468 = 534", "534 - 468 = 1,002", "468 - 534 = 1,002"}, "534 + 468 = 1,002", "Addition checks subtraction when it rebuilds the starting amount."),
			cq("A child writes 268 + 157 = 315. What is the main problem?", "Check the regrouping.", []string{"they did not regroup 15 ones and 12 tens", "they used too many zeros", "addition should use division", "the numbers cannot be added"}, "they did not regroup 15 ones and 12 tens", "The ones make 15 and the tens then make 12; both trades are needed."),
		},
		cq("Maya says 72-38 is 46 because she does 8-2 and 7-3. How would the place-value trade correct her?", "Use the model, not digit tricks.", []string{"Rename 72 as 6 tens + 12 ones; then 12-8=4 and 60-30=30, so 34", "Swap the digits", "Always subtract the smaller digit from the larger", "Use multiplication"}, "Rename 72 as 6 tens + 12 ones; then 12-8=4 and 60-30=30, so 34", "Subtracting digits without their place values loses the meaning. The trade creates enough ones while preserving 72."))
}

func buildP3MultiplicationDivisionPack(base gin.H) gin.H {
	return mergePack(base,
		"Multiplication and division tell one equal-groups story from different directions.",
		"Multiplication builds equal groups. Division can either share one total equally or ask how many equal groups fit. When a fact is forgotten, turn division into missing multiplication instead of guessing.",
		"division",
		[]string{
			"A multiplication equation has three jobs: number of groups × in each group = total.",
			"Sharing division asks: if the total is shared equally, how many are in each group?",
			"Grouping division asks: if each group has this many, how many full groups can we make?",
			"Both division stories can use the same equation, but the answer means a different thing.",
			"42 ÷ 6 is a missing multiplication fact: 6 × ? = 42.",
			"A remainder is a real leftover after making every possible full group.",
			"A remainder must be smaller than the divisor, or one more full group is possible.",
			"Division can be checked by rebuilding the total: divisor × quotient + remainder.",
		},
		[]string{
			"Name the total, group size, and number of groups in the story.",
			"Ask whether the story is sharing equally or making groups.",
			"Draw counters or a bar split into equal groups when the meaning is unclear.",
			"Write the matching multiplication equation before calculating.",
			"For division, turn it into divisor × ? = total.",
			"Build as many full groups as possible.",
			"Write any leftover as a remainder and compare it with the divisor.",
			"Read the question again: does it want groups, amount in each group, or a rounded-up real-world answer?",
			"Check with divisor × quotient + remainder.",
			"Say one sentence about what the quotient and remainder mean in this story.",
		},
		[]gin.H{
			ex("Equal groups", "There are 4 bags with 6 marbles each. 4 groups × 6 in each group = 24 marbles. This is 4 × 6 = 24."),
			ex("Sharing", "24 marbles are shared by 6 children. Draw 6 equal spaces and share one marble at a time. Each child gets 4, so 24 ÷ 6 = 4."),
			ex("Grouping", "24 marbles go into bags of 6. Make groups of 6: 6, 12, 18, 24. There are 4 bags, so 24 ÷ 6 = 4."),
			ex("Missing multiplication", "For 56 ÷ 8, ask 8 × ? = 56. Since 8 × 7 = 56, the quotient is 7."),
			ex("Remainder", "38 cookies go into boxes of 5. Seven full boxes use 35 cookies, with 3 left: 38 ÷ 5 = 7 r 3."),
			ex("Remainder check", "Check 38 ÷ 5 = 7 r 3: 5 × 7 + 3 = 38. Also 3 is smaller than 5, so the remainder is possible."),
			ex("Real-life decision", "38 children travel in vans that hold 5. 7 r 3 is the division result, but 8 vans are needed because the last 3 children still need a van."),
			ex("Place-value division", "84 ÷ 3 can be shared as 60 ÷ 3 = 20 and 24 ÷ 3 = 8. Together, 84 ÷ 3 = 28. The parts still rebuild 84."),
		},
		[]gin.H{
			mis("Using a keyword only", "Words like each or groups do not choose the operation alone. Say what the total, group size, and unknown are."),
			mis("Mixing up sharing and grouping", "Both can be written 24 ÷ 6 = 4, but sharing asks 4 in each group while grouping asks 4 groups."),
			mis("Remainder equal to the divisor", "38 ÷ 5 cannot be 6 r 8 because 8 is enough to make another full group."),
			mis("Stopping after the quotient", "A word problem may need the remainder interpreted: leftovers, full groups only, or one more container."),
			mis("Forgetting the inverse check", "Multiply divisor by quotient and add the remainder to see if you rebuild the original total."),
			mis("Treating long division as symbols only", "At every step, say whether you are sharing hundreds, tens, or ones."),
		},
		[]gin.H{
			cq("5 trays have 7 cupcakes each. Which equation tells the story?", "Name groups, in each group, total.", []string{"5 + 7 = 12", "5 × 7 = 35", "35 ÷ 5 = 7 only", "7 - 5 = 2"}, "5 × 7 = 35", "There are 5 equal groups of 7, so the total is 35."),
			cq("24 stickers are shared equally by 6 children. What does the answer 4 mean?", "Sharing story.", []string{"4 children", "4 stickers each", "4 stickers left", "4 boxes"}, "4 stickers each", "The total is being shared among 6 children, so 4 is the amount each child receives."),
			cq("24 stickers go into packs of 6. What does the answer 4 mean?", "Grouping story.", []string{"4 stickers each", "4 packs", "4 stickers left", "4 children"}, "4 packs", "Each pack holds 6, so 24 makes 4 full packs."),
			cq("42 ÷ 7 = ?", "Turn it into missing multiplication.", []string{"5", "6", "7", "49"}, "6", "Ask 7 × ? = 42. The missing factor is 6."),
			cq("Which remainder is possible for division by 6?", "Remainders are smaller than the divisor.", []string{"r 6", "r 8", "r 5", "r 12"}, "r 5", "The only remainder smaller than 6 is 5."),
			cq("35 ÷ 4 = ?", "Make full groups of 4.", []string{"8 r 3", "7 r 7", "9 r 1", "8 r 4"}, "8 r 3", "4 × 8 = 32, with 3 left. The remainder 3 is smaller than 4."),
			cq("Which check proves 53 ÷ 7 = 7 r 4?", "Rebuild the total.", []string{"7 + 7 + 4 = 18", "7 × 7 + 4 = 53", "53 - 7 = 46", "4 × 7 = 28"}, "7 × 7 + 4 = 53", "Divisor × quotient + remainder must equal the original total."),
			cq("84 ÷ 3 can be split as...", "Share place-value parts.", []string{"60 ÷ 3 + 24 ÷ 3", "80 ÷ 3 + 4", "84 ÷ 30", "3 ÷ 84"}, "60 ÷ 3 + 24 ÷ 3", "60 and 24 are both divisible by 3, giving 20 + 8 = 28."),
		},
		[]gin.H{
			cq("A farmer puts 54 eggs into cartons of 6. How many cartons?", "Grouping: cartons of 6.", []string{"8", "9", "48", "60"}, "9", "54 ÷ 6 = 9 because 6 × 9 = 54."),
			cq("54 eggs are shared equally into 6 baskets. How many eggs in each basket?", "Sharing: 6 equal baskets.", []string{"6", "8", "9", "48"}, "9", "54 ÷ 6 = 9, but here 9 means eggs in each basket."),
			cq("Which equation matches 8 groups of 9?", "Build the total.", []string{"8 × 9 = 72", "72 ÷ 8 = 9 only", "8 + 9 = 17", "72 - 9 = 63"}, "8 × 9 = 72", "Eight equal groups of nine make 72."),
			cq("67 ÷ 8 = ?", "Use a nearby multiplication fact.", []string{"8 r 3", "7 r 11", "9 r 1", "8 r 8"}, "8 r 3", "8 × 8 = 64, leaving 3. The remainder is smaller than 8."),
			cq("A bus holds 8 pupils. 67 pupils need buses. How many buses are needed?", "Interpret the remainder.", []string{"8", "8 r 3", "9", "11"}, "9", "Eight buses hold 64 pupils, so 3 pupils still need one more bus."),
			cq("Which statement is true about 47 ÷ 5 = 9 r 2?", "Read quotient and remainder.", []string{"5 × 9 + 2 = 47", "5 × 2 + 9 = 47", "the remainder is bigger than the divisor", "there are no leftovers"}, "5 × 9 + 2 = 47", "The inverse check rebuilds 47, and 2 is a valid remainder."),
			cq("96 ÷ 4 = ?", "Use known facts or place-value sharing.", []string{"22", "23", "24", "26"}, "24", "4 × 24 = 96. You can share 80 and 16: 20 + 4 = 24."),
			cq("A child writes 29 ÷ 4 = 6 r 5. What should you say?", "Check the remainder.", []string{"Correct", "Make one more group: it should be 7 r 1", "Change divisor to 5", "Ignore the remainder"}, "Make one more group: it should be 7 r 1", "A remainder of 5 is too large for groups of 4; four of those leftovers make another group."),
		},
		cq("A school has 94 pencils. They can be packed in boxes of 8 or shared equally among 8 tables. What stays the same and what changes?", "Compare two division meanings.", []string{"Both give 11 r 6; the quotient means boxes in one story and pencils per table in the other", "They need different equations", "The remainder disappears", "The answer must always round up"}, "Both give 11 r 6; the quotient means boxes in one story and pencils per table in the other", "The equation is the same, but the story changes what the quotient and remainder describe. Boxes need 12 boxes; equal sharing gives 11 pencils per table with 6 left."))
}

func buildP5TopicLessonPack(code string, base gin.H) (gin.H, bool) {
	switch code {
	case "P5-WHOLE-OPERATIONS":
		return mergePack(base,
			"Large numbers and order of operations are about structure before calculation.",
			"P5 numbers extend place value to millions, then asks the child to control multi-step calculations. The important habit is to mark the structure first: place value, operation priority, brackets, and a reasonableness check.",
			"p5OrderOps",
			[]string{"Each place is ten times the place to its right, even in the millions.", "Multiplying by 10, 100, or 1000 changes value by place shifts, not by adding zeros blindly.", "Dividing by 10, 100, or 1000 moves value to smaller places.", "Brackets say which group must be calculated first.", "Multiplication and division have the same priority and are read left to right.", "Addition and subtraction have the same priority and are read left to right.", "Word problems need a bar model or equation plan before calculation."},
			[]string{"Read the number by place-value groups.", "For x10/100/1000 or divide by 10/100/1000, say which place each digit moves to.", "Circle brackets first.", "Mark multiplication/division before addition/subtraction.", "Work left to right inside the same priority level.", "Estimate before exact calculation.", "For word problems, write the story equation before evaluating.", "Check whether the final size fits the story."},
			[]gin.H{ex("Ten million place value", "4,608,215 means 4 millions, 6 hundred-thousands, 8 thousands, 2 hundreds, 1 ten and 5 ones. The zero holds the ten-thousands place."), ex("Multiply by 1000", "4,236 x 1000 = 4,236,000 because each digit becomes 1000 times its old value."), ex("Divide by 100", "82,000 divided by 100 = 820 because each group of 100 is counted as 1."), ex("Order of operations", "6 + 4 x 8 = 38, not 80, because multiplication is done before addition."), ex("Brackets", "(6 + 4) x 8 = 80 because brackets change the structure."), ex("Word problem plan", "If 5 clubs each buy 24 balls and later share 30 extra balls, write 5 x 24 + 30 before calculating.")},
			[]gin.H{mis("Adding zeros blindly", "Zeros work only for whole-number x10/100/1000 in this context. Say the place movement so the rule has meaning."), mis("Doing operations from left to right always", "Only operations with the same priority go left to right. Multiplication/division happen before addition/subtraction."), mis("Ignoring brackets", "Brackets are a command to group first."), mis("Treating a word problem as one keyword", "Multi-step problems need a story plan, not a keyword hunt."), mis("Skipping estimation", "P5 numbers are large enough that a misplaced place can create an impossible answer.")},
			[]gin.H{cq("In 6,430,218, the 4 means...", "Name the place.", []string{"4", "40", "40,000", "400,000"}, "400,000", "The 4 is in the hundred-thousands place."), cq("7,305 x 100 = ?", "Move each digit two places left in value.", []string{"7305", "73,050", "730,500", "7,305,000"}, "730,500", "Multiplying by 100 makes the value 100 times as large."), cq("560,000 divided by 1000 = ?", "Count groups of 1000.", []string{"56", "560", "5,600", "56,000"}, "560", "560,000 contains 560 groups of 1000."), cq("8 + 6 x 5 = ?", "Multiplication before addition.", []string{"38", "70", "48", "30"}, "38", "6x5=30, then 8+30=38."), cq("(8 + 6) x 5 = ?", "Brackets first.", []string{"38", "70", "48", "30"}, "70", "8+6=14, then 14x5=70."), cq("Which plan fits: 12 packs with 25 cards each, then 40 cards given away?", "Write story equation.", []string{"12+25-40", "12x25-40", "12x40-25", "25-12+40"}, "12x25-40", "12 equal packs means multiply, then given away means subtract.")},
			[]gin.H{cq("3,800,000 divided by 100 = ?", "Shift value to smaller places.", []string{"38,000", "380,000", "3,800", "38"}, "38,000", "3,800,000 has 38,000 hundreds."), cq("45 - 15 divided by 3 = ?", "Division before subtraction.", []string{"10", "40", "35", "15"}, "40", "15 divided by 3 is 5, then 45-5=40."), cq("Which expression gives 96?", "Check structure.", []string{"12 + 6 x 14", "(12 + 6) x 4", "12 x 6 + 24", "12 + 6 + 14"}, "12 x 6 + 24", "12x6=72, and 72+24=96."), cq("A school orders 18 boxes with 125 books in each. About how many books?", "Estimate first.", []string{"about 200", "about 2,000", "about 20,000", "about 200,000"}, "about 2,000", "18 is about 20 and 125 is about 100, so about 2000."), cq("Which step should come first for 7 x (36 - 18) + 5?", "Brackets first.", []string{"36 - 18", "7 x 36", "18 + 5", "7 + 5"}, "36 - 18", "Brackets must be evaluated before multiplication.")},
			cq("A child gets 1,250,000 for 1,250 x 100 and says, 'I added two zeros.' What is the better explanation?", "Use place value, not a trick.", []string{"Each digit became 100 times its value", "The answer is larger because zeros are lucky", "Only the last digit changed", "Multiplication changed into addition"}, "Each digit became 100 times its value", "The rule works because multiplying by 100 moves every digit two places left in value.")), true
	case "P5-FRACTIONS":
		return mergePack(base,
			"Fractions are numbers, divisions, operators, and parts of stories.",
			"P5 fractions connect several meanings: 3 divided by 4 is 3/4, a mixed number is whole plus part, and multiplying by a fraction means taking a fraction of something.",
			"fractionBars",
			[]string{"A fraction can be a division statement: 3 divided by 4 = 3/4.", "Mixed numbers and improper fractions are two names for the same point on a number line.", "Adding or subtracting mixed numbers needs same-sized fractional parts.", "Multiplying by a proper fraction makes the amount smaller because you take part of it.", "Fraction word problems should identify the whole before calculating.", "A bar model helps decide whether to add, subtract, multiply, or compare."},
			[]string{"Name the whole.", "Choose bar, number line, or set model.", "Convert mixed numbers only when it makes the operation clearer.", "For addition/subtraction, make common denominators.", "For multiplication by a fraction, find the unit fraction first.", "Estimate whether the result should be bigger or smaller.", "Check the final form and unit."},
			[]gin.H{ex("Fraction as division", "3 cakes shared by 4 children gives each child 3/4 of a cake, so 3 divided by 4 = 3/4."), ex("Mixed addition", "2 1/3 + 1 2/3 = 4 because 1/3 + 2/3 = 1 whole."), ex("Mixed subtraction", "5 1/4 - 2 3/4 can become 4 5/4 - 2 3/4 = 2 2/4 = 2 1/2."), ex("Fraction of a whole number", "3/5 of 40 means split 40 into 5 equal parts, then take 3 parts: 8x3=24."), ex("Fraction times fraction", "1/2 of 3/4 is 3/8 because half of three quarters covers three eighths."), ex("Word problem model", "If 2/5 of a class are girls, the whole is the class, not only the girls.")},
			[]gin.H{mis("Forgetting the whole", "A fraction answer is meaningless if we do not know what the whole is."), mis("Adding denominators", "1/3 + 1/3 = 2/3, not 2/6, because the part size stays thirds."), mis("Multiplying fractions as a size increase", "A proper fraction of an amount is smaller than the original amount."), mis("Converting too early", "Use a model first; convert only when it helps the operation."), mis("Losing the unit", "A fraction of metres, dollars, or pupils must keep the unit in the answer.")},
			[]gin.H{cq("3 divided by 5 can be written as...", "Fraction as division.", []string{"5/3", "3/5", "8/5", "15"}, "3/5", "The dividend becomes the numerator and the divisor becomes the denominator."), cq("2 3/4 as an improper fraction is...", "2 wholes are 8 quarters.", []string{"5/4", "8/4", "11/4", "23/4"}, "11/4", "8/4 + 3/4 = 11/4."), cq("1 2/5 + 2 1/5 = ?", "Add wholes and fifths.", []string{"3 3/5", "3 2/10", "4 3/5", "2 3/5"}, "3 3/5", "1+2=3 and 2/5+1/5=3/5."), cq("3/4 of 28 = ?", "Find one quarter first.", []string{"7", "21", "24", "31"}, "21", "28 divided by 4 is 7, then 7x3=21."), cq("1/2 x 2/3 = ?", "Half of two thirds.", []string{"1/3", "2/5", "2/6", "3/4"}, "1/3", "Half of 2/3 is 1/3."), cq("Before solving a fraction word problem, first identify...", "Do not lose the whole.", []string{"the whole", "the prettiest number", "only the numerator", "the page number"}, "the whole", "The whole tells what the fraction is part of.")},
			[]gin.H{cq("5 1/6 - 2 5/6 = ?", "Regroup one whole as sixths.", []string{"2 2/6", "2 1/3", "3 4/6", "3 2/6"}, "2 1/3", "5 1/6 = 4 7/6. 4 7/6 - 2 5/6 = 2 2/6 = 2 1/3."), cq("2/3 of 45 = ?", "One third first.", []string{"15", "30", "33", "43"}, "30", "45 divided by 3 is 15, then 15x2=30."), cq("Which result must be smaller than 24?", "Proper fraction of a number.", []string{"24 x 3/4", "24 + 3/4", "24 divided by 3/4", "24 + 3"}, "24 x 3/4", "Taking three quarters of 24 gives part of 24."), cq("A ribbon is 3 1/2 m. Maya uses 1 3/4 m. How much is left?", "Subtract mixed numbers.", []string{"1 1/4 m", "1 3/4 m", "2 1/4 m", "5 1/4 m"}, "1 3/4 m", "3 1/2 = 3 2/4. 3 2/4 - 1 3/4 = 1 3/4."), cq("If 4 children share 3 pizzas equally, each gets...", "Fraction as division.", []string{"3/4 pizza", "4/3 pizza", "1 pizza", "7 pizzas"}, "3/4 pizza", "3 pizzas divided by 4 children = 3/4 pizza each.")},
			cq("A tank is 3/5 full. After 1/4 of the full tank is used, what model should come first?", "Two fractions of the same whole.", []string{"A bar split into twentieths", "Add 3 and 1 only", "Compare denominators only", "Ignore the tank size"}, "A bar split into twentieths", "Both fractions refer to the full tank, so common equal parts make the subtraction meaningful.")), true
	case "P5-DECIMALS":
		return mergePack(base,
			"Decimal operations and measurement conversions are the same place-value idea.",
			"P5 decimals extend P4 decimal place value into multiplying/dividing by powers of ten and converting units. The amount stays the same during conversion; only the unit changes.",
			"decimal",
			[]string{"Multiplying a decimal by 10, 100, or 1000 shifts value to larger places.", "Dividing by 10, 100, or 1000 shifts value to smaller places.", "Conversion works inside the same measurement family: km/m/cm, kg/g, l/ml.", "A smaller unit usually gives a bigger number because each unit is smaller.", "A larger unit usually gives a smaller number.", "Word problems require unit consistency before calculation."},
			[]string{"Name the unit family.", "Write the conversion bridge, such as 1 kg = 1000 g.", "Decide whether the new unit is smaller or larger.", "Move decimal value by the bridge factor.", "Convert all quantities to one unit before adding or subtracting.", "Estimate to catch decimal-point errors."},
			[]gin.H{ex("Multiply decimal", "4.82 x 100 = 482 because 4.82 becomes 100 times as large."), ex("Divide decimal", "36.5 divided by 10 = 3.65 because each value becomes one tenth as large."), ex("Mass conversion", "3.25 kg = 3250 g because kilograms to grams uses x1000."), ex("Length conversion", "450 cm = 4.5 m because centimetres to metres uses divided by 100."), ex("Mixed units", "2.4 l + 350 ml is easier as 2400 ml + 350 ml = 2750 ml."), ex("Word problem", "If a rope is 3.6 m and 85 cm is cut off, convert 3.6 m to 360 cm first.")},
			[]gin.H{mis("Moving the decimal the wrong direction", "Ask whether the new unit is smaller or larger before moving."), mis("Changing unit families", "kg cannot become m. Conversion must stay inside the same family."), mis("Adding different units", "Convert first; do not add 2.4 l and 350 ml as 352.4."), mis("Ignoring trailing zeros", "3.50 m and 3.5 m are equal, but 3.50 may show centimetre precision."), mis("Forgetting units in final answer", "The number alone does not tell whether it is grams, metres, or litres.")},
			[]gin.H{cq("6.73 x 100 = ?", "Shift value two places larger.", []string{"0.0673", "67.3", "673", "6730"}, "673", "Hundred times as large gives 673."), cq("48.2 divided by 10 = ?", "One tenth as large.", []string{"4.82", "482", "0.482", "48.02"}, "4.82", "Dividing by 10 shifts each digit one place smaller."), cq("2.75 kg = ? g", "1 kg = 1000 g.", []string{"275 g", "2750 g", "27,500 g", "0.275 g"}, "2750 g", "2.75x1000=2750."), cq("430 cm = ? m", "100 cm = 1 m.", []string{"4.3 m", "43 m", "0.43 m", "4300 m"}, "4.3 m", "430 divided by 100 is 4.3."), cq("Which can be added directly?", "Same unit needed.", []string{"2.5 kg + 300 g", "2.5 kg + 0.3 kg", "250 cm + 4 kg", "3 l + 50 cm"}, "2.5 kg + 0.3 kg", "Both quantities are in kilograms."), cq("3.6 m - 85 cm should first become...", "Convert to one unit.", []string{"360 cm - 85 cm", "36 cm - 85 cm", "3.6 cm - 85 cm", "360 m - 85 m"}, "360 cm - 85 cm", "3.6 m is 360 cm.")},
			[]gin.H{cq("0.48 x 1000 = ?", "Three-place value shift.", []string{"4.8", "48", "480", "4800"}, "480", "0.48x1000=480."), cq("7.2 l = ? ml", "1 l = 1000 ml.", []string{"72 ml", "720 ml", "7200 ml", "0.0072 ml"}, "7200 ml", "7.2x1000=7200."), cq("A bottle has 1.5 l. Maya pours out 275 ml. How much remains?", "Use one unit.", []string{"1225 ml", "1275 ml", "1.225 ml", "1775 ml"}, "1225 ml", "1.5 l = 1500 ml. 1500-275=1225 ml."), cq("Which statement is true?", "Unit-size reasoning.", []string{"3.2 kg = 3200 g", "3.2 kg = 320 g", "3.2 m = 3200 kg", "3.2 l = 32 ml"}, "3.2 kg = 3200 g", "kg to g uses x1000."), cq("Why is 0.75 km equal to 750 m?", "Smaller unit gives bigger number.", []string{"metres are smaller units, so there are more of them", "kilometres and grams match", "75 is always 750", "the decimal disappears by luck"}, "metres are smaller units, so there are more of them", "1 km has 1000 m, so 0.75 km has 750 m.")},
			cq("A recipe needs 2.4 l of water. A jug holds 750 ml. Which first step prevents a decimal mistake?", "Convert before comparing.", []string{"Write 2.4 l as 2400 ml", "Write 750 ml as 750 l", "Add 2.4 and 750", "Round both to 1"}, "Write 2.4 l as 2400 ml", "Both amounts must be in the same unit before deciding how many jugs are needed.")), true
	case "P5-PERCENTAGE-RATE":
		return mergePack(base,
			"Percentage and rate both compare quantities against a clear base.",
			"Percentage means out of 100 of the same whole. Rate means one quantity for one unit of another quantity. Both fail when the base or unit is unclear.",
			"p5PercentRate",
			[]string{"100% means the whole amount.", "1% means one hundredth of the whole.", "A percent can be rewritten as a fraction or decimal.", "Percentage of a quantity can use friendly benchmarks such as 10%, 25%, 50%, and 1%.", "Discount and GST change the original amount by a percentage.", "Rate is a per-one-unit comparison, such as dollars per ticket or km per hour.", "Rate word problems need units written beside the number."},
			[]string{"For percentage, identify the whole as 100%.", "Convert percent to fraction or decimal when helpful.", "Use friendly percentages to build harder percentages.", "For discount, subtract the percentage amount from the original.", "For GST or increase, add the percentage amount to the original.", "For rate, write quantity per unit.", "Use rate x number of units = total, or total divided by units = rate."},
			[]gin.H{ex("Percent grid", "37% means 37 out of 100 equal squares."), ex("Fraction and decimal", "25% = 25/100 = 1/4 = 0.25."), ex("Percentage of quantity", "15% of $80 is 10% + 5% = $8 + $4 = $12."), ex("Discount", "A 20% discount on $50 is $10 off, so the sale price is $40."), ex("Rate", "$18 for 3 notebooks is $6 per notebook because 18 divided by 3 is 6."), ex("Speed rate", "120 km in 2 h is 60 km/h because 120 divided by 2 is 60.")},
			[]gin.H{mis("Forgetting 100% whole", "Always ask: 100% of what?"), mis("Treating percent as a unitless trick", "Percent is a fraction of a specific whole."), mis("Adding discount instead of subtracting", "Discount reduces the original price."), mis("Leaving out rate units", "60 alone is incomplete; 60 km/h and $60/item mean different things."), mis("Mixing total and rate", "Rate is per one unit; total is for all units.")},
			[]gin.H{cq("40% means...", "Out of 100.", []string{"40 out of 100", "40 out of 10", "4 out of 1000", "400 whole"}, "40 out of 100", "Percent means per hundred."), cq("25% as a fraction is...", "Simplify 25/100.", []string{"1/2", "1/4", "1/5", "25/10"}, "1/4", "25/100 simplifies to 1/4."), cq("10% of 90 = ?", "One tenth.", []string{"9", "10", "18", "90"}, "9", "10% is one tenth, and 90 divided by 10 is 9."), cq("A $60 bag has 20% discount. Discount amount?", "Find 20% first.", []string{"$6", "$12", "$48", "$80"}, "$12", "10% is $6, so 20% is $12."), cq("$24 for 6 pens is...", "Rate per pen.", []string{"$4 per pen", "$6 per pen", "$24 per pen", "$144 per pen"}, "$4 per pen", "24 divided by 6 is 4."), cq("A car travels 150 km in 3 h. Rate?", "Distance per hour.", []string{"50 km/h", "45 km/h", "153 km/h", "450 km/h"}, "50 km/h", "150 divided by 3 is 50 km/h.")},
			[]gin.H{cq("35% of 200 = ?", "Use 30% + 5%.", []string{"35", "70", "65", "235"}, "70", "30% is 60 and 5% is 10, so 70."), cq("A price is $80 before 8% GST. GST amount?", "8% of original.", []string{"$6.40", "$8.00", "$86.40", "$0.80"}, "$6.40", "1% is $0.80, so 8% is $6.40."), cq("If 5 kg of rice costs $22.50, price per kg?", "Total divided by units.", []string{"$4.50/kg", "$5/kg", "$17.50/kg", "$112.50/kg"}, "$4.50/kg", "22.50 divided by 5 is 4.50."), cq("Which is a rate?", "Look for per unit.", []string{"45 km/h", "45%", "45 kg", "45 triangles"}, "45 km/h", "km per hour compares distance to one hour."), cq("A $50 item is discounted by 10%, then costs...", "Subtract discount.", []string{"$5", "$45", "$55", "$60"}, "$45", "10% of $50 is $5 off, so $50-$5=$45.")},
			cq("A child says 20% discount and 20% GST always cancel. What should she check?", "Percent changes depend on the base.", []string{"whether both percentages use the same base amount", "whether 20 is even", "whether dollars are decimals", "whether the item is heavy"}, "whether both percentages use the same base amount", "Percent changes act on a base. After one change, the next base may be different.")), true
	case "P5-RATIO":
		return mergePack(base,
			"Ratio compares quantities by equal units, not by one total only.",
			"A ratio such as 2:3 means two equal parts of one quantity for three equal parts of another. The actual amount can scale up or down while the relationship stays the same.",
			"p5Ratio",
			[]string{"Ratio compares quantities in the same situation.", "Each ratio part is a unit, not necessarily one object.", "Equivalent ratios multiply or divide all parts by the same number.", "A three-part ratio needs all bars aligned to the same unit size.", "When total is given, add ratio parts first.", "When one part value is given, find the value of one unit first.", "Ratio word problems usually become bar models."},
			[]string{"Write the ratio with labels.", "Draw one bar per quantity.", "Mark the same unit size on every bar.", "If a total is given, add parts to find total units.", "If one amount is given, divide by its ratio units to find one unit.", "Scale every part by the same factor.", "Check that the final quantities keep the same relationship."},
			[]gin.H{ex("Ratio meaning", "The ratio boys:girls = 2:3 means for every 2 equal units of boys, there are 3 equal units of girls."), ex("Equivalent ratio", "2:3 = 4:6 because both parts are multiplied by 2."), ex("Total given", "Red:blue = 2:3 and total is 25. There are 5 units, so 1 unit is 5. Red is 10 and blue is 15."), ex("One amount given", "A:B = 3:5. If A=12, then 1 unit is 4 and B=20."), ex("Three quantities", "A:B:C = 2:3:4 has 9 units in total."), ex("Word problem", "If the ratio changes after adding more to one group, draw before and after bars.")},
			[]gin.H{mis("Adding only the two numbers", "2:3 is a relationship. Add parts only when a total is given."), mis("Scaling one side only", "Equivalent ratios must multiply or divide every part by the same factor."), mis("Forgetting labels", "2:3 means different things if the order changes."), mis("Treating parts as actual counts automatically", "A part may represent 1, 4, 10, or another amount."), mis("Comparing three quantities without aligned units", "All ratio bars must use the same unit size.")},
			[]gin.H{cq("Which is equivalent to 2:5?", "Multiply both parts by the same number.", []string{"4:10", "4:5", "2:10", "7:10"}, "4:10", "2x2:5x2 = 4:10."), cq("Boys:girls = 3:4. Total parts = ?", "Add ratio units.", []string{"1", "3", "4", "7"}, "7", "3+4=7 units."), cq("Red:blue = 2:3. Total 20 counters. Red counters?", "Find one unit.", []string{"8", "10", "12", "15"}, "8", "5 units = 20, so 1 unit=4. Red=2 units=8."), cq("A:B = 5:2. If B=8, A=?", "Find one unit from B.", []string{"10", "16", "20", "40"}, "20", "2 units=8, so 1 unit=4 and A=5 units=20."), cq("Which ratio has the same relationship as 6:9?", "Simplify or scale.", []string{"2:3", "3:2", "6:3", "12:9"}, "2:3", "Divide both parts by 3."), cq("Why label a ratio?", "Order matters.", []string{"2:3 can reverse meaning if labels swap", "labels make it longer only", "labels change the arithmetic", "labels are never used"}, "2:3 can reverse meaning if labels swap", "Boys:girls = 2:3 is not the same as girls:boys = 2:3.")},
			[]gin.H{cq("A:B:C = 2:3:5. Total is 60. C = ?", "Total units first.", []string{"10", "12", "30", "50"}, "30", "10 units=60, so 1 unit=6 and C=5 units=30."), cq("The ratio of cats to dogs is 4:7. There are 28 dogs. Cats?", "Use dogs to find one unit.", []string{"12", "16", "20", "49"}, "16", "7 units=28, so 1 unit=4 and cats=4 units=16."), cq("Which pair keeps ratio 3:8?", "Scale both parts.", []string{"9 and 24", "6 and 8", "3 and 11", "12 and 24"}, "9 and 24", "3x3=9 and 8x3=24."), cq("If orange:apple = 2:5, which statement is true?", "Read parts.", []string{"apple has more parts", "orange has more parts", "both are equal", "there are 10 orange parts"}, "apple has more parts", "5 parts is more than 2 parts."), cq("A ratio problem gives the difference between two quantities. What model helps?", "Difference is gap between bars.", []string{"bar model with aligned units", "only a pie chart", "only a protractor", "ignore the labels"}, "bar model with aligned units", "The bar lengths show the gap in ratio units.")},
			cq("A:B = 2:5. B has 18 more than A. What does the difference represent?", "Use bars to read the gap.", []string{"3 ratio units", "7 ratio units", "2 ratio units", "5 ratio units"}, "3 ratio units", "B has 5 units and A has 2 units, so the difference is 3 units.")), true
	case "P5-AREA-VOLUME":
		return mergePack(base,
			"Area and volume formulas come from counting: surface squares and unit cubes.",
			"P5 introduces triangle area and cuboid volume. The formulas should come from pictures: a triangle is half a related rectangle, and a cuboid is layers of unit cubes.",
			"p5AreaVolume",
			[]string{"A triangle with the same base and height as a rectangle has half the rectangle's area.", "Base and height must be perpendicular.", "Composite area can be split into familiar shapes.", "Volume counts unit cubes filling a solid.", "A cuboid volume is area of one layer times number of layers.", "Liquid volume in a rectangular tank uses the same cuboid structure.", "Units matter: area uses square units; volume uses cubic units."},
			[]string{"Decide whether the question asks area, perimeter, or volume.", "For triangle area, identify perpendicular base and height.", "Use rectangle area first, then halve for triangle.", "For composite area, split into rectangles/triangles or subtract missing parts.", "For volume, count cubes in one layer.", "Multiply by number of layers.", "Write square or cubic units correctly."},
			[]gin.H{ex("Triangle from rectangle", "A rectangle 10 cm by 6 cm has area 60 cm2. A triangle with the same base and height has half: 30 cm2."), ex("Base-height pair", "A slanted side is not automatically the height. Height must meet the base at a right angle."), ex("Composite triangle shape", "Split the shape into a rectangle and a triangle, find each area, then add."), ex("Unit cubes", "A cuboid 4 by 3 by 2 has 12 cubes in one layer and 2 layers, so 24 cubes."), ex("Rectangular tank", "A tank 30 cm by 20 cm filled to 10 cm has volume 30x20x10 = 6000 cm3."), ex("Liquid link", "For water in a rectangular tank, the water height is one dimension of the cuboid.")},
			[]gin.H{mis("Using a slanted side as height", "Height must be perpendicular to the chosen base."), mis("Forgetting half for triangle", "Triangle area is half of the matching rectangle."), mis("Mixing area and volume units", "Area is square units. Volume is cubic units."), mis("Counting only the front face for volume", "Volume includes layers going back."), mis("Adding internal split lines to perimeter", "Split lines help area but are not outside edges.")},
			[]gin.H{cq("Triangle base 8 cm, height 5 cm. Area?", "Half of rectangle.", []string{"13 cm2", "20 cm2", "40 cm2", "80 cm2"}, "20 cm2", "8x5 divided by 2 = 20 cm2."), cq("Why must height be perpendicular?", "Height measures straight distance from base.", []string{"It is the shortest distance to the base", "It makes the side longer", "It is always the slanted side", "It removes the unit"}, "It is the shortest distance to the base", "The rectangle-half model needs a right-angle height."), cq("A cuboid is 5 cm by 4 cm by 3 cm. Volume?", "Layer area times layers.", []string{"12 cm3", "20 cm3", "60 cm3", "120 cm3"}, "60 cm3", "5x4x3=60."), cq("Volume unit should be...", "Cubes fill space.", []string{"cm", "cm2", "cm3", "kg"}, "cm3", "Volume counts cubic units."), cq("A tank base is 20 cm by 10 cm and water height is 8 cm. Volume?", "Rectangular tank.", []string{"160 cm3", "800 cm3", "1600 cm3", "38 cm3"}, "1600 cm3", "20x10x8=1600 cm3."), cq("Composite area first step?", "Make familiar shapes.", []string{"split into known shapes", "add every printed number", "use volume formula", "ignore units"}, "split into known shapes", "Composite figures become manageable when split into rectangles and triangles.")},
			[]gin.H{cq("A triangle has area 36 cm2 and base 9 cm. Height?", "Use base x height divided by 2.", []string{"4 cm", "8 cm", "18 cm", "27 cm"}, "8 cm", "9 x height divided by 2 = 36, so 9 x height = 72 and height=8."), cq("A cuboid volume is 96 cm3. Length 8 cm, breadth 4 cm. Height?", "Find missing layer count.", []string{"3 cm", "4 cm", "12 cm", "84 cm"}, "3 cm", "8x4=32 per layer. 96 divided by 32 is 3."), cq("Which expression finds triangle area?", "Half of base times height.", []string{"base x height divided by 2", "base + height divided by 2", "base x height x 2", "base + height + base"}, "base x height divided by 2", "The matching rectangle is base x height, and triangle is half."), cq("A shape is a rectangle plus a triangle. Rectangle area 48 cm2, triangle area 15 cm2. Total area?", "Add parts.", []string{"33 cm2", "48 cm2", "63 cm2", "720 cm2"}, "63 cm2", "48+15=63."), cq("Why does length x breadth x height work for a cuboid?", "Layers of cubes.", []string{"length x breadth counts one layer, then height counts layers", "it is a memory trick only", "height is always area", "it counts outside edges"}, "length x breadth counts one layer, then height counts layers", "The formula grows from counting unit cubes.")},
			cq("A child says a 10 by 6 triangle has area 60 because 10x6=60. What question should you ask?", "Connect to the rectangle model.", []string{"Where is the matching rectangle, and is the triangle half of it?", "Can you add another zero?", "Is 60 a nice number?", "Can you ignore height?"}, "Where is the matching rectangle, and is the triangle half of it?", "The rectangle is 60 cm2; the triangle with same base and height is half, 30 cm2.")), true
	case "P5-AVERAGE":
		return mergePack(base,
			"Average means equal sharing of a total.",
			"Average is not a magic middle number. It asks what each value would be if the total were shared equally across all items.",
			"p5Average",
			[]string{"Average = total divided by number of values.", "Total = average x number of values.", "Average is a balance point: high values and low values can level out.", "Average does not have to be one of the original data values.", "Outliers can pull the average up or down.", "Average must keep the unit of the data."},
			[]string{"Find or build the total.", "Count how many values there are.", "Share the total equally.", "Write the unit.", "If average and count are known, multiply to recover total.", "Check whether the average is reasonable between low and high values."},
			[]gin.H{ex("Equal sharing", "Scores 6, 8, and 10 have total 24. Shared equally among 3 scores, each would be 8."), ex("Average formula", "Average of 12, 15, 18 is 45 divided by 3 = 15."), ex("Find total", "If average is 7 books for 5 pupils, total books = 7x5 = 35."), ex("Not an original value", "The average of 2 and 3 is 2.5, even though no value is 2.5."), ex("Reasonable range", "The average of 4, 5, and 15 must be between 4 and 15."), ex("Outlier", "A very large value can pull the average higher.")},
			[]gin.H{mis("Using highest value as average", "Average shares the total equally; it is not automatically the highest or middle value."), mis("Forgetting count", "Total alone is not enough. You must know how many values share it."), mis("Thinking average must be in the list", "Average can be a decimal or fraction between values."), mis("Dropping the unit", "Average height, average score and average dollars need units or labels."), mis("Ignoring outliers", "One unusual value can shift the average noticeably.")},
			[]gin.H{cq("Average of 6, 8, 10 = ?", "Total then share.", []string{"6", "8", "10", "24"}, "8", "6+8+10=24 and 24 divided by 3 is 8."), cq("Average = total divided by ...", "Equal sharing count.", []string{"largest value", "number of values", "smallest value", "difference"}, "number of values", "Average shares total among all values."), cq("Average of 5 values is 9. Total?", "Reverse formula.", []string{"14", "45", "9", "5"}, "45", "9x5=45."), cq("Can an average be a decimal?", "Think equal sharing.", []string{"Yes", "No", "Only for money", "Only for geometry"}, "Yes", "Equal sharing can produce decimals."), cq("Average of 3, 4, 20 is pulled upward by...", "Find outlier.", []string{"3", "4", "20", "the count"}, "20", "20 is much larger and raises the average."), cq("A good first step for average is...", "Build total.", []string{"add the values", "pick the biggest", "subtract all values", "draw a triangle"}, "add the values", "Average starts with the total.")},
			[]gin.H{cq("Average of 7, 9, 11, 13 = ?", "Total divided by 4.", []string{"9", "10", "11", "40"}, "10", "7+9+11+13=40, and 40 divided by 4 is 10."), cq("Four children average 12 stickers. Total stickers?", "Average x count.", []string{"3", "16", "48", "124"}, "48", "12x4=48."), cq("Average of 2 and 3 is...", "Can be decimal.", []string{"2", "2.5", "3", "5"}, "2.5", "5 shared by 2 is 2.5."), cq("Which average is impossible for values 10, 12, 14?", "Average must be in range.", []string{"10", "12", "14", "20"}, "20", "The average cannot be greater than every value in the set."), cq("A set has total 63 and average 9. How many values?", "Count = total divided by average.", []string{"6", "7", "8", "9"}, "7", "63 divided by 9 is 7.")},
			cq("Maya says the average of 1, 1, 1, 21 is 1 because most numbers are 1. What is the correction?", "Average uses total, not majority.", []string{"The average is 6 because total 24 shared by 4 is 6", "The average is 1 because it appears most", "The average is 21", "There is no average"}, "The average is 6 because total 24 shared by 4 is 6", "Average is equal sharing of the total; it is different from mode.")), true
	case "P5-GEOMETRY":
		return mergePack(base,
			"Unknown angles are solved by evidence, not by guessing.",
			"P5 geometry combines angle rules with triangle and quadrilateral properties. The child should name the rule she is using before calculating.",
			"p5Geometry",
			[]string{"Angles on a straight line add to 180 degrees.", "Angles at a point add to 360 degrees.", "Vertically opposite angles are equal.", "Angles in a triangle add to 180 degrees.", "Equal sides in an isosceles triangle give equal base angles.", "An equilateral triangle has three 60-degree angles.", "Parallel lines in parallelograms create equal and supplementary angle relationships.", "Quadrilateral properties are evidence for unknown-angle work."},
			[]string{"Mark known angles.", "Name the rule that connects them.", "Write the equation.", "Solve one unknown at a time.", "For triangles, check the sum is 180 degrees.", "For quadrilaterals, use properties before arithmetic.", "Write a reason beside each calculation."},
			[]gin.H{ex("Straight line", "If two angles on a straight line are 65 degrees and x degrees, then x=115 degrees because 65+115=180."), ex("Point", "Angles around a point total 360 degrees."), ex("Vertically opposite", "When two lines cross, opposite angles are equal."), ex("Triangle sum", "A triangle with angles 50 degrees and 60 degrees has third angle 70 degrees."), ex("Isosceles triangle", "If two sides are equal, the two base angles are equal."), ex("Equilateral triangle", "All three angles are 60 degrees because they are equal and total 180 degrees."), ex("Parallelogram", "Opposite angles are equal and adjacent angles add to 180 degrees."), ex("Trapezium", "Parallel sides create angle relationships along a transversal.")},
			[]gin.H{mis("Guessing from how the drawing looks", "Diagrams may not be drawn to scale. Use angle rules."), mis("Using the wrong total", "Straight line uses 180 degrees. Around a point uses 360 degrees. Triangle uses 180 degrees."), mis("Forgetting equal angles in isosceles triangles", "Equal sides give equal base angles."), mis("Calling every four-sided shape a rectangle", "Use the actual properties: parallelogram, rhombus, trapezium."), mis("Writing answer without reason", "Unknown-angle answers need evidence.")},
			[]gin.H{cq("Angles on a straight line add to...", "Straight angle.", []string{"90 degrees", "180 degrees", "270 degrees", "360 degrees"}, "180 degrees", "A straight line is half a full turn."), cq("Angles around a point add to...", "Full turn.", []string{"90 degrees", "180 degrees", "270 degrees", "360 degrees"}, "360 degrees", "Around a point is a full turn."), cq("A triangle has 50 degrees and 70 degrees. Third angle?", "Triangle sum.", []string{"50 degrees", "60 degrees", "70 degrees", "120 degrees"}, "60 degrees", "180-50-70=60."), cq("In an equilateral triangle, each angle is...", "Three equal angles.", []string{"45 degrees", "60 degrees", "90 degrees", "180 degrees"}, "60 degrees", "180 divided by 3 is 60."), cq("Vertically opposite angles are...", "Crossing lines.", []string{"equal", "always 90 degrees", "always 180 degrees", "unrelated"}, "equal", "Opposite angles formed by intersecting lines are equal."), cq("In an isosceles triangle, the base angles are...", "Equal sides, equal base angles.", []string{"equal", "always 60 degrees", "always 90 degrees", "ignored"}, "equal", "The two equal sides create equal base angles.")},
			[]gin.H{cq("An angle on a straight line is 118 degrees. The other angle?", "180-degree total.", []string{"62 degrees", "72 degrees", "118 degrees", "242 degrees"}, "62 degrees", "180-118=62."), cq("Three angles around a point are 90 degrees, 120 degrees, and x. x = ?", "360-degree total.", []string{"120 degrees", "130 degrees", "150 degrees", "210 degrees"}, "150 degrees", "360-90-120=150."), cq("An isosceles triangle has vertex angle 40 degrees. Each base angle?", "Remaining angle split equally.", []string{"40 degrees", "70 degrees", "80 degrees", "140 degrees"}, "70 degrees", "180-40=140, then 140 divided by 2 is 70."), cq("A parallelogram has one angle 65 degrees. Adjacent angle?", "Adjacent angles are supplementary.", []string{"65 degrees", "115 degrees", "130 degrees", "295 degrees"}, "115 degrees", "Adjacent angles in a parallelogram add to 180 degrees."), cq("Which reason proves x=45 degrees when two lines cross and the opposite angle is 45 degrees?", "Name evidence.", []string{"vertically opposite angles are equal", "all diagrams are equal", "straight lines are 45 degrees", "triangles are 45 degrees"}, "vertically opposite angles are equal", "The rule, not the appearance, proves the value.")},
			cq("A child says an angle is 70 degrees because it 'looks about 70'. What should she write instead?", "Evidence-first geometry.", []string{"the rule or measurement that proves 70 degrees", "only the final number", "the colour of the line", "a bigger drawing"}, "the rule or measurement that proves 70 degrees", "P5 geometry answers need evidence such as straight-line sum, triangle sum, or vertically opposite angles.")), true
	}
	return nil, false
}

func buildP4TopicLessonPack(code string, base gin.H) (gin.H, bool) {
	switch code {
	case "P4-WHOLE-100000":
		return mergePack(base,
			"Large numbers, rounding, estimation, and patterns all depend on place value.",
			"P4 extends the same ones-tens-hundreds idea to ten-thousands and hundred-thousands. The child should read a number as grouped value, compare from the largest place, use ≈ only for rounded or estimated values, and explain which place changes in a pattern.",
			"place",
			[]string{"Each place is ten times the place to its right.", "Comparing starts at the largest place because one ten-thousand can outweigh many ones.", "Rounding is choosing the nearest friendly benchmark, not changing every digit.", "The symbol ≈ means approximately equal: close enough for an estimate, not exactly the same amount.", "Rounding can cross a boundary, such as 99,950 becoming 100,000.", "Patterns become easier when she names the size of each jump and the place that changes."},
			[]string{"Build the number with place cards: ten-thousands, thousands, hundreds, tens, ones.", "Read the expanded value before saying the compact number.", "Compare from the leftmost different digit.", "Decide if the task needs an exact value or an estimate.", "For rounding, mark the target place and inspect only the next digit.", "Use ≈ for rounded or estimated values; use = only for exact values.", "For patterns, subtract neighbouring terms to find the jump.", "Check whether an estimate is above or below the exact number."},
			[]gin.H{ex("Build 62,705", "62,705 means 6 ten-thousands, 2 thousands, 7 hundreds, 0 tens, and 5 ones. The zero still matters because it holds the tens place."), ex("Compare", "78,430 is greater than 76,999 because the ten-thousands are equal but 8 thousands is greater than 6 thousands."), ex("Approximately equal", "79,486 ≈ 80,000 when rounded to the nearest ten-thousand. It is useful for talking about size, but the exact amount is still 79,486."), ex("Boundary rounding", "99,950 to the nearest hundred is 100,000. The tens digit is 5, so 99,900 rounds up and crosses into a new hundred-thousand."), ex("Estimate check", "38,742 + 1,985 ≈ 39,000 + 2,000 = 41,000. The exact answer should be close to 41,000."), ex("Pattern", "72,560, 68,560, 64,560 changes by -4,000 each step; the thousands place is moving down.")},
			[]gin.H{mis("Reading digit by digit only", "Ask what each digit is worth: the 6 in 62,705 is 60,000, not 6."), mis("Comparing from the right", "Start at the largest place. Ones cannot beat thousands."), mis("Using = for an estimate", "Rounded numbers are close, not exact. Write 79,486 ≈ 80,000, not 79,486 = 80,000."), mis("Rounding too many places", "Choose one target place and ignore the rest after checking the next digit."), mis("Missing a boundary carry", "If rounding 99,950 up, the 9s can trade into 100,000."), mis("Guessing a pattern from the last digit only", "Find the jump between terms and name which place changes.")},
			[]gin.H{cq("What is the value of 8 in 83,419?", "Name the place first.", []string{"8", "800", "8,000", "80,000"}, "80,000", "The 8 is in the ten-thousands place."), cq("Which is greatest?", "Compare from the left.", []string{"56,904", "56,490", "57,009", "55,999"}, "57,009", "57 thousand is greater than 56 thousand or 55 thousand."), cq("Round 68,451 to the nearest hundred.", "Look at the tens digit.", []string{"68,400", "68,500", "68,000", "69,000"}, "68,500", "The target is hundreds. The tens digit is 5, so round up."), cq("Which symbol fits: 79,486 __ 80,000 after rounding to the nearest ten-thousand?", "Exact or estimate?", []string{"=", "≈", ">", "<"}, "≈", "80,000 is a rounded estimate, not the exact amount."), cq("What comes next: 24,750, 25,000, 25,250, ___?", "Find the jump.", []string{"25,300", "25,500", "26,000", "24,500"}, "25,500", "Each step adds 250."), cq("What is the jump: 72,560, 68,560, 64,560?", "Subtract neighbouring terms.", []string{"-400", "-4,000", "+4,000", "-40,000"}, "-4,000", "Each term is 4 thousands less than the previous one."), cq("Which expanded form is 70,306?", "Keep the zero thousands and zero tens in mind.", []string{"70,000+300+6", "7,000+300+6", "70,000+3,000+60", "70,000+30+6"}, "70,000+300+6", "There are 7 ten-thousands, 3 hundreds, and 6 ones.")},
			[]gin.H{cq("A library has 38,742 books. It buys 1,985 more. Which estimate is best before exact addition?", "Round both numbers.", []string{"38,742 + 1,985 ≈ 39,000 + 2,000 = 41,000", "about 30,000 + 1,000", "about 38 + 19", "about 40,000 + 20,000"}, "38,742 + 1,985 ≈ 39,000 + 2,000 = 41,000", "38,742 is near 39,000 and 1,985 is near 2,000."), cq("Which number is 4,000 less than 72,560?", "Only the thousands value changes.", []string{"68,560", "72,160", "32,560", "76,560"}, "68,560", "72 thousands minus 4 thousands is 68 thousands."), cq("Round 99,950 to the nearest hundred.", "This crosses a boundary.", []string{"99,900", "100,000", "99,000", "100,100"}, "100,000", "The tens digit is 5, so 99,900 rounds up to 100,000."), cq("Which use of ≈ is correct?", "Approximately equal means rounded or estimated.", []string{"79,486 ≈ 80,000", "79,486 ≈ 79,486", "80,000 ≈ 7,486", "79,486 = 80,000"}, "79,486 ≈ 80,000", "79,486 rounds to 80,000 to the nearest ten-thousand."), cq("Which statement is true?", "Use place-value reasoning.", []string{"90,005 > 90,050", "87,999 > 88,001", "100,000 > 99,999", "45,700 < 45,070"}, "100,000 > 99,999", "100,000 is one more than 99,999.")},
			cq("A school wants to say its fundraiser collected about $80,000. The exact amount was $79,486. Which statement is mathematically honest?", "Connect rounding to real reporting.", []string{"79,486 = 80,000", "79,486 ≈ 80,000 to the nearest ten-thousand", "79,486 is closer to 70,000", "Rounded numbers can replace exact accounting"}, "79,486 ≈ 80,000 to the nearest ten-thousand", "A rounded public estimate can use ≈. Exact records should still keep 79,486.")), true
	case "P4-FACTORS-MULTIPLES":
		return mergePack(base,
			"Factors build one number exactly; multiples are the landing points when you keep jumping by one number.",
			"This topic should not feel like two lists to memorise. It is one relationship seen from two directions: if 6 × 8 = 48, then 6 and 8 are factors of 48, and 48 is a multiple of both 6 and 8. Word problems usually ask either exact sharing or repeated cycles meeting. Common factors answer equal-pack questions; common multiples answer same-time or next-together questions. The deciding question matters: factors answer 'Can one total split exactly?' while multiples answer 'When do repeated jumps land together?'",
			"factorRainbow",
			[]string{"A factor test is an exact-division test: no remainder means yes.", "A factor pair is two factors whose product rebuilds the target number.", "An array model shows factor pairs as rows and columns.", "A T-chart keeps factor pairs organised so the child does not miss a pair.", "A multiple is a repeated equal jump: 6, 12, 18, 24 are landing points for jumps of 6.", "Common factors are shared building blocks.", "Common means the answer must pass both numbers, not just one list.", "The greatest common factor is useful for the largest equal pack size with no leftovers.", "Common multiples are shared landing points.", "The least common multiple is the first shared landing point that solves a cycle problem.", "The first common multiple is useful when repeated cycles meet again.", "Story words guide the model: exactly/no leftover points to factors; together/again points to multiples; both numbers often means common.", "A real word problem decides whether the shared factor or shared multiple is useful.", "Factor stories start with one fixed total; multiple stories start with a repeating jump."},
			[]string{"Ask the deciding question first: split one total exactly, or find when jumps meet?", "Underline the story job words: exactly, no leftover, equal packs, together, again, both numbers.", "Start with one multiplication fact and say it three ways: 6 × 8 = 48, 6 is a factor, 48 is a multiple.", "For factors, test exact division or list factor pairs from small to large.", "Use a T-chart: small factors on the left, matching partners on the right.", "Stop factor-pair listing when the smaller factor meets or passes the larger factor.", "For common factors, circle only candidates that divide both totals exactly.", "For multiples, skip-count or multiply by 1, 2, 3, and so on.", "For common multiples, circle only landing points reached by both jumps.", "For common factors, compare exact factor lists and choose the largest if the question asks for greatest equal groups.", "For common multiples, compare landing points and choose the first if the question asks when cycles meet again.", "In word problems, ask: do we need equal pack size, or do we need the next same time?"},
			[]gin.H{ex("One fact, three meanings", "6 × 8 = 48 means 6 and 8 are factors of 48. It also means 48 is a multiple of 6 and a multiple of 8."), ex("Read the story words", "No leftover, exactly, equal packs and equal pieces point to factors. Together again, same time, bells and buses point to multiples. Both totals or both cycles usually means common."), ex("Array model", "An array with 6 rows and 8 columns has 48 objects. That picture shows 6 and 8 as factor pairs of 48."), ex("T-chart factor pairs", "For 36, write 1|36, 2|18, 3|12, 4|9, 6|6. Stop at 6|6 because after that the pairs repeat in reverse."), ex("Multiple as landing points", "Multiples of 8 are 8, 16, 24, 32, 40 because they are 8×1, 8×2, 8×3, 8×4, 8×5."), ex("Common factor filter", "For 18 and 24, factors of 18 include 1, 2, 3, 6, 9, 18. Factors of 24 include 1, 2, 3, 4, 6, 8, 12, 24. The shared candidates are 1, 2, 3, 6."), ex("Common factor", "Common factors of 24 and 36 are 1, 2, 3, 4, 6, 12. Each one divides both numbers exactly."), ex("Greatest equal packs", "24 pencils and 36 stickers can make at most 12 equal packs with no leftovers because 12 is the greatest common factor."), ex("Common multiple", "Multiples of 6 and 8 first meet at 24, because 6×4=24 and 8×3=24."), ex("Least common multiple filter", "The shared landing points of jumps by 6 and 8 start 24, 48, 72. The first one is 24, so 24 solves the first-meet problem."), ex("Cycles meet", "A bus every 12 minutes and a train every 15 minutes meet again after 60 minutes because 60 is the first common multiple."), ex("Choose the idea", "Equal bags, packs, teams, ribbons or groups usually use common factors. Bells, buses, lights and repeating schedules usually use common multiples."), withDiagram(ex("Story walk-through: apples and oranges in bags", "Step 1 Which story is it: \"share equally\" or \"meet again\"? 18 apples and 24 oranges go into equal bags with nothing left over. This one is \"share equally\", so we need common factors. Step 2 Draw it: draw bags and put the same number of apples and the same number of oranges in each. Try 4 bags: 18 apples leave 2 over, so 4 fails. Try 6 bags: 3 apples and 4 oranges each, nothing left. Step 3 List and circle: factors of 18 are 1, 2, 3, 6, 9, 18; factors of 24 are 1, 2, 3, 4, 6, 8, 12, 24. Circle 1, 2, 3, 6. Step 4 Size check: a common factor is never bigger than the smaller total; a common multiple is never smaller than the bigger number. Here, the smaller total is 18 and 6 is much smaller than 18. Step 5 Say the answer back: 6 bags is the most; each bag holds 3 apples and 4 oranges."), "factor-story-bags.svg"), withDiagram(ex("Story walk-through: stickers and cards in sets", "Step 1 Which story is it: \"share equally\" or \"meet again\"? 16 stickers and 40 cards are made into equal sets with nothing left over. This one is \"share equally\", so we need common factors. Step 2 Draw it: draw 8 sets, then put 2 stickers and 5 cards in each set. Test 5 sets: 16 stickers leave 1 over, so 5 fails. Step 3 List and circle: factors of 16 are 1, 2, 4, 8, 16; factors of 40 are 1, 2, 4, 5, 8, 10, 20, 40. Circle 1, 2, 4, 8. Step 4 Size check: a common factor is never bigger than the smaller total; a common multiple is never smaller than the bigger number. Here, the smaller total is 16 and 8 is not bigger than 16. Step 5 Say the answer back: The most sets is 8; each set holds 2 stickers and 5 cards."), "factor-story-bags.svg"), withDiagram(ex("Story walk-through: two swimmers", "Step 1 Which story is it: \"share equally\" or \"meet again\"? One child swims every 4 days and the other every 6 days, and both swam today (day 0). The question is when they swim together again, so this one is \"meet again\" and we need common multiples. Step 2 Draw it: draw a row of days 0 to 24, mark every 4th day in one colour and every 6th day in another, and circle the day where both marks meet. Step 3 List and circle: multiples of 4: 4, 8, 12, 16, 20, 24; multiples of 6: 6, 12, 18, 24. Circle 12 and 24. Step 4 Size check: a common factor is never bigger than the smaller total; a common multiple is never smaller than the bigger number. Here, the bigger number is 6 and 12 is not smaller than 6. Step 5 Say the answer back: They swim together again on day 12, the first shared mark."), "multiple-story-calendar.svg"), withDiagram(ex("Story walk-through: piano and chess days", "Step 1 Which story is it: \"share equally\" or \"meet again\"? Amy practises piano every 6 days and Ben plays chess every 9 days, and both did so today. The question is when both happen on one day again, so this one is \"meet again\" and we need common multiples. Step 2 Draw it: draw a row of days with marks every 6 days and every 9 days in two colours, and look for the first day with both marks. Step 3 List and circle: multiples of 6: 6, 12, 18, 24; multiples of 9: 9, 18, 27. Circle 18. Step 4 Size check: a common factor is never bigger than the smaller total; a common multiple is never smaller than the bigger number. Here, the bigger number is 9 and 18 is not smaller than 9. Step 5 Say the answer back: They are both busy again after 18 days."), "multiple-story-calendar.svg")},
			[]gin.H{mis("Mixing factor and multiple", "Say the sentence both ways: 6 is a factor of 48; 48 is a multiple of 6."), mis("Accepting a divisor with remainder", "If 48 ÷ 5 leaves a remainder, 5 is not a factor of 48."), mis("Stopping factor pairs too early", "Keep testing until the factor pair meets or reverses."), mis("Listing only multiples when asked for factors", "Factors stay at or below the number. Multiples keep going upward."), mis("Using common multiple for equal packs", "Equal packs with no leftovers need common factors, not common multiples."), mis("Using common factor for cycles meeting", "Repeated cycles meeting again need common multiples, not common factors."), mis("Thinking greatest always means multiple", "Greatest equal pack size asks for the greatest common factor, not a common multiple."), mis("Thinking first common multiple means largest", "Often the useful common multiple is the first shared landing point, not the biggest one."), mis("Skipping the story question", "Ask whether the story is about one total being split or repeated jumps meeting."), mis("Only memorising tables", "Use division, multiplication, arrays or a T-chart when memory is unsure."), mis("Choosing factor or multiple from one keyword", "The words share equally can hide a common multiple: the smallest number of sweets that can be shared equally among 4 children or among 6 children is 12, a common multiple, because the total is not given and must be reached by both counts. Do the size check: a common factor is never bigger than the smaller total, a common multiple is never smaller than the bigger number. Then draw the picture before choosing.")},
			[]gin.H{cq("Which sentence is true for 6 × 8 = 48?", "Read the same fact in both directions.", []string{"48 is a factor of 6", "6 is a factor of 48", "8 is a multiple of 48", "6 and 48 are the same size"}, "6 is a factor of 48", "Because 48 can be built from 6 groups of 8, 6 divides 48 exactly."), cq("Which is a factor of 48?", "It must divide exactly.", []string{"5", "7", "8", "10"}, "8", "48 ÷ 8 = 6 with no remainder."), cq("Which is a multiple of 9?", "Use the 9 times table.", []string{"27", "32", "44", "50"}, "27", "9 × 3 = 27."), cq("Which pair makes a factor pair of 56?", "Multiply to 56.", []string{"6 and 8", "7 and 8", "5 and 11", "9 and 6"}, "7 and 8", "7 × 8 = 56."), cq("Why is a 6 by 8 array useful for 48?", "Arrays show factor pairs.", []string{"It shows 6×8=48", "It proves 6+8=48", "It shows 48 is prime", "It removes all factors"}, "It shows 6×8=48", "Rows and columns make the factor pair visible."), cq("A number is divisible by 6. What must be true?", "Use exact division.", []string{"It is a multiple of 6", "It must be odd", "It is smaller than 6", "It has no factors"}, "It is a multiple of 6", "If 6 divides it exactly, it appears in the 6 times table."), cq("Common factor of 18 and 30?", "Check both numbers.", []string{"4", "5", "6", "9"}, "6", "18 ÷ 6 = 3 and 30 ÷ 6 = 5."), cq("First common multiple of 4 and 10?", "List landing points.", []string{"10", "14", "20", "40"}, "20", "4, 8, 12, 16, 20 and 10, 20 meet at 20."), cq("A story asks for largest equal packs with no leftovers. Which idea fits?", "Choose the model.", []string{"common factor", "common multiple", "rounding", "angle turn"}, "common factor", "Equal packs split totals exactly, so use common factors."), cq("A story asks when two lights flash together again. Which idea fits?", "Repeated cycles meet.", []string{"common multiple", "common factor", "area", "rounding"}, "common multiple", "Repeated schedules meet at shared landing points."), withDiagram(cq("Which picture fits this story: equal bags or jumps that meet? Mia has 20 cherries and 30 plums and packs them into equal bags with nothing left over.", "Decide the picture before any number.", []string{"Equal bags", "Jumps that meet"}, "Equal bags", "One fixed total is split exactly, so draw bags."), "factor-story-bags.svg"), withDiagram(cq("Mia packs 20 cherries and 30 plums into equal bags with nothing left over. What is the greatest number of bags?", "Draw the bags, list factors of 20 and 30, circle the common ones.", []string{"5", "10", "20", "60"}, "10", "10 divides 20 and 30 exactly, and a common factor cannot be bigger than 20. Each bag holds 2 cherries and 3 plums."), "factor-story-bags.svg"), withDiagram(cq("Which picture fits this story: equal bags or jumps that meet? Ravi swims every 3 days and Lena swims every 5 days. Both swim today.", "Decide the picture before any number.", []string{"Equal bags", "Jumps that meet"}, "Jumps that meet", "Repeated jumps that must land on the same day, so draw jump marks."), "multiple-story-calendar.svg"), withDiagram(cq("Ravi swims every 3 days and Lena every 5 days, and both swim today. After how many days do they swim on the same day again?", "Mark both jumps on the day row and circle the first shared mark.", []string{"8", "15", "30", "45"}, "15", "Multiples of 3 and of 5 first meet at 15, and a common multiple cannot be smaller than 5."), "multiple-story-calendar.svg"), withDiagram(cq("Which picture fits this story: equal bags or jumps that meet? A shop has 16 red pens and 28 blue pens and makes equal sets with no pens left over.", "Decide the picture before any number.", []string{"Equal bags", "Jumps that meet"}, "Equal bags", "Totals split exactly into equal sets, so draw bags."), "factor-story-bags.svg"), withDiagram(cq("A shop makes equal sets from 16 red pens and 28 blue pens with no pens left over. What is the greatest number of sets?", "Draw the sets, list factors of 16 and 28, circle the common ones.", []string{"2", "4", "8", "44"}, "4", "Common factors of 16 and 28 are 1, 2, 4, and 4 is the greatest. Each set has 4 red and 7 blue pens."), "factor-story-bags.svg")},
			[]gin.H{cq("Which number has exactly these factor pairs: 1×24, 2×12, 3×8, 4×6?", "Rebuild from pairs.", []string{"12", "24", "36", "48"}, "24", "Every pair multiplies to 24."), cq("Is 7 a factor of 91?", "Use division or multiplication.", []string{"Yes, because 7×13=91", "Yes, because 91 is odd", "No, because 7 is prime", "No, because 91 ends in 1"}, "Yes, because 7×13=91", "A factor can be checked by multiplication."), cq("Which number is both a factor of 60 and a multiple of 5?", "Both conditions must be true.", []string{"8", "10", "12", "18"}, "10", "60 ÷ 10 = 6 and 10 is 5×2."), cq("Two bells ring every 6 min and 8 min. They ring together now. When together again?", "This is common multiple thinking.", []string{"12 min", "14 min", "24 min", "48 min"}, "24 min", "The first common multiple of 6 and 8 is 24."), cq("A teacher has 24 pencils and 36 stickers. What is the greatest equal number of packs she can make with nothing left over?", "This is common factor thinking.", []string{"6", "8", "12", "24"}, "12", "12 divides both 24 and 36 exactly, and no larger listed number divides both."), cq("A bus leaves every 12 min and a train leaves every 15 min. They leave now. When together again?", "Cycles meeting use common multiples.", []string{"15 min", "30 min", "45 min", "60 min"}, "60 min", "12 and 15 first share the landing point 60."), cq("Two ribbons are 18 cm and 24 cm. What is the greatest equal length pieces can be cut with no leftover?", "Exact cutting uses common factors.", []string{"3 cm", "6 cm", "12 cm", "42 cm"}, "6 cm", "6 is the greatest common factor of 18 and 24."), cq("Which question asks for the first landing point shared by both numbers?", "Choose common multiple language.", []string{"When do two buses arrive together again?", "What is the biggest equal team size?", "How many equal bags can be made with no leftover?", "What side length makes equal ribbon pieces?"}, "When do two buses arrive together again?", "Together again is a repeated-cycle question, so use common multiples."), withDiagram(cq("A number is a multiple of 3 and of 4 and lies between 30 and 50. Which numbers fit?", "Common multiples of 3 and 4 are multiples of 12.", []string{"36 only", "48 only", "36 and 48", "12 and 24"}, "36 and 48", "Multiples of 12 are 12, 24, 36, 48, 60; only 36 and 48 lie between 30 and 50."), "multiple-story-calendar.svg"), withDiagram(cq("What is the smallest number of sweets that can be shared equally among 4 children or among 6 children?", "The words shared equally are a trap: is the total given?", []string{"2", "10", "12", "24"}, "12", "The total is unknown and must be a multiple of 4 and of 6, so this is a common multiple question. The first one is 12; a common multiple is never smaller than 6."), "multiple-story-calendar.svg"), withDiagram(cq("Tom packs 30 red beads and 42 blue beads into equal bags with nothing left over. What is the greatest number of bags?", "Draw the bags, then list and circle.", []string{"3", "6", "7", "12"}, "6", "6 divides 30 and 42, and no larger common factor exists. Each bag has 5 red and 7 blue beads."), "factor-story-bags.svg"), withDiagram(cq("Two lamps blink every 8 seconds and every 12 seconds and blink together now. When do they blink together again?", "Draw the jump marks and circle the first shared mark.", []string{"4 seconds", "20 seconds", "24 seconds", "96 seconds"}, "24 seconds", "Multiples of 8 and of 12 first meet at 24."), "multiple-story-calendar.svg"), withDiagram(cq("Sam says 18 apples and 24 oranges can fill 36 equal bags. Which size check shows he is wrong?", "Use the size rule for common factors.", []string{"A common factor is never bigger than the smaller total, 18", "A common multiple is never smaller than 24", "36 is not an even number", "Apples and oranges cannot go in one bag"}, "A common factor is never bigger than the smaller total, 18", "36 is bigger than 18, so it cannot divide both totals."), "factor-story-bags.svg")},
			cq("A coach wants equal teams with no players left over. There are 36 players. Which team sizes work and why?", "Use factors in a real constraint.", []string{"5 only", "4, 6, or 9", "7 or 8", "Any number smaller than 36"}, "4, 6, or 9", "36 can be divided exactly by 4, 6, and 9, so those team sizes make equal teams with no leftover.")), true
	case "P4-FOUR-OPERATIONS":
		return mergePack(base,
			"Written algorithms are shortcuts for place-value stories; word problems need a model before the algorithm.",
			"P4 multiplication and division should not be memorised as columns. Use Concrete → pictorial → abstract: imagine blocks or equal groups, draw an area model box or bar model, then write the compact algorithm. Each line must mean a place-value action: partial products for multiplication, quotient-place decisions for division, and inverse checks for proof. For word problems, the child must first decide the relationship: part-whole, comparison, or equal groups.",
			"algorithm",
			[]string{"An algorithm gate prevents blind column work: model the story, estimate, calculate by place, then inverse check.", "An estimate predicts the answer size before exact work.", "Concrete → pictorial → abstract means the written method grows from blocks, arrays, bars, or equal groups.", "Every written algorithm line must pass a meaning audit: say what value that line represents.", "Partial products show which place values are being multiplied.", "Each partial product row is a place-value row, not just digits lined up by habit.", "An area model box turns 326×24 into 326×20 and 326×4 before the compact algorithm.", "Regrouping is trading value between ones, tens, hundreds, and thousands.", "Long division chooses each quotient digit by place, not by loose digit tricks.", "The division loop is divide, multiply, subtract, bring down, then check the remainder.", "After each division subtraction, the remainder must be smaller than the divisor before bringing down.", "A zero in the quotient can be a placeholder that shows no groups in that place.", "Each subtraction in division records the amount already shared or grouped.", "Inverse operations prove the algorithm rebuilt the original number.", "A bar model shows whether a word problem is part-whole, comparison, or equal groups.", "Operation choice comes from the relationship, not from a keyword."},
			[]string{"For word problems, ask what the story is about before calculating.", "Draw a bar model: part-whole, comparison, or equal groups.", "Write what each bar or group represents.", "Choose the operation from the model, not from one keyword.", "Estimate the answer size before exact work.", "Identify the places in the multiplier or dividend.", "For multiplication, split the multiplier into tens and ones when useful.", "Use an area model box or partial-products table before the compact column method.", "Record partial products with the correct place shift.", "Audit each multiplication line: what place value does this row represent?", "For division, decide each quotient digit by place: hundreds, tens, ones.", "If a place has no groups, write 0 in that quotient place instead of deleting the place.", "Use the division loop: divide, multiply, subtract, bring down.", "After each division step, name what remains and check it is smaller than the divisor.", "Verify with multiplication or divisor × quotient + remainder."},
			[]gin.H{ex("Algorithm gate", "For every written method, use the same gate: model the story, estimate the size, calculate by place, then inverse check. If one step is missing, the answer may be a lucky column result."), ex("2-digit multiplication", "326 × 24 = 326×20 + 326×4. 326×20=6520 and 326×4=1304, so 326×24=7824."), ex("Line meaning audit", "In 326×24, the 6520 row means 326×20. If a child says it means 326×2, the place value has been lost."), ex("Area model box", "For 326×24, draw 326 on one side and split 24 into 20 and 4. The two boxes are 326×20 and 326×4. Add the boxes at the end."), ex("1-digit multiplication", "4,203 × 6 means 4,000×6 + 200×6 + 3×6. Regrouping keeps each value in the correct place, giving 25,218."), ex("Long division by place", "846 ÷ 3: 8 hundreds give 2 hundreds, 24 tens give 8 tens, 6 ones give 2 ones, so 282."), ex("Division loop", "In long division, each cycle means divide the current place, multiply back, subtract what was used, bring down the next place, then check the new remainder."), ex("Zero in the quotient", "824 ÷ 4 gives 2 hundreds, then 2 tens cannot make a group of 4 tens, so write 0 tens and continue. 24 ones ÷4=6, so the quotient is 206."), ex("Place-value correction", "918 ÷ 3 is not 36. The 9 is hundreds, so the quotient starts with 3 hundreds. The answer is 306."), ex("Remainder check", "631 ÷ 5 = 126 r1 because 5×126+1=631. The remainder must be smaller than 5."), ex("Part-whole word problem", "Maya has 326 stickers. Leo has 248 stickers. Together means two parts make one whole, so add 326+248."), ex("Comparison word problem", "A shop sold 1,248 apples and 376 more oranges than apples. The bar model shows apples plus an extra 376, so oranges = 1,248+376."), ex("Equal groups word problem", "24 boxes with 326 books each means 24 equal groups of 326, so multiply 326×24."), ex("Check", "728 ÷ 4 = 182 because 182 × 4 = 728. The inverse operation proves the quotient.")},
			[]gin.H{mis("Treating a tens digit as ones in 2-digit multiplication", "In 326×24, the 2 means 20, so the partial product is 326×20, not 326×2."), mis("Carrying digits without naming the value", "A carried 2 may mean 2 tens or 2 hundreds. Say the value, not just the digit."), mis("Not lining up partial products", "The ×20 row is a tens row. A missing place shift makes the answer ten times too small."), mis("Writing a quotient digit in the wrong place", "Say whether you are dividing hundreds, tens, or ones before writing the quotient digit."), mis("Dropping zero in the quotient", "If there are 0 tens, write 0 in the tens place. Otherwise 206 can accidentally become 26."), mis("Subtracting in division without checking what remains", "The remainder after each step must be smaller than the divisor before you bring down."), mis("Using keyword tricks", "Words like more, left, altogether, or each can mislead. Draw the relationship first."), mis("Adding all numbers in a word problem", "The operation must match the bar model, not the number of values shown."), mis("Skipping estimate or inverse check", "Estimate catches unreasonable answers; inverse check catches tidy but wrong algorithms.")},
			[]gin.H{cq("Before solving a word problem, what should come first?", "Model before algorithm.", []string{"Draw or imagine the relationship", "Choose the biggest number", "Look for one keyword only", "Start long division"}, "Draw or imagine the relationship", "The model shows whether the story is part-whole, comparison, or equal groups."), cq("Which algorithm gate is in the right order?", "Use the full routine.", []string{"model → estimate → calculate by place → inverse check", "calculate → guess → erase → keyword", "inverse check → model → ignore estimate", "choose biggest number → add everything"}, "model → estimate → calculate by place → inverse check", "The gate keeps the column method connected to meaning."), cq("236 × 4 = ?", "Use place-value multiplication.", []string{"824", "944", "1,244", "914"}, "944", "4×6=24, carry 2 tens. 4×3 tens +2 =14 tens. 4×2 hundreds +1 =9 hundreds."), cq("36 × 24 means...", "Break 24 into tens and ones.", []string{"36×2 + 36×4", "36×20 + 36×4", "36+24", "36÷24"}, "36×20 + 36×4", "24 is 20 + 4, so multiply both parts."), cq("In 156 × 23, why is 156 × 20 useful?", "Understand the tens digit.", []string{"Because 2 means 20 in 23", "Because 20 is always smaller", "Because addition is easier", "Because we ignore 3"}, "Because 2 means 20 in 23", "The digit 2 is in the tens place."), cq("What does the area model box for 326×24 split?", "Pictorial before abstract.", []string{"326 into 3 and 26", "24 into 20 and 4", "326 into 32 and 6 only", "24 into 2 and 4"}, "24 into 20 and 4", "The multiplier 24 is 20+4, so the boxes are 326×20 and 326×4."), cq("The row 326×20=6520 means...", "Audit the line.", []string{"326 groups of 2", "326 groups of 20", "326 plus 20", "326 divided by 20"}, "326 groups of 20", "The row is a tens row because 20 means 2 tens."), cq("846 ÷ 3 = ?", "Divide by place.", []string{"262", "282", "292", "288"}, "282", "8 hundreds, 24 tens, and 6 ones divide evenly by 3."), cq("824 ÷ 4 = ?", "Watch the zero in the quotient.", []string{"26", "206", "260", "216"}, "206", "8 hundreds ÷4 = 2 hundreds. 2 tens cannot make a group of 4 tens, so write 0 tens. 24 ones ÷4 = 6."), cq("918 ÷ 3 = ?", "Start with hundreds.", []string{"36", "306", "316", "360"}, "306", "9 hundreds ÷3 = 3 hundreds. Then 18 ones ÷3 = 6 ones."), cq("Check 728 ÷ 4 = 182.", "Use inverse.", []string{"182×4=728", "728×4=2912", "182+4=186", "728-182=546"}, "182×4=728", "Division is checked by multiplication.")},
			[]gin.H{cq("326 × 24 = ?", "Use partial products.", []string{"1,304", "6,520", "7,824", "8,224"}, "7,824", "326×20=6520 and 326×4=1304; total 7824."), cq("4,203 × 6 = ?", "Keep zero places and regrouping.", []string{"24,018", "25,218", "25,128", "2,5218"}, "25,218", "4,203×6 = 24,000 + 1,200 + 18 = 25,218."), cq("936 ÷ 3 = ?", "Divide by place.", []string{"32", "302", "312", "321"}, "312", "9 hundreds ÷3=3 hundreds, 3 tens ÷3=1 ten, 6 ones ÷3=2 ones."), cq("After a division subtraction with divisor 5, a remainder of 7 means...", "Check remainder size.", []string{"continue, because 7 is fine", "the quotient digit was too small", "the division is finished", "write r7 immediately"}, "the quotient digit was too small", "After a subtraction step, the remainder should be smaller than the divisor before bringing down."), cq("Which check proves 631 ÷ 5 = 126 r1?", "Use divisor × quotient + remainder.", []string{"5×126+1=631", "126+5+1=132", "631×5=3155", "126-1=125"}, "5×126+1=631", "For division with remainder, divisor × quotient + remainder must rebuild the dividend."), cq("Which answer is unreasonable for 2,908 × 7?", "Estimate about 2,900×7.", []string{"20,356", "20,000", "2,036", "20,300"}, "2,036", "2,908×7 should be near 20,000, so 2,036 is far too small."), cq("A quotient digit belongs in the hundreds place when...", "Think place value before writing.", []string{"the divisor is odd", "the first hundreds can be divided", "the answer has a zero", "the numbers look large"}, "the first hundreds can be divided", "Long division places the quotient digit above the place being divided."), cq("A school packs 24 boxes with 326 books in each box. Which model fits?", "Equal groups.", []string{"24 equal groups of 326", "326 minus 24", "one comparison extra", "a decimal line"}, "24 equal groups of 326", "Each box has the same number, so this is equal groups and uses multiplication."), cq("Maya has 1,248 points. Leo has 376 fewer. What should the bar model show?", "Comparison.", []string{"Maya bar, Leo bar, and a 376 difference", "Two equal groups of 1,248", "Only one total bar", "A compass turn"}, "Maya bar, Leo bar, and a 376 difference", "Fewer means compare the two amounts; Leo's bar is shorter by 376.")},
			cq("A child says 918 ÷ 3 = 36 because 9÷3=3 and 18÷3=6. What is the best correction?", "Use place value, not just digits.", []string{"It is fine because 36 sounds close", "The 9 is hundreds, so the answer starts with 300, not 30", "Division cannot use place value", "Always use a calculator"}, "The 9 is hundreds, so the answer starts with 300, not 30", "918 has 9 hundreds. 9 hundreds ÷3 = 3 hundreds, so the quotient is 306, not 36.")), true
	case "P4-FRACTIONS-MIXED":
		return mergePack(base,
			"Fractions are numbers, but the first skill is choosing the right model.",
			"P4 fractions move beyond shaded pizza slices. The child needs to connect five ideas: the whole must be named first, rename without changing the amount, improper and mixed numbers sit on the same number line, fraction of a set uses equal groups, and addition/subtraction only works with same-sized fractional parts. Use a model gate before calculating: whole, equal parts, job, model.",
			"fractionBars",
			[]string{"Use the model gate: whole, equal parts, job, model.", "The first question is: what is the whole?", "The denominator names the size of equal parts in one whole.", "The numerator counts how many equal parts are used.", "A fraction bar shows why 1/2 = 2/4: the same length is cut into smaller equal pieces.", "Mixed numbers and improper fractions are two names for the same point on a number line.", "A number line helps show fractions as numbers greater than, equal to, or less than 1.", "Fraction of a set means divide the whole set into equal groups first, then take the number of groups named by the numerator.", "The word 'of' in a set problem usually means find part of a count.", "Fractions can only be compared fairly when the whole and part size are clear.", "Use the same whole before comparing fractions.", "Fractions can only be added or subtracted when the parts are the same size.", "A common denominator is not a magic trick; it makes the pieces the same size."},
			[]string{"Use the model gate: whole, equal parts, job, model.", "Identify the whole first: one shape, a set, or one unit on a number line.", "Check that parts or groups are equal.", "Name the job: rename, place on number line, compare, find part of a set, or combine/subtract same-sized pieces.", "Choose the model: bar/number line for a fraction as a number, set model for fraction of a set, common-denominator bars for addition/subtraction.", "Use a fraction bar when renaming: split each old part into equal smaller parts.", "For mixed to improper, convert wholes into fractional parts.", "For improper to mixed, make as many wholes as possible and put the leftover parts after the whole number.", "For number-line questions, mark each whole as denominator/denominator before placing the fraction.", "For fraction of a set, find one unit fraction first, then multiply.", "For fraction of a set, divide by the denominator then multiply by the numerator.", "For comparing, use same denominators, benchmark fractions like 1/2 and 1, or number lines.", "For addition or subtraction, rename unlike fractions to a common denominator first.", "Say whether you are renaming, comparing, finding part of a set, or combining same-sized parts before calculating."},
			[]gin.H{ex("Model gate", "Before calculating, ask: What is the whole? Are the parts equal? What job is the question asking? Which model fits: bar, number line, set, or common denominator?"), ex("Equivalent fraction bar", "1/2 and 2/4 can cover the same length. 2/4 has more pieces, but each piece is smaller, so the value stays the same."), ex("Improper to mixed", "11/4 means 11 fourths. 8 fourths make 2 wholes, with 3 fourths left, so 11/4 = 2 3/4."), ex("Mixed to improper", "3 2/5 means 3 wholes and 2 fifths. 3 wholes = 15 fifths, plus 2 fifths = 17/5."), ex("Number line", "7/4 is between 1 and 2 because 4/4=1 and 8/4=2. It lands at 1 3/4."), ex("Benchmark compare", "5/8 is greater than 1/2 because 1/2 = 4/8, and 5/8 is one eighth more."), ex("Fraction of a set", "3/4 of 20 means split 20 into 4 equal groups of 5, then take 3 groups: 15."), ex("Model choice", "7/4 asks where a fraction number sits, so a bar or number line fits. 3/4 of 20 asks for part of a count, so a set model fits."), ex("Unit fraction first", "2/5 of 30 starts with 1/5 of 30. 30 ÷ 5 = 6, then 2 fifths is 2 groups of 6, so 12."), ex("Same whole", "1/2 of a large pizza is not the same amount of food as 1/2 of a small pizza. The whole matters."), ex("Same whole compare", "1/2 of a small cake and 1/3 of a large cake are unsafe to compare without knowing the wholes. Same whole first, then compare pieces."), ex("Related denominators", "1/2 + 1/4 works by turning 1/2 into 2/4, then 2/4 + 1/4 = 3/4."), ex("Unlike denominators", "3/4 - 1/6 cannot be subtracted until the parts match. Rename to twelfths: 9/12 - 2/12 = 7/12."), ex("Mixed number addition", "1 1/4 + 2/4 keeps the whole and combines fourths: 1 + 3/4 = 1 3/4."), ex("Set model mistake check", "2/3 of 18 is not 2÷3. Split 18 into 3 equal groups of 6, then take 2 groups: 12.")},
			[]gin.H{mis("Using one model for every fraction", "Choose the model from the job: number line for fraction number, set model for part of a count, common-denominator bars for operations."), mis("Calling improper fractions wrong", "Improper fractions are valid; they simply describe more than one whole."), mis("Thinking equivalent fractions are different amounts", "Equivalent fractions rename the same amount with a different part size."), mis("Adding denominators", "Denominators name part size. Same-sized parts keep the same denominator."), mis("Adding unlike fractions without renaming", "Rename to a common denominator so the parts are the same size."), mis("Subtracting denominators", "Subtract the counted parts, not the part names. 3/4 - 1/6 becomes 9/12 - 2/12."), mis("Ignoring the whole", "A fraction of a set and a fraction of one shape use the same notation but different wholes."), mis("Using the wrong model", "A bar or number line shows a fraction as a number; a set model shows a fraction of many objects."), mis("Unequal parts in shape problems", "Equal parts must be same-sized; unequal-looking slices are not a fraction model."), mis("Unequal groups in set problems", "3/4 of a set means 4 equal groups before taking 3 groups."), mis("Comparing only numerators", "Bigger numerator is not enough when denominators or wholes differ."), mis("Using the same denominator without knowing why", "The common denominator works because both fractions are renamed into same-sized pieces.")},
			[]gin.H{cq("Which model gate is complete?", "Do not start with a formula.", []string{"whole, equal parts, job, model", "numerator only", "largest number, answer", "colour, shape, page"}, "whole, equal parts, job, model", "Fractions need the whole, equal parts, the job, and the right model before calculation."), cq("9/4 is...", "Make wholes from fourths.", []string{"1 1/4", "2 1/4", "2 3/4", "9 1/4"}, "2 1/4", "8 fourths make 2 wholes, with 1 fourth left."), cq("2 3/5 = ?", "Convert wholes to fifths.", []string{"5/5", "10/5", "13/5", "23/5"}, "13/5", "2 wholes = 10 fifths. Add 3 fifths."), cq("Why does 1/2 = 2/4?", "Use a fraction bar idea.", []string{"Same length, smaller equal pieces", "2 is bigger than 1", "4 is bigger than 2", "They are never equal"}, "Same length, smaller equal pieces", "The amount covered is the same; only the part size changed."), cq("1/3 of 18 = ?", "Divide by denominator.", []string{"3", "6", "9", "15"}, "6", "18 split into 3 equal groups gives 6 in each group."), cq("Which model fits 3/5 of 25?", "The word of plus a count points to a set model.", []string{"set model", "protractor", "line graph", "compass"}, "set model", "3/5 of 25 means split 25 objects into 5 equal groups, then take 3 groups."), cq("On a number line, 9/4 is...", "Compare with 8/4.", []string{"less than 1", "exactly 2", "2 1/4", "4 1/2"}, "2 1/4", "8/4=2, with 1 fourth left."), cq("Which is greater?", "Use equivalent fractions.", []string{"2/3", "3/6", "They are equal", "Cannot tell"}, "2/3", "3/6 = 1/2, and 2/3 is greater than 1/2."), cq("Which is greater than 1/2?", "Use benchmark fractions.", []string{"3/8", "4/10", "5/8", "2/5"}, "5/8", "Half of eighths is 4/8, so 5/8 is more than half."), cq("1/6 + 1/3 = ?", "Use sixths.", []string{"2/9", "1/2", "2/6", "1/9"}, "1/2", "1/3 = 2/6. 1/6 + 2/6 = 3/6 = 1/2."), cq("3/4 - 1/6 = ?", "Rename both as twelfths.", []string{"2/10", "7/12", "2/4", "1/2"}, "7/12", "3/4 = 9/12 and 1/6 = 2/12, so 9/12 - 2/12 = 7/12."), cq("1 1/4 + 2/4 = ?", "Combine same-sized fourths.", []string{"1 3/4", "1 3/8", "3/8", "2 1/4"}, "1 3/4", "The whole stays 1. 1/4 + 2/4 = 3/4.")},
			[]gin.H{cq("A class has 32 pupils. 3/8 are in choir. How many?", "Fraction of a set.", []string{"8", "12", "20", "24"}, "12", "32 ÷ 8 = 4; 3 groups of 4 is 12."), cq("Which is equal to 2 1/3?", "Use thirds.", []string{"5/3", "7/3", "8/3", "9/3"}, "7/3", "2 wholes = 6 thirds; plus 1 third = 7 thirds."), cq("5/6 - 1/3 = ?", "Convert 1/3 to sixths.", []string{"4/3", "1/2", "3/6", "4/6"}, "1/2", "1/3 = 2/6, so 5/6 - 2/6 = 3/6 = 1/2."), cq("2/3 + 1/6 = ?", "Rename thirds as sixths.", []string{"3/9", "1/2", "5/6", "3/6"}, "5/6", "2/3 = 4/6. 4/6 + 1/6 = 5/6."), cq("Maya read 3/4 of a book. Ben read 1/6. How much more did Maya read?", "Compare same whole with twelfths.", []string{"2/10", "7/12", "4/10", "1/2"}, "7/12", "3/4 = 9/12 and 1/6 = 2/12, so Maya read 7/12 more."), cq("Which story matches 2/5 of 30?", "Use set meaning.", []string{"30 split into 5 groups, take 2 groups", "30 plus 2 plus 5", "2 split into 30 groups", "5 groups of 30 twice"}, "30 split into 5 groups, take 2 groups", "The denominator tells how many equal groups; numerator tells how many to take."), cq("Which comparison is unsafe without more information?", "Check the whole.", []string{"1/2 of a small cake vs 1/2 of a big cake", "3/4 vs 2/4 of the same bar", "5/5 vs 1 whole of the same bar", "1/3 vs 2/3 of the same strip"}, "1/2 of a small cake vs 1/2 of a big cake", "The fraction is the same, but the whole is different."), cq("Which explanation is best for 2/3 of 18?", "Set model.", []string{"18 ÷ 3 = 6, then 2 groups make 12", "18 ÷ 2 = 9, then add 3", "2 ÷ 3", "18 + 2 + 3"}, "18 ÷ 3 = 6, then 2 groups make 12", "The denominator makes equal groups first; the numerator tells how many groups to take."), cq("Why use twelfths for 3/4 - 1/6?", "Common denominator meaning.", []string{"Twelfths can rename both fourths and sixths", "12 is bigger than both numerators", "All answers must use 12", "It makes the answer larger"}, "Twelfths can rename both fourths and sixths", "A common denominator creates same-sized pieces before subtracting.")},
			cq("A recipe uses 1 3/4 cups of flour. Maya doubles it. What is the best first move?", "Choose a representation that makes doubling clear.", []string{"Double only the 3", "Turn 1 3/4 into 7/4 or double wholes and parts carefully", "Add denominators", "Round to 1 cup"}, "Turn 1 3/4 into 7/4 or double wholes and parts carefully", "1 3/4 can be doubled as 7/4 × 2 = 14/4 = 3 1/2, or 1+1 and 3/4+3/4.")), true
	case "P4-DECIMALS":
		return mergePack(base,
			"Decimals are fractions written in the place-value system.",
			"P4 decimals should connect tenths, hundredths, thousandths, money and measurement to place value. The key is not the decimal point itself, but what each place means and which unit the decimal is measuring. Treat the decimal point like a place-value fence: digits left and right of the fence have different jobs. 0.305 is 305 thousandths, not the whole number 305.",
			"decimal",
			[]string{"Use the decimal gate before calculating: unit, place, estimate, format.", "Tenths, hundredths, and thousandths continue the base-ten pattern after ones.", "The decimal point is a place-value fence; moving across it changes the place name and value.", "Tenths are bigger than hundredths, and hundredths are bigger than thousandths.", "Trailing zeros after a decimal can keep the value the same, such as 0.6 = 0.60 = 0.600.", "Decimal comparison works left to right by place value, not by counting digits.", "Decimal operations need aligned decimal points because places must match.", "Decimal × whole number means repeated equal decimal groups.", "Decimal ÷ whole number means sharing a decimal amount equally.", "Money uses two decimal places: dollars before the point, cents after the point.", "Money can be checked in cents: $6.87 = 687 cents and $10.00 = 1000 cents.", "A cents answer is an intermediate check, not the final money format.", "The final answer gate checks unit, symbol, and decimal places before the answer is finished.", "In change problems, change = amount paid - cost.", "An estimate tells where the decimal point should be before exact calculation."},
			[]string{"Use the decimal gate: name the unit, read the place, estimate the size, then format the answer.", "Read the decimal by place value.", "Name the unit: number, metre, litre, dollar, or cents.", "Rename with zeros when helpful, such as 0.6 = 0.60 or $10 = $10.00.", "Compare from ones, then tenths, then hundredths, then thousandths.", "For money, read cents as hundredths of a dollar.", "For money answers, write the dollar sign and exactly two decimal places.", "Line up decimal points for addition and subtraction.", "Regroup $10.00 as $9 and 100 cents when subtracting money like $6.87.", "Or convert to cents first, subtract, then convert back to dollars.", "If you use cents, convert back: 313 cents = $3.13, not $313.", "Run the final answer gate: does the answer have the requested unit, symbol, and format?", "For decimal × whole number, think repeated equal decimal groups and estimate the size.", "For decimal ÷ whole number, share equally and check with multiplication.", "Estimate before exact calculation.", "Connect simple decimals to fractions: 0.5=1/2, 0.25=1/4, 0.75=3/4."},
			[]gin.H{ex("Decimal gate", "Before calculating, ask: What unit is this? Which place values are used? What size should the answer be? What format should the final answer use?"), ex("Place-value fence", "The decimal point is a fence: dollars or ones are on the left, tenths and hundredths are on the right. Do not erase the fence when comparing or writing money."), ex("Place value", "4.307 means 4 ones, 3 tenths, 0 hundredths, and 7 thousandths."), ex("Thousandths", "0.305 is 305 thousandths. Compare it as 0.305, not as the whole number 305."), ex("Trailing zeros", "0.6 = 0.60 = 0.600 because 6 tenths, 60 hundredths, and 600 thousandths cover the same amount."), ex("Compare", "0.9 > 0.12 because 0.9 = 0.90, and 90 hundredths is greater than 12 hundredths."), ex("Add", "3.6 + 1.27 = 3.60 + 1.27 = 4.87."), ex("Money change", "A toy costs $6.87 and Maya pays $10.00. Change is $10.00 - $6.87 = $3.13, not 313."), ex("Money regrouping", "$10.00 - $6.87 can be seen as $9 and 100 cents minus $6 and 87 cents, leaving $3 and 13 cents."), ex("Cents check", "$10.00 is 1000 cents and $6.87 is 687 cents. 1000-687=313 cents, which is $3.13."), ex("Cents are not final format", "313 cents is a useful check, but a money answer should be written as $3.13. The unit and format finish the problem."), ex("Final answer gate", "After calculating money, ask: Does it have a dollar sign? Does it have two decimal places? Is the cents check converted back? If yes, $3.13 is finished; 313 is not."), ex("Decimal multiplication", "2.4 × 3 means 2.4 + 2.4 + 2.4 = 7.2. The estimate is about 2×3 or 3×3, so 7.2 is reasonable."), ex("Decimal division", "4.8 ÷ 4 means share 4.8 into 4 equal parts. Each part is 1.2, and 1.2 × 4 checks back to 4.8."), ex("Fraction link", "0.25 = 25 hundredths = 25/100 = 1/4."), ex("Measurement link", "1.25 m means 1 metre and 25 hundredths of a metre, so the unit still matters.")},
			[]gin.H{mis("More decimal digits means bigger", "0.9 is bigger than 0.123 because tenths are more powerful than hundredths and thousandths."), mis("Treating decimals as whole numbers", "0.305 is 305 thousandths, not 305 ones."), mis("Not aligning decimal points", "Digits must stay in their place-value columns."), mis("Reading 0.04 as four tenths", "0.04 is four hundredths; 0.4 is four tenths."), mis("Dropping the decimal point in money", "$3.13 is 3 dollars and 13 cents. 313 is not a clear money answer."), mis("Writing cents as dollars", "313 cents must be converted back to $3.13, not written as $313."), mis("Stopping at the cents check", "A cents check like 313c is not the final answer unless the question asks for cents."), mis("Forgetting cents need two decimal places", "$3.1 should be written as $3.10 when talking about money."), mis("Placing the decimal point without estimating", "In 2.4×3, 72 is too large and 0.72 is too small; estimate shows the answer should be around 7."), mis("Forgetting inverse check for decimal division", "If 4.8÷4=1.2, then 1.2×4 must rebuild 4.8."), mis("Forgetting estimate", "An answer like 36.2 for 3.6+1.27 should feel too large.")},
			[]gin.H{cq("What is the value of 7 in 2.375?", "Name the decimal place.", []string{"7 ones", "7 tenths", "7 hundredths", "7 thousandths"}, "7 hundredths", "After the point: tenths, hundredths, thousandths."), cq("0.305 means...", "Read by place value.", []string{"305 ones", "305 thousandths", "35 hundredths", "3 tenths only"}, "305 thousandths", "The last digit is in the thousandths place, so 0.305 is 305 thousandths."), cq("Which is greatest?", "Use equal decimal places.", []string{"0.8", "0.75", "0.708", "0.079"}, "0.8", "0.8 = 0.800, the greatest."), cq("Why is 0.6 = 0.600?", "Trailing zeros keep the value.", []string{"They cover the same amount with smaller place units", "More digits always means bigger", "600 is bigger than 6", "They are never equal"}, "They cover the same amount with smaller place units", "6 tenths is the same amount as 600 thousandths."), cq("0.6 + 0.25 = ?", "Rename 0.6.", []string{"0.31", "0.65", "0.85", "0.925"}, "0.85", "0.60+0.25=0.85."), cq("4.5 - 1.28 = ?", "Line up decimal points.", []string{"3.22", "3.32", "3.28", "2.22"}, "3.22", "4.50-1.28=3.22."), cq("2.4 × 3 = ?", "Think repeated decimal groups.", []string{"0.72", "6.4", "7.2", "72"}, "7.2", "2.4+2.4+2.4=7.2. Estimate: a little more than 2×3."), cq("4.8 ÷ 4 = ?", "Share equally, then check.", []string{"0.12", "1.2", "12", "1.4"}, "1.2", "1.2×4=4.8, so each equal part is 1.2."), cq("A toy costs $6.87. Maya pays $10.00. Change?", "Paid minus cost.", []string{"$3.13", "$3.23", "$293", "$313"}, "$3.13", "$10.00 - $6.87 = $3.13. The decimal point separates dollars and cents."), cq("Which decimal gate is correct for money change?", "Finish with money format.", []string{"unit: dollars/cents; place: hundredths; estimate: about $3; format: $3.13", "unit: none; place: whole number; estimate: 313; format: 313", "only subtract digits", "remove the decimal point"}, "unit: dollars/cents; place: hundredths; estimate: about $3; format: $3.13", "Money answers need unit, place value, estimate, and final format."), cq("The cents check gives 313c. Which final answer passes the final answer gate?", "Use requested money format.", []string{"313", "$313", "$3.13", "$3.1"}, "$3.13", "The final answer gate checks symbol and two decimal places. 313c becomes $3.13."), cq("Round 6.47 to the nearest tenth.", "Look at hundredths.", []string{"6.4", "6.5", "6.47", "7.0"}, "6.5", "The hundredths digit is 7, so 6.4 rounds up to 6.5.")},
			[]gin.H{cq("Which is smaller: 3.09 or 3.1?", "Rename 3.1 as 3.10.", []string{"3.09", "3.1", "They are equal", "Cannot tell"}, "3.09", "3.09 is 3 and 9 hundredths; 3.10 is 3 and 10 hundredths."), cq("A ribbon is 2.35 m. Another is 1.8 m. Total?", "Line up decimal points.", []string{"3.15 m", "4.15 m", "4.13 m", "20.15 m"}, "4.15 m", "2.35 + 1.80 = 4.15."), cq("$12.50 - $4.75 = ?", "Regroup one dollar as 100 cents.", []string{"$7.75", "$8.25", "$7.85", "$775"}, "$7.75", "$12.50 - $4.75 = $7.75. Keep two decimal places for money."), cq("$10.00 - $6.87 starts by regrouping as...", "Use dollars and cents.", []string{"$9 and 100 cents", "$10 and 87 cents", "$6 and 100 cents", "$3 and 13 cents before subtracting"}, "$9 and 100 cents", "To subtract 87 cents, trade $1 from $10 into 100 cents."), cq("1000 - 687 = 313 cents. What is the final money answer?", "Convert cents back to dollars.", []string{"$313", "$31.30", "$3.13", "313 dollars"}, "$3.13", "313 cents is 3 dollars and 13 cents, so write $3.13."), cq("3.6 × 4 = ?", "Repeated groups and estimate.", []string{"1.44", "14.4", "144", "7.6"}, "14.4", "3.6×4 is close to 4×4=16, and exact repeated addition gives 14.4."), cq("9.6 ÷ 4 = ?", "Share and check.", []string{"2.4", "24", "0.24", "3.2"}, "2.4", "2.4×4=9.6, so 9.6÷4=2.4."), cq("6 ÷ 10 = ?", "Dividing by 10 moves one place right into tenths.", []string{"60", "0.6", "0.06", "6.10"}, "0.6", "6 ones become 6 tenths."), cq("Which estimate fits 9.8 - 4.95?", "Use friendly numbers.", []string{"about 1", "about 5", "about 10", "about 15"}, "about 5", "9.8 is about 10 and 4.95 is about 5."), cq("Which answer has a decimal point problem for money?", "Money needs dollars and cents.", []string{"$3.13", "$7.75", "$313", "$2.40"}, "$313", "$313 means three hundred thirteen dollars, not three dollars and thirteen cents.")},
			cq("A child says 0.305 is greater than 0.35 because 305 is greater than 35. What should you say?", "Correct using place value.", []string{"Correct, longer number wins", "Compare as 0.305 and 0.350, so 0.35 is greater", "Remove the decimal point", "Both are equal"}, "Compare as 0.305 and 0.350, so 0.35 is greater", "Adding a zero shows 0.350 has 350 thousandths, more than 305 thousandths.")), true
	case "P4-AREA-PERIMETER":
		return mergePack(base,
			"Area and perimeter are two different stories about the same shape: covering inside or walking outside.",
			"P4 area/perimeter is not just formulas. The child first makes a measure decision: cover inside means area; walk outside means perimeter. Then she chooses the model: rectangle formula, square side reverse, composite split/subtract, or perimeter trace with missing sides. Same area can have different perimeter, and same perimeter can enclose different areas.",
			"areaComposite",
			[]string{"The measure decision comes first: cover inside or walk outside.", "Area counts square units that cover the inside of a figure.", "Perimeter counts length units along the outside boundary only.", "Rectangle area is length × width because it counts rows of equal square units.", "A square side can be found from perimeter by dividing by 4.", "A square side can be found from area by asking which number times itself gives the area.", "Composite area can be found by splitting into rectangles or subtracting a missing rectangle.", "Composite perimeter is not a formula hunt; it is an outside trace.", "Missing-side work comes before perimeter addition.", "Horizontal outside lengths must balance to the full width; vertical outside lengths must balance to the full height.", "Internal split lines help us calculate area but must not be added to perimeter.", "Missing sides can often be found from equal opposite lengths, total width/height, or total perimeter.", "Equal area does not mean equal perimeter.", "Tracing perimeter means start at one corner and return to the start without walking any inside split line."},
			[]string{"Ask: Is the story covering inside or walking outside?", "Circle the story clue: cover/fill/paint/tile means area; fence/border/ribbon/walk around means perimeter.", "Draw the shape and label known lengths.", "For a rectangle, use rows × columns.", "For a square from perimeter, divide by 4; for a square from area, find the repeated factor.", "For composite area, split into rectangles or subtract a missing rectangle.", "For composite perimeter, find missing outside sides before adding.", "For a missing horizontal outside side, use full horizontal length minus known horizontal parts.", "For a missing vertical outside side, use full vertical height minus known vertical parts.", "Trace with your finger around the outside boundary only.", "Use an outside-walk ledger: start corner, walk clockwise, record each outside side, return to start.", "Make a perimeter ledger: list each outside side once, in walking order.", "Cross out internal split lines before adding perimeter.", "Check units: area uses square units, perimeter uses length units.", "Write an equation for missing dimensions before calculating."},
			[]gin.H{ex("Measure decision", "If the story says tile, paint, cover, floor or surface, it asks for area. If it says fence, border, ribbon or walk around, it asks for perimeter."), ex("Same rectangle, two questions", "A 9 cm by 4 cm rectangle has area 9×4=36 cm², but perimeter 9+4+9+4=26 cm. Same shape, different question."), ex("Same area, different perimeter", "A 6×4 rectangle and an 8×3 rectangle both have area 24 cm². Their perimeters are 20 cm and 22 cm, so equal area does not mean equal perimeter."), ex("Same perimeter, different area", "A 6×4 rectangle and a 7×3 rectangle both have perimeter 20 cm, but their areas are 24 cm² and 21 cm²."), ex("Square from perimeter", "A square with perimeter 36 cm has side 36÷4=9 cm because all four sides are equal."), ex("Square from area", "A square with area 49 cm² has side 7 cm because 7×7=49."), ex("Split for area", "An L-shape can become a 6×4 rectangle plus a 3×2 rectangle: 24+6=30 cm²."), ex("Subtract for area", "The same L-shape can be a 6×6 rectangle minus a 3×2 missing corner: 36-6=30 cm²."), ex("Missing-side balance", "Before tracing perimeter, rebuild missing outside lengths. Horizontal parts must add to the full width; vertical parts must add to the full height."), ex("Trace for perimeter", "For that L-shape, start at one corner and return to the start, walking only the outside: 6+6+3+2+3+4=24 cm. The inside split line is not part of the boundary."), ex("Perimeter ledger", "Write an outside-walk ledger in order, such as top + right + bottom + step + inner vertical outside + left. Each outside side appears once; the split line can help area but is crossed out for perimeter."), ex("Missing outside side", "If a full top length is 12 cm and one top part is 7 cm, the missing top part is 12-7=5 cm."), ex("Missing vertical side", "If the total height is 10 cm and two vertical parts are 6 cm and 3 cm, the missing vertical part is 10-6-3=1 cm."), ex("Missing rectangle side", "If rectangle area is 48 cm² and one side is 6 cm, the other side is 48÷6=8 cm because 6×?=48.")},
			[]gin.H{mis("Choosing a formula before the measure", "Decide cover inside or walk outside before calculating."), mis("Using cm² for perimeter", "Perimeter is distance, so use cm or m."), mis("Adding internal split lines to perimeter", "Only the outside border counts. Split lines are calculation helpers, not boundary."), mis("Using area formula for every story", "First decide what is being measured: covering or walking around."), mis("Counting squares for perimeter", "Perimeter is side length around the edge, not the number of inside squares."), mis("Forgetting missing sides", "Composite perimeter often requires finding unlabeled outside sides."), mis("Skipping the outside-walk ledger", "Without a walking list, children often miss a side or count an internal split line."), mis("Thinking equal area gives equal perimeter", "Equal area does not mean equal perimeter. The shape can be stretched longer and thinner."), mis("Thinking equal perimeter gives equal area", "The same outside distance can enclose different inside amounts."), mis("Finding square side from area by dividing by 4", "Divide by 4 works for square perimeter, not square area. For area, find side×side."), mis("Treating every labelled line as outside perimeter", "Labels on split lines can help area, but only outside boundary belongs to perimeter.")},
			[]gin.H{cq("A rectangle is 8 cm by 3 cm. Area?", "Inside squares.", []string{"11 cm²", "22 cm²", "24 cm²", "24 cm"}, "24 cm²", "Area = 8×3 = 24 cm²."), cq("A square side is 5 cm. Perimeter?", "Four equal sides.", []string{"10 cm", "20 cm", "25 cm²", "25 cm"}, "20 cm", "5+5+5+5=20 cm."), cq("A square has perimeter 36 cm. Side length?", "Divide perimeter by 4.", []string{"6 cm", "8 cm", "9 cm", "144 cm"}, "9 cm", "A square has 4 equal sides, so 36÷4=9 cm."), cq("A square has area 49 cm². Side length?", "Find side×side.", []string{"7 cm", "12.25 cm", "14 cm", "196 cm"}, "7 cm", "7×7=49, so each side is 7 cm."), cq("Which question asks for area?", "Think covering.", []string{"Fence around a garden", "Ribbon around a card", "Tiles covering a floor", "Distance around a field"}, "Tiles covering a floor", "Tiles cover the inside surface."), cq("Which question asks for perimeter?", "Think walking around.", []string{"Paint a wall", "Cover a table", "Put ribbon around a card", "Fill a square with tiles"}, "Put ribbon around a card", "Ribbon follows the outside boundary."), cq("A rectangle has area 42 cm² and length 7 cm. Width?", "Undo multiplication.", []string{"6 cm", "7 cm", "35 cm", "49 cm"}, "6 cm", "42÷7=6."), cq("A 6×4 rectangle and an 8×3 rectangle both have area 24 cm². What can be different?", "Area and perimeter are different measures.", []string{"perimeter", "number of square units", "both areas", "the unit cm²"}, "perimeter", "They cover the same area, but the outside boundary length can differ."), cq("When finding composite perimeter, an internal split line should be...", "Trace outside only.", []string{"added twice", "ignored", "multiplied", "used as cm²"}, "ignored", "Internal split lines are not outside boundary.")},
			[]gin.H{cq("A 12×8 rectangle has a 4×3 piece removed. Area left?", "Subtract the cut-out.", []string{"60 cm²", "84 cm²", "96 cm²", "108 cm²"}, "84 cm²", "12×8=96; 4×3=12; 96-12=84."), cq("An L-shape is split into 7×4 and 3×5 rectangles. Area?", "Add the two inside rectangles.", []string{"19 cm²", "34 cm²", "43 cm²", "70 cm²"}, "43 cm²", "7×4=28 and 3×5=15, so total area is 43 cm²."), cq("A full width is 14 cm. One top part is 9 cm. Missing top part?", "Use total minus known part.", []string{"5 cm", "9 cm", "14 cm", "23 cm"}, "5 cm", "14-9=5 cm."), cq("A total height is 10 cm. Two outside vertical parts are 6 cm and 3 cm. Missing vertical part?", "Use total height minus known parts.", []string{"1 cm", "3 cm", "9 cm", "19 cm"}, "1 cm", "10-6-3=1 cm."), cq("A rectangle perimeter is 30 cm. One side is 9 cm. Other side?", "Use 2L+2W.", []string{"3 cm", "6 cm", "12 cm", "21 cm"}, "6 cm", "30-18=12 for two widths, so each width is 6."), cq("A shape is split into two rectangles to find area. Does the split line count in perimeter?", "Inside line or outside boundary?", []string{"Yes, always", "No, unless it is on the outside boundary", "Only if it is red", "Only for squares"}, "No, unless it is on the outside boundary", "Perimeter is the outside walk only."), cq("A garden needs fencing but not flooring. Find...", "Use story meaning.", []string{"area", "perimeter", "volume", "average"}, "perimeter", "Fencing goes around the outside."), cq("A floor needs tiles. Which measure matters?", "Covering inside.", []string{"area", "perimeter", "angle", "line symmetry"}, "area", "Tiles cover the inside surface, so use area."), cq("Which outside-walk ledger is safest?", "Do not miss or double-count edges.", []string{"start corner, walk outside, list each side once, return to start", "add every line you see", "count inside squares", "multiply all labels"}, "start corner, walk outside, list each side once, return to start", "A ledger is a walking list of outside sides only, written in order.")},
			cq("A child finds area by splitting an L-shape into two rectangles, then adds the split line to the perimeter too. What is the mistake?", "Separate calculation helper from boundary.", []string{"The split line helped area but is not part of the outside boundary", "All split lines must be counted twice", "Area and perimeter are always equal", "The shape cannot be split"}, "The split line helped area but is not part of the outside boundary", "A split line can be useful for area, but perimeter only walks the outside edge.")), true
	case "P4-GEOMETRY-ANGLES":
		return mergePack(base,
			"Angles are direction + structure: turn decisions, angle notation, and shape properties must connect.",
			"P4 angles should build the same model each time: first choose the job (name, measure, draw, turn, or explain structure), then use one proof path (notation, protractor, compass step ledger, or shape properties). A true answer tells what is measured or constrained and why, not just which number you remember.",
			"angle",
			[]string{"Angles are turn sizes: they describe a change from one ray to another about one vertex.", "Every angle question has a job: name, measure, draw, turn, or use properties.", "A good answer names the evidence: vertex, baseline and 0° scale choice, compass steps, or shape rule.", "A full turn is 360°, a half turn is 180°, and a right angle is 90°.", "In ∠ABC, B is the vertex; the middle letter names the corner being measured.", "A protractor has two scales; the correct scale is the one that starts at the base ray's 0°.", "An 8-point compass splits a full turn into 8 equal steps of 45°.", "Turn ledger prevents direction mistakes: start direction, path direction, jump count, then degree.", "Clockwise and anticlockwise from the same points can differ; step-by-step path decides the answer.", "Rectangles and squares have four right angles; rectangles add opposite sides equal and parallel, squares add all sides equal.", "Drawing angles needs a baseline and the right protractor scale before taking the number.", "For shape claims, avoid judging only by appearance: use a property proof and counter-check with one example."},
			[]string{"Step one: choose the job the question is asking for.", "Angle naming step: find the vertex first (the middle letter in the notation).", "Measurement step: place the protractor center on the vertex and align one arm to 0°.", "Scale step: if the protractor has two scales, use the 0° line that matches the drawn base ray.", "Drawing step: draw a base ray first, then mark the target degree on the correct scale.", "Compass step: count jumps between compass points, not just labels.", "Direction step: write clockwise or anticlockwise before counting.", "Verification step: convert the jump count to degrees and compare to benchmarks (acute/right/obtuse/supplement).", "Shape property step: for rectangle check 4 right angles + opposite equal and parallel.", "Shape property step: for square check rectangle rules plus 4 equal sides.", "For any answer, say the reason in one short sentence: why this number or property is valid.", "If two clues conflict, the protractor / ledger / geometry rule overrides appearance."},
			[]gin.H{ex("job decision", "Ask: am I naming, measuring, drawing, turning, or using shape properties? Every angle question has a job: name, measure, draw, turn, or use properties. Before calculating, name the job: angle name, angle measure, draw an angle, compass turn, or shape property. Ask: what job is this question asking me to do? Point to the evidence before answering."), ex("Evidence chain", "A good answer names the evidence: vertex, base 0° scale, compass steps, or shape property. Name uses the vertex. Measure or draw uses the protractor 0° scale."), ex("angle notation", "In ∠ABC, B is the vertex. The middle letter names the corner. Which angle name has vertex Q? Choose an angle whose middle letter is Q."), ex("Protractor scales", "A protractor has two scales. Start at the base 0° scale; an acute angle cannot read 130°."), ex("Turn ledger", "A turn ledger prevents compass mistakes: start direction, path direction, landing points, step count, then degrees. If it is compass turning, write a turn ledger: start, direction, landing points, step count, degrees. NE→E→SE→S. There are 3 jumps, not 4 labels. For NE to S clockwise: NE→E→SE→S, 3 jumps, 135°. S to NE anticlockwise also needs its own jump path."), ex("Drawing and proof", "To draw an 8 cm by 5 cm rectangle, make four right-angle corners and keep opposite sides parallel and equal. A shape is a rectangle only if it has four right angles. A square must pass both tests: four right angles and four equal sides. Draw four 4 cm sides and check every corner is a right angle. A rough drawing is not enough: Four 4 cm sides and four 90° corners are the evidence."), ex("Job decision", "Angle problems can ask for name, measure, drawing, turning, or structure checks. Different jobs use different evidence. A question: what is it asking for first: name, measure, draw, turn, or prove shape?"), ex("Angle name", "In ∠ABC, BA and BC are the two arms and B is the vertex in the middle."), ex("Compass turn", "N to E is 2 steps on the 8-point compass: N→NE→E, so 2×45°=90°."), ex("Rectangle proof", "A rectangle must have 4 right angles and opposite sides equal and parallel."), ex("Square proof", "A square has all rectangle rules + all 4 sides equal. Missing any part is not a square."), ex("Depth of explanation", "An angle size claim is complete when you also say how it is measured or proved.")},
			[]gin.H{mis("Counting compass labels instead of jumps", "NE→E→SE→S has three jumps. Count the moves between landing points, not the labels you can see."), mis("A rough drawing is not enough", "Use the property proof: right angles, equal sides where needed, and opposite sides parallel and equal."), mis("Naming the wrong vertex", "In ∠ABC, B is the vertex because B is in the middle."), mis("Reading the wrong protractor scale", "Start from the 0° line your base ray uses; an acute angle cannot jump to 130° by a single arm."), mis("Forgetting the protractor center", "The centre must be exactly at the vertex point."), mis("Judging only by appearance", "A rough sketch can hide wrong size; use the given measurement/proof path."), mis("Confusing length with size", "Longer arms do not mean a bigger angle."), mis("Counting compass labels as 90°", "In an 8-point compass each neighbouring label is 45° apart."), mis("Using labels instead of jumps", "NE→E→SE→S uses three jumps to S, even though you can read four labels in between."), mis("Ignoring direction", "Clockwise and anticlockwise produce different step sequences and degrees."), mis("Forgetting square rules", "A square is rectangle rule plus equal sides."), mis("Drawing a rectangle by sight", "Use right-angle and opposite-side checks before trusting how it looks."), mis("Calling every rectangle a square", "A rectangle is not a square unless all sides are equal."), mis("Assuming split angles are always half/quarter", "The split depends on given clues or protractor proof, not the sketch style.")},
			[]gin.H{cq("In ∠ABC, which point is the vertex?", "Middle letter is the vertex.", []string{"A", "B", "C", "All three"}, "B", "The angle is measured at B between BA and BC."), cq("A right angle is...", "Benchmark value.", []string{"45°", "90°", "180°", "360°"}, "90°", "One right angle is a quarter turn, so 90°."), cq("An angle smaller than 90° is...", "Angle type.", []string{"acute", "right", "obtuse", "straight"}, "acute", "Acute means less than 90°."), cq("A straight angle is...", "Half turn language.", []string{"90°", "120°", "180°", "360°"}, "180°", "A straight line is a half-turn."), cq("Drawing is rough but the question asks for angle name; what is the vertex?", "Choose the middle letter first.", []string{"A", "B", "C", "None"}, "B", "∠ABC means B is the vertex."), cq("An angle looks acute, but the protractor reading says 130°. What should you check first?", "Use angle type as a guardrail.", []string{"The arm is too long", "Wrong protractor scale", "The protractor is too small", "The question is wrong"}, "Wrong protractor scale", "Acute means less than 90°. 130° usually means a different scale line was used."), cq("On an 8-point compass, N to E clockwise is...", "2 jumps of 45°", []string{"45°", "90°", "135°", "180°"}, "90°", "N→NE→E is 2 jumps of 45° each."), cq("Which turn ledger is correct for NE to S clockwise?", "Count jumps between points.", []string{"NE→E→SE→S, 3 jumps, 135°", "NE→E→SE→S, 4 jumps, 180°", "NE→N→NW→W, 3 jumps, 135°", "NE→S, 1 jump, 45°"}, "NE→E→SE→S, 3 jumps, 135°", "A compass path is jump-by-jump: N-E-S is NE→E→SE→S."), cq("A rectangle has...", "Shape property check.", []string{"4 right angles", "3 equal sides", "no parallel sides", "curved sides"}, "4 right angles", "Every rectangle has exactly four 90° angles."), cq("What matters when measuring an angle?", "Angle is a turn.", []string{"Length of arms", "Turn between arms", "Colour of line", "Page number"}, "Turn between arms", "Angle size is opening size from one arm to the other."), cq("A square is also a...", "Property compare.", []string{"rectangle", "circle", "triangle", "line graph"}, "rectangle", "A square is a special rectangle with extra equal-side constraint."), cq("To draw a rectangle accurately, what must you check?", "Use geometric rule first.", []string{"4 right angles, opposite equal sides, and parallel opposite sides", "only one long side", "curved corners", "8 compass directions"}, "4 right angles, opposite equal sides, and parallel opposite sides", "A rectangle is a right-angle shape with equal opposite pairs."), cq("A child turns from North to South-East and says it is 90°. What should she check?", "Use compass jumps.", []string{"The number of 45° jumps", "Whether North is a shape", "Whether all turns are 90°", "The length of the arrow"}, "The number of 45° jumps", "N→NE→E→SE is 3 jumps, so 135°."), cq("S to N clockwise vs anticlockwise", "Direction changes the steps.", []string{"Clockwise: one path", "Anticlockwise: different path", "Same steps", "Direction never changes"}, "Anticlockwise: different path", "Direction changes the sequence of jumps and therefore the value.")},
			[]gin.H{cq("Which job should you do first for ∠WXY?", "Read the question stem.", []string{"Name the vertex", "Read job first: name/measure/draw/turn/property", "Write answer immediately", "Choose clockwise"}, "Read job first: name/measure/draw/turn/property", "A naming job uses ∠ notation; a measure job needs protractor path; a drawing job needs baseline and scale; a turn job uses compass steps; a property job uses shape rules."), cq("A compass says N to S clockwise through 8-point points. What is the angle?", "Count jumps.", []string{"90°", "135°", "180°", "45°"}, "180°", "N→NE→E→SE→S is 4 jumps, so 4×45°=180°."), cq("In ∠ABC, which point is the vertex?", "Middle letter.", []string{"A", "B", "C", "All three"}, "B", "The central point in angle notation is the vertex."), cq("Which is the correct reading of 120° drawing on protractor?", "Anchor the baseline first.", []string{"Match one arm to 0° line", "Read opposite labels", "Guess from the picture", "Count arm length"}, "Match one arm to 0° line", "Wrong baseline alignment causes 120° to look right by accident."), cq("S to NE anticlockwise is how many steps on 8-point compass?", "Count the jump chain.", []string{"90°", "135°", "180°", "225°"}, "135°", "S→SE→E→NE is 3 jumps, each 45°."), cq("A rectangle has all...", "Shape structure.", []string{"3 right angles", "4 right angles", "2 parallel lines only", "a circle"}, "4 right angles", "Rectangle needs four right angles, plus opposite sides equal and parallel."), cq("A square has exactly...", "Square rule.", []string{"3 equal sides", "4 right angles plus 4 equal sides", "2 right angles", "Only opposite equal sides"}, "4 right angles plus 4 equal sides", "Rectangles are right-angle four-sided shapes; squares are equal-sided rectangles."), cq("Which is a safe protractor reading step?", "Choose the base ray as 0° anchor.", []string{"Read the big number first", "Align base ray with chosen 0° scale", "Count labels only", "Ignore line thickness"}, "Align the base ray with chosen 0° scale", "The protractor result is only valid if the chosen base is truly the 0° line."), cq("Why can two different scale readings give different values?", "Scale matching.", []string{"Because there are two rays", "Because two 0° scales exist", "Because of angle size", "Because of unit"}, "Because there are two 0° scales", "Both scales are valid only when baseline and direction are fixed."), cq("A child says S to NE anticlockwise is 225°. What should be checked?", "Benchmark logic.", []string{"Use 45° steps", "Use 90° only", "Use straight-angle rule", "Use right angle only"}, "Use 45° steps", "S to NE anticlockwise is 3 steps = 135°, not 225°."), cq("To prove a shape is a rectangle, which three checks must work first?", "Structure-first proof.", []string{"Right angles and opposite sides equal/parallel", "Only looks similar", "Equal area", "Any diagonals"}, "Right angles and opposite sides equal/parallel", "Proof order controls correctness before labels or numbers."), cq("A shape has 4 right angles and opposite sides equal but all sides not equal. What is it?", "Classification.", []string{"square", "rectangle", "trapezium", "kite"}, "rectangle", "Equal opposite sides plus right angles is rectangle.")},
			cq("A child draws a shape that looks like a rectangle but one corner is not 90°. Is it a rectangle?", "Use the rule, not appearance.", []string{"Yes, because it looks close", "No, a rectangle must have 4 right angles", "Only if opposite sides are equal", "Only if area is right"}, "No, a rectangle must have 4 right angles", "Even one right-angle mistake means it is not a rectangle until corrected.")), true
	case "P4-GEOMETRY-SYMMETRY":

		return mergePack(base,
			"Line symmetry means one half can fold exactly onto the other.",
			"P4 symmetry is visual but still logical: the line is a mirror, every point needs a matching point the same distance away on the other side. Completing a symmetric figure means copying anchor points, not redrawing by eye. Use a two-pass check: outline first, then inner details. The check is same shape, same size, same distance, opposite side.",
			"symmetry",
			[]string{"A line of symmetry divides a figure into two matching halves.", "Matching points are the same distance from the mirror line.", "A point on the mirror line reflects to itself; it stays on the line.", "A shape can have zero, one, or many lines of symmetry.", "Completing a symmetric figure is copying with distance and direction reversed.", "Anchor points create the skeleton: copy corner, endpoint and dot positions first, then connect.", "For a vertical mirror line, left/right changes side but up/down stays the same.", "For a horizontal mirror line, up/down changes side but left/right stays the same.", "Corners, endpoints, dots and inner marks must all reflect.", "Outline match is only the first pass; inner details and colours must match too.", "A repeating pattern can be symmetric only if the shapes and colours match across the mirror line."},
			[]string{"Imagine folding along the proposed line.", "Check whether both halves match exactly.", "For grid symmetry, count squares from the mirror line.", "Choose anchor points such as corners and endpoints.", "Label original anchor points A, B, C before copying A', B', C'.", "Place each matching point the same distance on the opposite side.", "If the point is on the mirror line, leave it on the mirror line.", "Connect matching points in the same order as the original side.", "Do a two-pass check: outline first, then dots, holes, colours and inner marks.", "Check corners, inner details, and pattern colours, not just the outline size."},
			[]gin.H{ex("Vertical symmetry", "A heart-like shape may fold left to right if both sides match exactly."), ex("Square", "A square has 4 lines of symmetry: vertical, horizontal, and two diagonals."), ex("Rectangle", "A non-square rectangle has 2 lines of symmetry: vertical and horizontal."), ex("Grid completion", "If a point is 3 squares left of the mirror line, its partner is 3 squares right."), ex("Point on the mirror", "A point on the mirror line reflects to itself. It does not move to either side."), ex("Horizontal mirror line", "If a point is 2 squares above a horizontal mirror line and 3 squares right of a marker, the reflected point is 2 squares below and still 3 squares right."), ex("Anchor skeleton", "Copy corners, endpoints and dots first. Then connect A' to B' to C' in the same order as A to B to C. If the order changes, the reflected shape can become wrong."), ex("Anchor points", "To complete a half-shape, copy each corner first. A point 2 squares left and 4 squares up must become 2 squares right and 4 squares up."), ex("Inner detail check", "If the outline matches but a dot is 2 squares from the mirror on the left and 4 squares from the mirror on the right, the whole figure is not symmetric."), ex("Pattern symmetry", "If the left side has a blue triangle 1 square from the mirror, the right side needs the same blue triangle 1 square from the mirror.")},
			[]gin.H{mis("Almost matching is accepted", "Symmetry requires exact matching, not just similar-looking halves."), mis("Only checking area", "Both halves can have equal area but different shapes, so same area is not enough."), mis("Copying in the same direction", "A mirror reverses direction across the line."), mis("Moving points on the mirror line", "A point already on the mirror line stays there."), mis("Ignoring distance from the mirror line", "Equal distance is the core rule."), mis("Connecting reflected points in a new order", "After reflecting anchor points, connect them in the same order as the original figure."), mis("Forgetting inner details", "Dots, holes, stripes and colours must reflect too, not only the outside edge.")},
			[]gin.H{cq("A line of symmetry means...", "Think folding.", []string{"two matching halves", "a longer side", "a right angle", "a graph scale"}, "two matching halves", "The halves match exactly when folded."), cq("How many symmetry lines does a square have?", "Vertical, horizontal, diagonals.", []string{"1", "2", "4", "8"}, "4", "A square has four lines of symmetry."), cq("A non-square rectangle has...", "Use shape properties.", []string{"0 lines", "1 line", "2 lines", "4 lines"}, "2 lines", "Vertical and horizontal lines through the centre work."), cq("On a grid, a point 2 squares left of the mirror line maps to...", "Same distance other side.", []string{"2 squares right", "1 square right", "2 squares left", "anywhere"}, "2 squares right", "Mirror points keep equal distance."), cq("A point on the mirror line reflects to...", "It is already on the fold.", []string{"itself", "the far left", "the far right", "any corner"}, "itself", "A point on the mirror line reflects to itself because the fold line is the fixed place."), cq("For a horizontal mirror line, a point 2 squares above maps to...", "Above/below changes side.", []string{"2 squares below", "2 squares above", "2 squares left", "on the far right"}, "2 squares below", "With a horizontal mirror line, up/down distance flips to the other side."), cq("Which shape usually has no line symmetry?", "Think fold test.", []string{"Square", "Circle", "Scalene triangle", "Rectangle"}, "Scalene triangle", "A scalene triangle has no equal sides to create a matching fold."), cq("To test a possible symmetry line, first...", "Use the fold idea.", []string{"Count colours only", "Imagine folding on that line", "Add all sides", "Find the biggest angle"}, "Imagine folding on that line", "Folding reveals whether halves match."), cq("When completing a symmetric figure, what should you copy first?", "Use anchor points.", []string{"Corners and endpoints", "Only the colour", "Only the biggest side", "The title"}, "Corners and endpoints", "Anchor points let you copy exact distances before connecting lines."), cq("A dot is 3 squares from the mirror line. Its reflected dot is...", "Same distance, opposite side.", []string{"3 squares on the opposite side", "1 square on the same side", "anywhere near the shape", "always on the mirror line"}, "3 squares on the opposite side", "Matching points keep the same distance from the mirror line.")},
			[]gin.H{cq("A figure has equal area on both sides of a line. Is it definitely symmetric?", "Same area is not enough.", []string{"Yes always", "No, shapes must match exactly", "Only if it is blue", "Only for squares"}, "No, shapes must match exactly", "Symmetry requires exact matching, not only equal area."), cq("A regular pentagon has how many lines of symmetry?", "One through each vertex and opposite side midpoint.", []string{"0", "1", "5", "10"}, "5", "Each vertex can anchor one symmetry line."), cq("If a letter B has a horizontal line of symmetry, what does that mean?", "Fold top to bottom.", []string{"Top and bottom match", "Left and right match", "It has no curves", "It is a square"}, "Top and bottom match", "Horizontal symmetry folds top onto bottom."), cq("When completing a symmetric drawing, why count grid squares?", "Accuracy.", []string{"To make matching distances", "To find volume", "To choose colour", "To make it faster only"}, "To make matching distances", "Counting keeps each reflected point equally far from the mirror line."), cq("A point lies exactly on the mirror line. Where should the reflected point go?", "The mirror line is fixed.", []string{"The same point", "One square left", "One square right", "Anywhere on the shape"}, "The same point", "Points on the mirror line stay fixed when reflected."), cq("A diagonal line is suggested as a line of symmetry. What should you check?", "Use the fold test.", []string{"Whether both sides fold exactly together", "Only whether areas are equal", "Only whether it has right angles", "Only the colour"}, "Whether both sides fold exactly together", "Diagonal symmetry still needs matching shape, size and distance across the line."), cq("A reflected triangle has all matching points correct, but the lines are joined in a different order. What happens?", "Order matters.", []string{"It may become a different shape", "It is always correct", "It becomes a graph", "It has more symmetry lines"}, "It may become a different shape", "After copying anchor points, connect them in the same order."), cq("A shape outline is symmetric, but one inner star is missing on the other side. Is the whole figure symmetric?", "Check all details.", []string{"No, inner details must reflect too", "Yes, outline is enough", "Only for rectangles", "Only if the star is small"}, "No, inner details must reflect too", "The whole figure includes internal marks and colours.")},
			cq("A child completes only the outline but puts an inner dot in the wrong place. Is the figure symmetric?", "All features must reflect.", []string{"Yes, outline is enough", "No, every marked point must match", "Only if the dot is small", "No shape can have dots"}, "No, every marked point must match", "Symmetry applies to the whole figure, including internal details.")), true
	case "P4-GEOMETRY-NETS":
		return mergePack(base,
			"A net, a solid, and a 2D representation are three views of the same object: flat plan, folded object, and finished sketch.",
			"P4 geometry asks the child to switch views. First decide whether the picture is a flat net before folding, a folded solid, or a finished 2D representation. Then use a face kit check, fold test, and drawing routine instead of guessing from how the picture looks.",
			"nets",
			[]string{"A solid has faces, edges, and vertices.", "A three-question gate comes first: What view is this? Does the face kit match? Can it fold or draw correctly?", "A view decision comes first: flat net, folded solid, or finished 2D drawing.", "A net is a flat face map before folding.", "A shared edge in a net acts like a hinge: the neighbour face folds up around that edge.", "A valid net needs both the face kit and the fold test.", "A net must have the right number and shapes of faces.", "Faces connected in a net must fold without gaps or overlap.", "Every folded face needs one face slot: front, back, left, right, top or bottom.", "A slot ledger names where each face lands after folding.", "The slot ledger rule is one face per slot, and every needed slot filled.", "If two faces try to land in the same slot, the net fails even if the face count is correct.", "A slot collision means two different flat squares try to become the same folded face.", "If needed slots stay empty, the net also fails.", "A six-square straight strip is a single-row trap: face kit passes, but fold test fails because it makes a belt, not a closed box.", "Six correct faces can still be arranged wrongly.", "Opposite faces come in matching pairs in cubes and cuboids.", "A 2D representation is a finished-solid sketch with depth lines.", "Hidden faces and hidden edges still exist even when a 2D drawing does not show them.", "Different nets can fold into the same solid.", "Prisms have two matching parallel end faces joined by rectangular side faces.", "Pyramids have one base and triangular faces meeting at one apex."},
			[]string{"Ask the three-question gate: view, face kit, fold/draw check.", "Ask: flat net, folded solid, or finished 2D drawing?", "If it is flat, list the face kit: shapes and number of faces.", "Choose one base face and mentally fold neighbours around its shared edges like hinges.", "Track face slots: front, back, top, bottom, left, right.", "Use a slot ledger: write which face lands in each slot.", "Track which face becomes front, back, top, bottom, left, or right after folding.", "Check that every face gets exactly one place: no gap, no overlap.", "Check for slot collisions: no two faces may write the same slot name.", "Check for empty slots too; a cube needs all six face places filled.", "If a straight strip makes a belt instead of a box, say: face kit passes, fold test fails.", "If a net has the right faces but cannot fold, say: face kit passes, fold test fails.", "If the slot ledger repeats a slot, say: face kit passes, slot ledger fails.", "For cubes and cuboids, pair opposite faces before deciding if a net can fold.", "If it is a finished drawing, name visible faces and remember hidden/opposite faces.", "To draw a cuboid, draw a front rectangle, copy a back rectangle diagonally, then connect matching corners.", "Use dashed hidden edges only when the drawing needs to show unseen edges.", "Match the drawing back to the solid: cube, cuboid, prism, pyramid, cone, or cylinder."},
			[]gin.H{ex("Three-question gate", "Before answering, ask: What view is this? Does the face kit match? Can it fold or draw correctly? This stops guessing from appearance."), ex("View switch", "A cube net is six flat squares before folding. A 2D cube drawing is the finished cube shown on paper with slanting depth lines."), ex("Hinge rule", "Two faces that share an edge in the net fold around that edge like a hinge. If too many faces try to fold into the same position, the net fails."), ex("Face slots", "When folding a cube net, each square must land in one slot: front, back, top, bottom, left or right. Two faces cannot land in the same slot, and no needed slot can stay empty."), ex("Slot ledger", "Write a slot ledger while folding: base/front, top, bottom, left, right, back. If a face repeats a slot or leaves a needed slot empty, the fold test fails."), ex("Slot collision", "A slot collision happens when two flat squares both try to become the same folded face, such as both trying to become the top. The face kit passes, but the slot ledger fails."), ex("Two checks for nets", "A cube net needs the correct face kit and a successful fold test. Six squares in a straight strip has the face kit, but the fold test fails. Face count is not enough."), ex("Cube net", "A cube has 6 square faces. A cube net must have 6 squares connected so they can fold around one square with no overlap."), ex("Single-row trap", "Six squares in one straight row has the right face count, but it folds like a belt around the middle. It cannot close the cube correctly, so Face count is not enough."), ex("Opposite faces", "In a cube, each face has one opposite face that does not share an edge with it in the finished cube. A net must place faces so the folded cube gets all three opposite pairs."), ex("Cube drawing", "A 2D drawing of a cube shows three visible faces with slanting edges, even though the real cube has six faces."), ex("Hidden face check", "When a cube drawing shows only three faces, the back, bottom or left faces may be hidden by the drawing angle; they did not disappear."), ex("Cuboid drawing", "To draw a cuboid: draw a front rectangle, copy a back rectangle diagonally up and right, then connect matching corners. Hidden back edges can be dashed."), ex("Cuboid face pairs", "A cuboid has 6 rectangular faces in 3 matching opposite pairs: front/back, left/right, top/bottom."), ex("Triangular prism", "A triangular prism has two matching triangular ends and three rectangular side faces. In a 2D drawing, the matching triangles are joined corner to corner."), ex("Cylinder", "A cylinder net shows two circles and one rectangle; a 2D drawing usually shows two curved ellipses and two side lines."), ex("Square pyramid net", "A square pyramid net has one square base and four triangles attached around it. After folding, the triangles meet at one apex."), ex("Pyramid drawing", "A square pyramid drawing shows a square or slanted base and triangular faces meeting at one top vertex.")},
			[]gin.H{mis("Counting faces only", "Correct number of faces is necessary but not enough; arrangement must fold without overlap."), mis("Stopping after the face kit", "A shape can pass the face kit and still fail the fold test."), mis("Trusting a six-square straight strip", "A straight strip has six squares, but it makes a belt, not a closed cube."), mis("Confusing net with 2D representation", "A net is before folding. A 2D representation is the drawing of the finished solid."), mis("Using wrong face shapes", "A cube needs squares; a cuboid usually needs rectangles."), mis("Ignoring opposite faces", "For cubes and cuboids, opposite faces must end up across from each other after folding."), mis("Ignoring hidden faces", "A 2D drawing may hide faces and edges, but the solid still has all of them."), mis("Treating depth lines as real slanted faces", "In a cube or cuboid drawing, slanting lines show depth on paper; the real faces are still squares or rectangles."), mis("Drawing a prism like a pyramid", "A prism has two matching parallel ends; a pyramid has faces meeting at one apex.")},
			[]gin.H{cq("Before solving a nets question, what should you ask first?", "Use the gate.", []string{"What view is this?", "What colour is it?", "How long is the page?", "Where is the graph title?"}, "What view is this?", "First decide whether it is a flat net, a folded solid, or a finished 2D drawing."), cq("A net shows a solid...", "Before or after folding?", []string{"before folding, with faces flat", "after it is built, with depth shading", "as a line graph", "as a decimal"}, "before folding, with faces flat", "A net is the unfolded flat plan."), cq("A slot ledger for a cube should have...", "Use one face per slot.", []string{"six different slots filled once", "two faces in the top slot", "only front and back", "no bottom slot"}, "six different slots filled once", "A cube needs front, back, top, bottom, left, and right, each used once."), cq("A 2D representation of a cube usually shows...", "Think finished solid on paper.", []string{"three visible faces and slanting edges", "six loose squares in a row", "two circles and one rectangle", "only one square"}, "three visible faces and slanting edges", "A 2D drawing tries to show depth on a flat page."), cq("A square pyramid has...", "Base plus side faces.", []string{"1 square and 4 triangles", "6 squares", "2 circles and 1 rectangle", "4 rectangles only"}, "1 square and 4 triangles", "The square is the base; triangles fold up to meet."), cq("A square pyramid net should have...", "Base and side faces.", []string{"one square with four triangles attached", "six squares", "two circles and one rectangle", "two triangles only"}, "one square with four triangles attached", "The square is the base and the four triangles meet at one apex."), cq("A cylinder net can show...", "Unroll the curved surface.", []string{"2 circles and 1 rectangle", "6 squares", "4 triangles", "1 circle only"}, "2 circles and 1 rectangle", "The rectangle wraps around; circles close top and bottom."), cq("If a net has overlapping faces when folded, it is...", "Folding test.", []string{"valid", "not valid", "always a cube", "a line graph"}, "not valid", "Faces must cover the solid without overlapping."), cq("What should you check after counting the faces?", "Face count is only step one.", []string{"Whether the arrangement can fold with no overlap", "The colour of the faces", "Whether it has decimals", "The graph title"}, "Whether the arrangement can fold with no overlap", "A net needs the correct face kit and a valid fold."), cq("The best way to test a net is to...", "Use spatial reasoning.", []string{"Count page numbers", "Mentally fold around a base face", "Add all angles", "Compare decimals"}, "Mentally fold around a base face", "Folding checks whether the flat plan becomes a solid."), cq("A triangular prism has...", "Prism structure.", []string{"two matching triangular ends and three rectangles", "one square and four triangles", "two circles and one rectangle", "six squares"}, "two matching triangular ends and three rectangles", "A prism has matching parallel ends joined by rectangular side faces.")},
			[]gin.H{cq("Which net could be a cube?", "Use face count and arrangement.", []string{"Six connected squares with no impossible overlap", "Five squares", "Six circles", "One rectangle"}, "Six connected squares with no impossible overlap", "A cube needs six square faces arranged to fold."), cq("Six squares in one straight row are...", "Face kit is not enough.", []string{"a valid cube net", "not a valid cube net because it makes a belt, not a box", "a cylinder net", "a square pyramid net"}, "not a valid cube net because it makes a belt, not a box", "The face kit passes, but the fold test fails because the strip cannot close into all six cube slots."), cq("A cube net has six squares, but two squares fold into the same face slot. What is true?", "Use face slots.", []string{"It fails the fold test", "It is always valid", "It becomes a cylinder", "It has no edges"}, "It fails the fold test", "Every folded face needs one slot: front, back, top, bottom, left or right. Two faces cannot share one slot."), cq("A slot ledger says: front=A, top=B, top=C, left=D, right=E, bottom=F. What is wrong?", "Look for repeated slots.", []string{"top is used twice", "front is missing", "there are no six faces", "a cube needs two tops"}, "top is used twice", "Two faces are trying to become the top face, so this is a slot collision."), cq("Which picture is a 2D representation, not a net?", "Decide what the picture is doing.", []string{"A slant-edge drawing of a cuboid", "Six squares laid flat", "A square with four triangles around it", "Two circles with a rectangle"}, "A slant-edge drawing of a cuboid", "A representation shows the finished solid on paper; a net shows faces laid flat."), cq("A cuboid has dimensions 2×3×5. Its face pairs are...", "Opposite faces match.", []string{"2×3, 2×5, 3×5", "2×2 only", "3 circles", "5 triangles"}, "2×3, 2×5, 3×5", "Each pair of dimensions makes one pair of opposite rectangular faces."), cq("To draw a cuboid clearly, a good routine is...", "Use matching corners.", []string{"front rectangle, copy a back rectangle diagonally, connect matching corners", "draw one triangle only", "draw six loose rectangles", "draw a line graph"}, "front rectangle, copy a back rectangle diagonally, connect matching corners", "This creates visible front/back faces and depth edges."), cq("A triangular prism includes...", "Think prism faces.", []string{"2 triangles and 3 rectangles", "6 squares", "1 square and 4 triangles", "2 circles"}, "2 triangles and 3 rectangles", "The triangular ends and rectangular side faces form a triangular prism."), cq("Why can one solid have several different nets?", "Same faces, different layout.", []string{"Because faces can be arranged in different foldable ways", "Because solids change shape", "Because face count does not matter", "Because nets are random"}, "Because faces can be arranged in different foldable ways", "The same faces can be connected in more than one valid flat plan."), cq("In a 2D cuboid drawing, why are some edges slanted?", "Paper is flat, but the object has depth.", []string{"To show depth", "Because the real cuboid has slanted faces", "Because all edges are hidden", "To show decimals"}, "To show depth", "The slanted lines are drawing clues, not new face shapes."), cq("Dashed hidden edges in a solid drawing usually mean...", "Seen or unseen?", []string{"edges that exist but are behind the visible faces", "edges that do not exist", "decimal points", "fold lines that must be cut off"}, "edges that exist but are behind the visible faces", "A drawing can show unseen edges with dashes to remind us the solid is 3D.")},
			cq("A child checks only that a cube net has six squares. The slot ledger later shows top used twice and back missing. What is the best correction?", "Face count is not enough.", []string{"The face kit passes, but the slot ledger fails", "It must be valid because there are six squares", "The back face is optional", "A cube can have two top faces"}, "The face kit passes, but the slot ledger fails", "A valid cube needs one face in each slot. Repeating top and missing back proves the fold cannot make a proper cube.")), true
	case "P4-STATISTICS":
		return mergePack(base,
			"Statistics is evidence reading: choose the data job, audit the scale, then make a claim with exact values and units.",
			"My Pals P4 focuses on tables and line graphs. A table is best for exact lookup and organised values. A line graph is best for showing change across ordered points, usually time. The child should not just look for the biggest mark; she should first decide what question the data should answer: exact value, compare, total, trend, or graph choice. Scale audit comes before trend claims: the same values can look calm or dramatic when the vertical scale changes.",
			"statistics",
			[]string{"Use the statistics gate: job, display, scale, evidence.", "Statistics is not decoration; it is evidence for a question.", "Question job first: exact value, compare, total, trend, or graph choice.", "Table first when the question asks for an exact row, exact column, total, or difference.", "Line graphs need ordered data. Line graph only when the horizontal labels have a meaningful order, such as days, weeks, months, or time.", "The horizontal axis tells which ordered point; the vertical axis tells the measured amount.", "A scale tells how much each grid step is worth.", "Scale audit comes before trend claims.", "A trend describes adjacent changes: increase, decrease, same, or mixed.", "For trend claims, write direction + amount + unit + interval.", "Evidence means the actual value plus the unit, not just 'higher' or 'lower'.", "A graph-choice question asks which display protects the meaning of the data.", "Two line graphs with the same data can look different if the vertical scale changes.", "A steep line is not evidence until the exact values and scale have been checked."},
			[]string{"Use the statistics gate: what job, which display, what scale, what evidence sentence?", "Name the question type: exact value, compare, total, trend, or graph choice.", "Read the title to know the context.", "Read the horizontal axis: is it ordered time or only categories?", "Read the vertical axis: what is being measured?", "Read the unit and scale before reading any value.", "Run a scale audit before trend: check starting value and step size.", "For exact value, read one row or point.", "For compare, subtract two values.", "For total, add the relevant rows or points.", "For trend, compare adjacent ordered points and write direction + amount + unit + interval.", "Compare exact values, not line steepness.", "For graph choice, decide whether the display protects the data meaning.", "For tables, find the exact row and column.", "For line graphs, trace from the point to the axis scale.", "Write an evidence sentence: The data shows ___ because ___."},
			[]gin.H{ex("Statistics gate", "Before touching numbers, ask: What job is the question asking? Which display fits? What is the scale? What evidence sentence will prove the answer?"), ex("Question job first", "Before touching numbers, ask whether the question wants one exact value, a comparison, a total, a trend, or the best graph type."), ex("Exact value job", "If the question asks, 'How many books on Wednesday?', use the table row or the line graph point and write the exact value with the unit."), ex("Compare job", "A table shows Monday 24 books and Tuesday 31 books. To compare Tuesday and Monday, subtract 31 - 24 = 7 books."), ex("Total job", "If the question asks for books from Monday to Wednesday, add the three values. A total job is not answered by only choosing the highest point."), ex("Trend job", "A line graph with 15, 15, 18, 12 shows: same, then increase by 3, then decrease by 6. Trend language follows the ordered points."), ex("Scale audit before trend", "Before saying a line increased a lot, read the vertical scale and the exact values. A steep line is a clue, not proof."), ex("Same data, different vertical scale", "Sales 92, 94, 95, 96 can look very steep if the axis starts at 90, but the total change is only 4 items."), ex("Evidence frame", "For trend claims, write: direction + amount + unit + interval. Example: Sales increased by 4 items from Monday to Thursday."), ex("Evidence sentence", "A strong answer says: The data shows sales increased slowly because the values went from 92 to 96, only 4 more."), ex("Complete a table", "If the line graph shows 20 books on Wednesday, the Wednesday row in the table should say 20 books."), ex("Scale job", "If the vertical scale is 1 step = 5 pupils and a point reaches 6 steps, the value is 30 pupils. Say the unit, not only 30."), ex("Graph choice", "Daily temperature can use a line graph because days are ordered. Favourite fruits should not be connected by a line because fruit names are categories."), ex("Misleading scale", "A graph with values 80 to 100 can look dramatic if the vertical axis starts at 80. Read exact values before saying the change is large."), ex("Three different jobs", "Exact value, compare, total, trend, and graph choice are different jobs. Exact asks 'what number?'; compare asks 'how much more?'; total asks 'how many altogether?'; trend asks 'how did it change?'; graph choice asks 'what display fits this data?'")},
			[]gin.H{mis("Ignoring scale", "One square may mean 2, 5, 10, or more. Always read the scale before reading the point."), mis("Comparing by picture size only", "Read exact values before deciding whether a change is big or small."), mis("Trusting steepness", "A steep line is only a visual clue. Check the scale and actual values before making a claim."), mis("Forgetting units", "A number could mean pupils, dollars, books, minutes or degrees Celsius."), mis("Reading only one axis", "A point needs both its horizontal label and vertical value."), mis("Calling every change a trend", "A trend needs ordered data points and should describe adjacent values."), mis("Connecting unrelated categories", "Do not connect unrelated categories with a line. A line graph needs an ordered variable such as time."), mis("Choosing a graph before reading the question", "Decide the evidence job first: exact lookup, compare, total, trend, or graph choice."), mis("Making a claim without evidence", "A claim like 'much more' needs actual values and units to prove it."), mis("Leaving out the interval", "Trend evidence should say when the change happened, not only that it changed.")},
			[]gin.H{cq("Which statistics gate is strongest?", "Use the full gate.", []string{"job, display, scale, evidence", "colour, tallest point, page", "first number only", "line shape only"}, "job, display, scale, evidence", "A statistics answer needs the task, the right display, the scale, and an evidence sentence."), cq("A graph scale is 1 step = 5. A point reaches 6 steps. Value?", "Multiply scale by steps.", []string{"6", "11", "30", "60"}, "30", "6×5=30."), cq("What should happen before a trend claim?", "Avoid being tricked by steepness.", []string{"Scale audit before trend", "Choose the brightest colour", "Ignore the axis", "Copy the tallest point"}, "Scale audit before trend", "Read the starting value, step size, exact values, and unit before saying how much it changed."), cq("Which should you do before answering?", "Avoid guessing from the picture.", []string{"Name the question type and read title, labels, scale", "Only look for the tallest point", "Only count colours", "Only read the last point"}, "Name the question type and read title, labels, scale", "Data questions need context and scale before calculation."), cq("A line graph from 12 to 20 shows...", "Find change.", []string{"decrease by 8", "increase by 8", "same", "increase by 32"}, "increase by 8", "20-12=8."), cq("Class A has 28 books, B has 19. How many more?", "Comparison means subtract.", []string{"7", "9", "37", "47"}, "9", "28-19=9."), cq("A table is best for...", "Think exact values.", []string{"Showing exact rows of data", "Playing audio", "Measuring angles", "Folding nets"}, "Showing exact rows of data", "Tables keep values organised and exact."), cq("A line graph is best when data points are...", "Think order.", []string{"ordered over time", "random names only", "shape corners", "unrelated categories"}, "ordered over time", "A line shows movement from one ordered point to the next."), cq("A point on a line graph needs...", "Read both axes.", []string{"a horizontal label and a vertical value", "only a colour", "only the highest number", "a protractor"}, "a horizontal label and a vertical value", "The point answers when/which item and how much."), cq("Favourite fruits: apple, banana, grape. Should these be joined by a line?", "Categories are not ordered time.", []string{"Yes, always", "No, categories should not be connected as a trend", "Only if grape is highest", "Only if the line is blue"}, "No, categories should not be connected as a trend", "A line implies movement through an ordered variable.")},
			[]gin.H{cq("Scale is 1 square = 4 pupils. A point is halfway between 6 and 7 squares. Value?", "Use half step.", []string{"24", "26", "28", "30"}, "26", "6.5×4=26."), cq("A line graph goes 15, 15, 18, 12. Which statement is best?", "Describe trend carefully.", []string{"Always increasing", "Same, then increases, then decreases", "Always decreasing", "No data"}, "Same, then increases, then decreases", "15 to 15 stays same, then 18 increases, then 12 decreases."), cq("A table shows Mon 18, Tue 24, Wed 24, Thu 31. What is the total?", "Add exact table values.", []string{"55", "79", "97", "3124"}, "97", "18+24+24+31=97."), cq("A line graph shows 12, 18, 18, 25. What happened from the second to third point?", "Compare adjacent values.", []string{"increased", "decreased", "stayed the same", "cannot be read"}, "stayed the same", "18 to 18 means no change."), cq("Which data is better for a line graph?", "Look for meaningful order.", []string{"Temperature from Monday to Friday", "Favourite snacks", "Class names", "Types of pencils"}, "Temperature from Monday to Friday", "Days are ordered, so the line can show change over time."), cq("A temperature graph uses days on the horizontal axis and °C on the vertical axis. What does the point at Wednesday, 28 mean?", "Read both axes and the unit.", []string{"Wednesday was 28°C", "There were 28 Wednesdays", "28 is the scale only", "Wednesday is hotter than every day"}, "Wednesday was 28°C", "The horizontal label gives the day; the vertical value gives the temperature."), cq("Why might two line graphs with the same data look different?", "Think scale.", []string{"Different vertical scales", "Data changes automatically", "Titles do not matter", "Graphs cannot show same data"}, "Different vertical scales", "A compressed or stretched scale changes the visual impression."), cq("Which sentence gives evidence?", "Use values and units.", []string{"It went up a lot", "It looks steep", "Sales increased from 92 to 96, by 4 items", "The line is blue"}, "Sales increased from 92 to 96, by 4 items", "Evidence uses the actual values and unit."), cq("Values are 92, 94, 95, 96 from Mon to Thu. Which trend sentence is best?", "Use direction + amount + unit + interval.", []string{"The line is steep", "Sales increased by 4 items from Mon to Thu", "Sales doubled", "Thu is blue"}, "Sales increased by 4 items from Mon to Thu", "96 - 92 = 4, and the unit is items over the Mon-to-Thu interval.")},
			cq("Maya says the shop sold 'much more' because the line looks steep. The values are 92, 94, 95, 96. What should she say instead?", "Use exact values before describing the trend.", []string{"Sales increased slowly by 4 in total", "Sales doubled", "The graph has no values", "The table is impossible"}, "Sales increased slowly by 4 in total", "The line may look steep because of the scale. Exact values and scale tell the real change.")), true
	case "P4-TIME":
		return mergePack(base,
			"Time is a timeline: start time, duration, and finish time are three connected parts.",
			"My Pals P4 revisits seconds, duration and 24-hour time. The child should stop treating clock numbers like ordinary subtraction. Draw a timeline, decide which part is missing, then jump forward or backward across easy landmarks such as the next hour, noon, or midnight.",
			"time",
			[]string{"Use the time gate before calculating: missing part, direction, unit, boundary.", "Duration is elapsed time, not just the number shown on the clock.", "The three-part relationship is start + duration = finish.", "If the finish is missing, jump forward from the start.", "If the start is missing, jump backward from the finish.", "If the duration is missing, measure the distance from start to finish.", "Seconds are small units: 60 seconds make 1 minute.", "24-hour time removes am/pm but still needs careful conversion.", "00:15 means 12:15 am, soon after midnight.", "Crossing noon changes am to pm; crossing midnight changes pm to am and starts the next day.", "The same duration can cross a different boundary depending on the start time."},
			[]string{"Use the time gate: what is missing, which direction, which unit, which boundary?", "Write the three parts: start, duration, finish.", "Circle the missing part: start, duration, or finish.", "Convert to one format if the question mixes 12-hour and 24-hour time.", "Convert seconds and minutes before calculating if units are mixed.", "For finish time, jump forward on a timeline.", "For start time, jump backward on a timeline.", "For duration, count the jumps between start and finish.", "Jump to the next easy time such as the next hour or noon/midnight.", "Check whether the answer crossed noon or midnight.", "For 12-hour answers, check am/pm; for 24-hour answers, use 00 after midnight.", "Write the final answer exactly as asked."},
			[]gin.H{ex("Time gate", "Before calculating, ask: What is missing? Which direction should I jump? Which unit is being used? Will I cross noon or midnight?"), ex("Seconds to minutes", "125 seconds is 2 min 5 s because 120 seconds make 2 minutes, with 5 seconds left."), ex("Minutes to seconds", "2 min 45 s = 165 s because 2 minutes is 120 seconds, then add 45 seconds."), ex("24-hour conversion", "6:45 pm is 18:45 because pm after noon adds 12 to the hour."), ex("After midnight", "00:15 is 12:15 am. In 24-hour time, 00 shows the hour after midnight."), ex("Find finish", "A swim starts at 14:35 and lasts 50 min. Finish is missing, so jump forward: 25 min to 15:00, then 25 min more to 15:25."), ex("Find start", "If a lesson finishes at 16:10 and lasts 45 min, start is missing, so jump backward: 10 min to 16:00, then 35 min more to 15:25. In equation form, 16:10 - 45 min = 15:25."), ex("Find duration", "From 9:50 to 10:10, duration is missing. Jump 10 min to 10:00, then 10 min to 10:10, so duration is 20 min."), ex("Same duration, daytime", "11:00 am + 1 h 30 min = 12:30 pm because the jump crosses noon."), ex("Same duration, night", "11:00 pm + 1 h 30 min = 12:30 am the next day because the jump crosses midnight."), ex("Cross noon", "11:40 am + 35 min = 12:15 pm. Jump 20 min to noon, then 15 min more."), ex("Cross midnight", "23:30 + 1 h 10 min = 00:40 the next day.")},
			[]gin.H{mis("Treating 12 am as noon", "12 am is midnight. 12 pm is noon."), mis("Writing 24:30", "After 23:59 comes 00:00, not 24:00 for ordinary clock times."), mis("Subtracting clock numbers directly", "9:50 to 10:10 is 20 minutes, not 60."), mis("Using the wrong direction", "Finish missing means jump forward. Start missing means jump backward. Duration missing means measure the gap."), mis("Finding finish when the question asks for start", "Circle the missing part first. Finish time and start time use opposite jumps."), mis("Forgetting units", "Seconds, minutes, and hours must be converted before calculating."), mis("Forgetting the next day", "A time after midnight may look smaller, but it is on the next day.")},
			[]gin.H{cq("Which time gate is correct for a missing finish time?", "Choose the direction first.", []string{"start + duration, jump forward", "finish - start, jump nowhere", "duration - finish, jump backward", "am + pm only"}, "start + duration, jump forward", "If finish is missing, start at the start time and jump forward by the duration."), cq("180 seconds = ?", "60 seconds make 1 minute.", []string{"1 min 80 s", "2 min", "3 min", "18 min"}, "3 min", "180 ÷ 60 = 3."), cq("2 min 45 s = ?", "Convert minutes first.", []string{"125 s", "145 s", "165 s", "245 s"}, "165 s", "2 minutes is 120 seconds. 120 + 45 = 165 seconds."), cq("18:20 is...", "Convert from 24-hour time.", []string{"6:20 am", "6:20 pm", "8:20 pm", "18:20 am"}, "6:20 pm", "18 - 12 = 6, so it is 6:20 pm."), cq("00:15 is...", "00 means after midnight.", []string{"12:15 am", "12:15 pm", "1:15 am", "midday"}, "12:15 am", "00:15 is fifteen minutes after midnight."), cq("7:05 pm in 24-hour time is...", "PM after noon adds 12.", []string{"07:05", "17:05", "19:05", "24:05"}, "19:05", "7 + 12 = 19."), cq("11:00 am + 1 h 30 min = ?", "Same duration, daytime boundary.", []string{"12:30 am", "12:30 pm", "1:30 pm", "10:30 am"}, "12:30 pm", "11:00 am + 1 hour reaches noon, then 30 minutes more is 12:30 pm."), cq("11:00 pm + 1 h 30 min = ?", "Same duration, night boundary.", []string{"12:30 am", "12:30 pm", "1:30 am", "00:30 pm"}, "12:30 am", "11:00 pm + 1 hour reaches midnight, then 30 minutes more is 12:30 am the next day."), cq("9:50 to 10:10 is...", "Jump to 10:00 first.", []string{"10 min", "20 min", "40 min", "60 min"}, "20 min", "9:50 to 10:00 is 10 min, then 10 more min.")},
			[]gin.H{cq("A swim starts at 14:35 and lasts 50 min. Finish time?", "Use start + duration = finish.", []string{"15:15", "15:25", "14:85", "13:45"}, "15:25", "14:35 + 25 min = 15:00, then 25 min more = 15:25."), cq("A lesson finishes at 16:10 and lasts 45 min. Start time?", "Use finish - duration = start.", []string{"15:15", "15:25", "16:55", "15:35"}, "15:25", "16:10 - 10 min = 16:00, then 35 min more back = 15:25."), cq("If start time is missing, what should the timeline do?", "Use the time gate.", []string{"jump backward from the finish", "jump forward from the start", "ignore duration", "change every pm to am"}, "jump backward from the finish", "A missing start means use finish - duration, so the jumps go backward."), cq("A train leaves at 10:55 pm and arrives 45 min later. Arrival?", "Check whether midnight is crossed.", []string{"11:40 pm", "11:40 am", "12:40 am", "12:40 pm"}, "11:40 pm", "10:55 pm + 45 min stays before midnight: 11:40 pm."), cq("23:30 + 1 h 10 min = ?", "Cross midnight in 24-hour time.", []string{"00:40", "12:40 pm", "24:40", "01:40"}, "00:40", "23:30 + 30 min = 00:00, plus 40 min = 00:40."), cq("Which is earliest?", "Compare using 24-hour time.", []string{"07:45", "7:50 am", "18:00", "12:05 pm"}, "07:45", "07:45 is five minutes before 7:50 am."), cq("A timer shows 3 min 20 s. How many seconds?", "Convert minutes to seconds.", []string{"180 s", "200 s", "320 s", "380 s"}, "200 s", "3 min is 180 s, plus 20 s gives 200 s."), cq("Which relationship finds a missing duration?", "Choose the missing part.", []string{"finish - start = duration", "start + finish = duration", "duration - finish = start", "seconds × pm = duration"}, "finish - start = duration", "Elapsed time is the distance between start and finish.")},
			cq("Maya says 12:30 am is after lunch because it has 12. What is the best correction?", "Use the midnight/noon boundary.", []string{"12:30 am is half past midnight, before morning", "12:30 am is always lunch time", "Any 12 time is pm", "24-hour time cannot show it"}, "12:30 am is half past midnight, before morning", "12 am starts the new day at midnight. 12 pm is noon.")), true
	default:
		return nil, false
	}
}

func buildTextbookLessonCards(t model.CurriculumTopic) []gin.H {
	cards := []gin.H{}
	for _, m := range concreteTextbookMappings(t.Mappings) {
		cards = append(cards, gin.H{
			"book":              m.Book,
			"chapterTitle":      m.ChapterTitle,
			"lessonObjective":   lessonObjective(m.ChapterTitle, t.Title),
			"visualKind":        visualKindForTopic(t.TopicCode),
			"workedExamples":    lessonExamples(t.TopicCode, m.ChapterTitle),
			"practiceQuestions": lessonPracticeQuestions(t.TopicCode, m.ChapterTitle),
			"explainPrompt":     "Explain your thinking in one full sentence: what model did you use, and why?",
		})
	}
	return cards
}

func concreteTextbookMappings(mappings []model.TextbookMapping) []model.TextbookMapping {
	out := []model.TextbookMapping{}
	seen := map[string]bool{}
	for _, m := range mappings {
		title := strings.TrimSpace(m.ChapterTitle)
		if !isConcreteTextbookLesson(title) {
			continue
		}
		key := textbookLessonKey(m.Book, title)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, m)
	}
	return out
}

func isConcreteTextbookLesson(title string) bool {
	s := strings.ToLower(title)
	return strings.Contains(s, "lesson ") || strings.Contains(s, "practice ")
}

func textbookLessonKey(book, title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	s = regexp.MustCompile(`\s*\(p\.\s*\d+\)\s*`).ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, ",", "")
	s = strings.Join(strings.Fields(s), " ")
	return strings.ToLower(strings.TrimSpace(book)) + "|" + s
}

func lessonObjective(title, topic string) string {
	s := strings.TrimSpace(title)
	if i := strings.Index(s, ": "); i >= 0 {
		s = s[i+2:]
	}
	if i := strings.Index(s, " (p."); i >= 0 {
		s = s[:i]
	}
	lower := strings.ToLower(strings.TrimSpace(s))
	if strings.HasPrefix(lower, "understanding and measuring ") {
		return "Understand and measure " + strings.TrimPrefix(lower, "understanding and measuring ") + " as part of " + topic + ", then solve with reasoning."
	}
	for _, p := range []struct {
		prefix string
		verb   string
	}{
		{"understanding ", "Understand "},
		{"drawing ", "Draw "},
		{"comparing and ordering ", "Compare and order "},
		{"adding ", "Add "},
		{"subtracting ", "Subtract "},
		{"multiplying by ", "Multiply by "},
		{"dividing by ", "Divide by "},
		{"solving ", "Solve "},
	} {
		if strings.HasPrefix(lower, p.prefix) {
			lower = p.verb + strings.TrimPrefix(lower, p.prefix)
			return lower + " as part of " + topic + ", then solve with reasoning."
		}
	}
	return "Understand " + lower + " as part of " + topic + ", then solve with reasoning."
}

func visualKindForTopic(code string) string {
	switch {
	case strings.Contains(code, "P5-WHOLE-OPERATIONS"):
		return "p5OrderOps"
	case strings.Contains(code, "P5-PERCENTAGE-RATE"):
		return "p5PercentRate"
	case strings.Contains(code, "P5-RATIO"):
		return "p5Ratio"
	case strings.Contains(code, "P5-AREA-VOLUME"):
		return "p5AreaVolume"
	case strings.Contains(code, "P5-GEOMETRY"):
		return "p5Geometry"
	case strings.Contains(code, "P5-AVERAGE"):
		return "p5Average"
	case strings.Contains(code, "FOUR-OPERATIONS"):
		return "algorithm"
	case strings.Contains(code, "FACTORS-MULTIPLES"):
		return "factorRainbow"
	case strings.Contains(code, "FRACTIONS"):
		return "fractionBars"
	case strings.Contains(code, "GEOMETRY-ANGLES"):
		return "angle"
	case strings.Contains(code, "GEOMETRY-SYMMETRY"):
		return "symmetry"
	case strings.Contains(code, "GEOMETRY-NETS"):
		return "nets"
	case strings.Contains(code, "STATISTICS"):
		return "statistics"
	case strings.Contains(code, "WHOLE"), strings.Contains(code, "ADD-SUB"):
		return "place"
	case strings.Contains(code, "MULT-DIV"), strings.Contains(code, "FACTORS"):
		return "division"
	case strings.Contains(code, "DECIMALS"), strings.Contains(code, "MONEY"):
		return "decimal"
	case strings.Contains(code, "MEASUREMENT"):
		return "measure"
	case strings.Contains(code, "TIME"):
		return "time"
	case strings.Contains(code, "AREA"):
		return "area"
	default:
		return "generic"
	}
}

func lessonPracticeQuestions(code, title string) []gin.H {
	s := strings.ToLower(title)
	switch {
	case strings.Contains(code, "P4-WHOLE-100000") && (strings.Contains(s, "numbers to 100 000") || strings.Contains(s, "numbers to 100,000")):
		return []gin.H{
			cq("In 62,705, the 6 means...", "Read the place before the digit.", []string{"6", "60", "6000", "60,000"}, "60,000", "The 6 is in the ten-thousands place."),
			cq("Which expanded form is correct?", "Each digit keeps its place.", []string{"60,000 + 2,000 + 700 + 5", "6+2+7+5", "60,000 + 200 + 70 + 5", "62 + 705"}, "60,000 + 2,000 + 700 + 5", "There are no tens, so the tens part is 0."),
			cq("Why use a place-value chart?", "Avoid treating digits as a string.", []string{"It shows what each digit is worth", "It makes numbers shorter", "It changes the answer", "It removes zeros"}, "It shows what each digit is worth", "The same digit has different value in different places."),
		}
	case strings.Contains(code, "P4-WHOLE-100000") && strings.Contains(s, "comparing and ordering"):
		return []gin.H{
			cq("Which is greater: 58,402 or 57,999?", "Compare from the left.", []string{"58,402", "57,999", "They are equal", "Cannot tell"}, "58,402", "Both have 5 ten-thousands, then 8 thousands is greater than 7 thousands."),
			cq("To order 64,120, 64,012, 64,201, compare...", "Line up places.", []string{"ten-thousands, then thousands, hundreds, tens, ones", "only the last digit", "only commas", "only the number length"}, "ten-thousands, then thousands, hundreds, tens, ones", "Compare one place at a time from left to right."),
			cq("Why is 70,005 greater than 69,999?", "Crossing the ten-thousand boundary.", []string{"70 thousand is more than 69 thousand", "5 is bigger than 9", "It has fewer 9s", "It looks cleaner"}, "70 thousand is more than 69 thousand", "The ten-thousands and thousands decide before the ones matter."),
		}
	case strings.Contains(code, "P4-WHOLE-100000") && strings.Contains(s, "number patterns"):
		return []gin.H{
			cq("12, 17, 22, 27, ... follows which rule?", "Find the repeated change.", []string{"+5", "+10", "×5", "-5"}, "+5", "Each term increases by 5."),
			cq("A pattern goes 50,000, 45,000, 40,000. Next?", "The rule is -5,000.", []string{"35,000", "39,000", "45,000", "55,000"}, "35,000", "Keep subtracting 5,000."),
			cq("A rule must work...", "Check more than one jump.", []string{"between every neighbouring pair", "only once", "only for small numbers", "only if numbers are even"}, "between every neighbouring pair", "One lucky jump does not prove the pattern."),
		}
	case strings.Contains(code, "P4-WHOLE-100000") && strings.Contains(s, "rounding and estimation"):
		return []gin.H{
			cq("62,705 rounded to the nearest thousand is...", "Use the halfway point.", []string{"62,000", "62,700", "63,000", "70,000"}, "63,000", "62,705 is past 62,500, so it rounds up."),
			cq("Why estimate before exact calculation?", "Check reasonableness.", []string{"To know the answer size", "To skip thinking", "To change the question", "To remove units"}, "To know the answer size", "An estimate catches answers that are far too big or small."),
			cq("398 × 6 is close to...", "Round 398 to 400.", []string{"400×6", "300×6", "398+6", "400÷6"}, "400×6", "400×6=2400, so the exact answer should be near 2400."),
		}
	case strings.Contains(code, "P4-FACTORS-MULTIPLES") && strings.Contains(s, "factors"):
		return []gin.H{
			cq("Which is a factor pair of 24?", "Two numbers multiply to 24.", []string{"3 and 8", "5 and 5", "7 and 4", "24 and 2"}, "3 and 8", "3×8=24."),
			cq("6 is a factor of 42 because...", "Check divisibility.", []string{"42÷6 has no remainder", "42 is smaller than 6", "6 is even", "42 has two digits"}, "42÷6 has no remainder", "A factor divides the number exactly."),
			cq("Why draw an array for factors?", "Rows and columns show pairs.", []string{"It shows equal rows and columns", "It hides the quotient", "It changes multiplication", "It removes all factors"}, "It shows equal rows and columns", "Each array arrangement represents a factor pair."),
		}
	case strings.Contains(code, "P4-FACTORS-MULTIPLES") && strings.Contains(s, "multiples"):
		return []gin.H{
			cq("Which is a multiple of 7?", "Land by counting in 7s.", []string{"21", "23", "29", "34"}, "21", "7×3=21."),
			cq("The first four multiples of 6 are...", "Keep adding 6.", []string{"6, 12, 18, 24", "1, 2, 3, 6", "6, 7, 8, 9", "12, 18, 25, 30"}, "6, 12, 18, 24", "Multiples are landing points when you count by that number."),
			cq("24 is a common multiple of 6 and 8 because...", "Both land there.", []string{"6×4 and 8×3 both equal 24", "6+8=24", "24 is smaller than both", "It has digit 4"}, "6×4 and 8×3 both equal 24", "A common multiple belongs to both skip-counting lists."),
		}
	case strings.Contains(code, "P4-DECIMALS") && strings.Contains(s, "comparing") && strings.Contains(s, "decimals"):
		return []gin.H{
			cq("Which is larger: 0.6 or 0.58?", "Write 0.6 as 0.60.", []string{"0.6", "0.58", "They are equal", "Cannot tell"}, "0.6", "0.60 has 60 hundredths, more than 58 hundredths."),
			cq("Why is 0.125 not automatically bigger than 0.5?", "More digits does not mean bigger.", []string{"0.125 is 125 thousandths, but 0.5 is 500 thousandths", "125 is always bigger", "Decimals ignore place", "0.5 has no hundredths"}, "0.125 is 125 thousandths, but 0.5 is 500 thousandths", "Compare decimals by place value."),
			cq("To order decimals, first...", "Line up decimal points.", []string{"line up decimal points", "count digits only", "remove zeros randomly", "round everything"}, "line up decimal points", "Tenths compare with tenths, hundredths with hundredths."),
		}
	case strings.Contains(code, "P4-DECIMALS") && strings.Contains(s, "tenths"):
		return []gin.H{
			cq("0.6 means...", "Read the tenths place.", []string{"6 tenths", "6 hundredths", "60 ones", "6 thousands"}, "6 tenths", "The first digit after the decimal point is tenths."),
			cq("0.6 is the same as...", "Connect fraction and decimal.", []string{"6/10", "6/100", "60/10", "1/6"}, "6/10", "Six tenths can be written as 0.6 or 6/10."),
			cq("Why does 0.60 equal 0.6?", "Equivalent decimal.", []string{"60 hundredths equals 6 tenths", "Zero always changes value", "Hundredths are bigger than tenths", "They are not equal"}, "60 hundredths equals 6 tenths", "The extra zero shows precision but does not change the value."),
		}
	case strings.Contains(code, "P4-DECIMALS") && strings.Contains(s, "hundredths"):
		return []gin.H{
			cq("0.37 means...", "Read hundredths.", []string{"37 hundredths", "37 tenths", "3 ones and 7 tenths", "370 ones"}, "37 hundredths", "Two decimal places mean hundredths."),
			cq("In 4.28, the 8 is in the...", "Name the place.", []string{"ones place", "tenths place", "hundredths place", "thousandths place"}, "hundredths place", "4.28 has 2 tenths and 8 hundredths."),
			cq("Which is 5 hundredths?", "Write with two decimal places.", []string{"0.5", "0.05", "0.005", "5.00"}, "0.05", "Five hundredths is 5/100 = 0.05."),
		}
	case strings.Contains(code, "P4-DECIMALS") && strings.Contains(s, "thousandths"):
		return []gin.H{
			cq("0.125 means...", "Read thousandths.", []string{"125 thousandths", "125 hundredths", "12 tenths", "125 ones"}, "125 thousandths", "Three decimal places mean thousandths."),
			cq("In 6.304, the 4 is in the...", "Name the place.", []string{"ones place", "tenths place", "hundredths place", "thousandths place"}, "thousandths place", "The third digit after the decimal point is thousandths."),
			cq("Which is larger: 0.125 or 0.2?", "Write 0.2 as 0.200.", []string{"0.125", "0.2", "They are equal", "Cannot tell"}, "0.2", "0.200 is 200 thousandths, more than 125 thousandths."),
		}
	case strings.Contains(code, "P4-DECIMALS") && strings.Contains(s, "rounding decimals"):
		return []gin.H{
			cq("3.76 rounded to 1 decimal place is...", "Look at the hundredths digit.", []string{"3.7", "3.8", "4.0", "3.76"}, "3.8", "The hundredths digit is 6, so 3.7 rounds up to 3.8."),
			cq("2.349 rounded to 2 decimal places is...", "Look at the thousandths digit.", []string{"2.34", "2.35", "2.30", "2.349"}, "2.35", "The thousandths digit is 9, so 2.34 rounds up."),
			cq("Why keep trailing zeros sometimes?", "Show the requested place.", []string{"To show the precision asked for", "Because zeros are always bigger", "To change value", "To hide decimals"}, "To show the precision asked for", "2.50 shows two decimal places even though it equals 2.5."),
		}
	case strings.Contains(code, "P4-DECIMALS") && strings.Contains(s, "fractions and decimals"):
		return []gin.H{
			cq("7/10 as a decimal is...", "Tenths become one decimal place.", []string{"0.7", "0.07", "7.0", "0.17"}, "0.7", "Seven tenths is 0.7."),
			cq("35/100 as a decimal is...", "Hundredths become two decimal places.", []string{"0.35", "3.5", "0.035", "35.0"}, "0.35", "35 hundredths is 0.35."),
			cq("0.125 means...", "Read thousandths.", []string{"125/1000", "125/100", "12/5", "1/25"}, "125/1000", "Three decimal places means thousandths."),
		}
	case strings.Contains(code, "P4-DECIMALS") && strings.Contains(s, "addition"):
		return []gin.H{
			cq("$6.87 + $2.40 = ?", "Line up decimal points.", []string{"$9.27", "$8.127", "$9.07", "$8.47"}, "$9.27", "Add cents with cents and dollars with dollars."),
			cq("Why write 3.5 as 3.50 before adding 2.75?", "Match decimal places.", []string{"So hundredths line up", "To make it larger", "To remove tenths", "Because 3.5 is wrong"}, "So hundredths line up", "3.5 and 3.50 have the same value."),
			cq("4.08 + 0.7 should be treated as...", "Align places.", []string{"4.08 + 0.70", "4.08 + 0.07", "408 + 7", "4.8 + 0.7"}, "4.08 + 0.70", "0.7 is 7 tenths, not 7 hundredths."),
		}
	case strings.Contains(code, "P4-DECIMALS") && strings.Contains(s, "subtraction"):
		return []gin.H{
			cq("$10.00 - $6.87 = ?", "Subtract cents carefully.", []string{"$3.13", "$3.23", "$4.13", "$313"}, "$3.13", "10.00 - 6.87 = 3.13, so the decimal point matters."),
			cq("5.2 - 1.86 should be written as...", "Use equal decimal places.", []string{"5.20 - 1.86", "5.02 - 1.86", "52 - 186", "5.2 - 18.6"}, "5.20 - 1.86", "5.2 equals 5.20, which lets hundredths line up."),
			cq("Why can $3.13 not be written as 313?", "Money needs dollars and cents.", []string{"313 means three hundred thirteen dollars or cents without context", "Decimals are optional", "It is the same in money", "Units do not matter"}, "313 means three hundred thirteen dollars or cents without context", "The decimal point separates dollars and cents."),
		}
	case strings.Contains(code, "P4-DECIMALS") && strings.Contains(s, "multiplication"):
		return []gin.H{
			cq("2.4 × 3 means...", "Repeated equal groups.", []string{"2.4 + 2.4 + 2.4", "2.4 + 3", "24 × 30", "2.4 ÷ 3"}, "2.4 + 2.4 + 2.4", "Multiplying by 3 means three equal groups."),
			cq("0.6 × 4 = ?", "Six tenths four times.", []string{"2.4", "0.24", "24", "1.0"}, "2.4", "6 tenths × 4 = 24 tenths = 2.4."),
			cq("A good estimate for 3.9 × 5 is...", "Round 3.9 to 4.", []string{"about 20", "about 2", "about 200", "about 9"}, "about 20", "4×5=20, so the exact answer should be close."),
		}
	case strings.Contains(code, "P4-DECIMALS") && strings.Contains(s, "division"):
		return []gin.H{
			cq("4.8 ÷ 4 means...", "Share equally.", []string{"Share 4.8 into 4 equal parts", "Add 4.8 four times", "Ignore the decimal", "Divide 4 by 8"}, "Share 4.8 into 4 equal parts", "Division can mean equal sharing."),
			cq("3.6 ÷ 3 = ?", "Think 36 tenths divided by 3.", []string{"1.2", "12", "0.12", "6.3"}, "1.2", "36 tenths ÷ 3 = 12 tenths = 1.2."),
			cq("How can you check 5.4 ÷ 6 = 0.9?", "Use inverse.", []string{"0.9 × 6 = 5.4", "0.9 + 6 = 6.9", "5.4 × 6 = 32.4", "6 - 0.9 = 5.1"}, "0.9 × 6 = 5.4", "Division is checked by multiplication."),
		}
	case strings.Contains(code, "P4-DECIMALS") && strings.Contains(s, "word problems"):
		return []gin.H{
			cq("A toy costs $6.87. Maya pays $10.00. Change?", "Subtract cost from paid.", []string{"$3.13", "$3.23", "$293", "$313"}, "$3.13", "$10.00 - $6.87 = $3.13."),
			cq("Before solving a decimal word problem, first check...", "Units and operation.", []string{"money/length/unit and what is being found", "only the biggest number", "only the first decimal", "whether it has a picture"}, "money/length/unit and what is being found", "Decimals often represent money or measurement, so units matter."),
			cq("If 4 pens cost $9.60, one pen costs...", "Divide total by 4.", []string{"$2.40", "$2.04", "$3.40", "$38.40"}, "$2.40", "$9.60 ÷ 4 = $2.40."),
		}
	case strings.Contains(code, "P4-AREA-PERIMETER") && strings.Contains(s, "perimeter of rectangles"):
		return []gin.H{
			cq("A rectangle is 7 cm by 4 cm. Its perimeter is...", "Walk around outside.", []string{"28 cm²", "22 cm", "11 cm", "14 cm²"}, "22 cm", "Perimeter = 7+4+7+4."),
			cq("A square has side 6 cm. Its perimeter is...", "Four equal sides.", []string{"12 cm", "24 cm", "36 cm²", "6 cm"}, "24 cm", "A square perimeter is 4×side = 24 cm."),
			cq("Perimeter units are...", "Boundary length.", []string{"cm or m", "cm² only", "kg", "seconds"}, "cm or m", "Perimeter measures length around the outside."),
		}
	case strings.Contains(code, "P4-AREA-PERIMETER") && strings.Contains(s, "area of rectangles"):
		return []gin.H{
			cq("A rectangle is 7 cm by 4 cm. Its area is...", "Count unit squares.", []string{"28 cm²", "22 cm", "11 cm²", "14 cm"}, "28 cm²", "Area = length × breadth = 7×4."),
			cq("A square has side 6 cm. Its area is...", "Rows and columns.", []string{"12 cm", "24 cm", "36 cm²", "6 cm²"}, "36 cm²", "Area = 6×6 = 36 square centimetres."),
			cq("Area units are...", "Surface coverage.", []string{"cm² or m²", "cm only", "kg", "minutes"}, "cm² or m²", "Area counts square units covering the inside."),
		}
	case strings.Contains(code, "P4-AREA-PERIMETER") && strings.Contains(s, "rectangles and squares"):
		return []gin.H{
			cq("A rectangle is 7 cm by 4 cm. Its area is...", "Count unit squares.", []string{"28 cm²", "22 cm", "11 cm²", "14 cm"}, "28 cm²", "Area = length × breadth = 7×4."),
			cq("The same rectangle has perimeter...", "Walk around outside.", []string{"28 cm²", "22 cm", "11 cm", "14 cm²"}, "22 cm", "Perimeter = 7+4+7+4."),
			cq("Why do area and perimeter use different units?", "Inside vs outside.", []string{"Area uses square units; perimeter uses length units", "They are always the same", "Perimeter uses square units", "Area has no unit"}, "Area uses square units; perimeter uses length units", "Area covers surface; perimeter measures boundary length."),
		}
	case strings.Contains(code, "P4-AREA-PERIMETER") && strings.Contains(s, "composite figures"):
		return []gin.H{
			cq("For an L-shape, a good first step is...", "Make rectangles.", []string{"Split it into rectangles", "Add every visible number", "Guess the missing corner", "Use a protractor"}, "Split it into rectangles", "Composite area is found by splitting or subtracting rectangles."),
			cq("When finding composite perimeter, remember to...", "Walk only outside.", []string{"count only outer edges", "count inner split lines too", "multiply all sides", "ignore missing lengths"}, "count only outer edges", "Split lines help area but are not part of the outside boundary."),
			cq("Why find missing lengths?", "Opposite sides help.", []string{"To complete the outside walk or rectangle split", "To change cm to kg", "To avoid drawing", "To find an angle"}, "To complete the outside walk or rectangle split", "Composite shapes often hide side lengths that can be inferred."),
		}
	case strings.Contains(code, "P4-AREA-PERIMETER") && strings.Contains(s, "word problems"):
		return []gin.H{
			cq("A garden fence question usually asks for...", "Fence goes around.", []string{"perimeter", "area", "angle", "volume"}, "perimeter", "A fence follows the outside boundary."),
			cq("A floor-tile question usually asks for...", "Tiles cover inside.", []string{"area", "perimeter", "time", "mass"}, "area", "Tiles cover a surface."),
			cq("If a rectangle area is 48 cm² and length is 8 cm, breadth is...", "Use missing factor.", []string{"6 cm", "40 cm", "56 cm", "384 cm"}, "6 cm", "8 × ? = 48, so ? = 6."),
		}
	case strings.Contains(code, "P4-GEOMETRY-SYMMETRY") && strings.Contains(s, "making symmetric"):
		return []gin.H{
			cq("To complete a symmetric shape, copy each point...", "Mirror rule.", []string{"same distance on the other side", "anywhere on the page", "only upward", "twice as far away"}, "same distance on the other side", "Each point has a matching point across the mirror line."),
			cq("If a shape crosses the mirror line, points on the line...", "Fixed points.", []string{"stay on the line", "move far away", "disappear", "must become circles"}, "stay on the line", "A point on the mirror line reflects to itself."),
			cq("A pattern is symmetric only if...", "Check every part.", []string{"all matching parts reflect correctly", "one pair matches", "it uses bright colours", "it is made of squares"}, "all matching parts reflect correctly", "One correct part is not enough; the whole pattern must match."),
		}
	case strings.Contains(code, "P4-GEOMETRY-SYMMETRY") && strings.Contains(s, "symmetric figures"):
		return []gin.H{
			cq("A line of symmetry means...", "Fold test.", []string{"both sides match exactly", "one side is bigger", "the line is always horizontal", "there are no corners"}, "both sides match exactly", "If folded on the line, the two halves land on each other."),
			cq("A point 3 squares left of the mirror line should match a point...", "Same distance rule.", []string{"3 squares right", "1 square right", "on the line", "6 squares left"}, "3 squares right", "Mirror points are the same distance from the line."),
			cq("Why use a grid for symmetry?", "Count distance.", []string{"It helps match equal distances", "It hides the mirror line", "It changes the shape", "It removes corners"}, "It helps match equal distances", "Each reflected point can be counted across the mirror line."),
		}
	case strings.Contains(code, "P4-TIME") && strings.Contains(s, "seconds"):
		return []gin.H{
			cq("180 seconds = ?", "60 seconds make 1 minute.", []string{"1 min 80 s", "2 min", "3 min", "18 min"}, "3 min", "180 ÷ 60 = 3."),
			cq("3 min 20 s = ?", "Convert minutes first.", []string{"180 s", "200 s", "320 s", "380 s"}, "200 s", "3 min is 180 s, plus 20 s gives 200 s."),
			cq("Why use seconds for a short race?", "Choose sensible unit.", []string{"Seconds measure short durations clearly", "Minutes are always forbidden", "Hours are smaller", "Seconds are for money"}, "Seconds measure short durations clearly", "Short events need a smaller time unit."),
		}
	case strings.Contains(code, "P4-TIME") && (strings.Contains(s, "24-hour") || strings.Contains(s, "24 hour")):
		return []gin.H{
			cq("18:20 is...", "Convert from 24-hour time.", []string{"6:20 am", "6:20 pm", "8:20 pm", "18:20 am"}, "6:20 pm", "18 - 12 = 6, so it is 6:20 pm."),
			cq("7:05 pm in 24-hour time is...", "PM after noon adds 12.", []string{"07:05", "17:05", "19:05", "24:05"}, "19:05", "7 + 12 = 19."),
			cq("23:30 + 1 h 10 min = ?", "Cross midnight.", []string{"00:40", "12:40 pm", "24:40", "01:40"}, "00:40", "23:30 + 30 min = 00:00, plus 40 min = 00:40."),
		}
	case strings.Contains(s, "multiplying by a 1-digit"):
		return []gin.H{
			cq("426 × 3 means...", "Use place value.", []string{"400×3 + 20×3 + 6×3", "426 + 3", "426×30", "400×20×6"}, "400×3 + 20×3 + 6×3", "A 3-digit number can be split into hundreds, tens, and ones."),
			cq("Why estimate before multiplying?", "Check answer size.", []string{"To catch unreasonable answers", "To avoid all working", "To change the question", "To skip units"}, "To catch unreasonable answers", "An estimate tells whether the exact answer is in the right range."),
			cq("In 608 × 4, why does the zero matter?", "Place value.", []string{"It keeps the tens place", "It means ignore hundreds", "It changes 4 to 40", "It is decoration"}, "It keeps the tens place", "The zero shows there are no tens, but the hundreds and ones still keep their places."),
		}
	case strings.Contains(s, "multiplying by a 2-digit"):
		return []gin.H{
			cq("326 × 24 should be split into...", "Use the places in 24.", []string{"326×20 + 326×4", "326×2 + 326×4", "326+24", "326÷24"}, "326×20 + 326×4", "The 2 in 24 means 2 tens, so it is 20."),
			cq("Why is the tens row shifted left?", "It represents tens.", []string{"It is multiplying by 20, not 2", "It makes the answer prettier", "It avoids addition", "It is always optional"}, "It is multiplying by 20, not 2", "The tens digit creates a partial product in the tens place."),
			cq("Which check is sensible for 312 × 18?", "Estimate.", []string{"About 300×20", "About 3×2", "About 300÷20", "About 18-312"}, "About 300×20", "312 is near 300 and 18 is near 20, so the answer should be near 6000."),
		}
	case strings.Contains(s, "dividing by a 1-digit"):
		return []gin.H{
			cq("936 ÷ 3 starts with...", "Divide by place.", []string{"9 hundreds ÷ 3", "6 ones ÷ 3", "936 × 3", "3 ÷ 936"}, "9 hundreds ÷ 3", "Long division starts from the largest place."),
			cq("How do you check 936 ÷ 3 = 312?", "Use inverse.", []string{"312×3=936", "312+3=315", "936×3=2808", "936-312=624"}, "312×3=936", "Division is checked by multiplication."),
			cq("A remainder after a division step must be...", "Remainder rule.", []string{"smaller than the divisor", "larger than the quotient", "always zero", "ignored"}, "smaller than the divisor", "If it is not smaller, another group can still be made."),
		}
	case strings.Contains(s, "solving word problems"):
		return []gin.H{
			cq("Before choosing an operation, first ask...", "Understand the story.", []string{"What do we know and what are we finding?", "Which number is biggest?", "Can I add all numbers?", "Can I ignore units?"}, "What do we know and what are we finding?", "A word problem is a story; identify knowns and unknowns first."),
			cq("A bar model helps because it shows...", "Visual reasoning.", []string{"parts, whole, or comparison", "only handwriting", "the page number", "the font"}, "parts, whole, or comparison", "The model shows the relationship before calculation."),
			cq("For a two-step problem, what should she do?", "Avoid one-step guessing.", []string{"Find the first hidden value, then answer the final question", "Add every number", "Pick the smallest number", "Skip checking"}, "Find the first hidden value, then answer the final question", "Two-step problems need one result before the final answer."),
		}
	case strings.Contains(s, "understanding and measuring angles"):
		return []gin.H{
			cq("What does an angle measure?", "Focus on the meaning.", []string{"Arm length", "Turn between arms", "Line colour", "Shape area"}, "Turn between arms", "Angle size measures the turn/opening, not how long the arms are."),
			cq("An angle looks acute but reads 130°. What should you check?", "Diagnose the likely error.", []string{"Wrong protractor scale", "The pencil colour", "Whether 130 is acute", "The page number"}, "Wrong protractor scale", "If the drawing looks acute but the reading is obtuse, she likely read from the wrong 0°."),
			cq("Which benchmark is a right angle?", "Use the key anchor.", []string{"45°", "90°", "180°", "360°"}, "90°", "A right angle is one quarter turn."),
		}
	case strings.Contains(s, "drawing angles"):
		return []gin.H{
			cq("What should be placed on the vertex?", "Set up the protractor.", []string{"The protractor centre", "The 180° label", "The pencil tip only", "The ruler end"}, "The protractor centre", "The centre point anchors the turn at the vertex."),
			cq("To draw 120°, after drawing the base ray you should...", "Use the correct scale.", []string{"Guess the opening", "Mark 120° from the base ray's 0°", "Draw any rectangle", "Measure the longest side"}, "Mark 120° from the base ray's 0°", "The mark must be read from the same 0° side as the base ray."),
			cq("Why check the angle after drawing?", "Good drawing habit.", []string{"To prove the turn is correct", "To make the line darker", "To change the unit", "To avoid writing degrees"}, "To prove the turn is correct", "Measuring back catches a wrong scale or misplaced protractor."),
		}
	case strings.Contains(s, "8-point compass") || strings.Contains(s, "turns"):
		return []gin.H{
			cq("In an 8-point compass, each neighbouring step is...", "360° split into 8 parts.", []string{"30°", "45°", "60°", "90°"}, "45°", "360° ÷ 8 = 45° per compass step."),
			cq("N to E clockwise is...", "Count N→NE→E.", []string{"45°", "90°", "135°", "180°"}, "90°", "Two 45° steps make 90°."),
			cq("NE to S clockwise is...", "Count NE→E→SE→S.", []string{"45°", "90°", "135°", "180°"}, "135°", "Three 45° steps make 135°."),
		}
	case strings.Contains(s, "properties of squares and rectangles"):
		return []gin.H{
			cq("Every rectangle must have...", "Use properties, not the sketch.", []string{"4 right angles", "4 equal sides", "curved sides", "3 corners"}, "4 right angles", "A rectangle is defined by four right angles; opposite sides are equal."),
			cq("What extra property makes a square special?", "Compare square and rectangle.", []string{"All sides equal", "No right angles", "Only one pair of equal sides", "It is curved"}, "All sides equal", "A square is a rectangle with all four sides equal."),
			cq("If a sketch of a rectangle looks slanted, what should you trust?", "Drawings may not be exact.", []string{"The property rules and given labels", "Only the look", "Only the colour", "Ignore all angles"}, "The property rules and given labels", "In geometry, given properties are more reliable than an imperfect drawing."),
		}
	case strings.Contains(s, "drawing squares and rectangles"):
		return []gin.H{
			cq("To draw a rectangle accurately, first draw...", "Start with one side.", []string{"A base side and a right angle", "Any curve", "A line graph", "A random diagonal"}, "A base side and a right angle", "A rectangle needs right angles at its corners."),
			cq("To draw a square, which two properties must both hold?", "Use the definition.", []string{"Equal sides and right angles", "One long side and one curve", "Only equal sides", "Only parallel lines"}, "Equal sides and right angles", "A square needs four equal sides and four right angles."),
			cq("Why measure sides after drawing?", "Check accuracy.", []string{"To confirm opposite/equal sides match", "To find mass", "To read a scale", "To convert time"}, "To confirm opposite/equal sides match", "Checking sides catches a drawing that looks right but is not accurate."),
		}
	case strings.Contains(s, "comparing") && strings.Contains(s, "fractions"):
		return []gin.H{
			cq("Which is larger, 3/4 or 2/3?", "Use same denominator or model.", []string{"3/4", "2/3", "They are equal", "Cannot compare"}, "3/4", "3/4 = 9/12 and 2/3 = 8/12, so 3/4 is larger."),
			cq("Why must the whole be the same size?", "Fair comparison.", []string{"Fractions compare equal parts of the same whole", "The numerator always wins", "The denominator is ignored", "Mixed numbers cannot compare"}, "Fractions compare equal parts of the same whole", "A bigger-looking slice from a smaller whole may not be larger."),
			cq("A useful strategy for unlike fractions is...", "Make parts comparable.", []string{"Find a common denominator", "Count digits", "Choose the smaller denominator only", "Ignore the numerator"}, "Find a common denominator", "Common denominators make the parts the same size."),
		}
	case strings.Contains(s, "adding fractions"):
		return []gin.H{
			cq("1/4 + 2/4 = ?", "Same-size parts.", []string{"3/8", "3/4", "2/8", "1/2"}, "3/4", "Add numerators when the parts are the same size."),
			cq("Before adding unlike fractions, first...", "Make parts match.", []string{"Find equivalent fractions with a common denominator", "Add denominators directly", "Erase the numerator", "Round to whole numbers"}, "Find equivalent fractions with a common denominator", "You can only add parts directly when they are the same size."),
			cq("Why not add 1/3 + 1/6 as 2/9?", "Denominators name part size.", []string{"Thirds and sixths are different-sized parts", "9 is too large", "Numerators are not numbers", "Fractions cannot be added"}, "Thirds and sixths are different-sized parts", "Convert 1/3 to 2/6, then 2/6 + 1/6 = 3/6."),
		}
	case strings.Contains(s, "subtracting fractions"):
		return []gin.H{
			cq("3/4 - 1/4 = ?", "Same denominator.", []string{"2/4", "2/0", "4/8", "1/2 only"}, "2/4", "Taking away one fourth from three fourths leaves two fourths."),
			cq("Before subtracting unlike fractions, first...", "Make parts match.", []string{"Use a common denominator", "Subtract denominators", "Choose the bigger numerator", "Ignore units"}, "Use a common denominator", "The parts must be the same size before subtracting."),
			cq("What does 5/6 - 1/3 become with sixths?", "Convert thirds to sixths.", []string{"5/6 - 2/6", "5/6 - 1/6", "6/9 - 1/9", "5/3 - 1/6"}, "5/6 - 2/6", "1/3 is equivalent to 2/6, so subtract 2 sixths."),
		}
	case strings.Contains(code, "FRACTIONS") && strings.Contains(s, "word problems"):
		return []gin.H{
			cq("For a fraction word problem, what should you identify first?", "Find the whole.", []string{"The whole amount or whole set", "Only the denominator", "The longest sentence", "The page number"}, "The whole amount or whole set", "Fractions must refer to a whole."),
			cq("3/5 of 30 starts by finding...", "Unit fraction first.", []string{"1/5 of 30", "3+5+30", "30×5", "5/3"}, "1/5 of 30", "One fifth is 30÷5=6; then three fifths is 18."),
			cq("A bar model helps show...", "Reasoning before calculating.", []string{"the whole and the selected parts", "only the answer", "only the title", "only the unit"}, "the whole and the selected parts", "It makes the fraction relationship visible."),
		}
	case strings.Contains(s, "tables"):
		return []gin.H{
			cq("A table is best when you need...", "Use exact rows.", []string{"Exact values", "A compass direction", "A net", "A protractor"}, "Exact values", "Tables organise exact values by row and column."),
			cq("Before adding table values, check...", "Avoid wrong unit.", []string{"Heading and unit", "Font size", "Page colour", "Compass labels"}, "Heading and unit", "The heading tells whether the numbers are books, pupils, dollars, or something else."),
			cq("To compare Tue 20 and Mon 12, use...", "Difference means subtract.", []string{"20 - 12", "20 + 12", "20 × 12", "20 ÷ 12"}, "20 - 12", "A comparison asks how many more, so subtract."),
		}
	case strings.Contains(s, "line graphs"):
		return []gin.H{
			cq("A line graph is strongest for showing...", "Ordered values.", []string{"Change over time", "A square net", "A compass turn only", "Fraction bars only"}, "Change over time", "Connected points show movement across ordered data."),
			cq("If the scale is 1 step = 5, 6 steps means...", "Read the scale.", []string{"6", "11", "30", "60"}, "30", "6×5=30."),
			cq("A line from 12 to 20 shows...", "Find change.", []string{"increase by 8", "decrease by 8", "same", "increase by 32"}, "increase by 8", "20-12=8."),
		}
	}
	switch visualKindForTopic(code) {
	case "algorithm":
		return []gin.H{
			cq("Why split 326×24 into 326×20 and 326×4?", "Use place value.", []string{"Because 24 is 20+4", "Because 2 means 2 ones", "Because 4 is ignored", "Because division is easier"}, "Because 24 is 20+4", "The tens digit in 24 means twenty, so partial products keep place value."),
			cq("How do you check division?", "Use inverse operation.", []string{"divisor × quotient + remainder", "add all digits", "copy the question", "round every number"}, "divisor × quotient + remainder", "The check rebuilds the dividend."),
			cq("What catches an unreasonable product?", "Before exact work.", []string{"Estimate first", "Use a darker pencil", "Skip regrouping", "Hide the working"}, "Estimate first", "An estimate gives a size check for the final answer."),
		}
	case "fractionBars":
		return []gin.H{
			cq("A fraction model must use...", "Definition of fraction.", []string{"equal parts", "different-sized parts", "only whole numbers", "random slices"}, "equal parts", "Fractions name equal parts of a whole or set."),
			cq("7/4 means...", "Improper fraction.", []string{"1 3/4", "3/7", "less than 1", "7 wholes"}, "1 3/4", "Four fourths make one whole; seven fourths is 1 whole and 3 fourths."),
			cq("For fraction of a set, first find...", "Unit fraction.", []string{"one part", "the colour", "the biggest denominator", "the page number"}, "one part", "Find one equal part first, then multiply if needed."),
		}
	case "statistics":
		return []gin.H{
			cq("What should you read first?", "Avoid guessing from the picture.", []string{"Title, labels, and scale", "Only the biggest mark", "Only colours", "The last row"}, "Title, labels, and scale", "They tell what the data means."),
			cq("A pie chart slice only makes sense after you know...", "Part of whole.", []string{"the whole total", "the pencil colour", "the compass direction", "the page number"}, "the whole total", "Pie charts show parts of one whole."),
			cq("A misleading graph often uses...", "Critical reading.", []string{"a stretched or compressed scale", "too many right angles", "a missing denominator", "a square net"}, "a stretched or compressed scale", "Scale can change how dramatic the same data looks."),
		}
	default:
		return []gin.H{
			cq("Which model helps most for this lesson?", "Choose the thinking tool.", []string{"A random guess", "A diagram or place-value model", "Only the final answer", "Skipping the unit"}, "A diagram or place-value model", "A model shows what the numbers mean."),
			cq("What should the answer include?", "Think like a teacher checking reasoning.", []string{"Only the number", "Number, unit, and reason", "Only a drawing", "Only speed"}, "Number, unit, and reason", "The unit and reason show understanding."),
			cq("How do you prove you understand?", "Go beyond calculation.", []string{"Say why the method works", "Say it is easy", "Hide the working", "Copy the example"}, "Say why the method works", "Explaining the method is evidence of concept understanding."),
		}
	}
}

func lessonExamples(code, title string) []gin.H {
	s := strings.ToLower(title)
	switch {
	case strings.Contains(code, "P4-WHOLE-100000") && (strings.Contains(s, "numbers to 100 000") || strings.Contains(s, "numbers to 100,000")):
		return []gin.H{ex("Place value", "62,705 is 60,000 + 2,000 + 700 + 0 + 5. Each digit has value because of its place."), ex("Read aloud", "62,705 is sixty-two thousand, seven hundred and five; the zero keeps the tens place empty.")}
	case strings.Contains(code, "P4-WHOLE-100000") && strings.Contains(s, "comparing and ordering"):
		return []gin.H{ex("Compare by place", "58,402 is greater than 57,999 because both start with 5 ten-thousands, then 8 thousands is greater than 7 thousands."), ex("Order carefully", "Line up ten-thousands, thousands, hundreds, tens and ones before deciding.")}
	case strings.Contains(code, "P4-WHOLE-100000") && strings.Contains(s, "number pattern"):
		return []gin.H{ex("Find the rule", "12, 17, 22, 27 grows by 5 each step."), ex("Check every step", "A pattern rule must work between every neighbouring pair, not just the first two numbers.")}
	case strings.Contains(code, "P4-WHOLE-100000") && strings.Contains(s, "rounding and estimation"):
		return []gin.H{ex("Number line", "62,705 rounded to the nearest thousand is 63,000 because it is past the halfway point 62,500."), ex("Estimate first", "398×6 is close to 400×6=2400, so an exact answer near 2400 is reasonable.")}
	case strings.Contains(code, "P4-DECIMALS") && strings.Contains(s, "comparing") && strings.Contains(s, "decimals"):
		return []gin.H{ex("Line up places", "Compare 0.6 and 0.58 as 0.60 and 0.58; 0.60 is larger."), ex("Do not count digits", "0.125 is not automatically bigger than 0.5 just because it has more digits.")}
	case strings.Contains(code, "P4-DECIMALS") && strings.Contains(s, "rounding decimals"):
		return []gin.H{ex("Round to 1 decimal place", "3.76 rounds to 3.8 because the hundredths digit is 6."), ex("Round to 2 decimal places", "2.349 rounds to 2.35 because the thousandths digit is 9.")}
	case strings.Contains(code, "P4-DECIMALS") && strings.Contains(s, "fractions and decimals"):
		return []gin.H{ex("Tenths", "7/10 is 0.7 because tenths use one decimal place."), ex("Hundredths", "35/100 is 0.35 because hundredths use two decimal places.")}
	case strings.Contains(code, "P4-DECIMALS") && strings.Contains(s, "addition"):
		return []gin.H{ex("Decimal addition", "$6.87 + $2.40 = $9.27 after lining up dollars with dollars and cents with cents."), ex("Equal places", "3.5 + 2.75 is easier as 3.50 + 2.75.")}
	case strings.Contains(code, "P4-DECIMALS") && strings.Contains(s, "subtraction"):
		return []gin.H{ex("Money change", "$10.00 - $6.87 = $3.13; the decimal point separates dollars and cents."), ex("Regroup", "5.20 - 1.86 uses 5.20, not 5.2, so hundredths line up.")}
	case strings.Contains(code, "P4-DECIMALS") && strings.Contains(s, "multiplication"):
		return []gin.H{ex("Repeated decimals", "2.4 × 3 means 2.4 + 2.4 + 2.4 = 7.2."), ex("Estimate", "3.9 × 5 is close to 4 × 5 = 20, so 19.5 is reasonable.")}
	case strings.Contains(code, "P4-DECIMALS") && strings.Contains(s, "division"):
		return []gin.H{ex("Share equally", "4.8 ÷ 4 means share 4.8 into 4 equal parts, giving 1.2 each."), ex("Inverse check", "If 5.4 ÷ 6 = 0.9, then 0.9 × 6 should return 5.4.")}
	case strings.Contains(code, "P4-DECIMALS") && strings.Contains(s, "word problems"):
		return []gin.H{ex("Money word problem", "If a toy costs $6.87 and Maya pays $10.00, change is $10.00 - $6.87 = $3.13."), ex("Unit price", "$9.60 for 4 pens means one pen costs $9.60 ÷ 4 = $2.40.")}
	case strings.Contains(code, "P4-AREA-PERIMETER") && strings.Contains(s, "perimeter of rectangles"):
		return []gin.H{ex("Perimeter", "A 7 cm by 4 cm rectangle has perimeter 7+4+7+4=22 cm because you walk around the outside."), ex("Square perimeter", "A square with side 6 cm has perimeter 6+6+6+6=24 cm.")}
	case strings.Contains(code, "P4-AREA-PERIMETER") && strings.Contains(s, "area of rectangles"):
		return []gin.H{ex("Area", "A 7 cm by 4 cm rectangle has area 7×4=28 cm² because it covers 28 unit squares."), ex("Square area", "A square with side 6 cm has area 6×6=36 cm².")}
	case strings.Contains(code, "P4-AREA-PERIMETER") && strings.Contains(s, "rectangles and squares"):
		return []gin.H{ex("Area", "A 7 cm by 4 cm rectangle has area 7×4=28 cm² because it covers 28 unit squares."), ex("Perimeter", "The same rectangle has perimeter 7+4+7+4=22 cm because you walk around the outside.")}
	case strings.Contains(code, "P4-AREA-PERIMETER") && strings.Contains(s, "composite figures"):
		return []gin.H{ex("Split", "Split an L-shape into two rectangles, find each area, then add."), ex("Subtract", "Or start with a large rectangle and subtract the missing corner.")}
	case strings.Contains(code, "P4-AREA-PERIMETER") && strings.Contains(s, "word problems"):
		return []gin.H{ex("Fence problem", "A fence asks for perimeter because it goes around the outside."), ex("Tile problem", "Tiles ask for area because they cover the inside surface.")}
	case strings.Contains(code, "P4-GEOMETRY-SYMMETRY") && strings.Contains(s, "making symmetric"):
		return []gin.H{ex("Complete the shape", "Copy each point to the same distance on the other side of the mirror line."), ex("Points on the line", "A point on the mirror line stays where it is after reflection.")}
	case strings.Contains(code, "P4-GEOMETRY-SYMMETRY") && strings.Contains(s, "symmetric figures"):
		return []gin.H{ex("Fold test", "A line of symmetry splits a shape into two matching halves."), ex("Same distance", "A point 3 squares left of the mirror line reflects to a point 3 squares right.")}
	case strings.Contains(s, "understanding and measuring angles"):
		return []gin.H{ex("Angle as turn", "The angle is the turn between two arms; longer arms do not make a bigger angle."), ex("Protractor read", "Put the centre on the vertex, line one arm with 0°, then read from that same side.")}
	case strings.Contains(s, "drawing angles"):
		return []gin.H{ex("Draw 120°", "Draw a base ray, mark 120° from the correct 0° scale, then draw the second ray through the mark."), ex("Check", "Measure the angle again after drawing to catch a wrong scale.")}
	case strings.Contains(s, "8-point compass") || strings.Contains(s, "turns"):
		return []gin.H{ex("Compass step", "An 8-point compass has 8 equal steps, so each step is 45°."), ex("N to E", "N→NE→E is two steps, so the clockwise turn is 90°.")}
	case strings.Contains(s, "properties of squares and rectangles"):
		return []gin.H{ex("Rectangle", "Every rectangle has 4 right angles and opposite sides equal."), ex("Square", "A square has 4 right angles and 4 equal sides, so it is a special rectangle.")}
	case strings.Contains(s, "drawing squares and rectangles"):
		return []gin.H{ex("Rectangle drawing", "Draw one side, make right angles at both ends, then draw the opposite side parallel."), ex("Square drawing", "Use right angles and equal side lengths; both conditions must hold.")}
	case strings.Contains(s, "24-hour") || strings.Contains(s, "24 hour"):
		return []gin.H{ex("24-hour time", "18:45 means 6:45 pm because times after noon add 12 to the hour."), ex("Boundary check", "23:30 + 1 h 10 min becomes 00:40 the next day; after 23:59 the clock returns to 00:00.")}
	case strings.Contains(s, "seconds"):
		return []gin.H{ex("Seconds to minutes", "125 seconds is 2 min 5 s because 120 seconds make 2 minutes, with 5 seconds left."), ex("Timer reading", "3 min 20 s is 200 seconds because 3×60=180, and 180+20=200.")}
	case strings.Contains(s, "factor"):
		return []gin.H{ex("Factor pairs", "For 24, factor pairs are 1×24, 2×12, 3×8, and 4×6. Each pair makes exactly 24."), ex("Check a factor", "6 is a factor of 42 because 42÷6=7 with no remainder.")}
	case strings.Contains(s, "multiple"):
		return []gin.H{ex("Landing points", "Multiples of 6 are 6, 12, 18, 24, 30 because you keep adding 6."), ex("Common multiple", "24 is a common multiple of 6 and 8 because both 6×4 and 8×3 land on 24.")}
	case strings.Contains(s, "rounding"):
		return []gin.H{ex("Number line", "62,705 rounded to the nearest thousand is 63,000 because it is past the halfway point 62,500."), ex("Estimate first", "398×6 is close to 400×6=2400, so an exact answer near 2400 is reasonable.")}
	case strings.Contains(s, "number pattern"):
		return []gin.H{ex("Find the rule", "12, 17, 22, 27 grows by 5 each step."), ex("Check every step", "A pattern rule must work between every pair, not just the first two numbers.")}
	case strings.Contains(s, "multiplying by a 2-digit"):
		return []gin.H{ex("Partial products", "326×24 means 326×20 plus 326×4, then add both parts."), ex("Estimate check", "326×24 is close to 300×20=6000, so 7824 is reasonable.")}
	case strings.Contains(s, "multiplying by a 1-digit"):
		return []gin.H{ex("Place value multiplication", "426×3 means 400×3 + 20×3 + 6×3."), ex("Regrouping", "When 6×8 gives 48, write 8 and regroup 4 tens.")}
	case strings.Contains(s, "dividing by a 1-digit"):
		return []gin.H{ex("Long division scaffold", "648÷3: 6 hundreds ÷3=2 hundreds, 4 tens ÷3 leaves 1 ten, then divide 18 ones."), ex("Inverse check", "If 648÷3=216, check with 216×3=648.")}
	case strings.Contains(s, "solving word problems"):
		return []gin.H{ex("Bar model", "Draw what is known and unknown before choosing an operation."), ex("Two-step thinking", "If a problem asks for 'how many left after buying more', add first, then subtract.")}
	case strings.Contains(s, "mixed number"):
		return []gin.H{ex("Whole plus fraction", "2 3/4 means 2 wholes and 3 quarters."), ex("Number line", "2 3/4 sits between 2 and 3, closer to 3.")}
	case strings.Contains(s, "improper fraction"):
		return []gin.H{ex("More than one whole", "7/4 means seven quarters: 4 quarters make 1 whole, with 3 quarters left."), ex("Convert", "7/4 = 1 3/4 because 7÷4=1 remainder 3.")}
	case strings.Contains(s, "comparing") && strings.Contains(s, "fractions"):
		return []gin.H{ex("Same whole", "Compare fractions only when the whole is the same size."), ex("Common denominator", "3/4 is 9/12 and 2/3 is 8/12, so 3/4 is larger.")}
	case strings.Contains(s, "adding fractions"):
		return []gin.H{ex("Same denominator", "1/4 + 2/4 = 3/4 because the parts are all fourths."), ex("Unlike denominators", "Convert to same-size parts before adding.")}
	case strings.Contains(s, "subtracting fractions"):
		return []gin.H{ex("Same denominator", "3/4 - 1/4 = 2/4 because you take away one fourth."), ex("Unlike denominators", "Convert 1/3 to 2/6 before subtracting from sixths.")}
	case strings.Contains(s, "fraction of a set"):
		return []gin.H{ex("Set model", "1/4 of 20 counters is 5 counters because 20÷4=5."), ex("Multiple parts", "3/5 of 30 is 18 because one fifth is 6, and three fifths is 18.")}
	case strings.Contains(s, "fraction") && strings.Contains(s, "word problems"):
		return []gin.H{ex("Bar model", "Draw the whole set or whole amount before choosing the fraction operation."), ex("Unit fraction first", "For 3/5 of 30, find 1/5 first: 30÷5=6, then 3 parts is 18.")}
	case strings.Contains(code, "FRACTIONS") && strings.Contains(s, "word problems"):
		return []gin.H{ex("Bar model", "Draw the whole set or whole amount before choosing the fraction operation."), ex("Unit fraction first", "For 3/5 of 30, find 1/5 first: 30÷5=6, then 3 parts is 18.")}
	case strings.Contains(s, "comparing") && strings.Contains(s, "ordering"):
		return []gin.H{ex("Compare by place", "58,402 is greater than 57,999 because the thousands digit 8 is greater than 7."), ex("Order carefully", "Line up ten-thousands, thousands, hundreds, tens, ones before deciding.")}
	case strings.Contains(s, "tenths"):
		return []gin.H{ex("Tenths", "0.6 means 6 tenths, the same as 6/10."), ex("Equivalent decimal", "0.6 = 0.60 because 6 tenths equals 60 hundredths.")}
	case strings.Contains(s, "hundredths"):
		return []gin.H{ex("Hundredths", "0.37 means 37 hundredths."), ex("Place value", "In 4.28, the 8 is in the hundredths place.")}
	case strings.Contains(s, "thousandths"):
		return []gin.H{ex("Thousandths", "0.125 means 125 thousandths."), ex("Compare", "0.125 is smaller than 0.2 because 0.200 has more thousandths.")}
	case strings.Contains(s, "comparing") && strings.Contains(s, "decimals"):
		return []gin.H{ex("Line up places", "Compare 0.6 and 0.58 as 0.60 and 0.58; 0.60 is larger."), ex("Do not count digits", "0.125 is not automatically bigger than 0.5 just because it has more digits.")}
	case strings.Contains(s, "rectangles and squares"):
		return []gin.H{ex("Perimeter", "A 7 cm by 4 cm rectangle has perimeter 7+4+7+4=22 cm."), ex("Area", "The same rectangle has area 7×4=28 square cm because it covers 28 unit squares.")}
	case strings.Contains(s, "composite figures"):
		return []gin.H{ex("Split", "Split an L-shape into two rectangles, find each area, then add."), ex("Subtract", "Or start with a large rectangle and subtract the missing corner.")}
	case strings.Contains(s, "table"):
		return []gin.H{ex("Exact values", "A table lets you read exact values before comparing."), ex("Total", "Add the row values only after checking the unit in the heading.")}
	case strings.Contains(s, "line graph"):
		return []gin.H{ex("Trend", "A line moving from 12 to 20 shows an increase of 8."), ex("Scale", "If each grid step is 5, a point at 6 steps means 30.")}
	}
	switch visualKindForTopic(code) {
	case "place":
		return []gin.H{ex("Example 1", "Use place value columns so each digit keeps its meaning."), ex("Example 2", "Estimate first, then calculate, then compare the result with the estimate.")}
	case "division":
		return []gin.H{ex("Example 1", "Draw equal groups before writing the equation."), ex("Example 2", "Check division with multiplication: divisor × quotient + remainder.")}
	case "decimal":
		return []gin.H{ex("Example 1", "Line up decimal points so tenths meet tenths and hundredths meet hundredths."), ex("Example 2", "Use equivalent forms such as 0.6 = 0.60 when comparing or adding.")}
	case "measure":
		return []gin.H{ex("Example 1", "Convert to one unit before adding or subtracting."), ex("Example 2", "Write the unit in the final answer because the number alone is incomplete.")}
	case "time":
		return []gin.H{ex("Example 1", "Use a timeline and jump to the next easy time first."), ex("Example 2", "Check am/pm when crossing noon or midnight.")}
	case "area":
		return []gin.H{ex("Example 1", "Area counts inside squares; perimeter walks around the outside."), ex("Example 2", "Split composite figures into rectangles before calculating.")}
	default:
		return []gin.H{ex("Example 1", "Name the model before calculating."), ex("Example 2", "Explain why the answer fits the situation.")}
	}
}

func ex(title, body string) gin.H { return gin.H{"title": title, "body": body} }

// withDiagram names a curriculum diagram (a file in web/static/curriculum) that
// the lesson UI shows together with the item.
func withDiagram(item gin.H, file string) gin.H {
	item["diagram"] = file
	return item
}

func mis(mistake, fix string) gin.H { return gin.H{"mistake": mistake, "fix": fix} }
func cq(prompt, sub string, options []string, correct, explain string) gin.H {
	return gin.H{"prompt": prompt, "sub": sub, "options": options, "correct": correct, "explain": explain}
}

func curriculumSeedTopics() []seedTopic {
	return []seedTopic{
		{"UNMAPPED", "Mapped", "Textbook Mapping", "Unmapped", "Unmapped textbook chapters", []string{"Temporary holding topic for textbook chapters that need parent confirmation."}, nil, nil, 0},
		{"P1-WHOLE-100", "P1", "Number and Algebra", "Whole Numbers", "Numbers up to 100", []string{"Count objects in a set.", "Represent tens and ones.", "Read, write, compare and order numbers.", "Recognise simple number patterns and ordinal numbers."}, nil, []string{"Table Chart"}, 101},
		{"P1-ADD-SUB", "P1", "Number and Algebra", "Whole Numbers", "Addition and Subtraction", []string{"Understand addition and subtraction.", "Use +, - and =.", "Relate addition and subtraction.", "Add/subtract within 100 and calculate mentally within 20."}, []string{"P1-WHOLE-100"}, []string{"Add & Sub"}, 102},
		{"P1-MULT-DIV", "P1", "Number and Algebra", "Whole Numbers", "Multiplication and Division", []string{"Understand multiplication and division.", "Use multiplication sign.", "Multiply within 40 and divide within 20."}, []string{"P1-ADD-SUB"}, []string{"Times Tables", "Division Sense"}, 103},
		{"P1-MONEY", "P1", "Number and Algebra", "Money", "Money", []string{"Count cents up to $1.", "Count dollars up to $100."}, []string{"P1-WHOLE-100"}, nil, 104},
		{"P1-LENGTH", "P1", "Measurement and Geometry", "Measurement", "Length in centimetres", []string{"Measure, compare and order length in cm.", "Draw line segments to the nearest cm."}, []string{"P1-WHOLE-100"}, nil, 105},
		{"P1-TIME", "P1", "Measurement and Geometry", "Measurement", "Time", []string{"Tell time to 5 minutes.", "Use am/pm and h/min.", "Understand one hour and half-hour duration."}, []string{"P1-WHOLE-100"}, []string{"Clock Time"}, 106},
		{"P1-SHAPES", "P1", "Measurement and Geometry", "Geometry", "2D Shapes", []string{"Identify, name, describe and classify basic 2D shapes.", "Form and copy figures on grids."}, nil, nil, 107},
		{"P1-STATISTICS", "P1", "Statistics", "Data Representation and Interpretation", "Picture Graphs", []string{"Read and interpret data from picture graphs."}, nil, nil, 108},

		{"P2-WHOLE-1000", "P2", "Number and Algebra", "Whole Numbers", "Numbers up to 1000", []string{"Count in tens and hundreds.", "Use hundreds, tens and ones.", "Compare, order and recognise number patterns.", "Identify odd and even numbers."}, []string{"P1-WHOLE-100"}, []string{"Table Chart"}, 201},
		{"P2-ADD-SUB", "P2", "Number and Algebra", "Whole Numbers", "Addition and Subtraction up to 3 digits", []string{"Use addition and subtraction algorithms up to 3 digits.", "Calculate mentally with 3-digit numbers and ones/tens/hundreds."}, []string{"P1-ADD-SUB", "P2-WHOLE-1000"}, []string{"Add & Sub", "Long Form"}, 202},
		{"P2-MULT-DIV", "P2", "Number and Algebra", "Whole Numbers", "Multiplication and Division Tables 2,3,4,5,10", []string{"Know multiplication tables of 2, 3, 4, 5 and 10.", "Use division sign.", "Relate multiplication and division.", "Multiply and divide within known tables."}, []string{"P1-MULT-DIV"}, []string{"Times Tables", "Division Sense"}, 203},
		{"P2-FRACTIONS", "P2", "Number and Algebra", "Fractions", "Fraction of a Whole", []string{"Understand fractions as part of a whole.", "Represent, compare and order unit and like fractions with denominators up to 12.", "Add and subtract like fractions within one whole."}, []string{"P1-WHOLE-100"}, nil, 204},
		{"P2-MONEY", "P2", "Number and Algebra", "Money", "Money in dollars and cents", []string{"Count money in dollars and cents.", "Read/write decimal money notation.", "Compare amounts and convert dollars/cents."}, []string{"P2-WHOLE-1000"}, nil, 205},
		{"P2-MEASUREMENT", "P2", "Measurement and Geometry", "Measurement", "Length, Mass and Volume", []string{"Measure length in metres, mass in kg/g and liquid volume in litres.", "Use m, g, kg and litre.", "Compare and order lengths, masses and volumes."}, []string{"P1-LENGTH"}, []string{"Weight"}, 206},
		{"P2-TIME", "P2", "Measurement and Geometry", "Measurement", "Time to the minute", []string{"Tell time to the minute.", "Measure time in hours and minutes.", "Convert hours/minutes to minutes only and vice versa."}, []string{"P1-TIME"}, []string{"Clock Time"}, 207},
		{"P2-GEOMETRY", "P2", "Measurement and Geometry", "Geometry", "2D and 3D Shapes", []string{"Make patterns with 2D shapes.", "Identify, name, describe and classify cube, cuboid, cone, cylinder and sphere."}, []string{"P1-SHAPES"}, nil, 208},
		{"P2-STATISTICS", "P2", "Statistics", "Data Representation and Interpretation", "Picture Graphs with Scales", []string{"Read and interpret data from picture graphs with scales."}, []string{"P1-STATISTICS"}, nil, 209},

		{"P3-WHOLE-10000", "P3", "Number and Algebra", "Whole Numbers", "Numbers up to 10,000", []string{"Count in hundreds and thousands.", "Use thousands, hundreds, tens and ones.", "Read, write, compare and order numbers.", "Recognise number sequences."}, []string{"P2-WHOLE-1000"}, []string{"Table Chart"}, 301},
		{"P3-ADD-SUB", "P3", "Number and Algebra", "Whole Numbers", "Addition and Subtraction up to 4 digits", []string{"Use addition and subtraction algorithms up to 4 digits.", "Mentally add/subtract two 2-digit numbers."}, []string{"P2-ADD-SUB", "P3-WHOLE-10000"}, []string{"Add & Sub", "Long Form"}, 302},
		{"P3-MULT-DIV", "P3", "Number and Algebra", "Whole Numbers", "Multiplication and Division", []string{"Learn multiplication tables of 6, 7, 8 and 9.", "Multiply and divide within multiplication tables.", "Understand division with remainder.", "Use multiplication/division algorithms up to 3 digits by 1 digit.", "Calculate mentally within multiplication tables."}, []string{"P2-MULT-DIV"}, []string{"Times Tables", "Division Sense", "Long Form"}, 303},
		{"P3-FRACTIONS", "P3", "Number and Algebra", "Fractions", "Equivalent and Related Fractions", []string{"Find equivalent fractions.", "Express fractions in simplest form.", "Compare and order unlike fractions with denominators up to 12.", "Add/subtract two related fractions within one whole."}, []string{"P2-FRACTIONS"}, nil, 304},
		{"P3-MONEY", "P3", "Number and Algebra", "Money", "Adding and Subtracting Money", []string{"Add and subtract money in decimal notation."}, []string{"P2-MONEY", "P3-ADD-SUB"}, nil, 305},
		{"P3-MEASUREMENT", "P3", "Measurement and Geometry", "Measurement", "Length, Mass and Volume in Compound Units", []string{"Measure km and ml.", "Measure length, mass and liquid volume in compound units.", "Convert km/m, m/cm, kg/g and l/ml within easy numbers."}, []string{"P2-MEASUREMENT"}, []string{"Weight"}, 306},
		{"P3-TIME", "P3", "Measurement and Geometry", "Measurement", "Time Duration and 24-hour Clock", []string{"Measure time in seconds.", "Find starting time, finishing time or duration when given the other two.", "Use the 24-hour clock."}, []string{"P2-TIME"}, []string{"Clock Time"}, 307},
		{"P3-AREA-PERIMETER", "P3", "Measurement and Geometry", "Area and Volume", "Area and Perimeter", []string{"Understand area and perimeter.", "Measure area in square units, cm² and m².", "Find perimeter of rectilinear figures, rectangles and squares.", "Find area of rectangles and squares."}, []string{"P2-MEASUREMENT", "P3-MULT-DIV"}, nil, 308},
		{"P3-GEOMETRY", "P3", "Measurement and Geometry", "Geometry", "Angles, Perpendicular and Parallel Lines", []string{"Understand angles.", "Identify right angles and angles greater/smaller than right angle.", "Identify and draw perpendicular and parallel lines."}, []string{"P2-GEOMETRY"}, nil, 309},
		{"P3-STATISTICS", "P3", "Statistics", "Data Representation and Interpretation", "Bar Graphs", []string{"Read and interpret data from bar graphs.", "Use different scales on an axis."}, []string{"P2-STATISTICS"}, nil, 310},

		{"P4-WHOLE-100000", "P4", "Number and Algebra", "Whole Numbers", "Numbers up to 100,000", []string{"Use ten thousands to ones place values.", "Read, write, compare and order numbers.", "Recognise number patterns.", "Round to nearest 10, 100 or 1000.", "Use approximately equal sign."}, []string{"P3-WHOLE-10000"}, []string{"Table Chart"}, 401},
		{"P4-FACTORS-MULTIPLES", "P4", "Number and Algebra", "Whole Numbers", "Factors and Multiples", []string{"Understand factors, multiples and their relationship.", "Determine if a 1-digit number is a factor of a number within 100.", "Find common factors and common multiples."}, []string{"P3-MULT-DIV"}, []string{"Times Tables", "Division Sense"}, 402},
		{"P4-FOUR-OPERATIONS", "P4", "Number and Algebra", "Whole Numbers", "Four Operations Algorithms", []string{"Multiply up to 4 digits by 1 digit.", "Multiply up to 3 digits by 2 digits.", "Divide up to 4 digits by 1 digit."}, []string{"P3-ADD-SUB", "P3-MULT-DIV", "P4-WHOLE-100000"}, []string{"Long Form", "Times Tables", "Division Sense"}, 403},
		{"P4-FRACTIONS-MIXED", "P4", "Number and Algebra", "Fractions", "Mixed Numbers, Improper Fractions and Fraction of a Set", []string{"Understand mixed numbers and improper fractions.", "Understand fraction as part of a set.", "Add/subtract fractions with denominators up to 12 and not more than two different denominators."}, []string{"P3-FRACTIONS", "P3-MULT-DIV"}, nil, 404},
		{"P4-DECIMALS", "P4", "Number and Algebra", "Decimals", "Decimals up to 3 Decimal Places", []string{"Use tenths, hundredths and thousandths.", "Compare and order decimals.", "Convert decimals and fractions when denominators are factors of 10 or 100.", "Round decimals.", "Add/subtract decimals up to 2 decimal places.", "Multiply/divide decimals up to 2 decimal places by a 1-digit whole number."}, []string{"P3-FRACTIONS", "P4-WHOLE-100000"}, nil, 405},
		{"P4-AREA-PERIMETER", "P4", "Measurement and Geometry", "Area and Volume", "Area and Perimeter of Composite Figures", []string{"Find one dimension of a rectangle from area/perimeter.", "Find side of square from area/perimeter.", "Find area and perimeter of composite figures made of rectangles and squares."}, []string{"P3-AREA-PERIMETER", "P4-FOUR-OPERATIONS"}, nil, 406},
		{"P4-GEOMETRY-ANGLES", "P4", "Measurement and Geometry", "Geometry", "Angles, Rectangles and Squares", []string{"Name angles using notation.", "Measure angles in degrees.", "Draw angles of given size.", "Use properties of rectangles and squares and draw them."}, []string{"P3-GEOMETRY"}, nil, 407},
		{"P4-GEOMETRY-SYMMETRY", "P4", "Measurement and Geometry", "Geometry", "Line Symmetry", []string{"Identify symmetric figures.", "Determine lines of symmetry.", "Complete symmetric figures on square grid."}, []string{"P2-GEOMETRY"}, nil, 408},
		{"P4-GEOMETRY-NETS", "P4", "Measurement and Geometry", "Geometry", "Nets and 2D Representations of Solids", []string{"Identify 2D representations of cube, cuboid, cone, cylinder, prism and pyramid.", "Draw 2D representations of cube, cuboid, prism and pyramid.", "Identify nets and the solid formed by a net."}, []string{"P2-GEOMETRY"}, nil, 409},
		{"P4-STATISTICS", "P4", "Statistics", "Data Representation and Interpretation", "Tables and Line Graphs", []string{"Complete tables from data.", "Read and interpret data from tables and line graphs.", "Read scales and describe changes in data over time."}, []string{"P3-STATISTICS"}, nil, 410},
		{"P4-TIME", "P4", "Measurement and Geometry", "Measurement", "Seconds and 24-Hour Clock", []string{"Measure time in seconds.", "Convert between seconds and minutes.", "Use the 24-hour clock.", "Find start time, finish time or duration using a timeline.", "Solve time questions that cross noon or midnight."}, []string{"P3-TIME"}, []string{"Clock Time"}, 411},

		{"P5-WHOLE-OPERATIONS", "P5", "Number and Algebra", "Whole Numbers", "Numbers up to 10 million and Order of Operations", []string{"Read and write numbers up to 10 million.", "Multiply/divide by 10, 100, 1000 and multiples.", "Use order of operations and brackets without calculator.", "Solve multi-step word problems using models and checks."}, []string{"P4-WHOLE-100000", "P4-FOUR-OPERATIONS"}, nil, 501},
		{"P5-FRACTIONS", "P5", "Number and Algebra", "Fractions", "Fraction and Division; Four Operations", []string{"Divide whole numbers with quotient as fraction.", "Express fractions as decimals.", "Add/subtract mixed numbers.", "Multiply fractions and whole numbers."}, []string{"P4-FRACTIONS-MIXED", "P4-DECIMALS"}, nil, 502},
		{"P5-AREA-VOLUME", "P5", "Measurement and Geometry", "Area and Volume", "Area of Triangle and Volume of Cube/Cuboid", []string{"Understand base and height of triangle.", "Find area of triangle and composite figures.", "Build solids with unit cubes.", "Measure and calculate volume of cube/cuboid and rectangular tank."}, []string{"P4-AREA-PERIMETER"}, nil, 503},
		{"P5-RATIO", "P5", "Number and Algebra", "Ratio", "Ratio and Equivalent Ratios", []string{"Understand ratio as a comparison between quantities.", "Find equivalent ratios.", "Compare three quantities using ratio.", "Solve ratio word problems using units and bar models."}, []string{"P4-FRACTIONS-MIXED", "P4-FACTORS-MULTIPLES"}, nil, 504},
		{"P5-DECIMALS", "P5", "Number and Algebra", "Decimals", "Decimals and Measurement Conversion", []string{"Multiply/divide decimals by 10, 100, 1000 and multiples.", "Convert measurements between smaller and larger units in decimal form.", "Solve decimal measurement word problems."}, []string{"P4-DECIMALS", "P3-MEASUREMENT"}, nil, 505},
		{"P5-PERCENTAGE-RATE", "P5", "Number and Algebra", "Percentage and Rate", "Percentage and Rate", []string{"Express part of whole as percentage.", "Find percentage part of a whole.", "Find discount, GST and annual interest.", "Understand rate and find rate/total/unit count."}, []string{"P4-FRACTIONS-MIXED", "P4-DECIMALS", "P5-RATIO"}, nil, 506},
		{"P5-AVERAGE", "P5", "Statistics", "Data Analysis", "Average", []string{"Understand average as equal sharing of a total.", "Find average from total and number of values.", "Find total from average and number of values.", "Use average to interpret data carefully."}, []string{"P4-STATISTICS", "P5-WHOLE-OPERATIONS"}, nil, 507},
		{"P5-GEOMETRY", "P5", "Measurement and Geometry", "Geometry", "Angles, Triangles and Quadrilaterals", []string{"Use angles on a straight line, at a point and vertically opposite angles.", "Find unknown angles.", "Use properties and angle sum of triangles.", "Use properties of parallelogram, rhombus and trapezium."}, []string{"P4-GEOMETRY-ANGLES"}, nil, 508},

		{"P6-FRACTIONS", "P6", "Number and Algebra", "Fractions", "Division of Fractions", []string{"Divide a proper fraction by a whole number.", "Divide a whole number or proper fraction by a proper fraction."}, []string{"P5-FRACTIONS"}, nil, 601},
		{"P6-PERCENTAGE-RATIO", "P6", "Number and Algebra", "Percentage and Ratio", "Percentage and Ratio", []string{"Find whole given part and percentage.", "Find percentage increase/decrease.", "Use ratio notation and equivalent ratios.", "Divide quantities in a ratio and find missing terms.", "Relate fractions and ratios."}, []string{"P5-PERCENTAGE-RATE", "P5-FRACTIONS"}, nil, 602},
		{"P6-ALGEBRA", "P6", "Number and Algebra", "Algebra", "Simple Algebra", []string{"Use letters for unknown numbers.", "Interpret simple algebraic expressions.", "Simplify simple linear expressions.", "Evaluate expressions by substitution.", "Solve simple linear equations with whole number coefficients."}, []string{"P5-WHOLE-OPERATIONS"}, nil, 603},
		{"P6-AREA-VOLUME", "P6", "Measurement and Geometry", "Area and Volume", "Circle Area/Circumference and Cube/Cuboid Volume", []string{"Find area and circumference of circles.", "Find area/perimeter of semicircles and quarter circles.", "Find area/perimeter of composite figures.", "Find unknown dimensions of cuboids and cubes from volume."}, []string{"P5-AREA-VOLUME"}, nil, 604},
		{"P6-GEOMETRY", "P6", "Measurement and Geometry", "Geometry", "Special Quadrilaterals and Unknown Angles", []string{"Find unknown angles in composite figures involving square, rectangle, triangle, parallelogram, rhombus and trapezium."}, []string{"P5-GEOMETRY"}, nil, 605},
		{"P6-STATISTICS", "P6", "Statistics", "Data Analysis", "Average of a Set of Data", []string{"Understand average as total value divided by number of data.", "Use relationship between average, total value and number of data."}, []string{"P4-STATISTICS"}, nil, 606},
	}
}

func curriculumSeedTextbookMappings() []seedTextbookMapping {
	const src = "my-pals-3rd-edition-contents"
	m := func(code, book, title string) seedTextbookMapping {
		return seedTextbookMapping{TopicCode: code, Book: book, ChapterTitle: title, Source: src, Confidence: "confirmed"}
	}
	return []seedTextbookMapping{
		m("P4-WHOLE-100000", "4A", "Chapter 1 Lesson 1: Numbers to 100 000"),
		m("P4-WHOLE-100000", "4A", "Chapter 1 Lesson 2: Comparing and Ordering Numbers"),
		m("P4-WHOLE-100000", "4A", "Chapter 1 Lesson 3: Number Patterns"),
		m("P4-WHOLE-100000", "4A", "Chapter 1 Lesson 4: Rounding and Estimation"),
		m("P4-FACTORS-MULTIPLES", "4A", "Chapter 2 Lesson 1: Factors"),
		m("P4-FACTORS-MULTIPLES", "4A", "Chapter 2 Lesson 2: Multiples"),
		m("P4-FOUR-OPERATIONS", "4A", "Chapter 3 Lesson 1: Multiplying by a 1-Digit Number"),
		m("P4-FOUR-OPERATIONS", "4A", "Chapter 3 Lesson 2: Multiplying by a 2-Digit Number"),
		m("P4-FOUR-OPERATIONS", "4A", "Chapter 3 Lesson 3: Dividing by a 1-Digit Number"),
		m("P4-FOUR-OPERATIONS", "4A", "Chapter 4 Lesson 1: Solving Word Problems"),
		m("P4-GEOMETRY-ANGLES", "4A", "Chapter 5 Lesson 1: Understanding and Measuring Angles"),
		m("P4-GEOMETRY-ANGLES", "4A", "Chapter 5 Lesson 2: Drawing Angles to 180°"),
		m("P4-GEOMETRY-ANGLES", "4A", "Chapter 5 Lesson 3: Turns and 8-Point Compass"),
		m("P4-GEOMETRY-ANGLES", "4A", "Chapter 6 Lesson 1: Properties of Squares and Rectangles"),
		m("P4-GEOMETRY-ANGLES", "4A", "Chapter 6 Lesson 2: Drawing Squares and Rectangles"),
		m("P4-GEOMETRY-SYMMETRY", "4A", "Chapter 7 Lesson 1: Symmetric Figures and Lines of Symmetry"),
		m("P4-GEOMETRY-SYMMETRY", "4A", "Chapter 7 Lesson 2: Making Symmetric Figures and Patterns"),
		m("P4-FRACTIONS-MIXED", "4B", "Chapter 8 Practice 1: Mixed Numbers"),
		m("P4-FRACTIONS-MIXED", "4B", "Chapter 8 Practice 2: Improper Fractions"),
		m("P4-FRACTIONS-MIXED", "4B", "Chapter 8 Practice 3: Improper Fractions and Mixed Numbers"),
		m("P4-FRACTIONS-MIXED", "4B", "Chapter 8 Practice 4: Comparing and Ordering Fractions"),
		m("P4-FRACTIONS-MIXED", "4B", "Chapter 8 Practice 5: Fraction of a Set"),
		m("P4-FRACTIONS-MIXED", "4B", "Chapter 9 Practice 1: Adding Fractions"),
		m("P4-FRACTIONS-MIXED", "4B", "Chapter 9 Practice 2: Subtracting Fractions"),
		m("P4-FRACTIONS-MIXED", "4B", "Chapter 9 Practice 3: Word Problems"),
		m("P4-DECIMALS", "4B", "Chapter 10 Practice 1: Understanding Tenths"),
		m("P4-DECIMALS", "4B", "Chapter 10 Practice 2: Understanding Hundredths"),
		m("P4-DECIMALS", "4B", "Chapter 10 Practice 3: Understanding Thousandths"),
		m("P4-DECIMALS", "4B", "Chapter 10 Practice 4: Comparing and Ordering Decimals"),
		m("P4-DECIMALS", "4B", "Chapter 10 Practice 5: Rounding Decimals"),
		m("P4-DECIMALS", "4B", "Chapter 10 Practice 6: Fractions and Decimals"),
		m("P4-DECIMALS", "4B", "Chapter 11 Practice 1: Addition"),
		m("P4-DECIMALS", "4B", "Chapter 11 Practice 2: Subtraction"),
		m("P4-DECIMALS", "4B", "Chapter 11 Practice 3: Multiplication"),
		m("P4-DECIMALS", "4B", "Chapter 11 Practice 4: Division"),
		m("P4-DECIMALS", "4B", "Chapter 12 Practice 1: Solving Word Problems (1)"),
		m("P4-DECIMALS", "4B", "Chapter 12 Practice 2: Solving Word Problems (2)"),
		m("P4-AREA-PERIMETER", "4B", "Chapter 13 Practice 1: Rectangles and Squares"),
		m("P4-AREA-PERIMETER", "4B", "Chapter 13 Practice 2: Composite Figures"),
		m("P4-AREA-PERIMETER", "4B", "Chapter 13 Practice 3: Word Problems"),
		m("P4-STATISTICS", "4B", "Chapter 14 Practice 1: Tables"),
		m("P4-STATISTICS", "4B", "Chapter 14 Practice 2: Line Graphs"),
		m("P4-TIME", "4B", "Chapter 15 Practice 1: Seconds"),
		m("P4-TIME", "4B", "Chapter 15 Practice 2: 24-Hour Clock"),
		m("P5-WHOLE-OPERATIONS", "5A", "Chapter 1 Lesson 1: Numbers to 10 Million"),
		m("P5-WHOLE-OPERATIONS", "5A", "Chapter 2 Lesson 1: Multiplying by 10, 100, 1000 and Their Multiples"),
		m("P5-WHOLE-OPERATIONS", "5A", "Chapter 2 Lesson 2: Dividing by 10, 100, 1000 and Their Multiples"),
		m("P5-WHOLE-OPERATIONS", "5A", "Chapter 2 Lesson 3: Order of Operations"),
		m("P5-WHOLE-OPERATIONS", "5A", "Chapter 2 Lesson 4: Solving Word Problems"),
		m("P5-FRACTIONS", "5A", "Chapter 3 Lesson 1: Fractions and Division"),
		m("P5-FRACTIONS", "5A", "Chapter 3 Lesson 2: Addition of Mixed Numbers"),
		m("P5-FRACTIONS", "5A", "Chapter 3 Lesson 3: Subtraction of Mixed Numbers"),
		m("P5-FRACTIONS", "5A", "Chapter 4 Lesson 1: Product of a Fraction and a Whole Number"),
		m("P5-FRACTIONS", "5A", "Chapter 4 Lesson 2: Product of Two Fractions"),
		m("P5-FRACTIONS", "5A", "Chapter 4 Lesson 3: Product of a Mixed Number and a Whole Number"),
		m("P5-FRACTIONS", "5A", "Chapter 5 Lesson 1: Solving Word Problems"),
		m("P5-AREA-VOLUME", "5A", "Chapter 6 Lesson 1: Area of a Triangle"),
		m("P5-AREA-VOLUME", "5A", "Chapter 6 Lesson 2: Composite Figures"),
		m("P5-RATIO", "5A", "Chapter 7 Lesson 1: Finding Ratios"),
		m("P5-RATIO", "5A", "Chapter 7 Lesson 2: Equivalent Ratios"),
		m("P5-RATIO", "5A", "Chapter 7 Lesson 3: Comparing Three Quantities"),
		m("P5-RATIO", "5A", "Chapter 7 Lesson 4: Solving Word Problems"),
		m("P5-AREA-VOLUME", "5A", "Chapter 8 Lesson 1: Volume of a Solid"),
		m("P5-AREA-VOLUME", "5A", "Chapter 8 Lesson 2: Drawing Cubes and Cuboids"),
		m("P5-AREA-VOLUME", "5A", "Chapter 8 Lesson 3: Volume of a Cube and a Cuboid"),
		m("P5-AREA-VOLUME", "5A", "Chapter 8 Lesson 4: Volume of a Liquid"),
		m("P5-DECIMALS", "5B", "Chapter 9 Lesson 1: Multiplying by 10, 100, 1000 and Their Multiples"),
		m("P5-DECIMALS", "5B", "Chapter 9 Lesson 2: Dividing by 10, 100, 1000 and Their Multiples"),
		m("P5-DECIMALS", "5B", "Chapter 9 Lesson 3: Converting Measurements"),
		m("P5-DECIMALS", "5B", "Chapter 9 Lesson 4: Solving Word Problems"),
		m("P5-PERCENTAGE-RATE", "5B", "Chapter 10 Lesson 1: Percent"),
		m("P5-PERCENTAGE-RATE", "5B", "Chapter 10 Lesson 2: Percentages as Fractions and Decimals"),
		m("P5-PERCENTAGE-RATE", "5B", "Chapter 10 Lesson 3: Decimals and Fractions as Percentages"),
		m("P5-PERCENTAGE-RATE", "5B", "Chapter 10 Lesson 4: Percentage of a Quantity"),
		m("P5-PERCENTAGE-RATE", "5B", "Chapter 10 Lesson 5: Solving Word Problems"),
		m("P5-AVERAGE", "5B", "Chapter 11 Lesson 1: Understanding Average"),
		m("P5-PERCENTAGE-RATE", "5B", "Chapter 12 Lesson 1: Rate"),
		m("P5-PERCENTAGE-RATE", "5B", "Chapter 12 Lesson 2: Solving Word Problems"),
		m("P5-GEOMETRY", "5B", "Chapter 13 Lesson 1: Angles Properties"),
		m("P5-GEOMETRY", "5B", "Chapter 14 Lesson 1: Classifying Triangles"),
		m("P5-GEOMETRY", "5B", "Chapter 14 Lesson 2: Sum of Angles in a Triangle"),
		m("P5-GEOMETRY", "5B", "Chapter 14 Lesson 3: Right-angled, Isosceles and Equilateral Triangles"),
		m("P5-GEOMETRY", "5B", "Chapter 14 Lesson 4: Finding Unknown Angles"),
		m("P5-GEOMETRY", "5B", "Chapter 14 Lesson 5: Drawing Triangles"),
		m("P5-GEOMETRY", "5B", "Chapter 15 Lesson 1: Parallelograms, Rhombuses and Trapeziums"),
		m("P5-GEOMETRY", "5B", "Chapter 15 Lesson 2: Finding Unknown Angles"),
		m("P5-GEOMETRY", "5B", "Chapter 15 Lesson 3: Drawing Four-sided Figures"),
	}
}
