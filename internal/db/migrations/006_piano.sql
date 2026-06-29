CREATE TABLE IF NOT EXISTS piano_assignments (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    title      TEXT NOT NULL,
    section    TEXT,
    goal       TEXT,
    due_date   TEXT,
    status     TEXT NOT NULL DEFAULT 'Not Started',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS piano_practice_sessions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    practiced_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    minutes    INTEGER NOT NULL DEFAULT 0,
    content    TEXT,
    stuck      TEXT,
    note       TEXT
);

CREATE TABLE IF NOT EXISTS piano_skill_stats (
    skill              TEXT PRIMARY KEY,
    correct_count      INTEGER NOT NULL DEFAULT 0,
    wrong_count        INTEGER NOT NULL DEFAULT 0,
    last_practiced_at  DATETIME
);

INSERT OR IGNORE INTO schema_migrations(version) VALUES(6);
