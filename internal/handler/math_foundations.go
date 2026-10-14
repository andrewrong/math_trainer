package handler

import (
	"database/sql"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"mathtrainer/internal/model"
)

// MathFoundationsHandler keeps prerequisite evidence separate from timed maths
// practice. It is deliberately small: the browser owns the visual models and
// item wording; the server owns durable evidence and readiness states.
type MathFoundationsHandler struct{ db *sql.DB }

func NewMathFoundations(db *sql.DB) *MathFoundationsHandler { return &MathFoundationsHandler{db: db} }

func (h *MathFoundationsHandler) Register(r gin.IRouter) {
	r.GET("/math/foundations/overview", h.overview)
	r.POST("/math/foundations/attempts", h.saveAttempt)
	r.POST("/math/foundations/paper-checks", h.savePaperCheck)
}

type foundationSkill struct {
	Key          string `json:"key"`
	Title        string `json:"title"`
	ShortTitle   string `json:"shortTitle"`
	Description  string `json:"description"`
	ParentPrompt string `json:"parentPrompt"`
	Source       string `json:"source"`
	Order        int    `json:"order"`
}

var foundationSkills = []foundationSkill{
	{Key: "even-odd-pairing", Title: "Pairing: Even and Odd", ShortTitle: "Even & odd", Description: "Pair objects two by two. An even number has no leftover; an odd number has one.", ParentPrompt: "Ask: Can you make pairs of two? Is there one left over?", Source: "Number sense · prepares equal groups", Order: 1},
	{Key: "fraction-whole-parts", Title: "A Whole and Equal Parts", ShortTitle: "Equal parts", Description: "A fraction names equal parts of one whole. Unequal pieces do not make a fair fraction model.", ParentPrompt: "Ask: Are all the parts the same size? What is the whole?", Source: "Builds on P3 Fractions", Order: 2},
	{Key: "fraction-name-model", Title: "Name a Fraction", ShortTitle: "Name fractions", Description: "The bottom number tells how many equal parts; the top number tells how many are chosen.", ParentPrompt: "Ask: How many equal parts altogether? How many are shaded?", Source: "Builds on P3 Fractions", Order: 3},
	{Key: "fraction-equivalent-compare", Title: "Equivalent and Compare", ShortTitle: "Equivalent fractions", Description: "Use the same whole and a bar or number line to see which fraction is larger, smaller, or equal.", ParentPrompt: "Ask: Is the whole the same? Can you show both fractions on one bar?", Source: "P3 Fractions → P4 bridge", Order: 4},
	{Key: "p4-fraction-bridge", Title: "Bridge to P4 Fractions", ShortTitle: "P4 bridge", Description: "Connect improper fractions, mixed numbers, and fractions of a set to the equal-parts idea.", ParentPrompt: "Ask: How many whole groups do you have? What fraction is left?", Source: "Prepares P4 Mixed Numbers & Fraction of a Set", Order: 5},
}

type foundationAttemptRequest struct {
	SkillKey       string `json:"skillKey" binding:"required"`
	ItemKey        string `json:"itemKey" binding:"required"`
	SetKey         string `json:"setKey"`
	Mode           string `json:"mode" binding:"required"`
	Correct        bool   `json:"correct"`
	HintUsed       bool   `json:"hintUsed"`
	ElapsedSeconds int    `json:"elapsedSeconds"`
}

type foundationPaperCheckRequest struct {
	SkillKey  string `json:"skillKey" binding:"required"`
	Completed bool   `json:"completed"`
	Note      string `json:"note"`
}

type foundationSkillState struct {
	foundationSkill
	Status          string `json:"status"`
	AttemptCount    int    `json:"attemptCount"`
	IndependentSets int    `json:"independentSets"`
	PaperChecks     int    `json:"paperChecks"`
	NextStep        string `json:"nextStep"`
}

func (h *MathFoundationsHandler) overview(c *gin.Context) {
	states, err := h.skillStates(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	next := "even-odd-pairing"
	for _, state := range states {
		if state.Status != "bridge_ready" {
			next = state.Key
			break
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"title":          "Math Foundations",
		"purpose":        "Repair the small ideas that make P4 fractions feel easier.",
		"nextSkillKey":   next,
		"skills":         states,
		"mainLessonLink": "#/curriculum/P4-FRACTIONS-MIXED",
	})
}

func (h *MathFoundationsHandler) saveAttempt(c *gin.Context) {
	var req foundationAttemptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	req.SkillKey, req.ItemKey, req.SetKey = strings.TrimSpace(req.SkillKey), strings.TrimSpace(req.ItemKey), strings.TrimSpace(req.SetKey)
	if !isFoundationSkill(req.SkillKey) || req.ItemKey == "" {
		c.JSON(http.StatusBadRequest, model.APIError{Error: "unknown foundation skill or missing item"})
		return
	}
	if req.Mode != "diagnostic" && req.Mode != "guided" && req.Mode != "check" {
		c.JSON(http.StatusBadRequest, model.APIError{Error: "mode must be diagnostic, guided, or check"})
		return
	}
	if req.ElapsedSeconds < 0 || req.ElapsedSeconds > 3600 {
		c.JSON(http.StatusBadRequest, model.APIError{Error: "elapsedSeconds must be between 0 and 3600"})
		return
	}
	_, err := h.db.ExecContext(c, `INSERT INTO math_foundation_attempts(skill_key,item_key,set_key,mode,correct,hint_used,elapsed_seconds) VALUES(?,?,?,?,?,?,?)`, req.SkillKey, req.ItemKey, req.SetKey, req.Mode, boolInt(req.Correct), boolInt(req.HintUsed), req.ElapsedSeconds)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	states, err := h.skillStates(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	for _, state := range states {
		if state.Key == req.SkillKey {
			c.JSON(http.StatusOK, gin.H{"ok": true, "skill": state})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *MathFoundationsHandler) savePaperCheck(c *gin.Context) {
	var req foundationPaperCheckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	req.SkillKey, req.Note = strings.TrimSpace(req.SkillKey), strings.TrimSpace(req.Note)
	if !isFoundationSkill(req.SkillKey) {
		c.JSON(http.StatusBadRequest, model.APIError{Error: "unknown foundation skill"})
		return
	}
	if len(req.Note) > 600 {
		c.JSON(http.StatusBadRequest, model.APIError{Error: "note must be 600 characters or fewer"})
		return
	}
	_, err := h.db.ExecContext(c, `INSERT INTO math_foundation_paper_checks(skill_key,completed,note) VALUES(?,?,?)`, req.SkillKey, boolInt(req.Completed), req.Note)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *MathFoundationsHandler) skillStates(c *gin.Context) ([]foundationSkillState, error) {
	type stats struct{ attempts, paper int }
	byKey := map[string]*stats{}
	for _, skill := range foundationSkills {
		byKey[skill.Key] = &stats{}
	}
	rows, err := h.db.QueryContext(c, `SELECT skill_key, COUNT(*) FROM math_foundation_attempts GROUP BY skill_key`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var key string
		var count int
		if err := rows.Scan(&key, &count); err == nil && byKey[key] != nil {
			byKey[key].attempts = count
		}
	}
	rows.Close()
	rows, err = h.db.QueryContext(c, `SELECT skill_key, COUNT(*) FROM math_foundation_paper_checks WHERE completed=1 GROUP BY skill_key`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var key string
		var count int
		if err := rows.Scan(&key, &count); err == nil && byKey[key] != nil {
			byKey[key].paper = count
		}
	}
	rows.Close()

	sets := map[string]map[string]struct {
		total, correct, hints int
		day                   string
	}{}
	rows, err = h.db.QueryContext(c, `SELECT skill_key,set_key,correct,hint_used,date(created_at) FROM math_foundation_attempts WHERE mode='check' AND set_key<>''`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var key, setKey, day string
		var correct, hint int
		if err := rows.Scan(&key, &setKey, &correct, &hint, &day); err != nil || byKey[key] == nil {
			continue
		}
		if sets[key] == nil {
			sets[key] = map[string]struct {
				total, correct, hints int
				day                   string
			}{}
		}
		s := sets[key][setKey]
		s.total++
		s.correct += correct
		s.hints += hint
		s.day = day
		sets[key][setKey] = s
	}
	rows.Close()

	states := make([]foundationSkillState, 0, len(foundationSkills))
	for _, skill := range foundationSkills {
		independentDays := map[string]bool{}
		for _, set := range sets[skill.Key] {
			if set.total >= 5 && set.correct*100 >= set.total*80 && set.hints == 0 {
				independentDays[set.day] = true
			}
		}
		count := len(independentDays)
		status, next := "not_checked", "Start with the short check."
		if byKey[skill.Key].attempts > 0 {
			status, next = "learning", "Use the model, then try three guided questions."
		}
		if count == 1 {
			status, next = "practising", "Come back on another day for one more independent check."
		}
		if count >= 2 {
			status, next = "bridge_ready", "This skill has two independent checks on different days."
		}
		states = append(states, foundationSkillState{foundationSkill: skill, Status: status, AttemptCount: byKey[skill.Key].attempts, IndependentSets: count, PaperChecks: byKey[skill.Key].paper, NextStep: next})
	}
	sort.Slice(states, func(i, j int) bool { return states[i].Order < states[j].Order })
	return states, nil
}

func isFoundationSkill(key string) bool {
	for _, skill := range foundationSkills {
		if skill.Key == key {
			return true
		}
	}
	return false
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
