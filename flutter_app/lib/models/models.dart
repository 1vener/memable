// models.dart：数据模型
// 代码注释使用中文
import 'dart:convert';

/// 收藏库模型
class Library {
  final int id;
  final String name;
  final String path;
  final String kind;
  final String? thumbnailDir;
  final String? createdAt;
  final String? updatedAt;
  final bool isTemporary; // 临时扫描库

  Library({
    required this.id,
    required this.name,
    required this.path,
    required this.kind,
    this.thumbnailDir,
    this.createdAt,
    this.updatedAt,
    this.isTemporary = false,
  });

  factory Library.fromJson(Map<String, dynamic> json) {
    return Library(
      id: (json['id'] as num?)?.toInt() ?? 0,
      name: (json['name'] as String?) ?? '',
      path: (json['path'] as String?) ?? '',
      kind: json['kind'] as String? ?? 'image',
      thumbnailDir: json['thumbnail_dir'] as String?,
      createdAt: json['created_at'] as String?,
      updatedAt: json['updated_at'] as String?,
      isTemporary: json['is_temporary'] == 1 || json['is_temporary'] == true,
    );
  }
}

/// 媒体文件模型
class Media {
  final int id;
  final int libraryId;
  final String kind;
  final String relativePath;
  final int fileSize;
  final String? sha1;
  final String? phash;
  final String? thumbnailPath;
  final String? format;
  final int? width;
  final int? height;
  final int? durationMs;
  final String? videoCodec;
  final String? audioCodec;
  final double? frameRate;
  final int? bitRate;
  final DateTime? mtime;

  Media({
    required this.id,
    required this.libraryId,
    required this.kind,
    required this.relativePath,
    required this.fileSize,
    this.sha1,
    this.phash,
    this.thumbnailPath,
    this.format,
    this.width,
    this.height,
    this.durationMs,
    this.videoCodec,
    this.audioCodec,
    this.frameRate,
    this.bitRate,
    this.mtime,
  });

  factory Media.fromJson(Map<String, dynamic> json) {
    return Media(
      id: (json['id'] as num?)?.toInt() ?? 0,
      libraryId: (json['library_id'] as num?)?.toInt() ?? 0,
      kind: (json['kind'] as String?) ?? '',
      relativePath: (json['relative_path'] as String?) ?? '',
      fileSize: (json['file_size'] as num?)?.toInt() ?? 0,
      sha1: json['sha1'] as String?,
      phash: json['phash'] as String?,
      thumbnailPath: json['thumbnail_path'] as String?,
      format: json['format'] as String?,
      width: (json['width'] as num?)?.toInt(),
      height: (json['height'] as num?)?.toInt(),
      durationMs: (json['duration_ms'] as num?)?.toInt(),
      videoCodec: json['video_codec'] as String?,
      audioCodec: json['audio_codec'] as String?,
      frameRate: (json['frame_rate'] as num?)?.toDouble(),
      bitRate: (json['bit_rate'] as num?)?.toInt(),
      mtime: json['mtime'] != null
          ? DateTime.tryParse(json['mtime'] as String)
          : null,
    );
  }
}

/// 扫描会话模型
class ScanSession {
  final String id;
  final int? libraryId;
  final bool isTemporary;
  final String status;
  final int scanned;
  final int imported;
  final int skipped;
  final String? startedAt;
  final String? finishedAt;

  ScanSession({
    required this.id,
    this.libraryId,
    required this.isTemporary,
    required this.status,
    this.scanned = 0,
    this.imported = 0,
    this.skipped = 0,
    this.startedAt,
    this.finishedAt,
  });

  factory ScanSession.fromJson(Map<String, dynamic> json) {
    return ScanSession(
      id: (json['id'] as String?) ?? '',
      libraryId: (json['library_id'] as num?)?.toInt(),
      isTemporary: json['is_temporary'] == 1 || json['is_temporary'] == true,
      status: (json['status'] as String?) ?? 'unknown',
      scanned: json['scanned'] as int? ?? 0,
      imported: json['imported'] as int? ?? 0,
      skipped: json['skipped'] as int? ?? 0,
      startedAt: json['started_at'] as String?,
      finishedAt: json['finished_at'] as String?,
    );
  }
}

/// 搜索结果
class SearchResult {
  final Media media;
  final String fullPath;
  final String name;
  final String? thumbnailUrl;
  final double score;

  SearchResult({
    required this.media,
    required this.fullPath,
    String? name,
    this.thumbnailUrl,
    this.score = 0.0,
  }) : name = name ?? fullPath.split('/').last.split('\\').last;

  factory SearchResult.fromJson(Map<String, dynamic> json) {
    final media = Media.fromJson(json['Media'] ?? json['media'] ?? json);
    final fullPath =
        (json['FullPath'] ?? json['full_path'] ?? media.relativePath) as String;
    final distance = (json['Distance'] ?? json['distance'] ?? 0) as int;
    // 将 distance 转换为 0-1 相似度分数（distance 越小越相似）
    final score = distance > 0 ? (1.0 - distance / 64.0).clamp(0.0, 1.0) : 1.0;
    return SearchResult(
      media: media,
      fullPath: fullPath,
      thumbnailUrl: media.thumbnailPath,
      score: score,
    );
  }
}

/// 文件树节点
class FileTreeNode {
  final String name;
  final String path;
  final bool isDir;
  final int size;
  final bool hasChildren;

  FileTreeNode({
    required this.name,
    required this.path,
    required this.isDir,
    this.size = 0,
    this.hasChildren = false,
  });

  factory FileTreeNode.fromJson(Map<String, dynamic> json) {
    return FileTreeNode(
      name: (json['name'] as String?) ?? '',
      path: (json['path'] as String?) ?? '',
      isDir: (json['is_dir'] as bool?) ?? false,
      size: (json['size'] as num?)?.toInt() ?? 0,
      hasChildren: (json['has_children'] as bool?) ?? false,
    );
  }
}

/// 应用内重复检测报告。
class DuplicateReport {
  final String kind;
  final int groupCount;
  final int fileCount;
  final List<DuplicateGroup> groups;

  DuplicateReport({
    required this.kind,
    required this.groupCount,
    required this.fileCount,
    required this.groups,
  });

  factory DuplicateReport.fromJson(Map<String, dynamic> json) {
    final groups =
        (json['groups'] as List<dynamic>? ?? [])
            .map((e) => DuplicateGroup.fromJson(e as Map<String, dynamic>))
            .toList();
    return DuplicateReport(
      kind: json['kind'] as String? ?? '',
      groupCount: (json['group_count'] as num?)?.toInt() ?? groups.length,
      fileCount: (json['file_count'] as num?)?.toInt() ?? 0,
      groups: groups,
    );
  }
}

class DuplicateGroup {
  final int index;
  final String reason;
  final List<DuplicateItem> items;

  DuplicateGroup({
    required this.index,
    required this.reason,
    required this.items,
  });

  factory DuplicateGroup.fromJson(Map<String, dynamic> json) => DuplicateGroup(
    index: (json['index'] as num?)?.toInt() ?? 0,
    reason: json['reason'] as String? ?? '',
    items:
        (json['items'] as List<dynamic>? ?? [])
            .map((e) => DuplicateItem.fromJson(e as Map<String, dynamic>))
            .toList(),
  );
}

class DuplicateItem {
  final int id;
  final int libraryId;
  final String kind;
  final String relativePath;
  final String fullPath;
  final String? thumbnailPath;
  final int fileSize;
  final int? width;
  final int? height;
  final int? durationMs;
  final DateTime? mtime;
  final List<String> duplicatePaths;
  final bool isTarget; // 目录对比报告：true=所选目录文件

  DuplicateItem({
    required this.id,
    required this.libraryId,
    required this.kind,
    required this.relativePath,
    required this.fullPath,
    this.thumbnailPath,
    required this.fileSize,
    this.width,
    this.height,
    this.durationMs,
    this.mtime,
    required this.duplicatePaths,
    this.isTarget = false,
  });

  factory DuplicateItem.fromJson(Map<String, dynamic> json) => DuplicateItem(
    id: (json['id'] as num?)?.toInt() ?? 0,
    libraryId: (json['library_id'] as num?)?.toInt() ?? 0,
    kind: json['kind'] as String? ?? '',
    relativePath: json['relative_path'] as String? ?? '',
    fullPath: json['full_path'] as String? ?? '',
    thumbnailPath: json['thumbnail_path'] as String?,
    fileSize: (json['file_size'] as num?)?.toInt() ?? 0,
    width: (json['width'] as num?)?.toInt(),
    height: (json['height'] as num?)?.toInt(),
    durationMs: (json['duration_ms'] as num?)?.toInt(),
    mtime: json['mtime'] != null
        ? DateTime.tryParse(json['mtime'] as String)
        : null,
    duplicatePaths:
        (json['duplicate_paths'] as List<dynamic>? ?? []).cast<String>(),
    isTarget: json['is_target'] as bool? ?? false,
  );
}

/// CloudDrive2 网盘目录条目（目录树懒加载）。
class NetdriveDirEntry {
  final String path; // CD2 完整路径
  final String name;
  final bool isDir;
  final bool hasChildren;

  NetdriveDirEntry({
    required this.path,
    required this.name,
    required this.isDir,
    this.hasChildren = false,
  });

  factory NetdriveDirEntry.fromJson(Map<String, dynamic> json) =>
      NetdriveDirEntry(
        path: (json['path'] as String?) ?? '',
        name: (json['name'] as String?) ?? '',
        isDir: json['is_dir'] == true,
        hasChildren: json['has_children'] == true,
      );
}

/// 后台任务模型
class BackgroundTask {  final String id;
  final String kind;
  final String status;
  final String title;
  final int? libraryId;
  final String? scanSessionId;
  final String phase;
  final int totalItems;
  final int processedItems;
  final int succeededItems;
  final int skippedItems;
  final int failedItems;
  final String? resultJson;
  final String? errorMessage;
  final double processingRate;
  final int? etaSeconds;
  final String queuedAt;
  final String? startedAt;
  final String? finishedAt;
  final int queuePosition;

  BackgroundTask({
    required this.id,
    required this.kind,
    required this.status,
    required this.title,
    this.libraryId,
    this.scanSessionId,
    this.phase = 'queued',
    this.totalItems = 0,
    this.processedItems = 0,
    this.succeededItems = 0,
    this.skippedItems = 0,
    this.failedItems = 0,
    this.resultJson,
    this.errorMessage,
    this.processingRate = 0,
    this.etaSeconds,
    required this.queuedAt,
    this.startedAt,
    this.finishedAt,
    this.queuePosition = 0,
  });

  factory BackgroundTask.fromJson(Map<String, dynamic> json) {
    return BackgroundTask(
      id: (json['id'] as String?) ?? '',
      kind: (json['kind'] as String?) ?? '',
      status: (json['status'] as String?) ?? 'unknown',
      title: (json['title'] as String?) ?? '',
      libraryId: (json['library_id'] as num?)?.toInt(),
      scanSessionId: json['scan_session_id'] as String?,
      phase: (json['phase'] as String?) ?? 'queued',
      totalItems: (json['total_items'] as num?)?.toInt() ?? 0,
      processedItems: (json['processed_items'] as num?)?.toInt() ?? 0,
      succeededItems: (json['succeeded_items'] as num?)?.toInt() ?? 0,
      skippedItems: (json['skipped_items'] as num?)?.toInt() ?? 0,
      failedItems: (json['failed_items'] as num?)?.toInt() ?? 0,
      resultJson: json['result_json'] as String?,
      errorMessage: json['error_message'] as String?,
      processingRate: (json['processing_rate'] as num?)?.toDouble() ?? 0,
      etaSeconds: (json['eta_seconds'] as num?)?.toInt(),
      queuedAt: (json['queued_at'] as String?) ?? '',
      startedAt: json['started_at'] as String?,
      finishedAt: json['finished_at'] as String?,
      queuePosition: (json['queue_position'] as num?)?.toInt() ?? 0,
    );
  }

  /// 格式化处理速度（后端按字节/秒上报，跳过文件不计入）
  String get formattedRate {
    if (processingRate <= 0) return '';
    final bps = processingRate;
    if (bps >= 1024 * 1024) {
      return '${(bps / 1024 / 1024).toStringAsFixed(1)} MB/秒';
    }
    if (bps >= 1024) {
      return '${(bps / 1024).toStringAsFixed(1)} KB/秒';
    }
    return '${bps.toStringAsFixed(1)} B/秒';
  }

  /// 格式化预计剩余时间
  String get formattedEta {
    if (etaSeconds == null || etaSeconds! <= 0) return '';
    final s = etaSeconds!;
    if (s < 60) return '预计剩余 $s 秒';
    if (s < 3600) {
      final m = s ~/ 60;
      final sec = s % 60;
      return '预计剩余 $m 分 $sec 秒';
    }
    final h = s ~/ 3600;
    final m = (s % 3600) ~/ 60;
    return '预计剩余 $h 小时 $m 分';
  }

  bool get isRunning => status == 'running';
  bool get isQueued => status == 'queued';
  bool get isCompleted => status == 'completed';
  bool get isFailed => status == 'failed';
  bool get isCancelled => status == 'cancelled';
  bool get isActive => isRunning || isQueued;
  bool get isTerminal => isCompleted || isFailed || isCancelled;

  double get progress => totalItems > 0 ? processedItems / totalItems : 0.0;

  String get kindLabel {
    switch (kind) {
      case 'scan':
        return '同步扫描';
      case 'repair':
        return '修复扫描';
      case 'temporary_scan':
        return '临时扫描';
      case 'report_image':
        return '图片重复统计';
      case 'report_video':
        return '视频重复统计';
      case 'promote':
        return '入库';
      case 'directory_delete':
        return '删除目录';
      case 'scan_sha1':
        return '补齐 SHA1';
      case 'netdrive_sha1':
        return 'CD2 补齐 SHA1';
      default:
        return kind;
    }
  }

  String get statusLabel {
    switch (status) {
      case 'queued':
        return '等待中';
      case 'running':
        return '运行中';
      case 'completed':
        return '已完成';
      case 'failed':
        return '失败';
      case 'cancelled':
        return '已取消';
      default:
        return status;
    }
  }
}

/// 文件统计记录模型。
class FileStats {
  final int id;
  final String dirPath;
  final int totalBytes;
  final int totalCount;
  final List<ExtStat> extStats;
  final List<FileTreeStatNode> fileTree;
  final String? createdAt;

  FileStats({
    required this.id,
    required this.dirPath,
    required this.totalBytes,
    required this.totalCount,
    required this.extStats,
    required this.fileTree,
    this.createdAt,
  });

  factory FileStats.fromJson(Map<String, dynamic> json) {
    final extStatsRaw = json['ext_stats'];
    final fileTreeRaw = json['file_tree'];
    List<ExtStat> extList = [];
    List<FileTreeStatNode> treeList = [];

    if (extStatsRaw is String && extStatsRaw.isNotEmpty) {
      final parsed = jsonDecode(extStatsRaw) as List<dynamic>;
      extList =
          parsed
              .map((e) => ExtStat.fromJson(e as Map<String, dynamic>))
              .toList();
    } else if (extStatsRaw is List) {
      extList =
          extStatsRaw
              .map((e) => ExtStat.fromJson(e as Map<String, dynamic>))
              .toList();
    }

    if (fileTreeRaw is String && fileTreeRaw.isNotEmpty) {
      final parsed = jsonDecode(fileTreeRaw) as List<dynamic>;
      treeList =
          parsed
              .map((e) => FileTreeStatNode.fromJson(e as Map<String, dynamic>))
              .toList();
    } else if (fileTreeRaw is List) {
      treeList =
          fileTreeRaw
              .map((e) => FileTreeStatNode.fromJson(e as Map<String, dynamic>))
              .toList();
    }

    return FileStats(
      id: (json['id'] as num?)?.toInt() ?? 0,
      dirPath: (json['dir_path'] as String?) ?? '',
      totalBytes: (json['total_bytes'] as num?)?.toInt() ?? 0,
      totalCount: (json['total_count'] as num?)?.toInt() ?? 0,
      extStats: extList,
      fileTree: treeList,
      createdAt: json['created_at'] as String?,
    );
  }

  String get totalBytesFormatted {
    if (totalBytes < 1024) return '$totalBytes B';
    if (totalBytes < 1024 * 1024)
      return '${(totalBytes / 1024).toStringAsFixed(1)} KB';
    if (totalBytes < 1024 * 1024 * 1024)
      return '${(totalBytes / (1024 * 1024)).toStringAsFixed(1)} MB';
    return '${(totalBytes / (1024 * 1024 * 1024)).toStringAsFixed(2)} GB';
  }
}

/// 目录差异对比结果（统计记录 vs 目录当前状态）。
class FileStatsDiff {
  final String dirPath;
  final List<String> added; // 新增文件相对路径（正斜杠，字典序）
  final List<String> removed; // 删除文件相对路径（正斜杠，字典序）
  final int addedCount;
  final int removedCount;

  FileStatsDiff({
    required this.dirPath,
    required this.added,
    required this.removed,
    required this.addedCount,
    required this.removedCount,
  });

  factory FileStatsDiff.fromJson(Map<String, dynamic> json) {
    return FileStatsDiff(
      dirPath: (json['dir_path'] as String?) ?? '',
      added:
          ((json['added'] as List<dynamic>?) ?? [])
              .map((e) => e.toString())
              .toList(),
      removed:
          ((json['removed'] as List<dynamic>?) ?? [])
              .map((e) => e.toString())
              .toList(),
      addedCount: (json['added_count'] as num?)?.toInt() ?? 0,
      removedCount: (json['removed_count'] as num?)?.toInt() ?? 0,
    );
  }
}

/// 扩展名统计。
class ExtStat {
  final String ext;
  final int bytes;
  final int count;
  final double pctCount;
  final double pctBytes;

  ExtStat({
    required this.ext,
    required this.bytes,
    required this.count,
    required this.pctCount,
    required this.pctBytes,
  });

  factory ExtStat.fromJson(Map<String, dynamic> json) {
    return ExtStat(
      ext: (json['ext'] as String?) ?? '',
      bytes: (json['bytes'] as num?)?.toInt() ?? 0,
      count: (json['count'] as num?)?.toInt() ?? 0,
      pctCount: (json['pct_count'] as num?)?.toDouble() ?? 0.0,
      pctBytes: (json['pct_bytes'] as num?)?.toDouble() ?? 0.0,
    );
  }
}

/// 文件树节点（递归结构）。
class FileTreeStatNode {
  final String name;
  final String path;
  final bool isDir;
  final String? ext;
  final int? size;
  final List<FileTreeStatNode>? children;

  FileTreeStatNode({
    required this.name,
    required this.path,
    required this.isDir,
    this.ext,
    this.size,
    this.children,
  });

  factory FileTreeStatNode.fromJson(Map<String, dynamic> json) {
    List<FileTreeStatNode>? children;
    if (json['children'] != null) {
      children =
          (json['children'] as List<dynamic>)
              .map((e) => FileTreeStatNode.fromJson(e as Map<String, dynamic>))
              .toList();
    }

    return FileTreeStatNode(
      name: (json['name'] as String?) ?? '',
      path: (json['path'] as String?) ?? '',
      isDir: (json['is_dir'] as bool?) ?? false,
      ext: json['ext'] as String?,
      size: (json['size'] as num?)?.toInt(),
      children: children,
    );
  }
}

/// 重复报告摘要（三张表持久化后的最新报告）。
class ReportSummary {
  final int? id;
  final String scope; // all / same_dir
  final String mediaType; // image / video / all
  final bool stale;
  final int totalGroups;
  final int totalFiles;
  final int freedBytes;
  final int imageThreshold;
  final int videoPhashDistance;
  final int videoDurationDiffMs;
  final bool oshashFilter;
  final bool includeSha1;
  final String? createdAt;

  ReportSummary({
    this.id,
    required this.scope,
    required this.mediaType,
    this.stale = false,
    this.totalGroups = 0,
    this.totalFiles = 0,
    this.freedBytes = 0,
    this.imageThreshold = 90,
    this.videoPhashDistance = 12,
    this.videoDurationDiffMs = 3000,
    this.oshashFilter = true,
    this.includeSha1 = true,
    this.createdAt,
  });

  factory ReportSummary.fromJson(Map<String, dynamic> json) => ReportSummary(
    id: (json['id'] as num?)?.toInt(),
    scope: (json['scope'] as String?) ?? 'all',
    mediaType: (json['media_type'] as String?) ?? 'all',
    stale: json['stale'] == 1 || json['stale'] == true,
    totalGroups: (json['total_groups'] as num?)?.toInt() ?? 0,
    totalFiles: (json['total_files'] as num?)?.toInt() ?? 0,
    imageThreshold: (json['image_threshold'] as num?)?.toInt() ?? 90,
    videoPhashDistance: (json['video_phash_distance'] as num?)?.toInt() ?? 12,
    videoDurationDiffMs: (json['video_duration_diff_ms'] as num?)?.toInt() ?? 3000,
    oshashFilter: json['oshash_filter'] == 1 || json['oshash_filter'] == true,
    includeSha1: json['include_sha1'] == 1 || json['include_sha1'] == true,
    createdAt: json['created_at'] as String?,
  );

  bool get isSameDir => scope == 'same_dir';
}

/// 重复报告分组分页数据。
class DuplicateGroupPage {
  final int total;
  final int page;
  final int pageSize;
  final int totalPages;
  final List<DuplicateGroupItem> items;

  DuplicateGroupPage({
    required this.total,
    required this.page,
    required this.pageSize,
    required this.totalPages,
    required this.items,
  });

  factory DuplicateGroupPage.fromJson(Map<String, dynamic> json) {
    final items = (json['items'] as List<dynamic>? ?? [])
        .map((e) => DuplicateGroupItem.fromJson(e as Map<String, dynamic>))
        .toList();
    return DuplicateGroupPage(
      total: (json['total'] as num?)?.toInt() ?? items.length,
      page: (json['page'] as num?)?.toInt() ?? 1,
      pageSize: (json['page_size'] as num?)?.toInt() ?? 20,
      totalPages: (json['total_pages'] as num?)?.toInt() ?? 1,
      items: items,
    );
  }
}

/// 重复分组项（组信息 + 成员）。
class DuplicateGroupItem {
  final int id;
  final String groupType;
  final String? directory;
  final int memberCount;
  final int freedBytes;
  final List<DuplicateItem> items;

  DuplicateGroupItem({
    required this.id,
    required this.groupType,
    this.directory,
    required this.memberCount,
    this.freedBytes = 0,
    required this.items,
  });

  factory DuplicateGroupItem.fromJson(Map<String, dynamic> json) {
    final items = (json['items'] as List<dynamic>? ?? [])
        .map((e) => DuplicateItem.fromJson(e as Map<String, dynamic>))
        .toList();
    return DuplicateGroupItem(
      id: (json['id'] as num?)?.toInt() ?? 0,
      groupType: (json['group_type'] as String?) ?? '',
      directory: json['directory'] as String?,
      memberCount: (json['member_count'] as num?)?.toInt() ?? items.length,
      freedBytes: (json['freed_bytes'] as num?)?.toInt() ?? 0,
      items: items,
    );
  }

  String get reasonLabel {
    switch (groupType) {
      case 'sha1':
        return 'SHA1 完全相同';
      case 'image_similar':
        return 'pHash 视觉相似';
      case 'video_similar':
        return 'sprite pHash 视觉相似';
      default:
        return groupType;
    }
  }
}

/// 重复报告目录树节点。
class DuplicateTreeNode {
  final String name;
  final String path;
  final int fileCount;
  final List<DuplicateTreeNode> children;

  DuplicateTreeNode({
    required this.name,
    required this.path,
    this.fileCount = 0,
    this.children = const [],
  });

  factory DuplicateTreeNode.fromJson(Map<String, dynamic> json) {
    final children = (json['children'] as List<dynamic>? ?? [])
        .map((e) => DuplicateTreeNode.fromJson(e as Map<String, dynamic>))
        .toList();
    return DuplicateTreeNode(
      name: (json['name'] as String?) ?? '',
      path: (json['path'] as String?) ?? '',
      fileCount: (json['file_count'] as num?)?.toInt() ?? 0,
      children: children,
    );
  }
}

/// 报告生成选项。
class ReportOptions {
  String scope;
  String mediaType;
  int imageThreshold;
  int videoPhashDistance;
  int videoDurationDiffMs;
  bool oshashFilter;
  bool includeSha1;

  ReportOptions({
    this.scope = 'all',
    this.mediaType = 'all',
    this.imageThreshold = 90,
    this.videoPhashDistance = 12,
    this.videoDurationDiffMs = 3000,
    this.oshashFilter = true,
    this.includeSha1 = true,
  });

  Map<String, dynamic> toJson() => {
    'scope': scope,
    'media_type': mediaType,
    'image_threshold': imageThreshold,
    'video_phash_distance': videoPhashDistance,
    'video_duration_diff_ms': videoDurationDiffMs,
    'oshash_filter': oshashFilter,
    'include_sha1': includeSha1,
  };
}

/// 清除重复文件结果。
class ClearResult {
  final int deletedFiles;
  final int freedBytes;
  final int remainingGroups;

  ClearResult({
    this.deletedFiles = 0,
    this.freedBytes = 0,
    this.remainingGroups = 0,
  });

  factory ClearResult.fromJson(Map<String, dynamic> json) => ClearResult(
    deletedFiles: (json['deleted_files'] as num?)?.toInt() ?? 0,
    freedBytes: (json['freed_bytes'] as num?)?.toInt() ?? 0,
    remainingGroups: (json['remaining_groups'] as num?)?.toInt() ?? 0,
  );
}

/// 删除媒体结果。
class DeleteResult {
  final int deletedFiles;
  final int freedBytes;

  DeleteResult({this.deletedFiles = 0, this.freedBytes = 0});

  factory DeleteResult.fromJson(Map<String, dynamic> json) => DeleteResult(
    deletedFiles: (json['deleted_files'] as num?)?.toInt() ?? 0,
    freedBytes: (json['freed_bytes'] as num?)?.toInt() ?? 0,
  );
}

/// 目录对比报告（所选目录 vs 存量数据，独立报告）。
class DirCompareReport {
  final int id;
  final int libraryId;
  final String directory;
  final String mediaType;
  final int totalGroups;
  final int totalFiles;
  final String? createdAt;

  DirCompareReport({
    required this.id,
    required this.libraryId,
    required this.directory,
    required this.mediaType,
    required this.totalGroups,
    required this.totalFiles,
    this.createdAt,
  });

  factory DirCompareReport.fromJson(Map<String, dynamic> json) =>
      DirCompareReport(
        id: (json['id'] as num?)?.toInt() ?? 0,
        libraryId: (json['library_id'] as num?)?.toInt() ?? 0,
        directory: json['directory'] as String? ?? '',
        mediaType: json['media_type'] as String? ?? 'all',
        totalGroups: (json['total_groups'] as num?)?.toInt() ?? 0,
        totalFiles: (json['total_files'] as num?)?.toInt() ?? 0,
        createdAt: json['created_at'] as String?,
      );
}

/// 目录对比报告摘要。
class DirCompareSummary {
  final DirCompareReport? report;
  final int freedBytes;

  DirCompareSummary({this.report, this.freedBytes = 0});

  factory DirCompareSummary.fromJson(Map<String, dynamic> json) =>
      DirCompareSummary(
        report: json['report'] == null
            ? null
            : DirCompareReport.fromJson(json['report'] as Map<String, dynamic>),
        freedBytes: (json['freed_bytes'] as num?)?.toInt() ?? 0,
      );
}

/// 目录对比组成员（复用 DuplicateItem，isTarget 标记所选目录文件）。

/// 目录对比分组项。
class DirCompareGroupItem {
  final int id;
  final String groupType;
  final int memberCount;
  final int freedBytes;
  final List<DuplicateItem> items;

  DirCompareGroupItem({
    required this.id,
    required this.groupType,
    required this.memberCount,
    this.freedBytes = 0,
    required this.items,
  });

  factory DirCompareGroupItem.fromJson(Map<String, dynamic> json) {
    final items = (json['items'] as List<dynamic>? ?? [])
        .map((e) => DuplicateItem.fromJson(e as Map<String, dynamic>))
        .toList();
    return DirCompareGroupItem(
      id: (json['id'] as num?)?.toInt() ?? 0,
      groupType: json['group_type'] as String? ?? '',
      memberCount: (json['member_count'] as num?)?.toInt() ?? items.length,
      freedBytes: (json['freed_bytes'] as num?)?.toInt() ?? 0,
      items: items,
    );
  }

  String get reasonLabel {
    switch (groupType) {
      case 'sha1':
        return 'SHA1 完全相同';
      case 'image_similar':
        return 'pHash 视觉相似';
      case 'video_similar':
        return 'sprite pHash 视觉相似';
      default:
        return groupType;
    }
  }
}

/// 目录对比分组分页结果。
class DirCompareGroupPage {
  final int total;
  final int page;
  final int pageSize;
  final int totalPages;
  final List<DirCompareGroupItem> items;

  DirCompareGroupPage({
    required this.total,
    required this.page,
    required this.pageSize,
    required this.totalPages,
    required this.items,
  });

  factory DirCompareGroupPage.fromJson(Map<String, dynamic> json) {
    final items = (json['items'] as List<dynamic>? ?? [])
        .map((e) => DirCompareGroupItem.fromJson(e as Map<String, dynamic>))
        .toList();
    return DirCompareGroupPage(
      total: (json['total'] as num?)?.toInt() ?? items.length,
      page: (json['page'] as num?)?.toInt() ?? 1,
      pageSize: (json['page_size'] as num?)?.toInt() ?? 20,
      totalPages: (json['total_pages'] as num?)?.toInt() ?? 1,
      items: items,
    );
  }
}
