CREATE TABLE IF NOT EXISTS reading_profiles (
    profile_id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    profile_type TEXT NOT NULL CHECK(profile_type IN ('child','adult')),
    review_mode TEXT NOT NULL CHECK(review_mode IN ('parent','self')),
    avatar_color TEXT NOT NULL DEFAULT '#185fa5',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO reading_profiles(profile_id,display_name,profile_type,review_mode,avatar_color)
VALUES
    ('learner','Learner','child','parent','#185fa5'),
    ('me','Me','adult','self','#1D9E75');

CREATE TABLE IF NOT EXISTS reading_books (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL,
    author TEXT NOT NULL DEFAULT '',
    genre TEXT NOT NULL DEFAULT 'fiction' CHECK(genre IN ('fiction','informational','other')),
    language TEXT NOT NULL DEFAULT 'en',
    isbn TEXT NOT NULL DEFAULT '',
    publisher TEXT NOT NULL DEFAULT '',
    cover_path TEXT NOT NULL DEFAULT '',
    metadata_source TEXT NOT NULL DEFAULT 'manual',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_reading_books_title_author
ON reading_books(title,author);

CREATE TABLE IF NOT EXISTS reading_assets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    book_id INTEGER REFERENCES reading_books(id) ON DELETE CASCADE,
    asset_type TEXT NOT NULL CHECK(asset_type IN ('cover','copyright','ebook')),
    source_path TEXT NOT NULL,
    original_name TEXT NOT NULL DEFAULT '',
    mime_type TEXT NOT NULL DEFAULT '',
    sha256 TEXT NOT NULL DEFAULT '',
    ingestion_status TEXT NOT NULL DEFAULT 'queued' CHECK(ingestion_status IN ('queued','processing','ready','failed')),
    ingestion_error TEXT NOT NULL DEFAULT '',
    parse_version TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_reading_assets_sha
ON reading_assets(sha256) WHERE sha256 <> '';

CREATE TABLE IF NOT EXISTS reading_sections (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    book_id INTEGER NOT NULL REFERENCES reading_books(id) ON DELETE CASCADE,
    asset_id INTEGER NOT NULL REFERENCES reading_assets(id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    page_start INTEGER NOT NULL DEFAULT 0,
    page_end INTEGER NOT NULL DEFAULT 0,
    body TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(asset_id,ordinal)
);

CREATE INDEX IF NOT EXISTS idx_reading_sections_book
ON reading_sections(book_id,ordinal);

CREATE TABLE IF NOT EXISTS reading_sections_fts (
    section_id INTEGER PRIMARY KEY,
    book_id INTEGER NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_reading_sections_search_book
ON reading_sections_fts(book_id);

CREATE TABLE IF NOT EXISTS reading_page_maps (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    book_id INTEGER NOT NULL REFERENCES reading_books(id) ON DELETE CASCADE,
    asset_id INTEGER REFERENCES reading_assets(id) ON DELETE CASCADE,
    print_page_start INTEGER NOT NULL,
    digital_page_start INTEGER NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(book_id,asset_id,print_page_start)
);

CREATE TABLE IF NOT EXISTS reading_progress (
    profile_id TEXT NOT NULL REFERENCES reading_profiles(profile_id) ON DELETE CASCADE,
    book_id INTEGER NOT NULL REFERENCES reading_books(id) ON DELETE CASCADE,
    chapter TEXT NOT NULL DEFAULT '',
    page_number INTEGER NOT NULL DEFAULT 0,
    percent INTEGER NOT NULL DEFAULT 0 CHECK(percent BETWEEN 0 AND 100),
    status TEXT NOT NULL DEFAULT 'reading' CHECK(status IN ('want_to_read','reading','finished','paused')),
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY(profile_id,book_id)
);

CREATE TABLE IF NOT EXISTS reading_sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    profile_id TEXT NOT NULL REFERENCES reading_profiles(profile_id) ON DELETE CASCADE,
    book_id INTEGER NOT NULL REFERENCES reading_books(id) ON DELETE CASCADE,
    session_date TEXT NOT NULL,
    chapter TEXT NOT NULL DEFAULT '',
    page_from INTEGER NOT NULL DEFAULT 0,
    page_to INTEGER NOT NULL DEFAULT 0,
    percent INTEGER NOT NULL DEFAULT 0 CHECK(percent BETWEEN 0 AND 100),
    minutes INTEGER NOT NULL DEFAULT 0 CHECK(minutes >= 0),
    reaction TEXT NOT NULL DEFAULT '',
    quick_note TEXT NOT NULL DEFAULT '',
    response_mode TEXT NOT NULL DEFAULT 'typed' CHECK(response_mode IN ('typed','parent_scribed')),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_reading_sessions_profile_date
ON reading_sessions(profile_id,session_date DESC);

CREATE TABLE IF NOT EXISTS reading_checks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    profile_id TEXT NOT NULL REFERENCES reading_profiles(profile_id) ON DELETE CASCADE,
    book_id INTEGER NOT NULL REFERENCES reading_books(id) ON DELETE CASCADE,
    session_id INTEGER REFERENCES reading_sessions(id) ON DELETE SET NULL,
    tier TEXT NOT NULL CHECK(tier IN ('session','chapter','book')),
    skill_focus TEXT NOT NULL DEFAULT '',
    grounded INTEGER NOT NULL DEFAULT 0,
    source_label TEXT NOT NULL DEFAULT 'General prompt',
    source_section_ids TEXT NOT NULL DEFAULT '[]',
    questions_json TEXT NOT NULL DEFAULT '[]',
    responses_json TEXT NOT NULL DEFAULT '[]',
    feedback_json TEXT NOT NULL DEFAULT '[]',
    response_mode TEXT NOT NULL DEFAULT 'typed',
    provider_name TEXT NOT NULL DEFAULT '',
    prompt_version TEXT NOT NULL DEFAULT '',
    content_revision TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_reading_checks_profile_book
ON reading_checks(profile_id,book_id,created_at DESC);

CREATE TABLE IF NOT EXISTS reading_skill_observations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    check_id INTEGER NOT NULL REFERENCES reading_checks(id) ON DELETE CASCADE,
    profile_id TEXT NOT NULL REFERENCES reading_profiles(profile_id) ON DELETE CASCADE,
    skill TEXT NOT NULL,
    suggested_state TEXT NOT NULL DEFAULT 'developing' CHECK(suggested_state IN ('clear','developing','revisit')),
    confirmed_state TEXT NOT NULL DEFAULT '' CHECK(confirmed_state IN ('','clear','developing','revisit')),
    confirmed_by TEXT NOT NULL DEFAULT '',
    confirmed_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(check_id,skill)
);

CREATE TABLE IF NOT EXISTS reading_weekly_reviews (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    profile_id TEXT NOT NULL REFERENCES reading_profiles(profile_id) ON DELETE CASCADE,
    week_start TEXT NOT NULL,
    reflection TEXT NOT NULL DEFAULT '',
    next_goal TEXT NOT NULL DEFAULT '',
    reviewer TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(profile_id,week_start)
);
