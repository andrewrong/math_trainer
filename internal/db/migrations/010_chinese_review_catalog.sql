CREATE TABLE IF NOT EXISTS chinese_review_catalog (
    character        TEXT PRIMARY KEY,
    pinyin           TEXT,
    tone             TEXT,
    meaning          TEXT,
    words_json       TEXT NOT NULL DEFAULT '[]',
    example_sentence TEXT,
    pinyin_hint      TEXT,
    shape_hint       TEXT,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO schema_migrations(version) VALUES(10);
