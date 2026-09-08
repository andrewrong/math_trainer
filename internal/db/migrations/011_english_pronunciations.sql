CREATE TABLE IF NOT EXISTS english_pronunciations (
    word_key   TEXT NOT NULL,
    locale     TEXT NOT NULL DEFAULT 'en-GB',
    ipa        TEXT NOT NULL,
    source     TEXT NOT NULL CHECK(source IN ('espeak_ng','youdao','parent_verified')),
    verified   INTEGER NOT NULL DEFAULT 0 CHECK(verified IN (0,1)),
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (word_key, locale)
);

CREATE INDEX IF NOT EXISTS idx_english_pronunciations_locale ON english_pronunciations(locale, verified);

INSERT OR IGNORE INTO schema_migrations(version) VALUES(11);
