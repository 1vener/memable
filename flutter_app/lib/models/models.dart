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
  final List<FileTreeNode> children;

  FileTreeNode({
    required this.name,
    required this.path,
    required this.isDir,
    this.size = 0,
    this.children = const [],
  });

  factory FileTreeNode.fromJson(Map<String, dynamic> json) {
    return FileTreeNode(
      name: (json['name'] as String?) ?? '',
      path: (json['path'] as String?) ?? '',
      isDir: (json['is_dir'] as bool?) ?? false,
      size: (json['size'] as num?)?.toInt() ?? 0,
      children: (json['children'] as List<dynamic>?)
              ?.map((e) => FileTreeNode.fromJson(e as Map<String, dynamic>))
              .toList() ??
          [],
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
