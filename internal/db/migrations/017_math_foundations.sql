CREATE TABLE IF NOT EXISTS math_foundation_attempts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    skill_key TEXT NOT NULL,
    item_key TEXT NOT NULL,
    set_key TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL CHECK(mode IN ('diagnostic','guided','check')),
    correct INTEGER NOT NULL CHECK(correct IN (0,1)),
    hint_used INTEGER NOT NULL DEFAULT 0 CHECK(hint_used IN (0,1)),
    elapsed_seconds INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS math_foundation_paper_checks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    skill_key TEXT NOT NULL,
    completed INTEGER NOT NULL DEFAULT 1 CHECK(completed IN (0,1)),
    note TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_math_foundation_attempts_skill ON math_foundation_attempts(skill_key, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_math_foundation_paper_checks_skill ON math_foundation_paper_checks(skill_key, created_at DESC);

INSERT OR IGNORE INTO schema_migrations(version) VALUES(17);
