-- 迁移 v7：media 表新增 cover_phash 列（视频封面帧 pHash，供以图搜图对比视频缩略图）。
-- 普通加列，media 无 CHECK 约束变更，无需重建表。
ALTER TABLE media ADD COLUMN cover_phash TEXT;
