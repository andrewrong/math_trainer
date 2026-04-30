package handler

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"mathtrainer/internal/model"
)

type MathHandler struct {
	db *sql.DB
}

func NewMath(db *sql.DB) *MathHandler { return &MathHandler{db: db} }

func (h *MathHandler) Register(r gin.IRouter) {
	r.GET("/math/sessions", h.listSessions)
	r.POST("/math/sessions", h.saveSession)
	r.DELETE("/math/sessions", h.clearSessions)

	r.GET("/math/wrongs", h.listWrongs)
	r.PUT("/math/wrongs", h.upsertWrong)

	r.GET("/math/mastered", h.listMastered)
	r.PUT("/math/mastered/:factKey", h.setMastered)

	r.GET("/math/challenge/weak", h.listChallengeWeak)
	r.PUT("/math/challenge/weak", h.upsertChallengeWeak)
	r.DELETE("/math/challenge/weak/:key", h.deleteChallengeWeak)
}

// ─── Sessions ─────────────────────────────────────────────────────────────────

func (h *MathHandler) listSessions(c *gin.Context) {
	rows, err := h.db.QueryContext(c, `
		SELECT id, created_at, area, level, type_key, total, correct, rate, elapsed
		FROM math_sessions ORDER BY created_at ASC`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	defer rows.Close()
	sessions := []model.MathSession{}
	for rows.Next() {
		var s model.MathSession
		var ts string
		if err := rows.Scan(&s.ID, &ts, &s.Area, &s.Level, &s.TypeKey,
			&s.Total, &s.Correct, &s.Rate, &s.Elapsed); err != nil {
			continue
		}
		s.CreatedAt, _ = time.Parse("2006-01-02T15:04:05Z", ts)
		if s.CreatedAt.IsZero() {
			s.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ts)
		}
		sessions = append(sessions, s)
	}
	c.JSON(http.StatusOK, gin.H{"sessions": sessions})
}

func (h *MathHandler) saveSession(c *gin.Context) {
	var req model.SaveSessionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	res, err := h.db.ExecContext(c, `
		INSERT INTO math_sessions(area,level,type_key,total,correct,rate,elapsed)
		VALUES(?,?,?,?,?,?,?)`,
		req.Area, req.Level, req.TypeKey, req.Total, req.Correct, req.Rate, req.Elapsed)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(http.StatusOK, gin.H{"id": id})
}

func (h *MathHandler) clearSessions(c *gin.Context) {
	if _, err := h.db.ExecContext(c, `DELETE FROM math_sessions`); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.OKResponse{OK: true})
}

// ─── Wrongs (shared for practice + challenge via category) ───────────────────

func (h *MathHandler) listWrongs(c *gin.Context) {
	category := c.DefaultQuery("category", "practice")
	rows, err := h.db.QueryContext(c, `
		SELECT key, category, q_a, q_op, q_b, q_ans, count
		FROM wrong_answers WHERE category = ?
		ORDER BY count DESC`, category)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	defer rows.Close()
	wrongs := []model.WrongAnswer{}
	for rows.Next() {
		var w model.WrongAnswer
		if err := rows.Scan(&w.Key, &w.Category,
			&w.Q.A, &w.Q.Op, &w.Q.B, &w.Q.Ans, &w.Count); err != nil {
			continue
		}
		w.Q.Key = w.Key
		wrongs = append(wrongs, w)
	}
	c.JSON(http.StatusOK, gin.H{"wrongs": wrongs})
}

func (h *MathHandler) upsertWrong(c *gin.Context) {
	var req model.UpsertWrongReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	if req.Delta > 0 {
		// Insert or increment
		_, err := h.db.ExecContext(c, `
			INSERT INTO wrong_answers(key,category,q_a,q_op,q_b,q_ans,count)
			VALUES(?,?,?,?,?,?,1)
			ON CONFLICT(key,category) DO UPDATE SET count=count+1, updated_at=datetime('now')`,
			req.Key, req.Category, req.A, req.Op, req.B, req.Ans)
		if err != nil {
			c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
			return
		}
	} else if req.Delta < 0 {
		// Decrement, delete if reaches 0
		_, err := h.db.ExecContext(c, `
			UPDATE wrong_answers SET count=count-1, updated_at=datetime('now')
			WHERE key=? AND category=?`, req.Key, req.Category)
		if err != nil {
			c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
			return
		}
		h.db.ExecContext(c, `DELETE FROM wrong_answers WHERE key=? AND category=? AND count<=0`,
			req.Key, req.Category)
	}
	c.JSON(http.StatusOK, model.OKResponse{OK: true})
}

// ─── Challenge weak (convenience wrappers using category='challenge') ─────────

func (h *MathHandler) listChallengeWeak(c *gin.Context) {
	// Reuse listWrongs with category=challenge
	c.Request.URL.RawQuery = "category=challenge"
	h.listWrongs(c)
}

func (h *MathHandler) upsertChallengeWeak(c *gin.Context) {
	var req model.UpsertWrongReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	req.Category = "challenge"
	// Rebuild request context
	c.Set("overrideReq", req)
	h.upsertWrongWithReq(c, req)
}

func (h *MathHandler) deleteChallengeWeak(c *gin.Context) {
	key := c.Param("key")
	if _, err := h.db.ExecContext(c, `DELETE FROM wrong_answers WHERE key=? AND category='challenge'`, key); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.OKResponse{OK: true})
}

func (h *MathHandler) upsertWrongWithReq(c *gin.Context, req model.UpsertWrongReq) {
	if req.Delta > 0 {
		h.db.ExecContext(c, `
			INSERT INTO wrong_answers(key,category,q_a,q_op,q_b,q_ans,count)
			VALUES(?,?,?,?,?,?,1)
			ON CONFLICT(key,category) DO UPDATE SET count=count+1, updated_at=datetime('now')`,
			req.Key, req.Category, req.A, req.Op, req.B, req.Ans)
	} else if req.Delta < 0 {
		h.db.ExecContext(c, `
			UPDATE wrong_answers SET count=count-1, updated_at=datetime('now')
			WHERE key=? AND category=?`, req.Key, req.Category)
		h.db.ExecContext(c, `DELETE FROM wrong_answers WHERE key=? AND category=? AND count<=0`,
			req.Key, req.Category)
	}
	c.JSON(http.StatusOK, model.OKResponse{OK: true})
}

// ─── Mastered facts ───────────────────────────────────────────────────────────

func (h *MathHandler) listMastered(c *gin.Context) {
	rows, err := h.db.QueryContext(c, `SELECT fact_key, status FROM mastered_facts`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	defer rows.Close()
	facts := []model.MasteredFact{}
	for rows.Next() {
		var f model.MasteredFact
		rows.Scan(&f.FactKey, &f.Status)
		facts = append(facts, f)
	}
	c.JSON(http.StatusOK, gin.H{"facts": facts})
}

func (h *MathHandler) setMastered(c *gin.Context) {
	factKey := c.Param("factKey")
	var req model.SetMasteredReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	_, err := h.db.ExecContext(c, `
		INSERT INTO mastered_facts(fact_key, status)
		VALUES(?,?)
		ON CONFLICT(fact_key) DO UPDATE SET status=excluded.status, updated_at=datetime('now')`,
		factKey, req.Status)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.OKResponse{OK: true})
}
