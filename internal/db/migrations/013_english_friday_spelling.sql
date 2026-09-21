-- Parent-confirmed mistakes from a paper Friday spelling test.
-- A row exists only while the word still needs review.
CREATE TABLE IF NOT EXISTS english_friday_spelling_mistakes (
    week_id    TEXT NOT NULL REFERENCES english_weeks(week_id) ON DELETE CASCADE,
    word       TEXT NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (week_id, word)
);

CREATE INDEX IF NOT EXISTS idx_english_friday_spelling_week
    ON english_friday_spelling_mistakes(week_id);
