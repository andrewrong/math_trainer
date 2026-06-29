CREATE TABLE IF NOT EXISTS chinese_review_rounds (
    week_id      TEXT NOT NULL,
    round_index  INTEGER NOT NULL,
    start_pos    INTEGER NOT NULL,
    end_pos      INTEGER NOT NULL,
    total        INTEGER NOT NULL DEFAULT 0,
    correct      INTEGER NOT NULL DEFAULT 0,
    wrong        INTEGER NOT NULL DEFAULT 0,
    completed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (week_id, round_index)
);

CREATE TABLE IF NOT EXISTS chinese_review_kind_stats (
    character        TEXT NOT NULL,
    kind             TEXT NOT NULL,
    wrong_count      INTEGER NOT NULL DEFAULT 0,
    last_reviewed_at DATETIME,
    PRIMARY KEY (character, kind)
);

CREATE INDEX IF NOT EXISTS idx_chinese_review_rounds_week ON chinese_review_rounds(week_id);
CREATE INDEX IF NOT EXISTS idx_chinese_review_kind_stats_character ON chinese_review_kind_stats(character);

INSERT OR IGNORE INTO schema_migrations(version) VALUES(5);
