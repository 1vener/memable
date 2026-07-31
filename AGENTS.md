# Agent Guidance

## 仓库状态（当前）

- 项目已实现为可运行系统：Go 后端 + Flutter 客户端 + SQLite，产品规格见 `需求文档.md`（行为变更需同步更新该文档）。
- 顶层结构：
  - `cmd/server`（入口）、`cmd/migrate`（迁移工具）
  - `internal/`：`api`（HTTP 处理器）、`config`、`db`（内嵌 schema.sql + migrations/）、`repo`（数据仓库）、`media`（解码/哈希/缩略图/sprite）、`scan`、`duplicate`（重复报告服务）、`search`、`task`（任务调度）、`worker`、`recycle`（回收站）、`errx`、`logx`
  - `flutter_app/lib`：`screens`、`services`、`models`、`widgets`
  - 根目录：`schema.sql`、`config.yaml`、`go.mod`、`需求文档.md`、`AGENTS.md`
- 数据库迁移：`internal/db` 内嵌 `schema.sql` 与 `migrations/`（v2=重复报告三表；v3=background_tasks.kind 增加 report_duplicate）。新表结构变更需新增迁移文件并在 `internal/db/db.go` 的 `steps` 注册。

## 核心架构速览

- 数据流：Flutter → REST（127.0.0.1:8080）→ `internal/api` → `repo`/服务层 → SQLite；媒体处理走 `internal/media`（ffprobe/ffmpeg 解码、SHA1、pHash/dHash/aHash、OSHASH、25 帧 sprite pHash、单张封面、300px 缩略图）。
- 重复报告：三张表 `duplicate_reports/groups/members`；生成任务 kind=`report_duplicate`，单事务替换旧报告；删除类变更即时级联并清理成员数 <2 的组；新增/重新处理置 `stale=1`。
- 报告 API：`/api/reports/duplicate`（生成/摘要/分组分页(支持 `directory` 过滤)/目录树/默认值/clear），`/api/media/delete`。
- 删除安全：源文件默认移入系统回收站（`internal/recycle`，Windows 用 PowerShell），`delete.permanent: true` 可永久删除；目录删除为同步接口；缩略图为生成物直接删除。

## 已实现的关键行为

- 报告页目录树视图：card_swiper 叠卡（45° 平移 20px、数量胶囊、底部首个文件信息、`_LazyThumb` 懒加载）；**目录树默认全部展开**，点击可收起，刷新后收起状态保留。
- 分组视图：平铺分页（默认 20 组/页，可切 10/20/50/100）；右键菜单（打开/复制/打开媒体、其它目录二级菜单、删除此文件外本组重复文件）；一键清除（目录/整页 + 六种保留条件）；删除后局部刷新，空组不再展示。
- 任务页展示完整统计/元信息/结果摘要；主屏幕底部常驻运行中任务进度条（HomeScreen 每 2 秒轮询）。
- 主题：M3 定制、默认浅色、自定义主题色（设置页预设 + HEX，SharedPreferences 持久化）。
- 收藏库目录删除：同步执行返回 `deleted_media/local_deleted`；删除后清理父级展开缓存并刷新树。

## 开发约定

- 代码注释使用中文；行为/需求变更同步更新 `需求文档.md`。
- 缩略图路径：`media.thumbnail_path` 只存相对"该类型缩略图根目录"的内容寻址路径（`{key[:2]}/{key}.png`，不含类型前缀）；根目录默认系统缓存（Windows `%LOCALAPPDATA%`、macOS `~/Library/Caches`、Linux `$XDG_CACHE_HOME` 下 `memable/thumbnails/{image,video}`），`config.yaml` 的 `thumbnail.image_dir/video_dir` 可覆盖；静态访问 `/api/thumbnails/{kind}/{rel}`，整体移动缩略图目录只需改配置无需改库。
- 检测阈值：图片相似度默认由 `similarity.image_phash_distance` 换算百分比（10→84%）；视频 pHash 距离/时长差取配置；oshash 仅作候选提示，**不得作为硬过滤**（否则视觉相似但 oshash 不同的视频会漏检）。
- JSON 字段一律 snake_case（repo/服务层结构体必须写 json tag；曾因缺 tag 导致客户端解析失败）。

## 构建与测试

- 后端：`go build ./...`、`go test ./...`；可执行文件 `go build -o server.exe ./cmd/server`。沙箱下需设置工作区内 GOCACHE（如 `$env:GOCACHE='D:\environment\memable\.go-cache'`）否则写入被拒。
- 客户端：在 `flutter_app/` 下运行 `dart analyze`（依赖已解析，package_config 存在）。`flutter test` 需要本地 flutter_tester 产物与网络，离线环境可能无法运行。
- 运行：`server.exe -config config.yaml`（默认 127.0.0.1:8080）；客户端 `flutter run -d windows`。

## 注意事项（踩坑记录）

- 不要把运行时产物提交：`memable.db*`、`server.exe`、`thumbnail/`、`internal/scan/thumbnail/`、`report_*.html`。
- `flutter_app/lib/screens/report_screen.dart` 曾被外部多次修改，改动前先读现状；长 apply_patch 易因中文/空白匹配失败，宜小步、按精确上下文提交。
- 修改 `background_tasks` 等带 CHECK 约束的表时，需重建表迁移（参考 migrations/003）。
