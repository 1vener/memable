// search_screen.dart：桌面端搜索
// 代码注释使用中文
import 'package:flutter/material.dart';
import '../models/models.dart';
import '../services/api_service.dart';

class SearchScreen extends StatefulWidget {
  final ApiService api;
  const SearchScreen({super.key, required this.api});

  @override
  State<SearchScreen> createState() => _SearchScreenState();
}

class _SearchScreenState extends State<SearchScreen> {
  final _controller = TextEditingController();
  List<SearchResult> _results = [];
  bool _loading = false;
  bool _gridView = false; // false = 列表视图, true = 网格视图

  Future<void> _search() async {
    final q = _controller.text.trim();
    if (q.isEmpty) return;
    setState(() => _loading = true);
    try {
      final results = await widget.api.searchText(q);
      setState(() => _results = results);
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('搜索失败: $e'), backgroundColor: const Color(0xFFEF4444)),
        );
      }
    } finally {
      setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Column(
        children: [
          // 搜索栏
          Container(
            color: Colors.white,
            padding: const EdgeInsets.fromLTRB(24, 16, 24, 12),
            child: Row(
              children: [
                Expanded(
                  child: TextField(
                    controller: _controller,
                    decoration: InputDecoration(
                      hintText: '输入文件名或 SHA1 搜索...',
                      prefixIcon: const Icon(Icons.search, size: 20),
                      border: OutlineInputBorder(
                        borderRadius: BorderRadius.circular(10),
                        borderSide: BorderSide.none,
                      ),
                      filled: true,
                      fillColor: const Color(0xFFF1F5F9),
                      contentPadding: const EdgeInsets.symmetric(vertical: 10),
                    ),
                    onSubmitted: (_) => _search(),
                  ),
                ),
                const SizedBox(width: 12),
                FilledButton.icon(
                  onPressed: _search,
                  icon: const Icon(Icons.search, size: 18),
                  label: const Text('搜索'),
                ),
                const SizedBox(width: 8),
                // 视图切换
                SegmentedButton<bool>(
                  segments: const [
                    ButtonSegment(value: false, icon: Icon(Icons.view_list, size: 18)),
                    ButtonSegment(value: true, icon: Icon(Icons.grid_view, size: 18)),
                  ],
                  selected: {_gridView},
                  onSelectionChanged: (v) => setState(() => _gridView = v.first),
                  style: ButtonStyle(
                    visualDensity: VisualDensity.compact,
                  ),
                ),
              ],
            ),
          ),
          const Divider(height: 1, color: Color(0xFFE2E8F0)),

          // 结果统计
          if (_results.isNotEmpty)
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 8),
              color: Colors.white,
              child: Row(
                children: [
                  Text(
                    '${_results.length} 条结果',
                    style: const TextStyle(fontSize: 13, color: Color(0xFF64748B)),
                  ),
                  const Spacer(),
                  if (_loading) const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2)),
                ],
              ),
            ),

          // 结果区
          Expanded(
            child: _loading && _results.isEmpty
                ? const Center(child: CircularProgressIndicator())
                : _results.isEmpty
                    ? _buildEmptyState()
                    : _gridView
                        ? _buildGrid()
                        : _buildList(),
          ),
        ],
      ),
    );
  }

  Widget _buildEmptyState() {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(Icons.search_off, size: 64, color: Colors.grey[300]),
          const SizedBox(height: 16),
          const Text('输入关键词开始搜索', style: TextStyle(fontSize: 16, color: Color(0xFF94A3B8))),
          const SizedBox(height: 4),
          const Text('支持文件名模糊匹配和 SHA1 精确匹配', style: TextStyle(fontSize: 13, color: Color(0xFF94A3B8))),
        ],
      ),
    );
  }

  Widget _buildList() {
    return ListView.separated(
      padding: const EdgeInsets.all(24),
      itemCount: _results.length,
      separatorBuilder: (_, __) => const SizedBox(height: 8),
      itemBuilder: (ctx, i) {
        final r = _results[i];
        return Card(
          child: ListTile(
            contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
            leading: _thumb(r, 48),
            title: Text(
              r.fullPath,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w500),
            ),
            subtitle: Row(
              children: [
                _kindBadge(r.media.kind),
                const SizedBox(width: 8),
                Text(_formatSize(r.media.fileSize), style: const TextStyle(fontSize: 12, color: Color(0xFF64748B))),
                if (r.media.width != null && r.media.height != null) ...[
                  const SizedBox(width: 8),
                  Text('${r.media.width}×${r.media.height}', style: const TextStyle(fontSize: 12, color: Color(0xFF64748B))),
                ],
                if (r.distance > 0) ...[
                  const SizedBox(width: 8),
                  Text('距离 ${r.distance}', style: const TextStyle(fontSize: 12, color: Color(0xFF2563EB))),
                ],
              ],
            ),
            trailing: const Icon(Icons.chevron_right, color: Color(0xFF94A3B8)),
          ),
        );
      },
    );
  }

  Widget _buildGrid() {
    return GridView.builder(
      padding: const EdgeInsets.all(24),
      gridDelegate: const SliverGridDelegateWithMaxCrossAxisExtent(
        maxCrossAxisExtent: 200,
        childAspectRatio: 0.85,
        crossAxisSpacing: 12,
        mainAxisSpacing: 12,
      ),
      itemCount: _results.length,
      itemBuilder: (ctx, i) {
        final r = _results[i];
        return Card(
          clipBehavior: Clip.antiAlias,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(child: _thumb(r, double.infinity)),
              Padding(
                padding: const EdgeInsets.all(8),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      r.media.relativePath.split('/').last,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(fontSize: 12, fontWeight: FontWeight.w500),
                    ),
                    const SizedBox(height: 2),
                    Row(
                      children: [
                        _kindBadge(r.media.kind),
                        const Spacer(),
                        Text(_formatSize(r.media.fileSize), style: const TextStyle(fontSize: 11, color: Color(0xFF94A3B8))),
                      ],
                    ),
                  ],
                ),
              ),
            ],
          ),
        );
      },
    );
  }

  Widget _thumb(SearchResult r, double size) {
    if (r.media.thumbnailPath != null && r.media.thumbnailPath!.isNotEmpty) {
      return Image.network(
        widget.api.thumbnailUrl(r.media.thumbnailPath!),
        width: size,
        height: size,
        fit: BoxFit.cover,
        errorBuilder: (_, __, ___) => _thumbPlaceholder(r.media.kind, size),
      );
    }
    return _thumbPlaceholder(r.media.kind, size);
  }

  Widget _thumbPlaceholder(String kind, double size) {
    return Container(
      width: size,
      height: size,
      color: const Color(0xFFF1F5F9),
      child: Icon(
        kind == 'image' ? Icons.image_outlined : Icons.video_library_outlined,
        size: size * 0.4,
        color: const Color(0xFF94A3B8),
      ),
    );
  }

  Widget _kindBadge(String kind) {
    final (label, color) = switch (kind) {
      'image' => ('图片', const Color(0xFF3B82F6)),
      'video' => ('视频', const Color(0xFF8B5CF6)),
      _ => ('其他', const Color(0xFF6B7280)),
    };
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.1),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Text(label, style: TextStyle(fontSize: 11, color: color, fontWeight: FontWeight.w500)),
    );
  }

  String _formatSize(int bytes) {
    if (bytes < 1024) return '$bytes B';
    if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)} KB';
    if (bytes < 1024 * 1024 * 1024) return '${(bytes / 1024 / 1024).toStringAsFixed(1)} MB';
    return '${(bytes / 1024 / 1024 / 1024).toStringAsFixed(2)} GB';
  }
}
