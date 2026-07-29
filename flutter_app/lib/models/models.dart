// models.dart：数据模型
// 代码注释使用中文

/// 收藏库模型
class Library {
  final int id;
  final String name;
  final String path;
  final String kind;
  final String? thumbnailDir;
  final String? createdAt;
  final String? updatedAt;

  Library({
    required this.id,
    required this.name,
    required this.path,
    required this.kind,
    this.thumbnailDir,
    this.createdAt,
    this.updatedAt,
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
  final int? width;
  final int? height;
  final int? durationMs;

  Media({
    required this.id,
    required this.libraryId,
    required this.kind,
    required this.relativePath,
    required this.fileSize,
    this.sha1,
    this.phash,
    this.thumbnailPath,
    this.width,
    this.height,
    this.durationMs,
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
      width: (json['width'] as num?)?.toInt(),
      height: (json['height'] as num?)?.toInt(),
      durationMs: (json['duration_ms'] as num?)?.toInt(),
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
    final fullPath = (json['FullPath'] ?? json['full_path'] ?? media.relativePath) as String;
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

/// 重复检测报告
class DuplicateReport {
  final int groupCount;
  final String path;

  DuplicateReport({
    required this.groupCount,
    required this.path,
  });

  factory DuplicateReport.fromJson(Map<String, dynamic> json) {
    return DuplicateReport(
      groupCount: json['groups'] as int? ?? 0,
      path: json['report_path'] as String? ?? '',
    );
  }
}

/// 后台任务模型
class BackgroundTask {
  final String id;
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
      queuedAt: (json['queued_at'] as String?) ?? '',
      startedAt: json['started_at'] as String?,
      finishedAt: json['finished_at'] as String?,
      queuePosition: (json['queue_position'] as num?)?.toInt() ?? 0,
    );
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
      case 'scan': return '扫描';
      case 'repair': return '修复扫描';
      case 'temporary_scan': return '临时扫描';
      case 'report_image': return '图片报告';
      case 'report_video': return '视频报告';
      case 'promote': return '入库';
      default: return kind;
    }
  }

  String get statusLabel {
    switch (status) {
      case 'queued': return '等待中';
      case 'running': return '运行中';
      case 'completed': return '已完成';
      case 'failed': return '失败';
      case 'cancelled': return '已取消';
      default: return status;
    }
  }
}
