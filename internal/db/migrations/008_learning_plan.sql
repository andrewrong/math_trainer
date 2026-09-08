CREATE TABLE IF NOT EXISTS learning_plan_settings (
    id                     INTEGER PRIMARY KEY CHECK(id = 1),
    main_lesson_mode       TEXT NOT NULL DEFAULT 'manual' CHECK(main_lesson_mode IN ('manual', 'auto')),
    main_lesson_topic_code TEXT NOT NULL DEFAULT 'P4-FRACTIONS-MIXED',
    updated_at             DATETIME NOT NULL DEFAULT (datetime('now'))
);

INSERT OR IGNORE INTO learning_plan_settings(id, main_lesson_mode, main_lesson_topic_code)
VALUES(1, 'manual', 'P4-FRACTIONS-MIXED');

INSERT OR IGNORE INTO schema_migrations(version) VALUES(8);
