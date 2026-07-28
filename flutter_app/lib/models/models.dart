// models.dart：数据模型
// 代码注释使用中文

/// 收藏库模型
class Library {
  final int id;
  final String name;
  final String path;
  final String kind;
  final String? createdAt;
  final String? updatedAt;

  Library({
    required this.id,
    required this.name,
    required this.path,
    required this.kind,
    this.createdAt,
    this.updatedAt,
  });

  factory Library.fromJson(Map<String, dynamic> json) {
    return Library(
      id: json['id'] as int,
      name: json['name'] as String,
      path: json['path'] as String,
      kind: json['kind'] as String,
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
      id: json['id'] as int,
      libraryId: json['library_id'] as int,
      kind: json['kind'] as String,
      relativePath: json['relative_path'] as String,
      fileSize: json['file_size'] as int,
      sha1: json['sha1'] as String?,
      phash: json['phash'] as String?,
      thumbnailPath: json['thumbnail_path'] as String?,
      width: json['width'] as int?,
      height: json['height'] as int?,
      durationMs: json['duration_ms'] as int?,
    );
  }
}

/// 扫描会话模型
class ScanSession {
  final String id;
  final int? libraryId;
  final bool isTemporary;
  final String status;
  final String? startedAt;
  final String? finishedAt;

  ScanSession({
    required this.id,
    this.libraryId,
    required this.isTemporary,
    required this.status,
    this.startedAt,
    this.finishedAt,
  });

  factory ScanSession.fromJson(Map<String, dynamic> json) {
    return ScanSession(
      id: json['id'] as String,
      libraryId: json['library_id'] as int?,
      isTemporary: json['is_temporary'] == 1 || json['is_temporary'] == true,
      status: json['status'] as String,
      startedAt: json['started_at'] as String?,
      finishedAt: json['finished_at'] as String?,
    );
  }
}

/// 搜索结果
class SearchResult {
  final Media media;
  final String fullPath;
  final int distance;

  SearchResult({
    required this.media,
    required this.fullPath,
    this.distance = 0,
  });

  factory SearchResult.fromJson(Map<String, dynamic> json) {
    return SearchResult(
      media: Media.fromJson(json['Media'] ?? json['media'] ?? json),
      fullPath: json['FullPath'] ?? json['full_path'] ?? '',
      distance: json['Distance'] ?? json['distance'] ?? 0,
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
      name: json['name'] as String,
      path: json['path'] as String,
      isDir: json['is_dir'] as bool,
      size: json['size'] as int? ?? 0,
      children: (json['children'] as List<dynamic>?)
              ?.map((e) => FileTreeNode.fromJson(e as Map<String, dynamic>))
              .toList() ??
          [],
    );
  }
}
