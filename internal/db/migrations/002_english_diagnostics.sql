CREATE TABLE IF NOT EXISTS english_diagnostics (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    grade_level TEXT NOT NULL DEFAULT 'Grade 4',
    report_json TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_english_diagnostics_created ON english_diagnostics(created_at);

INSERT OR IGNORE INTO schema_migrations(version) VALUES(2);
