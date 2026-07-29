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

  // ===== 收藏库管理 =====

  Future<List<Library>> getLibraries() async {
    final res = await http.get(Uri.parse('$baseUrl/api/libraries'));
    if (res.statusCode != 200) throw Exception('获取库列表失败: ${res.body}');
    final list = jsonDecode(res.body) as List<dynamic>;
    return list.map((e) => Library.fromJson(e as Map<String, dynamic>)).toList();
  }

  Future<Library> createLibrary(String name, String path, {String kind = 'image'}) async {
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

  /// 启动库扫描
  Future<Map<String, dynamic>> startScan({int? libraryId, String? scanPath}) async {
    if (libraryId != null) {
      final res = await http.post(Uri.parse('$baseUrl/api/libraries/$libraryId/scan'));
      if (res.statusCode != 200 && res.statusCode != 202) throw Exception('启动扫描失败: ${res.body}');
      return jsonDecode(res.body);
    } else if (scanPath != null) {
      final res = await http.post(
        Uri.parse('$baseUrl/api/scan/temporary'),
        headers: {'Content-Type': 'application/json'},
        body: jsonEncode({'path': scanPath}),
      );
      if (res.statusCode != 200 && res.statusCode != 202) throw Exception('启动临时扫描失败: ${res.body}');
      return jsonDecode(res.body);
    }
    throw Exception('必须指定 libraryId 或 scanPath');
  }

  Future<Map<String, dynamic>> scanLibrary(int libraryId) async {
    final res = await http.post(Uri.parse('$baseUrl/api/libraries/$libraryId/scan'));
    if (res.statusCode != 200 && res.statusCode != 202) throw Exception('启动扫描失败: ${res.body}');
    return jsonDecode(res.body);
  }

  /// 重复扫描：补采缺失元数据、补生成缩略图、采集新文件
  Future<Map<String, dynamic>> repairLibrary(int libraryId) async {
    final res = await http.post(Uri.parse('$baseUrl/api/libraries/$libraryId/repair'));
    if (res.statusCode != 200 && res.statusCode != 202) throw Exception('启动修复扫描失败: ${res.body}');
    return jsonDecode(res.body);
  }

  Future<Map<String, dynamic>> scanTemporary(String path) async {
    final res = await http.post(
      Uri.parse('$baseUrl/api/scan/temporary'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'path': path}),
    );
    if (res.statusCode != 200 && res.statusCode != 202) throw Exception('启动临时扫描失败: ${res.body}');
    return jsonDecode(res.body);
  }

  Future<Map<String, dynamic>> getSession(String sessionId) async {
    final res = await http.get(Uri.parse('$baseUrl/api/sessions/$sessionId'));
    if (res.statusCode != 200) throw Exception('查询会话失败: ${res.body}');
    return jsonDecode(res.body);
  }

  Future<void> cancelSession(String sessionId) async {
    final res = await http.post(Uri.parse('$baseUrl/api/sessions/$sessionId/cancel'));
    if (res.statusCode != 200) throw Exception('取消扫描失败: ${res.body}');
  }

  Future<Map<String, dynamic>> promoteSession(String sessionId, int libraryId) async {
    final res = await http.post(
      Uri.parse('$baseUrl/api/sessions/$sessionId/promote'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'library_id': libraryId}),
    );
    if (res.statusCode != 200) throw Exception('入库失败: ${res.body}');
    return jsonDecode(res.body);
  }

  // ===== 任务管理 =====

  Future<List<BackgroundTask>> getTasks() async {
    final res = await http.get(Uri.parse('$baseUrl/api/tasks'));
    if (res.statusCode != 200) throw Exception('获取任务列表失败: ${res.body}');
    final list = jsonDecode(res.body) as List<dynamic>;
    return list.map((e) => BackgroundTask.fromJson(e as Map<String, dynamic>)).toList();
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
    return results.map((e) => SearchResult.fromJson(e as Map<String, dynamic>)).toList();
  }

  /// 以图搜图（通过文件上传，服务端计算 pHash）
  Future<List<SearchResult>> searchImage({required File file, int maxDistance = 12}) async {
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
    return results.map((e) => SearchResult.fromJson(e as Map<String, dynamic>)).toList();
  }

  /// 以图搜图（通过 phash）
  Future<List<SearchResult>> searchImageByPhash(String phash, {int maxDistance = 12}) async {
    final res = await http.post(
      Uri.parse('$baseUrl/api/search/image'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'phash': phash, 'max_distance': maxDistance}),
    );
    if (res.statusCode != 200) throw Exception('以图搜图失败: ${res.body}');
    final data = jsonDecode(res.body);
    final results = data['results'] as List<dynamic>;
    return results.map((e) => SearchResult.fromJson(e as Map<String, dynamic>)).toList();
  }

  // ===== 重复报告 =====

  /// 提交重复检测报告任务（图片 + 视频）
  Future<Map<String, dynamic>> submitReport() async {
    final results = <String, dynamic>{};

    // 提交图片报告任务
    final imgRes = await http.post(
      Uri.parse('$baseUrl/api/reports/image'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'output_path': 'report_image.html'}),
    );
    if (imgRes.statusCode == 200 || imgRes.statusCode == 202) {
      final data = jsonDecode(imgRes.body);
      results['image_task_id'] = data['task_id'];
    }

    // 提交视频报告任务
    final vidRes = await http.post(
      Uri.parse('$baseUrl/api/reports/video'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'output_path': 'report_video.html'}),
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

  Future<Map<String, dynamic>> generateImageReport({String? outputPath}) async {
    final res = await http.post(
      Uri.parse('$baseUrl/api/reports/image'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'output_path': outputPath ?? 'report_image.html'}),
    );
    if (res.statusCode != 200) throw Exception('生成图片报告失败: ${res.body}');
    return jsonDecode(res.body);
  }

  Future<Map<String, dynamic>> generateVideoReport({String? outputPath}) async {
    final res = await http.post(
      Uri.parse('$baseUrl/api/reports/video'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'output_path': outputPath ?? 'report_video.html'}),
    );
    if (res.statusCode != 200) throw Exception('生成视频报告失败: ${res.body}');
    return jsonDecode(res.body);
  }

  // ===== 文件树 =====

  Future<List<FileTreeNode>> getFileTree(int libraryId) async {
    final res = await http.get(Uri.parse('$baseUrl/api/libraries/$libraryId/tree'));
    if (res.statusCode != 200) throw Exception('获取文件树失败: ${res.body}');
    final list = jsonDecode(res.body) as List<dynamic>;
    return list.map((e) => FileTreeNode.fromJson(e as Map<String, dynamic>)).toList();
  }

  /// 列出库下指定目录的所有媒体
  Future<List<Media>> getFiles(int libraryId, {String path = ''}) async {
    final uri = Uri.parse('$baseUrl/api/libraries/$libraryId/files').replace(
      queryParameters: path.isNotEmpty ? {'path': path} : null,
    );
    final res = await http.get(uri);
    if (res.statusCode != 200) throw Exception('获取文件列表失败: ${res.body}');
    final list = jsonDecode(res.body) as List<dynamic>;
    return list.map((e) => Media.fromJson(e as Map<String, dynamic>)).toList();
  }

  /// 缩略图 URL
  String thumbnailUrl(String thumbnailPath) {
    return '$baseUrl/api/thumbnails/$thumbnailPath';
  }
}
