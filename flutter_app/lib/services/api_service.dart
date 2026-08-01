// api_service.dart：HTTP API 客户端
// 代码注释使用中文
import 'dart:convert';
import 'dart:io';
import 'package:http/http.dart' as http;
import '../models/models.dart';

/// API 服务客户端
class ApiService {
  final String baseUrl;

  ApiService({this.baseUrl = 'http://localhost:8080'});

  // ===== 健康检查 =====

  Future<void> health() async {
    final res = await http.get(Uri.parse('$baseUrl/api/health'));
    if (res.statusCode != 200) throw Exception('API 健康检查失败');
  }

  /// 获取服务端存储位置（缩略图目录、日志位置），供设置页展示。
  Future<Map<String, dynamic>> fetchSettings() async {
    final res = await http.get(Uri.parse('$baseUrl/api/settings'));
    if (res.statusCode != 200) throw Exception('获取设置失败: ${res.body}');
    return jsonDecode(res.body) as Map<String, dynamic>;
  }

  // ===== 收藏库管理 =====

  Future<List<Library>> getLibraries() async {
    final res = await http.get(Uri.parse('$baseUrl/api/libraries'));
    if (res.statusCode != 200) throw Exception('获取库列表失败: ${res.body}');
    final list = jsonDecode(res.body) as List<dynamic>;
    return list
        .map((e) => Library.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  Future<Library> createLibrary(
    String name,
    String path, {
    String kind = 'image',
  }) async {
    final res = await http.post(
      Uri.parse('$baseUrl/api/libraries'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'name': name, 'path': path, 'kind': kind}),
    );
    if (res.statusCode != 201) throw Exception('创建库失败: ${res.body}');
    return Library.fromJson(jsonDecode(res.body));
  }

  Future<void> deleteLibrary(int id) async {
    final res = await http.delete(Uri.parse('$baseUrl/api/libraries/$id'));
    if (res.statusCode != 200) throw Exception('删除库失败: ${res.body}');
  }

  Future<void> updateLibraryPath(int id, String newPath) async {
    final res = await http.put(
      Uri.parse('$baseUrl/api/libraries/$id'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'path': newPath}),
    );
    if (res.statusCode != 200) throw Exception('更新路径失败: ${res.body}');
  }

  // ===== 扫描 =====

  /// 启动同步扫描；force 仅适用于正式收藏库。
  Future<Map<String, dynamic>> startScan({
    int? libraryId,
    String? scanPath,
    bool force = false,
  }) async {
    if (libraryId != null) {
      final res = await http.post(
        Uri.parse('$baseUrl/api/libraries/$libraryId/scan'),
        headers: {'Content-Type': 'application/json'},
        body: jsonEncode({'force': force}),
      );
      if (res.statusCode != 200 && res.statusCode != 202)
        throw Exception('启动同步扫描失败: ${res.body}');
      return jsonDecode(res.body);
    } else if (scanPath != null) {
      final res = await http.post(
        Uri.parse('$baseUrl/api/scan/temporary'),
        headers: {'Content-Type': 'application/json'},
        body: jsonEncode({'path': scanPath}),
      );
      if (res.statusCode != 200 && res.statusCode != 202)
        throw Exception('启动临时扫描失败: ${res.body}');
      return jsonDecode(res.body);
    }
    throw Exception('必须指定 libraryId 或 scanPath');
  }

  Future<Map<String, dynamic>> scanLibrary(
    int libraryId, {
    bool force = false,
  }) async {
    final res = await http.post(
      Uri.parse('$baseUrl/api/libraries/$libraryId/scan'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'force': force}),
    );
    if (res.statusCode != 200 && res.statusCode != 202)
      throw Exception('启动同步扫描失败: ${res.body}');
    return jsonDecode(res.body);
  }

  /// 启动补齐 SHA1 后台任务（主扫描不计算视频 SHA1，需要时单独补齐）。
  Future<Map<String, dynamic>> scanSha1(int libraryId) async {
    final res = await http.post(
      Uri.parse('$baseUrl/api/libraries/$libraryId/scan-sha1'),
    );
    if (res.statusCode != 200 && res.statusCode != 202)
      throw Exception('启动补齐 SHA1 失败: ${res.body}');
    return jsonDecode(res.body);
  }

  Future<Map<String, dynamic>> scanTemporary(String path) async {
    final res = await http.post(
      Uri.parse('$baseUrl/api/scan/temporary'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'path': path}),
    );
    if (res.statusCode != 200 && res.statusCode != 202)
      throw Exception('启动临时扫描失败: ${res.body}');
    return jsonDecode(res.body);
  }

  Future<Map<String, dynamic>> getSession(String sessionId) async {
    final res = await http.get(Uri.parse('$baseUrl/api/sessions/$sessionId'));
    if (res.statusCode != 200) throw Exception('查询会话失败: ${res.body}');
    return jsonDecode(res.body);
  }

  Future<void> cancelSession(String sessionId) async {
    final res = await http.post(
      Uri.parse('$baseUrl/api/sessions/$sessionId/cancel'),
    );
    if (res.statusCode != 200) throw Exception('取消扫描失败: ${res.body}');
  }

  Future<Map<String, dynamic>> promoteSession(
    String sessionId,
    int libraryId,
  ) async {
    final res = await http.post(
      Uri.parse('$baseUrl/api/sessions/$sessionId/promote'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'library_id': libraryId}),
    );
    if (res.statusCode != 200) throw Exception('入库失败: ${res.body}');
    return jsonDecode(res.body);
  }

  // ===== 媒体操作 =====

  /// 在服务端机器上打开媒体文件
  Future<void> openMediaFile(int mediaId) async {
    final res = await http.post(
      Uri.parse('$baseUrl/api/media/$mediaId/open'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'action': 'file'}),
    );
    if (res.statusCode != 200) {
      final err = jsonDecode(res.body)['error'] ?? '打开文件失败';
      throw Exception(err);
    }
  }

  /// 在服务端机器上打开媒体所在目录（并选中文件）
  Future<void> openMediaDirectory(int mediaId) async {
    final res = await http.post(
      Uri.parse('$baseUrl/api/media/$mediaId/open'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'action': 'directory'}),
    );
    if (res.statusCode != 200) {
      final err = jsonDecode(res.body)['error'] ?? '打开目录失败';
      throw Exception(err);
    }
  }

  // ===== 任务管理 =====

  Future<List<BackgroundTask>> getTasks() async {
    final res = await http.get(Uri.parse('$baseUrl/api/tasks'));
    if (res.statusCode != 200) throw Exception('获取任务列表失败: ${res.body}');
    final list = jsonDecode(res.body) as List<dynamic>;
    return list
        .map((e) => BackgroundTask.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  Future<BackgroundTask> getTask(String taskId) async {
    final res = await http.get(Uri.parse('$baseUrl/api/tasks/$taskId'));
    if (res.statusCode != 200) throw Exception('查询任务失败: ${res.body}');
    return BackgroundTask.fromJson(jsonDecode(res.body));
  }

  Future<void> cancelTask(String taskId) async {
    final res = await http.post(Uri.parse('$baseUrl/api/tasks/$taskId/cancel'));
    if (res.statusCode != 200) throw Exception('取消任务失败: ${res.body}');
  }

  // ===== 搜索 =====

  /// 文字搜索
  Future<List<SearchResult>> searchText({required String query}) async {
    final res = await http.get(
      Uri.parse('$baseUrl/api/search?q=${Uri.encodeQueryComponent(query)}'),
    );
    if (res.statusCode != 200) throw Exception('搜索失败: ${res.body}');
    final data = jsonDecode(res.body);
    final results = data['results'] as List<dynamic>;
    return results
        .map((e) => SearchResult.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  /// 以图搜图（通过文件上传，服务端计算 pHash）
  Future<List<SearchResult>> searchImage({
    required File file,
    int maxDistance = 12,
  }) async {
    final req = http.MultipartRequest(
      'POST',
      Uri.parse('$baseUrl/api/search/image/upload'),
    );
    req.files.add(await http.MultipartFile.fromPath('image', file.path));
    final streamed = await req.send();
    final res = await http.Response.fromStream(streamed);
    if (res.statusCode != 200) throw Exception('以图搜图失败: ${res.body}');
    final data = jsonDecode(res.body);
    final results = data['results'] as List<dynamic>;
    return results
        .map((e) => SearchResult.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  /// 以图搜图（通过 phash）
  Future<List<SearchResult>> searchImageByPhash(
    String phash, {
    int maxDistance = 12,
  }) async {
    final res = await http.post(
      Uri.parse('$baseUrl/api/search/image'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'phash': phash, 'max_distance': maxDistance}),
    );
    if (res.statusCode != 200) throw Exception('以图搜图失败: ${res.body}');
    final data = jsonDecode(res.body);
    final results = data['results'] as List<dynamic>;
    return results
        .map((e) => SearchResult.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  // ===== 重复报告 =====

  /// 提交重复检测报告任务（图片 + 视频）
  Future<Map<String, dynamic>> submitReport() async {
    final results = <String, dynamic>{};

    // 提交图片报告任务
    final imgRes = await http.post(
      Uri.parse('$baseUrl/api/reports/image'),
      headers: {'Content-Type': 'application/json'},
      body: '{}',
    );
    if (imgRes.statusCode == 200 || imgRes.statusCode == 202) {
      final data = jsonDecode(imgRes.body);
      results['image_task_id'] = data['task_id'];
    }

    // 提交视频报告任务
    final vidRes = await http.post(
      Uri.parse('$baseUrl/api/reports/video'),
      headers: {'Content-Type': 'application/json'},
      body: '{}',
    );
    if (vidRes.statusCode == 200 || vidRes.statusCode == 202) {
      final data = jsonDecode(vidRes.body);
      results['video_task_id'] = data['task_id'];
    }

    if (results.isEmpty) {
      throw Exception('报告任务提交失败');
    }
    return results;
  }

  /// 提交重复报告生成任务（三张表持久化，支持范围/类型/阈值选项）。
  Future<Map<String, dynamic>> generateReport(ReportOptions options) async {
    final res = await http.post(
      Uri.parse('$baseUrl/api/reports/duplicate'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode(options.toJson()),
    );
    if (res.statusCode != 200 && res.statusCode != 202) {
      throw Exception('提交重复报告任务失败: ${res.body}');
    }
    return jsonDecode(res.body) as Map<String, dynamic>;
  }

  /// 读取最新重复报告摘要（无报告时返回 null）。
  Future<ReportSummary?> getReportSummary() async {
    final res = await http.get(Uri.parse('$baseUrl/api/reports/duplicate'));
    if (res.statusCode != 200) throw Exception('读取重复报告失败: ${res.body}');
    final data = jsonDecode(res.body) as Map<String, dynamic>;
    if (data['report'] == null) return null;
    final summary = ReportSummary.fromJson(
      data['report'] as Map<String, dynamic>,
    );
    return ReportSummary(
      id: summary.id,
      scope: summary.scope,
      mediaType: summary.mediaType,
      stale: summary.stale,
      totalGroups: summary.totalGroups,
      totalFiles: summary.totalFiles,
      freedBytes: (data['freed_bytes'] as num?)?.toInt() ?? 0,
      imageThreshold: summary.imageThreshold,
      videoPhashDistance: summary.videoPhashDistance,
      videoDurationDiffMs: summary.videoDurationDiffMs,
      oshashFilter: summary.oshashFilter,
      includeSha1: summary.includeSha1,
      createdAt: summary.createdAt,
    );
  }

  /// 读取报告生成选项默认值（与后端配置一致）。
  Future<Map<String, dynamic>> getReportDefaults() async {
    final res = await http.get(
      Uri.parse('$baseUrl/api/reports/duplicate/defaults'),
    );
    if (res.statusCode != 200) {
      throw Exception('读取报告默认值失败: ${res.body}');
    }
    return jsonDecode(res.body) as Map<String, dynamic>;
  }

  /// 重复报告分组分页数据。
  Future<DuplicateGroupPage> getReportGroups({
    int page = 1,
    int pageSize = 20,
    String kind = 'all',
    String? directory,
  }) async {
    final params = <String, String>{
      'page': '$page',
      'page_size': '$pageSize',
      'kind': kind,
      // 根目录必须显式传“.”；省略参数会被后端解释为“不筛选目录”。
      if (directory != null) 'directory': directory.isEmpty ? '.' : directory,
    };
    final uri = Uri.parse(
      '$baseUrl/api/reports/duplicate/groups',
    ).replace(queryParameters: params);
    final res = await http.get(uri);
    if (res.statusCode != 200) throw Exception('读取重复分组失败: ${res.body}');
    return DuplicateGroupPage.fromJson(
      jsonDecode(res.body) as Map<String, dynamic>,
    );
  }

  /// 重复报告目录树。
  Future<List<DuplicateTreeNode>> getReportTree() async {
    final res = await http.get(
      Uri.parse('$baseUrl/api/reports/duplicate/tree'),
    );
    if (res.statusCode != 200) throw Exception('读取报告目录树失败: ${res.body}');
    final list = jsonDecode(res.body) as List<dynamic>;
    return list
        .map((e) => DuplicateTreeNode.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  /// 一键清除重复文件（按目录/整页/单组 + 保留条件）。
  Future<ClearResult> clearDuplicates({
    required String scope,
    required String keep,
    String? directory,
    List<int>? groupIds,
    int? groupId,
    bool? permanent,
  }) async {
    final res = await http.post(
      Uri.parse('$baseUrl/api/reports/duplicate/clear'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({
        'scope': scope,
        'keep': keep,
        if (directory != null) 'directory': directory,
        if (groupIds != null) 'group_ids': groupIds,
        if (groupId != null) 'group_id': groupId,
        if (permanent != null) 'permanent': permanent,
      }),
    );
    if (res.statusCode != 200) throw Exception('清除重复文件失败: ${res.body}');
    return ClearResult.fromJson(jsonDecode(res.body) as Map<String, dynamic>);
  }

  /// 删除媒体（源文件默认移入回收站，可永久删除）。
  Future<DeleteResult> deleteMedia(
    List<int> mediaIds, {
    bool? permanent,
  }) async {
    final res = await http.post(
      Uri.parse('$baseUrl/api/media/delete'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({
        'media_ids': mediaIds,
        if (permanent != null) 'permanent': permanent,
      }),
    );
    if (res.statusCode != 200) throw Exception('删除媒体失败: ${res.body}');
    return DeleteResult.fromJson(jsonDecode(res.body) as Map<String, dynamic>);
  }

  // ===== 文件树 =====

  /// 获取目录的直属子项（懒加载，展开时按需获取）
  Future<List<FileTreeNode>> getFileTree(
    int libraryId, {
    String path = '',
  }) async {
    final uri = Uri.parse(
      '$baseUrl/api/libraries/$libraryId/tree',
    ).replace(queryParameters: path.isNotEmpty ? {'path': path} : null);
    final res = await http.get(uri);
    if (res.statusCode != 200) throw Exception('获取文件树失败: ${res.body}');
    final list = jsonDecode(res.body) as List<dynamic>;
    return list
        .map((e) => FileTreeNode.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  /// 列出库下指定目录的直属媒体（仅直接包含的文件，不含子目录）
  Future<List<Media>> getFiles(int libraryId, {String path = ''}) async {
    final uri = Uri.parse(
      '$baseUrl/api/libraries/$libraryId/files',
    ).replace(queryParameters: path.isNotEmpty ? {'path': path} : null);
    final res = await http.get(uri);
    if (res.statusCode != 200) throw Exception('获取文件列表失败: ${res.body}');
    final list = jsonDecode(res.body) as List<dynamic>;
    return list.map((e) => Media.fromJson(e as Map<String, dynamic>)).toList();
  }

  /// 删除目录（同步执行，返回删除结果）
  Future<Map<String, dynamic>> deleteDirectory(
    int libraryId,
    String dirPath,
  ) async {
    final res = await http.delete(
      Uri.parse('$baseUrl/api/libraries/$libraryId/directories'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'path': dirPath}),
    );
    if (res.statusCode != 200 && res.statusCode != 202) {
      throw Exception('删除目录失败: ${res.body}');
    }
    return jsonDecode(res.body);
  }

  /// 缩略图 URL（kind: image/video；缩略图根目录按类型区分）
  String thumbnailUrl(String kind, String thumbnailPath) {
    return '$baseUrl/api/thumbnails/$kind/$thumbnailPath';
  }

  // ===== 工具 - 文件统计 =====

  /// 创建文件统计（传入目录路径，服务端遍历计算）。
  Future<FileStats> createFileStats(String dirPath) async {
    final res = await http.post(
      Uri.parse('$baseUrl/api/tools/file-stats'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'dir_path': dirPath}),
    );
    if (res.statusCode != 201) throw Exception('统计失败: ${res.body}');
    return FileStats.fromJson(jsonDecode(res.body));
  }

  /// 获取所有统计记录。
  Future<List<FileStats>> getFileStatsList() async {
    final res = await http.get(Uri.parse('$baseUrl/api/tools/file-stats'));
    if (res.statusCode != 200) throw Exception('获取统计列表失败: ${res.body}');
    final list = jsonDecode(res.body) as List<dynamic>;
    return list
        .map((e) => FileStats.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  /// 获取单条统计记录。
  Future<FileStats> getFileStats(int id) async {
    final res = await http.get(Uri.parse('$baseUrl/api/tools/file-stats/$id'));
    if (res.statusCode != 200) throw Exception('查询统计记录失败: ${res.body}');
    return FileStats.fromJson(jsonDecode(res.body));
  }

  /// 删除统计记录。
  Future<void> deleteFileStats(int id) async {
    final res = await http.delete(
      Uri.parse('$baseUrl/api/tools/file-stats/$id'),
    );
    if (res.statusCode != 200) throw Exception('删除统计记录失败: ${res.body}');
  }
}
