# 开发任务清单

> 对应 `需求文档.md` v1.0 + 第8节数据库设计；表结构见 `schema.sql`。
> 状态图例：`[ ]` 待办 · `[~]` 进行中 · `[x]` 完成

## 阶段 0 - 基础设施

- [x] 0.1 设计 SQLite 表结构脚本 `schema.sql`
- [x] 0.2 校验 schema.sql 可在 SQLite 执行（modernc.org/sqlite 内存库通过）
- [x] 0.3 初始化 Go 模块（`go mod init`）、目录结构（cmd/server, internal/...）
- [x] 0.4 配置文件加载（config.yaml + Viper）：缩略图目录/最大边、采样比例、相似度阈值、worker 并发数等运行参数
- [x] 0.5 日志/错误处理骨架（log/slog 结构化日志 internal/logx + internal/errx）

## 阶段 1 - 存储层

- [x] 1.1 数据库连接（modernc.org/sqlite，WAL，foreign_keys=ON，busy_timeout，单连接池，_time_format=sqlite）
- [x] 1.2 migrations：go:embed schema.sql + schema_version 表（版本 2）
- [x] 1.3 Repository：libraries / scan_sessions / media CRUD（含 NeedScan 增量判定、SHA1 查重、全路径模糊搜索、ListByKind、UpdateLibrary）
- [x] 1.4 事务封装与重试（WithTx + SQLITE_BUSY 指数退避）

## 阶段 2 - 媒体采集

- [x] 2.1 目录遍历器（按 library 递归，输出相对路径 + mtime + size）
- [x] 2.2 增量判定：`(library_id, relative_path, mtime, file_size)` 命中则跳过
- [x] 2.3 类型识别（图片/视频）
- [x] 2.4 图片 metadata（size/format/width/height/created/modified/path）
- [x] 2.5 视频元数据：封装 `ffprobe -print_format json` 解析（duration/codec/format/...）
- [x] 2.6 SHA1 流式哈希

## 阶段 3 - Hash 计算

- [x] 3.1 pHash / dHash / aHash（Go image）
- [x] 3.2 视频 oshash（兼容 Stash/OpenSubtitles OSHash）
- [x] 3.3 单元测试：已知图片/视频 fixture 的哈希稳定

## 阶段 4 - 缩略图与视频视觉指纹

- [x] 4.1 图片缩略图：最大边 300px，相对路径落盘，路径可配置
- [x] 4.2 视频封面时间点选择：短视频 50%、中等视频 30%、长视频 10%
- [x] 4.3 ffmpeg 抽取单张封面，黑屏/近纯色检测与最多 5 次回退重试
- [x] 4.4 视频封面 resize（最大边 300px）并写入 `media.thumbnail_path`
- [x] 4.5 Stash 风格视频 sprite pHash：25 张临时截图（避开首尾 5%）拼成 5x5 sprite，生成 `media.phash` 后清理临时文件

## 阶段 5 - Worker Pool 调度

- [x] 5.1 Worker pool（可配置并发数）
- [x] 5.2 扫描任务入队、取消、状态（scan_sessions.status）
- [x] 5.3 失败重试与进度上报

## 阶段 6 - 相似/重复检测

- [x] 6.1 图片：SHA1 精确 + pHash/dHash/aHash 相似（Hamming）
- [x] 6.2 视频：SHA1 精确 → oshash 粗筛 → Stash 风格 sprite pHash 距离+时长差候选分组
- [x] 6.3 阈值判定（sprite pHash Hamming 距离、允许时长差）
- [x] 6.4 重复报告生成（HTML，含完整路径 + 图片缩略图/视频封面）

## 阶段 7 - 搜索

- [x] 7.1 文本搜索：文件名模糊匹配全路径 + sha1 精确
- [x] 7.2 以图搜图：对比图片缩略图与视频封面图
- [x] 7.3 临时目录扫描流程（is_temporary=1，临时缩略图目录）
- [x] 7.4 入库流程：移动文件 + UPDATE is_temporary=0 + 迁移缩略图目录

## 阶段 8 - 收藏库管理

- [x] 8.1 库 CRUD（libraries）
- [x] 8.2 根目录迁移（仅 UPDATE libraries.path，相对路径不变）
- [x] 8.3 文件树展示 + 删除联动（级联删除 media + 物理删除缩略图）

## 阶段 9 - Flutter 客户端

- [x] 9.1 与 Go 服务端 API 对接
- [x] 9.2 库管理 / 扫描进度 / 搜索 / 报告查看 UI

## 阶段 10 - 打包与验收

- [x] 10.1 本地打包（不依赖云服务）
- [ ] 10.2 大规模库性能压测（需准备数千图片/视频环境）
- [x] 10.3 文档与 README
