package handler

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"mathtrainer/internal/model"
)

type PianoHandler struct {
	db *sql.DB
}

func NewPiano(db *sql.DB) *PianoHandler { return &PianoHandler{db: db} }

func (h *PianoHandler) Register(r gin.IRouter) {
	r.GET("/piano/assignments", h.listAssignments)
	r.POST("/piano/assignments", h.createAssignment)
	r.PUT("/piano/assignments/:id", h.updateAssignment)
	r.GET("/piano/practice-sessions", h.listPracticeSessions)
	r.POST("/piano/practice-sessions", h.createPracticeSession)
	r.PUT("/piano/skill-stats", h.updateSkillStats)
}

func (h *PianoHandler) listAssignments(c *gin.Context) {
	rows, err := h.db.QueryContext(c, `SELECT id,title,section,goal,due_date,status,created_at,updated_at FROM piano_assignments ORDER BY due_date='',due_date,updated_at DESC`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	defer rows.Close()
	out := []model.PianoAssignment{}
	for rows.Next() {
		var a model.PianoAssignment
		if err := rows.Scan(&a.ID, &a.Title, &a.Section, &a.Goal, &a.DueDate, &a.Status, &a.CreatedAt, &a.UpdatedAt); err == nil {
			out = append(out, a)
		}
	}
	c.JSON(http.StatusOK, gin.H{"assignments": out})
}

func (h *PianoHandler) createAssignment(c *gin.Context) {
	var req model.SavePianoAssignmentReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	if req.Status == "" {
		req.Status = "Not Started"
	}
	res, err := h.db.ExecContext(c, `INSERT INTO piano_assignments(title,section,goal,due_date,status) VALUES(?,?,?,?,?)`, req.Title, req.Section, req.Goal, req.DueDate, req.Status)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(http.StatusOK, gin.H{"id": id, "ok": true})
}

func (h *PianoHandler) updateAssignment(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var req model.SavePianoAssignmentReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	if req.Status == "" {
		req.Status = "Not Started"
	}
	if _, err := h.db.ExecContext(c, `UPDATE piano_assignments SET title=?,section=?,goal=?,due_date=?,status=?,updated_at=datetime('now') WHERE id=?`, req.Title, req.Section, req.Goal, req.DueDate, req.Status, id); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.OKResponse{OK: true})
}

func (h *PianoHandler) listPracticeSessions(c *gin.Context) {
	rows, err := h.db.QueryContext(c, `SELECT id,practiced_at,minutes,content,stuck,note FROM piano_practice_sessions ORDER BY practiced_at DESC LIMIT 30`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	defer rows.Close()
	out := []model.PianoPracticeSession{}
	for rows.Next() {
		var s model.PianoPracticeSession
		if err := rows.Scan(&s.ID, &s.PracticedAt, &s.Minutes, &s.Content, &s.Stuck, &s.Note); err == nil {
			out = append(out, s)
		}
	}
	c.JSON(http.StatusOK, gin.H{"sessions": out})
}

func (h *PianoHandler) createPracticeSession(c *gin.Context) {
	var req model.SavePianoPracticeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	if req.Minutes < 0 {
		req.Minutes = 0
	}
	res, err := h.db.ExecContext(c, `INSERT INTO piano_practice_sessions(minutes,content,stuck,note) VALUES(?,?,?,?)`, req.Minutes, req.Content, req.Stuck, req.Note)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(http.StatusOK, gin.H{"id": id, "ok": true})
}

func (h *PianoHandler) updateSkillStats(c *gin.Context) {
	var req model.UpdatePianoSkillReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	correct, wrong := 0, 0
	if req.Correct {
		correct = 1
	} else {
		wrong = 1
	}
	if _, err := h.db.ExecContext(c, `
		INSERT INTO piano_skill_stats(skill,correct_count,wrong_count,last_practiced_at)
		VALUES(?,?,?,datetime('now'))
		ON CONFLICT(skill) DO UPDATE SET
			correct_count=correct_count+excluded.correct_count,
			wrong_count=wrong_count+excluded.wrong_count,
			last_practiced_at=datetime('now')`,
		req.Skill, correct, wrong); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.OKResponse{OK: true})
}
