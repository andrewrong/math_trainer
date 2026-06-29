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
}

func (h *CurriculumHandler) ensureSeed(c *gin.Context) error {
	var n int
	if err := h.db.QueryRowContext(c, `SELECT COUNT(*) FROM curriculum_topics`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
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
			INSERT OR IGNORE INTO curriculum_topics
			(topic_code, grade, strand, sub_strand, title, objectives_json, prerequisites_json, related_json, sort_order)
			VALUES(?,?,?,?,?,?,?,?,?)`,
			t.Code, t.Grade, t.Strand, t.SubStrand, t.Title, string(obj), string(pre), string(rel), t.SortOrder); err != nil {
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
	if grade != "P3" && grade != "P4" {
		c.JSON(http.StatusBadRequest, model.APIError{Error: "only P3/P4 artifact generation is supported"})
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

func (h *CurriculumHandler) ensureArtifactsForTopic(c *gin.Context, code string) error {
	var n int
	if err := h.db.QueryRowContext(c, `SELECT COUNT(*) FROM curriculum_artifacts WHERE topic_code=?`, code).Scan(&n); err != nil {
		return err
	}
	if n >= 2 {
		return nil
	}
	t, err := h.topicByCode(c, code)
	if err != nil || (t.Grade != "P3" && t.Grade != "P4") {
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
		{"time|24-hour|duration", "P3-TIME"},
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
	if strings.HasPrefix(code, "P4-") {
		if pack, ok := buildP4TopicLessonPack(code, base); ok {
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
	return base
}

func buildP4TopicLessonPack(code string, base gin.H) (gin.H, bool) {
	switch code {
	case "P4-WHOLE-100000":
		return mergePack(base,
			"Large numbers are not bigger because they look long; they are bigger because of place value.",
			"P4 extends the same ones-tens-hundreds idea to ten-thousands and hundred-thousands. The child should read a number as grouped value, compare from the largest place, and estimate before exact work.",
			"place",
			[]string{"Each place is ten times the place to its right.", "Comparing starts at the largest place because one ten-thousand can outweigh many ones.", "Rounding is choosing the nearest friendly benchmark, not changing every digit.", "Patterns become easier when she asks which place is changing."},
			[]string{"Build the number with place cards: ten-thousands, thousands, hundreds, tens, ones.", "Read the expanded value before saying the compact number.", "Compare from the leftmost different digit.", "For rounding, mark the target place and inspect only the next digit.", "For patterns, say the size of each jump.", "Check whether an estimate is above or below the exact number."},
			[]gin.H{ex("Build 62,705", "62,705 means 6 ten-thousands, 2 thousands, 7 hundreds, 0 tens, and 5 ones. The zero still matters because it holds the tens place."), ex("Compare", "78,430 is greater than 76,999 because the ten-thousands are equal but 8 thousands is greater than 6 thousands."), ex("Round", "43,682 to the nearest thousand is 44,000 because the hundreds digit is 6, so 43 thousand moves to 44 thousand."), ex("Pattern", "18,500, 19,000, 19,500, 20,000 changes by 500 each step; the hundreds place keeps driving the jump.")},
			[]gin.H{mis("Reading digit by digit only", "Ask what each digit is worth: the 6 in 62,705 is 60,000, not 6."), mis("Comparing from the right", "Start at the largest place. Ones cannot beat thousands."), mis("Rounding too many places", "Choose one target place and ignore the rest after checking the next digit."), mis("Dropping zeros", "Zeros keep places open, especially in numbers like 40,508.")},
			[]gin.H{cq("What is the value of 8 in 83,419?", "Name the place first.", []string{"8", "800", "8,000", "80,000"}, "80,000", "The 8 is in the ten-thousands place."), cq("Which is greatest?", "Compare from the left.", []string{"56,904", "56,490", "57,009", "55,999"}, "57,009", "57 thousand is greater than 56 thousand or 55 thousand."), cq("Round 68,451 to the nearest hundred.", "Look at the tens digit.", []string{"68,400", "68,500", "68,000", "69,000"}, "68,500", "The target is hundreds. The tens digit is 5, so round up."), cq("What comes next: 24,750, 25,000, 25,250, ___?", "Find the jump.", []string{"25,300", "25,500", "26,000", "24,500"}, "25,500", "Each step adds 250."), cq("Which expanded form is 70,306?", "Keep the zero thousands and zero tens in mind.", []string{"70,000+300+6", "7,000+300+6", "70,000+3,000+60", "70,000+30+6"}, "70,000+300+6", "There are 7 ten-thousands, 3 hundreds, and 6 ones."), cq("About how much is 49,860 to the nearest thousand?", "Choose the closer thousand.", []string{"49,000", "50,000", "49,800", "60,000"}, "50,000", "It is only 140 away from 50,000.")},
			[]gin.H{cq("A library has 38,742 books. It buys 1,985 more. Which estimate is best before exact addition?", "Round both numbers.", []string{"about 39,000 + 2,000", "about 30,000 + 1,000", "about 38 + 19", "about 40,000 + 20,000"}, "about 39,000 + 2,000", "38,742 is near 39,000 and 1,985 is near 2,000."), cq("Which number is 4,000 less than 72,560?", "Only the thousands value changes.", []string{"68,560", "72,160", "32,560", "76,560"}, "68,560", "72 thousands minus 4 thousands is 68 thousands."), cq("Round 105,499 to the nearest thousand.", "Look at the hundreds digit.", []string{"105,000", "105,500", "106,000", "110,000"}, "105,000", "The hundreds digit is 4, so it stays at 105,000."), cq("Which statement is true?", "Use place-value reasoning.", []string{"90,005 > 90,050", "87,999 > 88,001", "100,000 > 99,999", "45,700 < 45,070"}, "100,000 > 99,999", "100,000 is one more than 99,999.")},
			cq("A school wants to say its fundraiser collected about $80,000. The exact amount was $79,486. Which rounding explains this honestly?", "Connect rounding to real reporting.", []string{"Nearest ten gives $79,490", "Nearest hundred gives $79,500", "Nearest thousand gives $79,000", "Nearest ten-thousand gives $80,000"}, "Nearest ten-thousand gives $80,000", "For a broad public estimate, nearest ten-thousand turns $79,486 into $80,000. The exact amount should still be used for accounting.")), true
	case "P4-FACTORS-MULTIPLES":
		return mergePack(base,
			"Factors are exact building blocks; multiples are the numbers those blocks can build.",
			"This topic should feel like organised multiplication, not a list to memorise. A factor divides with no leftover. A multiple appears when skip-counting by that number.",
			"division",
			[]string{"Factor and multiple are inverse ideas.", "Factor pairs prevent missing factors.", "Common factors show shared building blocks.", "Common multiples show shared landing points."},
			[]string{"Ask if the division is exact.", "List factor pairs in order so none are missed.", "For multiples, skip-count or multiply by 1, 2, 3, and so on.", "For common factors, compare factor lists.", "For common multiples, compare skip-counting lists.", "Explain with multiplication and division, not only a list."},
			[]gin.H{ex("Factor pair", "For 36: 1×36, 2×18, 3×12, 4×9, 6×6. Stop when the pairs meet."), ex("Multiple", "Multiples of 8 are 8, 16, 24, 32, 40 because they are 8×1, 8×2, 8×3, 8×4, 8×5."), ex("Common factor", "Factors of 24 include 1,2,3,4,6,8,12,24. Factors of 36 include 1,2,3,4,6,9,12,18,36. Common factors include 1,2,3,4,6,12."), ex("Common multiple", "Multiples of 6 and 8 first meet at 24, because 6×4=24 and 8×3=24.")},
			[]gin.H{mis("Mixing factor and multiple", "A factor is usually smaller or equal; a multiple is usually larger or equal."), mis("Stopping factor pairs too early", "Keep testing until the pair reverses or meets."), mis("Counting a non-exact divisor", "If there is a remainder, it is not a factor."), mis("Only memorising tables", "Use division checks when memory is unsure.")},
			[]gin.H{cq("Which is a factor of 48?", "It must divide exactly.", []string{"5", "7", "8", "10"}, "8", "48 ÷ 8 = 6 with no remainder."), cq("Which is a multiple of 9?", "Use 9 times table.", []string{"27", "32", "45? no", "50"}, "27", "9 × 3 = 27."), cq("Which pair makes a factor pair of 56?", "Multiply to 56.", []string{"6 and 8", "7 and 8", "5 and 11", "9 and 6"}, "7 and 8", "7 × 8 = 56."), cq("A number is divisible by 6. What must be true?", "Use exact division.", []string{"It is a multiple of 6", "It must be odd", "It is smaller than 6", "It has no factors"}, "It is a multiple of 6", "If 6 divides it exactly, it appears in the 6 times table."), cq("Common factor of 18 and 30?", "Check both numbers.", []string{"4", "5", "6", "9"}, "6", "18 ÷ 6 = 3 and 30 ÷ 6 = 5."), cq("First common multiple of 4 and 10?", "List landing points.", []string{"10", "14", "20", "40"}, "20", "4,8,12,16,20 and 10,20 meet at 20.")},
			[]gin.H{cq("Which number has exactly these factor pairs: 1×24, 2×12, 3×8, 4×6?", "Rebuild from pairs.", []string{"12", "24", "36", "48"}, "24", "Every pair multiplies to 24."), cq("Is 7 a factor of 91?", "Use division or multiplication.", []string{"Yes, because 7×13=91", "Yes, because 91 is odd", "No, because 7 is prime", "No, because 91 ends in 1"}, "Yes, because 7×13=91", "A factor can be checked by multiplication."), cq("Which number is both a factor of 60 and a multiple of 5?", "Both conditions must be true.", []string{"8", "10", "12", "18"}, "10", "60 ÷ 10 = 6 and 10 is 5×2."), cq("Two bells ring every 6 min and 8 min. They ring together now. When together again?", "This is common multiple thinking.", []string{"12 min", "14 min", "24 min", "48 min"}, "24 min", "The first common multiple of 6 and 8 is 24.")},
			cq("A coach wants equal teams with no players left over. There are 36 players. Which team sizes work and why?", "Use factors in a real constraint.", []string{"5 only", "4, 6, or 9", "7 or 8", "Any number smaller than 36"}, "4, 6, or 9", "36 can be divided exactly by 4, 6, and 9, so those team sizes make equal teams with no leftover.")), true
	case "P4-FOUR-OPERATIONS":
		return mergePack(base,
			"Written algorithms are compressed place-value reasoning, not magic steps.",
			"P4 multiplication and division should connect back to equal groups and place value. The written method is useful only when the child can explain what each carried digit, partial product, quotient digit, and remainder means.",
			"place",
			[]string{"Multi-digit multiplication splits a number into place-value parts.", "Long division shares or groups one place at a time.", "Estimation tells whether the answer size is sensible.", "Inverse operations prove the algorithm rebuilt the original number."},
			[]string{"Estimate the answer first.", "For multiplication, split into partial products by place value.", "Record regrouping as value, not as a random carried digit.", "For division, divide, multiply, subtract, then bring down.", "Check quotient placement: hundreds, tens, ones.", "Verify with multiplication or divisor × quotient + remainder."},
			[]gin.H{ex("1-digit multiplication", "3,426 × 4 = 3,000×4 + 400×4 + 20×4 + 6×4. The column algorithm is this idea written shorter."), ex("2-digit multiplication", "236 × 24 = 236×20 + 236×4. The 20 row must be tens, not another ones row."), ex("Long division", "846 ÷ 3: 8 hundreds give 2 hundreds, 24 tens give 8 tens, 6 ones give 2 ones, so 282."), ex("Check", "728 ÷ 4 = 182 because 182 × 4 = 728.")},
			[]gin.H{mis("Forgetting the zero or place shift in ×2-digit", "The tens row means ×20, so it must sit one place left."), mis("Writing a quotient digit in the wrong place", "Say whether you are dividing hundreds, tens, or ones."), mis("Skipping estimate", "Without estimate, 3218×6 can accidentally become 1,930 and still look tidy."), mis("Not checking division", "Always rebuild: divisor × quotient + remainder.")},
			[]gin.H{cq("236 × 4 = ?", "Use place-value multiplication.", []string{"824", "944", "1,244", "914"}, "944", "4×6=24, carry 2 tens. 4×3 tens +2 =14 tens. 4×2 hundreds +1 =9 hundreds."), cq("36 × 24 means...", "Break 24 into tens and ones.", []string{"36×2 + 36×4", "36×20 + 36×4", "36+24", "36÷24"}, "36×20 + 36×4", "24 is 20 + 4, so multiply both parts."), cq("846 ÷ 3 = ?", "Divide by place.", []string{"262", "282", "292", "288"}, "282", "8 hundreds, 24 tens, and 6 ones divide evenly by 3."), cq("Estimate 3,218 × 6.", "Round first.", []string{"about 1,800", "about 18,000", "about 19,000", "about 190,000"}, "about 19,000", "3,218 is near 3,200 and 3,200×6=19,200."), cq("Check 728 ÷ 4 = 182.", "Use inverse.", []string{"182×4=728", "728×4=2912", "182+4=186", "728-182=546"}, "182×4=728", "Division is checked by multiplication."), cq("In 156 × 23, why is 156 × 20 useful?", "Understand the tens digit.", []string{"Because 2 means 20 in 23", "Because 20 is always smaller", "Because addition is easier", "Because we ignore 3"}, "Because 2 means 20 in 23", "The digit 2 is in the tens place.")},
			[]gin.H{cq("408 × 7 = ?", "Regroup through zeros carefully.", []string{"2,056", "2,856", "2,806", "2,8060"}, "2,856", "7×8=56, 7×0 tens plus 5 tens =5 tens, 7×4 hundreds=28 hundreds."), cq("742 ÷ 6 = ?", "There will be a remainder.", []string{"123 r 4", "124 r 2", "122 r 10", "132 r 4"}, "123 r 4", "6×123=738 and 742-738=4."), cq("24 boxes hold 36 pencils each. Total pencils?", "Use partial products.", []string{"60", "144", "864", "964"}, "864", "36×20=720 and 36×4=144; total 864."), cq("A wrong answer to 2,305×4 is 8,020. What likely happened?", "Diagnose the algorithm.", []string{"They ignored regrouping from ones", "They rounded correctly", "They divided instead", "They used ×40"}, "They ignored regrouping from ones", "5×4=20 gives 2 tens to carry. Missing regrouping changes the tens/hundreds.")},
			cq("A child says 918 ÷ 3 = 36 because 9÷3=3 and 18÷3=6. What is the best correction?", "Use place value, not just digits.", []string{"It is fine because 36 sounds close", "The 9 is hundreds, so the answer starts with 300, not 30", "Division cannot use place value", "Always use a calculator"}, "The 9 is hundreds, so the answer starts with 300, not 30", "918 has 9 hundreds. 9 hundreds ÷3 = 3 hundreds, so the quotient is 306, not 36.")), true
	case "P4-FRACTIONS-MIXED":
		return mergePack(base,
			"A fraction can name part of one whole, part of a set, or more than one whole.",
			"P4 fractions move beyond shaded pizza slices. The child needs to see mixed numbers, improper fractions, and fraction-of-a-set as the same equal-part idea used in different situations.",
			"fraction",
			[]string{"A denominator names the size of equal parts.", "An improper fraction is not wrong; it is a count of parts bigger than one whole.", "A mixed number separates wholes from leftover parts.", "Fraction of a set means split the objects into equal groups, then take the required number of groups."},
			[]string{"Identify the whole or the set first.", "Check that parts or groups are equal.", "For mixed to improper, convert wholes into fractional parts.", "For improper to mixed, make as many wholes as possible.", "For fraction of a set, divide by the denominator then multiply by the numerator.", "Use a diagram or number line before symbolic work when unsure."},
			[]gin.H{ex("Improper to mixed", "11/4 means 11 fourths. 8 fourths make 2 wholes, with 3 fourths left, so 11/4 = 2 3/4."), ex("Mixed to improper", "3 2/5 means 3 wholes and 2 fifths. 3 wholes = 15 fifths, plus 2 fifths = 17/5."), ex("Fraction of a set", "3/4 of 20 means split 20 into 4 equal groups of 5, then take 3 groups: 15."), ex("Related denominators", "1/2 + 1/4 works by turning 1/2 into 2/4, then 2/4 + 1/4 = 3/4.")},
			[]gin.H{mis("Calling improper fractions wrong", "Improper fractions are valid; they simply describe more than one whole."), mis("Adding denominators", "Denominators name part size. Same-sized parts keep the same denominator."), mis("Ignoring the whole", "A fraction of a set and a fraction of one shape use the same notation but different wholes."), mis("Comparing only numerators", "Bigger numerator is not enough when denominators differ.")},
			[]gin.H{cq("9/4 is...", "Make wholes from fourths.", []string{"1 1/4", "2 1/4", "2 3/4", "9 1/4"}, "2 1/4", "8 fourths make 2 wholes, with 1 fourth left."), cq("2 3/5 = ?", "Convert wholes to fifths.", []string{"5/5", "10/5", "13/5", "23/5"}, "13/5", "2 wholes = 10 fifths. Add 3 fifths."), cq("1/3 of 18 = ?", "Divide by denominator.", []string{"3", "6", "9", "15"}, "6", "18 split into 3 equal groups gives 6 in each group."), cq("3/4 of 28 = ?", "Divide then multiply.", []string{"7", "14", "21", "24"}, "21", "28 ÷ 4 = 7; 3 groups of 7 is 21."), cq("Which is greater?", "Use equivalent fractions.", []string{"2/3", "3/6", "They are equal", "Cannot tell"}, "2/3", "3/6 = 1/2, and 2/3 is greater than 1/2."), cq("1/6 + 1/3 = ?", "Use sixths.", []string{"2/9", "1/2", "2/6", "1/9"}, "1/2", "1/3 = 2/6. 1/6 + 2/6 = 3/6 = 1/2.")},
			[]gin.H{cq("A class has 32 pupils. 3/8 are in choir. How many?", "Fraction of a set.", []string{"8", "12", "20", "24"}, "12", "32 ÷ 8 = 4; 3 groups of 4 is 12."), cq("Which is equal to 2 1/3?", "Use thirds.", []string{"5/3", "7/3", "8/3", "9/3"}, "7/3", "2 wholes = 6 thirds; plus 1 third = 7 thirds."), cq("5/6 - 1/3 = ?", "Convert 1/3 to sixths.", []string{"4/3", "1/2", "3/6", "4/6"}, "1/2", "1/3 = 2/6, so 5/6 - 2/6 = 3/6 = 1/2."), cq("Which story matches 2/5 of 30?", "Use set meaning.", []string{"30 split into 5 groups, take 2 groups", "30 plus 2 plus 5", "2 split into 30 groups", "5 groups of 30 twice"}, "30 split into 5 groups, take 2 groups", "The denominator tells how many equal groups; numerator tells how many to take.")},
			cq("A recipe uses 1 3/4 cups of flour. Maya doubles it. What is the best first move?", "Choose a representation that makes doubling clear.", []string{"Double only the 3", "Turn 1 3/4 into 7/4 or double wholes and parts carefully", "Add denominators", "Round to 1 cup"}, "Turn 1 3/4 into 7/4 or double wholes and parts carefully", "1 3/4 can be doubled as 7/4 × 2 = 14/4 = 3 1/2, or 1+1 and 3/4+3/4.")), true
	case "P4-DECIMALS":
		return mergePack(base,
			"Decimals are fractions written in the place-value system.",
			"P4 decimals should connect tenths, hundredths, and thousandths to both place value and fractions. The key is not the decimal point itself, but what each place means.",
			"decimal",
			[]string{"Tenths, hundredths, and thousandths continue the base-ten pattern after ones.", "Adding zeros after a decimal can keep the value the same.", "Decimal comparison works left to right by place value.", "Decimal operations need aligned decimal points because places must match."},
			[]string{"Read the decimal by place value.", "Rename with zeros when helpful, such as 0.6 = 0.60.", "Compare from ones, then tenths, then hundredths, then thousandths.", "Line up decimal points for addition and subtraction.", "Estimate before exact calculation.", "Connect simple decimals to fractions: 0.5=1/2, 0.25=1/4, 0.75=3/4."},
			[]gin.H{ex("Place value", "4.307 means 4 ones, 3 tenths, 0 hundredths, and 7 thousandths."), ex("Compare", "0.9 > 0.12 because 0.9 = 0.90, and 90 hundredths is greater than 12 hundredths."), ex("Add", "3.6 + 1.27 = 3.60 + 1.27 = 4.87."), ex("Fraction link", "0.25 = 25 hundredths = 25/100 = 1/4.")},
			[]gin.H{mis("More decimal digits means bigger", "0.9 is bigger than 0.123 because tenths are more powerful than hundredths and thousandths."), mis("Not aligning decimal points", "Digits must stay in their place-value columns."), mis("Reading 0.04 as four tenths", "0.04 is four hundredths; 0.4 is four tenths."), mis("Forgetting estimate", "An answer like 36.2 for 3.6+1.27 should feel too large.")},
			[]gin.H{cq("What is the value of 7 in 2.375?", "Name the decimal place.", []string{"7 ones", "7 tenths", "7 hundredths", "7 thousandths"}, "7 hundredths", "After the point: tenths, hundredths, thousandths."), cq("Which is greatest?", "Use equal decimal places.", []string{"0.8", "0.75", "0.708", "0.079"}, "0.8", "0.8 = 0.800, the greatest."), cq("0.6 + 0.25 = ?", "Rename 0.6.", []string{"0.31", "0.65", "0.85", "0.925"}, "0.85", "0.60+0.25=0.85."), cq("4.5 - 1.28 = ?", "Line up decimal points.", []string{"3.22", "3.32", "3.28", "2.22"}, "3.22", "4.50-1.28=3.22."), cq("0.25 equals...", "Think hundredths.", []string{"1/2", "1/4", "3/4", "25/10"}, "1/4", "25/100 simplifies to 1/4."), cq("Round 6.47 to the nearest tenth.", "Look at hundredths.", []string{"6.4", "6.5", "6.47", "7.0"}, "6.5", "The hundredths digit is 7, so 6.4 rounds up to 6.5.")},
			[]gin.H{cq("Which is smaller: 3.09 or 3.1?", "Rename 3.1 as 3.10.", []string{"3.09", "3.1", "They are equal", "Cannot tell"}, "3.09", "3.09 is 3 and 9 hundredths; 3.10 is 3 and 10 hundredths."), cq("A ribbon is 2.35 m. Another is 1.8 m. Total?", "Line up decimal points.", []string{"3.15 m", "4.15 m", "4.13 m", "20.15 m"}, "4.15 m", "2.35 + 1.80 = 4.15."), cq("6 ÷ 10 = ?", "Dividing by 10 moves one place right into tenths.", []string{"60", "0.6", "0.06", "6.10"}, "0.6", "6 ones become 6 tenths."), cq("Which estimate fits 9.8 - 4.95?", "Use friendly numbers.", []string{"about 1", "about 5", "about 10", "about 15"}, "about 5", "9.8 is about 10 and 4.95 is about 5.")},
			cq("A child says 0.305 is greater than 0.35 because 305 is greater than 35. What should you say?", "Correct using place value.", []string{"Correct, longer number wins", "Compare as 0.305 and 0.350, so 0.35 is greater", "Remove the decimal point", "Both are equal"}, "Compare as 0.305 and 0.350, so 0.35 is greater", "Adding a zero shows 0.350 has 350 thousandths, more than 305 thousandths.")), true
	case "P4-AREA-PERIMETER":
		return mergePack(base,
			"Area and perimeter answer different questions about the same shape.",
			"P4 area/perimeter is not just formulas. The child needs to decide whether the story asks for covering the inside, walking around the outside, or finding a missing side.",
			"area",
			[]string{"Area counts square units inside a figure.", "Perimeter counts length units around the outside boundary.", "Composite figures can be split into rectangles for area.", "Missing sides can often be found from equal opposite lengths or from total perimeter."},
			[]string{"Underline whether the question asks inside or outside.", "Draw the shape and label known lengths.", "For area, split into rectangles or subtract a missing rectangle.", "For perimeter, trace only the outside boundary.", "Check units: area uses square units, perimeter uses length units.", "For missing dimensions, write the equation before calculating."},
			[]gin.H{ex("Area of rectangle", "A 9 cm by 4 cm rectangle covers 9×4 = 36 cm²."), ex("Perimeter of rectangle", "The same rectangle has perimeter 9+4+9+4 = 26 cm."), ex("Composite area", "A 10×6 rectangle with a 3×2 corner removed has area 60-6=54 cm²."), ex("Missing side", "If a rectangle area is 48 cm² and one side is 6 cm, the other side is 48÷6=8 cm.")},
			[]gin.H{mis("Using cm² for perimeter", "Perimeter is distance, so use cm or m."), mis("Adding internal split lines to perimeter", "Only the outside border counts."), mis("Using area formula for every question", "First decide what is being measured."), mis("Forgetting missing sides", "Composite perimeter often requires finding unlabeled outside sides.")},
			[]gin.H{cq("A rectangle is 8 cm by 3 cm. Area?", "Inside squares.", []string{"11 cm²", "22 cm²", "24 cm²", "24 cm"}, "24 cm²", "Area = 8×3 = 24 cm²."), cq("A square side is 5 cm. Perimeter?", "Four equal sides.", []string{"10 cm", "20 cm", "25 cm²", "25 cm"}, "20 cm", "5+5+5+5=20 cm."), cq("Which question asks for area?", "Think covering.", []string{"Fence around a garden", "Ribbon around a card", "Tiles covering a floor", "Distance around a field"}, "Tiles covering a floor", "Tiles cover the inside surface."), cq("A rectangle has area 42 cm² and length 7 cm. Width?", "Undo multiplication.", []string{"6 cm", "7 cm", "35 cm", "49 cm"}, "6 cm", "42÷7=6."), cq("A 6 cm by 4 cm rectangle has perimeter...", "Add outside sides.", []string{"10 cm", "20 cm", "24 cm", "24 cm²"}, "20 cm", "6+4+6+4=20 cm."), cq("Composite area often starts by...", "Choose a model.", []string{"Guessing", "Splitting into rectangles", "Adding every number", "Ignoring units"}, "Splitting into rectangles", "Rectangles make area easier to calculate.")},
			[]gin.H{cq("A 12×8 rectangle has a 4×3 piece removed. Area left?", "Subtract the cut-out.", []string{"60 cm²", "84 cm²", "96 cm²", "108 cm²"}, "84 cm²", "12×8=96; 4×3=12; 96-12=84."), cq("A rectangle perimeter is 30 cm. One side is 9 cm. Other side?", "Use 2L+2W.", []string{"3 cm", "6 cm", "12 cm", "21 cm"}, "6 cm", "30-18=12 for two widths, so each width is 6."), cq("Which unit fits area?", "Area is square units.", []string{"cm", "cm²", "kg", "degrees"}, "cm²", "Area counts square centimetres."), cq("A garden needs fencing but not flooring. Find...", "Use story meaning.", []string{"area", "perimeter", "volume", "average"}, "perimeter", "Fencing goes around the outside.")},
			cq("A composite figure can be split two different ways and both give the same area. What does that show?", "Reason about structure.", []string{"One method must be fake", "Area is conserved when the same region is split differently", "Perimeter and area are equal", "Units do not matter"}, "Area is conserved when the same region is split differently", "Splitting changes the calculation path, not the amount of space covered.")), true
	case "P4-GEOMETRY-ANGLES":
		return mergePack(base,
			"Angles measure turns, and shape properties help us reason without guessing from a drawing.",
			"P4 angles should connect three ideas: turn, measure in degrees, and properties of rectangles/squares. The picture may not be exact, so rules matter.",
			"generic",
			[]string{"A full turn is 360°, a half turn is 180°, and a right angle is 90°.", "Measuring an angle means measuring the turn between two arms.", "Rectangles and squares have four right angles.", "Drawing angles requires lining up the protractor baseline and reading the correct scale."},
			[]string{"Decide if the angle is less than, equal to, or greater than 90° first.", "Place the protractor centre on the vertex.", "Line one arm with 0°.", "Read the scale that starts from that 0°.", "Use shape properties before trusting the look of the drawing.", "For rectangles/squares, use right angles and equal/opposite sides."},
			[]gin.H{ex("Right angle", "A corner of a rectangle is 90°."), ex("Straight angle", "A straight line makes 180°."), ex("Measure", "If one arm starts at 0° and the other reaches 65°, the angle is 65°."), ex("Draw", "To draw 120°, draw a base ray, mark 120° on the correct protractor scale, then connect the vertex to the mark.")},
			[]gin.H{mis("Reading the wrong protractor scale", "Start from the 0° line your base ray uses."), mis("Judging only by appearance", "Sketches may not be exact; use given values and properties."), mis("Confusing angle size with arm length", "Longer arms do not mean a bigger angle."), mis("Forgetting square properties", "A square has 4 equal sides and 4 right angles.")},
			[]gin.H{cq("A right angle is...", "Know the benchmark.", []string{"45°", "90°", "180°", "360°"}, "90°", "A right angle is one quarter turn."), cq("An angle smaller than 90° is...", "Compare to right angle.", []string{"acute", "right", "obtuse", "straight"}, "acute", "Acute angles are less than 90°."), cq("A straight angle is...", "Half turn.", []string{"90°", "120°", "180°", "360°"}, "180°", "A straight line is a half turn."), cq("A rectangle has...", "Use properties.", []string{"4 right angles", "3 equal sides", "no parallel sides", "curved sides"}, "4 right angles", "Every rectangle has four right angles."), cq("What matters when measuring an angle?", "Angle is turn.", []string{"Length of arms", "Turn between arms", "Colour of line", "Page number"}, "Turn between arms", "Angle size measures the opening/turn."), cq("A square is also a...", "Classify by properties.", []string{"rectangle", "circle", "triangle", "line graph"}, "rectangle", "A square has all rectangle properties plus equal sides.")},
			[]gin.H{cq("Two right turns make...", "90° + 90°.", []string{"90°", "120°", "180°", "360°"}, "180°", "Two right angles form a straight angle."), cq("Which could be an obtuse angle?", "Greater than 90° but less than 180°.", []string{"35°", "90°", "125°", "200°"}, "125°", "125° is between 90° and 180°."), cq("If a rectangle has one side 8 cm and another 5 cm, what is true?", "Use opposite sides.", []string{"All sides are 8 cm", "Opposite sides match", "No angle is 90°", "It has 3 corners"}, "Opposite sides match", "Rectangles have opposite sides equal."), cq("When drawing 70°, what should you do first?", "Use tools correctly.", []string{"Draw any curve", "Draw a base ray and place the protractor centre on the vertex", "Guess the opening", "Measure the longest side"}, "Draw a base ray and place the protractor centre on the vertex", "The baseline and vertex anchor the angle.")},
			cq("A child measures an acute-looking angle as 130°. What should she check first?", "Diagnose protractor error.", []string{"Whether she used the wrong scale", "Whether the pencil is sharp", "Whether 130 is always acute", "Whether the arms are long"}, "Whether she used the wrong scale", "If the angle looks acute but reads 130°, she likely read the wrong protractor scale.")), true
	case "P4-GEOMETRY-SYMMETRY":
		return mergePack(base,
			"Line symmetry means one half can fold exactly onto the other.",
			"P4 symmetry is visual but still logical: the line is a mirror, every point needs a matching point the same distance away on the other side.",
			"generic",
			[]string{"A line of symmetry divides a figure into two matching halves.", "Matching points are the same distance from the mirror line.", "A shape can have zero, one, or many lines of symmetry.", "Completing a symmetric figure is copying with distance and direction reversed."},
			[]string{"Imagine folding along the proposed line.", "Check whether both halves match exactly.", "For grid symmetry, count squares from the mirror line.", "Place each matching point the same distance on the other side.", "Connect the copied points carefully.", "Check corners, not just the outline size."},
			[]gin.H{ex("Vertical symmetry", "A heart-like shape may fold left to right if both sides match exactly."), ex("Square", "A square has 4 lines of symmetry: vertical, horizontal, and two diagonals."), ex("Rectangle", "A non-square rectangle has 2 lines of symmetry: vertical and horizontal."), ex("Grid completion", "If a point is 3 squares left of the mirror line, its partner is 3 squares right.")},
			[]gin.H{mis("Almost matching is accepted", "Symmetry requires exact matching, not just similar-looking halves."), mis("Only checking area", "Both halves can have equal area but different shapes."), mis("Copying in the same direction", "A mirror reverses direction across the line."), mis("Ignoring distance from the mirror line", "Equal distance is the core rule.")},
			[]gin.H{cq("A line of symmetry means...", "Think folding.", []string{"two matching halves", "a longer side", "a right angle", "a graph scale"}, "two matching halves", "The halves match exactly when folded."), cq("How many symmetry lines does a square have?", "Vertical, horizontal, diagonals.", []string{"1", "2", "4", "8"}, "4", "A square has four lines of symmetry."), cq("A non-square rectangle has...", "Use shape properties.", []string{"0 lines", "1 line", "2 lines", "4 lines"}, "2 lines", "Vertical and horizontal lines through the centre work."), cq("On a grid, a point 2 squares left of the mirror line maps to...", "Same distance other side.", []string{"2 squares right", "1 square right", "2 squares left", "anywhere"}, "2 squares right", "Mirror points keep equal distance."), cq("Which shape usually has no line symmetry?", "Think fold test.", []string{"Square", "Circle", "Scalene triangle", "Rectangle"}, "Scalene triangle", "A scalene triangle has no equal sides to create a matching fold."), cq("To test a possible symmetry line, first...", "Use the fold idea.", []string{"Count colours only", "Imagine folding on that line", "Add all sides", "Find the biggest angle"}, "Imagine folding on that line", "Folding reveals whether halves match.")},
			[]gin.H{cq("A figure has equal area on both sides of a line. Is it definitely symmetric?", "Equal area is not enough.", []string{"Yes always", "No, shapes must match exactly", "Only if it is blue", "Only for squares"}, "No, shapes must match exactly", "Symmetry requires exact matching, not only equal area."), cq("A regular pentagon has how many lines of symmetry?", "One through each vertex and opposite side midpoint.", []string{"0", "1", "5", "10"}, "5", "Each vertex can anchor one symmetry line."), cq("If a letter B has a horizontal line of symmetry, what does that mean?", "Fold top to bottom.", []string{"Top and bottom match", "Left and right match", "It has no curves", "It is a square"}, "Top and bottom match", "Horizontal symmetry folds top onto bottom."), cq("When completing a symmetric drawing, why count grid squares?", "Accuracy.", []string{"To make matching distances", "To find volume", "To choose colour", "To make it faster only"}, "To make matching distances", "Counting keeps each reflected point equally far from the mirror line.")},
			cq("A child completes only the outline but puts an inner dot in the wrong place. Is the figure symmetric?", "All features must reflect.", []string{"Yes, outline is enough", "No, every marked point must match", "Only if the dot is small", "No shape can have dots"}, "No, every marked point must match", "Symmetry applies to the whole figure, including internal details.")), true
	case "P4-GEOMETRY-NETS":
		return mergePack(base,
			"A net is a flat plan that folds into a solid.",
			"P4 nets require spatial reasoning: faces, edges, and how flat shapes become a 3D object. The goal is to mentally fold, not memorise one pattern.",
			"generic",
			[]string{"A solid has faces, edges, and vertices.", "A net must have the right number and shapes of faces.", "Faces connected in a net must fold without overlapping.", "Different nets can fold into the same solid."},
			[]string{"Name the solid first.", "List its faces and face shapes.", "Count whether the net has the correct faces.", "Choose one face as the base.", "Mentally fold neighbouring faces up.", "Check for overlap or missing opposite faces."},
			[]gin.H{ex("Cube", "A cube has 6 square faces. A cube net must have 6 squares connected so they can fold around one square."), ex("Cuboid", "A cuboid has 6 rectangular faces in 3 matching pairs."), ex("Cylinder", "A cylinder has 2 circular faces and 1 curved surface; a simple flat net shows two circles and one rectangle."), ex("Pyramid", "A square pyramid has 1 square base and 4 triangular faces.")},
			[]gin.H{mis("Counting faces only", "Correct number of faces is necessary but not enough; arrangement must fold without overlap."), mis("Confusing 2D drawing with 3D solid", "A drawing shows the solid; a net is meant to fold into it."), mis("Using wrong face shapes", "A cube needs squares; a cuboid usually needs rectangles."), mis("Forgetting opposite faces", "A closed solid needs all faces to cover every side.")},
			[]gin.H{cq("A cube net must have...", "Count faces.", []string{"4 squares", "5 squares", "6 squares", "8 squares"}, "6 squares", "A cube has six square faces."), cq("A square pyramid has...", "Base plus side faces.", []string{"1 square and 4 triangles", "6 squares", "2 circles and 1 rectangle", "4 rectangles only"}, "1 square and 4 triangles", "The square is the base; triangles fold up to meet."), cq("A cylinder net can show...", "Unroll the curved surface.", []string{"2 circles and 1 rectangle", "6 squares", "4 triangles", "1 circle only"}, "2 circles and 1 rectangle", "The rectangle wraps around; circles close top and bottom."), cq("A cuboid has how many faces?", "Like a rectangular box.", []string{"4", "5", "6", "8"}, "6", "A cuboid has six rectangular faces."), cq("If a net has overlapping faces when folded, it is...", "Folding test.", []string{"valid", "not valid", "always a cube", "a line graph"}, "not valid", "Faces must cover the solid without overlapping."), cq("The best way to test a net is to...", "Use spatial reasoning.", []string{"Count page numbers", "Mentally fold around a base face", "Add all angles", "Compare decimals"}, "Mentally fold around a base face", "Folding checks whether the flat plan becomes a solid.")},
			[]gin.H{cq("Which net could be a cube?", "Use face count and arrangement.", []string{"Six connected squares with no impossible overlap", "Five squares", "Six circles", "One rectangle"}, "Six connected squares with no impossible overlap", "A cube needs six square faces arranged to fold."), cq("A cuboid has dimensions 2×3×5. Its face pairs are...", "Opposite faces match.", []string{"2×3, 2×5, 3×5", "2×2 only", "3 circles", "5 triangles"}, "2×3, 2×5, 3×5", "Each pair of dimensions makes one pair of opposite rectangular faces."), cq("A triangular prism includes...", "Think prism faces.", []string{"2 triangles and 3 rectangles", "6 squares", "1 square and 4 triangles", "2 circles"}, "2 triangles and 3 rectangles", "The triangular ends and rectangular side faces form a triangular prism."), cq("Why can one solid have several different nets?", "Same faces, different layout.", []string{"Because faces can be arranged in different foldable ways", "Because solids change shape", "Because face count does not matter", "Because nets are random"}, "Because faces can be arranged in different foldable ways", "The same faces can be connected in more than one valid flat plan.")},
			cq("A net has 6 squares, but four squares all fold onto the same side. What is the issue?", "Face count alone fails.", []string{"It has too many squares", "The arrangement causes overlap", "It is a perfect cube", "It needs circles"}, "The arrangement causes overlap", "A valid net needs both correct faces and a foldable arrangement.")), true
	case "P4-STATISTICS":
		return mergePack(base,
			"Data displays are arguments: read the scale before trusting the picture.",
			"P4 statistics should teach her to ask what the data is showing, how it is scaled, and what comparison or trend the question needs.",
			"generic",
			[]string{"Tables organise exact values.", "Line graphs show change over time or ordered points.", "Pie charts show parts of a whole.", "A scale can make changes look bigger or smaller than they are."},
			[]string{"Read the title to know the context.", "Read labels and units.", "Check the scale before reading values.", "Find exact values before comparing.", "For line graphs, describe increase/decrease/same.", "For pie charts, connect sectors to fractions of a whole."},
			[]gin.H{ex("Table", "A table can show exact values such as Monday 24, Tuesday 31, Wednesday 28."), ex("Line graph", "If points go from 12 to 20, the value increased by 8."), ex("Pie chart", "A half circle means 1/2 or 50% of the total."), ex("Scale", "If one grid step is 5 pupils, a bar at 6 steps represents 30 pupils.")},
			[]gin.H{mis("Ignoring scale", "One square may mean 2, 5, 10, or more."), mis("Comparing by picture size only", "Read exact values before deciding."), mis("Forgetting units", "A number could mean pupils, dollars, books, or minutes."), mis("Treating pie chart slices as counts without total", "A slice shows a fraction; count needs the total.")},
			[]gin.H{cq("A graph scale is 1 step = 5. A bar reaches 6 steps. Value?", "Multiply scale by steps.", []string{"6", "11", "30", "60"}, "30", "6×5=30."), cq("Which should you read first?", "Avoid guessing.", []string{"Title, labels, and scale", "Only colours", "Only largest number", "Only last bar"}, "Title, labels, and scale", "They tell what the data means."), cq("A line graph from 12 to 20 shows...", "Find change.", []string{"decrease by 8", "increase by 8", "same", "increase by 32"}, "increase by 8", "20-12=8."), cq("A pie chart half means...", "Part of whole.", []string{"1/4", "1/3", "1/2", "whole"}, "1/2", "Half the circle is half the total."), cq("Class A has 28 books, B has 19. How many more?", "Comparison means subtract.", []string{"7", "9", "37", "47"}, "9", "28-19=9."), cq("A table is best for...", "Think exact values.", []string{"Showing exact rows of data", "Playing audio", "Measuring angles", "Folding nets"}, "Showing exact rows of data", "Tables keep values organised and exact.")},
			[]gin.H{cq("Scale is 1 square = 4 pupils. A bar is halfway between 6 and 7 squares. Value?", "Use half step.", []string{"24", "26", "28", "30"}, "26", "6.5×4=26."), cq("A pie chart sector is 1/4 of 80 pupils. How many?", "Fraction of total.", []string{"10", "20", "40", "60"}, "20", "80÷4=20."), cq("A line graph goes 15, 15, 18, 12. Which statement is best?", "Describe trend carefully.", []string{"Always increasing", "Same, then increases, then decreases", "Always decreasing", "No data"}, "Same, then increases, then decreases", "15 to 15 stays same, then 18 increases, then 12 decreases."), cq("Why might two graphs with the same data look different?", "Think scale.", []string{"Different scales", "Data changes automatically", "Titles do not matter", "Graphs cannot show same data"}, "Different scales", "A compressed or stretched scale changes the visual impression.")},
			cq("A poster says sales doubled because the line looks twice as steep, but the vertical scale starts at 90 not 0. What should you check?", "Critical reading of data.", []string{"Exact values and scale", "Colour of the line", "Font size", "Whether it is pretty"}, "Exact values and scale", "Graphs can exaggerate. Exact values and scale tell the real change.")), true
	default:
		return nil, false
	}
}

func buildTextbookLessonCards(t model.CurriculumTopic) []gin.H {
	cards := []gin.H{}
	for _, m := range t.Mappings {
		cards = append(cards, gin.H{
			"book":            m.Book,
			"chapterTitle":    m.ChapterTitle,
			"lessonObjective": lessonObjective(m.ChapterTitle, t.Title),
			"visualKind":      visualKindForTopic(t.TopicCode),
			"workedExamples":  lessonExamples(t.TopicCode, m.ChapterTitle),
			"practiceQuestions": []gin.H{
				cq("Which model helps most for this lesson?", "Choose the thinking tool.", []string{"A random guess", "A diagram or place-value model", "Only the final answer", "Skipping the unit"}, "A diagram or place-value model", "A model shows what the numbers mean."),
				cq("What should the answer include?", "Think like a teacher checking reasoning.", []string{"Only the number", "Number, unit, and reason", "Only a drawing", "Only speed"}, "Number, unit, and reason", "The unit and reason show understanding."),
				cq("If the answer looks unreasonable, what should you check first?", "Find the likely source.", []string{"The title", "Place value/unit/model", "The font", "The page number"}, "Place value/unit/model", "Most mistakes come from mismatched place value, units, or model choice."),
				cq("How do you prove you understand?", "Go beyond calculation.", []string{"Say why the method works", "Say it is easy", "Hide the working", "Copy the example"}, "Say why the method works", "Explaining the method is evidence of concept understanding."),
			},
			"explainPrompt": "Explain your thinking in one full sentence: what model did you use, and why?",
		})
	}
	return cards
}

func lessonObjective(title, topic string) string {
	s := strings.TrimSpace(title)
	if i := strings.Index(s, ": "); i >= 0 {
		s = s[i+2:]
	}
	if i := strings.Index(s, " (p."); i >= 0 {
		s = s[:i]
	}
	return "Understand " + strings.ToLower(s) + " as part of " + topic + ", then solve with reasoning."
}

func visualKindForTopic(code string) string {
	switch {
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

func lessonExamples(code, title string) []gin.H {
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

func ex(title, body string) gin.H   { return gin.H{"title": title, "body": body} }
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
		{"P4-STATISTICS", "P4", "Statistics", "Data Representation and Interpretation", "Tables, Line Graphs and Pie Charts", []string{"Complete tables from data.", "Read and interpret data from tables, line graphs and pie charts."}, []string{"P3-STATISTICS"}, nil, 410},

		{"P5-WHOLE-OPERATIONS", "P5", "Number and Algebra", "Whole Numbers", "Numbers up to 10 million and Order of Operations", []string{"Read and write numbers up to 10 million.", "Multiply/divide by 10, 100, 1000 and multiples.", "Use order of operations and brackets without calculator."}, []string{"P4-WHOLE-100000", "P4-FOUR-OPERATIONS"}, nil, 501},
		{"P5-FRACTIONS", "P5", "Number and Algebra", "Fractions", "Fraction and Division; Four Operations", []string{"Divide whole numbers with quotient as fraction.", "Express fractions as decimals.", "Add/subtract mixed numbers.", "Multiply fractions and whole numbers."}, []string{"P4-FRACTIONS-MIXED", "P4-DECIMALS"}, nil, 502},
		{"P5-DECIMALS", "P5", "Number and Algebra", "Decimals", "Decimals and Measurement Conversion", []string{"Multiply/divide decimals by 10, 100, 1000 and multiples.", "Convert measurements between smaller and larger units in decimal form."}, []string{"P4-DECIMALS", "P3-MEASUREMENT"}, nil, 503},
		{"P5-PERCENTAGE-RATE", "P5", "Number and Algebra", "Percentage and Rate", "Percentage and Rate", []string{"Express part of whole as percentage.", "Find percentage part of a whole.", "Find discount, GST and annual interest.", "Understand rate and find rate/total/unit count."}, []string{"P4-FRACTIONS-MIXED", "P4-DECIMALS"}, nil, 504},
		{"P5-AREA-VOLUME", "P5", "Measurement and Geometry", "Area and Volume", "Area of Triangle and Volume of Cube/Cuboid", []string{"Understand base and height of triangle.", "Find area of triangle and composite figures.", "Build solids with unit cubes.", "Measure and calculate volume of cube/cuboid and rectangular tank."}, []string{"P4-AREA-PERIMETER"}, nil, 505},
		{"P5-GEOMETRY", "P5", "Measurement and Geometry", "Geometry", "Angles and Triangles", []string{"Use angles on a straight line, at a point and vertically opposite angles.", "Find unknown angles.", "Use properties and angle sum of triangles.", "Use properties of parallelogram, rhombus and trapezium."}, []string{"P4-GEOMETRY-ANGLES"}, nil, 506},

		{"P6-FRACTIONS", "P6", "Number and Algebra", "Fractions", "Division of Fractions", []string{"Divide a proper fraction by a whole number.", "Divide a whole number or proper fraction by a proper fraction."}, []string{"P5-FRACTIONS"}, nil, 601},
		{"P6-PERCENTAGE-RATIO", "P6", "Number and Algebra", "Percentage and Ratio", "Percentage and Ratio", []string{"Find whole given part and percentage.", "Find percentage increase/decrease.", "Use ratio notation and equivalent ratios.", "Divide quantities in a ratio and find missing terms.", "Relate fractions and ratios."}, []string{"P5-PERCENTAGE-RATE", "P5-FRACTIONS"}, nil, 602},
		{"P6-ALGEBRA", "P6", "Number and Algebra", "Algebra", "Simple Algebra", []string{"Use letters for unknown numbers.", "Interpret simple algebraic expressions.", "Simplify simple linear expressions.", "Evaluate expressions by substitution.", "Solve simple linear equations with whole number coefficients."}, []string{"P5-WHOLE-OPERATIONS"}, nil, 603},
		{"P6-AREA-VOLUME", "P6", "Measurement and Geometry", "Area and Volume", "Circle Area/Circumference and Cube/Cuboid Volume", []string{"Find area and circumference of circles.", "Find area/perimeter of semicircles and quarter circles.", "Find area/perimeter of composite figures.", "Find unknown dimensions of cuboids and cubes from volume."}, []string{"P5-AREA-VOLUME"}, nil, 604},
		{"P6-GEOMETRY", "P6", "Measurement and Geometry", "Geometry", "Special Quadrilaterals and Unknown Angles", []string{"Find unknown angles in composite figures involving square, rectangle, triangle, parallelogram, rhombus and trapezium."}, []string{"P5-GEOMETRY"}, nil, 605},
		{"P6-STATISTICS", "P6", "Statistics", "Data Analysis", "Average of a Set of Data", []string{"Understand average as total value divided by number of data.", "Use relationship between average, total value and number of data."}, []string{"P4-STATISTICS"}, nil, 606},
	}
}
