// dir_compare_screen.dart：目录对比独立页面。
// 生成（选收藏库 + 目录 + 阈值）+ 结果展示（目录树/重复分组双视图 + 右键菜单 + 一键清除），
// 与重复报告页（report_screen.dart）的展示与操作保持一致。
// 代码注释使用中文。
import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../models/models.dart';
import '../services/api_service.dart';
import '../widgets/context_menu.dart';
import '../widgets/report_common.dart';

enum _DirView { directory, group }

class DirCompareScreen extends StatefulWidget {
  final ApiService api;
  const DirCompareScreen({super.key, required this.api});

  @override
  State<DirCompareScreen> createState() => _DirCompareScreenState();
}

class _DirCompareScreenState extends State<DirCompareScreen> {
  DirCompareSummary? _summary;
  List<DuplicateTreeNode> _tree = [];
  List<DirCompareGroupItem> _allGroups = []; // 全量分组（目录树视图聚合用）
  final Map<int, DirCompareGroupItem> _groupByMediaId = {};
  DirCompareGroupPage? _page;
  int _pageNo = 1;
  int _pageSize = 20;
  static const _pageSizeOptions = [10, 20, 50, 100];
  String _kind = 'all';
  _DirView _view = _DirView.directory;

  bool _loading = true;
  bool _clearingDir = false;
  String? _error;
  String? _dirTaskId; // 进行中的目录对比任务
  Timer? _dirPollTimer;

  // 目录树收起状态（默认全部展开，可点击收起）
  final Set<String> _collapsedPaths = {};
  String? _selectedDirectory;

  // 目录树搜索：关键词非空时过滤树并自动定位到第一个匹配节点
  final TextEditingController _treeSearchCtrl = TextEditingController();
  String _treeQuery = '';

  @override
  void initState() {
    super.initState();
    _syncDirTask();
    _loadAll();
  }

  @override
  void dispose() {
    _dirPollTimer?.cancel();
    _treeSearchCtrl.dispose();
    super.dispose();
  }

  // ===== 任务同步与轮询 =====

  /// 进入页面时检测是否有进行中的目录对比任务，恢复轮询。
  Future<void> _syncDirTask() async {
    try {
      final tasks = await widget.api.getTasks();
      for (final t in tasks) {
        if (t.kind == 'report_directory' &&
            (t.status == 'queued' || t.status == 'running')) {
          _dirTaskId = t.id;
          _startDirPolling();
          break;
        }
      }
    } catch (_) {
      // 查询失败不阻塞页面
    }
  }

  void _startDirPolling() {
    _dirPollTimer ??= Timer.periodic(
      const Duration(seconds: 1),
      (_) => _pollDirTask(),
    );
  }

  Future<void> _pollDirTask() async {
    final id = _dirTaskId;
    if (id == null) {
      _dirPollTimer?.cancel();
      _dirPollTimer = null;
      return;
    }
    try {
      final task = await widget.api.getTask(id);
      if (task.isCompleted || task.isFailed || task.isCancelled) {
        _dirPollTimer?.cancel();
        _dirPollTimer = null;
        _dirTaskId = null;
        if (task.isFailed && mounted) {
          setState(() => _error = task.errorMessage ?? '目录对比任务未完成');
        }
        await _loadAll();
      }
    } catch (_) {
      // 任务查询失败不阻塞
    }
  }

  // ===== 数据加载 =====

  Future<void> _loadAll() async {
    DirCompareSummary? summary;
    var summaryOk = false;
    List<DuplicateTreeNode> tree = [];
    var treeOk = false;
    List<DirCompareGroupItem> groups = [];
    var groupsOk = false;
    DirCompareGroupPage? page;
    var pageOk = false;
    String? loadError;

    try {
      summary = await widget.api.getDirCompareSummary();
      summaryOk = true;
    } catch (e) {
      loadError = '摘要: $e';
    }
    try {
      tree = await widget.api.getDirCompareTree(kind: _kind);
      treeOk = true;
    } catch (e) {
      loadError = '目录树: $e';
    }
    try {
      groups = await _fetchAllGroups();
      groupsOk = true;
    } catch (e) {
      loadError = '分组: $e';
    }
    try {
      page = await _fetchPage(_pageNo, _pageSize);
      pageOk = true;
    } catch (e) {
      loadError = '分页: $e';
    }
    if (!mounted) return;
    setState(() {
      if (summaryOk) _summary = summary;
      if (treeOk) {
        _tree = tree;
        if (_selectedDirectory != null &&
            !_treeContainsPath(tree, _selectedDirectory!)) {
          _selectedDirectory = tree.isEmpty ? null : tree.first.path;
        }
      }
      if (groupsOk) {
        final valid =
            groups.where((g) => g.items.length >= 2).toList();
        _allGroups = valid;
        _groupByMediaId
          ..clear()
          ..addEntries(
            valid.expand(
              (g) => g.items.map((m) => MapEntry(m.id, g)),
            ),
          );
      }
      if (pageOk) {
        _page = page;
        if (page!.totalPages < _pageNo) {
          _pageNo = page.totalPages < 1 ? 1 : page.totalPages;
        }
      }
      _loading = false;
      _error = loadError;
    });
  }

  Future<List<DirCompareGroupItem>> _fetchAllGroups() async {
    final first = await widget.api.getDirCompareGroups(
      page: 1,
      pageSize: 100,
      kind: _kind,
    );
    final all = [...first.items];
    for (var p = 2; p <= first.totalPages; p++) {
      final page = await widget.api.getDirCompareGroups(
        page: p,
        pageSize: 100,
        kind: _kind,
      );
      all.addAll(page.items);
    }
    return all;
  }

  Future<DirCompareGroupPage> _fetchPage(int pageNo, int pageSize) =>
      widget.api.getDirCompareGroups(
        page: pageNo,
        pageSize: pageSize,
        kind: _kind,
      );

  Future<void> _refreshPage() async {
    try {
      final page = await _fetchPage(_pageNo, _pageSize);
      if (mounted) setState(() => _page = page);
    } catch (e) {
      if (mounted) setState(() => _error = '读取分组失败: $e');
    }
  }

  bool _treeContainsPath(List<DuplicateTreeNode> nodes, String path) {
    for (final n in nodes) {
      if (n.path == path || _treeContainsPath(n.children, path)) {
        return true;
      }
    }
    return false;
  }

  // ===== 生成目录对比 =====

  Future<void> _showGenerateDialog() async {
    if (mounted) setState(() => _error = null);
    final result = await showDialog<
      ({
        int libraryId,
        String directory,
        String mediaType,
        int imageThreshold,
        int videoPhashDistance,
        int videoDurationDiffMs,
      })
    >(context: context, builder: (_) => DirCompareDialog(api: widget.api));
    if (result == null || !mounted) return;
    await _submitDirCompare(
      result.libraryId,
      result.directory,
      mediaType: result.mediaType,
      imageThreshold: result.imageThreshold,
      videoPhashDistance: result.videoPhashDistance,
      videoDurationDiffMs: result.videoDurationDiffMs,
    );
  }

  Future<void> _submitDirCompare(
    int libraryId,
    String directory, {
    String mediaType = 'all',
    int? imageThreshold,
    int? videoPhashDistance,
    int? videoDurationDiffMs,
  }) async {
    try {
      final resp = await widget.api.generateDirCompare(
        libraryId: libraryId,
        directory: directory,
        mediaType: mediaType,
        imageThreshold: imageThreshold,
        videoPhashDistance: videoPhashDistance,
        videoDurationDiffMs: videoDurationDiffMs,
      );
      _dirTaskId = resp['task_id'] as String?;
      _startDirPolling();
      if (mounted) setState(() {});
    } catch (e) {
      if (mounted) setState(() => _error = '提交目录对比失败: $e');
    }
  }

  // ===== 媒体操作 =====

  Future<void> _openMedia(int mediaId, bool directory) async {
    try {
      directory
          ? await widget.api.openMediaDirectory(mediaId)
          : await widget.api.openMediaFile(mediaId);
    } catch (e) {
      if (mounted) setState(() => _error = '打开失败: $e');
    }
  }

  /// 排除重复（目录对比版）：仅当前报告生效，重新生成后恢复参与检测。
  Future<void> _excludeMedia(int mediaId) async {
    try {
      await widget.api.excludeDirCompareMedia(mediaId);
      _removeMediaFromLocalState([mediaId]);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text('已排除该文件：不再计入当前目录对比报告的重复统计，重新生成后恢复参与检测'),
          ),
        );
      }
      await _loadAll();
    } catch (e) {
      if (mounted) setState(() => _error = '排除重复失败: $e');
    }
  }

  /// 删除此文件外本组重复文件（目录树视图传 directoryScope 时只删该目录之外成员）。
  Future<void> _deleteOthers(
    DirCompareGroupItem group,
    int exceptId, {
    String? directoryScope,
  }) async {
    final List<int> others;
    String title;
    if (directoryScope != null) {
      others =
          group.items
              .where(
                (m) =>
                    m.id != exceptId &&
                    relDir(m.relativePath) != directoryScope,
              )
              .map((m) => m.id)
              .toList();
      title = '删除此目录外本组重复文件';
    } else {
      others =
          group.items.where((m) => m.id != exceptId).map((m) => m.id).toList();
      title = '删除此文件外本组重复文件';
    }
    if (others.isEmpty) return;
    final ok = await confirmClearDialog(
      context,
      title: title,
      keep: 'keep_current',
      count: others.length,
    );
    if (!ok || !mounted) return;
    try {
      final result = await widget.api.deleteMedia(others);
      _removeMediaFromLocalState(others);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              '已删除 ${result.deletedFiles} 个文件，释放 ${formatBytes(result.freedBytes)}',
            ),
          ),
        );
      }
      await _loadAll();
    } catch (e) {
      if (mounted) setState(() => _error = '删除失败: $e');
    }
  }

  /// 删除此文件：独立简单确认 → 删除本地文件/media 记录/缩略图 → 局部刷新。
  void _deleteFile(DuplicateItem item) {
    showDialog<void>(
      context: context,
      builder:
          (ctx) => AlertDialog(
            title: const Text('删除此文件'),
            content: Text(
              '将删除「${fileName(item.fullPath)}」\n'
              '同步删除本地文件、数据库记录与缩略图，此操作不可恢复。',
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.of(ctx).pop(),
                child: const Text('取消'),
              ),
              FilledButton(
                style: FilledButton.styleFrom(
                  backgroundColor: Theme.of(ctx).colorScheme.error,
                ),
                onPressed: () {
                  Navigator.of(ctx).pop();
                  _deleteOne(item);
                },
                child: const Text('确定删除'),
              ),
            ],
          ),
    );
  }

  Future<void> _deleteOne(DuplicateItem item) async {
    try {
      final result = await widget.api.deleteMedia([item.id]);
      _removeMediaFromLocalState([item.id]);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              '已删除 1 个文件，释放 ${formatBytes(result.freedBytes)}',
            ),
          ),
        );
      }
      await _loadAll();
    } catch (e) {
      if (mounted) setState(() => _error = '删除失败: $e');
    }
  }

  /// 乐观移除已删除的 media id：同步更新 _allGroups / _groupByMediaId / _page。
  void _removeMediaFromLocalState(List<int> deletedIds) {
    if (deletedIds.isEmpty) return;
    final idSet = deletedIds.toSet();
    setState(() {
      _groupByMediaId.removeWhere((id, _) => idSet.contains(id));
      final updated = <DirCompareGroupItem>[];
      for (final g in _allGroups) {
        final remaining = g.items.where((m) => !idSet.contains(m.id)).toList();
        if (remaining.length >= 2) {
          updated.add(
            DirCompareGroupItem(
              id: g.id,
              groupType: g.groupType,
              memberCount: remaining.length,
              freedBytes: g.freedBytes,
              items: remaining,
            ),
          );
        }
      }
      _allGroups = updated;
      if (_page != null) {
        final pageItems = <DirCompareGroupItem>[];
        for (final g in _page!.items) {
          final remaining =
              g.items.where((m) => !idSet.contains(m.id)).toList();
          if (remaining.length >= 2) {
            pageItems.add(
              DirCompareGroupItem(
                id: g.id,
                groupType: g.groupType,
                memberCount: remaining.length,
                freedBytes: g.freedBytes,
                items: remaining,
              ),
            );
          }
        }
        _page = DirCompareGroupPage(
          total: pageItems.length,
          page: _page!.page,
          pageSize: _page!.pageSize,
          totalPages:
              pageItems.isEmpty ? 1 : (pageItems.length / _page!.pageSize).ceil(),
          items: pageItems,
        );
      }
    });
  }

  // ===== 一键清除 =====

  Future<void> _clearDirectory(String dir) async {
    setState(() => _clearingDir = true);
    try {
      final dirItems =
          _allGroups
              .expand((g) => g.items)
              .where((m) => relDir(m.relativePath) == dir)
              .toList();
      final keep = await showKeepDialog(
        context,
        title: '一键清除此目录重复数据',
        count: dirItems.length,
      );
      if (keep == null || !mounted) return;
      final ok = await confirmClearDialog(
        context,
        title: '一键清除此目录重复数据',
        keep: keep,
        count: dirItems.length,
      );
      if (!ok || !mounted) return;
      final toDelete = _computeDirDeletedIds(dirItems, keep);
      final result = await widget.api.clearDirCompare(
        scope: 'directory',
        keep: keep,
        directory: dir,
      );
      _removeMediaFromLocalState(toDelete);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              '已删除 ${result.deletedFiles} 个文件，释放 ${formatBytes(result.freedBytes)}'
              '，剩余 ${result.remainingGroups} 组',
            ),
          ),
        );
      }
      await _loadAll();
    } catch (e) {
      if (mounted) setState(() => _error = '清除失败: $e');
    } finally {
      if (mounted) setState(() => _clearingDir = false);
    }
  }

  Future<void> _clearPage() async {
    final groupIds = (_page?.items ?? []).map((g) => g.id).toList();
    final count = (_page?.items ?? []).fold<int>(
      0,
      (sum, g) => sum + g.memberCount,
    );
    final keep = await showKeepDialog(context, title: '一键清除本页重复文件', count: count);
    if (keep == null || !mounted) return;
    final ok = await confirmClearDialog(
      context,
      title: '一键清除本页重复文件',
      keep: keep,
      count: count,
    );
    if (!ok || !mounted) return;
    try {
      final result = await widget.api.clearDirCompare(
        scope: 'page',
        keep: keep,
        groupIds: groupIds,
      );
      final deletedIds = <int>[];
      for (final g in (_page?.items ?? [])) {
        final keepIdx = pickKeepIndex(g.items, keep);
        for (var i = 0; i < g.items.length; i++) {
          if (i != keepIdx) deletedIds.add(g.items[i].id);
        }
      }
      _removeMediaFromLocalState(deletedIds);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              '已删除 ${result.deletedFiles} 个文件，释放 ${formatBytes(result.freedBytes)}'
              '，剩余 ${result.remainingGroups} 组',
            ),
          ),
        );
      }
      await _loadAll();
    } catch (e) {
      if (mounted) setState(() => _error = '清除失败: $e');
    }
  }

  /// 计算本目录内每组按保留条件应删除的 media id 列表（乐观更新用）。
  List<int> _computeDirDeletedIds(List<DuplicateItem> dirItems, String keep) {
    final byGroup = <int, List<DuplicateItem>>{};
    for (final item in dirItems) {
      final g = _groupByMediaId[item.id];
      if (g == null) continue;
      byGroup.putIfAbsent(g.id, () => []).add(item);
    }
    final toDelete = <int>[];
    for (final members in byGroup.values) {
      if (members.length < 2) continue;
      final keepIdx = pickKeepIndex(members, keep);
      for (var i = 0; i < members.length; i++) {
        if (i != keepIdx) toDelete.add(members[i].id);
      }
    }
    return toDelete;
  }

  // ===== UI =====

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    if (_loading) return const Center(child: CircularProgressIndicator());
    final summary = _summary;
    if (summary == null || summary.report == null) {
      return _buildEmpty(cs);
    }
    return Column(
      children: [
        _buildToolbar(cs),
        if (_dirTaskId != null) _buildTaskBanner(cs),
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
              _view == _DirView.directory
                  ? _buildDirectoryView(cs)
                  : _buildGroupView(cs),
        ),
      ],
    );
  }

  Widget _buildEmpty(ColorScheme cs) => Center(
    child: Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(
          Icons.folder_copy_outlined,
          size: 64,
          color: cs.outline.withValues(alpha: 0.4),
        ),
        const SizedBox(height: 16),
        Text('暂无目录对比结果', style: TextStyle(fontSize: 16, color: cs.onSurface)),
        const SizedBox(height: 6),
        Text('选择收藏库与目录，与其余存量数据对比重复', style: TextStyle(fontSize: 13, color: cs.outline)),
        const SizedBox(height: 20),
        FilledButton.icon(
          onPressed: _dirTaskId != null ? null : _showGenerateDialog,
          icon: _dirTaskId != null
              ? const SizedBox(
                  width: 16,
                  height: 16,
                  child: CircularProgressIndicator(strokeWidth: 2),
                )
              : const Icon(Icons.play_arrow),
          label: Text(_dirTaskId != null ? '对比中…' : '开始目录对比'),
        ),
      ],
    ),
  );

  Widget _buildTaskBanner(ColorScheme cs) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 8),
      color: cs.primary.withValues(alpha: 0.08),
      child: Row(
        children: [
          SizedBox(
            width: 16,
            height: 16,
            child: CircularProgressIndicator(strokeWidth: 2, color: cs.primary),
          ),
          const SizedBox(width: 10),
          const Expanded(
            child: Text('目录对比报告生成中…', style: TextStyle(fontSize: 13)),
          ),
        ],
      ),
    );
  }

  Widget _buildToolbar(ColorScheme cs) {
    final summary = _summary?.report;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 10),
      decoration: BoxDecoration(
        border: Border(bottom: BorderSide(color: cs.outlineVariant)),
      ),
      child: Wrap(
        spacing: 12,
        runSpacing: 8,
        crossAxisAlignment: WrapCrossAlignment.center,
        children: [
          Icon(Icons.folder_copy_outlined, color: cs.primary),
          Text(
            '目录对比',
            style: TextStyle(
              fontSize: 16,
              fontWeight: FontWeight.w700,
              color: cs.onSurface,
            ),
          ),
          if (summary != null)
            Text(
              '${summary.directory} · ${summary.totalGroups} 组 · '
              '${summary.totalFiles} 个文件 · 可释放 ${formatBytes(_summary!.freedBytes)}',
              style: TextStyle(fontSize: 12, color: cs.outline),
            ),
          _mediaTypeSegmented(cs),
          _viewModeSegmented(cs),
          IconButton(
            icon: const Icon(Icons.refresh, size: 20),
            tooltip: '刷新',
            onPressed: _loading ? null : _loadAll,
          ),
          FilledButton.icon(
            onPressed: _dirTaskId != null ? null : _showGenerateDialog,
            icon: _dirTaskId != null
                ? const SizedBox(
                    width: 16,
                    height: 16,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.play_arrow, size: 18),
            label: Text(_dirTaskId != null ? '对比中' : '重新生成'),
          ),
        ],
      ),
    );
  }

  Widget _mediaTypeSegmented(ColorScheme cs) {
    return SegmentedButton<String>(
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
      onSelectionChanged: (value) async {
        setState(() {
          _kind = value.first;
          _selectedDirectory = null;
          _pageNo = 1;
        });
        await _loadAll();
      },
    );
  }

  Widget _viewModeSegmented(ColorScheme cs) {
    return SegmentedButton<_DirView>(
      segments: const [
        ButtonSegment(
          value: _DirView.directory,
          label: Text('目录树'),
          icon: Icon(Icons.account_tree_outlined, size: 16),
        ),
        ButtonSegment(
          value: _DirView.group,
          label: Text('重复分组'),
          icon: Icon(Icons.grid_view_outlined, size: 16),
        ),
      ],
      selected: {_view},
      onSelectionChanged: (value) => setState(() => _view = value.first),
    );
  }

  // ===== 目录树视图 =====

  Widget _buildDirectoryView(ColorScheme cs) {
    final selected =
        _selectedDirectory ?? (_tree.isNotEmpty ? _tree.first.path : null);
    final files =
        selected == null
            ? <DuplicateItem>[]
            : _allGroups
                .expand((g) => g.items)
                .where((item) => relDir(item.relativePath) == selected)
                .toList();
    return LayoutBuilder(
      builder: (context, constraints) {
        if (constraints.maxWidth >= 800) {
          return Row(
            children: [
              SizedBox(width: 300, child: _buildTreeList(cs, selected)),
              VerticalDivider(width: 1, color: cs.outlineVariant),
              Expanded(
                child: Column(
                  children: [
                    _buildDirActionBar(cs, selected),
                    Expanded(child: _buildDirClusters(files, cs)),
                  ],
                ),
              ),
            ],
          );
        }
        return Column(
          children: [
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
              child: DropdownButtonFormField<String>(
                value:
                    _tree.any((n) => n.path == selected)
                        ? selected
                        : (_tree.isNotEmpty ? _tree.first.path : null),
                items:
                    _tree
                        .map(
                          (n) => DropdownMenuItem(
                            value: n.path,
                            child: Text(
                              n.path,
                              style: const TextStyle(fontSize: 13),
                            ),
                          ),
                        )
                        .toList(),
                onChanged: (v) => setState(() => _selectedDirectory = v),
                decoration: const InputDecoration(isDense: true),
              ),
            ),
            _buildDirActionBar(cs, selected),
            Expanded(child: _buildDirClusters(files, cs)),
          ],
        );
      },
    );
  }

  Widget _buildDirActionBar(ColorScheme cs, String? selectedDir) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
      child: Row(
        children: [
          Text(
            selectedDir == null || selectedDir.isEmpty ? '根目录' : selectedDir,
            style: TextStyle(fontSize: 13, color: cs.onSurfaceVariant),
            overflow: TextOverflow.ellipsis,
          ),
          const Spacer(),
          if (selectedDir != null)
            OutlinedButton.icon(
              onPressed:
                  _clearingDir ? null : () => _clearDirectory(selectedDir),
              icon:
                  _clearingDir
                      ? const SizedBox(
                          width: 16,
                          height: 16,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Icon(Icons.delete_sweep_outlined, size: 16),
              label: Text(_clearingDir ? '清除中…' : '一键清除此目录重复数据'),
            ),
        ],
      ),
    );
  }

  Widget _buildTreeList(ColorScheme cs, String? selected) {
    if (_tree.isEmpty) {
      return const Center(child: Text('无重复目录'));
    }
    final query = _treeQuery.trim().toLowerCase();
    // 搜索时只展示匹配节点（保留祖先链保证层级定位），并强制展开全部节点
    final visibleTree = query.isEmpty ? _tree : _filterTreeNodes(_tree, query);
    return Column(
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(10, 8, 10, 2),
          child: TextField(
            controller: _treeSearchCtrl,
            onChanged: _onTreeSearch,
            decoration: InputDecoration(
              hintText: '搜索目录…',
              prefixIcon: const Icon(Icons.search, size: 18),
              isDense: true,
              border: OutlineInputBorder(
                borderRadius: BorderRadius.circular(8),
              ),
              contentPadding: const EdgeInsets.symmetric(
                horizontal: 10,
                vertical: 8,
              ),
            ),
            style: const TextStyle(fontSize: 12),
          ),
        ),
        Expanded(
          child:
              visibleTree.isEmpty
                  ? const Center(child: Text('无匹配目录'))
                  : ListView(
                    padding: const EdgeInsets.all(10),
                    children: visibleTree
                        .expand(
                          (node) => _buildTreeRows(
                            node,
                            0,
                            selected,
                            cs,
                            query: query,
                          ),
                        )
                        .toList(),
                  ),
        ),
      ],
    );
  }

  /// 目录树搜索：更新关键词；非空时自动选中并定位到第一个匹配节点。
  void _onTreeSearch(String v) {
    setState(() {
      _treeQuery = v;
      final q = v.trim().toLowerCase();
      if (q.isNotEmpty) {
        final first = _firstTreeMatch(_tree, q);
        if (first != null) _selectedDirectory = first.path;
      }
    });
  }

  /// 递归过滤树：匹配节点保留，非匹配但存在匹配后代的节点保留（祖先链）。
  List<DuplicateTreeNode> _filterTreeNodes(
    List<DuplicateTreeNode> nodes,
    String q,
  ) {
    final out = <DuplicateTreeNode>[];
    for (final node in nodes) {
      final matched =
          node.path.toLowerCase().contains(q) ||
          node.name.toLowerCase().contains(q);
      final children = _filterTreeNodes(node.children, q);
      if (matched || children.isNotEmpty) {
        out.add(
          DuplicateTreeNode(
            name: node.name,
            path: node.path,
            fileCount: node.fileCount,
            children: children,
          ),
        );
      }
    }
    return out;
  }

  /// 深度优先返回第一个匹配节点（含子节点）。
  DuplicateTreeNode? _firstTreeMatch(List<DuplicateTreeNode> nodes, String q) {
    for (final node in nodes) {
      if (node.path.toLowerCase().contains(q) ||
          node.name.toLowerCase().contains(q)) {
        return node;
      }
      final child = _firstTreeMatch(node.children, q);
      if (child != null) return child;
    }
    return null;
  }

  /// 节点是否命中搜索关键词。
  bool _isTreeMatch(DuplicateTreeNode node, String q) =>
      q.isNotEmpty &&
      (node.path.toLowerCase().contains(q) ||
          node.name.toLowerCase().contains(q));

  List<Widget> _buildTreeRows(
    DuplicateTreeNode node,
    int depth,
    String? selected,
    ColorScheme cs, {
    String query = '',
  }) {
    // 默认全部展开，确保深层目录立即可见（可点击收起）；搜索时强制展开
    final searching = query.isNotEmpty;
    final expanded = searching || !_collapsedPaths.contains(node.path);
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
                              ? _collapsedPaths.add(node.path)
                              : _collapsedPaths.remove(node.path);
                        }),
          ),
          title: Text(
            node.name,
            overflow: TextOverflow.ellipsis,
            style: TextStyle(
              fontSize: 12,
              // 搜索时高亮匹配节点
              fontWeight: searching && _isTreeMatch(node, query)
                  ? FontWeight.w700
                  : FontWeight.w400,
              color: searching && _isTreeMatch(node, query) ? cs.primary : null,
            ),
          ),
          trailing:
              node.fileCount > 0
                  ? Text(
                      '${node.fileCount}',
                      style: TextStyle(fontSize: 11, color: cs.outline),
                    )
                  : null,
          onTap: () => setState(() => _selectedDirectory = node.path),
        ),
      ),
    ];
    if (expanded) {
      for (final child in node.children) {
        rows.addAll(
          _buildTreeRows(child, depth + 1, selected, cs, query: query),
        );
      }
    }
    return rows;
  }

  /// 目录树叠卡：按重复组聚合。目录对比的组是"目标 vs 存量"，目录内通常只有
  /// 1 个成员，因此单成员组也展示（叠卡显示本目录成员，点击进组详情看全部）。
  Widget _buildDirClusters(List<DuplicateItem> files, ColorScheme cs) {
    if (files.isEmpty) {
      return Center(
        child: Text('此目录没有直属重复文件', style: TextStyle(color: cs.outline)),
      );
    }
    final clusters = <int, List<DuplicateItem>>{};
    for (final item in files) {
      final g = _groupByMediaId[item.id];
      if (g == null) {
        continue;
      }
      clusters.putIfAbsent(g.id, () => []).add(item);
    }
    if (clusters.isEmpty) {
      return Center(
        child: Text('此目录没有直属重复文件', style: TextStyle(color: cs.outline)),
      );
    }
    return SingleChildScrollView(
      padding: const EdgeInsets.all(16),
      child: Wrap(
        spacing: 20,
        runSpacing: 20,
        children: [
          for (final entry in clusters.entries)
            StackedCluster(
              items: entry.value,
              api: widget.api,
              width: 300,
              onTap: () {
                final g = _groupByMediaId[entry.value.first.id];
                if (g != null) _showGroupDetail(g);
              },
              onSecondaryTap:
                  (pos) => _showMemberMenu(
                    pos,
                    entry.value.first,
                    _groupByMediaId[entry.value.first.id],
                    directoryScope:
                        _selectedDirectory ?? (_tree.isNotEmpty ? _tree.first.path : null),
                  ),
            ),
        ],
      ),
    );
  }

  // ===== 分组视图 =====

  Widget _buildGroupView(ColorScheme cs) {
    final items = _page?.items ?? [];
    return Column(
      children: [
        _buildGroupActionBar(cs),
        Expanded(
          child:
              items.isEmpty
                  ? Center(
                      child: Text(
                        '本页没有重复分组',
                        style: TextStyle(color: cs.outline),
                      ),
                    )
                  : ListView.builder(
                      padding: const EdgeInsets.fromLTRB(20, 16, 20, 24),
                      itemCount: items.length,
                      itemBuilder: (context, index) =>
                          _buildGroupSection(cs, items[index]),
                    ),
        ),
      ],
    );
  }

  Widget _buildGroupActionBar(ColorScheme cs) {
    final total = _page?.total ?? 0;
    final totalPages = _page?.totalPages ?? 1;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 8),
      decoration: BoxDecoration(
        border: Border(bottom: BorderSide(color: cs.outlineVariant)),
      ),
      child: Row(
        children: [
          Text('共 $total 组', style: TextStyle(fontSize: 12, color: cs.outline)),
          const SizedBox(width: 16),
          DropdownButton<int>(
            value: _pageSize,
            underline: const SizedBox.shrink(),
            items:
                _pageSizeOptions
                    .map(
                      (s) => DropdownMenuItem(
                        value: s,
                        child: Text(
                          '$s 组/页',
                          style: const TextStyle(fontSize: 12),
                        ),
                      ),
                    )
                    .toList(),
            onChanged: (v) {
              if (v == null) return;
              setState(() {
                _pageSize = v;
                _pageNo = 1;
              });
              _refreshPage();
            },
          ),
          const Spacer(),
          IconButton(
            icon: const Icon(Icons.chevron_left, size: 20),
            tooltip: '上一页',
            onPressed:
                _pageNo <= 1
                    ? null
                    : () {
                        setState(() => _pageNo--);
                        _refreshPage();
                      },
          ),
          Text(
            '$_pageNo / $totalPages',
            style: TextStyle(fontSize: 12, color: cs.onSurfaceVariant),
          ),
          IconButton(
            icon: const Icon(Icons.chevron_right, size: 20),
            tooltip: '下一页',
            onPressed:
                _pageNo >= totalPages
                    ? null
                    : () {
                        setState(() => _pageNo++);
                        _refreshPage();
                      },
          ),
          const SizedBox(width: 12),
          FilledButton.tonalIcon(
            onPressed: (_page?.items.isEmpty ?? true) ? null : _clearPage,
            icon: const Icon(Icons.delete_sweep_outlined, size: 16),
            label: const Text('一键清除本页重复文件'),
          ),
        ],
      ),
    );
  }

  Widget _buildGroupSection(ColorScheme cs, DirCompareGroupItem group) {
    return Container(
      margin: const EdgeInsets.only(bottom: 24),
      padding: const EdgeInsets.fromLTRB(4, 0, 4, 20),
      decoration: BoxDecoration(
        border: Border(bottom: BorderSide(color: cs.outlineVariant)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _buildGroupHeader(cs, group),
          const SizedBox(height: 12),
          _buildMemberGrid(
            group.items,
            cs,
            group: group,
            shrinkWrap: true,
          ),
        ],
      ),
    );
  }

  Widget _buildGroupHeader(ColorScheme cs, DirCompareGroupItem group) {
    // 所有成员父路径去重（保持首现顺序），根目录直属文件显示 "/"
    final dirs = <String>[];
    for (final item in group.items) {
      final dir = relDir(item.relativePath);
      if (!dirs.contains(dir)) dirs.add(dir);
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Wrap(
          spacing: 10,
          runSpacing: 8,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 5),
              decoration: BoxDecoration(
                color: cs.primary.withValues(alpha: 0.1),
                borderRadius: BorderRadius.circular(8),
              ),
              child: Text(
                group.reasonLabel,
                style: TextStyle(fontSize: 11, color: cs.primary),
              ),
            ),
            Text(
              '${group.memberCount} 个文件',
              style: TextStyle(
                fontSize: 12,
                fontWeight: FontWeight.w600,
                color: cs.onSurface,
              ),
            ),
            Text(
              '可释放 ${formatBytes(group.freedBytes)}',
              style: TextStyle(fontSize: 12, color: cs.outline),
            ),
          ],
        ),
        if (dirs.isNotEmpty) ...[
          const SizedBox(height: 7),
          Wrap(
            spacing: 6,
            runSpacing: 6,
            children: [
              for (final dir in dirs)
                DirChip(
                  label: dir.isEmpty ? '/' : dir,
                  onTap: () {
                    final rep = group.items.firstWhere(
                      (i) => relDir(i.relativePath) == dir,
                    );
                    _openMedia(rep.id, true);
                  },
                ),
            ],
          ),
        ],
      ],
    );
  }

  Widget _buildMemberGrid(
    List<DuplicateItem> items,
    ColorScheme cs, {
    required DirCompareGroupItem group,
    bool shrinkWrap = false,
    bool showPathTooltip = false,
    ValueChanged<DuplicateItem>? onTapMember,
  }) {
    if (items.isEmpty) {
      return Center(
        child: Text('没有重复文件', style: TextStyle(color: cs.outline)),
      );
    }
    return GridView.builder(
      shrinkWrap: shrinkWrap,
      physics: shrinkWrap ? const NeverScrollableScrollPhysics() : null,
      padding: const EdgeInsets.all(16),
      gridDelegate: const SliverGridDelegateWithMaxCrossAxisExtent(
        maxCrossAxisExtent: 200,
        crossAxisSpacing: 12,
        mainAxisSpacing: 12,
        childAspectRatio: 0.82,
      ),
      itemCount: items.length,
      itemBuilder: (context, index) {
        final item = items[index];
        return DuplicateMemberCard(
          item: item,
          api: widget.api,
          sameDirMode: false,
          showOtherPaths: true,
          group: null, // 菜单已由 menuBuilder 自定义（携带组信息）
          onTap: onTapMember == null ? null : () => onTapMember(item),
          onError: (m) => setState(() => _error = m),
          onDeleted: (ids) => _removeMediaFromLocalState(ids),
          menuBuilder: (ctx, pos, m) => _showMemberMenu(pos, m, group),
          badge: item.isTarget ? _targetBadge(cs) : null,
          showPathTooltip: showPathTooltip,
        );
      },
    );
  }

  /// "所选目录"角标（目录对比特有：标识所选目录内的文件）。
  Widget _targetBadge(ColorScheme cs) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
      decoration: BoxDecoration(
        color: cs.primary,
        borderRadius: BorderRadius.circular(4),
      ),
      child: Text(
        '所选目录',
        style: TextStyle(fontSize: 10, color: cs.onPrimary),
      ),
    );
  }

  /// 组详情弹窗：放大展示全部成员，可右键操作。
  void _showGroupDetail(DirCompareGroupItem group) {
    showDialog(
      context: context,
      builder:
          (ctx) => Dialog(
            insetPadding: const EdgeInsets.all(28),
            child: SizedBox(
              width: 720,
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Padding(
                    padding: const EdgeInsets.fromLTRB(20, 16, 12, 8),
                    child: Row(
                      children: [
                        Text(
                          '${group.reasonLabel} · ${group.memberCount} 个文件',
                          style: const TextStyle(
                            fontSize: 15,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                        const Spacer(),
                        IconButton(
                          icon: const Icon(Icons.close, size: 18),
                          onPressed: () => Navigator.of(ctx).pop(),
                        ),
                      ],
                    ),
                  ),
                  Flexible(
                    child: SizedBox(
                      height: 420,
                      child: _buildMemberGrid(
                        group.items,
                        Theme.of(ctx).colorScheme,
                        group: group,
                        showPathTooltip: true,
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ),
    );
  }

  // ===== 右键菜单 =====

  /// 成员/叠卡右键菜单（与重复报告一致，排除/删除走目录对比接口）。
  void _showMemberMenu(
    Offset position,
    DuplicateItem item,
    DirCompareGroupItem? group, {
    String? directoryScope,
  }) {
    // 目录树视图下，跨目录组才显示"删除此目录外本组重复文件"
    final hasOtherDirMembers =
        group != null &&
        group.items.any(
          (m) => relDir(m.relativePath) != relDir(item.relativePath),
        );
    final deleteLabel =
        directoryScope != null && hasOtherDirMembers
            ? '删除此目录外本组重复文件'
            : '删除此文件外本组重复文件';
    // 其它目录重复路径（二级菜单），按父路径去重
    final otherPaths =
        group == null
            ? <DuplicateItem>[]
            : group.items
                .where(
                  (m) =>
                      m.id != item.id &&
                      relDir(m.relativePath) != relDir(item.relativePath),
                )
                .toList();
    final otherDirItems = <DuplicateItem>[];
    for (final o in otherPaths) {
      final dir = relDir(o.relativePath);
      if (!otherDirItems.any((e) => relDir(e.relativePath) == dir)) {
        otherDirItems.add(o);
      }
    }
    showContextMenu(
      context: context,
      position: position,
      items: [
        ContextMenuItem(
          icon: Icons.folder_open,
          label: '打开文件路径',
          onTap: () => _openMedia(item.id, true),
        ),
        ContextMenuItem(
          icon: Icons.copy,
          label: '复制文件路径',
          onTap: () => Clipboard.setData(ClipboardData(text: item.fullPath)),
        ),
        ContextMenuItem(
          icon:
              item.kind == 'video'
                  ? Icons.play_circle_outline
                  : Icons.image_outlined,
          label: item.kind == 'video' ? '打开视频' : '打开图片',
          onTap: () => _openMedia(item.id, false),
        ),
        ContextMenuItem(
          icon: Icons.block,
          label: '排除重复',
          onTap: () => _excludeMedia(item.id),
        ),
        ContextMenuItem(
          icon: Icons.delete_outline,
          label: '删除此文件',
          isDestructive: true,
          onTap: () => _deleteFile(item),
        ),
        if (otherDirItems.isNotEmpty) ...[
          const ContextMenuItem.divider(),
          for (final other in otherDirItems)
            ContextMenuItem(
              icon: Icons.subdirectory_arrow_right,
              label: dirLabel(other),
              onTap: () => _showPathMenu(position, other),
            ),
        ],
        if (group != null && group.items.length > 1) ...[
          const ContextMenuItem.divider(),
          ContextMenuItem(
            icon: Icons.delete_forever_outlined,
            label: deleteLabel,
            isDestructive: true,
            onTap:
                () => _deleteOthers(
                  group,
                  item.id,
                  directoryScope:
                      directoryScope != null && hasOtherDirMembers
                          ? directoryScope
                          : null,
                ),
          ),
        ],
      ],
    );
  }

  /// 二级菜单：其它目录重复文件的打开/复制/打开媒体。
  void _showPathMenu(Offset position, DuplicateItem other) {
    showContextMenu(
      context: context,
      position: position,
      items: [
        ContextMenuItem(
          icon: Icons.folder_open,
          label: '打开文件路径',
          onTap: () => _openMedia(other.id, true),
        ),
        ContextMenuItem(
          icon: Icons.copy,
          label: '复制文件路径',
          onTap: () => Clipboard.setData(ClipboardData(text: other.fullPath)),
        ),
        ContextMenuItem(
          icon:
              other.kind == 'video'
                  ? Icons.play_circle_outline
                  : Icons.image_outlined,
          label: other.kind == 'video' ? '打开视频' : '打开图片',
          onTap: () => _openMedia(other.id, false),
        ),
      ],
    );
  }
}

/// 目录对比生成对话框：选择收藏库 + 目录（树选择）+ 阈值参数。
class DirCompareDialog extends StatefulWidget {
  final ApiService api;
  const DirCompareDialog({super.key, required this.api});

  @override
  State<DirCompareDialog> createState() => _DirCompareDialogState();
}

class _DirCompareDialogState extends State<DirCompareDialog> {
  List<Library> _libraries = [];
  Library? _selectedLib;
  final Map<String, List<FileTreeNode>> _childrenCache = {};
  String? _selectedDir;
  String _mediaType = 'all';
  final _imageCtrl = TextEditingController(text: '90');
  final _videoCtrl = TextEditingController(text: '12');
  final _durCtrl = TextEditingController(text: '3000');
  bool _loading = true;
  bool _submitting = false;

  @override
  void initState() {
    super.initState();
    _loadLibraries();
  }

  @override
  void dispose() {
    _imageCtrl.dispose();
    _videoCtrl.dispose();
    _durCtrl.dispose();
    super.dispose();
  }

  Future<void> _loadLibraries() async {
    try {
      final libs = await widget.api.getLibraries();
      if (!mounted) return;
      setState(() {
        _libraries = libs;
        _selectedLib = libs.isNotEmpty ? libs.first : null;
        _loading = false;
      });
      if (_selectedLib != null) {
        await _loadRoot();
      }
    } catch (e) {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _loadRoot() async {
    final lib = _selectedLib;
    if (lib == null) return;
    setState(() => _selectedDir = '.'); // 默认选中库根目录
    try {
      final nodes = await widget.api.getFileTree(lib.id);
      if (mounted) {
        setState(() {
          _childrenCache[''] = nodes;
        });
      }
    } catch (_) {}
  }

  /// 展开目录节点：懒加载子目录。
  Future<void> _expandDir(String path) async {
    if (_childrenCache.containsKey(path)) return;
    final lib = _selectedLib;
    if (lib == null) return;
    try {
      final nodes = await widget.api.getFileTree(lib.id, path: path);
      if (mounted) {
        setState(() {
          _childrenCache[path] = nodes;
        });
      }
    } catch (_) {}
  }

  List<FileTreeNode> _dirsOf(String path) {
    final cached = _childrenCache[path];
    if (cached == null) return const [];
    return cached.where((n) => n.isDir).toList();
  }

  void _onSubmit() {
    final lib = _selectedLib;
    final dir = _selectedDir;
    if (lib == null || dir == null) return;
    setState(() => _submitting = true);
    Navigator.of(context).pop((
      libraryId: lib.id,
      directory: dir,
      mediaType: _mediaType,
      imageThreshold: int.tryParse(_imageCtrl.text.trim()) ?? 90,
      videoPhashDistance: int.tryParse(_videoCtrl.text.trim()) ?? 12,
      videoDurationDiffMs: int.tryParse(_durCtrl.text.trim()) ?? 3000,
    ));
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return AlertDialog(
      title: const Text('目录对比（所选目录 vs 存量数据）'),
      content: SizedBox(
        width: 480,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Text('收藏库', style: TextStyle(fontSize: 13)),
              const SizedBox(height: 4),
              DropdownButtonFormField<int>(
                value: _selectedLib?.id,
                isExpanded: true,
                items: _libraries
                    .map(
                      (l) => DropdownMenuItem(
                        value: l.id,
                        child: Text(l.name, overflow: TextOverflow.ellipsis),
                      ),
                    )
                    .toList(),
                onChanged: (v) {
                  final lib = _libraries.firstWhere((l) => l.id == v);
                  setState(() => _selectedLib = lib);
                  _loadRoot();
                },
                decoration: const InputDecoration(isDense: true),
              ),
              const SizedBox(height: 12),
              const Text('目录（含子目录）', style: TextStyle(fontSize: 13)),
              const SizedBox(height: 4),
              if (_loading)
                const Padding(
                  padding: EdgeInsets.all(16),
                  child: Center(child: CircularProgressIndicator()),
                )
              else
                Container(
                  height: 240,
                  decoration: BoxDecoration(
                    border: Border.all(color: cs.outlineVariant),
                    borderRadius: BorderRadius.circular(8),
                  ),
                  child: ListView(
                    children: _buildDirRows('', 0),
                  ),
                ),
              const SizedBox(height: 12),
              const Text('媒体类型', style: TextStyle(fontSize: 13)),
              const SizedBox(height: 4),
              SegmentedButton<String>(
                segments: const [
                  ButtonSegment(value: 'all', label: Text('全部')),
                  ButtonSegment(value: 'image', label: Text('图片')),
                  ButtonSegment(value: 'video', label: Text('视频')),
                ],
                selected: {_mediaType},
                onSelectionChanged: (v) => setState(() => _mediaType = v.first),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: _imageCtrl,
                decoration: const InputDecoration(
                  labelText: '图片相似度阈值 %',
                  isDense: true,
                ),
                keyboardType: TextInputType.number,
              ),
              const SizedBox(height: 8),
              TextField(
                controller: _videoCtrl,
                decoration: const InputDecoration(
                  labelText: '视频 pHash 距离',
                  isDense: true,
                ),
                keyboardType: TextInputType.number,
              ),
              const SizedBox(height: 8),
              TextField(
                controller: _durCtrl,
                decoration: const InputDecoration(
                  labelText: '视频时长差（毫秒）',
                  isDense: true,
                ),
                keyboardType: TextInputType.number,
              ),
            ],
          ),
        ),
      ),
      actions: [
        TextButton(
          onPressed: _submitting ? null : () => Navigator.of(context).pop(),
          child: const Text('取消'),
        ),
        FilledButton(
          onPressed: _submitting || _selectedLib == null ? null : _onSubmit,
          child: const Text('开始对比'),
        ),
      ],
    );
  }

  List<Widget> _buildDirRows(String path, int depth) {
    final cs = Theme.of(context).colorScheme;
    final dirs = _dirsOf(path);
    final rows = <Widget>[];
    for (final dir in dirs) {
      final selected = _selectedDir == dir.path;
      rows.add(
        Padding(
          padding: EdgeInsets.only(left: depth * 14.0),
          child: ListTile(
            dense: true,
            selected: selected,
            leading: dir.hasChildren
                ? IconButton(
                    visualDensity: VisualDensity.compact,
                    icon: const Icon(Icons.keyboard_arrow_right, size: 17),
                    onPressed: () => _expandDir(dir.path),
                  )
                : const SizedBox(width: 40),
            title: Text(dir.name, style: const TextStyle(fontSize: 12)),
            trailing:
                selected
                    ? Icon(Icons.radio_button_checked, size: 16, color: cs.primary)
                    : null,
            onTap: () => setState(() => _selectedDir = dir.path),
          ),
        ),
      );
      rows.addAll(_buildDirRows(dir.path, depth + 1));
    }
    return rows;
  }
}
