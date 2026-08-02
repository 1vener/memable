-- 迁移 v6：background_tasks.kind 增加 report_directory（重建表）+ 目录对比报告独立三表
PRAGMA foreign_keys = OFF;

CREATE TABLE background_tasks_new (
    id                  TEXT    PRIMARY KEY,
    kind                TEXT    NOT NULL CHECK (kind IN ('scan','repair','temporary_scan','report_image','report_video','report_duplicate','report_directory','promote','directory_delete','scan_sha1')),
    status              TEXT    NOT NULL DEFAULT 'queued'
                        CHECK (status IN ('queued','running','completed','failed','cancelled')),
    title               TEXT    NOT NULL,
    dedupe_key          TEXT,
    library_id          INTEGER,
    scan_session_id     TEXT,
    payload_json        TEXT,
    phase               TEXT    NOT NULL DEFAULT 'queued',
    total_items         INTEGER NOT NULL DEFAULT 0,
    processed_items     INTEGER NOT NULL DEFAULT 0,
    succeeded_items     INTEGER NOT NULL DEFAULT 0,
    skipped_items       INTEGER NOT NULL DEFAULT 0,
    failed_items        INTEGER NOT NULL DEFAULT 0,
    processing_rate     REAL    NOT NULL DEFAULT 0,
    eta_seconds         INTEGER,
    result_json         TEXT,
    error_message       TEXT,
    queued_at           TIMESTAMP NOT NULL DEFAULT (datetime('now')),
    started_at          TIMESTAMP,
    updated_at          TIMESTAMP NOT NULL DEFAULT (datetime('now')),
    finished_at         TIMESTAMP
);

INSERT INTO background_tasks_new (
    id, kind, status, title, dedupe_key, library_id, scan_session_id, payload_json,
    phase, total_items, processed_items, succeeded_items, skipped_items, failed_items,
    processing_rate, eta_seconds, result_json, error_message,
    queued_at, started_at, updated_at, finished_at
) SELECT
    id, kind, status, title, dedupe_key, library_id, scan_session_id, payload_json,
    phase, total_items, processed_items, succeeded_items, skipped_items, failed_items,
    processing_rate, eta_seconds, result_json, error_message,
    queued_at, started_at, updated_at, finished_at
FROM background_tasks;

DROP TABLE background_tasks;
ALTER TABLE background_tasks_new RENAME TO background_tasks;

CREATE UNIQUE INDEX IF NOT EXISTS idx_tasks_dedupe
    ON background_tasks(dedupe_key)
    WHERE dedupe_key IS NOT NULL AND status IN ('queued','running');
CREATE INDEX IF NOT EXISTS idx_tasks_status ON background_tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_queued ON background_tasks(queued_at) WHERE status IN ('queued','running');

-- 目录对比报告独立三表（所选目录 vs 存量数据，不替换重复报告）
CREATE TABLE IF NOT EXISTS dir_duplicate_reports (
    id                      INTEGER PRIMARY KEY AUTOINCREMENT,
    background_task_id      TEXT    REFERENCES background_tasks(id) ON DELETE SET NULL,
    library_id              INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    directory               TEXT    NOT NULL DEFAULT '',   -- 所选目录相对库根路径（正斜杠，含子目录）
    media_type              TEXT    NOT NULL DEFAULT 'all' CHECK (media_type IN ('image','video','all')),
    image_threshold         INTEGER NOT NULL DEFAULT 90,
    video_phash_distance    INTEGER NOT NULL DEFAULT 12,
    video_duration_diff_ms  INTEGER NOT NULL DEFAULT 3000,
    oshash_filter           INTEGER NOT NULL DEFAULT 1,
    include_sha1            INTEGER NOT NULL DEFAULT 1,
    total_groups            INTEGER NOT NULL DEFAULT 0,
    total_files             INTEGER NOT NULL DEFAULT 0,
    created_at              TIMESTAMP NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS dir_duplicate_groups (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    report_id   INTEGER NOT NULL REFERENCES dir_duplicate_reports(id) ON DELETE CASCADE,
    group_type  TEXT    NOT NULL CHECK (group_type IN ('sha1','image_similar','video_similar'))
);
CREATE INDEX IF NOT EXISTS idx_dir_groups_report ON dir_duplicate_groups(report_id);

CREATE TABLE IF NOT EXISTS dir_duplicate_members (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id    INTEGER NOT NULL REFERENCES dir_duplicate_groups(id) ON DELETE CASCADE,
    media_id    INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    is_target   INTEGER NOT NULL DEFAULT 0 CHECK (is_target IN (0,1)), -- 1=所选目录文件
    UNIQUE (group_id, media_id)
);
CREATE INDEX IF NOT EXISTS idx_dir_members_group ON dir_duplicate_members(group_id);
CREATE INDEX IF NOT EXISTS idx_dir_members_media ON dir_duplicate_members(media_id);

PRAGMA foreign_keys = ON;
