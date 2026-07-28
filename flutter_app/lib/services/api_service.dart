// api_service.dart：HTTP API 客户端
// 代码注释使用中文
import 'dart:convert';
import 'package:http/http.dart' as http;
import '../models/models.dart';

/// API 服务客户端
class ApiService {
  final String baseUrl;

  ApiService({this.baseUrl = 'http://localhost:8080'});

  // ===== 收藏库管理 =====

  Future<List<Library>> getLibraries() async {
    final res = await http.get(Uri.parse('$baseUrl/api/libraries'));
    if (res.statusCode != 200) throw Exception('获取库列表失败: ${res.body}');
    final list = jsonDecode(res.body) as List<dynamic>;
    return list.map((e) => Library.fromJson(e as Map<String, dynamic>)).toList();
  }

  Future<Library> createLibrary(String name, String path, String kind) async {
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

  Future<Map<String, dynamic>> scanLibrary(int libraryId) async {
    final res = await http.post(Uri.parse('$baseUrl/api/libraries/$libraryId/scan'));
    if (res.statusCode != 200) throw Exception('启动扫描失败: ${res.body}');
    return jsonDecode(res.body);
  }

  Future<Map<String, dynamic>> scanTemporary(String path) async {
    final res = await http.post(
      Uri.parse('$baseUrl/api/scan/temporary'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'path': path}),
    );
    if (res.statusCode != 200) throw Exception('启动临时扫描失败: ${res.body}');
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

  // ===== 搜索 =====

  Future<List<SearchResult>> searchText(String query) async {
    final res = await http.get(
      Uri.parse('$baseUrl/api/search?q=${Uri.encodeQueryComponent(query)}'),
    );
    if (res.statusCode != 200) throw Exception('搜索失败: ${res.body}');
    final data = jsonDecode(res.body);
    final results = data['results'] as List<dynamic>;
    return results.map((e) => SearchResult.fromJson(e as Map<String, dynamic>)).toList();
  }

  Future<List<SearchResult>> searchImage(String phash, {int maxDistance = 12}) async {
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

  /// 缩略图 URL
  String thumbnailUrl(String thumbnailPath) {
    return '$baseUrl/api/thumbnails/$thumbnailPath';
  }
}
