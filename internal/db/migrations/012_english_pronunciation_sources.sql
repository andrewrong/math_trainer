-- Expand the provenance constraint for databases that applied migration 011
-- before Youdao-backed UK IPA became available.
CREATE TABLE IF NOT EXISTS english_pronunciations_v2 (
    word_key   TEXT NOT NULL,
    locale     TEXT NOT NULL DEFAULT 'en-GB',
    ipa        TEXT NOT NULL,
    source     TEXT NOT NULL CHECK(source IN ('espeak_ng','youdao','parent_verified')),
    verified   INTEGER NOT NULL DEFAULT 0 CHECK(verified IN (0,1)),
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (word_key, locale)
);

INSERT OR REPLACE INTO english_pronunciations_v2(word_key, locale, ipa, source, verified, updated_at)
SELECT word_key, locale, ipa, source, verified, updated_at FROM english_pronunciations;

DROP TABLE english_pronunciations;
ALTER TABLE english_pronunciations_v2 RENAME TO english_pronunciations;
CREATE INDEX IF NOT EXISTS idx_english_pronunciations_locale ON english_pronunciations(locale, verified);

INSERT OR IGNORE INTO schema_migrations(version) VALUES(12);
