-- Math session history
CREATE TABLE IF NOT EXISTS math_sessions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    area       TEXT NOT NULL,
    level      TEXT NOT NULL,
    type_key   TEXT NOT NULL,
    total      INTEGER NOT NULL,
    correct    INTEGER NOT NULL,
    rate       INTEGER NOT NULL,
    elapsed    INTEGER NOT NULL
);

-- Wrong answers (practice + challenge share this table via category column)
CREATE TABLE IF NOT EXISTS wrong_answers (
    key        TEXT NOT NULL,
    category   TEXT NOT NULL CHECK(category IN ('practice','challenge')),
    q_a        INTEGER NOT NULL,
    q_op       TEXT NOT NULL,
    q_b        INTEGER NOT NULL,
    q_ans      INTEGER NOT NULL,
    count      INTEGER NOT NULL DEFAULT 1,
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (key, category)
);

-- Mastered multiplication facts
CREATE TABLE IF NOT EXISTS mastered_facts (
    fact_key   TEXT PRIMARY KEY,
    status     TEXT NOT NULL CHECK(status IN ('ok','weak')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- English weekly word sets
CREATE TABLE IF NOT EXISTS english_weeks (
    week_id     TEXT PRIMARY KEY,
    label       TEXT NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    words_json  TEXT NOT NULL DEFAULT '[]',
    story       TEXT NOT NULL DEFAULT '',
    blanks_json TEXT NOT NULL DEFAULT '[]'
);

-- Spell test scores (extracted from week so they can be updated incrementally)
CREATE TABLE IF NOT EXISTS spell_scores (
    week_id  TEXT NOT NULL REFERENCES english_weeks(week_id) ON DELETE CASCADE,
    word     TEXT NOT NULL,
    correct  INTEGER NOT NULL DEFAULT 0,
    wrong    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (week_id, word)
);

-- Schema version tracking (for future migrations)
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

INSERT OR IGNORE INTO schema_migrations(version) VALUES(1);

-- Indexes for common query patterns
CREATE INDEX IF NOT EXISTS idx_sessions_area ON math_sessions(area);
CREATE INDEX IF NOT EXISTS idx_sessions_created ON math_sessions(created_at);
CREATE INDEX IF NOT EXISTS idx_wrong_category ON wrong_answers(category);
CREATE INDEX IF NOT EXISTS idx_spell_week ON spell_scores(week_id);
