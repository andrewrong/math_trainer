CREATE TABLE IF NOT EXISTS chinese_context_stories (
    set_key     TEXT PRIMARY KEY,
    story_json  TEXT NOT NULL,
    blanks_json TEXT NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO schema_migrations(version) VALUES(18);
