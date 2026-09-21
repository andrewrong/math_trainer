package handler

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"mathtrainer/internal/config"
	"mathtrainer/internal/model"
)

// PianoHandler tracks teacher-assigned work. It intentionally has no score
// parser: uploaded photos/PDFs remain the authoritative musical source.
type PianoHandler struct {
	db         *sql.DB
	cfg        config.ReadingConfig
	httpClient *http.Client
}

func NewPiano(db *sql.DB, cfg config.ReadingConfig) *PianoHandler {
	timeout := time.Duration(cfg.OCR.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 900 * time.Second
	}
	return &PianoHandler{db: db, cfg: cfg, httpClient: &http.Client{Timeout: timeout}}
}
func (h *PianoHandler) Register(r gin.IRouter) {
	r.GET("/piano/weeks", h.list)
	r.POST("/piano/weeks", h.create)
	r.GET("/piano/weeks/:weekId", h.detail)
	r.POST("/piano/weeks/:weekId/tasks", h.addTask)
	r.PATCH("/piano/tasks/:taskId", h.updateTask)
	r.POST("/piano/weeks/:weekId/assets", h.upload)
	r.GET("/piano/assets/:assetId", h.asset)
	r.POST("/piano/weeks/:weekId/sessions", h.session)
	r.POST("/piano/weeks/:weekId/feedback", h.feedback)
	r.GET("/piano/today", h.today)
}
func pianoID(c *gin.Context, key string) (int64, bool) {
	id, e := strconv.ParseInt(c.Param(key), 10, 64)
	if e != nil || id < 1 {
		c.JSON(400, model.APIError{Error: "invalid " + key})
		return 0, false
	}
	return id, true
}
func pianoErr(c *gin.Context, s int, e error) { c.JSON(s, model.APIError{Error: e.Error()}) }
func (h *PianoHandler) list(c *gin.Context) {
	rows, e := h.db.QueryContext(c, `SELECT id,label,lesson_date,teacher_note,created_at FROM piano_weeks ORDER BY lesson_date DESC,created_at DESC`)
	if e != nil {
		pianoErr(c, 500, e)
		return
	}
	defer rows.Close()
	out := []model.PianoWeek{}
	for rows.Next() {
		var w model.PianoWeek
		if rows.Scan(&w.ID, &w.Label, &w.LessonDate, &w.TeacherNote, &w.CreatedAt) == nil {
			out = append(out, w)
		}
	}
	c.JSON(200, gin.H{"weeks": out})
}
func (h *PianoHandler) create(c *gin.Context) {
	var q model.CreatePianoWeekReq
	if c.ShouldBindJSON(&q) != nil {
		pianoErr(c, 400, errors.New("label is required"))
		return
	}
	q.Label = strings.TrimSpace(q.Label)
	if q.Label == "" {
		pianoErr(c, 400, errors.New("label is required"))
		return
	}
	res, e := h.db.ExecContext(c, `INSERT INTO piano_weeks(label,lesson_date,teacher_note) VALUES(?,?,?)`, q.Label, q.LessonDate, strings.TrimSpace(q.TeacherNote))
	if e != nil {
		pianoErr(c, 500, e)
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(201, gin.H{"id": id})
}
func (h *PianoHandler) detail(c *gin.Context) {
	id, ok := pianoID(c, "weekId")
	if !ok {
		return
	}
	var w model.PianoWeek
	if e := h.db.QueryRowContext(c, `SELECT id,label,lesson_date,teacher_note,created_at FROM piano_weeks WHERE id=?`, id).Scan(&w.ID, &w.Label, &w.LessonDate, &w.TeacherNote, &w.CreatedAt); e != nil {
		pianoErr(c, 404, errors.New("piano week not found"))
		return
	}
	w.Tasks = h.tasks(c, id)
	w.Assets = h.assets(c, id)
	w.Feedback = h.feedbacks(c, id)
	c.JSON(200, gin.H{"week": w})
}
func (h *PianoHandler) tasks(c *gin.Context, w int64) []model.PianoTask {
	rows, e := h.db.QueryContext(c, `SELECT id,week_id,kind,title,range_label,goal,priority,status FROM piano_tasks WHERE week_id=? ORDER BY priority DESC,id`, w)
	if e != nil {
		return nil
	}
	defer rows.Close()
	o := []model.PianoTask{}
	for rows.Next() {
		var x model.PianoTask
		if rows.Scan(&x.ID, &x.WeekID, &x.Kind, &x.Title, &x.RangeLabel, &x.Goal, &x.Priority, &x.Status) == nil {
			o = append(o, x)
		}
	}
	return o
}
func (h *PianoHandler) assets(c *gin.Context, w int64) []model.PianoAsset {
	rows, e := h.db.QueryContext(c, `SELECT id,week_id,COALESCE(task_id,0),kind,original_name,mime_type,ocr_text,created_at FROM piano_assets WHERE week_id=? ORDER BY id DESC`, w)
	if e != nil {
		return nil
	}
	defer rows.Close()
	o := []model.PianoAsset{}
	for rows.Next() {
		var x model.PianoAsset
		if rows.Scan(&x.ID, &x.WeekID, &x.TaskID, &x.Kind, &x.OriginalName, &x.MimeType, &x.OCRText, &x.CreatedAt) == nil {
			o = append(o, x)
		}
	}
	return o
}
func (h *PianoHandler) feedbacks(c *gin.Context, w int64) []model.PianoFeedback {
	rows, e := h.db.QueryContext(c, `SELECT id,week_id,COALESCE(task_id,0),COALESCE(asset_id,0),note,created_at FROM piano_feedback WHERE week_id=? ORDER BY id DESC`, w)
	if e != nil {
		return nil
	}
	defer rows.Close()
	o := []model.PianoFeedback{}
	for rows.Next() {
		var x model.PianoFeedback
		if rows.Scan(&x.ID, &x.WeekID, &x.TaskID, &x.AssetID, &x.Note, &x.CreatedAt) == nil {
			o = append(o, x)
		}
	}
	return o
}
func (h *PianoHandler) addTask(c *gin.Context) {
	w, ok := pianoID(c, "weekId")
	if !ok {
		return
	}
	var q model.SavePianoTaskReq
	if c.ShouldBindJSON(&q) != nil {
		pianoErr(c, 400, errors.New("kind and title are required"))
		return
	}
	if q.Priority < 1 || q.Priority > 3 {
		q.Priority = 2
	}
	if q.Status == "" {
		q.Status = "active"
	}
	q.Title = strings.TrimSpace(q.Title)
	if q.Title == "" {
		pianoErr(c, 400, errors.New("task title is required"))
		return
	}
	res, e := h.db.ExecContext(c, `INSERT INTO piano_tasks(week_id,kind,title,range_label,goal,priority,status) VALUES(?,?,?,?,?,?,?)`, w, q.Kind, q.Title, strings.TrimSpace(q.RangeLabel), strings.TrimSpace(q.Goal), q.Priority, q.Status)
	if e != nil {
		pianoErr(c, 500, e)
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(201, gin.H{"id": id})
}
func (h *PianoHandler) updateTask(c *gin.Context) {
	id, ok := pianoID(c, "taskId")
	if !ok {
		return
	}
	var q model.SavePianoTaskReq
	if c.ShouldBindJSON(&q) != nil {
		pianoErr(c, 400, errors.New("valid task is required"))
		return
	}
	if q.Priority < 1 || q.Priority > 3 {
		q.Priority = 2
	}
	if q.Status == "" {
		q.Status = "active"
	}
	_, e := h.db.ExecContext(c, `UPDATE piano_tasks SET kind=?,title=?,range_label=?,goal=?,priority=?,status=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, q.Kind, q.Title, q.RangeLabel, q.Goal, q.Priority, q.Status, id)
	if e != nil {
		pianoErr(c, 500, e)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
func (h *PianoHandler) upload(c *gin.Context) {
	w, ok := pianoID(c, "weekId")
	if !ok {
		return
	}
	f, head, e := c.Request.FormFile("file")
	if e != nil {
		pianoErr(c, 400, errors.New("choose a photo or PDF"))
		return
	}
	defer f.Close()
	kind := c.DefaultQuery("kind", "assignment")
	if kind != "assignment" && kind != "feedback" {
		pianoErr(c, 400, errors.New("kind must be assignment or feedback"))
		return
	}
	path, mime, digest, e := h.store(head.Filename, f)
	if e != nil {
		pianoErr(c, 400, e)
		return
	}
	var task any = nil
	if v := c.Query("taskId"); v != "" {
		task, _ = strconv.ParseInt(v, 10, 64)
	}
	res, e := h.db.ExecContext(c, `INSERT INTO piano_assets(week_id,task_id,kind,source_path,original_name,mime_type,ocr_text) VALUES(?,?,?,?,?,?,?)`, w, task, kind, path, head.Filename, mime, "")
	if e != nil {
		pianoErr(c, 500, e)
		return
	}
	id, _ := res.LastInsertId()
	ocrText, ocrErr := h.callPianoOCR(c, path, head.Filename)
	if ocrErr == nil && ocrText != "" {
		if _, err := h.db.ExecContext(c, `UPDATE piano_assets SET ocr_text=? WHERE id=?`, ocrText, id); err != nil {
			pianoErr(c, 500, err)
			return
		}
	}
	response := gin.H{
		"assetId":              id,
		"sha256":               digest,
		"requiresConfirmation": true,
		"suggestion":           pianoSuggestion(ocrText),
		"notice":               "The original photo or PDF is the source of truth. OCR only suggests text fields; it never reads, redraws, or evaluates notation.",
	}
	if ocrErr != nil {
		response["ocrWarning"] = "Source saved. OCR suggestions are unavailable right now; add the task details manually."
	}
	c.JSON(201, response)
}

type pianoOCRResult struct {
	Markdown string `json:"markdown"`
	Engine   string `json:"engine"`
	Pages    int    `json:"pages"`
}

func (h *PianoHandler) callPianoOCR(ctx *gin.Context, path, filename string) (string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(h.cfg.OCR.BaseURL), "/")
	parsed, err := url.Parse(baseURL)
	if baseURL == "" || err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("OCR service is not configured")
	}
	engine := strings.ToLower(strings.TrimSpace(h.cfg.OCR.Engine))
	if engine == "" {
		engine = "auto"
	}
	if engine != "auto" && engine != "paddle" && engine != "glm" && engine != "qwen" {
		return "", errors.New("OCR engine is invalid")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read source for OCR: %w", err)
	}
	endpoints := []string{"/v1/ocr/image", "/ocr/image"}
	if strings.EqualFold(filepath.Ext(filename), ".pdf") {
		endpoints = []string{"/v1/ocr/pdf", "/ocr/pdf"}
	}
	for i, endpoint := range endpoints {
		result, status, requestErr := h.postPianoOCR(ctx, baseURL+endpoint, data, filename, engine)
		if requestErr == nil {
			return result.Markdown, nil
		}
		if status != http.StatusNotFound || i == len(endpoints)-1 {
			return "", requestErr
		}
	}
	return "", errors.New("OCR route fallback did not return a response")
}

func (h *PianoHandler) postPianoOCR(ctx *gin.Context, target string, data []byte, filename, engine string) (pianoOCRResult, int, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return pianoOCRResult{}, 0, err
	}
	if _, err := part.Write(data); err != nil {
		return pianoOCRResult{}, 0, err
	}
	if err := writer.WriteField("engine", engine); err != nil {
		return pianoOCRResult{}, 0, err
	}
	if err := writer.Close(); err != nil {
		return pianoOCRResult{}, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, &body)
	if err != nil {
		return pianoOCRResult{}, 0, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "learning-compass/piano-homework")
	if h.cfg.OCR.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+h.cfg.OCR.APIKey)
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return pianoOCRResult{}, 0, fmt.Errorf("OCR request failed: %w", err)
	}
	defer resp.Body.Close()
	response, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return pianoOCRResult{}, resp.StatusCode, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var serviceError struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(response, &serviceError)
		message := strings.TrimSpace(serviceError.Error)
		if message == "" {
			message = strings.TrimSpace(string(response))
		}
		return pianoOCRResult{}, resp.StatusCode, fmt.Errorf("OCR returned %s: %s", resp.Status, truncatePiano(message, 500))
	}
	var result pianoOCRResult
	if err := json.Unmarshal(response, &result); err != nil {
		return pianoOCRResult{}, resp.StatusCode, fmt.Errorf("decode OCR response: %w", err)
	}
	result.Markdown = strings.TrimSpace(result.Markdown)
	if result.Markdown == "" || result.Engine == "" || result.Pages < 1 {
		return pianoOCRResult{}, resp.StatusCode, errors.New("OCR service returned an invalid success response")
	}
	return result, resp.StatusCode, nil
}

func truncatePiano(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "…"
}

func pianoSuggestion(text string) gin.H {
	lines := strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == '\r' })
	title := ""
	for _, line := range lines {
		line = strings.Trim(strings.TrimSpace(line), "#*- ")
		if len(line) > 2 && len(line) < 100 {
			title = line
			break
		}
	}
	rangeMatch := regexp.MustCompile(`(?i)\b(?:p(?:age|\.)?\s*\d+(?:\s*[-–]\s*\d+)?|bars?\s*\d+(?:\s*[-–]\s*\d+)?)\b`).FindString(text)
	return gin.H{"title": title, "rangeLabel": rangeMatch, "ocrText": truncatePiano(strings.TrimSpace(text), 4000)}
}
func (h *PianoHandler) store(name string, src io.Reader) (string, string, string, error) {
	ext := strings.ToLower(filepath.Ext(name))
	mime := map[string]string{".pdf": "application/pdf", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".webp": "image/webp"}[ext]
	if mime == "" {
		return "", "", "", errors.New("use PDF, JPG, PNG, or WebP")
	}
	dir := filepath.Join(h.cfg.ContentDir, "piano")
	if dir == "piano" {
		dir = "./resources/reading/piano"
	}
	if e := os.MkdirAll(dir, 0755); e != nil {
		return "", "", "", e
	}
	b, e := io.ReadAll(io.LimitReader(src, 32<<20))
	if e != nil {
		return "", "", "", e
	}
	if len(b) >= 32<<20 {
		return "", "", "", errors.New("file exceeds 32 MB")
	}
	sum := sha256.Sum256(b)
	d := hex.EncodeToString(sum[:])
	p := filepath.Join(dir, d+ext)
	if _, e := os.Stat(p); os.IsNotExist(e) {
		if e = os.WriteFile(p, b, 0644); e != nil {
			return "", "", "", e
		}
	}
	return p, mime, d, nil
}
func (h *PianoHandler) asset(c *gin.Context) {
	id, ok := pianoID(c, "assetId")
	if !ok {
		return
	}
	var p, m string
	if e := h.db.QueryRowContext(c, `SELECT source_path,mime_type FROM piano_assets WHERE id=?`, id).Scan(&p, &m); e != nil {
		pianoErr(c, 404, errors.New("piano source not found"))
		return
	}
	c.Header("Content-Type", m)
	c.Header("Content-Disposition", "inline")
	c.File(p)
}
func (h *PianoHandler) session(c *gin.Context) {
	w, ok := pianoID(c, "weekId")
	if !ok {
		return
	}
	var q model.SavePianoSessionReq
	if c.ShouldBindJSON(&q) != nil {
		pianoErr(c, 400, errors.New("choose a self-check status"))
		return
	}
	if q.Minutes < 1 || q.Minutes > 90 {
		q.Minutes = 25
	}
	var taskID any
	if q.TaskID > 0 {
		taskID = q.TaskID
	}
	_, e := h.db.ExecContext(c, `INSERT INTO piano_sessions(week_id,task_id,minutes,self_status,difficulty_note) VALUES(?,?,?,?,?)`, w, taskID, q.Minutes, q.SelfStatus, strings.TrimSpace(q.DifficultyNote))
	if e != nil {
		pianoErr(c, 500, e)
		return
	}
	if q.TaskID > 0 {
		priority := 2
		if q.SelfStatus == "stuck" {
			priority = 3
		} else if q.SelfStatus == "smooth" {
			priority = 1
		}
		_, _ = h.db.ExecContext(c, `UPDATE piano_tasks SET priority=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND week_id=? AND status='active'`, priority, q.TaskID, w)
	}
	c.JSON(201, gin.H{"ok": true})
}
func (h *PianoHandler) feedback(c *gin.Context) {
	w, ok := pianoID(c, "weekId")
	if !ok {
		return
	}
	var q model.SavePianoFeedbackReq
	if c.ShouldBindJSON(&q) != nil {
		pianoErr(c, 400, errors.New("feedback is invalid"))
		return
	}
	var taskID, assetID any
	if q.TaskID > 0 {
		taskID = q.TaskID
	}
	if q.AssetID > 0 {
		assetID = q.AssetID
	}
	_, e := h.db.ExecContext(c, `INSERT INTO piano_feedback(week_id,task_id,asset_id,note) VALUES(?,?,?,?)`, w, taskID, assetID, strings.TrimSpace(q.Note))
	if e != nil {
		pianoErr(c, 500, e)
		return
	}
	if q.TaskID > 0 {
		_, _ = h.db.ExecContext(c, `UPDATE piano_tasks SET priority=3,updated_at=CURRENT_TIMESTAMP WHERE id=? AND week_id=? AND status='active'`, q.TaskID, w)
	}
	c.JSON(201, gin.H{"ok": true})
}
func (h *PianoHandler) today(c *gin.Context) {
	var id int64
	if e := h.db.QueryRowContext(c, `SELECT id FROM piano_weeks ORDER BY lesson_date DESC,created_at DESC LIMIT 1`).Scan(&id); e != nil {
		c.JSON(200, gin.H{"plan": nil})
		return
	}
	tasks := h.tasks(c, id)
	var focus *model.PianoTask
	for i := range tasks {
		if tasks[i].Status == "active" && (focus == nil || tasks[i].Priority > focus.Priority) {
			focus = &tasks[i]
		}
	}
	c.JSON(200, gin.H{"weekId": id, "minutes": 30, "plan": gin.H{"warmup": "Warm up with the teacher-set technique.", "focus": focus, "reflect": "Choose smooth, practising, or stuck. Add one short note."}})
}
