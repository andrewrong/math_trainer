CREATE TABLE IF NOT EXISTS english_curriculum_units (
    unit_id    TEXT PRIMARY KEY,
    grade      TEXT NOT NULL,
    book       TEXT NOT NULL,
    unit_no    INTEGER NOT NULL,
    title      TEXT NOT NULL,
    page_start INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS english_curriculum_progress (
    unit_id          TEXT PRIMARY KEY REFERENCES english_curriculum_units(unit_id) ON DELETE CASCADE,
    status           TEXT NOT NULL DEFAULT 'Not Started',
    reading_score    INTEGER NOT NULL DEFAULT 0,
    vocabulary_score INTEGER NOT NULL DEFAULT 0,
    language_score   INTEGER NOT NULL DEFAULT 0,
    writing_score    INTEGER NOT NULL DEFAULT 0,
    updated_at        DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS english_curriculum_lessons (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    unit_id       TEXT NOT NULL REFERENCES english_curriculum_units(unit_id) ON DELETE CASCADE,
    lesson_code   TEXT NOT NULL,
    title         TEXT NOT NULL,
    skill_focus   TEXT NOT NULL DEFAULT '',
    source_ref    TEXT NOT NULL DEFAULT '',
    sort_order    INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS english_curriculum_artifacts (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    unit_id       TEXT NOT NULL REFERENCES english_curriculum_units(unit_id) ON DELETE CASCADE,
    artifact_type TEXT NOT NULL,
    content_json  TEXT NOT NULL DEFAULT '{}',
    created_at    DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_engcurr_units_grade ON english_curriculum_units(grade);
CREATE INDEX IF NOT EXISTS idx_engcurr_units_sort ON english_curriculum_units(grade, sort_order);
CREATE INDEX IF NOT EXISTS idx_engcurr_lessons_unit ON english_curriculum_lessons(unit_id);
CREATE INDEX IF NOT EXISTS idx_engcurr_artifacts_unit ON english_curriculum_artifacts(unit_id);

INSERT OR IGNORE INTO schema_migrations(version) VALUES(7);
