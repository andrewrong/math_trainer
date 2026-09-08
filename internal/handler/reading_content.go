package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/html"

	"mathtrainer/internal/ai"
	"mathtrainer/internal/model"
)

const (
	readingParseVersion = "reading-content-v1"
	maxReadingUpload    = 120 << 20
	maxReadingEvidence  = 14000
	maxReadingOCRResult = 64 << 20
)

type readingSection struct {
	ID        int64
	Ordinal   int
	Title     string
	PageStart int
	PageEnd   int
	Body      string
}

func (h *ReadingHandler) importReadingBook(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxReadingUpload)
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		readingError(c, http.StatusBadRequest, errors.New("choose a PDF or EPUB"))
		return
	}
	defer file.Close()
	path, mimeType, digest, err := h.storeReadingFile(header.Filename, file, "ebook")
	if err != nil {
		readingError(c, http.StatusBadRequest, err)
		return
	}
	var existingBookID int64
	if err := h.db.QueryRowContext(c, `SELECT book_id FROM reading_assets WHERE sha256=? LIMIT 1`, digest).Scan(&existingBookID); err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "This book file is already in the library.", "bookId": existingBookID})
		return
	}
	sections, metadata, err := h.parseReadingDocument(c, path)
	if err != nil {
		_ = os.Remove(path)
		readingError(c, http.StatusUnprocessableEntity, fmt.Errorf("analyse book: %w", err))
		return
	}
	metadata.Title = strings.TrimSpace(metadata.Title)
	if metadata.Title == "" {
		metadata.Title = strings.TrimSpace(strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename)))
	}
	metadata.Author = strings.TrimSpace(metadata.Author)
	metadata.Genre = strings.ToLower(strings.TrimSpace(metadata.Genre))
	if metadata.Genre != "fiction" && metadata.Genre != "informational" && metadata.Genre != "other" {
		metadata.Genre = "other"
	}
	metadata.Language = strings.TrimSpace(metadata.Language)
	if metadata.Language == "" {
		metadata.Language = "en"
	}
	metadata.ISBN = strings.TrimSpace(metadata.ISBN)
	metadata.Publisher = strings.TrimSpace(metadata.Publisher)

	tx, err := h.db.BeginTx(c, nil)
	if err != nil {
		_ = os.Remove(path)
		readingError(c, http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()
	bookResult, err := tx.ExecContext(c, `
		INSERT INTO reading_books(title,author,genre,language,isbn,publisher,metadata_source)
		VALUES(?,?,?,?,?,?,?)`, metadata.Title, metadata.Author, metadata.Genre, metadata.Language, metadata.ISBN, metadata.Publisher, "ebook-auto")
	if err != nil {
		_ = os.Remove(path)
		readingError(c, http.StatusBadRequest, err)
		return
	}
	bookID, _ := bookResult.LastInsertId()
	assetResult, err := tx.ExecContext(c, `
		INSERT INTO reading_assets(book_id,asset_type,source_path,original_name,mime_type,sha256,ingestion_status)
		VALUES(?,'ebook',?,?,?,?, 'processing')`, bookID, path, header.Filename, mimeType, digest)
	if err != nil {
		_ = os.Remove(path)
		readingError(c, http.StatusBadRequest, err)
		return
	}
	assetID, _ := assetResult.LastInsertId()
	if err := tx.Commit(); err != nil {
		_ = os.Remove(path)
		readingError(c, http.StatusInternalServerError, err)
		return
	}
	if err := h.indexReadingDocument(c, assetID, bookID, sections, metadata); err != nil {
		_, _ = h.db.ExecContext(c, `DELETE FROM reading_books WHERE id=?`, bookID)
		_ = os.Remove(path)
		readingError(c, http.StatusInternalServerError, fmt.Errorf("index book: %w", err))
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"id": bookID, "assetId": assetID, "status": "ready", "book": metadata,
		"sections": len(sections), "requiresConfirmation": false,
	})
}

func (h *ReadingHandler) uploadReadingAsset(c *gin.Context) {
	bookID, ok := readingID(c, "bookId")
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxReadingUpload)
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		readingError(c, http.StatusBadRequest, errors.New("choose a PDF, EPUB, or book image"))
		return
	}
	defer file.Close()
	assetType := strings.ToLower(strings.TrimSpace(c.DefaultQuery("assetType", "ebook")))
	if assetType != "ebook" && assetType != "cover" && assetType != "copyright" {
		readingError(c, http.StatusBadRequest, errors.New("assetType must be ebook, cover, or copyright"))
		return
	}
	path, mimeType, digest, err := h.storeReadingFile(header.Filename, file, assetType)
	if err != nil {
		readingError(c, http.StatusBadRequest, err)
		return
	}
	var existing int64
	if digest != "" {
		err = h.db.QueryRowContext(c, `SELECT id FROM reading_assets WHERE sha256=? LIMIT 1`, digest).Scan(&existing)
		if err == nil {
			readingError(c, http.StatusConflict, fmt.Errorf("this file is already attached as asset %d", existing))
			return
		}
	}
	status := "ready"
	if assetType == "ebook" {
		status = "queued"
	}
	result, err := h.db.ExecContext(c, `
		INSERT INTO reading_assets(book_id,asset_type,source_path,original_name,mime_type,sha256,ingestion_status)
		VALUES(?,?,?,?,?,?,?)`, bookID, assetType, path, header.Filename, mimeType, digest, status)
	if err != nil {
		readingError(c, http.StatusBadRequest, err)
		return
	}
	assetID, _ := result.LastInsertId()
	if assetType == "cover" {
		_, _ = h.db.ExecContext(c, `UPDATE reading_books SET cover_path=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, path, bookID)
	}
	if assetType == "ebook" {
		if err := h.ingestReadingAsset(c, assetID, bookID, path); err != nil {
			_, _ = h.db.ExecContext(c, `UPDATE reading_assets SET ingestion_status='failed',ingestion_error=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, err.Error(), assetID)
			c.JSON(http.StatusAccepted, gin.H{"assetId": assetID, "status": "failed", "error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusCreated, gin.H{"assetId": assetID, "status": "ready"})
}

func (h *ReadingHandler) serveReadingAssetImage(c *gin.Context) {
	assetID, ok := readingID(c, "assetId")
	if !ok {
		return
	}
	var path, mimeType, assetType string
	if err := h.db.QueryRowContext(c, `SELECT source_path,mime_type,asset_type FROM reading_assets WHERE id=?`, assetID).Scan(&path, &mimeType, &assetType); err != nil {
		readingError(c, http.StatusNotFound, errors.New("image not found"))
		return
	}
	if assetType != "cover" && assetType != "copyright" {
		readingError(c, http.StatusForbidden, errors.New("ebook content is not exposed by the reading tracker"))
		return
	}
	c.Header("Content-Type", mimeType)
	c.Header("Content-Disposition", "inline")
	c.File(path)
}

func (h *ReadingHandler) storeReadingFile(name string, src io.Reader, assetType string) (string, string, string, error) {
	ext := strings.ToLower(filepath.Ext(name))
	allowed := map[string]string{
		".pdf":  "application/pdf",
		".epub": "application/epub+zip",
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".png":  "image/png",
		".webp": "image/webp",
	}
	mimeType, ok := allowed[ext]
	if !ok || (assetType == "ebook" && ext != ".pdf" && ext != ".epub") || (assetType != "ebook" && ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp") {
		return "", "", "", errors.New("unsupported file type")
	}
	contentDir := h.cfg.ContentDir
	if contentDir == "" {
		contentDir = "./resources/reading"
	}
	dir := filepath.Join(contentDir, assetType)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", "", err
	}
	tmp, err := os.CreateTemp(dir, ".reading-upload-*")
	if err != nil {
		return "", "", "", err
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(src, maxReadingUpload+1))
	if err != nil {
		return "", "", "", err
	}
	if written > maxReadingUpload {
		return "", "", "", errors.New("file exceeds 120 MB")
	}
	if err := tmp.Close(); err != nil {
		return "", "", "", err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	finalPath := filepath.Join(dir, digest+ext)
	if _, err := os.Stat(finalPath); errors.Is(err, os.ErrNotExist) {
		if err := os.Rename(tmpPath, finalPath); err != nil {
			return "", "", "", err
		}
	}
	return finalPath, mimeType, digest, nil
}

func (h *ReadingHandler) ingestReadingAsset(ctx context.Context, assetID, bookID int64, path string) error {
	if _, err := h.db.ExecContext(ctx, `UPDATE reading_assets SET ingestion_status='processing',ingestion_error='',updated_at=CURRENT_TIMESTAMP WHERE id=?`, assetID); err != nil {
		return err
	}
	sections, metadata, err := h.parseReadingDocument(ctx, path)
	if err != nil {
		return err
	}
	return h.indexReadingDocument(ctx, assetID, bookID, sections, metadata)
}

func (h *ReadingHandler) parseReadingDocument(ctx context.Context, path string) ([]readingSection, model.BookCandidate, error) {
	var sections []readingSection
	var metadata model.BookCandidate
	var err error
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf":
		sections, err = parseReadingPDF(ctx, path)
		if err != nil || len(sections) == 0 {
			var ocr readingOCRResult
			ocr, err = h.callReadingOCRPDF(ctx, path)
			if err == nil {
				body := normalizeReadingText(ocr.Markdown)
				if body != "" {
					sections = []readingSection{{Ordinal: 1, Title: "OCR text", PageStart: 1, PageEnd: ocr.Pages, Body: body}}
				}
			}
		}
	case ".epub":
		sections, metadata, err = parseReadingEPUB(path)
	default:
		err = errors.New("only PDF and EPUB can be indexed")
	}
	if err != nil {
		return nil, model.BookCandidate{}, err
	}
	if len(sections) == 0 {
		return nil, model.BookCandidate{}, errors.New("no readable text was found")
	}
	if metadata.Title == "" || metadata.Author == "" || metadata.Genre == "" {
		parts := make([]string, 0, min(6, len(sections)))
		for _, section := range sections {
			parts = append(parts, section.Title+"\n"+section.Body)
			if len(strings.Join(parts, "\n\n")) >= 12000 || len(parts) >= 6 {
				break
			}
		}
		inferred := model.BookCandidate{RawText: truncateReading(strings.Join(parts, "\n\n"), 12000)}
		if h.ai != nil {
			inferred = h.extractBookMetadataFromText(ctx, inferred)
		}
		if metadata.Title == "" {
			metadata.Title = inferred.Title
		}
		if metadata.Author == "" {
			metadata.Author = inferred.Author
		}
		if metadata.Genre == "" {
			metadata.Genre = inferred.Genre
		}
		if metadata.Language == "" {
			metadata.Language = inferred.Language
		}
		if metadata.ISBN == "" {
			metadata.ISBN = inferred.ISBN
		}
		if metadata.Publisher == "" {
			metadata.Publisher = inferred.Publisher
		}
	}
	return sections, metadata, nil
}

func (h *ReadingHandler) indexReadingDocument(ctx context.Context, assetID, bookID int64, sections []readingSection, metadata model.BookCandidate) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM reading_sections_fts WHERE section_id IN (SELECT id FROM reading_sections WHERE asset_id=?)`, assetID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM reading_sections WHERE asset_id=?`, assetID); err != nil {
		return err
	}
	for _, section := range sections {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO reading_sections(book_id,asset_id,ordinal,title,page_start,page_end,body)
			VALUES(?,?,?,?,?,?,?)`, bookID, assetID, section.Ordinal, section.Title, section.PageStart, section.PageEnd, section.Body)
		if err != nil {
			return err
		}
		sectionID, _ := result.LastInsertId()
		if _, err := tx.ExecContext(ctx, `INSERT INTO reading_sections_fts(section_id,book_id,title,body) VALUES(?,?,?,?)`, sectionID, bookID, section.Title, section.Body); err != nil {
			return err
		}
	}
	if metadata.Title != "" || metadata.Author != "" {
		_, err = tx.ExecContext(ctx, `
			UPDATE reading_books SET
				title=CASE WHEN trim(title)='' THEN ? ELSE title END,
				author=CASE WHEN trim(author)='' THEN ? ELSE author END,
				publisher=CASE WHEN trim(publisher)='' THEN ? ELSE publisher END,
				language=CASE WHEN trim(language)='' THEN ? ELSE language END,
				updated_at=CURRENT_TIMESTAMP WHERE id=?`, metadata.Title, metadata.Author, metadata.Publisher, metadata.Language, bookID)
		if err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE reading_assets SET ingestion_status='ready',parse_version=?,ingestion_error='',updated_at=CURRENT_TIMESTAMP WHERE id=?`, readingParseVersion, assetID); err != nil {
		return err
	}
	return tx.Commit()
}

func parseReadingPDF(ctx context.Context, path string) ([]readingSection, error) {
	cmd := exec.CommandContext(ctx, "pdftotext", "-layout", path, "-")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("extract PDF text: %w", err)
	}
	pages := strings.Split(string(output), "\f")
	sections := []readingSection{}
	for i, page := range pages {
		body := normalizeReadingText(page)
		if len(body) < 20 {
			continue
		}
		sections = append(sections, readingSection{Ordinal: i + 1, Title: fmt.Sprintf("Page %d", i+1), PageStart: i + 1, PageEnd: i + 1, Body: body})
	}
	return sections, nil
}

type epubContainer struct {
	Rootfiles []struct {
		FullPath string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}

type epubPackage struct {
	Metadata struct {
		Titles     []epubTitle `xml:"title"`
		Meta       []epubMeta  `xml:"meta"`
		Creator    string      `xml:"creator"`
		Language   string      `xml:"language"`
		Publisher  string      `xml:"publisher"`
		Identifier string      `xml:"identifier"`
	} `xml:"metadata"`
	Manifest []struct {
		ID        string `xml:"id,attr"`
		Href      string `xml:"href,attr"`
		MediaType string `xml:"media-type,attr"`
	} `xml:"manifest>item"`
	Spine []struct {
		IDRef string `xml:"idref,attr"`
	} `xml:"spine>itemref"`
}

type epubTitle struct {
	ID    string `xml:"id,attr"`
	Value string `xml:",chardata"`
}

type epubMeta struct {
	Property string `xml:"property,attr"`
	Refines  string `xml:"refines,attr"`
	Value    string `xml:",chardata"`
}

func parseReadingEPUB(path string) ([]readingSection, model.BookCandidate, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, model.BookCandidate{}, fmt.Errorf("open EPUB: %w", err)
	}
	defer reader.Close()
	files := map[string]*zip.File{}
	for _, file := range reader.File {
		files[filepath.ToSlash(file.Name)] = file
	}
	containerBytes, err := readZipFile(files["META-INF/container.xml"])
	if err != nil {
		return nil, model.BookCandidate{}, errors.New("EPUB is missing META-INF/container.xml")
	}
	var container epubContainer
	if err := xml.Unmarshal(containerBytes, &container); err != nil || len(container.Rootfiles) == 0 {
		return nil, model.BookCandidate{}, errors.New("EPUB package path is invalid")
	}
	opfPath := filepath.ToSlash(container.Rootfiles[0].FullPath)
	opfBytes, err := readZipFile(files[opfPath])
	if err != nil {
		return nil, model.BookCandidate{}, errors.New("EPUB package document is missing")
	}
	var pkg epubPackage
	if err := xml.Unmarshal(opfBytes, &pkg); err != nil {
		return nil, model.BookCandidate{}, fmt.Errorf("parse EPUB package: %w", err)
	}
	metadata := model.BookCandidate{
		Title: epubMainTitle(pkg.Metadata.Titles, pkg.Metadata.Meta), Author: strings.TrimSpace(pkg.Metadata.Creator),
		Language: strings.TrimSpace(pkg.Metadata.Language), Publisher: strings.TrimSpace(pkg.Metadata.Publisher), ISBN: strings.TrimSpace(pkg.Metadata.Identifier),
	}
	manifest := map[string]struct{ href, media string }{}
	for _, item := range pkg.Manifest {
		manifest[item.ID] = struct{ href, media string }{item.Href, item.MediaType}
	}
	base := filepath.ToSlash(filepath.Dir(opfPath))
	sections := []readingSection{}
	for _, item := range pkg.Spine {
		entry, ok := manifest[item.IDRef]
		if !ok || (!strings.Contains(entry.media, "html") && !strings.HasSuffix(strings.ToLower(entry.href), ".xhtml") && !strings.HasSuffix(strings.ToLower(entry.href), ".html")) {
			continue
		}
		entryPath := filepath.ToSlash(filepath.Clean(filepath.Join(base, entry.href)))
		bodyBytes, err := readZipFile(files[entryPath])
		if err != nil {
			continue
		}
		title, body := extractReadingHTML(bodyBytes)
		title = cleanReadingSectionTitle(title, metadata.Title)
		body = normalizeReadingText(body)
		if len(body) < 20 {
			continue
		}
		if title == "" {
			title = fmt.Sprintf("Section %d", len(sections)+1)
		}
		sections = append(sections, readingSection{Ordinal: len(sections) + 1, Title: title, Body: body})
	}
	return sections, metadata, nil
}

func epubMainTitle(titles []epubTitle, metadata []epubMeta) string {
	mainID := ""
	for _, item := range metadata {
		if strings.EqualFold(strings.TrimSpace(item.Property), "title-type") && strings.EqualFold(strings.TrimSpace(item.Value), "main") {
			mainID = strings.TrimPrefix(strings.TrimSpace(item.Refines), "#")
			break
		}
	}
	if mainID != "" {
		for _, title := range titles {
			if title.ID == mainID && strings.TrimSpace(title.Value) != "" {
				return strings.TrimSpace(title.Value)
			}
		}
	}
	for _, title := range titles {
		if strings.TrimSpace(title.Value) != "" {
			return strings.TrimSpace(title.Value)
		}
	}
	return ""
}

func cleanReadingSectionTitle(title, bookTitle string) string {
	title = strings.TrimSpace(title)
	bookTitle = strings.TrimSpace(bookTitle)
	if title == "" || bookTitle == "" {
		return title
	}
	lowerTitle := strings.ToLower(title)
	for _, separator := range []string{", ", " | ", " — ", " - ", ": "} {
		suffix := separator + strings.ToLower(bookTitle)
		if strings.HasSuffix(lowerTitle, suffix) {
			return strings.TrimSpace(title[:len(title)-len(suffix)])
		}
	}
	return title
}

func readZipFile(file *zip.File) ([]byte, error) {
	if file == nil {
		return nil, os.ErrNotExist
	}
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(io.LimitReader(reader, 20<<20))
}

func extractReadingHTML(data []byte) (string, string) {
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return "", ""
	}
	var title string
	parts := []string{}
	var walk func(*html.Node, bool)
	walk = func(node *html.Node, skip bool) {
		if node.Type == html.ElementNode {
			name := strings.ToLower(node.Data)
			if name == "script" || name == "style" || name == "svg" || name == "nav" {
				skip = true
			}
		}
		if node.Type == html.TextNode && !skip {
			value := strings.TrimSpace(node.Data)
			if value != "" {
				if title == "" && node.Parent != nil && (node.Parent.Data == "h1" || node.Parent.Data == "h2" || node.Parent.Data == "title") {
					title = value
				}
				parts = append(parts, value)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child, skip)
		}
	}
	walk(doc, false)
	return title, strings.Join(parts, " ")
}

func normalizeReadingText(value string) string {
	value = strings.ReplaceAll(value, "\x00", " ")
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return ' '
		}
		return r
	}, value)
	return strings.TrimSpace(regexp.MustCompile(`[ \t\r\n]+`).ReplaceAllString(value, " "))
}

func (h *ReadingHandler) scanReadingLibrary(c *gin.Context) {
	if len(h.cfg.LibraryDirs) == 0 {
		c.JSON(http.StatusOK, gin.H{"imported": 0, "skipped": 0, "message": "No reading.library_dirs are configured."})
		return
	}
	imported, skipped := 0, 0
	errorsFound := []string{}
	for _, configured := range h.cfg.LibraryDirs {
		root, err := filepath.Abs(configured)
		if err != nil {
			errorsFound = append(errorsFound, err.Error())
			continue
		}
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			errorsFound = append(errorsFound, "Cannot read "+configured)
			continue
		}
		err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if ext != ".pdf" && ext != ".epub" {
				return nil
			}
			digest, err := hashReadingFile(path)
			if err != nil {
				errorsFound = append(errorsFound, entry.Name()+": "+err.Error())
				return nil
			}
			var existing int
			if err := h.db.QueryRowContext(c, `SELECT COUNT(*) FROM reading_assets WHERE sha256=?`, digest).Scan(&existing); err == nil && existing > 0 {
				skipped++
				return nil
			}
			title := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
			bookResult, err := h.db.ExecContext(c, `INSERT INTO reading_books(title,genre,language,metadata_source) VALUES(?,'other','en','ebook')`, title)
			if err != nil {
				errorsFound = append(errorsFound, entry.Name()+": "+err.Error())
				return nil
			}
			bookID, _ := bookResult.LastInsertId()
			mimeType := "application/pdf"
			if ext == ".epub" {
				mimeType = "application/epub+zip"
			}
			assetResult, err := h.db.ExecContext(c, `INSERT INTO reading_assets(book_id,asset_type,source_path,original_name,mime_type,sha256,ingestion_status) VALUES(?,'ebook',?,?,?,?, 'queued')`, bookID, path, entry.Name(), mimeType, digest)
			if err != nil {
				errorsFound = append(errorsFound, entry.Name()+": "+err.Error())
				return nil
			}
			assetID, _ := assetResult.LastInsertId()
			if err := h.ingestReadingAsset(c, assetID, bookID, path); err != nil {
				_, _ = h.db.ExecContext(c, `UPDATE reading_assets SET ingestion_status='failed',ingestion_error=? WHERE id=?`, err.Error(), assetID)
				errorsFound = append(errorsFound, entry.Name()+": "+err.Error())
			} else {
				imported++
			}
			return nil
		})
		if err != nil {
			errorsFound = append(errorsFound, err.Error())
		}
	}
	c.JSON(http.StatusOK, gin.H{"imported": imported, "skipped": skipped, "errors": errorsFound})
}

func hashReadingFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (h *ReadingHandler) recognizeReadingBook(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 24<<20)
	cover, coverHeader, err := c.Request.FormFile("cover")
	if err != nil {
		readingError(c, http.StatusBadRequest, errors.New("choose a cover image"))
		return
	}
	defer cover.Close()
	coverBytes, err := io.ReadAll(io.LimitReader(cover, 12<<20))
	if err != nil {
		readingError(c, http.StatusBadRequest, err)
		return
	}
	var copyrightBytes []byte
	var copyrightName string
	if extra, header, extraErr := c.Request.FormFile("copyright"); extraErr == nil {
		defer extra.Close()
		copyrightBytes, _ = io.ReadAll(io.LimitReader(extra, 12<<20))
		copyrightName = header.Filename
	}
	candidate, err := h.callReadingOCR(c, coverBytes, coverHeader.Filename, copyrightBytes, copyrightName)
	if err != nil {
		readingError(c, http.StatusServiceUnavailable, err)
		return
	}
	if candidate.Genre == "" {
		candidate.Genre = "fiction"
	}
	if candidate.Language == "" {
		candidate.Language = "en"
	}
	c.JSON(http.StatusOK, gin.H{"candidate": candidate, "requiresConfirmation": true})
}

func (h *ReadingHandler) callReadingOCR(ctx context.Context, cover []byte, coverName string, copyright []byte, copyrightName string) (model.BookCandidate, error) {
	coverResult, err := h.callReadingOCRImage(ctx, cover, coverName)
	if err != nil {
		return model.BookCandidate{}, fmt.Errorf("recognise book cover: %w", err)
	}
	parts := []string{"## Book cover\n" + coverResult.Markdown}
	if len(copyright) > 0 {
		copyrightResult, copyrightErr := h.callReadingOCRImage(ctx, copyright, copyrightName)
		if copyrightErr != nil {
			return model.BookCandidate{}, fmt.Errorf("recognise copyright page: %w", copyrightErr)
		}
		parts = append(parts, "## Copyright page\n"+copyrightResult.Markdown)
	}
	candidate := model.BookCandidate{RawText: strings.Join(parts, "\n\n")}
	if h.ai != nil {
		candidate = h.extractBookMetadataFromText(ctx, candidate)
	}
	if candidate.ISBN == "" {
		candidate.ISBN = readingISBNFromText(candidate.RawText)
	}
	if candidate.Title == "" && candidate.RawText == "" {
		return model.BookCandidate{}, errors.New("OCR response did not contain recognised text")
	}
	return candidate, nil
}

type readingOCRResult struct {
	Markdown   string `json:"markdown"`
	Engine     string `json:"engine"`
	Pages      int    `json:"pages"`
	DurationMS int64  `json:"duration_ms"`
}

func (h *ReadingHandler) callReadingOCRImage(ctx context.Context, image []byte, filename string) (readingOCRResult, error) {
	if strings.TrimSpace(filename) == "" {
		filename = "book-cover.jpg"
	}
	return h.callReadingOCRFile(ctx, image, filename, []string{"/v1/ocr/image", "/ocr/image"})
}

func (h *ReadingHandler) callReadingOCRPDF(ctx context.Context, path string) (readingOCRResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return readingOCRResult{}, fmt.Errorf("read PDF for OCR: %w", err)
	}
	return h.callReadingOCRFile(ctx, data, filepath.Base(path), []string{"/v1/ocr/pdf", "/ocr/pdf"})
}

func (h *ReadingHandler) callReadingOCRFile(ctx context.Context, data []byte, filename string, endpoints []string) (readingOCRResult, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(h.cfg.OCR.BaseURL), "/")
	if baseURL == "" {
		return readingOCRResult{}, errors.New("OCR service is not configured")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return readingOCRResult{}, errors.New("OCR base URL must be an absolute HTTP or HTTPS URL without a query or fragment")
	}
	engine := strings.ToLower(strings.TrimSpace(h.cfg.OCR.Engine))
	if engine == "" {
		engine = "auto"
	}
	if engine != "auto" && engine != "paddle" && engine != "glm" && engine != "qwen" {
		return readingOCRResult{}, errors.New("OCR engine must be auto, paddle, glm, or qwen")
	}
	for index, endpoint := range endpoints {
		result, status, requestErr := h.postReadingOCRFile(ctx, baseURL+endpoint, data, filename, engine)
		if requestErr == nil {
			return result, nil
		}
		if status != http.StatusNotFound || index == len(endpoints)-1 {
			return readingOCRResult{}, requestErr
		}
	}
	return readingOCRResult{}, errors.New("OCR route fallback did not return a response")
}

func (h *ReadingHandler) postReadingOCRFile(ctx context.Context, target string, data []byte, filename, engine string) (readingOCRResult, int, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return readingOCRResult{}, 0, err
	}
	if _, err := part.Write(data); err != nil {
		return readingOCRResult{}, 0, err
	}
	if err := writer.WriteField("engine", engine); err != nil {
		return readingOCRResult{}, 0, err
	}
	if err := writer.Close(); err != nil {
		return readingOCRResult{}, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, &body)
	if err != nil {
		return readingOCRResult{}, 0, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "learning-compass/reading-journal")
	if h.cfg.OCR.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+h.cfg.OCR.APIKey)
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return readingOCRResult{}, 0, fmt.Errorf("OCR request failed: %w", err)
	}
	defer resp.Body.Close()
	responseData, err := io.ReadAll(io.LimitReader(resp.Body, maxReadingOCRResult))
	if err != nil {
		return readingOCRResult{}, resp.StatusCode, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var serviceError struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(responseData, &serviceError)
		message := strings.TrimSpace(serviceError.Error)
		if message == "" {
			message = strings.TrimSpace(string(responseData))
		}
		if message == "" {
			message = resp.Status
		}
		return readingOCRResult{}, resp.StatusCode, fmt.Errorf("OCR returned %s: %s", resp.Status, truncateReading(message, 500))
	}
	var result readingOCRResult
	if err := json.Unmarshal(responseData, &result); err != nil {
		return readingOCRResult{}, resp.StatusCode, fmt.Errorf("decode OCR response: %w", err)
	}
	result.Markdown = strings.TrimSpace(result.Markdown)
	if result.Markdown == "" || result.Engine == "" || result.Pages < 1 {
		return readingOCRResult{}, resp.StatusCode, errors.New("OCR service returned an invalid success response")
	}
	return result, resp.StatusCode, nil
}

func readingISBNFromText(text string) string {
	match := regexp.MustCompile(`(?i)ISBN(?:-1[03])?[:\s]*((?:97[89][\s-]?)?[0-9][0-9\s-]{8,16}[0-9X])`).FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	return strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(match[1]))
}

func (h *ReadingHandler) extractBookMetadataFromText(ctx context.Context, candidate model.BookCandidate) model.BookCandidate {
	result, err := h.ai.GenerateText(ctx, ai.TextRequest{
		SystemPrompt: "Extract book metadata from OCR text. Return JSON only.",
		UserPrompt:   "Return {\"title\":\"\",\"author\":\"\",\"isbn\":\"\",\"publisher\":\"\",\"language\":\"en\",\"genre\":\"fiction|informational|other\"}. Use an empty string when a value is not supported by the OCR text; do not guess. OCR text:\n" + truncateReading(candidate.RawText, 8000),
		MaxTokens:    500, ResponseMIMEType: "application/json",
	})
	if err != nil {
		return candidate
	}
	var extracted model.BookCandidate
	if json.Unmarshal([]byte(cleanReadingJSON(result)), &extracted) == nil {
		extracted.RawText = candidate.RawText
		extracted.Confidence = candidate.Confidence
		return extracted
	}
	return candidate
}

func (h *ReadingHandler) buildReadingCheck(ctx context.Context, req model.CreateReadingCheckReq) (model.ReadingCheck, error) {
	var title, author, genre, profileType string
	if err := h.db.QueryRowContext(ctx, `
		SELECT b.title,b.author,b.genre,p.profile_type FROM reading_books b CROSS JOIN reading_profiles p
		WHERE b.id=? AND p.profile_id=?`, req.BookID, req.ProfileID).Scan(&title, &author, &genre, &profileType); err != nil {
		return model.ReadingCheck{}, err
	}
	sections, err := h.findReadingEvidence(ctx, req)
	if err != nil {
		return model.ReadingCheck{}, err
	}
	skill := h.nextReadingSkill(ctx, req.ProfileID, genre)
	count := 1
	if req.Tier == "chapter" {
		count = 3
		skill = "Chapter Core Analysis"
	} else if req.Tier == "book" {
		count = 5
		skill = "Book Analysis"
	}
	check := model.ReadingCheck{SkillFocus: skill, SourceLabel: "General prompt", ContentRevision: readingParseVersion}
	if len(sections) == 0 || h.ai == nil {
		check.Questions = genericReadingQuestions(profileType, genre, req.Tier, skill)
		return check, nil
	}
	evidenceText := renderReadingEvidence(sections)
	if evidenceText == "" {
		check.Questions = genericReadingQuestions(profileType, genre, req.Tier, skill)
		return check, nil
	}
	level := "adult reader"
	if profileType == "child" {
		level = "Primary 4 learner using clear, age-appropriate English"
	}
	system := `You are a careful reading teacher. Create evidence-grounded questions, not trivia. Train a reader to identify core ideas, use textual evidence, infer, analyse development and monitor understanding. Never ask about content outside SOURCE. Return valid JSON only.`
	user := fmt.Sprintf(`BOOK: %s by %s
TEXT TYPE: %s
READER: %s
CHECK: %s
TARGET SKILL: %s
QUESTION COUNT: %d

Return {"questions":[{"skill":"one concise skill label","question":"English question","difficulty":1,"expectedIdeas":["idea"],"hint":"a hint that does not reveal the answer"}]}.
For a session ask one focused thinking question. For a chapter ask exactly: core idea, evidence-backed analysis, think further. For a book ask exactly: summary, development, evidence, theme/author purpose, evaluation/connection. Do not reveal answers in the question or hint.

SOURCE:
%s`, title, author, genre, level, req.Tier, skill, count, evidenceText)
	result, err := h.ai.GenerateTextDetailed(ctx, ai.TextRequest{SystemPrompt: system, UserPrompt: user, MaxTokens: 1600, ResponseMIMEType: "application/json"})
	if err != nil {
		check.Questions = genericReadingQuestions(profileType, genre, req.Tier, skill)
		return check, nil
	}
	var parsed struct {
		Questions []model.ReadingQuestion `json:"questions"`
	}
	if json.Unmarshal([]byte(cleanReadingJSON(result.Text)), &parsed) != nil || len(parsed.Questions) != count {
		check.Questions = genericReadingQuestions(profileType, genre, req.Tier, skill)
		return check, nil
	}
	markers := make([]string, 0, len(sections))
	for _, section := range sections {
		markers = append(markers, "section:"+strconv.FormatInt(section.ID, 10))
	}
	for i := range parsed.Questions {
		parsed.Questions[i].Question = strings.TrimSpace(parsed.Questions[i].Question)
		parsed.Questions[i].Skill = strings.TrimSpace(parsed.Questions[i].Skill)
		parsed.Questions[i].Difficulty = min(max(parsed.Questions[i].Difficulty, 1), 3)
		parsed.Questions[i].Evidence = markers
		if parsed.Questions[i].Question == "" {
			check.Questions = genericReadingQuestions(profileType, genre, req.Tier, skill)
			return check, nil
		}
	}
	check.Grounded = true
	check.SourceLabel = readingSourceLabel(req, sections)
	check.Questions = parsed.Questions
	check.ProviderName = result.Provider
	return check, nil
}

func (h *ReadingHandler) findReadingEvidence(ctx context.Context, req model.CreateReadingCheckReq) ([]readingSection, error) {
	pageFrom, pageTo := req.PageFrom, req.PageTo
	if pageTo == 0 {
		pageTo = pageFrom
	}
	if pageFrom > 0 {
		var printStart, digitalStart int
		err := h.db.QueryRowContext(ctx, `SELECT print_page_start,digital_page_start FROM reading_page_maps WHERE book_id=? AND print_page_start<=? ORDER BY print_page_start DESC LIMIT 1`, req.BookID, pageFrom).Scan(&printStart, &digitalStart)
		if err == nil {
			pageFrom = pageFrom - printStart + digitalStart
			pageTo = pageTo - printStart + digitalStart
		}
	}
	query := `SELECT id,ordinal,title,page_start,page_end,body FROM reading_sections WHERE book_id=?`
	args := []any{req.BookID}
	if strings.TrimSpace(req.Chapter) != "" {
		query += ` AND lower(title) LIKE lower(?)`
		args = append(args, "%"+strings.TrimSpace(req.Chapter)+"%")
	} else if pageFrom > 0 {
		query += ` AND page_start>0 AND page_start<=? AND page_end>=?`
		args = append(args, pageTo, pageFrom)
	}
	query += ` ORDER BY ordinal LIMIT 18`
	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sections := []readingSection{}
	for rows.Next() {
		var section readingSection
		if err := rows.Scan(&section.ID, &section.Ordinal, &section.Title, &section.PageStart, &section.PageEnd, &section.Body); err != nil {
			return nil, err
		}
		sections = append(sections, section)
	}
	if len(sections) == 0 && req.Tier == "book" {
		rows, err := h.db.QueryContext(ctx, `SELECT id,ordinal,title,page_start,page_end,body FROM reading_sections WHERE book_id=? ORDER BY ordinal`, req.BookID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		all := []readingSection{}
		for rows.Next() {
			var section readingSection
			if rows.Scan(&section.ID, &section.Ordinal, &section.Title, &section.PageStart, &section.PageEnd, &section.Body) == nil {
				all = append(all, section)
			}
		}
		sections = sampleReadingSections(all, 10)
	}
	return sections, nil
}

func sampleReadingSections(all []readingSection, limit int) []readingSection {
	if len(all) <= limit {
		return all
	}
	result := make([]readingSection, 0, limit)
	seen := map[int]bool{}
	for i := 0; i < limit; i++ {
		index := int(float64(i) * float64(len(all)-1) / float64(limit-1))
		if !seen[index] {
			result = append(result, all[index])
			seen[index] = true
		}
	}
	return result
}

func renderReadingEvidence(sections []readingSection) string {
	var out strings.Builder
	for _, section := range sections {
		label := section.Title
		if section.PageStart > 0 {
			label = fmt.Sprintf("%s (digital page %d)", label, section.PageStart)
		}
		piece := fmt.Sprintf("\n[SECTION %d — %s]\n%s\n", section.ID, label, section.Body)
		if out.Len()+len(piece) > maxReadingEvidence {
			remaining := maxReadingEvidence - out.Len()
			if remaining > 300 {
				out.WriteString(truncateReading(piece, remaining))
			}
			break
		}
		out.WriteString(piece)
	}
	return strings.TrimSpace(out.String())
}

func readingSourceLabel(req model.CreateReadingCheckReq, sections []readingSection) string {
	if strings.TrimSpace(req.Chapter) != "" {
		return "Grounded in “" + strings.TrimSpace(req.Chapter) + "”"
	}
	if req.PageFrom > 0 {
		end := req.PageTo
		if end == 0 {
			end = req.PageFrom
		}
		return fmt.Sprintf("Grounded in your mapped reading range, pages %d–%d", req.PageFrom, end)
	}
	if len(sections) > 0 {
		return "Grounded in the linked book content"
	}
	return "General prompt"
}

var readingSkills = []string{
	"Gist / Main Idea",
	"Key Details & Structure",
	"Vocabulary in Context",
	"Evidence & Inference",
	"Character or Idea Development",
	"Theme / Author’s Purpose",
	"Evaluation & Self-Monitoring",
}

func (h *ReadingHandler) nextReadingSkill(ctx context.Context, profileID, genre string) string {
	rows, err := h.db.QueryContext(ctx, `SELECT skill_focus FROM reading_checks WHERE profile_id=? AND tier='session' ORDER BY id DESC LIMIT 12`, profileID)
	if err != nil {
		return readingSkills[0]
	}
	defer rows.Close()
	counts := map[string]int{}
	recent := []string{}
	for rows.Next() {
		var skill string
		if rows.Scan(&skill) == nil {
			counts[skill]++
			recent = append(recent, skill)
		}
	}
	choices := append([]string(nil), readingSkills...)
	sort.SliceStable(choices, func(i, j int) bool { return counts[choices[i]] < counts[choices[j]] })
	for _, skill := range choices {
		if len(recent) >= 2 && recent[0] == skill && recent[1] == skill {
			continue
		}
		return skill
	}
	return readingSkills[0]
}

func genericReadingQuestions(profileType, genre, tier, skill string) []model.ReadingQuestion {
	if profileType == "adult" {
		switch tier {
		case "chapter":
			return []model.ReadingQuestion{
				{Skill: "Core Idea", Question: "What is the central idea of this chapter, in your own words?", Difficulty: 2, Hint: "Separate the main movement from supporting detail."},
				{Skill: "Evidence & Reasoning", Question: "Which moment, claim, or detail best supports your reading of the chapter?", Difficulty: 2, Hint: "Name the evidence, then explain why it matters."},
				{Skill: "Think Further", Question: "What question, connection, or disagreement will you carry into the next chapter?", Difficulty: 2, Hint: "Choose something that changed or challenged your thinking."},
			}
		case "book":
			return genericAdultBookQuestions()
		default:
			return []model.ReadingQuestion{{Skill: skill, Question: "What idea from this reading is worth keeping, and why?", Difficulty: 2, Hint: "Name the idea, then add the reason it matters to you."}}
		}
	}
	if tier == "chapter" {
		return []model.ReadingQuestion{
			{Skill: "Gist / Main Idea", Question: "What is the most important idea or event in this chapter?", Difficulty: 2, Hint: "Say it in one clear sentence, without retelling every detail."},
			{Skill: "Evidence & Inference", Question: "What detail from the chapter supports your thinking? Explain how it helps.", Difficulty: 2, Hint: "Use a moment, action, fact, or sentence as evidence."},
			{Skill: "Evaluation & Self-Monitoring", Question: "What made you think hardest in this chapter, and what do you understand now?", Difficulty: 2, Hint: "It is fine to name something that is still unclear."},
		}
	}
	if tier == "book" {
		return genericChildBookQuestions(genre)
	}
	question := "What is the main idea of what you read today?"
	hint := "Say who or what it is mostly about, then tell the most important point."
	switch skill {
	case "Key Details & Structure":
		question, hint = "Which two details are most important, and how do they connect?", "Choose details that help explain the main idea, not just interesting extras."
	case "Vocabulary in Context":
		question, hint = "Which word or phrase mattered to the meaning? What do you think it means here?", "Use the words around it as clues."
	case "Evidence & Inference":
		question, hint = "What can you work out that the author does not say directly? What is your evidence?", "Start with ‘I think… because the text shows…’."
	case "Character or Idea Development":
		question, hint = "What changed in a character, problem, or important idea? Why did it change?", "Compare the beginning of your reading with the end."
	case "Theme / Author’s Purpose":
		question, hint = "What message or purpose is the author building? Which detail gives you that idea?", "Think beyond what happened to why the author included it."
	case "Evaluation & Self-Monitoring":
		question, hint = "What part was clear, and what part should you reread or ask about?", "Strong readers notice both understanding and confusion."
	}
	return []model.ReadingQuestion{{Skill: skill, Question: question, Difficulty: 2, Hint: hint}}
}

func genericChildBookQuestions(genre string) []model.ReadingQuestion {
	development := "How did the main character or problem change from beginning to end?"
	theme := "What message does the story leave with the reader?"
	if genre == "informational" {
		development = "How did the important ideas build from beginning to end?"
		theme = "What was the author trying to explain or help the reader understand?"
	}
	return []model.ReadingQuestion{
		{Skill: "Summary", Question: "Summarise the book in five clear sentences or fewer.", Difficulty: 2, Hint: "Keep only the beginning situation, central development, and outcome or conclusion."},
		{Skill: "Development", Question: development, Difficulty: 2, Hint: "Compare the beginning with the end and explain what caused the change."},
		{Skill: "Evidence", Question: "Choose one strong piece of evidence that supports an important idea in the book. Why is it strong?", Difficulty: 3, Hint: "Name the detail and connect it to the idea it proves."},
		{Skill: "Theme / Author’s Purpose", Question: theme, Difficulty: 3, Hint: "Use more than one part of the book to support your answer."},
		{Skill: "Evaluation & Connection", Question: "What did this book make you think differently about? Would you recommend it? Explain.", Difficulty: 2, Hint: "Give a reason connected to the book, not only a rating."},
	}
}

func genericAdultBookQuestions() []model.ReadingQuestion {
	return []model.ReadingQuestion{
		{Skill: "Summary", Question: "What is the book’s central argument or narrative movement?", Difficulty: 3, Hint: "Compress the whole without flattening its important tension."},
		{Skill: "Development", Question: "Which character, idea, or problem changes most substantially, and how?", Difficulty: 3, Hint: "Trace the change rather than listing events."},
		{Skill: "Evidence", Question: "Which passage or example carries unusual explanatory weight? Why?", Difficulty: 3, Hint: "Connect the evidence to a larger claim."},
		{Skill: "Theme / Author’s Purpose", Question: "What is the author ultimately asking the reader to see, question, or value?", Difficulty: 3, Hint: "Distinguish the subject from the author’s treatment of it."},
		{Skill: "Evaluation & Connection", Question: "Where do you agree, resist, or connect this book to another idea?", Difficulty: 3, Hint: "Make the reason for your response explicit."},
	}
}

func (h *ReadingHandler) evaluateReadingResponses(ctx context.Context, check model.ReadingCheck, sectionIDs []int64, responses []string) ([]model.ReadingFeedback, string) {
	fallback := make([]model.ReadingFeedback, len(check.Questions))
	for i, question := range check.Questions {
		answer := ""
		if i < len(responses) {
			answer = strings.TrimSpace(responses[i])
		}
		state := "developing"
		hint := question.Hint
		if len(strings.Fields(answer)) >= 12 {
			state = "clear"
		}
		if answer == "" {
			state = "revisit"
			hint = "Try answering in one complete sentence before adding evidence."
		}
		fallback[i] = model.ReadingFeedback{Skill: question.Skill, Affirmation: "You stopped to explain your thinking.", Hint: hint, SuggestedState: state}
	}
	if h.ai == nil {
		return fallback, ""
	}
	sections, _ := h.loadReadingSections(ctx, sectionIDs)
	evidence := renderReadingEvidence(sections)
	questionsJSON, _ := json.Marshal(check.Questions)
	responsesJSON, _ := json.Marshal(responses)
	system := `You are a supportive Primary reading coach. Evaluate reasoning, not writing polish. First affirm a specific strength. If the response is incomplete, give one hint that points back to evidence without revealing a full answer. Return JSON only. Never claim a detail that is absent from SOURCE.`
	user := fmt.Sprintf(`QUESTIONS: %s
RESPONSES: %s
GROUNDED: %t
SOURCE: %s

Return {"feedback":[{"skill":"matching skill","affirmation":"specific brief affirmation","hint":"one actionable hint or empty string","suggestedState":"clear|developing|revisit"}]}. Return one item per question.`, questionsJSON, responsesJSON, check.Grounded, truncateReading(evidence, 12000))
	result, err := h.ai.GenerateTextDetailed(ctx, ai.TextRequest{SystemPrompt: system, UserPrompt: user, MaxTokens: 1200, ResponseMIMEType: "application/json"})
	if err != nil {
		return fallback, ""
	}
	var parsed struct {
		Feedback []model.ReadingFeedback `json:"feedback"`
	}
	if json.Unmarshal([]byte(cleanReadingJSON(result.Text)), &parsed) != nil || len(parsed.Feedback) != len(check.Questions) {
		return fallback, ""
	}
	for i := range parsed.Feedback {
		parsed.Feedback[i].SuggestedState = normalizedReadingState(parsed.Feedback[i].SuggestedState)
		if parsed.Feedback[i].Skill == "" {
			parsed.Feedback[i].Skill = check.Questions[i].Skill
		}
	}
	return parsed.Feedback, result.Provider
}

func (h *ReadingHandler) loadReadingSections(ctx context.Context, ids []int64) ([]readingSection, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id,ordinal,title,page_start,page_end,body FROM reading_sections WHERE id IN (`+strings.Join(placeholders, ",")+`) ORDER BY ordinal`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sections := []readingSection{}
	for rows.Next() {
		var section readingSection
		if err := rows.Scan(&section.ID, &section.Ordinal, &section.Title, &section.PageStart, &section.PageEnd, &section.Body); err != nil {
			return nil, err
		}
		sections = append(sections, section)
	}
	return sections, rows.Err()
}

func cleanReadingJSON(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "```json")
	value = strings.TrimPrefix(value, "```")
	value = strings.TrimSuffix(value, "```")
	return strings.TrimSpace(value)
}

func truncateReading(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	if limit <= 1 {
		return value[:limit]
	}
	return value[:limit-1] + "…"
}
