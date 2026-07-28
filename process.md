# 开发进度

## 2026-07-28

### 已完成
- **数据库设计**：生成 `schema.sql`，包含 libraries / scan_sessions / media / video_frames / config 共 5 张表。
  - 图片与视频统一进 `media`，`kind` 区分。
  - 所有文件路径存**相对路径**，根目录迁移仅改 `libraries.path`。
  - 增量扫描判定改为 `(library_id, relative_path, mtime, file_size)`。
  - 临时扫描用 `scan_sessions.is_temporary=1` 标记，不另建物理临时表。
  - pHash/dHash/aHash/oshash 以 16 进制字符串存储。
  - 阈值与采样比例写入 `config` 表，运行时可调。
- **schema 校验**：因本机无 sqlite3 CLI，使用 `modernc.org/sqlite` 在内存库执行 `schema.sql`，建表与默认配置插入均通过，已清理临时校验工程。
- **PRD 修订**（已落回 `需求文档.md`）：
  1. 3.2 跳过判定：由"路径命中即跳过"改为"路径+mtime+size 一致才跳过"，避免漏扫被修改的文件。
  2. 3.1 路径迁移：明确相对路径设计，根目录迁移只需 UPDATE 一处。
  3. 5.5 匹配算法：单向 A→B 改为双向最近邻取 max，避免漏检。
  4. 6.2 临时扫描：改为同表 + is_temporary 标记，避免跨表 JOIN 不可见；入库改为移动文件而非复制。
  5. 新增第 8 节"数据库设计要点"，作为表结构与 PRD 的对照锚点。
- **任务清单**：生成 `task.md`，覆盖阶段 0~10。

### 进行中
- 无（等待启动阶段 0.3：Go 模块与目录结构初始化）。

### 待决
- Go 服务端目录结构与编码规范待 0.3 启动时确定。
- 配置文件格式（yaml/env/仅 DB 表）待 0.4 选型。

### 文件变更
- 新增：`schema.sql`、`task.md`、`process.md`
- 修改：`需求文档.md`

---

## 2026-07-28（第二阶段）

### 已完成
- **阶段 0 全量 + 1.1/1.2**：按确认方案落地。
  - `cmd/server/main.go`：入口，加载 config → 打开 DB → Migrate。
  - `internal/config/config.go`：Viper 加载 config.yaml，支持 `MEMABLE_*` env 覆盖；默认值与 PRD 对齐。
  - `internal/db/db.go`：modernc.org/sqlite 连接（WAL/foreign_keys/busy_timeout/单连接池），go:embed schema.sql，schema_version 表管理迁移（当前 v1）。
  - `internal/db/schema.sql`：从根 schema.sql 同步，增加 schema_version 表。
  - `internal/errx/errx.go`：错误包装骨架。
  - `config.yaml`：默认配置。
  - `internal/db/db_test.go`：内存库 Open+Migrate 测试，通过。
- **验证**：`go vet ./...` 通过；`go test ./internal/db/ -v` 通过；`go build ./cmd/server` 可启动并输出"memable 服务启动完成（阶段 0+1.1）"。

### 进行中
- 无（本轮范围已完成）。

### 待决
- 1.3 Repository CRUD 是否立即继续，还是先停在阶段 0+1.1。
- 日志库选型（标准 log vs slog vs zerolog）未最终确定；当前仅用 fmt 输出，未引入结构化日志。

### 文件变更
- 新增：`go.mod`、`go.sum`、`config.yaml`、`cmd/server/main.go`、`internal/config/config.go`、`internal/db/db.go`、`internal/db/schema.sql`、`internal/db/db_test.go`、`internal/errx/errx.go`
- 修改：`schema.sql`（增加 schema_version 表）、`task.md`

---

## 2026-07-28（第三阶段）

### 已完成
- **结构化日志**：选用标准库 `log/slog`（零依赖，符合完全本地约束），`internal/logx` 初始化 text/json handler，级别与格式由 config.yaml `log.level`/`log.format` 控制；main 入口已全面替换为 slog。
- **阶段 1 全部完成**：
  - `internal/repo/models.go`：Library / ScanSession / Media / VideoFrame 模型。
  - `internal/repo/library_repo.go`：CRUD + `UpdatePath` 根目录迁移。
  - `internal/repo/session_repo.go`：CRUD + `Promote` 临时扫描入库。
  - `internal/repo/media_repo.go`：Upsert（ON CONFLICT 更新）、`NeedScan` 增量判定、`FindBySha1`、`SearchByPath` 全路径模糊搜索（LIKE 转义）。
- `internal/repo/frame_repo.go`：早期关键帧 Repository，已因最新 PRD 调整而待删除。
  - `internal/repo/tx.go`：`WithTx` 事务封装 + SQLITE_BUSY 指数退避重试（50/100/200ms...）。
- **踩坑修复**：modernc.org/sqlite 不会把 TEXT 列的 datetime 字符串自动转成 time.Time；解决方式为时间列声明类型改为 `TIMESTAMP` + DSN 增加 `_time_format=sqlite`（已用最小用例验证）。`schema.sql` 时间列类型已同步更新（根目录与 internal/db 两份）。
- **验证**：`go vet ./...` 通过；`go test ./...` 6 个测试全部通过（库 CRUD、会话生命周期含 Promote、NeedScan 三种变化场景、SHA1 查重、模糊搜索、事务回滚）；二进制启动输出结构化日志 `level=INFO msg="memable 服务启动完成" schema_version=1`。

### 进行中
- 无（阶段 0+1 全部完成）。

### 待决
- 是否进入阶段 2（目录遍历、增量判定接入、ffprobe 封装）——ffprobe 需要本机安装 ffmpeg 套件，需确认环境或允许跳过视频采集的单测。

### 文件变更
- 新增：`internal/logx/logx.go`、`internal/repo/models.go`、`internal/repo/tx.go`、`internal/repo/library_repo.go`、`internal/repo/session_repo.go`、`internal/repo/media_repo.go`、`internal/repo/frame_repo.go`、`internal/repo/repo_test.go`
- 修改：`cmd/server/main.go`（slog）、`internal/config/config.go`（LogConfig）、`config.yaml`（log 段）、`internal/db/db.go`（_time_format）、`schema.sql`（时间列 TIMESTAMP）、`internal/db/schema.sql`（同步）、`task.md`

---

## 2026-07-28（第四阶段）

### 已完成
- **阶段 2 媒体采集全部完成**：
  - `internal/media/types.go`：媒体类型识别（图片 jpg/jpeg/png/gif；视频 mp4/mkv/avi/mov/wmv/flv/webm/m4v）与相对路径规范化。
  - `internal/media/walk.go`：递归目录遍历，输出 `AbsPath`、`RelativePath`、`Kind`、`Size`、`Mtime`。
  - `internal/media/hash.go`：SHA1 流式哈希，适配大文件。
  - `internal/media/image.go`：图片 metadata 采集（DecodeConfig，仅读头部，不解码完整像素）。
  - `internal/media/video.go`：ffprobe JSON 解析（duration/codec/format/width/height/frame_rate/bit_rate）。
  - `internal/scan/service.go`：扫描编排，创建 scan_session，遍历文件，调用 `NeedScan`，需要处理时计算 SHA1 与 metadata 并 Upsert 到 `media`，完成后更新会话状态。
- **环境确认**：本机 `ffmpeg` / `ffprobe` 可用，版本 8.1 essentials build。
- **验证**：`gofmt -w ./cmd ./internal`、`go vet ./...`、`go test ./... -count=1` 全部通过；测试覆盖目录遍历、图片 metadata、SHA1、ffmpeg 生成临时视频后 ffprobe 解析、扫描服务增量跳过与文件修改后重扫。

### 进行中
- 无（阶段 2 全部完成）。

### 待决
- 是否进入阶段 3（pHash/dHash/aHash 与视频 oHash）。图片相似哈希需要确定是否引入第三方库，或先用标准库自行实现基础版本。

### 文件变更
- 新增：`internal/media/types.go`、`internal/media/walk.go`、`internal/media/hash.go`、`internal/media/image.go`、`internal/media/video.go`、`internal/media/media_test.go`、`internal/scan/service.go`、`internal/scan/service_test.go`
- 修改：`internal/repo/media_repo.go`（Upsert 后回读 ID，避免 ON CONFLICT 场景 ID 不准）、`task.md`、`process.md`

---

## 2026-07-28（第五阶段）

### 已完成
- **阶段 3.1 图片感知哈希完成**：使用 Go 标准库实现 `aHash`、`dHash`、`pHash`，均输出 64 bit / 16 位十六进制字符串。
  - `aHash`：8x8 灰度均值比较。
  - `dHash`：9x8 灰度相邻像素差分。
  - `pHash`：32x32 灰度图做二维 DCT，取左上 8x8 低频，跳过 DC 分量，用中位数生成 64 bit 指纹。
  - 新增 `HammingHex64` 供后续相似度计算使用。
- **扫描接入**：`internal/scan/service.go` 在图片 metadata 采集后同步计算并写入 `media.phash/dhash/ahash`。
- **验证**：`gofmt -w ./cmd ./internal`、`go vet ./...`、`go test ./... -count=1` 全部通过；测试覆盖图片哈希长度、稳定性、Hamming 距离，以及扫描入库后哈希字段写入。

### 进行中
- 阶段 3.2 视频 oshash 待确认算法定义。

### 待决
- PRD 只写“oHash 参考 Stash”，没有给出可执行算法细节。后续已确认兼容 Stash/OpenSubtitles OSHash。

### 文件变更
- 新增：`internal/media/perceptual.go`
- 修改：`internal/media/media_test.go`、`internal/scan/service.go`、`internal/scan/service_test.go`、`task.md`、`process.md`

---

## 2026-07-28（第六阶段）

### 已完成
- **阶段 3.2 视频 oshash 完成**：按用户确认实现严格兼容 Stash/OpenSubtitles OSHash。
  - 算法：`file_size + uint64_le_sum(first 64KiB) + uint64_le_sum(last 64KiB)`，输出 16 位十六进制字符串。
  - 新增 `internal/media/oshash.go`，提供 `OSHashFile` 与 `OSHashReader`。
  - 当前代码仍写入旧字段 `media.ohash`，待 schema/代码迁移为 `media.oshash`。
  - PRD 已将术语统一为 `oshash`。
- **测试**：移植 Stash 官方测试向量，覆盖 regular、短文件、尾部碰撞等场景；阶段 3.3 标记完成。
- **验证**：`gofmt -w ./cmd ./internal`、`go vet ./...`、`go test ./... -count=1` 全部通过。

### 进行中
- 无（阶段 3 全部完成）。

### 待决
- 是否进入阶段 4（图片缩略图、视频封面图、临时 sprite pHash 生成）。视频不再持久化关键帧序列。

### 文件变更
- 新增：`internal/media/oshash.go`
- 修改：`internal/media/media_test.go`、`internal/scan/service.go`、`需求文档.md`、`task.md`、`process.md`

---

## 2026-07-29（PRD 调整：视频相似检测与媒体产物）

### 已完成
- **参考 Stash 视频相似检测修改 PRD**：保留 25 张截图拼 5x5 sprite 后生成单个 pHash，并用 Hamming 距离 + 可选时长差进行重复分组；sprite 过程产物只临时生成并清理。
- **`需求文档.md` 修改**：
  - 视频只持久化一张封面图，封面时间点按视频时长选择，黑屏/近纯色时最多 5 次回退重试。
  - 视频 sprite pHash 仍作为第三步视觉相似检测核心，25 张截图和 sprite 图不入库。
  - 删除视频关键帧序列、`video_frames` 关键帧 hash、9x9 精确复核及第五关键帧封面规则。
  - 术语统一为 `oshash`，明确字段迁移目标为 `media.oshash`。
- **`task.md` 修改**：阶段 4 改为图片缩略图、视频封面、临时 sprite pHash；阶段 6 删除关键帧精确复核。

### 进行中
- 无（本次仅 PRD/任务调整）。

### 待决
- 进入阶段 4 实现时，需要生成视频封面图和临时 sprite pHash；不生成或持久化关键帧序列。

### 文件变更
- 修改：`需求文档.md`、`task.md`、`process.md`
