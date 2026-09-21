-- The former Piano module used hardcoded music notation that is no longer
-- trustworthy. Retire its records completely before a future redesign.
DROP TABLE IF EXISTS piano_assignments;
DROP TABLE IF EXISTS piano_practice_sessions;
DROP TABLE IF EXISTS piano_skill_stats;

INSERT OR IGNORE INTO schema_migrations(version) VALUES(14);
