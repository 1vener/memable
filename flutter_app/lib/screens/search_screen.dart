// search_screen.dart：搜索页面（文字搜索 + 以图搜图 + 网格/列表视图）
// 代码注释使用中文
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:file_picker/file_picker.dart';
import '../models/models.dart';
import '../services/api_service.dart';
import '../widgets/context_menu.dart';

class SearchScreen extends StatefulWidget {
  final ApiService api;
  const SearchScreen({super.key, required this.api});

  @override
  State<SearchScreen> createState() => _SearchScreenState();
}

class _SearchScreenState extends State<SearchScreen> {
  final _searchCtrl = TextEditingController();
  bool _searching = false;
  List<SearchResult> _results = [];
  String? _error;
  bool _isGridView = true;
  // 是否已发起过搜索（用于区分"还没搜索"与"搜索无结果"两种空状态）
  bool _hasSearched = false;

  // 以图搜图
  String? _imagePath;
  Uint8List? _imageBytes;
  bool _isImageSearch = false;

  // 以视频搜视频：两路距离阈值（用户可调，0~64 整数）
  String? _videoPath;
  bool _isVideoSearch = false;
  double _imageMaxDistance = 12;
  double _videoMaxDistance = 16;

  @override
  void dispose() {
    _searchCtrl.dispose();
    super.dispose();
  }

  Future<void> _textSearch() async {
    final query = _searchCtrl.text.trim();
    if (query.isEmpty) return;

    setState(() {
      _searching = true;
      _error = null;
      _results = [];
      _isImageSearch = false;
      _isVideoSearch = false;
      _hasSearched = true;
    });

    try {
      final data = await widget.api.searchText(query: query);
      if (mounted) {
        setState(() {
          _results = data;
          _searching = false;
        });
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _error = '$e';
          _searching = false;
        });
      }
    }
  }

  Future<void> _pickImageAndSearch() async {
    final result = await FilePicker.platform.pickFiles(
      type: FileType.image,
      dialogTitle: '选择图片进行以图搜图',
    );
    if (result == null || result.files.isEmpty) return;

    final pf = result.files.first;
    // web 端无本地路径，显示名回退文件名
    final display = pf.path ?? pf.name;

    setState(() {
      _imagePath = display;
      _imageBytes = pf.bytes;
      _isImageSearch = true;
      _isVideoSearch = false;
      _videoPath = null;
      _searching = true;
      _error = null;
      _results = [];
      _hasSearched = true;
    });

    try {
      final data = await widget.api.searchImage(file: pf);
      if (mounted) {
        setState(() {
          _results = data;
          _searching = false;
        });
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _error = '$e';
          _searching = false;
        });
      }
    }
  }

  Future<void> _pickVideoAndSearch() async {
    final result = await FilePicker.platform.pickFiles(
      type: FileType.video,
      dialogTitle: '选择视频进行以视频搜视频',
    );
    if (result == null || result.files.isEmpty) return;

    final pf = result.files.first;
    if (pf.path == null && pf.bytes == null) return;
    final display = pf.path ?? pf.name;

    setState(() {
      _videoPath = display;
      _isVideoSearch = true;
      _isImageSearch = false;
      _imagePath = null;
      _searching = true;
      _error = null;
      _results = [];
      _hasSearched = true;
    });

    try {
      final data = await widget.api.searchVideo(
        pf,
        imageMaxDistance: _imageMaxDistance.round(),
        videoMaxDistance: _videoMaxDistance.round(),
      );
      if (mounted) {
        setState(() {
          _results = data;
          _searching = false;
        });
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _error = '$e';
          _searching = false;
        });
      }
    }
  }

  /// 距离阈值滑块：0~64 整数步进（divisions=64，只能选整数），实时显示相似度百分比。
  Widget _buildDistanceSlider({
    required String label,
    required double value,
    required ValueChanged<double> onChanged,
  }) {
    final cs = Theme.of(context).colorScheme;
    final dist = value.round();
    final percent = (1.0 - dist / 64.0) * 100;
    return Row(
      children: [
        SizedBox(
          width: 110,
          child: Text(
            label,
            style: TextStyle(fontSize: 12, color: cs.onSurfaceVariant),
          ),
        ),
        Expanded(
          child: Slider(
            value: value,
            min: 0,
            max: 64,
            divisions: 64,
            label: '$dist（${percent.toStringAsFixed(0)}%）',
            onChanged: onChanged,
          ),
        ),
        SizedBox(
          width: 110,
          child: Text(
            '距离 $dist · ${percent.toStringAsFixed(0)}%',
            style: TextStyle(fontSize: 12, color: cs.primary),
            textAlign: TextAlign.right,
          ),
        ),
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;

    return Padding(
      padding: const EdgeInsets.all(32),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // ========== 搜索栏 ==========
          Card(
            child: Padding(
              padding: const EdgeInsets.all(20),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text('搜索', style: TextStyle(fontSize: 15, fontWeight: FontWeight.w600, color: cs.onSurface)),
                  const SizedBox(height: 16),
                  // 文字搜索输入框
                  Row(
                    children: [
                      Expanded(
                        child: TextField(
                          controller: _searchCtrl,
                          style: const TextStyle(fontSize: 14),
                          decoration: InputDecoration(
                            hintText: '输入文件名或路径关键词搜索...',
                            prefixIcon: const Icon(Icons.search, size: 20),
                            suffixIcon: _searchCtrl.text.isNotEmpty
                                ? IconButton(
                                    icon: const Icon(Icons.clear, size: 18),
                                    onPressed: () {
                                      _searchCtrl.clear();
                                      setState(() {});
                                    },
                                  )
                                : null,
                          ),
                          onChanged: (_) => setState(() {}),
                          onSubmitted: (_) => _textSearch(),
                        ),
                      ),
                      const SizedBox(width: 12),
                      FilledButton.icon(
                        onPressed: _searching ? null : _textSearch,
                        icon: _searching && !_isImageSearch && !_isVideoSearch
                            ? const SizedBox(
                                width: 16, height: 16,
                                child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white),
                              )
                            : const Icon(Icons.search, size: 18),
                        label: const Text('搜索'),
                      ),
                    ],
                  ),
                  const SizedBox(height: 12),
                  // 以图搜图按钮
                  Row(
                    children: [
                      OutlinedButton.icon(
                        onPressed: _searching ? null : _pickImageAndSearch,
                        icon: const Icon(Icons.image_search, size: 18),
                        label: const Text('以图搜图'),
                      ),
                      if (_imagePath != null) ...[
                        const SizedBox(width: 12),
                        // 图片预览缩略图（web 兼容：用内存字节，桌面同样可用）
                        ClipRRect(
                          borderRadius: BorderRadius.circular(6),
                          child: _imageBytes != null
                              ? Image.memory(
                                  _imageBytes!,
                                  width: 32,
                                  height: 32,
                                  fit: BoxFit.cover,
                                  errorBuilder: (_, __, ___) => Container(
                                    width: 32, height: 32,
                                    color: cs.surfaceContainerHighest,
                                    child: const Icon(Icons.image, size: 16),
                                  ),
                                )
                              : Container(
                                  width: 32, height: 32,
                                  color: cs.surfaceContainerHighest,
                                  child: const Icon(Icons.image, size: 16),
                                ),
                        ),
                        const SizedBox(width: 8),
                        Expanded(
                          child: Text(
                            _imagePath!.split('/').last.split('\\').last,
                            style: TextStyle(fontSize: 12, color: cs.outline),
                            overflow: TextOverflow.ellipsis,
                          ),
                        ),
                        IconButton(
                          icon: const Icon(Icons.close, size: 16),
                          onPressed: () =>
                              setState(() => _imagePath = null),
                        ),
                      ],
                    ],
                  ),
                  const SizedBox(height: 12),
                  // 以视频搜视频按钮
                  Row(
                    children: [
                      OutlinedButton.icon(
                        onPressed: _searching ? null : _pickVideoAndSearch,
                        icon: const Icon(Icons.videocam, size: 18),
                        label: const Text('以视频搜视频'),
                      ),
                      if (_videoPath != null) ...[
                        const SizedBox(width: 12),
                        Icon(Icons.movie_outlined, size: 28, color: cs.primary),
                        const SizedBox(width: 8),
                        Expanded(
                          child: Text(
                            _videoPath!.split('/').last.split('\\').last,
                            style: TextStyle(fontSize: 12, color: cs.outline),
                            overflow: TextOverflow.ellipsis,
                          ),
                        ),
                        IconButton(
                          icon: const Icon(Icons.close, size: 16),
                          onPressed: () => setState(() => _videoPath = null),
                        ),
                      ],
                    ],
                  ),
                  // 距离阈值滑块（仅以视频搜视频时显示，只能选整数）
                  if (_isVideoSearch) ...[
                    const SizedBox(height: 8),
                    _buildDistanceSlider(
                      label: '图片匹配距离',
                      value: _imageMaxDistance,
                      onChanged: (v) => setState(() => _imageMaxDistance = v.roundToDouble()),
                    ),
                    _buildDistanceSlider(
                      label: '视频匹配距离',
                      value: _videoMaxDistance,
                      onChanged: (v) => setState(() => _videoMaxDistance = v.roundToDouble()),
                    ),
                  ],
                ],
              ),
            ),
          ),
          const SizedBox(height: 16),

          // ========== 工具栏 ==========
          if (_results.isNotEmpty || _error != null)
            Row(
              children: [
                if (_results.isNotEmpty)
                  Text(
                    '${_results.length} 条结果',
                    style: TextStyle(fontSize: 13, color: cs.outline),
                  ),
                const Spacer(),
                // 视图切换
                ToggleButtons(
                  isSelected: [_isGridView, !_isGridView],
                  onPressed: (i) => setState(() => _isGridView = i == 0),
                  borderRadius: BorderRadius.circular(8),
                  constraints: const BoxConstraints(minWidth: 36, minHeight: 32),
                  children: const [
                    Icon(Icons.grid_view, size: 18),
                    Icon(Icons.list, size: 18),
                  ],
                ),
              ],
            ),
          if (_results.isNotEmpty) const SizedBox(height: 12),

          // ========== 结果区域 ==========
          Expanded(
            child: _error != null
                ? Center(
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Icon(Icons.error_outline, size: 48, color: cs.error),
                        const SizedBox(height: 12),
                        Text('搜索失败', style: TextStyle(fontSize: 15, color: cs.onSurface)),
                        const SizedBox(height: 6),
                        Padding(
                          padding: const EdgeInsets.symmetric(horizontal: 32),
                          child: Text(_error!, style: TextStyle(fontSize: 13, color: cs.outline), overflow: TextOverflow.ellipsis, maxLines: 3),
                        ),
                        const SizedBox(height: 16),
                        FilledButton.tonal(onPressed: _textSearch, child: const Text('重试')),
                      ],
                    ),
                  )
                : _results.isEmpty
                    ? Center(
                        child: Column(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            Icon(
                              _isImageSearch
                                  ? Icons.image_search
                                  : _isVideoSearch
                                      ? Icons.videocam
                                      : Icons.search_off,
                              size: 64,
                              color: cs.outline.withValues(alpha: 0.4),
                            ),
                            const SizedBox(height: 16),
                            Text(
                              _hasSearched
                                  ? '查询结果为空'
                                  : (_isImageSearch
                                        ? '选择图片开始以图搜图'
                                        : _isVideoSearch
                                            ? '选择视频开始以视频搜视频'
                                            : '输入关键词开始搜索'),
                              style: TextStyle(fontSize: 15, color: cs.outline),
                            ),
                            if (_hasSearched) ...[
                              const SizedBox(height: 6),
                              Text(
                                _isImageSearch
                                    ? '没有找到相似的图片'
                                    : _isVideoSearch
                                        ? '没有找到相似的视频或图片'
                                        : '没有找到匹配的媒体，换个关键词试试',
                                style: TextStyle(fontSize: 13, color: cs.outline.withValues(alpha: 0.7)),
                              ),
                            ],
                          ],
                        ),
                      )
                    : _isGridView
                        ? _buildGridView(cs)
                        : _buildListView(cs),
          ),
        ],
      ),
    );
  }

  /// 网格视图
  Widget _buildGridView(ColorScheme cs) {
    return GridView.builder(
      gridDelegate: const SliverGridDelegateWithMaxCrossAxisExtent(
        maxCrossAxisExtent: 200,
        mainAxisSpacing: 10,
        crossAxisSpacing: 10,
        childAspectRatio: 0.8,
      ),
      itemCount: _results.length,
      itemBuilder: (context, index) {
        final result = _results[index];
        return _ResultGridCard(
          result: result,
          api: widget.api,
        );
      },
    );
  }

  /// 列表视图
  Widget _buildListView(ColorScheme cs) {
    return ListView.builder(
      itemCount: _results.length,
      itemBuilder: (context, index) {
        final result = _results[index];
        return _ResultListTile(
          result: result,
          api: widget.api,
          index: index + 1,
        );
      },
    );
  }
}

/// 网格结果卡片
class _ResultGridCard extends StatelessWidget {
  final SearchResult result;
  final ApiService api;

  const _ResultGridCard({required this.result, required this.api});

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;

    return Tooltip(
      message: '${result.fullPath}\n相似度: ${(result.score * 100).toStringAsFixed(1)}%',
      child: Material(
        color: Colors.transparent,
        borderRadius: BorderRadius.circular(12),
        child: InkWell(
          borderRadius: BorderRadius.circular(12),
          onSecondaryTapDown: (details) {
            showContextMenu(
              context: context,
              position: details.globalPosition,
              items: [
                const ContextMenuItem(icon: Icons.folder_open, label: '在库中查看'),
                const ContextMenuItem(icon: Icons.copy, label: '复制路径', shortcut: 'Ctrl+C'),
                const ContextMenuItem.divider(),
                const ContextMenuItem(icon: Icons.open_in_new, label: '打开文件'),
              ],
            );
          },
          child: Container(
            decoration: BoxDecoration(
              color: cs.surface,
              borderRadius: BorderRadius.circular(12),
              border: Border.all(color: cs.outlineVariant),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                // 缩略图区域
                Expanded(
                  child: ClipRRect(
                    borderRadius: const BorderRadius.vertical(top: Radius.circular(12)),
                    child: Stack(
                      fit: StackFit.expand,
                      children: [
                        result.thumbnailUrl != null
                            ? Image.network(
                                api.thumbnailUrl(
                                  result.media.kind,
                                  result.thumbnailUrl!,
                                ),
                                fit: BoxFit.cover,
                                errorBuilder: (_, __, ___) => _PlaceholderIcon(cs: cs),
                              )
                            : _PlaceholderIcon(cs: cs),
                        // 相似度徽章
                        Positioned(
                          top: 8,
                          right: 8,
                          child: Container(
                            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                            decoration: BoxDecoration(
                              color: Colors.black.withValues(alpha: 0.6),
                              borderRadius: BorderRadius.circular(12),
                            ),
                            child: Text(
                              '${(result.score * 100).toStringAsFixed(1)}%',
                              style: const TextStyle(fontSize: 11, color: Colors.white, fontWeight: FontWeight.w600),
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
                // 文件名
                Padding(
                  padding: const EdgeInsets.all(10),
                  child: Text(
                    result.name,
                    style: TextStyle(fontSize: 12, fontWeight: FontWeight.w500, color: cs.onSurface),
                    overflow: TextOverflow.ellipsis,
                    maxLines: 2,
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// 列表结果条目
class _ResultListTile extends StatelessWidget {
  final SearchResult result;
  final ApiService api;
  final int index;

  const _ResultListTile({
    required this.result,
    required this.api,
    required this.index,
  });

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;

    return Card(
      margin: const EdgeInsets.only(bottom: 6),
      child: GestureDetector(
        onSecondaryTapDown: (details) {
          showContextMenu(
            context: context,
            position: details.globalPosition,
            items: [
              const ContextMenuItem(icon: Icons.folder_open, label: '在库中查看'),
              const ContextMenuItem(icon: Icons.copy, label: '复制路径', shortcut: 'Ctrl+C'),
              const ContextMenuItem.divider(),
              const ContextMenuItem(icon: Icons.open_in_new, label: '打开文件'),
            ],
          );
        },
        child: ListTile(
        leading: result.thumbnailUrl != null
            ? ClipRRect(
                borderRadius: BorderRadius.circular(6),
                child: Image.network(
                  api.thumbnailUrl(result.media.kind, result.thumbnailUrl!),
                  width: 48,
                  height: 48,
                  fit: BoxFit.cover,
                  errorBuilder: (_, __, ___) => Container(
                    width: 48, height: 48,
                    color: cs.surfaceContainerHighest,
                    child: Icon(Icons.image, size: 20, color: cs.outline),
                  ),
                ),
              )
            : Container(
                width: 48, height: 48,
                decoration: BoxDecoration(
                  color: cs.surfaceContainerHighest,
                  borderRadius: BorderRadius.circular(6),
                ),
                child: Icon(Icons.image, size: 20, color: cs.outline),
              ),
        title: Text(result.name, style: const TextStyle(fontSize: 13), overflow: TextOverflow.ellipsis),
        subtitle: Text(
          result.fullPath,
          style: TextStyle(fontSize: 12, color: cs.outline),
          overflow: TextOverflow.ellipsis,
        ),
        trailing: Container(
          padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
          decoration: BoxDecoration(
            color: cs.primary.withValues(alpha: 0.1),
            borderRadius: BorderRadius.circular(12),
          ),
          child: Text(
            '${(result.score * 100).toStringAsFixed(1)}%',
            style: TextStyle(fontSize: 12, fontWeight: FontWeight.w600, color: cs.primary),
          ),
        ),
        ),
      ),
    );
  }
}

/// 缩略图占位图标
class _PlaceholderIcon extends StatelessWidget {
  final ColorScheme cs;
  const _PlaceholderIcon({required this.cs});

  @override
  Widget build(BuildContext context) {
    return Container(
      color: cs.surfaceContainerHighest,
      child: Center(
        child: Icon(Icons.image_outlined, size: 36, color: cs.outline.withValues(alpha: 0.5)),
      ),
    );
  }
}
