-- 迁移 v9：首页媒体分页按类型和修改时间排序索引
CREATE INDEX IF NOT EXISTS idx_media_kind_mtime_id ON media(kind, mtime DESC, id DESC);
