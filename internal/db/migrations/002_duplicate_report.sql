-- 迁移 v2：新增重复报告三张表（幂等，可重复执行）
CREATE TABLE IF NOT EXISTS duplicate_reports (
    id                      INTEGER PRIMARY KEY AUTOINCREMENT,
    background_task_id      TEXT    REFERENCES background_tasks(id) ON DELETE SET NULL,
    scope                   TEXT    NOT NULL DEFAULT 'all' CHECK (scope IN ('all','same_dir')),
    media_type              TEXT    NOT NULL DEFAULT 'all' CHECK (media_type IN ('image','video','all')),
    image_threshold         INTEGER NOT NULL DEFAULT 90,
    video_phash_distance    INTEGER NOT NULL DEFAULT 12,
    video_duration_diff_ms  INTEGER NOT NULL DEFAULT 3000,
    oshash_filter           INTEGER NOT NULL DEFAULT 1,
    include_sha1            INTEGER NOT NULL DEFAULT 1,
    stale                   INTEGER NOT NULL DEFAULT 0,
    total_groups            INTEGER NOT NULL DEFAULT 0,
    total_files             INTEGER NOT NULL DEFAULT 0,
    created_at              TIMESTAMP NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS duplicate_groups (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    report_id   INTEGER NOT NULL REFERENCES duplicate_reports(id) ON DELETE CASCADE,
    group_type  TEXT    NOT NULL CHECK (group_type IN ('sha1','image_similar','video_similar'))
);
CREATE INDEX IF NOT EXISTS idx_dup_groups_report ON duplicate_groups(report_id);

CREATE TABLE IF NOT EXISTS duplicate_group_members (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id  INTEGER NOT NULL REFERENCES duplicate_groups(id) ON DELETE CASCADE,
    media_id  INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    UNIQUE (group_id, media_id)
);
CREATE INDEX IF NOT EXISTS idx_dup_members_group ON duplicate_group_members(group_id);
CREATE INDEX IF NOT EXISTS idx_dup_members_media ON duplicate_group_members(media_id);
