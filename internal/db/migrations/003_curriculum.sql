CREATE TABLE IF NOT EXISTS curriculum_topics (
    topic_code         TEXT PRIMARY KEY,
    grade              TEXT NOT NULL,
    strand             TEXT NOT NULL,
    sub_strand         TEXT NOT NULL,
    title              TEXT NOT NULL,
    objectives_json    TEXT NOT NULL DEFAULT '[]',
    prerequisites_json TEXT NOT NULL DEFAULT '[]',
    related_json       TEXT NOT NULL DEFAULT '[]',
    sort_order         INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS curriculum_textbook_map (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    topic_code      TEXT NOT NULL REFERENCES curriculum_topics(topic_code) ON DELETE CASCADE,
    textbook_series TEXT NOT NULL DEFAULT 'My Pals Are Here! Maths',
    book            TEXT NOT NULL,
    chapter_title   TEXT NOT NULL,
    source          TEXT NOT NULL DEFAULT 'manual',
    confidence      TEXT NOT NULL DEFAULT 'confirmed',
    created_at      DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS curriculum_progress (
    topic_code       TEXT PRIMARY KEY REFERENCES curriculum_topics(topic_code) ON DELETE CASCADE,
    status           TEXT NOT NULL DEFAULT 'Not Started',
    concept_score    INTEGER NOT NULL DEFAULT 0,
    accuracy_score   INTEGER NOT NULL DEFAULT 0,
    speed_score      INTEGER NOT NULL DEFAULT 0,
    transfer_score   INTEGER NOT NULL DEFAULT 0,
    updated_at       DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS curriculum_artifacts (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    topic_code    TEXT NOT NULL REFERENCES curriculum_topics(topic_code) ON DELETE CASCADE,
    artifact_type TEXT NOT NULL,
    content_json  TEXT NOT NULL DEFAULT '{}',
    created_at    DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_curriculum_topics_grade ON curriculum_topics(grade);
CREATE INDEX IF NOT EXISTS idx_curriculum_topics_strand ON curriculum_topics(strand);
CREATE INDEX IF NOT EXISTS idx_curriculum_map_topic ON curriculum_textbook_map(topic_code);

INSERT OR IGNORE INTO schema_migrations(version) VALUES(3);
