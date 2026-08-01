-- 迁移 v5：media 表新增索引，加速重复检测与临时扫描数据查询
CREATE INDEX IF NOT EXISTS idx_media_scan_session_kind ON media(scan_session_id, kind);
CREATE INDEX IF NOT EXISTS idx_media_kind_created ON media(kind, created_at);
