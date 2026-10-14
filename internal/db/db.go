package db

import (
	"database/sql"
	_ "embed"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed migrations/001_init.sql
var initSQL string

//go:embed migrations/002_english_diagnostics.sql
var englishDiagnosticsSQL string

//go:embed migrations/003_curriculum.sql
var curriculumSQL string

//go:embed migrations/004_chinese.sql
var chineseSQL string

//go:embed migrations/005_chinese_review_v2.sql
var chineseReviewV2SQL string

//go:embed migrations/007_english_curriculum.sql
var englishCurriculumSQL string

//go:embed migrations/008_learning_plan.sql
var learningPlanSQL string

//go:embed migrations/009_reading_journal.sql
var readingJournalSQL string

//go:embed migrations/010_chinese_review_catalog.sql
var chineseReviewCatalogSQL string

//go:embed migrations/011_english_pronunciations.sql
var englishPronunciationsSQL string

//go:embed migrations/012_english_pronunciation_sources.sql
var englishPronunciationSourcesSQL string

//go:embed migrations/013_english_friday_spelling.sql
var englishFridaySpellingSQL string

//go:embed migrations/014_remove_piano.sql
var removePianoSQL string

//go:embed migrations/015_piano_homework.sql
var pianoHomeworkSQL string

//go:embed migrations/016_backfill_chinese_audio_metadata.sql
var chineseAudioBackfillSQL string

//go:embed migrations/017_math_foundations.sql
var mathFoundationsSQL string

//go:embed migrations/018_chinese_context_stories.sql
var chineseContextStoriesSQL string

// Open opens (or creates) the SQLite database, runs migrations, and returns
// a *sql.DB ready to use. WAL mode is enabled for better concurrent reads.
func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000", path)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite3: %w", err)
	}
	// Single writer for SQLite is fine; allow multiple concurrent readers.
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite3: %w", err)
	}
	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	if _, err := db.Exec(initSQL); err != nil {
		return fmt.Errorf("executing init.sql: %w", err)
	}
	if _, err := db.Exec(englishDiagnosticsSQL); err != nil {
		return fmt.Errorf("executing english diagnostics migration: %w", err)
	}
	if _, err := db.Exec(curriculumSQL); err != nil {
		return fmt.Errorf("executing curriculum migration: %w", err)
	}
	if _, err := db.Exec(chineseSQL); err != nil {
		return fmt.Errorf("executing chinese migration: %w", err)
	}
	if _, err := db.Exec(chineseReviewV2SQL); err != nil {
		return fmt.Errorf("executing chinese review v2 migration: %w", err)
	}
	if _, err := db.Exec(englishCurriculumSQL); err != nil {
		return fmt.Errorf("executing english curriculum migration: %w", err)
	}
	if _, err := db.Exec(learningPlanSQL); err != nil {
		return fmt.Errorf("executing learning plan migration: %w", err)
	}
	if _, err := db.Exec(readingJournalSQL); err != nil {
		return fmt.Errorf("executing reading journal migration: %w", err)
	}
	if _, err := db.Exec(chineseReviewCatalogSQL); err != nil {
		return fmt.Errorf("executing Chinese review catalog migration: %w", err)
	}
	if _, err := db.Exec(englishPronunciationsSQL); err != nil {
		return fmt.Errorf("executing English pronunciations migration: %w", err)
	}
	if _, err := db.Exec(englishPronunciationSourcesSQL); err != nil {
		return fmt.Errorf("executing English pronunciation sources migration: %w", err)
	}
	if _, err := db.Exec(englishFridaySpellingSQL); err != nil {
		return fmt.Errorf("executing English Friday spelling migration: %w", err)
	}
	if _, err := db.Exec(removePianoSQL); err != nil {
		return fmt.Errorf("removing retired piano data: %w", err)
	}
	if _, err := db.Exec(pianoHomeworkSQL); err != nil {
		return fmt.Errorf("executing piano homework migration: %w", err)
	}
	if _, err := db.Exec(chineseAudioBackfillSQL); err != nil {
		return fmt.Errorf("backfilling Chinese audio metadata: %w", err)
	}
	if _, err := db.Exec(mathFoundationsSQL); err != nil {
		return fmt.Errorf("executing math foundations migration: %w", err)
	}
	if _, err := db.Exec(chineseContextStoriesSQL); err != nil {
		return fmt.Errorf("executing Chinese context stories migration: %w", err)
	}
	return nil
}
