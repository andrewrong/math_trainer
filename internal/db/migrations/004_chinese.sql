CREATE TABLE IF NOT EXISTS chinese_weeks (
    week_id    TEXT PRIMARY KEY,
    label      TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS chinese_items (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    week_id          TEXT NOT NULL REFERENCES chinese_weeks(week_id) ON DELETE CASCADE,
    character        TEXT NOT NULL,
    pinyin           TEXT,
    tone             TEXT,
    meaning          TEXT,
    words_json       TEXT NOT NULL DEFAULT '[]',
    example_sentence TEXT,
    pinyin_hint      TEXT,
    shape_hint       TEXT,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS chinese_review_stats (
    character          TEXT PRIMARY KEY,
    wrong_count        INTEGER NOT NULL DEFAULT 0,
    review_count       INTEGER NOT NULL DEFAULT 0,
    pinyin_wrong       INTEGER NOT NULL DEFAULT 0,
    recognition_wrong  INTEGER NOT NULL DEFAULT 0,
    last_reviewed_at   DATETIME
);

CREATE INDEX IF NOT EXISTS idx_chinese_items_week ON chinese_items(week_id);
CREATE INDEX IF NOT EXISTS idx_chinese_weeks_created ON chinese_weeks(created_at);

INSERT OR IGNORE INTO schema_migrations(version) VALUES(4);
