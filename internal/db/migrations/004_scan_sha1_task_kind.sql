-- 迁移 v4：background_tasks.kind 增加 scan_sha1 类型（重建表）
PRAGMA foreign_keys = OFF;

CREATE TABLE background_tasks_new (
    id                  TEXT    PRIMARY KEY,
    kind                TEXT    NOT NULL CHECK (kind IN ('scan','repair','temporary_scan','report_image','report_video','report_duplicate','promote','directory_delete','scan_sha1')),
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

PRAGMA foreign_keys = ON;
