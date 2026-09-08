package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"mathtrainer/internal/model"
)

type EnglishCurriculumHandler struct {
	db *sql.DB
}

const englishCurriculumArtifactVersion = "eng-mypals-20260806-v18"

func NewEnglishCurriculum(db *sql.DB) *EnglishCurriculumHandler {
	return &EnglishCurriculumHandler{db: db}
}

func (h *EnglishCurriculumHandler) Register(r gin.IRouter) {
	r.GET("/english-curriculum/units", h.listUnits)
	r.GET("/english-curriculum/resources", h.listResources)
	r.GET("/english-curriculum/units/:unitId", h.getUnit)
	r.GET("/english-curriculum/units/:unitId/artifacts", h.getArtifacts)
	r.PUT("/english-curriculum/units/:unitId/progress", h.updateProgress)
	r.POST("/english-curriculum/contents/import", h.importContents)
	r.POST("/english-curriculum/artifacts/generate", h.generateArtifacts)
}

func (h *EnglishCurriculumHandler) ensureSeed(c *gin.Context) error {
	tx, err := h.db.BeginTx(c, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, u := range englishCurriculumSeedUnits() {
		if _, err := tx.ExecContext(c, `
			INSERT INTO english_curriculum_units(unit_id,grade,book,unit_no,title,page_start,sort_order)
			VALUES(?,?,?,?,?,?,?)
			ON CONFLICT(unit_id) DO UPDATE SET
				grade=excluded.grade,
				book=excluded.book,
				unit_no=excluded.unit_no,
				title=excluded.title,
				page_start=excluded.page_start,
				sort_order=excluded.sort_order`,
			u.UnitID, u.Grade, u.Book, u.UnitNo, u.Title, u.PageStart, u.SortOrder); err != nil {
			return err
		}
		for _, l := range englishCurriculumLessonsFor(u) {
			var n int
			if err := tx.QueryRowContext(c, `SELECT COUNT(*) FROM english_curriculum_lessons WHERE unit_id=? AND lesson_code=?`, u.UnitID, l["lessonCode"]).Scan(&n); err != nil {
				return err
			}
			if n > 0 {
				continue
			}
			if _, err := tx.ExecContext(c, `
				INSERT INTO english_curriculum_lessons(unit_id,lesson_code,title,skill_focus,source_ref,sort_order)
				VALUES(?,?,?,?,?,?)`,
				u.UnitID, l["lessonCode"], l["title"], l["skillFocus"], l["sourceRef"], l["sortOrder"]); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (h *EnglishCurriculumHandler) listUnits(c *gin.Context) {
	if err := h.ensureSeed(c); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	grade := c.DefaultQuery("grade", "4")
	rows, err := h.db.QueryContext(c, `
		SELECT unit_id,grade,book,unit_no,title,page_start,sort_order
		FROM english_curriculum_units WHERE grade=? ORDER BY sort_order`, grade)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	defer rows.Close()
	units := []model.EnglishCurriculumUnit{}
	for rows.Next() {
		var u model.EnglishCurriculumUnit
		if err := rows.Scan(&u.UnitID, &u.Grade, &u.Book, &u.UnitNo, &u.Title, &u.PageStart, &u.SortOrder); err == nil {
			units = append(units, u)
		}
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	rows.Close()
	for i := range units {
		h.attachProgress(c, &units[i])
	}
	c.JSON(http.StatusOK, gin.H{"units": units})
}

func (h *EnglishCurriculumHandler) listResources(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"resourceCoverage": englishCurriculumResourceCoverage(),
		"teachingRoadmap":  englishCurriculumTeachingRoadmap(),
		"repairBlocks":     englishCurriculumRepairBlocks(),
		"skillSpine":       englishCurriculumSkillSpine(),
		"diagnosticBridge": englishCurriculumDiagnosticBridge(),
		"homeLoop":         englishCurriculumHomeLoop(),
		"sourceAccess":     englishCurriculumSourceAccess(),
		"sourceNote":       "Local PDF coverage is a locator for planning. The app creates original lessons and does not copy textbook pages.",
	})
}

func (h *EnglishCurriculumHandler) importContents(c *gin.Context) {
	var req model.EnglishCurriculumImportReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	units, err := parseEnglishCurriculumImport(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	tx, err := h.db.BeginTx(c, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	defer tx.Rollback()
	for _, u := range units {
		if _, err := tx.ExecContext(c, `
			INSERT INTO english_curriculum_units(unit_id,grade,book,unit_no,title,page_start,sort_order)
			VALUES(?,?,?,?,?,?,?)
			ON CONFLICT(unit_id) DO UPDATE SET
				grade=excluded.grade,
				book=excluded.book,
				unit_no=excluded.unit_no,
				title=excluded.title,
				page_start=excluded.page_start,
				sort_order=excluded.sort_order`,
			u.UnitID, u.Grade, u.Book, u.UnitNo, u.Title, u.PageStart, u.SortOrder); err != nil {
			c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
			return
		}
		for _, l := range englishCurriculumLessonsFor(u) {
			var n int
			if err := tx.QueryRowContext(c, `SELECT COUNT(*) FROM english_curriculum_lessons WHERE unit_id=? AND lesson_code=?`, u.UnitID, l["lessonCode"]).Scan(&n); err != nil {
				c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
				return
			}
			if n > 0 {
				continue
			}
			if _, err := tx.ExecContext(c, `
				INSERT INTO english_curriculum_lessons(unit_id,lesson_code,title,skill_focus,source_ref,sort_order)
				VALUES(?,?,?,?,?,?)`,
				u.UnitID, l["lessonCode"], l["title"], l["skillFocus"], l["sourceRef"], l["sortOrder"]); err != nil {
				c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
				return
			}
		}
		if _, err := tx.ExecContext(c, `DELETE FROM english_curriculum_artifacts WHERE unit_id=? AND artifact_type='unitPack'`, u.UnitID); err != nil {
			c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "imported": len(units), "units": units})
}

func (h *EnglishCurriculumHandler) getUnit(c *gin.Context) {
	if err := h.ensureSeed(c); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	row := h.db.QueryRowContext(c, `
		SELECT unit_id,grade,book,unit_no,title,page_start,sort_order
		FROM english_curriculum_units WHERE unit_id=?`, c.Param("unitId"))
	var u model.EnglishCurriculumUnit
	if err := row.Scan(&u.UnitID, &u.Grade, &u.Book, &u.UnitNo, &u.Title, &u.PageStart, &u.SortOrder); err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, model.APIError{Error: "unit not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	h.attachProgress(c, &u)
	c.JSON(http.StatusOK, u)
}

func (h *EnglishCurriculumHandler) getArtifacts(c *gin.Context) {
	if err := h.ensureSeed(c); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	unit, err := h.loadUnit(c, c.Param("unitId"))
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, model.APIError{Error: "unit not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	if err := h.ensureArtifact(c, unit); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	rows, err := h.db.QueryContext(c, `SELECT artifact_type,content_json FROM english_curriculum_artifacts WHERE unit_id=?`, unit.UnitID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	defer rows.Close()
	out := gin.H{}
	for rows.Next() {
		var typ string
		var raw string
		if err := rows.Scan(&typ, &raw); err == nil {
			var v any
			if json.Unmarshal([]byte(raw), &v) == nil {
				out[typ] = v
			}
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *EnglishCurriculumHandler) updateProgress(c *gin.Context) {
	var req model.UpdateEnglishCurriculumProgressReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.APIError{Error: err.Error()})
		return
	}
	id := c.Param("unitId")
	if _, err := h.db.ExecContext(c, `
		INSERT INTO english_curriculum_progress(unit_id,status,reading_score,vocabulary_score,language_score,writing_score,updated_at)
		VALUES(?,?,?,?,?,?,datetime('now'))
		ON CONFLICT(unit_id) DO UPDATE SET
			status=excluded.status,
			reading_score=excluded.reading_score,
			vocabulary_score=excluded.vocabulary_score,
			language_score=excluded.language_score,
			writing_score=excluded.writing_score,
			updated_at=datetime('now')`,
		id, req.Status, req.ReadingScore, req.VocabularyScore, req.LanguageScore, req.WritingScore); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.OKResponse{OK: true})
}

func (h *EnglishCurriculumHandler) generateArtifacts(c *gin.Context) {
	if err := h.ensureSeed(c); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	grade := c.DefaultQuery("grade", "4")
	rows, err := h.db.QueryContext(c, `SELECT unit_id,grade,book,unit_no,title,page_start,sort_order FROM english_curriculum_units WHERE grade=?`, grade)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	defer rows.Close()
	units := []model.EnglishCurriculumUnit{}
	for rows.Next() {
		var u model.EnglishCurriculumUnit
		if err := rows.Scan(&u.UnitID, &u.Grade, &u.Book, &u.UnitNo, &u.Title, &u.PageStart, &u.SortOrder); err == nil {
			units = append(units, u)
		}
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
		return
	}
	rows.Close()
	for _, u := range units {
		raw, _ := json.Marshal(englishCurriculumUnitPack(u))
		if _, err := h.db.ExecContext(c, `DELETE FROM english_curriculum_artifacts WHERE unit_id=? AND artifact_type='unitPack'`, u.UnitID); err != nil {
			c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
			return
		}
		if _, err := h.db.ExecContext(c, `INSERT INTO english_curriculum_artifacts(unit_id,artifact_type,content_json) VALUES(?,?,?)`, u.UnitID, "unitPack", string(raw)); err != nil {
			c.JSON(http.StatusInternalServerError, model.APIError{Error: err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "generated": len(units), "version": englishCurriculumArtifactVersion})
}

func (h *EnglishCurriculumHandler) attachProgress(c *gin.Context, u *model.EnglishCurriculumUnit) {
	var p model.EnglishCurriculumProgress
	err := h.db.QueryRowContext(c, `
		SELECT unit_id,status,reading_score,vocabulary_score,language_score,writing_score,updated_at
		FROM english_curriculum_progress WHERE unit_id=?`, u.UnitID).
		Scan(&p.UnitID, &p.Status, &p.ReadingScore, &p.VocabularyScore, &p.LanguageScore, &p.WritingScore, &p.UpdatedAt)
	if err == nil {
		u.Progress = &p
	}
}

func (h *EnglishCurriculumHandler) loadUnit(c *gin.Context, id string) (model.EnglishCurriculumUnit, error) {
	var u model.EnglishCurriculumUnit
	err := h.db.QueryRowContext(c, `
		SELECT unit_id,grade,book,unit_no,title,page_start,sort_order
		FROM english_curriculum_units WHERE unit_id=?`, id).
		Scan(&u.UnitID, &u.Grade, &u.Book, &u.UnitNo, &u.Title, &u.PageStart, &u.SortOrder)
	return u, err
}

func (h *EnglishCurriculumHandler) ensureArtifact(c *gin.Context, u model.EnglishCurriculumUnit) error {
	var n int
	like := `%"artifactVersion":"` + englishCurriculumArtifactVersion + `"%`
	if err := h.db.QueryRowContext(c, `SELECT COUNT(*) FROM english_curriculum_artifacts WHERE unit_id=? AND artifact_type='unitPack' AND content_json LIKE ?`, u.UnitID, like).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	raw, _ := json.Marshal(englishCurriculumUnitPack(u))
	if _, err := h.db.ExecContext(c, `DELETE FROM english_curriculum_artifacts WHERE unit_id=? AND artifact_type='unitPack'`, u.UnitID); err != nil {
		return err
	}
	_, err := h.db.ExecContext(c, `INSERT INTO english_curriculum_artifacts(unit_id,artifact_type,content_json) VALUES(?,?,?)`, u.UnitID, "unitPack", string(raw))
	return err
}

func parseEnglishCurriculumImport(req model.EnglishCurriculumImportReq) ([]model.EnglishCurriculumUnit, error) {
	grade := normalizeEnglishCurriculumGrade(req.Grade)
	book := strings.TrimSpace(req.Book)
	if grade == "" || book == "" {
		return nil, fmt.Errorf("grade and book are required")
	}
	base, _ := strconv.Atoi(grade)
	units := []model.EnglishCurriculumUnit{}
	for i, raw := range strings.Split(req.Lines, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if isEnglishCurriculumImportNoise(line) {
			continue
		}
		unitNo, title, page, ok := parseEnglishCurriculumImportLine(line)
		if !ok {
			return nil, fmt.Errorf("line %d must look like: Unit 1 | Unit title | 2 or 1 Unit title 2", i+1)
		}
		sortOrder := unitNo
		if base > 0 {
			sortOrder = base*100 + unitNo
		}
		units = append(units, model.EnglishCurriculumUnit{
			UnitID:    fmt.Sprintf("G%s-U%d", grade, unitNo),
			Grade:     grade,
			Book:      book,
			UnitNo:    unitNo,
			Title:     title,
			PageStart: page,
			SortOrder: sortOrder,
		})
	}
	if len(units) == 0 {
		return nil, fmt.Errorf("no units found")
	}
	return units, nil
}

func parseEnglishCurriculumImportLine(line string) (int, string, int, bool) {
	var parts []string
	if strings.Contains(line, "|") {
		parts = strings.Split(line, "|")
	} else if strings.Contains(line, "\t") {
		parts = strings.Split(line, "\t")
	}
	if len(parts) >= 2 {
		unitNo, ok := parseEnglishUnitNo(parts[0])
		title := cleanEnglishCurriculumTitle(parts[1])
		page := 0
		if len(parts) >= 3 {
			page, _ = strconv.Atoi(strings.TrimSpace(parts[2]))
		}
		return unitNo, title, page, ok && title != ""
	}
	words := strings.Fields(line)
	if len(words) < 2 {
		return 0, "", 0, false
	}
	start := 1
	unitNo, ok := parseEnglishUnitNo(words[0])
	if strings.EqualFold(words[0], "unit") {
		if len(words) < 3 {
			return 0, "", 0, false
		}
		unitNo, ok = parseEnglishUnitNo(words[1])
		start = 2
	}
	end := len(words)
	page := 0
	if p, err := strconv.Atoi(words[end-1]); err == nil {
		page = p
		end--
	}
	if start >= end {
		return 0, "", 0, false
	}
	title := cleanEnglishCurriculumTitle(strings.Join(words[start:end], " "))
	return unitNo, title, page, ok && title != ""
}

func parseEnglishUnitNo(s string) (int, bool) {
	v := strings.ToLower(strings.TrimSpace(s))
	v = strings.TrimPrefix(v, "unit")
	v = strings.Trim(v, " .:-")
	n, err := strconv.Atoi(v)
	return n, err == nil && n > 0
}

func cleanEnglishCurriculumTitle(s string) string {
	words := strings.Fields(s)
	out := words[:0]
	for _, w := range words {
		if strings.Trim(w, ".·…") == "" {
			continue
		}
		out = append(out, w)
	}
	return strings.TrimSpace(strings.Join(out, " "))
}

func isEnglishCurriculumImportNoise(line string) bool {
	v := strings.ToLower(strings.TrimSpace(line))
	switch v {
	case "contents", "content", "review", "revision":
		return true
	}
	return strings.HasPrefix(v, "review ") || strings.HasPrefix(v, "revision ")
}

func normalizeEnglishCurriculumGrade(s string) string {
	v := strings.ToUpper(strings.TrimSpace(s))
	v = strings.TrimPrefix(v, "GRADE")
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "G")
	v = strings.TrimPrefix(v, "P")
	return strings.TrimSpace(v)
}

func englishCurriculumSeedUnits() []model.EnglishCurriculumUnit {
	return []model.EnglishCurriculumUnit{
		{UnitID: "G3-S1", Grade: "3", Book: "G3 Support", UnitNo: 1, Title: "Literal Retell and Full Sentences", PageStart: 0, SortOrder: 301},
		{UnitID: "G3-S2", Grade: "3", Book: "G3 Support", UnitNo: 2, Title: "Evidence Starter Questions", PageStart: 0, SortOrder: 302},
		{UnitID: "G3-S3", Grade: "3", Book: "G3 Support", UnitNo: 3, Title: "Vocabulary in Context Repair", PageStart: 0, SortOrder: 303},
		{UnitID: "G3-S4", Grade: "3", Book: "G3 Support", UnitNo: 4, Title: "Paragraph Basics", PageStart: 0, SortOrder: 304},
		{UnitID: "G4-U1", Grade: "4", Book: "4A", UnitNo: 1, Title: "Fantastic Animals", PageStart: 2, SortOrder: 401},
		{UnitID: "G4-U2", Grade: "4", Book: "4A", UnitNo: 2, Title: "I Am Special", PageStart: 20, SortOrder: 402},
		{UnitID: "G4-U3", Grade: "4", Book: "4A", UnitNo: 3, Title: "Fossils and Dinosaurs", PageStart: 38, SortOrder: 403},
		{UnitID: "G4-U4", Grade: "4", Book: "4A", UnitNo: 4, Title: "Poems Tell Stories Too", PageStart: 56, SortOrder: 404},
		{UnitID: "G4-U5", Grade: "4", Book: "4A", UnitNo: 5, Title: "Let's Experiment!", PageStart: 74, SortOrder: 405},
		{UnitID: "G4-U6", Grade: "4", Book: "4A", UnitNo: 6, Title: "Our Trip to Thailand", PageStart: 92, SortOrder: 406},
		{UnitID: "G4-U7", Grade: "4", Book: "4B", UnitNo: 7, Title: "A Bag of Laughs", PageStart: 2, SortOrder: 407},
		{UnitID: "G4-U8", Grade: "4", Book: "4B", UnitNo: 8, Title: "James and the Giant Peach", PageStart: 20, SortOrder: 408},
		{UnitID: "G4-U9", Grade: "4", Book: "4B", UnitNo: 9, Title: "What Do You Think of It?", PageStart: 38, SortOrder: 409},
		{UnitID: "G4-U10", Grade: "4", Book: "4B", UnitNo: 10, Title: "Wonders of Science", PageStart: 56, SortOrder: 410},
		{UnitID: "G4-U11", Grade: "4", Book: "4B", UnitNo: 11, Title: "Great Olympians", PageStart: 74, SortOrder: 411},
		{UnitID: "G5-P1", Grade: "5", Book: "G5 Preview", UnitNo: 1, Title: "Longer Inference with Evidence", PageStart: 0, SortOrder: 501},
		{UnitID: "G5-P2", Grade: "5", Book: "G5 Preview", UnitNo: 2, Title: "Multi-Paragraph Transition", PageStart: 0, SortOrder: 502},
	}
}

func englishCurriculumResourceCoverage() []gin.H {
	return []gin.H{
		englishResourceBook("1", "1A", "新加坡1A", "新加坡1A contents.pdf", "Units 1-8", "mostly available", []string{"Unit 8B"}),
		englishResourceBook("1", "1B", "新加坡1B", "新加坡1B contents.pdf", "Units 9-16", "complete", nil),
		englishResourceBook("2", "2A", "新加坡2A", "新加坡2A contents.pdf", "Units 1-8", "complete", nil),
		englishResourceBook("2", "2B", "新加坡2B", "新加坡2B content.pdf", "Units 9-16", "mostly available", []string{"Unit 9B", "Unit 11C"}),
		englishResourceBook("3", "3A", "新加坡3A", "新加坡3A contents.pdf", "Units 1-7", "mostly available", []string{"Unit 5A"}),
		englishResourceBook("3", "3B", "新加坡3B", "新加坡3B content.pdf", "Units 8-14", "mostly available", []string{"Unit 11D", "Unit 12A", "Unit 12D"}),
		englishResourceBook("4", "4A", "新加坡4A", "新加坡4A content.pdf", "Units 1-6", "mostly available", []string{"Unit 1B"}),
		englishResourceBook("4", "4B", "新加坡4B", "新加坡4B content.pdf", "Units 7-11", "complete", nil),
		englishResourceBook("5", "5A/5B", "Grade 5", "", "not available", "not available", []string{"Grade 5 folder not found"}),
		englishResourceBook("6", "6A", "6A", "新加坡小学6A contents  .pdf", "Units 1-6", "complete", nil),
		englishResourceBook("6", "6B", "6B", "新加坡6B content.pdf", "Units 7-12", "complete", nil),
	}
}

func englishCurriculumTeachingRoadmap() []gin.H {
	return []gin.H{
		{
			"phase":       "Current Grade 4 spine",
			"timeframe":   "now",
			"sourceBooks": []string{"4A", "4B"},
			"goal":        "Use the Grade 4 My Pals units as the main weekly path: read with evidence, learn academic words, notice one language pattern, then speak or write.",
			"why":         "This matches her current school level and turns textbook units into a repeatable home routine.",
			"focusSkills": []string{"evidence answers", "academic vocabulary in context", "language pattern transfer", "short structured writing"},
			"nextAction":  "Keep Grade 4 as the default path; use checkpoint scores to choose one weak skill each week.",
		},
		{
			"phase":       "Grade 3 prerequisite repair",
			"timeframe":   "short catch-up blocks",
			"sourceBooks": []string{"3A", "3B"},
			"goal":        "Use Grade 3 resources only when a Grade 4 task shows a gap in literal comprehension, sentence completeness, basic text type, or vocabulary use.",
			"why":         "She should not redo a whole lower grade; she needs targeted repair when Grade 4 output breaks down.",
			"focusSkills": []string{"literal retell", "full sentence answers", "basic sequencing", "simple paragraph structure"},
			"nextAction":  "When a Grade 4 checkpoint is below 70, pick one matching Grade 3 unit as a 2-3 day support block.",
		},
		{
			"phase":       "Grade 5 transition",
			"timeframe":   "after manual contents import",
			"sourceBooks": []string{"5A", "5B"},
			"goal":        "Seed Grade 5 from a contents page first, then use generated unit packs as a transition path after Grade 4 foundations are stable.",
			"why":         "Planning should stay calibrated to real unit titles. Manual contents import is enough for scope; full PDFs can refine later.",
			"focusSkills": []string{"future inference", "longer writing", "higher academic vocabulary"},
			"nextAction":  "Paste Grade 5 contents into Resources → Import contents manually when you are ready to preview the next year.",
		},
		{
			"phase":       "Grade 6 reference only",
			"timeframe":   "later",
			"sourceBooks": []string{"6A", "6B"},
			"goal":        "Keep Grade 6 as an upper-level reference for future academic reading and writing expectations.",
			"why":         "It is useful for planning direction, but too far ahead for daily work right now.",
			"focusSkills": []string{"synthesis", "more independent writing", "presentation confidence"},
			"nextAction":  "Use only for parent planning until Grade 4 and Grade 5 foundations are stable.",
		},
	}
}

func englishCurriculumRepairBlocks() []gin.H {
	return []gin.H{
		{
			"weakArea":      "Reading evidence",
			"trigger":       "Reading score below 70, or answers sound like guesses.",
			"supportSource": "Grade 3 support style: literal retell before inference.",
			"duration":      "2-3 short sessions",
			"studentSteps":  []string{"Retell what happened in order.", "Answer one why/how question.", "Point to the sentence that gives the clue.", "Say: I think ___ because the text says ___."},
			"successSignal": "She can point to a clue before explaining the answer.",
		},
		{
			"weakArea":      "Vocabulary use",
			"trigger":       "She recognises a word but cannot use it in her own sentence.",
			"supportSource": "Current unit words plus one easier familiar topic.",
			"duration":      "2 sessions",
			"studentSteps":  []string{"Say the word.", "Explain it in simple words.", "Choose one example and one non-example.", "Write a unit sentence that proves the meaning."},
			"successSignal": "The sentence shows the meaning without copying the definition.",
		},
		{
			"weakArea":      "Language pattern transfer",
			"trigger":       "She can copy the model sentence but breaks the pattern in a new sentence.",
			"supportSource": "One model sentence from the current unit.",
			"duration":      "2 sessions",
			"studentSteps":  []string{"Underline the pattern in the model.", "Change only the noun first.", "Change the verb or connector next.", "Compare the new sentence with the model."},
			"successSignal": "She can make two new sentences with the same pattern.",
		},
		{
			"weakArea":      "Writing structure",
			"trigger":       "Writing has ideas, but they are listed or not organised.",
			"supportSource": "Grade 3 paragraph repair before Grade 4 genre writing.",
			"duration":      "3 sessions",
			"studentSteps":  []string{"Say the main idea aloud.", "Add one detail.", "Add one evidence or example.", "Write 4-6 sentences.", "Fix only one focus: evidence, tense, or sentence completeness."},
			"successSignal": "The paragraph has one main idea and every sentence supports it.",
		},
	}
}

func englishCurriculumSkillSpine() []gin.H {
	return []gin.H{
		englishSkillSpineRow("Reading", "Retell literal events in order.", "Answer why/how with evidence from the text.", "Compare texts, infer themes, and explain author's purpose.", "Keeps answers from becoming guesses; every reading task should end with a clue."),
		englishSkillSpineRow("Vocabulary", "Explain common words and use them in complete sentences.", "Use academic and topic words in context.", "Choose precise words for audience, genre, and tone.", "Vocabulary is mastered only when she can use the word naturally."),
		englishSkillSpineRow("Language", "Write complete sentences with stable tense and basic connectors.", "Transfer one grammar pattern from the unit into new sentences.", "Control clauses, voice, reported speech, and cohesive devices.", "Grammar should be practised inside real reading/writing, not as isolated rules first."),
		englishSkillSpineRow("Writing", "Write a short paragraph with one clear idea.", "Write genre-specific responses: report, diary, explanation, poem, review, news.", "Plan multi-paragraph writing with evidence, voice, and revision.", "The home target is structure and clarity, not correcting every small error."),
		englishSkillSpineRow("Speaking", "Answer in full sentences and retell simply.", "Present or explain a unit idea with one example or evidence.", "Discuss, justify, and respond to questions independently.", "Speaking before writing reduces blank-page pressure and shows whether she understands."),
	}
}

func englishSkillSpineRow(skill, grade3Support, grade4Now, grade5Plus, parentUse string) gin.H {
	return gin.H{"skill": skill, "grade3Support": grade3Support, "grade4Now": grade4Now, "grade5Plus": grade5Plus, "parentUse": parentUse}
}

func englishCurriculumDiagnosticBridge() []gin.H {
	return []gin.H{
		englishDiagnosticBridgeRow("Listening / Speaking", "Can understand videos, but school explanations are short or missing reasons.", "G4-U5, G4-U6, G4-U10", "Use oral explanation: first, next, because, so. Parent asks: Can you add one reason?"),
		englishDiagnosticBridgeRow("Reading Evidence", "Answers why/how from feeling or memory instead of pointing to a clue.", "G4-U3, G4-U8, G4-U10, G3-S2", "Read once for gist, once for clue. Answer: I think ___ because the text says ___."),
		englishDiagnosticBridgeRow("Vocabulary Use", "Knows the word when seen, but cannot explain or use it naturally.", "G4-U1, G4-U3, G4-U11, G3-S3", "Say it, explain it simply, give one example and one non-example, then write a unit sentence."),
		englishDiagnosticBridgeRow("Grammar in Context", "Can copy model sentences, but tense/articles/connectors break in new writing.", "G4-U2, G4-U5, G4-U10", "Practise one target pattern from the unit. Fix only that pattern before moving on."),
		englishDiagnosticBridgeRow("Writing Structure", "Ideas are present, but writing is a list or lacks a clear main idea.", "G4-U4, G4-U8, G4-U9, G3-S4", "Say main idea aloud, add two details, then write 4-6 connected sentences."),
	}
}

func englishDiagnosticBridgeRow(area, diagnosticSignal, curriculumPath, weeklyMove string) gin.H {
	return gin.H{"area": area, "diagnosticSignal": diagnosticSignal, "curriculumPath": curriculumPath, "weeklyMove": weeklyMove}
}

func englishCurriculumHomeLoop() []gin.H {
	return []gin.H{
		{"step": "1. Read", "time": "8 min", "student": "Read a short unit section or summary twice: gist first, clue second.", "parent": "Ask one question only: Which words tell you that?"},
		{"step": "2. Say", "time": "4 min", "student": "Answer aloud in a full sentence with because / the text says.", "parent": "Do not translate the whole text. Push for full sentence + evidence."},
		{"step": "3. Notice", "time": "5 min", "student": "Pick one unit word or language pattern and make a new sentence.", "parent": "Compare her sentence with the model; fix only that pattern."},
		{"step": "4. Write", "time": "8 min", "student": "Write 4-6 sentences using the unit scaffold.", "parent": "Choose one revision focus: evidence, vocabulary, tense, or sentence completeness."},
		{"step": "5. Score", "time": "3 min", "student": "Save Reading / Vocabulary / Language / Writing scores.", "parent": "Next session starts from the lowest score, not from a random new task."},
	}
}

func englishCurriculumSourceAccess() gin.H {
	path := os.Getenv("MATH_TRAINER_ENGLISH_SOURCE_DIR")
	projectPath := "resources/english"
	status := "not checked"
	detail := "The app does not need direct PDF access for the current Grade 4 generated path."
	if _, err := os.Stat(path); err == nil {
		status = "available"
		detail = "Local English PDF folder is visible to the server process."
	} else if os.IsPermission(err) {
		status = "permission blocked"
		detail = "macOS denied access to the Downloads baidu-vm-share folder. Grant terminal/app Full Disk Access or copy selected contents pages into the project when deeper calibration is needed."
	} else if os.IsNotExist(err) {
		status = "not found"
		detail = "Local English PDF folder was not found at the expected path."
	} else if err != nil {
		status = "error"
		detail = err.Error()
	}
	projectStatus := "not found"
	projectDetail := "Copy selected contents pages or PDFs into resources/english when Downloads is blocked."
	if info, err := os.Stat(projectPath); err == nil && info.IsDir() {
		projectStatus = "available"
		projectDetail = "Project-local source drop is available to this app."
	} else if err != nil && !os.IsNotExist(err) {
		projectStatus = "error"
		projectDetail = err.Error()
	}
	return gin.H{"path": path, "status": status, "detail": detail, "projectPath": projectPath, "projectStatus": projectStatus, "projectDetail": projectDetail}
}

func englishResourceBook(grade, book, localFolder, contentsFile, unitRange, status string, missingParts []string) gin.H {
	readyForPlanning := status == "complete" || status == "mostly available"
	nextUse := "Use contents first, then build original unit lessons from objectives and text-type patterns."
	if !readyForPlanning {
		nextUse = "Needs local source files before this grade can be planned from My Pals."
	}
	return gin.H{
		"grade":            grade,
		"book":             book,
		"localFolder":      localFolder,
		"contentsFile":     contentsFile,
		"unitRange":        unitRange,
		"status":           status,
		"missingParts":     missingParts,
		"readyForPlanning": readyForPlanning,
		"nextUse":          nextUse,
	}
}

func englishCurriculumLessonsFor(u model.EnglishCurriculumUnit) []map[string]string {
	base := []string{"Reading", "Vocabulary", "Language Focus", "Writing and Speaking"}
	out := []map[string]string{}
	for i, title := range base {
		out = append(out, map[string]string{
			"lessonCode": "L" + strconv.Itoa(i+1),
			"title":      title,
			"skillFocus": title,
			"sourceRef":  u.Book + " Unit " + strconv.Itoa(u.UnitNo),
			"sortOrder":  strconv.Itoa(i + 1),
		})
	}
	return out
}

func englishSourceObjectivesFor(u model.EnglishCurriculumUnit, profile map[string]string) []string {
	switch u.UnitID {
	case "G4-U1":
		return []string{"read animal information reports", "read for information", "notice how information reports are organised", "write an information report", "present and listen to information reports"}
	case "G4-U2":
		return []string{"read diary excerpts", "learn about children with special strengths", "see events from another person's point of view", "learn how diary writing works", "write a diary entry", "listen to and present oral recounts"}
	case "G4-U3":
		return []string{"read how fossils are formed", "explain how fossils give clues about extinct dinosaurs", "notice how explanation texts are written", "write an explanation text", "present and listen to oral explanations"}
	case "G4-U4":
		return []string{"read narrative poems", "notice how narrative poems are written", "recite narrative poems", "listen to narrative poems", "write an original narrative poem"}
	case "G4-U5":
		return []string{"read instructions for science experiments", "explain how instructions help experiments work", "notice how experiment instructions are written", "write experiment instructions", "give instructions to others"}
	case "G4-U6":
		return []string{"read about interesting places to visit", "read informal correspondence such as postcards and email", "explain why people send postcards and email", "write postcards and email", "listen to telephone conversations", "make enquiries over the telephone"}
	case "G4-U7":
		return []string{"read funny poems and jokes", "explain how humour amuses readers", "play with language", "write funny poems"}
	case "G4-U8":
		return []string{"read extracts from a longer narrative", "role-play an extract from a narrative", "appreciate a performance based on a narrative", "notice how narratives are written", "write narratives", "read novels for enjoyment"}
	case "G4-U9":
		return []string{"read book reviews", "identify where book reviews can be found", "notice how book reviews are written", "write a book review", "present and listen to reviews"}
	case "G4-U10":
		return []string{"read science explanations", "learn how to answer why questions", "write an explanation text", "present an oral explanation", "listen to oral explanations"}
	case "G4-U11":
		return []string{"read newspaper reports about Olympic athletes", "see how newspaper reports give information", "write a newspaper report", "read a news report", "listen to news reports"}
	default:
		return []string{
			"read Grade " + u.Grade + " Unit " + strconv.Itoa(u.UnitNo) + " (" + u.Title + ") for main idea and evidence",
			"build unit vocabulary: " + profile["academicVocabulary"],
			"notice and transfer the language pattern: " + profile["languageFocus"],
			"complete the unit output: " + profile["writingTask"],
		}
	}
}

func englishSourcePDFRefs(u model.EnglishCurriculumUnit) []gin.H {
	prefix := "新加坡" + u.Book + " unit" + strconv.Itoa(u.UnitNo)
	roles := map[string]string{
		"A": "unit opener and learning objectives",
		"B": "unit development",
		"C": "unit development",
		"D": "unit practice or extension",
	}
	available := englishSourcePDFPartsAvailable(u.UnitID)
	refs := make([]gin.H, 0, 4)
	for _, part := range []string{"A", "B", "C", "D"} {
		ok := available[part]
		status := "available in local Grade " + u.Grade + " PDF folder"
		if !ok {
			status = "not verified in local Grade " + u.Grade + " PDF folder"
			if strings.HasPrefix(u.UnitID, "G4-") {
				status = "not found in local Grade 4 PDF folder"
			}
		}
		refs = append(refs, gin.H{
			"part":      part,
			"file":      prefix + part + ".pdf",
			"role":      roles[part],
			"available": ok,
			"status":    status,
		})
	}
	return refs
}

func englishSourcePDFPartsAvailable(unitID string) map[string]bool {
	parts := map[string]bool{"A": false, "B": false, "C": false, "D": false}
	if !strings.HasPrefix(unitID, "G4-") {
		return parts
	}
	parts = map[string]bool{"A": true, "B": true, "C": true, "D": true}
	// The local Grade 4 scan set has Unit 1A/1C/1D, but Unit 1B is absent.
	// Keep this as metadata instead of reading /Users/... at runtime.
	if unitID == "G4-U1" {
		parts["B"] = false
	}
	return parts
}

func englishSourceLessonCards(u model.EnglishCurriculumUnit, profile map[string]string) []gin.H {
	prefix := "Unit " + strconv.Itoa(u.UnitNo)
	specific := englishSpecificLessonCards(u.UnitID)
	if len(specific) == 4 {
		return englishLessonCardsWithSourceAvailability(u.UnitID, specific)
	}
	return englishLessonCardsWithSourceAvailability(u.UnitID, []gin.H{
		englishLessonCard("A", prefix+"A", "Preview: "+u.Title, "Use the title and topic to predict the text type, audience and purpose.", "Turn the imported contents title into a real reading purpose before opening the text.", []string{"What kind of text might " + u.Title + " be?", "What clue in the title gives that idea?", "Which Grade " + u.Grade + " skill might this unit practise?"}),
		englishLessonCard("B", prefix+"B", "Reading: "+u.Title, "Read the unit text and practise: "+profile["readingSkill"]+".", "Move from gist to evidence: read once for meaning, then point to the clue.", []string{"What is " + u.Title + " mostly about?", "Which sentence gives evidence?", "Can you answer in a full sentence?"}),
		englishLessonCard("C", prefix+"C", "Vocabulary and language: "+u.Title, "Use unit vocabulary ("+profile["academicVocabulary"]+") and the pattern: "+profile["languageFocus"]+".", "Learn the language pattern in context before trying a new sentence.", []string{"Which words are useful for this topic?", "What grammar pattern repeats?", "Can you write one new sentence about " + u.Title + "?"}),
		englishLessonCard("D", prefix+"D", "Output: "+u.Title, "Use the reading model to complete: "+profile["writingTask"]+".", "Transfer the skill into her own output, then check against the success criteria.", []string{"What is the writing structure?", "Did you include evidence or detail?", "Can you explain " + u.Title + " aloud?"}),
	})
}

func englishLessonCardsWithSourceAvailability(unitID string, cards []gin.H) []gin.H {
	available := englishSourcePDFPartsAvailable(unitID)
	for _, card := range cards {
		part, _ := card["part"].(string)
		ok := available[part]
		status := "local PDF available"
		if !ok {
			status = "local PDF missing; use this as an original practice plan, not as a textbook locator"
		}
		card["sourceAvailable"] = ok
		card["sourceStatus"] = status
	}
	return cards
}

func englishSpecificLessonCards(unitID string) []gin.H {
	switch unitID {
	case "G4-U1":
		return []gin.H{
			englishLessonCard("A", "Unit 1A", "Unit opener: fantastic animals", "Preview animal information reports and ask what makes an animal unusual, useful or well adapted.", "Turn curiosity into a reading purpose: features should connect to survival or behaviour.", []string{"What animal feature do you notice first?", "What might that feature help the animal do?", "What kind of report will this unit build?"}),
			englishLessonCard("B", "Unit 1B", "Reading: animal information report", "Read an animal report for facts, key details and feature-purpose links.", "Teach her that information reports are not stories; they organise facts clearly.", []string{"Which detail is a fact?", "How does this feature help the animal?", "Can you compare two animals with evidence?"}),
			englishLessonCard("C", "Unit 1C", "Language: describing and comparing animals", "Use precise adjectives, comparative language and topic vocabulary to describe animal features.", "Move from cute/interesting to exact academic description.", []string{"Is the adjective precise enough?", "Are you comparing size, speed, body part or behaviour?", "Can you replace nice with a stronger word?"}),
			englishLessonCard("D", "Unit 1D", "Writing: animal report", "Plan, write and present a short information report with feature, purpose and evidence.", "Transfer reading structure into her own report.", []string{"Does each paragraph have one clear idea?", "Did you explain why the feature matters?", "Can you present it without reading every word?"}),
		}
	case "G4-U2":
		return []gin.H{
			englishLessonCard("A", "Unit 2A", "Unit opener: I am special", "Preview diary writing and personal reflection about strengths, feelings and identity.", "Set up writing that needs real examples, not only labels like good or smart.", []string{"What makes someone special?", "Which feeling words might appear in a diary?", "What would make the entry believable?"}),
			englishLessonCard("B", "Unit 2B", "Reading: diary excerpts", "Read diary entries for point of view, feelings, sequence and clues about character.", "Diary reading asks: what happened, how did the writer feel, and what clue proves it?", []string{"Who is speaking?", "What feeling can you infer?", "Which sentence gives the clue?"}),
			englishLessonCard("C", "Unit 2C", "Language: diary voice", "Notice first-person voice, time markers, feeling words and simple past/present choices in diary writing.", "Connect grammar to voice: a diary should sound like a real person thinking back.", []string{"Does it sound like I/me/my?", "Are the events in order?", "Did the tense stay steady?"}),
			englishLessonCard("D", "Unit 2D", "Writing: diary entry", "Write a diary entry with event, feeling, reason and reflection.", "Build confidence in structured personal writing.", []string{"What happened first?", "How did you feel and why?", "What did you learn or decide?"}),
		}
	case "G4-U3":
		return []gin.H{
			englishLessonCard("A", "Unit 3A", "Unit opener: fossils and dinosaurs", "Preview fossil evidence and questions about extinct dinosaurs.", "Frame the unit as science reading: evidence helps us infer the past.", []string{"What can a fossil show?", "What can it not show directly?", "What does extinct mean?"}),
			englishLessonCard("B", "Unit 3B", "Reading: fossil explanation", "Read how fossils form and what scientists can infer from them.", "Teach cause, process and evidence instead of memorising dinosaur names.", []string{"What happened first in the process?", "Which clue supports the inference?", "Is this a fact or a guess from evidence?"}),
			englishLessonCard("C", "Unit 3C", "Language: time and evidence", "Use past tense, sequence words and evidence language to explain ancient events.", "Good fossil explanations need when, how and how we know.", []string{"Which words show time order?", "Is the verb in past tense?", "Where is the evidence phrase?"}),
			englishLessonCard("D", "Unit 3D", "Writing: science explanation", "Write a short explanation of fossil formation or what a fossil can tell us.", "Practise explaining a process clearly from start to finish.", []string{"Did you include first/next/finally?", "Did you explain why scientists know?", "Can someone draw the process from your writing?"}),
		}
	case "G4-U4":
		return []gin.H{
			englishLessonCard("A", "Unit 4A", "Unit opener: narrative poems", "Preview poems that tell stories and notice speaker, event and feeling.", "Show that poems can carry plot, not only rhyme.", []string{"Who might be speaking?", "What story could the poem tell?", "What mood do you expect?"}),
			englishLessonCard("B", "Unit 4B", "Reading: poem as story", "Read narrative poems for sequence, voice, image and mood.", "Help her track what happens while still hearing rhythm and imagery.", []string{"What happened first?", "Which line creates a picture?", "How does the speaker feel?"}),
			englishLessonCard("C", "Unit 4C", "Language: poetic choices", "Notice strong verbs, descriptive phrases, rhythm and line breaks.", "Poetry meaning often comes from small word choices.", []string{"Which verb is strongest?", "Where does the line break make you pause?", "Does the rhythm match the feeling?"}),
			englishLessonCard("D", "Unit 4D", "Writing and reciting: narrative poem", "Write and recite a short narrative poem with event, image and feeling.", "Make speaking part of writing: the poem should make sense aloud.", []string{"Does the poem have a tiny story?", "Can you read it with expression?", "What image will the listener remember?"}),
		}
	case "G4-U5":
		return []gin.H{
			englishLessonCard("A", "Unit 5A", "Unit opener: experiments", "Preview experiment instructions and why clear steps matter.", "Connect language accuracy to real-world procedure: unclear instructions change results.", []string{"What could go wrong if a step is missing?", "What materials are needed?", "What result might you observe?"}),
			englishLessonCard("B", "Unit 5B", "Reading: experiment instructions", "Read instructions for sequence, materials, safety and expected result.", "Treat instructions as a tool for doing, not just reading.", []string{"What do we need before Step 1?", "Which step must happen first?", "What should we observe?"}),
			englishLessonCard("C", "Unit 5C", "Language: imperatives and sequence", "Use command verbs, numbered steps and sequence words in instructions.", "Good instructions are short, exact and ordered.", []string{"Does the sentence start with an action verb?", "Is the order clear?", "Could someone follow this without asking you?"}),
			englishLessonCard("D", "Unit 5D", "Writing and speaking: experiment procedure", "Write and give experiment instructions with materials, steps and result.", "Build procedural speaking: clear voice, exact verbs, no skipped step.", []string{"Did you list materials?", "Are the steps numbered?", "Can you say the instruction clearly?"}),
		}
	case "G4-U6":
		return []gin.H{
			englishLessonCard("A", "Unit 6A", "Unit opener: trip to Thailand", "Preview travel writing, postcards, email and telephone enquiries.", "Show that travel English changes with purpose: share, ask, describe or plan.", []string{"Who is the message for?", "Is the purpose to tell or to ask?", "What detail makes the place real?"}),
			englishLessonCard("B", "Unit 6B", "Reading: places and informal correspondence", "Read travel texts, postcards or email for place details, sequence and purpose.", "Travel reading needs where, what happened, and why it mattered.", []string{"Where did they go?", "What detail helps you imagine the place?", "Is this message formal or informal?"}),
			englishLessonCard("C", "Unit 6C", "Language: postcards and email", "Notice greetings, closings, past-tense recount and friendly detail in postcards/email.", "Make writing sound natural without losing structure.", []string{"Does the opening fit the reader?", "Are events in order?", "Did you add one specific place detail?"}),
			englishLessonCard("D", "Unit 6D", "Writing and speaking: postcard, email and enquiry", "Write a travel postcard or email and practise a simple telephone enquiry.", "Combine written recount with practical spoken English.", []string{"What question do you need to ask?", "Did you include date/place/detail?", "Can you speak politely on the phone?"}),
		}
	case "G4-U7":
		return []gin.H{
			englishLessonCard("A", "Unit 7A", "Unit opener: funny poems and jokes", "Preview humour texts and ask how language creates surprise.", "Humour reading is inference practice: the funny part is often unstated.", []string{"What did you expect?", "What surprised you?", "Which word makes it funny?"}),
			englishLessonCard("B", "Unit 7B", "Reading: jokes and funny poems", "Read jokes and funny poems for setup, surprise, rhythm and implied meaning.", "Help her explain humour instead of only saying it is funny.", []string{"What is the setup?", "Where is the twist?", "What does the reader have to infer?"}),
			englishLessonCard("C", "Unit 7C", "Language: wordplay", "Notice rhyme, double meaning, dialogue and expressive verbs in humour.", "Wordplay strengthens vocabulary flexibility.", []string{"Does the word have two meanings?", "Is the rhyme helping the joke?", "Who is speaking?"}),
			englishLessonCard("D", "Unit 7D", "Writing: funny poem or joke", "Write a short funny poem or joke with setup and surprise.", "Keep the task small: one clear setup, one clean twist.", []string{"What should the reader expect?", "What changes at the end?", "Can you read it with timing?"}),
		}
	case "G4-U8":
		return []gin.H{
			englishLessonCard("A", "Unit 8A", "Unit opener: James and the Giant Peach", "Preview a longer narrative extract and fantasy setting.", "Use a known literary world to practise deeper character and event inference.", []string{"What makes the setting unusual?", "What problem might the character face?", "What kind of narrative is this?"}),
			englishLessonCard("B", "Unit 8B", "Reading: narrative extract", "Read a longer narrative for setting, character, problem, action and evidence.", "Longer texts need tracking: who, where, problem, change.", []string{"What changed from the beginning?", "What does the character want?", "Which action shows the character trait?"}),
			englishLessonCard("C", "Unit 8C", "Language: narrative craft and performance", "Notice dialogue, action verbs, descriptive detail and role-play choices.", "Performance reveals whether she understands character voice.", []string{"How would the character say this line?", "Which verb shows action clearly?", "What detail helps the scene feel real?"}),
			englishLessonCard("D", "Unit 8D", "Writing and reading: narrative scene", "Write a short narrative scene with setting, character action, dialogue and problem, then connect it to reading novels for enjoyment.", "Build from reading response into her own story craft.", []string{"Did the scene have a problem?", "Did dialogue fit the character?", "Can you explain why the character acted that way?"}),
		}
	case "G4-U9":
		return []gin.H{
			englishLessonCard("A", "Unit 9A", "Unit opener: book reviews", "Preview reviews and how readers share opinions with reasons.", "Make opinion writing evidence-based, not just I like it.", []string{"What can a review help a reader decide?", "What opinion words do you expect?", "What evidence would be useful?"}),
			englishLessonCard("B", "Unit 9B", "Reading: reviews", "Read book reviews for summary, opinion, reasons and recommendation.", "A review balances what the book is about with what the reader thinks.", []string{"What is summary and what is opinion?", "Which reason supports the opinion?", "Would the review help you choose the book?"}),
			englishLessonCard("C", "Unit 9C", "Language: opinion and evaluation", "Use opinion phrases, because, examples and evaluative adjectives accurately.", "Teach her to upgrade from good/bad to clear reasons.", []string{"Is this a fact or an opinion?", "Did you give a because reason?", "Can you choose a more precise adjective?"}),
			englishLessonCard("D", "Unit 9D", "Writing and presenting: book review", "Write and present a short book review with summary, opinion, reason and recommendation.", "Practise academic speaking through a familiar reading task.", []string{"Did you avoid giving away too much?", "Is your recommendation clear?", "Can you answer a listener's question?"}),
		}
	case "G4-U10":
		return []gin.H{
			englishLessonCard("A", "Unit 10A", "Unit opener: science explanations", "Preview why questions and the unit goals: read science explanations, answer why, write and present an explanation.", "Frame science reading as curiosity plus evidence.", []string{"What why question does the page ask?", "What does an explanation text try to do?", "Which goal needs speaking practice?"}),
			englishLessonCard("B", "Unit 10B", "Reading: volcano explanation", "Read a science explanation with a labelled diagram and identify cause, process and result.", "Teach her to use diagrams as evidence, not decoration.", []string{"What causes the volcano to erupt?", "What words name the process?", "Which diagram label helps your answer?"}),
			englishLessonCard("C", "Unit 10C", "Language: the", "Learn when to use the: particular nouns, unique nouns, superlatives, and when not to use it for general plural or uncountable nouns.", "This fixes article accuracy inside real science sentences.", []string{"Is this noun specific or general?", "Is it unique, like the moon?", "Does the adjective end in -est?"}),
			englishLessonCard("D", "Unit 10D", "Text type: explanation craft", "Notice how explanation texts use nouns, noun phrases, verbs and simple present tense to explain processes.", "Connect grammar to writing: science explanations need precise nouns and process verbs.", []string{"Which nouns name the things in the process?", "Which verbs show actions or changes?", "Is the tense simple present?"}),
		}
	case "G4-U11":
		return []gin.H{
			englishLessonCard("A", "Unit 11A", "Unit opener: great Olympians", "Preview newspaper reports about Olympic athletes and achievement.", "Connect sports interest to factual reporting, chronology and character inference.", []string{"What achievement is newsworthy?", "What facts would a reader need?", "What trait might the athlete show?"}),
			englishLessonCard("B", "Unit 11B", "Reading: newspaper report", "Read a newspaper report for headline, key facts, sequence and evidence.", "News reading asks who, what, where, when, why and how.", []string{"What is the main event?", "Which facts answer the 5W1H questions?", "What evidence shows effort or determination?"}),
			englishLessonCard("C", "Unit 11C", "Language: report facts and quotes", "Notice past tense, time phrases, precise nouns and reported speech in news writing.", "A newspaper report should sound factual and specific.", []string{"Is this sentence fact or opinion?", "Does the time phrase help order events?", "Could a quote make the report stronger?"}),
			englishLessonCard("D", "Unit 11D", "Writing and listening: newspaper report", "Write, read and listen to a short newspaper report about an athlete or event.", "Practise concise factual writing plus oral reporting.", []string{"Does the first sentence tell the main event?", "Did you include enough facts?", "Can a listener retell the report after hearing it?"}),
		}
	}
	return nil
}

func englishLessonCard(part, code, title, objective, purpose string, prompts []string) gin.H {
	return gin.H{"part": part, "code": code, "title": title, "objective": objective, "purpose": purpose, "coachPrompts": prompts}
}

func englishWeeklyPlan(u model.EnglishCurriculumUnit, profile map[string]string, deep gin.H) []gin.H {
	title := u.Title
	reading := profile["readingSkill"]
	language := profile["languageFocus"]
	speaking := profile["speakingTask"]
	writing := profile["writingTask"]
	vocab := profile["academicVocabulary"]
	if vocab == "" {
		vocab = "unit words"
	}
	return []gin.H{
		{
			"day":          "Day 1",
			"focus":        "Preview " + title,
			"studentTask":  "Use the title \"" + title + "\" to predict the text type, audience and purpose, then say four useful words from this unit: " + vocab + ".",
			"parentPrompt": "Ask: What kind of text might \"" + title + "\" be? What clue makes you think so?",
			"output":       "3 predictions about " + title + " + 4 vocabulary sentences",
		},
		{
			"day":          "Day 2",
			"focus":        "Reading with evidence",
			"studentTask":  "Read a short section from \"" + title + "\", then practise: " + reading + ". Point to the clue before explaining.",
			"parentPrompt": "Only push for two habits: full sentence and evidence.",
			"output":       "2 full-sentence answers about " + title + " with because / the text shows",
		},
		{
			"day":          "Day 3",
			"focus":        "Language pattern: " + language,
			"studentTask":  "Study one model sentence from \"" + title + "\", then write two new sentences using: " + language + ".",
			"parentPrompt": "Ask: Which word or grammar pattern changed the meaning?",
			"output":       "2 original " + title + " pattern sentences",
		},
		{
			"day":          "Day 4",
			"focus":        "Speaking before writing",
			"studentTask":  "Say the unit idea aloud first: " + speaking + " Then organise it with the scaffold.",
			"parentPrompt": "Ask: What is your main idea? What detail proves it?",
			"output":       speaking,
		},
		{
			"day":          "Day 5",
			"focus":        "Short writing and review",
			"studentTask":  "Write one short response for \"" + title + "\": " + writing + " Check the success criteria, then correct only the top one or two issues.",
			"parentPrompt": "Do not fix every error. Choose one focus: evidence, tense, vocabulary, or sentence completeness.",
			"output":       writing,
		},
	}
}

func englishLessonPractice(u model.EnglishCurriculumUnit, profile map[string]string, deep gin.H) []gin.H {
	cards := englishSourceLessonCards(u, profile)
	practice := make([]gin.H, 0, len(cards))
	for _, card := range cards {
		part, _ := card["part"].(string)
		title, _ := card["title"].(string)
		objective, _ := card["objective"].(string)
		practice = append(practice, gin.H{
			"part":      part,
			"title":     title,
			"objective": objective,
			"tasks":     englishLessonTasks(part, u, profile, deep),
		})
	}
	return practice
}

func englishLessonTasks(part string, u model.EnglishCurriculumUnit, profile map[string]string, deep gin.H) []gin.H {
	title := u.Title
	readingGoal := profile["readingSkill"]
	languageFocus := profile["languageFocus"]
	writingTask := profile["writingTask"]
	if rm, ok := deep["readingModel"].(gin.H); ok {
		if goal, ok := rm["goal"].(string); ok && goal != "" {
			readingGoal = goal
		}
	}
	if lp, ok := deep["languagePattern"].(gin.H); ok {
		if pattern, ok := lp["pattern"].(string); ok && pattern != "" {
			languageFocus = pattern
		}
	}
	if ws, ok := deep["writingScaffold"].(gin.H); ok {
		if genre, ok := ws["genre"].(string); ok && genre != "" {
			writingTask = "Plan one " + genre + "."
		}
	}

	switch part {
	case "A":
		return []gin.H{
			englishLessonTask("Text-type detective", "Use \""+title+"\" to predict the genre, audience and purpose.", "She can say: This unit is probably about ___ because ___."),
			englishLessonTask("Goal choice", "Choose whether reading, vocabulary, language or writing will be hardest in \""+title+"\" and say why.", "She names one concrete skill instead of saying everything is hard."),
		}
	case "B":
		return []gin.H{
			englishLessonTask("Gist then evidence", "Read a short section from \""+title+"\" for the main idea, then answer one question by pointing to the clue.", "She answers in a full sentence and names the clue."),
			englishLessonTask("Reading move", readingGoal, "She can explain what the reader is doing, not just the answer."),
		}
	case "C":
		return []gin.H{
			englishLessonTask("Word in context", "Pick two useful words from \""+title+"\" and write one unit sentence for each.", "The sentence connects to the unit topic, not a random sentence."),
			englishLessonTask("Pattern sentence", "Use the \""+title+"\" language focus: "+languageFocus+".", "The sentence is original and the pattern is visible."),
		}
	case "D":
		return []gin.H{
			englishLessonTask("Say before writing", "Explain the \""+title+"\" writing idea aloud before writing it.", "She can say the main idea and one detail without reading a script."),
			englishLessonTask("Short output", writingTask, "The final response has a main idea, evidence/detail, and a checked sentence pattern."),
		}
	default:
		return []gin.H{
			englishLessonTask("Read", "Read the section once for meaning and once for evidence.", "She can explain the clue."),
			englishLessonTask("Use", "Use one unit word in a full sentence.", "The sentence is complete and connected to the unit."),
		}
	}
}

func englishLessonTask(name, task, success string) gin.H {
	return gin.H{"name": name, "task": task, "success": success}
}

func englishCheckpointRubric(profile map[string]string, deep gin.H) []gin.H {
	readingGoal := profile["readingSkill"]
	languageFocus := profile["languageFocus"]
	writingTask := profile["writingTask"]
	vocabGoal := "uses unit words in her own sentences"
	if rm, ok := deep["readingModel"].(gin.H); ok {
		if goal, ok := rm["goal"].(string); ok && goal != "" {
			readingGoal = goal
		}
	}
	if lp, ok := deep["languagePattern"].(gin.H); ok {
		if pattern, ok := lp["pattern"].(string); ok && pattern != "" {
			languageFocus = pattern
		}
	}
	if ws, ok := deep["writingScaffold"].(gin.H); ok {
		if genre, ok := ws["genre"].(string); ok && genre != "" {
			writingTask = "writes one clear " + genre
		}
	}
	return []gin.H{
		englishRubricRow("Reading", readingGoal, "answers with a full sentence and points to evidence", "answers are short, guessed, or not tied to a clue", "Ask: Which words in the text tell you that?"),
		englishRubricRow("Vocabulary", vocabGoal, "explains each word simply and uses it in a unit-related sentence", "can recognise the word but cannot use it naturally", "Use say it, explain it, sentence it."),
		englishRubricRow("Language", languageFocus, "uses the target pattern in two original sentences", "copies the model but breaks the pattern in a new sentence", "Compare her sentence with the model and fix only the target pattern."),
		englishRubricRow("Writing / Speaking", writingTask, "has a main idea, one detail or evidence, and mostly complete sentences", "has ideas but they are listed, incomplete, or not organised", "Say the answer aloud first, then write the same idea."),
	}
}

func englishRubricRow(domain, focus, ready, needsPractice, nextMove string) gin.H {
	return gin.H{"domain": domain, "focus": focus, "ready": ready, "needsPractice": needsPractice, "nextMove": nextMove}
}

func englishReviewPrompts(profile map[string]string) []gin.H {
	return []gin.H{
		{
			"label":   "Strong",
			"prompt":  "What felt easiest in this unit about " + profile["theme"] + ", and what evidence shows it?",
			"lookFor": "She can name a real skill, such as evidence reading or using the unit vocabulary.",
		},
		{
			"label":   "Needs support",
			"prompt":  "Where did you need help: reading, vocabulary, language pattern, or writing?",
			"lookFor": "The answer should identify one narrow skill, not 'English is hard'.",
		},
		{
			"label":   "Next focus",
			"prompt":  "What is one small thing to practise again tomorrow?",
			"lookFor": "Choose one repeatable action: one evidence answer, two vocabulary sentences, or one revised paragraph.",
		},
	}
}

func englishSkillRoadmap(u model.EnglishCurriculumUnit, profile map[string]string, deep gin.H) gin.H {
	readingGoal := profile["readingSkill"]
	languageFocus := profile["languageFocus"]
	writingTask := profile["writingTask"]
	unitOutcome := profile["overview"]
	if rm, ok := deep["readingModel"].(gin.H); ok {
		if goal, ok := rm["goal"].(string); ok && goal != "" {
			readingGoal = goal
		}
	}
	if lp, ok := deep["languagePattern"].(gin.H); ok {
		if pattern, ok := lp["pattern"].(string); ok && pattern != "" {
			languageFocus = pattern
		}
	}
	if ws, ok := deep["writingScaffold"].(gin.H); ok {
		if genre, ok := ws["genre"].(string); ok && genre != "" {
			writingTask = "write one clear " + genre
		}
	}
	if outcome, ok := deep["unitOutcome"].(string); ok && outcome != "" {
		unitOutcome = outcome
	}

	return gin.H{
		"whyThisMatters": "This connects the unit to a Grade " + u.Grade + " loop: read with evidence, learn useful academic words, notice one language pattern, then use it in speaking or writing.",
		"bridge": []gin.H{
			{
				"stage":      "Before this",
				"studentCan": "Retell literal information, answer who / what / where questions, and say one complete sentence.",
				"homeCheck":  "Ask: What happened? Who is involved? Say it again in one full sentence.",
			},
			{
				"stage":      "This unit",
				"studentCan": unitOutcome + " The key reading move is: " + readingGoal + ".",
				"homeCheck":  "Ask one why/how question, then ask: Which words in the text tell you that?",
			},
			{
				"stage":      "Next level",
				"studentCan": "Transfer the same skill to a longer or unfamiliar text, then " + writingTask + " with less help.",
				"homeCheck":  "Use a new short text and ask her to explain the answer before writing it.",
			},
		},
		"focusCycle": []gin.H{
			{"focus": "Reading", "action": readingGoal, "evidence": "She points to a clue and explains why it matters."},
			{"focus": "Vocabulary", "action": "Explain each unit word simply, then use it in a unit-related sentence.", "evidence": "The sentence shows meaning, not just a memorised definition."},
			{"focus": "Language", "action": "Practise the target pattern: " + languageFocus + ".", "evidence": "She can make two new sentences without copying the model exactly."},
			{"focus": "Output", "action": "Say the idea aloud first, then " + writingTask + ".", "evidence": "The response has a main idea, a detail or evidence, and complete sentences."},
		},
	}
}

func englishSkillPractice(u model.EnglishCurriculumUnit, profile map[string]string, deep gin.H) []gin.H {
	readingGoal := profile["readingSkill"]
	languageFocus := profile["languageFocus"]
	writingTask := profile["writingTask"]
	vocabWord := "evidence"
	vocabMeaning := "a clue that supports an answer"
	vocabSentence := "The text gives evidence for my idea."
	languageExample := "I think ___ because the text says ___."
	questionFrame := "How do you know?"
	if rm, ok := deep["readingModel"].(gin.H); ok {
		if goal, ok := rm["goal"].(string); ok && goal != "" {
			readingGoal = goal
		}
		if frames, ok := rm["questionFrames"].([]string); ok && len(frames) > 0 && frames[0] != "" {
			questionFrame = frames[0]
		}
	}
	if lp, ok := deep["languagePattern"].(gin.H); ok {
		if pattern, ok := lp["pattern"].(string); ok && pattern != "" {
			languageFocus = pattern
		}
		if examples, ok := lp["examples"].([]string); ok && len(examples) > 0 && examples[0] != "" {
			languageExample = examples[0]
		}
	}
	if ws, ok := deep["writingScaffold"].(gin.H); ok {
		if genre, ok := ws["genre"].(string); ok && genre != "" {
			writingTask = "write one clear " + genre
		}
	}
	if vocab, ok := deep["vocabularyWords"].([]gin.H); ok && len(vocab) > 0 {
		if word, ok := vocab[0]["word"].(string); ok && word != "" {
			vocabWord = word
		}
		if meaning, ok := vocab[0]["meaning"].(string); ok && meaning != "" {
			vocabMeaning = meaning
		}
		if sentence, ok := vocab[0]["sentence"].(string); ok && sentence != "" {
			vocabSentence = sentence
		}
	}

	return []gin.H{
		englishSkillQuestion("Reading", "For "+u.Title+", which move best matches this unit's reading goal: "+readingGoal+"?", []string{"Read, find the clue, then explain why it matters.", "Pick the shortest answer.", "Copy a sentence without explaining it.", "Skip the hard words."}, "Read, find the clue, then explain why it matters.", "The reading target is not just getting an answer; it is showing the clue and explaining the thinking."),
		englishSkillQuestion("Reading", "If the question is \""+questionFrame+"\", what kind of answer is strongest?", []string{"A full answer with a clue from the text.", "One word only.", "A guess without reading again.", "A copied title."}, "A full answer with a clue from the text.", "School reading questions often ask for proof. The answer needs a claim plus a clue."),
		englishSkillQuestion("Vocabulary", "In "+u.Title+", what is the best way to show you understand \""+vocabWord+"\"?", []string{"Explain it simply and use it in a unit sentence.", "Spell it once and stop.", "Say it is a hard word.", "Use it in a random sentence that does not fit."}, "Explain it simply and use it in a unit sentence.", vocabWord+" means "+vocabMeaning+". A useful vocabulary answer shows meaning and use."),
		englishSkillQuestion("Vocabulary", "Which sentence uses \""+vocabWord+"\" in a useful way for this unit?", []string{vocabSentence, "I like " + vocabWord + ".", vocabWord + " is a word.", "The " + vocabWord + " went very quickly."}, vocabSentence, "The best sentence proves the meaning of the word inside a clear idea."),
		englishSkillQuestion("Language", "When writing about "+u.Title+", how should she practise the language focus: "+languageFocus+"?", []string{"Compare her own sentence with the model pattern.", "Change every word in the model.", "Ignore the pattern and write anything.", "Only memorise the title."}, "Compare her own sentence with the model pattern.", "The goal is transfer: she should use the pattern in a new sentence, then check what changed."),
		englishSkillQuestion("Language", "Which model sentence is best to study before writing?", []string{languageExample, "Because.", "And then nice.", "The unit is."}, languageExample, "A model sentence shows the pattern in a complete sentence, not just a fragment."),
		englishSkillQuestion("Writing", "Before she writes about "+u.Title+", what should she do for this task: "+writingTask+"?", []string{"Say the main idea and one detail aloud first.", "Write as fast as possible.", "Fix every spelling mistake before thinking.", "Use only one-word answers."}, "Say the main idea and one detail aloud first.", "Speaking first reduces blank-page pressure and makes the writing structure clearer."),
		englishSkillQuestion("Writing", "What should she check after writing about "+u.Title+"?", []string{"Main idea, one detail, and complete sentences.", "Only whether it is long.", "Only whether every word is fancy.", "Nothing after writing."}, "Main idea, one detail, and complete sentences.", "A short answer can be strong if the idea is clear, supported, and written in complete sentences."),
	}
}

func englishSkillQuestion(domain, prompt string, options []string, correct, explain string) gin.H {
	return gin.H{"domain": domain, "prompt": prompt, "options": options, "correct": correct, "explain": explain}
}

func englishCurriculumUnitPack(u model.EnglishCurriculumUnit) gin.H {
	profile := englishUnitProfile(u)
	deep := englishUnitDeepProfile(u, profile)
	sourceObjectives := englishSourceObjectivesFor(u, profile)
	sourcePDFRefs := englishSourcePDFRefs(u)
	sourceLessonCards := englishSourceLessonCards(u, profile)
	lessonFlow := []gin.H{
		{"stage": "Preview", "action": "Look at the title and predict the kind of text.", "why": "Good readers choose a reading strategy before they start."},
		{"stage": "Read", "action": "Read once for the gist, then again for clues.", "why": "The first read builds meaning; the second read finds evidence."},
		{"stage": "Explain", "action": "Answer with a full sentence plus because / evidence.", "why": "Grade " + u.Grade + " work expects reasoning, not one-word answers."},
		{"stage": "Use", "action": "Use the unit words and language pattern in speaking or writing.", "why": "A word is not mastered until she can use it naturally."},
	}
	readingModel := deep["readingModel"]
	vocabularyWords := deep["vocabularyWords"]
	languagePattern := deep["languagePattern"]
	writingScaffold := deep["writingScaffold"]
	weeklyPlan := englishWeeklyPlan(u, profile, deep)
	lessonPractice := englishLessonPractice(u, profile, deep)
	skillRoadmap := englishSkillRoadmap(u, profile, deep)
	checkpointRubric := englishCheckpointRubric(profile, deep)
	reviewPrompts := englishReviewPrompts(profile)
	skillPractice := englishSkillPractice(u, profile, deep)
	comprehensionPractice := []gin.H{
		{"prompt": "What is the main idea of this unit?", "options": []string{profile["theme"], "Only spelling", "Only handwriting", "Only speed"}, "correct": profile["theme"], "explain": "A main idea tells what the whole text or unit is mostly about."},
		{"prompt": "Which answer needs evidence from the text?", "options": []string{"How do you know?", "What colour is the cover?", "Can you copy the title?", "How fast can you read?"}, "correct": "How do you know?", "explain": "Grade " + u.Grade + " reading expects evidence, not just a short answer."},
		{"prompt": "What should a strong written answer include?", "options": []string{"A full sentence with a reason", "One random word", "Only a drawing", "No punctuation"}, "correct": "A full sentence with a reason", "explain": "School writing needs complete ideas and clear links."},
		{"prompt": "If a word is new, what should you try first?", "options": []string{"Use context clues", "Skip every sentence", "Guess without reading", "Stop reading"}, "correct": "Use context clues", "explain": "Look before and after the word to infer meaning."},
	}
	parentNotes := []string{
		"每天只抓一个目标：full sentence, evidence, or clearer word choice.",
		"不要把整课变成翻译；让她用英文说 what happened, why, and how do you know.",
		"如果回答太短，追问：Can you add one more detail?",
	}
	sourceRefs := []gin.H{
		{"book": u.Book, "unit": u.UnitNo, "title": u.Title, "pageStart": u.PageStart, "source": "My Pals Are Here! English contents page and local unit PDF locator"},
	}
	tabs := gin.H{
		"overview": gin.H{
			"unitOutcome":      deep["unitOutcome"],
			"overview":         profile["overview"],
			"sourceObjectives": sourceObjectives,
			"lessonFlow":       lessonFlow,
			"skills": gin.H{
				"reading":    profile["readingSkill"],
				"vocabulary": profile["academicVocabulary"],
				"language":   profile["languageFocus"],
				"writing":    profile["writingTask"],
			},
		},
		"roadmap":    skillRoadmap,
		"plan":       gin.H{"weeklyPlan": weeklyPlan, "lessonPractice": lessonPractice},
		"reading":    readingModel,
		"vocabulary": gin.H{"words": vocabularyWords, "routine": "say it, explain it in her own words, then use it in a sentence connected to the unit"},
		"language":   languagePattern,
		"writing":    writingScaffold,
		"practice":   gin.H{"questions": skillPractice, "fallbackQuestions": comprehensionPractice},
		"checkpoint": gin.H{"rubric": checkpointRubric, "reviewPrompts": reviewPrompts},
		"parent":     gin.H{"notes": parentNotes},
		"source":     gin.H{"objectives": sourceObjectives, "pdfRefs": sourcePDFRefs, "lessonCards": sourceLessonCards, "refs": sourceRefs},
	}
	return gin.H{
		"artifactVersion":       englishCurriculumArtifactVersion,
		"unitId":                u.UnitID,
		"grade":                 u.Grade,
		"book":                  u.Book,
		"title":                 u.Title,
		"unitOutcome":           deep["unitOutcome"],
		"sourceObjectives":      sourceObjectives,
		"sourcePdfRefs":         sourcePDFRefs,
		"sourceLessonCards":     sourceLessonCards,
		"theme":                 profile["theme"],
		"overview":              profile["overview"],
		"readingSkill":          profile["readingSkill"],
		"academicVocabulary":    profile["academicVocabulary"],
		"languageFocus":         profile["languageFocus"],
		"speakingTask":          profile["speakingTask"],
		"writingTask":           profile["writingTask"],
		"lessonFlow":            lessonFlow,
		"readingModel":          readingModel,
		"vocabularyWords":       vocabularyWords,
		"languagePattern":       languagePattern,
		"writingScaffold":       writingScaffold,
		"weeklyPlan":            weeklyPlan,
		"lessonPractice":        lessonPractice,
		"skillRoadmap":          skillRoadmap,
		"checkpointRubric":      checkpointRubric,
		"reviewPrompts":         reviewPrompts,
		"miniPractice":          deep["miniPractice"],
		"skillPractice":         skillPractice,
		"comprehensionPractice": comprehensionPractice,
		"parentNotes":           parentNotes,
		"sourceRefs":            sourceRefs,
		"tabs":                  tabs,
		"reviewStatus":          "deepened-from-unit-locator",
	}
}

func englishUnitProfile(u model.EnglishCurriculumUnit) map[string]string {
	title := u.Title
	switch title {
	case "Literal Retell and Full Sentences":
		return map[string]string{"theme": "Grade 3 support: retell clearly", "overview": "Repair the base skill: say what happened in order, then answer in a complete sentence.", "readingSkill": "retell who, what, where and when in order", "academicVocabulary": "first, next, finally, happened, because", "languageFocus": "complete sentence with subject and verb", "speakingTask": "Retell a short text in three ordered sentences.", "writingTask": "Write three full sentences about one short passage."}
	case "Evidence Starter Questions":
		return map[string]string{"theme": "Grade 3 support: find the clue", "overview": "Move from guessing to pointing at the sentence that proves the answer.", "readingSkill": "answer one why/how question with a text clue", "academicVocabulary": "clue, evidence, sentence, shows, tells", "languageFocus": "I think ___ because the text says ___", "speakingTask": "Answer one question aloud and point to the clue.", "writingTask": "Write one evidence answer using because."}
	case "Vocabulary in Context Repair":
		return map[string]string{"theme": "Grade 3 support: use words, not just recognise them", "overview": "Practise explaining a word simply and using it in a sentence that proves meaning.", "readingSkill": "use nearby words to infer meaning", "academicVocabulary": "meaning, example, context, similar, different", "languageFocus": "This word means ___ in this sentence", "speakingTask": "Explain three words in your own words.", "writingTask": "Write three sentences that show the word meaning."}
	case "Paragraph Basics":
		return map[string]string{"theme": "Grade 3 support: one clear paragraph", "overview": "Turn a list of ideas into one small paragraph with a main idea and supporting details.", "readingSkill": "identify the main idea and two details", "academicVocabulary": "main idea, detail, example, ending, support", "languageFocus": "because / but / so to connect ideas", "speakingTask": "Say the main idea and one detail before writing.", "writingTask": "Write a 4-sentence paragraph with one main idea."}
	case "Fantastic Animals":
		return map[string]string{"theme": "animal facts and descriptions", "overview": "Read about unusual animals and explain what makes them special.", "readingSkill": "identify key details and compare features", "academicVocabulary": "habitat, feature, protect, survive, unusual", "languageFocus": "adjectives and comparative language", "speakingTask": "Describe one animal and explain two special features.", "writingTask": "Write a short animal fact paragraph with one evidence sentence."}
	case "I Am Special":
		return map[string]string{"theme": "identity and personal strengths", "overview": "Use personal examples to explain what makes a person unique.", "readingSkill": "infer feelings and support with clues", "academicVocabulary": "strength, talent, proud, improve, personality", "languageFocus": "present simple for habits and abilities", "speakingTask": "Explain one strength and one thing to improve.", "writingTask": "Write a personal paragraph with because/but/so."}
	case "Fossils and Dinosaurs":
		return map[string]string{"theme": "past life and scientific evidence", "overview": "Use clues from fossils to explain what scientists can learn about dinosaurs.", "readingSkill": "use evidence to infer past events", "academicVocabulary": "fossil, skeleton, extinct, evidence, discover", "languageFocus": "past tense and time connectives", "speakingTask": "Explain what a fossil can tell us.", "writingTask": "Write an explanation using first, next, because, and therefore."}
	case "Poems Tell Stories Too":
		return map[string]string{"theme": "poetry as story", "overview": "Read poems for events, speaker, feeling, rhythm, and imagery.", "readingSkill": "notice speaker, mood, and figurative clues", "academicVocabulary": "stanza, rhythm, image, speaker, mood", "languageFocus": "verbs and descriptive phrases", "speakingTask": "Read a stanza aloud and explain the feeling.", "writingTask": "Write a short poem that tells a tiny story."}
	case "Let's Experiment!":
		return map[string]string{"theme": "scientific procedure", "overview": "Follow steps, predict outcomes, and explain results clearly.", "readingSkill": "sequence instructions and identify cause/effect", "academicVocabulary": "predict, observe, result, method, conclusion", "languageFocus": "imperatives and cause-effect connectives", "speakingTask": "Give clear experiment instructions.", "writingTask": "Write a simple experiment report."}
	case "Our Trip to Thailand":
		return map[string]string{"theme": "travel, culture, and recount", "overview": "Read and write about a trip using sequence, detail, and reflection.", "readingSkill": "sequence events and identify important details", "academicVocabulary": "journey, culture, temple, market, experience", "languageFocus": "past tense recount with time connectives", "speakingTask": "Retell a trip in order.", "writingTask": "Write a travel recount with beginning, middle, and ending."}
	case "A Bag of Laughs":
		return map[string]string{"theme": "humour and jokes", "overview": "Understand why something is funny by noticing expectation and surprise.", "readingSkill": "infer implied meaning and punchlines", "academicVocabulary": "humour, joke, surprise, expect, reaction", "languageFocus": "dialogue punctuation and expressive verbs", "speakingTask": "Tell a short joke and explain the surprise.", "writingTask": "Write a funny dialogue with clear speakers."}
	case "James and the Giant Peach":
		return map[string]string{"theme": "character, fantasy, and opinion", "overview": "Use a literary text to discuss characters, events, and personal response.", "readingSkill": "character inference and evidence", "academicVocabulary": "character, setting, problem, solution, opinion", "languageFocus": "opinion language and evidence phrases", "speakingTask": "Give an opinion about a character with evidence.", "writingTask": "Write a reading response paragraph."}
	case "What Do You Think of It?":
		return map[string]string{"theme": "opinion and reasoning", "overview": "Build opinions with reasons, examples, and respectful comparison.", "readingSkill": "distinguish fact, opinion, and reason", "academicVocabulary": "opinion, reason, agree, disagree, support", "languageFocus": "modal verbs and opinion phrases", "speakingTask": "State an opinion and respond to another view.", "writingTask": "Write an opinion paragraph with two reasons."}
	case "Wonders of Science":
		return map[string]string{"theme": "science ideas and explanation", "overview": "Read science texts to explain processes, causes, and effects.", "readingSkill": "explain cause/effect with evidence", "academicVocabulary": "process, effect, energy, material, investigate", "languageFocus": "present simple passive-style explanations and cause/effect words", "speakingTask": "Explain how one science process works.", "writingTask": "Write an explanation paragraph with because, so, and therefore."}
	case "Great Olympians":
		return map[string]string{"theme": "biography and achievement", "overview": "Read about athletes and explain effort, challenge, and achievement.", "readingSkill": "summarise life events and infer character traits", "academicVocabulary": "athlete, achievement, compete, effort, determination", "languageFocus": "past tense biography and time phrases", "speakingTask": "Introduce an Olympian and explain one trait.", "writingTask": "Write a short biography paragraph with evidence."}
	case "Longer Inference with Evidence":
		return map[string]string{"theme": "Grade 5 preview: longer inference", "overview": "Prepare for longer texts by tracking clues across more than one paragraph.", "readingSkill": "combine two clues to support one inference", "academicVocabulary": "infer, evidence, paragraph, motive, conclusion", "languageFocus": "Although ___, I infer ___ because ___", "speakingTask": "Explain one inference using two clues.", "writingTask": "Write an inference answer with two pieces of evidence."}
	case "Multi-Paragraph Transition":
		return map[string]string{"theme": "Grade 5 preview: organised writing", "overview": "Prepare for longer school writing by planning a main point, two body points and a short ending.", "readingSkill": "notice how ideas are grouped across paragraphs", "academicVocabulary": "introduction, paragraph, reason, example, conclusion", "languageFocus": "connectives that organise ideas: firstly, however, for example, therefore", "speakingTask": "Say a two-point plan before writing.", "writingTask": "Write two short paragraphs with clear links."}
	default:
		return englishGenericUnitProfile(u)
	}
}

func englishGenericUnitProfile(u model.EnglishCurriculumUnit) map[string]string {
	title := strings.ToLower(u.Title)
	out := map[string]string{
		"theme":              u.Title,
		"overview":           "Use this Grade " + u.Grade + " unit to read for meaning, explain ideas with evidence, and turn the unit theme into speaking and writing.",
		"readingSkill":       "identify the main idea and support it with evidence",
		"academicVocabulary": "explain, evidence, infer, describe, compare",
		"languageFocus":      "complete sentences with because / but / so",
		"speakingTask":       "Answer one unit question in a full sentence with evidence.",
		"writingTask":        "Write a short paragraph with a main idea and one supporting detail.",
		"genre":              "unit reading text",
	}
	switch {
	case strings.Contains(title, "poem"):
		out["readingSkill"] = "notice speaker, mood, image and rhythm"
		out["academicVocabulary"] = "speaker, mood, image, stanza, rhythm"
		out["languageFocus"] = "strong verbs and descriptive phrases"
		out["writingTask"] = "Write a short poem response with one image and one feeling."
		out["genre"] = "poetry"
	case strings.Contains(title, "science") || strings.Contains(title, "experiment") || strings.Contains(title, "fossil"):
		out["readingSkill"] = "explain cause, process and result with evidence"
		out["academicVocabulary"] = "process, cause, effect, observe, evidence"
		out["languageFocus"] = "sequence and cause-effect connectives"
		out["writingTask"] = "Write a short explanation with first / next / because / so."
		out["genre"] = "science explanation"
	case strings.Contains(title, "review") || strings.Contains(title, "think") || strings.Contains(title, "opinion"):
		out["readingSkill"] = "separate fact, opinion, reason and evidence"
		out["academicVocabulary"] = "opinion, reason, support, recommend, evaluate"
		out["languageFocus"] = "opinion phrases with because and examples"
		out["writingTask"] = "Write an opinion paragraph with two reasons."
		out["genre"] = "opinion or review"
	case strings.Contains(title, "trip") || strings.Contains(title, "travel") || strings.Contains(title, "journey") || strings.Contains(title, "world"):
		out["readingSkill"] = "sequence events and identify important details"
		out["academicVocabulary"] = "journey, place, culture, experience, detail"
		out["languageFocus"] = "past tense recount with time connectives"
		out["writingTask"] = "Write a short recount with beginning, middle and ending."
		out["genre"] = "recount or travel text"
	case strings.Contains(title, "story") || strings.Contains(title, "laugh") || strings.Contains(title, "giant"):
		out["readingSkill"] = "infer character, setting, problem and change"
		out["academicVocabulary"] = "character, setting, problem, solution, clue"
		out["languageFocus"] = "dialogue, action verbs and evidence phrases"
		out["writingTask"] = "Write a short narrative response with evidence."
		out["genre"] = "narrative"
	}
	return out
}

func englishUnitDeepProfile(u model.EnglishCurriculumUnit, profile map[string]string) gin.H {
	title := u.Title
	switch title {
	case "Fantastic Animals":
		return englishDeepPack(
			"Explain how animal features help animals live.",
			"information report",
			"Compare features, then connect each feature to a purpose.",
			[]string{"What is special about this animal?", "How does the feature help it survive?", "Which detail proves that?"},
			[]gin.H{
				engVocab("habitat", "the place where an animal or plant lives", "A desert is a difficult habitat because it is dry.", "Describe one animal habitat."),
				engVocab("feature", "an important part or quality", "Sharp claws are a useful feature for climbing.", "Name one feature of a bird."),
				engVocab("survive", "to stay alive in a difficult place", "Some animals survive by hiding from predators.", "Use survive in a sentence."),
				engVocab("protect", "to keep safe from harm", "A turtle shell helps protect its body.", "What protects a hedgehog?"),
			},
			"comparative adjectives",
			"Use comparative words to compare animal features: faster, stronger, longer, more useful.",
			[]string{"The cheetah is faster than many animals.", "A camel's hump is more useful in the desert."},
			"Some children only list features. Push for feature plus purpose.",
			"Animal fact paragraph",
			[]gin.H{
				engStep("Topic", "Name the animal and the main idea."),
				engStep("Feature", "Describe one special feature."),
				engStep("Purpose", "Explain how the feature helps the animal."),
				engStep("Evidence", "Add one fact or example."),
			},
			englishMiniPractice("Which answer best connects feature to purpose?", []string{"A camel has wide feet, so it can walk on sand.", "A camel is brown.", "A camel is an animal.", "A camel has a name."}, "A camel has wide feet, so it can walk on sand.", "Strong answers explain why the feature matters."),
		)
	case "I Am Special":
		return englishDeepPack(
			"Explain personal strengths with examples, not just labels.",
			"personal reflection",
			"Infer feelings from actions and words, then connect them to personal examples.",
			[]string{"What does the person feel?", "What action gives the clue?", "What example makes the idea believable?"},
			[]gin.H{
				engVocab("strength", "something a person can do well", "Her strength is explaining ideas clearly.", "Name one of your strengths."),
				engVocab("talent", "a natural or practised ability", "He has a talent for drawing animals.", "Use talent in a sentence."),
				engVocab("improve", "to get better with practice", "I can improve my reading by reading every day.", "What do you want to improve?"),
				engVocab("proud", "feeling happy about effort or success", "She felt proud after finishing the race.", "When do you feel proud?"),
			},
			"because / but / so",
			"Use because to explain, but to show contrast, and so to show result.",
			[]string{"I am good at reading because I practise often.", "I find spelling hard, but I keep trying.", "I listened carefully, so I understood the task."},
			"Children often write only 'I am special'. Ask for why and evidence.",
			"Personal paragraph",
			[]gin.H{
				engStep("Main idea", "Say one strength or quality."),
				engStep("Example", "Tell one real moment that shows it."),
				engStep("Growth", "Name one thing to improve."),
				engStep("Ending", "Finish with how you will practise."),
			},
			englishMiniPractice("Which sentence gives the clearest reason?", []string{"I am patient because I wait calmly when tasks are hard.", "I am patient.", "Patient is good.", "I like patient."}, "I am patient because I wait calmly when tasks are hard.", "The best sentence explains the idea with a real behaviour."),
		)
	case "Fossils and Dinosaurs":
		return englishDeepPack(
			"Use evidence from objects to explain something from the past.",
			"science explanation",
			"Separate what we know from what we infer from clues.",
			[]string{"What clue do scientists have?", "What can they infer?", "How strong is the evidence?"},
			[]gin.H{
				engVocab("fossil", "a trace or remain of an old living thing", "The fossil showed the shape of a leaf.", "Explain what a fossil can show."),
				engVocab("evidence", "a clue that supports an idea", "The footprint was evidence that an animal walked there.", "Use evidence in a sentence."),
				engVocab("extinct", "no longer living anywhere", "Dinosaurs are extinct.", "Name an extinct animal."),
				engVocab("discover", "to find something new", "Scientists discover clues by digging carefully.", "What can people discover?"),
			},
			"past tense with evidence verbs",
			"Use past tense for events long ago and verbs like showed, suggested, discovered.",
			[]string{"The fossil showed that the animal had sharp teeth.", "Scientists discovered footprints in the rock."},
			"Do not turn every clue into a certainty. Some clues suggest; they do not prove everything.",
			"Evidence explanation",
			[]gin.H{
				engStep("Clue", "Name the fossil or object."),
				engStep("Observation", "Describe what can be seen."),
				engStep("Inference", "Explain what it may mean."),
				engStep("Limit", "Say what we still cannot know for sure."),
			},
			englishMiniPractice("Which phrase shows careful inference?", []string{"The fossil suggests the animal ate meat.", "The fossil is meat.", "The animal definitely talked.", "The rock is old so everything is known."}, "The fossil suggests the animal ate meat.", "Suggests is careful because evidence gives clues, not every answer."),
		)
	case "Poems Tell Stories Too":
		return englishDeepPack(
			"Read poems for story, feeling, rhythm, and image.",
			"poem narrative",
			"Track who is speaking, what changes, and what feeling the words create.",
			[]string{"Who is the speaker?", "What happens first and next?", "Which words show the mood?"},
			[]gin.H{
				engVocab("stanza", "a group of lines in a poem", "The second stanza changes the mood.", "Point to a stanza in a poem."),
				engVocab("mood", "the feeling of a text", "The mood is peaceful because the words are soft.", "Describe a happy mood."),
				engVocab("image", "a picture made by words", "The poem creates an image of rain on glass.", "What image do you see?"),
				engVocab("speaker", "the voice telling the poem", "The speaker misses her friend.", "Who might the speaker be?"),
			},
			"strong verbs and describing phrases",
			"Choose verbs and details that make the reader see and feel the moment.",
			[]string{"The leaves whispered in the wind.", "The small boat drifted across the quiet lake."},
			"Children may summarise only plot. Ask which words create the feeling.",
			"Story poem",
			[]gin.H{
				engStep("Moment", "Choose one tiny event."),
				engStep("Image", "Add what you can see or hear."),
				engStep("Feeling", "Choose words that create a mood."),
				engStep("Shape", "Break the poem into short lines."),
			},
			englishMiniPractice("Which question helps understand a poem's mood?", []string{"Which words make the poem feel calm or exciting?", "How many pages are there?", "Is the font large?", "Can I skip the lines?"}, "Which words make the poem feel calm or exciting?", "Mood comes from word choice and images."),
		)
	case "Let's Experiment!":
		return englishDeepPack(
			"Explain a procedure clearly enough that someone can repeat it.",
			"procedure and report",
			"Read instructions as a sequence, then connect actions to results.",
			[]string{"What should happen first?", "What must be measured or observed?", "What caused the result?"},
			[]gin.H{
				engVocab("predict", "to say what you think will happen", "I predict the ice will melt faster in warm water.", "Make one prediction."),
				engVocab("observe", "to watch carefully and notice details", "We observed bubbles in the water.", "What can you observe?"),
				engVocab("result", "what happens at the end of a test", "The result showed that the paper absorbed water.", "Use result in a sentence."),
				engVocab("conclusion", "the final explanation after looking at results", "My conclusion is that sunlight helps the plant grow.", "Write a conclusion starter."),
			},
			"imperatives and sequence connectives",
			"Use command verbs and sequence words: first, next, then, finally.",
			[]string{"First, pour the water into the cup.", "Next, observe what changes."},
			"Missing steps make instructions hard to follow. Ask if another person could repeat it.",
			"Experiment report",
			[]gin.H{
				engStep("Question", "What are we testing?"),
				engStep("Prediction", "What do I think will happen?"),
				engStep("Method", "What steps did I follow?"),
				engStep("Result", "What happened?"),
				engStep("Conclusion", "What does the result mean?"),
			},
			englishMiniPractice("Which sentence belongs in a method?", []string{"Next, place the cup near the window.", "I think plants are pretty.", "The result was surprising.", "Science is fun."}, "Next, place the cup near the window.", "A method sentence tells an action in order."),
		)
	case "Our Trip to Thailand":
		return englishDeepPack(
			"Retell a real or imagined trip in order with useful details.",
			"travel recount",
			"Follow the sequence of events and notice details that show place and culture.",
			[]string{"Where did the event happen?", "What happened first, next, and last?", "What detail makes the place clear?"},
			[]gin.H{
				engVocab("journey", "a trip from one place to another", "The journey took two hours by bus.", "Describe a journey."),
				engVocab("culture", "the customs and way of life of a group", "Food, clothes, and festivals are part of culture.", "Name one part of culture."),
				engVocab("experience", "something that happens to you", "The boat ride was a new experience.", "Use experience in a sentence."),
				engVocab("market", "a place where people buy and sell things", "The market was busy and colourful.", "What can you see at a market?"),
			},
			"past tense recount connectives",
			"Use past tense with time words: yesterday, later, after that, finally.",
			[]string{"First, we visited a busy market.", "After that, we tried a sweet snack."},
			"Recounts can become lists. Ask for one sensory detail in each event.",
			"Travel recount",
			[]gin.H{
				engStep("Opening", "Tell where and when the trip happened."),
				engStep("Event 1", "Describe the first important event."),
				engStep("Event 2", "Add another event with detail."),
				engStep("Reflection", "Say what you learned or felt."),
			},
			englishMiniPractice("Which sentence gives the best travel detail?", []string{"The market smelled of grilled food and fresh fruit.", "The market was there.", "We went.", "It was a place."}, "The market smelled of grilled food and fresh fruit.", "Specific sensory details help the reader imagine the place."),
		)
	case "A Bag of Laughs":
		return englishDeepPack(
			"Understand humour by spotting expectation and surprise.",
			"humour and dialogue",
			"Notice what the reader expects, then how the ending changes it.",
			[]string{"What did I expect?", "What surprised me?", "Which words create the joke?"},
			[]gin.H{
				engVocab("humour", "something that makes people laugh", "The humour comes from the silly ending.", "What kind of humour do you like?"),
				engVocab("surprise", "something unexpected", "The surprise made the joke funny.", "Use surprise in a sentence."),
				engVocab("reaction", "what someone does or feels after something happens", "Her reaction was to laugh loudly.", "Describe a reaction."),
				engVocab("dialogue", "words spoken by characters", "The dialogue shows the two friends arguing.", "Write one line of dialogue."),
			},
			"dialogue punctuation",
			"Use quotation marks and speaker tags so readers know who is talking.",
			[]string{"\"That is not my sandwich,\" said Amir.", "\"Then why are you eating it?\" asked Mei."},
			"Dialogue without punctuation is hard to follow. Ask who is speaking each line.",
			"Funny dialogue",
			[]gin.H{
				engStep("Setup", "Create a normal situation."),
				engStep("Expectation", "Let the reader expect one thing."),
				engStep("Surprise", "End with a harmless surprise."),
				engStep("Dialogue", "Use clear speaker tags."),
			},
			englishMiniPractice("Why can a joke be funny?", []string{"It changes what the reader expected.", "It has no punctuation.", "It is always long.", "It uses only hard words."}, "It changes what the reader expected.", "Many jokes work by setting up an expectation and then turning it."),
		)
	case "James and the Giant Peach":
		return englishDeepPack(
			"Use character evidence to explain opinions about a story.",
			"literary response",
			"Track character actions, feelings, problems, and changes.",
			[]string{"What does the character do?", "What does that show about the character?", "Which evidence supports my opinion?"},
			[]gin.H{
				engVocab("character", "a person or creature in a story", "The main character changes during the story.", "Name a character you know."),
				engVocab("setting", "where and when a story happens", "The setting feels magical and strange.", "Describe a setting."),
				engVocab("problem", "what makes the story difficult for the character", "The problem begins when the peach grows.", "What is a story problem?"),
				engVocab("opinion", "what you think or believe", "My opinion is that James is brave.", "Give one opinion about a character."),
			},
			"opinion with evidence",
			"Use I think... because the text shows... to connect opinion and evidence.",
			[]string{"I think James is lonely because he has no one kind to talk to.", "The character seems brave because he keeps going."},
			"An opinion without evidence sounds like a guess. Ask: how do you know?",
			"Reading response",
			[]gin.H{
				engStep("Opinion", "State what you think about the character."),
				engStep("Evidence", "Name one action or event."),
				engStep("Explain", "Tell what the evidence shows."),
				engStep("Connect", "Link back to your opinion."),
			},
			englishMiniPractice("Which sentence uses evidence?", []string{"I think the character is brave because he keeps trying after the problem.", "The character is brave.", "I like the character.", "The story has a character."}, "I think the character is brave because he keeps trying after the problem.", "It gives an opinion and explains the evidence."),
		)
	case "What Do You Think of It?":
		return englishDeepPack(
			"Build opinions with reasons and examples.",
			"opinion text",
			"Separate facts, opinions, reasons, and examples.",
			[]string{"Is this a fact or an opinion?", "What reason supports it?", "What example makes it stronger?"},
			[]gin.H{
				engVocab("opinion", "what someone thinks or believes", "My opinion is that libraries are important.", "State one opinion."),
				engVocab("reason", "why you think something", "My reason is that books help people learn.", "Give one reason."),
				engVocab("support", "to help prove or explain an idea", "The example supports my opinion.", "Use support in a sentence."),
				engVocab("agree", "to have the same opinion", "I agree because the reason is strong.", "What do you agree with?"),
			},
			"modal verbs for opinion",
			"Use should, could, might, must to show strength of opinion.",
			[]string{"Schools should have more reading time.", "Children could choose books that match their interests."},
			"Children may repeat the opinion instead of giving a reason. Ask why twice.",
			"Opinion paragraph",
			[]gin.H{
				engStep("Opinion", "State what you think."),
				engStep("Reason 1", "Give the strongest reason."),
				engStep("Example", "Add a real or imagined example."),
				engStep("Closing", "Restate the opinion clearly."),
			},
			englishMiniPractice("Which is an opinion?", []string{"Students should read every day.", "A week has seven days.", "Water can freeze.", "A triangle has three sides."}, "Students should read every day.", "Should often signals an opinion or recommendation."),
		)
	case "Wonders of Science":
		return englishDeepPack(
			"Answer why questions with cause, effect, and evidence.",
			"science explanation",
			"Turn curiosity into an explanation: question, cause, process, result.",
			[]string{"What question is the text trying to explain?", "What cause explains it?", "What evidence or example helps?"},
			[]gin.H{
				engVocab("process", "a series of steps or changes", "Melting is a process where solid ice becomes water.", "Name one process."),
				engVocab("effect", "what happens because of a cause", "The effect of heating ice is that it melts.", "Use effect in a sentence."),
				engVocab("energy", "power that can make things change or move", "Heat energy can change ice into water.", "Where can energy come from?"),
				engVocab("investigate", "to try to find out the answer", "Scientists investigate why plants grow toward light.", "What can you investigate?"),
			},
			"cause and effect explanation",
			"Use because, so, therefore, and as a result to link cause and effect.",
			[]string{"Ice melts because heat energy makes the particles move more.", "The air cools, so water vapour can form tiny drops."},
			"Children often answer why with one word. Ask for cause plus result.",
			"Explanation text",
			[]gin.H{
				engStep("Question", "Start with a why or how question."),
				engStep("Cause", "Name the reason or force."),
				engStep("Process", "Explain the steps in order."),
				engStep("Result", "Say what happens at the end."),
			},
			englishMiniPractice("Which answer best explains why ice melts?", []string{"Ice melts because heat energy changes it from solid to liquid.", "Ice is cold.", "Ice is white.", "Ice is in a cup."}, "Ice melts because heat energy changes it from solid to liquid.", "A why answer needs a cause and a result."),
		)
	case "Great Olympians":
		return englishDeepPack(
			"Read a life story and explain achievement with evidence.",
			"biography",
			"Follow the timeline, then infer traits from choices and effort.",
			[]string{"What happened first?", "What challenge did the person face?", "What trait does the evidence show?"},
			[]gin.H{
				engVocab("athlete", "a person trained in sports", "The athlete practised every morning.", "Name an athlete."),
				engVocab("achievement", "something important someone has done", "Winning the race was a great achievement.", "What is one achievement?"),
				engVocab("determination", "not giving up when something is hard", "Her determination helped her train after losing.", "Use determination in a sentence."),
				engVocab("compete", "to try to win in a contest", "Many runners compete in the Olympics.", "Where can people compete?"),
			},
			"past tense biography",
			"Use past tense and time phrases to show life events in order.",
			[]string{"When she was young, she trained after school.", "Later, she competed in an important race."},
			"Biography is not just dates. Ask what the events show about the person.",
			"Biography paragraph",
			[]gin.H{
				engStep("Introduce", "Name the person and what they are known for."),
				engStep("Timeline", "Tell two important events in order."),
				engStep("Trait", "Explain one character trait with evidence."),
				engStep("Impact", "Say why the person matters."),
			},
			englishMiniPractice("Which answer infers a trait from evidence?", []string{"She trained every day, so she showed determination.", "She was an athlete.", "The race was on Saturday.", "She had shoes."}, "She trained every day, so she showed determination.", "A trait inference connects evidence to a quality."),
		)
	default:
		return englishGenericDeepProfile(u, profile)
	}
}

func englishGenericDeepProfile(u model.EnglishCurriculumUnit, profile map[string]string) gin.H {
	words := strings.Split(profile["academicVocabulary"], ",")
	vocab := make([]gin.H, 0, 4)
	for _, w := range words {
		word := strings.TrimSpace(w)
		if word == "" {
			continue
		}
		vocab = append(vocab, engVocab(word, "a useful word for this unit", "Use "+word+" when talking about "+u.Title+".", "Make one sentence with "+word+"."))
		if len(vocab) == 4 {
			break
		}
	}
	for len(vocab) < 4 {
		vocab = append(vocab, engVocab("evidence", "a clue that supports an answer", "The text gives evidence for my idea.", "Find one piece of evidence."))
	}
	genre := profile["genre"]
	if genre == "" {
		genre = "unit reading text"
	}
	return englishDeepPack(
		"Use "+u.Title+" to build Grade "+u.Grade+" reading, vocabulary, language and writing skills.",
		genre,
		profile["readingSkill"],
		[]string{"What is this mostly about?", "What evidence supports it?", "How can I say it in a full sentence?"},
		vocab,
		profile["languageFocus"],
		"Practise the pattern inside this unit, then transfer it into a new sentence.",
		[]string{"I think ___ because the text says ___.", "This shows ___ because ___."},
		"One-word answers hide thinking. Ask for a full sentence.",
		profile["writingTask"],
		[]gin.H{
			engStep("Idea", "State the answer."),
			engStep("Evidence", "Add the clue."),
			engStep("Explain", "Say why the clue matters."),
		},
		englishMiniPractice("Which answer is strongest?", []string{"I think this because the text gives a clue.", "Yes.", "Maybe.", "It is nice."}, "I think this because the text gives a clue.", "A strong answer shows thinking and evidence."),
	)
}

func englishDeepPack(outcome, genre, readingGoal string, questionFrames []string, vocab []gin.H, pattern, patternWhy string, examples []string, commonError, writingGenre string, writingSteps []gin.H, mini gin.H) gin.H {
	return gin.H{
		"unitOutcome": outcome,
		"readingModel": gin.H{
			"genre":          genre,
			"goal":           readingGoal,
			"questionFrames": questionFrames,
			"studentMove":    "Read, point to the clue, then explain it in a full sentence.",
		},
		"vocabularyWords": vocab,
		"languagePattern": gin.H{
			"pattern":      pattern,
			"whyItMatters": patternWhy,
			"examples":     examples,
			"commonError":  commonError,
			"tryIt":        "Write one original sentence using this pattern.",
		},
		"writingScaffold": gin.H{
			"genre":           writingGenre,
			"steps":           writingSteps,
			"successCriteria": []string{"clear main idea", "full sentences", "one reason or evidence", "unit vocabulary used naturally"},
		},
		"miniPractice": []gin.H{mini,
			englishMiniPractice("What should she do after choosing an answer?", []string{"Point to the clue or reason.", "Close the book immediately.", "Change every word.", "Only say yes."}, "Point to the clue or reason.", "Evidence habits matter more than guessing quickly."),
		},
	}
}

func engVocab(word, meaning, sentence, prompt string) gin.H {
	return gin.H{"word": word, "meaning": meaning, "sentence": sentence, "prompt": prompt}
}

func engStep(label, prompt string) gin.H {
	return gin.H{"label": label, "prompt": prompt}
}

func englishMiniPractice(prompt string, options []string, correct, explain string) gin.H {
	return gin.H{"prompt": prompt, "options": options, "correct": correct, "explain": explain}
}
