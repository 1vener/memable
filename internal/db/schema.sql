-- 本地媒体相似度/重复检测管理系统 - SQLite 表结构脚本
-- 适用 SQLite 3.35+
-- 代码注释使用中文
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;

-- ============================================================
-- 0. 数据库结构版本（内嵌迁移使用）
-- ============================================================
CREATE TABLE IF NOT EXISTS schema_version (
    version         INTEGER PRIMARY KEY,                               -- 迁移版本号，1 起始
    applied_at      TIMESTAMP NOT NULL DEFAULT (datetime('now'))
);

-- ============================================================
-- 1. 收藏库（媒体根目录）管理
-- ============================================================
CREATE TABLE IF NOT EXISTS libraries (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT    NOT NULL,                                  -- 收藏库名称，如"照片库"
    path            TEXT    NOT NULL,                                  -- 根目录绝对路径，迁移时直接 UPDATE 即可
    kind            TEXT    NOT NULL CHECK (kind IN ('image','video','mixed')), -- 库类型
    created_at      TIMESTAMP NOT NULL DEFAULT (datetime('now')),        -- 创建时间 ISO8601
    updated_at      TIMESTAMP NOT NULL DEFAULT (datetime('now')),         -- 更新时间
    UNIQUE (name)
);

-- ============================================================
-- 2. 扫描会话（含临时扫描）
-- ============================================================
CREATE TABLE IF NOT EXISTS scan_sessions (
    id              TEXT    PRIMARY KEY,                               -- UUID，每次扫描生成
    library_id      INTEGER,                                           -- 关联收藏库，NULL 表示独立临时扫描
    is_temporary    INTEGER NOT NULL DEFAULT 0 CHECK (is_temporary IN (0,1)), -- 1=临时扫描，0=正式入库
    status          TEXT    NOT NULL DEFAULT 'running'
                    CHECK (status IN ('running','completed','failed','cancelled','promoted')),
    started_at      TIMESTAMP NOT NULL DEFAULT (datetime('now')),
    finished_at     TIMESTAMP
);

-- ============================================================
-- 3. 媒体文件（图片/视频统一表，便于路径迁移与查询）
-- ============================================================
CREATE TABLE IF NOT EXISTS media (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    library_id      INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    scan_session_id TEXT    REFERENCES scan_sessions(id) ON DELETE SET NULL,
    kind            TEXT    NOT NULL CHECK (kind IN ('image','video')),
    relative_path   TEXT    NOT NULL,                                  -- 相对 library.path 的路径（UTF-8 规范化）
    file_size       INTEGER NOT NULL,                                  -- 字节
    mtime           TIMESTAMP NOT NULL,                                -- 文件修改时间
    -- 通用 metadata
    format          TEXT,                                              -- 扩展名/mime，如 jpg/mp4
    width           INTEGER,
    height          INTEGER,
    -- 图片专用
    phash           TEXT,                                              -- 16进制字符串
    dhash           TEXT,
    ahash           TEXT,
    -- 视频专用
    duration_ms     INTEGER,                                           -- 视频时长（毫秒）
    video_codec     TEXT,
    audio_codec     TEXT,
    frame_rate      REAL,
    bit_rate        INTEGER,
    oshash          TEXT,                                              -- Stash/OpenSubtitles 视频文件级指纹，粗筛用
    -- 去重与精确匹配
    sha1            TEXT,                                              -- 完整文件 SHA1
    thumbnail_path  TEXT,                                              -- 缩略图相对存储路径
    created_at      TIMESTAMP NOT NULL DEFAULT (datetime('now')),
    UNIQUE (library_id, relative_path)
);
CREATE INDEX IF NOT EXISTS idx_media_sha1   ON media(sha1) WHERE sha1 IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_media_phash  ON media(phash) WHERE phash IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_media_kind   ON media(kind);
CREATE INDEX IF NOT EXISTS idx_media_lib    ON media(library_id);

CREATE INDEX IF NOT EXISTS idx_media_oshash ON media(oshash) WHERE oshash IS NOT NULL;

-- ============================================================
-- 4. 后台任务（统一任务队列）
-- ============================================================
CREATE TABLE IF NOT EXISTS background_tasks (
    id                  TEXT    PRIMARY KEY,                             -- UUID
    kind                TEXT    NOT NULL CHECK (kind IN ('scan','repair','temporary_scan','report_image','report_video','promote')),
    status              TEXT    NOT NULL DEFAULT 'queued'
                        CHECK (status IN ('queued','running','completed','failed','cancelled')),
    title               TEXT    NOT NULL,                               -- 任务显示名称
    dedupe_key          TEXT,                                           -- 去重键（仅 queued/running 时唯一）
    library_id          INTEGER,                                        -- 关联收藏库
    scan_session_id     TEXT,                                           -- 关联扫描会话
    payload_json        TEXT,                                           -- 任务参数 JSON
    phase               TEXT    NOT NULL DEFAULT 'queued',              -- 当前阶段描述
    total_items         INTEGER NOT NULL DEFAULT 0,                     -- 总文件数
    processed_items     INTEGER NOT NULL DEFAULT 0,                     -- 已处理数
    succeeded_items     INTEGER NOT NULL DEFAULT 0,                     -- 成功数
    skipped_items       INTEGER NOT NULL DEFAULT 0,                     -- 跳过数
    failed_items        INTEGER NOT NULL DEFAULT 0,                     -- 失败数
    result_json         TEXT,                                           -- 结果 JSON
    error_message       TEXT,                                           -- 错误信息
    queued_at           TIMESTAMP NOT NULL DEFAULT (datetime('now')),
    started_at          TIMESTAMP,
    updated_at          TIMESTAMP NOT NULL DEFAULT (datetime('now')),
    finished_at         TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_tasks_dedupe
    ON background_tasks(dedupe_key)
    WHERE dedupe_key IS NOT NULL AND status IN ('queued','running');
CREATE INDEX IF NOT EXISTS idx_tasks_status ON background_tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_queued ON background_tasks(queued_at) WHERE status IN ('queued','running');
