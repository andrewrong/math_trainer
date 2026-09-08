package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"mathtrainer/internal/ai"
	"mathtrainer/internal/config"
	"mathtrainer/internal/model"
)

const readingPromptVersion = "reading-thinking-v1"

type ReadingHandler struct {
	db         *sql.DB
	ai         *ai.Chain
	cfg        config.ReadingConfig
	httpClient *http.Client
}

func NewReading(db *sql.DB, chain *ai.Chain, cfg config.ReadingConfig) *ReadingHandler {
	timeout := time.Duration(cfg.OCR.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 900 * time.Second
	}
	return &ReadingHandler{
		db:         db,
		ai:         chain,
		cfg:        cfg,
		httpClient: &http.Client{Timeout: timeout},
	}
}

func (h *ReadingHandler) Register(r gin.IRouter) {
	r.GET("/reading/profiles", h.listReadingProfiles)
	r.PATCH("/reading/profiles/:profileId", h.updateReadingProfile)
	r.GET("/reading/books", h.listReadingBooks)
	r.POST("/reading/books", h.createReadingBook)
	r.POST("/reading/books/import", h.importReadingBook)
	r.GET("/reading/books/:bookId", h.getReadingBook)
	r.POST("/reading/books/recognize", h.recognizeReadingBook)
	r.POST("/reading/books/:bookId/assets", h.uploadReadingAsset)
	r.GET("/reading/assets/:assetId/image", h.serveReadingAssetImage)
	r.PUT("/reading/books/:bookId/page-map", h.saveReadingPageMap)
	r.POST("/reading/library/scan", h.scanReadingLibrary)
	r.GET("/reading/sessions", h.listReadingSessions)
	r.POST("/reading/sessions", h.createReadingSession)
	r.POST("/reading/checks", h.createReadingCheck)
	r.POST("/reading/checks/:checkId/respond", h.respondReadingCheck)
	r.PUT("/reading/checks/:checkId/confirm", h.confirmReadingObservation)
	r.GET("/reading/weekly-review", h.getReadingWeeklyReview)
	r.PUT("/reading/weekly-review", h.putReadingWeeklyReview)
}

func (h *ReadingHandler) listReadingProfiles(c *gin.Context) {
	rows, err := h.db.QueryContext(c, `
		SELECT profile_id,display_name,profile_type,review_mode,avatar_color
		FROM reading_profiles ORDER BY CASE profile_type WHEN 'child' THEN 0 ELSE 1 END, created_at`)
	if err != nil {
		readingError(c, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	profiles := []model.ReadingProfile{}
	for rows.Next() {
		var p model.ReadingProfile
		if err := rows.Scan(&p.ProfileID, &p.DisplayName, &p.ProfileType, &p.ReviewMode, &p.AvatarColor); err != nil {
			readingError(c, http.StatusInternalServerError, err)
			return
		}
		profiles = append(profiles, p)
	}
	c.JSON(http.StatusOK, gin.H{"profiles": profiles})
}

func (h *ReadingHandler) updateReadingProfile(c *gin.Context) {
	var req struct {
		DisplayName string `json:"displayName" binding:"required"`
		AvatarColor string `json:"avatarColor"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		readingError(c, http.StatusBadRequest, err)
		return
	}
	if req.AvatarColor == "" {
		req.AvatarColor = "#185fa5"
	}
	result, err := h.db.ExecContext(c, `UPDATE reading_profiles SET display_name=?,avatar_color=?,updated_at=CURRENT_TIMESTAMP WHERE profile_id=?`,
		strings.TrimSpace(req.DisplayName), req.AvatarColor, c.Param("profileId"))
	if err != nil {
		readingError(c, http.StatusInternalServerError, err)
		return
	}
	if n, _ := result.RowsAffected(); n == 0 {
		readingError(c, http.StatusNotFound, errors.New("reading profile not found"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *ReadingHandler) listReadingBooks(c *gin.Context) {
	profileID := c.DefaultQuery("profileId", "learner")
	rows, err := h.db.QueryContext(c, `
		SELECT b.id,b.title,b.author,b.genre,b.language,b.isbn,b.publisher,b.cover_path,b.metadata_source,b.created_at,
		       COALESCE((SELECT ingestion_status FROM reading_assets a WHERE a.book_id=b.id AND a.asset_type='ebook' ORDER BY a.id DESC LIMIT 1),''),
		       COALESCE((SELECT id FROM reading_assets a WHERE a.book_id=b.id AND a.asset_type='cover' ORDER BY a.id DESC LIMIT 1),0),
		       p.chapter,p.page_number,p.percent,p.status,p.updated_at
		FROM reading_books b
		LEFT JOIN reading_progress p ON p.book_id=b.id AND p.profile_id=?
		ORDER BY CASE WHEN p.status='reading' THEN 0 WHEN p.status='want_to_read' THEN 1 WHEN p.status='finished' THEN 3 ELSE 2 END,
		         COALESCE(p.updated_at,b.created_at) DESC,b.title`, profileID)
	if err != nil {
		readingError(c, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	books := []model.ReadingBook{}
	for rows.Next() {
		book, err := scanReadingBook(rows, profileID)
		if err != nil {
			readingError(c, http.StatusInternalServerError, err)
			return
		}
		books = append(books, book)
	}
	c.JSON(http.StatusOK, gin.H{"books": books})
}

type readingBookScanner interface {
	Scan(dest ...any) error
}

func scanReadingBook(row readingBookScanner, profileID string) (model.ReadingBook, error) {
	var b model.ReadingBook
	var chapter, status, progressUpdated sql.NullString
	var page, percent sql.NullInt64
	err := row.Scan(&b.ID, &b.Title, &b.Author, &b.Genre, &b.Language, &b.ISBN, &b.Publisher, &b.CoverPath, &b.MetadataSource, &b.CreatedAt,
		&b.AssetStatus, &b.CoverAssetID, &chapter, &page, &percent, &status, &progressUpdated)
	if err != nil {
		return b, err
	}
	if status.Valid {
		b.Progress = &model.ReadingProgress{
			ProfileID: profileID,
			BookID:    b.ID,
			Chapter:   chapter.String,
			Page:      int(page.Int64),
			Percent:   int(percent.Int64),
			Status:    status.String,
			UpdatedAt: progressUpdated.String,
		}
	}
	return b, nil
}

func (h *ReadingHandler) createReadingBook(c *gin.Context) {
	var req model.CreateReadingBookReq
	if err := c.ShouldBindJSON(&req); err != nil {
		readingError(c, http.StatusBadRequest, err)
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Author = strings.TrimSpace(req.Author)
	if req.Genre == "" {
		req.Genre = "fiction"
	}
	if req.Language == "" {
		req.Language = "en"
	}
	if req.MetadataSource == "" {
		req.MetadataSource = "manual"
	}
	if req.ISBN != "" {
		var existing int64
		if err := h.db.QueryRowContext(c, `SELECT id FROM reading_books WHERE isbn=? LIMIT 1`, req.ISBN).Scan(&existing); err == nil {
			c.JSON(http.StatusConflict, gin.H{"error": "A book with this ISBN already exists.", "bookId": existing})
			return
		}
	}
	var existing int64
	err := h.db.QueryRowContext(c, `SELECT id FROM reading_books WHERE lower(trim(title))=lower(trim(?)) AND lower(trim(author))=lower(trim(?)) LIMIT 1`, req.Title, req.Author).Scan(&existing)
	if err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "This title and author are already in the library.", "bookId": existing})
		return
	}
	result, err := h.db.ExecContext(c, `
		INSERT INTO reading_books(title,author,genre,language,isbn,publisher,metadata_source)
		VALUES(?,?,?,?,?,?,?)`, req.Title, req.Author, req.Genre, req.Language, strings.TrimSpace(req.ISBN), strings.TrimSpace(req.Publisher), req.MetadataSource)
	if err != nil {
		readingError(c, http.StatusBadRequest, err)
		return
	}
	id, _ := result.LastInsertId()
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

func (h *ReadingHandler) getReadingBook(c *gin.Context) {
	bookID, ok := readingID(c, "bookId")
	if !ok {
		return
	}
	profileID := c.DefaultQuery("profileId", "learner")
	row := h.db.QueryRowContext(c, `
		SELECT b.id,b.title,b.author,b.genre,b.language,b.isbn,b.publisher,b.cover_path,b.metadata_source,b.created_at,
		       COALESCE((SELECT ingestion_status FROM reading_assets a WHERE a.book_id=b.id AND a.asset_type='ebook' ORDER BY a.id DESC LIMIT 1),''),
		       COALESCE((SELECT id FROM reading_assets a WHERE a.book_id=b.id AND a.asset_type='cover' ORDER BY a.id DESC LIMIT 1),0),
		       p.chapter,p.page_number,p.percent,p.status,p.updated_at
		FROM reading_books b LEFT JOIN reading_progress p ON p.book_id=b.id AND p.profile_id=? WHERE b.id=?`, profileID, bookID)
	book, err := scanReadingBook(row, profileID)
	if errors.Is(err, sql.ErrNoRows) {
		readingError(c, http.StatusNotFound, errors.New("book not found"))
		return
	}
	if err != nil {
		readingError(c, http.StatusInternalServerError, err)
		return
	}
	assets, _ := h.readingAssets(c, bookID)
	sections, _ := h.readingSectionSummaries(c, bookID)
	checks, _ := h.readingChecks(c, profileID, bookID, 30)
	c.JSON(http.StatusOK, gin.H{"book": book, "assets": assets, "sections": sections, "checks": checks})
}

func (h *ReadingHandler) readingSectionSummaries(c *gin.Context, bookID int64) ([]model.ReadingSectionSummary, error) {
	rows, err := h.db.QueryContext(c, `SELECT id,ordinal,title,page_start,page_end FROM reading_sections WHERE book_id=? ORDER BY ordinal`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sections := []model.ReadingSectionSummary{}
	for rows.Next() {
		var section model.ReadingSectionSummary
		if err := rows.Scan(&section.ID, &section.Ordinal, &section.Title, &section.PageStart, &section.PageEnd); err != nil {
			return nil, err
		}
		sections = append(sections, section)
	}
	return sections, rows.Err()
}

func (h *ReadingHandler) readingAssets(c *gin.Context, bookID int64) ([]model.ReadingAsset, error) {
	rows, err := h.db.QueryContext(c, `SELECT id,book_id,asset_type,original_name,mime_type,ingestion_status,ingestion_error,created_at FROM reading_assets WHERE book_id=? ORDER BY id DESC`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	assets := []model.ReadingAsset{}
	for rows.Next() {
		var a model.ReadingAsset
		if err := rows.Scan(&a.ID, &a.BookID, &a.AssetType, &a.OriginalName, &a.MimeType, &a.IngestionStatus, &a.IngestionError, &a.CreatedAt); err != nil {
			return nil, err
		}
		assets = append(assets, a)
	}
	return assets, rows.Err()
}

func (h *ReadingHandler) saveReadingPageMap(c *gin.Context) {
	bookID, ok := readingID(c, "bookId")
	if !ok {
		return
	}
	var req struct {
		AssetID          int64  `json:"assetId" binding:"required"`
		PrintPageStart   int    `json:"printPageStart" binding:"required"`
		DigitalPageStart int    `json:"digitalPageStart" binding:"required"`
		Note             string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		readingError(c, http.StatusBadRequest, err)
		return
	}
	_, err := h.db.ExecContext(c, `
		INSERT INTO reading_page_maps(book_id,asset_id,print_page_start,digital_page_start,note)
		VALUES(?,?,?,?,?)
		ON CONFLICT(book_id,asset_id,print_page_start) DO UPDATE SET digital_page_start=excluded.digital_page_start,note=excluded.note`,
		bookID, req.AssetID, req.PrintPageStart, req.DigitalPageStart, strings.TrimSpace(req.Note))
	if err != nil {
		readingError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *ReadingHandler) listReadingSessions(c *gin.Context) {
	profileID := c.DefaultQuery("profileId", "learner")
	weekStart := strings.TrimSpace(c.Query("weekStart"))
	bookID, _ := strconv.ParseInt(c.Query("bookId"), 10, 64)
	query := `
		SELECT s.id,s.profile_id,s.book_id,s.session_date,s.chapter,s.page_from,s.page_to,s.percent,s.minutes,s.reaction,s.quick_note,s.response_mode,s.created_at,
		       b.id,b.title,b.author,b.genre,b.language,b.isbn,b.publisher,b.cover_path,b.metadata_source,b.created_at
		FROM reading_sessions s JOIN reading_books b ON b.id=s.book_id WHERE s.profile_id=?`
	args := []any{profileID}
	if weekStart != "" {
		start, err := time.Parse("2006-01-02", weekStart)
		if err != nil {
			readingError(c, http.StatusBadRequest, errors.New("weekStart must use YYYY-MM-DD"))
			return
		}
		query += ` AND s.session_date>=? AND s.session_date<?`
		args = append(args, start.Format("2006-01-02"), start.AddDate(0, 0, 7).Format("2006-01-02"))
	}
	if bookID > 0 {
		query += ` AND s.book_id=?`
		args = append(args, bookID)
	}
	query += ` ORDER BY s.session_date DESC,s.id DESC LIMIT 200`
	rows, err := h.db.QueryContext(c, query, args...)
	if err != nil {
		readingError(c, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	sessions := []model.ReadingSession{}
	for rows.Next() {
		var s model.ReadingSession
		if err := rows.Scan(&s.ID, &s.ProfileID, &s.BookID, &s.SessionDate, &s.Chapter, &s.PageFrom, &s.PageTo, &s.Percent, &s.Minutes, &s.Reaction, &s.QuickNote, &s.ResponseMode, &s.CreatedAt,
			&s.Book.ID, &s.Book.Title, &s.Book.Author, &s.Book.Genre, &s.Book.Language, &s.Book.ISBN, &s.Book.Publisher, &s.Book.CoverPath, &s.Book.MetadataSource, &s.Book.CreatedAt); err != nil {
			readingError(c, http.StatusInternalServerError, err)
			return
		}
		sessions = append(sessions, s)
	}
	c.JSON(http.StatusOK, gin.H{"sessions": sessions})
}

func (h *ReadingHandler) createReadingSession(c *gin.Context) {
	var req model.CreateReadingSessionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		readingError(c, http.StatusBadRequest, err)
		return
	}
	if req.SessionDate == "" {
		req.SessionDate = time.Now().Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", req.SessionDate); err != nil {
		readingError(c, http.StatusBadRequest, errors.New("sessionDate must use YYYY-MM-DD"))
		return
	}
	if req.ResponseMode == "" {
		req.ResponseMode = "typed"
	}
	if req.PageFrom < 0 || req.PageTo < 0 || req.Minutes < 0 || req.Percent < 0 || req.Percent > 100 {
		readingError(c, http.StatusBadRequest, errors.New("reading progress values are out of range"))
		return
	}
	tx, err := h.db.BeginTx(c, nil)
	if err != nil {
		readingError(c, http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(c, `
		INSERT INTO reading_sessions(profile_id,book_id,session_date,chapter,page_from,page_to,percent,minutes,reaction,quick_note,response_mode)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`, req.ProfileID, req.BookID, req.SessionDate, strings.TrimSpace(req.Chapter), req.PageFrom, req.PageTo, req.Percent, req.Minutes, req.Reaction, strings.TrimSpace(req.QuickNote), req.ResponseMode)
	if err != nil {
		readingError(c, http.StatusBadRequest, err)
		return
	}
	status := "reading"
	if req.Finished {
		status = "finished"
		req.Percent = 100
	}
	page := req.PageTo
	if page == 0 {
		page = req.PageFrom
	}
	_, err = tx.ExecContext(c, `
		INSERT INTO reading_progress(profile_id,book_id,chapter,page_number,percent,status,updated_at)
		VALUES(?,?,?,?,?,?,CURRENT_TIMESTAMP)
		ON CONFLICT(profile_id,book_id) DO UPDATE SET
			chapter=excluded.chapter,page_number=excluded.page_number,percent=excluded.percent,status=excluded.status,updated_at=CURRENT_TIMESTAMP`,
		req.ProfileID, req.BookID, strings.TrimSpace(req.Chapter), page, req.Percent, status)
	if err != nil {
		readingError(c, http.StatusBadRequest, err)
		return
	}
	if err := tx.Commit(); err != nil {
		readingError(c, http.StatusInternalServerError, err)
		return
	}
	id, _ := result.LastInsertId()
	c.JSON(http.StatusCreated, gin.H{"id": id, "status": status})
}

func (h *ReadingHandler) createReadingCheck(c *gin.Context) {
	var req model.CreateReadingCheckReq
	if err := c.ShouldBindJSON(&req); err != nil {
		readingError(c, http.StatusBadRequest, err)
		return
	}
	if req.Tier != "session" && req.Tier != "chapter" && req.Tier != "book" {
		readingError(c, http.StatusBadRequest, errors.New("tier must be session, chapter, or book"))
		return
	}
	check, err := h.buildReadingCheck(c, req)
	if err != nil {
		readingError(c, http.StatusInternalServerError, err)
		return
	}
	questionsJSON, _ := json.Marshal(check.Questions)
	sectionsJSON, _ := json.Marshal(checkSourceSectionIDs(check))
	result, err := h.db.ExecContext(c, `
		INSERT INTO reading_checks(profile_id,book_id,session_id,tier,skill_focus,grounded,source_label,source_section_ids,questions_json,provider_name,prompt_version,content_revision)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, req.ProfileID, req.BookID, nullableReadingID(req.SessionID), req.Tier, check.SkillFocus, check.Grounded, check.SourceLabel, string(sectionsJSON), string(questionsJSON), check.ProviderName, readingPromptVersion, check.ContentRevision)
	if err != nil {
		readingError(c, http.StatusBadRequest, err)
		return
	}
	check.ID, _ = result.LastInsertId()
	check.ProfileID = req.ProfileID
	check.BookID = req.BookID
	check.SessionID = req.SessionID
	check.Tier = req.Tier
	check.PromptVersion = readingPromptVersion
	c.JSON(http.StatusCreated, gin.H{"check": check})
}

func checkSourceSectionIDs(check model.ReadingCheck) []int64 {
	ids := make([]int64, 0, len(check.Questions))
	seen := map[int64]bool{}
	for _, q := range check.Questions {
		for _, raw := range q.Evidence {
			if !strings.HasPrefix(raw, "section:") {
				continue
			}
			id, _ := strconv.ParseInt(strings.TrimPrefix(raw, "section:"), 10, 64)
			if id > 0 && !seen[id] {
				ids = append(ids, id)
				seen[id] = true
			}
		}
	}
	return ids
}

func (h *ReadingHandler) respondReadingCheck(c *gin.Context) {
	checkID, ok := readingID(c, "checkId")
	if !ok {
		return
	}
	var req model.RespondReadingCheckReq
	if err := c.ShouldBindJSON(&req); err != nil {
		readingError(c, http.StatusBadRequest, err)
		return
	}
	check, sectionIDs, err := h.loadReadingCheck(c, checkID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		readingError(c, status, err)
		return
	}
	feedback, provider := h.evaluateReadingResponses(c, check, sectionIDs, req.Responses)
	responsesJSON, _ := json.Marshal(req.Responses)
	feedbackJSON, _ := json.Marshal(feedback)
	if req.ResponseMode == "" {
		req.ResponseMode = "typed"
	}
	tx, err := h.db.BeginTx(c, nil)
	if err != nil {
		readingError(c, http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(c, `UPDATE reading_checks SET responses_json=?,feedback_json=?,response_mode=?,provider_name=CASE WHEN ?<>'' THEN ? ELSE provider_name END,updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		string(responsesJSON), string(feedbackJSON), req.ResponseMode, provider, provider, checkID); err != nil {
		readingError(c, http.StatusInternalServerError, err)
		return
	}
	for _, item := range feedback {
		state := normalizedReadingState(item.SuggestedState)
		if _, err := tx.ExecContext(c, `
			INSERT INTO reading_skill_observations(check_id,profile_id,skill,suggested_state)
			VALUES(?,?,?,?)
			ON CONFLICT(check_id,skill) DO UPDATE SET suggested_state=excluded.suggested_state`, checkID, check.ProfileID, item.Skill, state); err != nil {
			readingError(c, http.StatusInternalServerError, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		readingError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"feedback": feedback})
}

func (h *ReadingHandler) confirmReadingObservation(c *gin.Context) {
	checkID, ok := readingID(c, "checkId")
	if !ok {
		return
	}
	var req struct {
		Skill       string `json:"skill" binding:"required"`
		State       string `json:"state" binding:"required"`
		ConfirmedBy string `json:"confirmedBy"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		readingError(c, http.StatusBadRequest, err)
		return
	}
	state := normalizedReadingState(req.State)
	if state != req.State {
		readingError(c, http.StatusBadRequest, errors.New("state must be clear, developing, or revisit"))
		return
	}
	if req.ConfirmedBy == "" {
		req.ConfirmedBy = "Parent"
	}
	result, err := h.db.ExecContext(c, `UPDATE reading_skill_observations SET confirmed_state=?,confirmed_by=?,confirmed_at=CURRENT_TIMESTAMP WHERE check_id=? AND skill=?`, state, req.ConfirmedBy, checkID, req.Skill)
	if err != nil {
		readingError(c, http.StatusInternalServerError, err)
		return
	}
	if n, _ := result.RowsAffected(); n == 0 {
		readingError(c, http.StatusNotFound, errors.New("skill observation not found"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *ReadingHandler) getReadingWeeklyReview(c *gin.Context) {
	profileID := c.DefaultQuery("profileId", "learner")
	weekStart := c.Query("weekStart")
	if weekStart == "" {
		weekStart = readingWeekStart(time.Now()).Format("2006-01-02")
	}
	start, err := time.Parse("2006-01-02", weekStart)
	if err != nil {
		readingError(c, http.StatusBadRequest, errors.New("weekStart must use YYYY-MM-DD"))
		return
	}
	var review model.ReadingWeeklyReview
	review.ProfileID, review.WeekStart = profileID, weekStart
	_ = h.db.QueryRowContext(c, `SELECT reflection,next_goal,reviewer FROM reading_weekly_reviews WHERE profile_id=? AND week_start=?`, profileID, weekStart).
		Scan(&review.Reflection, &review.NextGoal, &review.Reviewer)
	rows, err := h.db.QueryContext(c, `
		SELECT s.session_date,COUNT(*),COALESCE(SUM(s.minutes),0),GROUP_CONCAT(DISTINCT b.title)
		FROM reading_sessions s JOIN reading_books b ON b.id=s.book_id
		WHERE s.profile_id=? AND s.session_date>=? AND s.session_date<? GROUP BY s.session_date ORDER BY s.session_date`,
		profileID, weekStart, start.AddDate(0, 0, 7).Format("2006-01-02"))
	if err != nil {
		readingError(c, http.StatusInternalServerError, err)
		return
	}
	days := []gin.H{}
	totalSessions, totalMinutes := 0, 0
	for rows.Next() {
		var date, titles string
		var sessions, minutes int
		if err := rows.Scan(&date, &sessions, &minutes, &titles); err != nil {
			rows.Close()
			readingError(c, http.StatusInternalServerError, err)
			return
		}
		days = append(days, gin.H{"date": date, "sessions": sessions, "minutes": minutes, "books": strings.Split(titles, ",")})
		totalSessions += sessions
		totalMinutes += minutes
	}
	rows.Close()
	skills, _ := h.readingSkillSummary(c, profileID)
	c.JSON(http.StatusOK, gin.H{
		"review":  review,
		"summary": gin.H{"days": days, "daysRead": len(days), "sessions": totalSessions, "minutes": totalMinutes, "skills": skills},
	})
}

func (h *ReadingHandler) putReadingWeeklyReview(c *gin.Context) {
	var req model.ReadingWeeklyReview
	if err := c.ShouldBindJSON(&req); err != nil {
		readingError(c, http.StatusBadRequest, err)
		return
	}
	if req.ProfileID == "" {
		req.ProfileID = "learner"
	}
	if req.WeekStart == "" {
		req.WeekStart = readingWeekStart(time.Now()).Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", req.WeekStart); err != nil {
		readingError(c, http.StatusBadRequest, errors.New("weekStart must use YYYY-MM-DD"))
		return
	}
	_, err := h.db.ExecContext(c, `
		INSERT INTO reading_weekly_reviews(profile_id,week_start,reflection,next_goal,reviewer)
		VALUES(?,?,?,?,?)
		ON CONFLICT(profile_id,week_start) DO UPDATE SET reflection=excluded.reflection,next_goal=excluded.next_goal,reviewer=excluded.reviewer,updated_at=CURRENT_TIMESTAMP`,
		req.ProfileID, req.WeekStart, strings.TrimSpace(req.Reflection), strings.TrimSpace(req.NextGoal), strings.TrimSpace(req.Reviewer))
	if err != nil {
		readingError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *ReadingHandler) readingSkillSummary(c *gin.Context, profileID string) ([]gin.H, error) {
	rows, err := h.db.QueryContext(c, `
		SELECT skill,
		       SUM(CASE COALESCE(NULLIF(confirmed_state,''),suggested_state) WHEN 'clear' THEN 1 ELSE 0 END),
		       SUM(CASE COALESCE(NULLIF(confirmed_state,''),suggested_state) WHEN 'developing' THEN 1 ELSE 0 END),
		       SUM(CASE COALESCE(NULLIF(confirmed_state,''),suggested_state) WHEN 'revisit' THEN 1 ELSE 0 END)
		FROM reading_skill_observations WHERE profile_id=? GROUP BY skill ORDER BY MAX(created_at) DESC`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []gin.H{}
	for rows.Next() {
		var skill string
		var clear, developing, revisit int
		if err := rows.Scan(&skill, &clear, &developing, &revisit); err != nil {
			return nil, err
		}
		state := "developing"
		if clear > developing+revisit {
			state = "clear"
		} else if revisit > clear {
			state = "revisit"
		}
		items = append(items, gin.H{"skill": skill, "state": state, "clear": clear, "developing": developing, "revisit": revisit})
	}
	return items, rows.Err()
}

func (h *ReadingHandler) readingChecks(c *gin.Context, profileID string, bookID int64, limit int) ([]model.ReadingCheck, error) {
	rows, err := h.db.QueryContext(c, `
		SELECT id,profile_id,book_id,COALESCE(session_id,0),tier,skill_focus,grounded,source_label,questions_json,responses_json,feedback_json,provider_name,prompt_version,content_revision,created_at
		FROM reading_checks WHERE profile_id=? AND book_id=? ORDER BY id DESC LIMIT ?`, profileID, bookID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	checks := []model.ReadingCheck{}
	for rows.Next() {
		check, err := scanReadingCheck(rows)
		if err != nil {
			return nil, err
		}
		checks = append(checks, check)
	}
	return checks, rows.Err()
}

func scanReadingCheck(row readingBookScanner) (model.ReadingCheck, error) {
	var check model.ReadingCheck
	var grounded int
	var questions, responses, feedback string
	err := row.Scan(&check.ID, &check.ProfileID, &check.BookID, &check.SessionID, &check.Tier, &check.SkillFocus, &grounded, &check.SourceLabel,
		&questions, &responses, &feedback, &check.ProviderName, &check.PromptVersion, &check.ContentRevision, &check.CreatedAt)
	if err != nil {
		return check, err
	}
	check.Grounded = grounded == 1
	_ = json.Unmarshal([]byte(questions), &check.Questions)
	_ = json.Unmarshal([]byte(responses), &check.Responses)
	_ = json.Unmarshal([]byte(feedback), &check.Feedback)
	return check, nil
}

func (h *ReadingHandler) loadReadingCheck(c *gin.Context, checkID int64) (model.ReadingCheck, []int64, error) {
	row := h.db.QueryRowContext(c, `
		SELECT id,profile_id,book_id,COALESCE(session_id,0),tier,skill_focus,grounded,source_label,questions_json,responses_json,feedback_json,provider_name,prompt_version,content_revision,created_at
		FROM reading_checks WHERE id=?`, checkID)
	check, err := scanReadingCheck(row)
	if err != nil {
		return check, nil, err
	}
	var raw string
	if err := h.db.QueryRowContext(c, `SELECT source_section_ids FROM reading_checks WHERE id=?`, checkID).Scan(&raw); err != nil {
		return check, nil, err
	}
	ids := []int64{}
	_ = json.Unmarshal([]byte(raw), &ids)
	return check, ids, nil
}

func normalizedReadingState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "clear":
		return "clear"
	case "revisit":
		return "revisit"
	default:
		return "developing"
	}
}

func nullableReadingID(id int64) any {
	if id <= 0 {
		return nil
	}
	return id
}

func readingWeekStart(t time.Time) time.Time {
	weekday := (int(t.Weekday()) + 6) % 7
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).AddDate(0, 0, -weekday)
}

func readingID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		readingError(c, http.StatusBadRequest, errors.New("invalid "+name))
		return 0, false
	}
	return id, true
}

func readingError(c *gin.Context, status int, err error) {
	c.JSON(status, model.APIError{Error: err.Error()})
}
