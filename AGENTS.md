# Agent Guidance

## Repository State

- Aside from this guide, the repository currently contains only `需求文档.md`; there is no source tree, manifest, lockfile, test setup, CI workflow, or Git metadata.
- Treat `需求文档.md` as the product specification, not as an executable implementation or command reference.
- Do not invent build, test, lint, or deployment commands until the corresponding tool configuration is added.

## Product Constraints

- The planned system is fully local: Flutter client, Go server, SQLite database, `ffprobe`/`ffmpeg` media tooling, and a Go worker pool. Do not introduce cloud-service dependencies without an explicit requirement.
- Media processing must retain full source paths and associate generated thumbnails with their source records; video processing persists one cover thumbnail and only uses sprite screenshots as temporary inputs.
- Exact duplicate detection uses SHA1; image similarity uses pHash/dHash/aHash; video similarity uses Stash/OpenSubtitles sprite pHash with `oshash` as a file-level coarse filter.
- Video sprite pHash uses 25 temporary screenshots sampled across 5%-95% of duration; do not persist a keyframe sequence or use `video_frames` for new behavior.
- Temporary search scans use temporary database tables and temporary thumbnail/frame directories. Promote them to the library only after the user chooses to import them.
- Generated thumbnails have a maximum edge of 300 px; their storage path is configurable.

## Documentation

- Product requirements and processing details are in `需求文档.md`; update that document when behavior or requirements change.
- The PRD specifies Chinese code comments. Follow that convention when implementation begins, unless a later repository-level style configuration supersedes it.
