package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenRunsEnglishCurriculumMigration(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	var name string
	if err := database.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='english_curriculum_units'`).Scan(&name); err != nil {
		t.Fatalf("english curriculum migration did not create units table: %v", err)
	}
}

func TestOpenRunsLearningPlanMigration(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	var mode, topicCode string
	if err := database.QueryRow(`SELECT main_lesson_mode,main_lesson_topic_code FROM learning_plan_settings WHERE id=1`).Scan(&mode, &topicCode); err != nil {
		t.Fatalf("learning plan migration did not create the default setting: %v", err)
	}
	if mode != "manual" || topicCode != "P4-FRACTIONS-MIXED" {
		t.Fatalf("default learning plan=(%q,%q), want manual fractions", mode, topicCode)
	}
}

func TestOpenRemovesRetiredPianoTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-piano.db")
	legacy, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE piano_assignments (id INTEGER PRIMARY KEY, title TEXT)`,
		`CREATE TABLE piano_practice_sessions (id INTEGER PRIMARY KEY, content TEXT)`,
		`CREATE TABLE piano_skill_stats (skill TEXT PRIMARY KEY, correct_count INTEGER)`,
		`INSERT INTO piano_assignments(title) VALUES ('Old practice')`,
	} {
		if _, err := legacy.Exec(statement); err != nil {
			legacy.Close()
			t.Fatal(err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	for _, table := range []string{"piano_assignments", "piano_practice_sessions", "piano_skill_stats"} {
		var count int
		if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("retired table %q still exists", table)
		}
	}
}
