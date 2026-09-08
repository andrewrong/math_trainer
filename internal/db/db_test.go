package db

import (
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
