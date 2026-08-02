# Agent Guidance

## 仓库状态（当前）

- 项目已实现为可运行系统：Go 后端 + Flutter 客户端 + SQLite，产品规格见 `需求文档.md`（行为变更需同步更新该文档）。
- 顶层结构：
  - `cmd/server`（入口）、`cmd/migrate`（迁移工具）
  - `internal/`：`api`（HTTP 处理器）、`config`、`db`（内嵌 schema.sql + migrations/）、`repo`（数据仓库）、`media`（解码/哈希/缩略图/sprite/封面）、`scan`、`duplicate`（重复报告服务）、`search`、`task`（任务调度）、`worker`、`recycle`（回收站）、`errx`、`logx`
  - `flutter_app/lib`：`screens`、`services`、`models`、`widgets`、`utils`（时间本地化 `time_fmt.dart`）
  - 根目录：`schema.sql`、`config.yaml`、`go.mod`、`需求文档.md`、`AGENTS.md`、`Build-Server.ps1`
- 数据库迁移：`internal/db` 内嵌 `schema.sql` 与 `migrations/`（v2=重复报告三表；v3=background_tasks.kind 增加 report_duplicate；v4=background_tasks.kind 增加 scan_sha1；v5=media 表新增 scan_session_id/kind 与 kind/created_at 索引；v6=background_tasks.kind 增加 report_directory + 目录对比报告独立三表；v7=media 表新增 cover_phash 列），当前 `schema_version=7`。新表结构变更需新增迁移文件并在 `internal/db/db.go` 的 `steps` 注册；修改带 CHECK 约束的表需重建表迁移（参照 003/004/006），并同步内嵌 schema.sql 与根目录 schema.sql。`ALTER TABLE` 类迁移（如 v7 加列）需在 steps 里配 `skip`（`columnExists`）做幂等保护：新库 schema.sql 已含该列，仅存量库真正执行。

## 核心架构速览

- 数据流：Flutter → REST（127.0.0.1:8080）→ `internal/api` → `repo`/服务层 → SQLite；媒体处理走 `internal/media`（ffprobe/ffmpeg 解码、SHA1、pHash/dHash/aHash、OSHASH、sprite pHash、封面、400px JPEG 缩略图）。
- 图片解码：Go 原生解码，超过 `maxDirectDecodePixels`（2400 万像素）改走 FFmpeg 限分辨率转码（最长边 3000px），防超大图解码 OOM。
- 视频 SHA1：主扫描/修复扫描**不生成**视频 SHA1；独立 `scan_sha1` 后台任务按收藏库补齐 `sha1 IS NULL` 的记录（不强制重算）。视频缩略图内容寻址 key 用 `oshash+file_size+duration_ms`（`VideoThumbnailKey`），图片仍用 SHA1。
- sprite pHash：5%~95% 区间分 5 段，每段 `-ss` 快速定位只解码 1 秒窗口抽 5 帧（`fps=5,scale=160:-1,tile=5x1`），Go 拼 5×5 后算 pHash；失败依次回退单条 `trim+fps+tile`、逐帧 25 次。封面由 sprite 中帧向两侧选非黑屏/纯色帧（`ComputeVideoSpritePHashAndCover` 同时返回 sprite pHash 与封面帧 pHash，封面帧 pHash 写入 `media.cover_phash`）。视频重复检测中，时长 `<4000ms` 的短视频不比较 sprite pHash，只按 SHA1 或 OSHash 相同合并；`>=4000ms` 的普通视频继续使用 sprite pHash + 时长差。
- 以图搜图匹配口径：图片对比 `media.phash`（全图，与缩略图等价）；**视频对比 `media.cover_phash`（封面帧 pHash），绝不能用 sprite pHash（25 帧拼贴图与单帧查询图不可比，会导致视频永远匹配不上）**。存量视频 cover_phash 由 `needsSync` 判定（`CoverPHash == nil` 视为需重扫）在下次完整性/修复扫描时自动补齐。
- 数据库：DSN 一律用 `_pragma` 形式设置 `journal_mode(WAL)`、`foreign_keys(1)`、`busy_timeout`、`synchronous(NORMAL)`、`cache_size`、`mmap_size`；**modernc 驱动只认 `_pragma`，裸参数（如 `_journal_mode=WAL`）会被静默忽略**。媒体入库为单条 `Upsert`（`INSERT ... RETURNING id`，无批量写入）。
- 扫描判定与进度：增量/完整性判定用内存快照（`mediaSnapshot`，扫描前一次加载全库记录），逐文件不再查库；`repo.ProgressFunc` 带 `totalBytes/processedBytes`，ETA 按字节吞吐计算且排除跳过文件，速度显示为字节/秒。
- 重复报告：三张表 `duplicate_reports/groups/members`；生成任务 kind=`report_duplicate`，单事务替换旧报告；删除类变更即时级联并清理成员数 <2 的组；新增/重新处理置 `stale=1`。视频短时长规则：`duration_ms < 4000` 时 SHA1 或 OSHash 任一相同即合并，跳过 sprite pHash；`>=4000ms` 的普通视频继续使用 sprite pHash + 时长差。**任务双队列**：报告类任务（`report_image`/`report_video`/`report_duplicate`）在独立报告队列串行执行（`TaskRepo.DequeueNextReport`/`reportKindsCSV`，`QueuePosition` 按所属队列计数），与主队列（扫描等）互不阻塞、可并发运行。
- 重复检测性能（百万级）：pHash 相似度用 **MIH（Multi-Index Hashing）** 索引（`internal/duplicate/mih.go`，64bit 拆 4 段 ×16bit，鸽笼原理无漏检）替代 O(n²) 两两比较；pHash 预解析为 uint64（`media.ParseHex64`/`HammingUint64`，不用 fmt.Sscanf）；视频先按 `duration_ms` 分桶（桶宽=时长差阈值）再比较。**增量检测**：`Service.Generate` 与上次报告参数（scope/media_type/各阈值）一致**且旧报告有分组**时才增量（仅对 `media.created_at >= 上次报告时间` 的媒体发起查询并与旧组合并；媒体 Upsert 冲突时刷新 `created_at`）；旧报告为空（历史 bug 产物）时回退全量，防止空报告传染。**报告写入显式逐表清空三张表再写入**（不依赖外键级联，避免历史外键未启用时残留孤儿组/成员），用 prepared statement 批量插入。
- 目录对比报告（`dir_duplicate_reports/groups/members` 独立三表）：**独立页面** `flutter_app/lib/screens/dir_compare_screen.dart`（侧边栏"目录对比"入口，Ctrl+6），与重复报告页分离；"开始/重新生成"按钮 → 选收藏库 + 目录（含子目录）→ 任务 kind=`report_directory`（报告队列）→ `Service.GenerateDirCompare`；检测复用 queryMask 机制（scope=`dir_vs_rest`，目标=所选目录媒体，索引全量），SHA1/OSHash/pHash 组必须**同时含目标与存量**（目标内部、存量内部不比较），成员带 `is_target` 标记；不替换重复报告，重新生成替换自身。**结果展示与操作与重复报告页完全一致**（目录树/重复分组双视图 + 媒体类型过滤 + 分页 10/20/50/100 + card_swiper 叠卡 + 右键菜单[打开/复制/打开/排除重复/其它目录二级菜单/删除此文件外本组重复文件] + 一键清除[目录/本页 + 六种保留条件] + 组详情弹窗），成员卡片/叠卡带 `is_target`"所选目录"角标；后端接口：`GET .../directory-compare/tree`（kind 过滤）、`GET .../groups`（kind 过滤）、`POST .../exclude`（`DirExcludeMedia`）、`POST .../clear`（`DirClear`，scope=directory/page/group + keep）；`PruneAfterMediaChange` 同时刷新普通与目录对比两张报告（`Dir.PruneGroupsAndUpdateStats` 清 <2 组 + 重算统计）。前端公共组件（`StackedCluster`/`LazyThumb`/`DirChip`/`DuplicateMemberCard`[menuBuilder 参数化]/`showKeepDialog`/`confirmClearDialog`/`pickKeepIndex`/工具函数）统一在 `widgets/report_common.dart`，两个报告页共用，勿在页面内复制私有副本。
- 临时扫描：与正式扫描同走 `ExecuteScan`（`temporary=true` 时不清理缺失文件、创建独立临时库、会话 `is_temporary=1`）；临时媒体**不参与重复报告**（`ListAllFormal`/`ListByKind` 排除）但**参与搜索**；`GET /api/libraries` 返回 `is_temporary`（EXISTS 判定该库是否有临时会话）供前端区分标识。临时库入库走 `POST /api/libraries/{id}/promote`（body：`target_library_id`/`target_dir`）：移动本地文件 + 同时更新 `media.library_id`/`relative_path`（目标路径前缀 target_dir）；目标路径为"目标目录/临时库最后一级目录名/原相对路径"（如 D:\tmp\test → D:\本地\2026\test\...），目标库存在相同相对路径时以"临时库最后一级目录名(1)"递增避让；完成后删除临时库记录。
- 主要 API：`/api/reports/duplicate`（生成/摘要/分组分页(支持 `directory` 过滤)/目录树/默认值/clear）、`POST /api/reports/duplicate/exclude`（排除重复：从当前报告移除指定媒体，仅当前报告生效，重新生成后重新参与检测）、`/api/reports/directory-compare`（目录对比：生成/摘要/分组分页(支持 `kind` 过滤)/目录树/exclude/clear）、`/api/media/delete`、`POST /api/libraries/{id}/scan-sha1`（补齐 SHA1 任务）、`POST /api/libraries/{id}/promote`（临时库入库到指定目录）、`POST /api/libraries/{id}/directories/rename|move`（库内目录重命名/移动，同步接口）、`GET /api/settings`（缩略图/日志保存位置）、`/api/tools/file-stats[/{id}/diff[/export]]`（文件统计与目录差异，xlsx 由 `internal/api/xlsx.go` 用 Go 标准库 archive/zip 手写，无第三方依赖）。
- 目录重命名/移动语义：同步接口（先盘后库，库更新失败回滚磁盘）；DB 侧批量前缀更新走 `repo.MediaRepo.RenameDirectoryPrefix`（substr 精确目录边界，非 LIKE，防 `a/bc` 误伤）；目标同名 409 不避让；**禁止移动到自身/子孙**；仅同卷 `os.Rename`（跨卷直接报错）；**Windows 大小写改名（A→a）需临时名两步中转且跳过目标存在检查**（NTFS 大小写不敏感，直接 rename 失败/无效果、os.Stat 必命中自身）。前端右键菜单在 `library_screen.dart` 的 `_FileTreePanel`（移动到用 `_MoveDirDialog` 懒加载目录树弹窗，禁选自身/子孙），操作后 `_afterDirOp` 失效旧路径展开缓存并刷新树与文件列表。
- 删除安全：源文件默认移入系统回收站（`internal/recycle`，Windows 用 PowerShell），`delete.permanent: true` 可永久删除；目录删除为同步接口；缩略图为生成物直接删除。

## 已实现的关键行为

- 报告页目录树视图：card_swiper 叠卡（45° 平移 20px、数量胶囊、底部首个文件信息、`_LazyThumb` 懒加载）；**目录树默认全部展开**，点击可收起，刷新后收起状态保留。
- 分组视图：平铺分页（默认 20 组/页，可切 10/20/50/100）；右键菜单（打开/复制/打开媒体、**排除重复**（`POST /api/reports/duplicate/exclude`，删除该文件全部组成员关系并清理 <2 的组、刷新统计，仅当前报告生效）、其它目录二级菜单、删除此文件外本组重复文件）；一键清除（目录/整页 + 六种保留条件）；删除后局部刷新，空组不再展示。
- 打开文件路径：统一走 `POST /api/media/{id}/open`（action=directory），Windows 用 `ShellExecuteW`（`internal/api/reveal_windows.go`）以 `/select,"<path>"` 形式（引号只包路径）启动 explorer 并选中文件——**不能用 os/exec 传 `"/select,path"` 整体引号形式，explorer 不识别会导致不选中**；macOS 用 `open -R`、Linux 依次尝试 nautilus/dolphin `--select` 后回退 xdg-open，打开后文件处于选中状态。
- 任务页展示完整统计/元信息/结果摘要（含 `total_bytes/processed_bytes`）、ETA（双口径：剩余字节/字节速率 与 剩余文件数折算/文件速率，取较大值 + 轻量平滑）与速度（MB/s）；主屏幕底部常驻运行中任务进度条（HomeScreen 每 2 秒轮询）。
- 扫描处理每个文件时打印 `处理文件 dir=... file=...` 日志；`log.file` 配置可把日志追加写入文件（默认控制台标准输出）。
- 设置页新增"存储位置"卡片：图片缩略图目录、视频封面目录、日志位置（数据来自 `GET /api/settings`，路径为服务端绝对路径）。
- 时间字段后端一律存/返 UTC，客户端经 `utils/time_fmt.dart` 的 `formatLocalTime` 统一转本地显示。
- 主题：M3 定制、默认浅色、自定义主题色（设置页预设 + HEX，SharedPreferences 持久化）。
- 收藏库目录删除：同步执行返回 `deleted_media/local_deleted`；删除后清理父级展开缓存并刷新树。
- 文件统计目录差异：历史记录行内 diff 按钮 → 重新遍历目录对比历史 `file_tree`，返回新增/删除文件相对路径（字典序）；前端构建目录树（叶子为文件名，默认折叠）；导出 Excel 两 sheet（新增/删除文件列表，列名"文件路径"，完整绝对路径）。

## 开发约定

- 代码注释使用中文；行为/需求变更同步更新 `需求文档.md`。
- 缩略图路径：`media.thumbnail_path` 只存相对"该类型缩略图根目录"的内容寻址路径（`{key[:2]}/{key}.jpg`，不含类型前缀）；根目录默认系统缓存（Windows `%LOCALAPPDATA%`、macOS `~/Library/Caches`、Linux `$XDG_CACHE_HOME` 下 `memable/thumbnails/{image,video}`），`config.yaml` 的 `thumbnail.image_dir/video_dir` 可覆盖；静态访问 `/api/thumbnails/{kind}/{rel}`，整体移动缩略图目录只需改配置无需改库。缩略图为 400px（`thumbnail.max_edge`）JPEG q90（CatmullRom 缩放，`x/image/draw`；旧版为 300px PNG 最近邻——`scan` 的 `needRepairFromSnapshot` 把 `.png` 后缀视为旧格式，完整性扫描时重生为 `.jpg` 并清理无引用旧文件，勿改回 PNG）。
- 检测阈值：图片相似度默认由 `similarity.image_phash_distance` 换算百分比（10→84%）；普通视频 pHash 距离/时长差取配置；普通视频的 oshash 仅作候选提示，**不得作为硬过滤**（否则视觉相似但 oshash 不同的视频会漏检）；短视频（`duration_ms < 4000`）是例外，按 SHA1 或 OSHash 相同直接合并且不比较 sprite pHash。
- 视频 SHA1 语义：主扫描不生成视频 SHA1；`needsSync` 对视频不要求 sha1（否则未补齐的视频会被反复重扫）；`scan_sha1` 任务只补 `sha1 IS NULL` 的记录；补齐后视频可参与 `sha1_exact` 精确分组；主扫描重新处理视频时会清空旧 sha1。
- JSON 字段一律 snake_case（repo/服务层结构体必须写 json tag；曾因缺 tag 导致客户端解析失败）。
- 进度回调 `repo.ProgressFunc` 签名含字节参数（totalBytes/processedBytes）；修改签名需同步 runner 的 EWMA/ETA 计算、scan 服务上报与相关测试。
- 时间：DB 默认 `datetime('now')` 为 UTC；新增时间展示一律走 `formatLocalTime`（客户端转本地时区）。

## 构建与测试

- 后端：`go build ./...`、`go test ./...`；Windows 可执行文件使用 `.\Build-Server.ps1` 强制按 `GOARCH=amd64` 构建，避免 32 位进程地址空间限制（本机 Go 工具链是 windows/386，直接 `go build` 得到 32 位产物，大图解码会 OOM）。沙箱下需设置工作区内 GOCACHE（如 `$env:GOCACHE='D:\environment\memable\.go-cache'`）否则写入被拒。
- 客户端：在 `flutter_app/` 下运行 `dart analyze`（依赖已解析，package_config 存在；沙箱内可能因写用户分析目录失败，需提权运行）。`flutter test` 需要本地 flutter_tester 产物与网络，离线环境可能无法运行。
- 运行：`server.exe -config config.yaml`（默认 127.0.0.1:8080）；客户端 `flutter run -d windows`。

## 注意事项（踩坑记录）

- 不要把运行时产物提交：`memable.db*`、`server.exe`、`thumbnail/`、`internal/scan/thumbnail/`、`report_*.html`、`debug.log`。
- 批量写入已按用户要求回退为单条 `Upsert`（每文件一个事务）；**不要再改回批量写入**。
- modernc.org/sqlite 的 DSN 只认 `_pragma` 参数；`_journal_mode`/`_foreign_keys`/`_busy_timeout` 裸参数会被静默忽略（曾导致 WAL/外键实际未启用）。
- `flutter_app/lib/screens/report_screen.dart`、`library_screen.dart` 曾被外部多次修改，改动前先读现状；长 apply_patch 易因中文/空白匹配失败，宜小步、按精确上下文提交。
- Go 返回列表一律用非 nil 空切片（`make([]T, 0)`，不要 `var s []T`），否则无数据时 JSON 序列化为 `null`；Flutter 端反序列化数组必须防御性 `as List<dynamic>? ?? []`（曾导致搜索无结果时 `type 'Null is not a subtype of type 'List<dynamic>'` 崩溃，服务端 `internal/search/service.go` 与客户端 `api_service.dart` 的 `data['results']` 同时修）。新接口两侧开发时默认遵循此约定。
- 修改 `background_tasks` 等带 CHECK 约束的表时，需重建表迁移（参考 migrations/003、004），并同步内嵌 schema.sql 与根目录 schema.sql。
