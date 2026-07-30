// report_screen.dart：应用内重复检测结果，支持目录树与分组视图。
// 代码注释使用中文
import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';

import '../models/models.dart';
import '../services/api_service.dart';
import '../widgets/context_menu.dart';

enum _ReportView { directory, group }

class ReportScreen extends StatefulWidget {
  final ApiService api;
  const ReportScreen({super.key, required this.api});

  @override
  State<ReportScreen> createState() => _ReportScreenState();
}

class _ReportScreenState extends State<ReportScreen> {
  final Map<String, DuplicateReport> _reports = {};
  final Set<String> _pendingTaskIds = {};
  final Set<String> _expandedPaths = {};
  Timer? _pollTimer;
  bool _loading = true;
  bool _submitting = false;
  String? _error;
  String _kind = 'all';
  String? _selectedDirectory;
  _ReportView _view = _ReportView.directory;

  @override
  void initState() {
    super.initState();
    _loadLatestReports();
  }

  @override
  void dispose() {
    _pollTimer?.cancel();
    super.dispose();
  }

  Future<void> _loadLatestReports() async {
    try {
      final tasks = await widget.api.getTasks();
      final seenKinds = <String>{};
      for (final task in tasks) {
        if (task.kind != 'report_image' && task.kind != 'report_video') {
          continue;
        }
        final kind = task.kind == 'report_image' ? 'image' : 'video';
        if (seenKinds.contains(kind)) {
          continue;
        }
        if (task.isCompleted && task.resultJson != null) {
          if (_readTaskResult(task)) {
            seenKinds.add(kind);
          }
        } else if (task.isActive) {
          _pendingTaskIds.add(task.id);
          seenKinds.add(kind);
        }
      }
      if (_pendingTaskIds.isNotEmpty) {
        _startPolling();
      }
    } catch (e) {
      _error = '读取重复统计结果失败: $e';
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  bool _readTaskResult(BackgroundTask task) {
    if (task.resultJson == null || task.resultJson == '{}') {
      return false;
    }
    try {
      final data = jsonDecode(task.resultJson!) as Map<String, dynamic>;
      // 旧版 HTML 报告结果的 groups 是数量，不包含应用内展示数据。
      if (data['kind'] is! String || data['groups'] is! List) {
        return false;
      }
      final report = DuplicateReport.fromJson(data);
      if (report.kind.isEmpty) {
        return false;
      }
      _reports[report.kind] = report;
      return true;
    } catch (_) {
      return false;
    }
  }

  Future<void> _submitReport() async {
    setState(() {
      _submitting = true;
      _error = null;
    });
    try {
      final result = await widget.api.submitReport();
      for (final key in ['image_task_id', 'video_task_id']) {
        final id = result[key] as String?;
        if (id != null) _pendingTaskIds.add(id);
      }
      _startPolling();
    } catch (e) {
      if (mounted) setState(() => _error = '提交重复统计失败: $e');
    } finally {
      if (mounted) setState(() => _submitting = false);
    }
  }

  void _startPolling() {
    _pollTimer ??= Timer.periodic(
      const Duration(seconds: 1),
      (_) => _pollTasks(),
    );
    _pollTasks();
  }

  Future<void> _pollTasks() async {
    if (_pendingTaskIds.isEmpty) {
      _pollTimer?.cancel();
      _pollTimer = null;
      return;
    }
    for (final id in List<String>.from(_pendingTaskIds)) {
      try {
        final task = await widget.api.getTask(id);
        if (task.isCompleted) {
          if (!_readTaskResult(task)) {
            _error = '任务已完成，但没有可展示的结构化重复统计结果';
          }
          _pendingTaskIds.remove(id);
        } else if (task.isFailed || task.isCancelled) {
          _pendingTaskIds.remove(id);
          _error = task.errorMessage ?? '重复统计任务未完成';
        }
      } catch (e) {
        _error = '查询重复统计任务失败: $e';
      }
    }
    if (mounted) setState(() {});
  }

  List<DuplicateGroup> get _groups {
    if (_kind == 'all') {
      return _reports.values.expand((report) => report.groups).toList();
    }
    return _reports[_kind]?.groups ?? [];
  }

  List<DuplicateItem> get _items =>
      _groups.expand((group) => group.items).toList();

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    if (_loading) return const Center(child: CircularProgressIndicator());

    return Column(
      children: [
        _buildToolbar(cs),
        if (_error != null)
          MaterialBanner(
            content: Text(_error!),
            actions: [
              TextButton(
                onPressed: () => setState(() => _error = null),
                child: const Text('关闭'),
              ),
            ],
          ),
        Expanded(
          child:
              _groups.isEmpty && _pendingTaskIds.isEmpty
                  ? _buildEmpty(cs)
                  : _view == _ReportView.directory
                  ? _buildDirectoryView(cs)
                  : _buildGroupView(cs),
        ),
      ],
    );
  }

  Widget _buildToolbar(ColorScheme cs) {
    final groupCount = _groups.length;
    final fileCount = _items.length;
    return Container(
      height: 64,
      padding: const EdgeInsets.symmetric(horizontal: 24),
      decoration: BoxDecoration(
        border: Border(bottom: BorderSide(color: cs.outlineVariant)),
      ),
      child: Row(
        children: [
          Icon(Icons.content_copy_rounded, color: cs.primary),
          const SizedBox(width: 10),
          Text(
            '重复统计',
            style: TextStyle(
              fontSize: 17,
              fontWeight: FontWeight.w700,
              color: cs.onSurface,
            ),
          ),
          const SizedBox(width: 14),
          Text(
            '$groupCount 组 · $fileCount 个文件',
            style: TextStyle(fontSize: 12, color: cs.outline),
          ),
          const SizedBox(width: 24),
          SegmentedButton<String>(
            segments: const [
              ButtonSegment(value: 'all', label: Text('全部')),
              ButtonSegment(
                value: 'image',
                label: Text('图片'),
                icon: Icon(Icons.image_outlined, size: 16),
              ),
              ButtonSegment(
                value: 'video',
                label: Text('视频'),
                icon: Icon(Icons.videocam_outlined, size: 16),
              ),
            ],
            selected: {_kind},
            onSelectionChanged:
                (value) => setState(() {
                  _kind = value.first;
                  _selectedDirectory = null;
                }),
          ),
          const Spacer(),
          SegmentedButton<_ReportView>(
            segments: const [
              ButtonSegment(
                value: _ReportView.directory,
                label: Text('目录树'),
                icon: Icon(Icons.account_tree_outlined, size: 16),
              ),
              ButtonSegment(
                value: _ReportView.group,
                label: Text('重复分组'),
                icon: Icon(Icons.grid_view_outlined, size: 16),
              ),
            ],
            selected: {_view},
            onSelectionChanged: (value) => setState(() => _view = value.first),
          ),
          const SizedBox(width: 12),
          FilledButton.icon(
            onPressed:
                _submitting || _pendingTaskIds.isNotEmpty
                    ? null
                    : _submitReport,
            icon:
                _pendingTaskIds.isNotEmpty
                    ? const SizedBox(
                      width: 16,
                      height: 16,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                    : const Icon(Icons.refresh, size: 18),
            label: Text(_pendingTaskIds.isNotEmpty ? '统计中' : '重新统计'),
          ),
        ],
      ),
    );
  }

  Widget _buildEmpty(ColorScheme cs) => Center(
    child: Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(
          Icons.find_in_page_outlined,
          size: 64,
          color: cs.outline.withValues(alpha: 0.4),
        ),
        const SizedBox(height: 16),
        Text('暂无重复统计结果', style: TextStyle(fontSize: 16, color: cs.onSurface)),
        const SizedBox(height: 6),
        Text(
          '统计结果将在应用内以目录树和重复分组展示',
          style: TextStyle(fontSize: 13, color: cs.outline),
        ),
        const SizedBox(height: 20),
        FilledButton.icon(
          onPressed: _submitReport,
          icon: const Icon(Icons.play_arrow),
          label: const Text('开始统计'),
        ),
      ],
    ),
  );

  Widget _buildDirectoryView(ColorScheme cs) {
    final roots = _buildDirectoryTree(_items);
    final selected =
        _selectedDirectory ?? (roots.isNotEmpty ? roots.first.path : null);
    final files =
        selected == null
            ? <DuplicateItem>[]
            : _items
                .where((item) => _parentPath(item.fullPath) == selected)
                .toList();
    return Row(
      children: [
        SizedBox(
          width: 320,
          child: ListView(
            padding: const EdgeInsets.all(10),
            children:
                roots
                    .expand((node) => _buildTreeRows(node, 0, selected, cs))
                    .toList(),
          ),
        ),
        VerticalDivider(width: 1, color: cs.outlineVariant),
        Expanded(child: _buildItemGrid(files, cs, emptyText: '此目录没有直属重复文件')),
      ],
    );
  }

  List<Widget> _buildTreeRows(
    _DirectoryNode node,
    int depth,
    String? selected,
    ColorScheme cs,
  ) {
    final expanded = _expandedPaths.contains(node.path) || depth == 0;
    final rows = <Widget>[
      Padding(
        padding: EdgeInsets.only(left: depth * 16.0, bottom: 2),
        child: ListTile(
          dense: true,
          selected: node.path == selected,
          leading: IconButton(
            visualDensity: VisualDensity.compact,
            icon: Icon(
              expanded ? Icons.keyboard_arrow_down : Icons.keyboard_arrow_right,
              size: 17,
            ),
            onPressed:
                node.children.isEmpty
                    ? null
                    : () => setState(() {
                      expanded
                          ? _expandedPaths.remove(node.path)
                          : _expandedPaths.add(node.path);
                    }),
          ),
          title: Text(
            node.name,
            overflow: TextOverflow.ellipsis,
            style: const TextStyle(fontSize: 12),
          ),
          trailing:
              node.directCount > 0
                  ? Text(
                    '${node.directCount}',
                    style: TextStyle(fontSize: 11, color: cs.outline),
                  )
                  : null,
          onTap: () => setState(() => _selectedDirectory = node.path),
        ),
      ),
    ];
    if (expanded) {
      for (final child in node.children) {
        rows.addAll(_buildTreeRows(child, depth + 1, selected, cs));
      }
    }
    return rows;
  }

  Widget _buildGroupView(ColorScheme cs) => ListView.builder(
    padding: const EdgeInsets.all(20),
    itemCount: _groups.length,
    itemBuilder: (context, index) {
      final group = _groups[index];
      return Card(
        margin: const EdgeInsets.only(bottom: 16),
        child: ExpansionTile(
          initiallyExpanded: index < 3,
          title: Text(
            '第 ${index + 1} 组 · ${group.reason}',
            style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w600),
          ),
          subtitle: Text(
            '${group.items.length} 个文件',
            style: TextStyle(fontSize: 12, color: cs.outline),
          ),
          children: [
            SizedBox(height: 260, child: _buildItemGrid(group.items, cs)),
          ],
        ),
      );
    },
  );

  Widget _buildItemGrid(
    List<DuplicateItem> items,
    ColorScheme cs, {
    String emptyText = '没有重复文件',
  }) {
    if (items.isEmpty)
      return Center(
        child: Text(emptyText, style: TextStyle(color: cs.outline)),
      );
    return GridView.builder(
      padding: const EdgeInsets.all(16),
      gridDelegate: const SliverGridDelegateWithMaxCrossAxisExtent(
        maxCrossAxisExtent: 190,
        crossAxisSpacing: 12,
        mainAxisSpacing: 12,
        childAspectRatio: 0.9,
      ),
      itemCount: items.length,
      itemBuilder:
          (context, index) => _DuplicateCard(
            item: items[index],
            api: widget.api,
            onError: (message) => setState(() => _error = message),
          ),
    );
  }
}

class _DuplicateCard extends StatelessWidget {
  final DuplicateItem item;
  final ApiService api;
  final ValueChanged<String> onError;

  const _DuplicateCard({
    required this.item,
    required this.api,
    required this.onError,
  });

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final tooltip = item.duplicatePaths.map((path) => '• $path').join('\n');
    return Tooltip(
      waitDuration: const Duration(milliseconds: 350),
      message: '重复文件完整路径\n$tooltip',
      child: GestureDetector(
        onDoubleTap: () => _open(false),
        onSecondaryTapDown:
            (details) => _showMenu(context, details.globalPosition),
        onLongPressStart:
            (details) => _showMenu(context, details.globalPosition),
        child: Card(
          clipBehavior: Clip.antiAlias,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Expanded(
                child:
                    item.thumbnailPath == null
                        ? _placeholder(cs)
                        : Image.network(
                          api.thumbnailUrl(item.thumbnailPath!),
                          fit: BoxFit.cover,
                          errorBuilder: (_, __, ___) => _placeholder(cs),
                        ),
              ),
              Padding(
                padding: const EdgeInsets.all(8),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      _fileName(item.fullPath),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(fontSize: 11),
                    ),
                    const SizedBox(height: 2),
                    Text(
                      _meta(item),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(fontSize: 10, color: cs.outline),
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _placeholder(ColorScheme cs) => ColoredBox(
    color: cs.surfaceContainerHighest,
    child: Icon(
      item.kind == 'video' ? Icons.videocam_outlined : Icons.image_outlined,
      color: cs.outline,
      size: 40,
    ),
  );

  void _showMenu(BuildContext context, Offset position) => showContextMenu(
    context: context,
    position: position,
    items: [
      ContextMenuItem(
        icon:
            item.kind == 'video'
                ? Icons.play_circle_outline
                : Icons.image_outlined,
        label: item.kind == 'video' ? '打开视频' : '打开图片',
        onTap: () => _open(false),
      ),
      ContextMenuItem(
        icon: Icons.folder_open,
        label: '打开文件所在目录',
        onTap: () => _open(true),
      ),
    ],
  );

  Future<void> _open(bool directory) async {
    try {
      directory
          ? await api.openMediaDirectory(item.id)
          : await api.openMediaFile(item.id);
    } catch (e) {
      onError('打开失败: $e');
    }
  }
}

class _DirectoryNode {
  final String name;
  final String path;
  final List<_DirectoryNode> children = [];
  int directCount = 0;

  _DirectoryNode(this.name, this.path);
}

List<_DirectoryNode> _buildDirectoryTree(List<DuplicateItem> items) {
  final roots = <String, _DirectoryNode>{};
  for (final item in items) {
    final rel = item.relativePath.replaceAll('\\', '/');
    final parts = rel.split('/');
    final full = item.fullPath.replaceAll('\\', '/');
    final rootPath = full
        .substring(0, full.length - rel.length)
        .replaceFirst(RegExp(r'/$'), '');
    final rootKey = '${item.libraryId}:$rootPath';
    final root = roots.putIfAbsent(
      rootKey,
      () => _DirectoryNode(rootPath, rootPath),
    );
    var current = root;
    var currentPath = rootPath;
    for (final part in parts.take(parts.length - 1)) {
      currentPath = '$currentPath/$part';
      current = current.children.firstWhere(
        (node) => node.path == currentPath,
        orElse: () {
          final node = _DirectoryNode(part, currentPath);
          current.children.add(node);
          return node;
        },
      );
    }
    current.directCount++;
  }
  for (final root in roots.values) {
    _sortTree(root);
  }
  final result =
      roots.values.toList()..sort((a, b) => a.path.compareTo(b.path));
  return result;
}

void _sortTree(_DirectoryNode node) {
  node.children.sort((a, b) => a.name.compareTo(b.name));
  for (final child in node.children) {
    _sortTree(child);
  }
}

String _parentPath(String path) {
  final normalized = path.replaceAll('\\', '/');
  final index = normalized.lastIndexOf('/');
  return index < 0 ? '' : normalized.substring(0, index);
}

String _fileName(String path) {
  final normalized = path.replaceAll('\\', '/');
  return normalized.substring(normalized.lastIndexOf('/') + 1);
}

String _meta(DuplicateItem item) {
  final size =
      item.fileSize < 1024 * 1024
          ? '${(item.fileSize / 1024).toStringAsFixed(1)} KB'
          : '${(item.fileSize / 1024 / 1024).toStringAsFixed(1)} MB';
  if (item.kind == 'video' && item.durationMs != null) {
    final seconds = item.durationMs! ~/ 1000;
    return '${seconds ~/ 60}:${(seconds % 60).toString().padLeft(2, '0')} · $size';
  }
  if (item.width != null && item.height != null)
    return '${item.width}×${item.height} · $size';
  return size;
}
