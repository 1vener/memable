// report_screen.dart：重复报告页（目录树视图 + 分组视图）。
// 支持生成选项、分页、右键菜单、card_swiper 叠卡效果与一键清除。
// 代码注释使用中文。
import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../models/models.dart';
import '../services/api_service.dart';
import '../widgets/context_menu.dart';
import '../widgets/report_common.dart';

enum _ReportView { directory, group }

class ReportScreen extends StatefulWidget {
  final ApiService api;
  const ReportScreen({super.key, required this.api});

  @override
  State<ReportScreen> createState() => _ReportScreenState();
}

class _ReportScreenState extends State<ReportScreen> {
  ReportSummary? _summary;
  List<DuplicateGroupItem> _allGroups = [];
  final Map<int, DuplicateGroupItem> _groupByMediaId = {};
  List<DuplicateTreeNode> _tree = [];
  DuplicateGroupPage? _page;

  bool _loading = true;
  bool _generating = false;
  bool _clearingDir = false;
  String? _error;
  String _kind = 'all';
  _ReportView _view = _ReportView.directory;
  int _pageNo = 1;
  int _pageSize = 20;
  static const _pageSizeOptions = [10, 20, 50, 100];

  // 目录树收起状态（默认全部展开，用户点击折叠；刷新后保留，不重建）
  final Set<String> _collapsedPaths = {};
  String? _selectedDirectory;

  // 目录树搜索：关键词非空时过滤树并自动定位到第一个匹配节点
  final TextEditingController _treeSearchCtrl = TextEditingController();
  String _treeQuery = '';

  Timer? _pollTimer;

  @override
  void dispose() {
    _pollTimer?.cancel();
    _treeSearchCtrl.dispose();
    super.dispose();
  }

  @override
  void initState() {
    super.initState();
    _refreshAll();
  }

  // ===== 数据加载 =====

  Future<void> _refreshAll() async {
    await _syncActiveReportTask();
    // 各数据源独立容错：成功返回空列表时也必须覆盖旧数据，避免删除后残留图片。
    ReportSummary? summary;
    var summaryOk = false;
    List<DuplicateTreeNode> tree = [];
    var treeOk = false;
    List<DuplicateGroupItem> groups = [];
    var groupsOk = false;
    String? loadError;

    try {
      summary = await widget.api.getReportSummary();
      summaryOk = true;
    } catch (e) {
      loadError = '摘要: $e';
    }
    try {
      // 目录树按当前媒体类型过滤，与 _allGroups 口径一致（否则树里会出现
      // 另一类型独占的目录，点击后无数据）。
      tree = await widget.api.getReportTree(kind: _kind);
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
    if (!mounted) return;
    setState(() {
      if (summaryOk) _summary = summary;
      if (treeOk) {
        _tree = tree;
        // 清理后当前目录可能已从重复目录树消失，不能继续保留幽灵选中项。
        if (_selectedDirectory != null &&
            !_treeContainsPath(tree, _selectedDirectory!)) {
          _selectedDirectory = tree.isEmpty ? null : tree.first.path;
        }
      }
      if (groupsOk) {
        // 单成员组已不构成重复。即使后端遇到历史脏数据，也不能继续渲染
        // “1 个文件”的重复卡片。
        final validGroups =
            groups.where((group) => group.items.length >= 2).toList();
        _allGroups = validGroups;
        _groupByMediaId
          ..clear()
          ..addEntries(
            validGroups.expand(
              (group) => group.items.map((item) => MapEntry(item.id, group)),
            ),
          );
      }
      _loading = false;
      if (loadError != null) {
        _error = '部分数据刷新失败: $loadError';
      } else {
        _error = null;
      }
    });
    if (groupsOk) {
      await _refreshPage();
    }
  }

  /// 页面加载时自动发现进行中的重复报告任务，保证按钮在任务结束前保持禁用。
  Future<void> _syncActiveReportTask() async {
    try {
      final tasks = await widget.api.getTasks();
      for (final task in tasks) {
        if (task.kind == 'report_duplicate' && task.isActive) {
          _activeTaskStr = task.id;
          _startPolling();
          return;
        }
      }
    } catch (_) {
      // 查询任务列表失败不阻塞报告数据加载
    }
  }

  bool _treeContainsPath(List<DuplicateTreeNode> nodes, String path) {
    for (final node in nodes) {
      if (node.path == path || _treeContainsPath(node.children, path)) {
        return true;
      }
    }
    return false;
  }

  Future<void> _refreshPage() async {
    try {
      final page = await widget.api.getReportGroups(
        page: _pageNo,
        pageSize: _pageSize,
        kind: _kind,
      );
      var nextPage = _pageNo;
      if (page.totalPages < nextPage) {
        nextPage = page.totalPages < 1 ? 1 : page.totalPages;
      }
      final data =
          nextPage == _pageNo
              ? page
              : await widget.api.getReportGroups(
                page: nextPage,
                pageSize: _pageSize,
                kind: _kind,
              );
      if (!mounted) return;
      setState(() {
        _pageNo = nextPage;
        _page = data;
      });
    } catch (e) {
      if (mounted) setState(() => _error = '读取分组数据失败: $e');
    }
  }

  Future<List<DuplicateGroupItem>> _fetchAllGroups() async {
    final all = <DuplicateGroupItem>[];
    var page = 1;
    while (true) {
      final p = await widget.api.getReportGroups(
        page: page,
        pageSize: 200,
        kind: _kind,
      );
      all.addAll(p.items);
      if (page >= p.totalPages || p.items.isEmpty) break;
      page++;
    }
    return all;
  }

  // ===== 生成报告 =====

  Future<void> _generateReport() async {
    final options = await _showOptionsDialog();
    if (options == null || !mounted) return;
    setState(() {
      _generating = true;
      _error = null;
    });
    try {
      final result = await widget.api.generateReport(options);
      _activeTaskStr = result['task_id'] as String?;
      _startPolling();
    } catch (e) {
      if (mounted) setState(() => _error = '提交重复报告失败: $e');
    } finally {
      if (mounted) setState(() => _generating = false);
    }
  }

  String? _activeTaskStr;

  /// 生成任务进行中（提交中或后台任务尚未结束）
  bool get _taskBusy => _generating || _activeTaskStr != null;

  void _startPolling() {
    _pollTimer ??= Timer.periodic(
      const Duration(seconds: 1),
      (_) => _pollTask(),
    );
  }

  Future<void> _pollTask() async {
    final id = _activeTaskStr;
    if (id == null) {
      _pollTimer?.cancel();
      _pollTimer = null;
      return;
    }
    try {
      final task = await widget.api.getTask(id);
      if (task.isCompleted) {
        _pollTimer?.cancel();
        _pollTimer = null;
        _activeTaskStr = null;
        await _refreshAll();
      } else if (task.isFailed || task.isCancelled) {
        _pollTimer?.cancel();
        _pollTimer = null;
        _activeTaskStr = null;
        if (mounted) {
          setState(() => _error = task.errorMessage ?? '重复报告任务未完成');
        }
      }
    } catch (e) {
      if (mounted) setState(() => _error = '查询任务失败: $e');
      // 任务已不存在（如服务重启清空队列）：停止轮询并恢复按钮
      if ('$e'.contains('不存在')) {
        _pollTimer?.cancel();
        _pollTimer = null;
        _activeTaskStr = null;
        if (mounted) setState(() {});
      }
    }
  }



  // ===== UI =====

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    if (_loading) return const Center(child: CircularProgressIndicator());
    return Column(
      children: [
        _buildToolbar(cs),
        if (_summary?.stale ?? false) _buildStaleBanner(cs),
        if (_error != null)
          MaterialBanner(
            content: Text(_error!),
            actions: [
              TextButton(
                onPressed: () => setState(() => _error = null),
                child: const Text('关闭'),
              ),            ],
          ),
        if (_summary == null)
          Expanded(child: _buildEmpty(cs))
        else
          Expanded(
            child:
                _view == _ReportView.directory
                    ? _buildDirectoryView(cs)
                    : _buildGroupView(cs),
          ),
      ],
    );
  }

  Widget _buildStaleBanner(ColorScheme cs) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 8),
      color: const Color(0xFFFEF3C7),
      child: Row(
        children: [
          const Icon(Icons.info_outline, size: 18, color: Color(0xFFB45309)),
          const SizedBox(width: 8),
          const Expanded(
            child: Text(
              '收藏库数据已变化，建议重新生成报告',
              style: TextStyle(fontSize: 13, color: Color(0xFF92400E)),
            ),
          ),
          TextButton(
            onPressed: _taskBusy ? null : _generateReport,
            child: const Text('重新生成'),
          ),
        ],
      ),
    );
  }

  Widget _buildToolbar(ColorScheme cs) {
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
          Icon(Icons.content_copy_rounded, color: cs.primary),
          Text(
            '重复统计',
            style: TextStyle(
              fontSize: 16,
              fontWeight: FontWeight.w700,
              color: cs.onSurface,
            ),
          ),
          if (_summary != null)
            Text(
              '${_summary!.totalGroups} 组 · ${_summary!.totalFiles} 个文件'
              ' · 可释放 ${formatBytes(_summary!.freedBytes)}',
              style: TextStyle(fontSize: 12, color: cs.outline),
            ),
          _mediaTypeSegmented(cs),
          _viewModeSegmented(cs),
          IconButton(
            icon: const Icon(Icons.refresh, size: 20),
            tooltip: '刷新',
            onPressed: _loading ? null : _refreshAll,
          ),
          FilledButton.icon(
            onPressed: _taskBusy ? null : _generateReport,
            icon:
                _taskBusy
                    ? const SizedBox(
                      width: 16,
                      height: 16,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                    : const Icon(Icons.play_arrow, size: 18),
            label: Text(_taskBusy ? '统计中' : '生成报告'),
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
        await _refreshAll();
      },
    );
  }

  Widget _viewModeSegmented(ColorScheme cs) {
    return SegmentedButton<_ReportView>(
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
        Text('配置生成选项后开始统计', style: TextStyle(fontSize: 13, color: cs.outline)),
        const SizedBox(height: 20),
        FilledButton.icon(
          onPressed: _taskBusy ? null : _generateReport,
          icon:
              _taskBusy
                  ? const SizedBox(
                    width: 16,
                    height: 16,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                  : const Icon(Icons.play_arrow),
          label: Text(_taskBusy ? '统计中' : '生成报告'),
        ),
      ],
    ),
  );

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
              label: Text(_clearingDir ? '清除中…' : '删除此目录下所有重复数据'),
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
    final visibleTree = query.isEmpty
        ? _tree
        : _filterTreeNodes(_tree, query);
    return Column(
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(10, 8, 10, 2),
          child: TextField(
            controller: _treeSearchCtrl,
            onChanged: (v) => _onTreeSearch(v),
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
        final first = _firstMatch(_tree, q);
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
  DuplicateTreeNode? _firstMatch(List<DuplicateTreeNode> nodes, String q) {
    for (final node in nodes) {
      if (node.path.toLowerCase().contains(q) ||
          node.name.toLowerCase().contains(q)) {
        return node;
      }
      final child = _firstMatch(node.children, q);
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
              color:
                  searching && _isTreeMatch(node, query)
                      ? cs.primary
                      : null,
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
                    itemBuilder:
                        (context, index) =>
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

  Widget _buildGroupSection(ColorScheme cs, DuplicateGroupItem group) {
    final isSameDir = _summary?.isSameDir ?? false;
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
            showOtherPaths: !isSameDir,
            onTapMember: (item) => _showGroupDetail(group),
            shrinkWrap: true,
          ),
        ],
      ),
    );
  }

  Widget _buildGroupHeader(ColorScheme cs, DuplicateGroupItem group) {
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

  // ===== 成员网格与卡片 =====

  Widget _buildMemberGrid(
    List<DuplicateItem> items,
    ColorScheme cs, {
    bool showOtherPaths = false,
    String emptyText = '没有重复文件',
    ValueChanged<DuplicateItem>? onTapMember,
    bool shrinkWrap = false,
    bool showPathTooltip = false,
    void Function(List<int> deletedIds)? onDeleted,
  }) {
    if (items.isEmpty) {
      return Center(
        child: Text(emptyText, style: TextStyle(color: cs.outline)),
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
        final group = _groupOf(item);
        return DuplicateMemberCard(
          item: item,
          api: widget.api,
          sameDirMode: _summary?.isSameDir ?? false,
          showOtherPaths: showOtherPaths,
          group: group,
          onTap: onTapMember == null ? null : () => onTapMember(item),
          onError: (m) => setState(() => _error = m),
          onDeleted: onDeleted ?? (ids) => _removeMediaFromLocalState(ids),
          showPathTooltip: showPathTooltip,
        );
      },
    );
  }

  DuplicateGroupItem? _groupOf(DuplicateItem item) {
    return _groupByMediaId[item.id];
  }

  /// 目录树叠卡右键时使用的当前目录（用于“删除此目录外本组重复文件”）。
  String? _selectedDirForCluster() {
    final sel =
        _selectedDirectory ?? (_tree.isNotEmpty ? _tree.first.path : null);
    // 后端 relDir 把根目录表示为 "."，前端 _relDir 表示为 ""，这里统一成前端表示
    return sel;
  }

  /// 目录树视图（同目录模式）：按重复组聚合为 card_swiper 叠卡，
  /// 比普通缩略图占用更多宽度；点击展开全部，右键显示菜单。
  Widget _buildDirClusters(List<DuplicateItem> files, ColorScheme cs) {
    if (files.isEmpty) {
      return Center(
        child: Text('此目录没有直属重复文件', style: TextStyle(color: cs.outline)),
      );
    }
    final clusters = <int, List<DuplicateItem>>{};
    for (final item in files) {
      final g = _groupOf(item);
      if (g == null || g.items.length < 2) {
        continue; // 删除后已无重复的文件不再展示
      }
      // same_dir 报告只显示当前目录内仍构成重复的组；all 报告允许跨目录组
      // 在当前目录只剩一个成员时展示该成员，目录树已明确表示它属于重复组。
      clusters.putIfAbsent(g.id, () => []).add(item);
    }
    if (_summary?.isSameDir == true) {
      clusters.removeWhere((_, members) => members.length < 2);
    }
    if (clusters.isEmpty) {
      return Center(
        child: Text('此目录没有直属重复文件', style: TextStyle(color: cs.outline)),
      );
    }
    // 全部数据模式：卡片展示整组（跨目录）成员——数量徽标显示整组文件总数，
    // 叠卡预览整组成员（本目录成员优先，跨目录成员补充，最多 3 张）。
    final selected = _selectedDirForCluster();
    return SingleChildScrollView(
      padding: const EdgeInsets.all(16),
      child: Wrap(
        spacing: 20,
        runSpacing: 20,
        children: [
          for (final entry in clusters.entries)
            StackedCluster(
              items: _clusterItemsFor(entry.value, selected),
              api: widget.api,
              width: 300,
              onTap: () {
                final g = _groupOf(entry.value.first);
                if (g != null) _showGroupDetail(g);
              },
              onSecondaryTap:
                  (pos) => _showMemberMenu(
                    pos,
                    entry.value.first,
                    _groupOf(entry.value.first),
                    directoryScope: _selectedDirForCluster(),
                  ),
            ),
        ],
      ),
    );
  }

  /// 目录树叠卡的展示成员：同目录模式直接用本目录成员；
  /// 全部数据模式用整组成员（本目录成员排前，其余目录成员排后），
  /// 使数量徽标与叠卡反映整组重复文件总数。
  List<DuplicateItem> _clusterItemsFor(
    List<DuplicateItem> localMembers,
    String? selected,
  ) {
    if (_summary?.isSameDir == true || selected == null) {
      return localMembers;
    }
    final g = _groupOf(localMembers.first);
    if (g == null) {
      return localMembers;
    }
    final local = <DuplicateItem>[];
    final rest = <DuplicateItem>[];
    for (final m in g.items) {
      (relDir(m.relativePath) == selected ? local : rest).add(m);
    }
    return [...local, ...rest];
  }

  /// 叠卡/缩略图的右键菜单（按模式区分）。
  /// directoryScope 非空时（目录树视图），“删除”项变为“删除此目录外本组重复文件”。
  void _showMemberMenu(
    Offset position,
    DuplicateItem item,
    DuplicateGroupItem? group, {
    String? directoryScope,
  }) {
    final sameDir = _summary?.isSameDir ?? false;
    // 目录树视图下，跨目录组才显示“删除此目录外本组重复文件”
    final hasOtherDirMembers =
        group != null &&
        group.items.any(
          (m) => relDir(m.relativePath) != relDir(item.relativePath),
        );
    final deleteLabel =
        directoryScope != null && hasOtherDirMembers
            ? '删除此目录外本组重复文件'
            : '删除此文件外本组重复文件';
    if (sameDir) {
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
          const ContextMenuItem.divider(),
          ContextMenuItem(
            icon: Icons.block,
            label: '排除重复',
            onTap: () => _excludeMedia(item.id),
          ),
          if (group != null && group.items.length > 1)
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
      );
      return;
    }
    // 全部数据模式：列出其它目录中的重复路径（二级菜单）。
    // 菜单项按父路径去重：同一父目录只保留一个代表项，标签显示父路径。
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

  Future<void> _openMedia(int mediaId, bool directory) async {
    try {
      directory
          ? await widget.api.openMediaDirectory(mediaId)
          : await widget.api.openMediaFile(mediaId);
    } catch (e) {
      if (mounted) setState(() => _error = '打开失败: $e');
    }
  }

  /// 排除重复：人工判定该文件无重复，将其从当前报告中移除。
  /// 仅对当前报告生效——重新生成报告后该文件重新参与检测。
  Future<void> _excludeMedia(int mediaId) async {
    try {
      await widget.api.excludeDuplicateMedia(mediaId);
      // 乐观更新：立即从本地状态移除该文件（组员 <2 的组自动丢弃）
      _removeMediaFromLocalState([mediaId]);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text('已排除该文件：不再计入当前报告的重复统计，重新生成报告后恢复参与检测'),
          ),
        );
      }
      await _refreshAll();
    } catch (e) {
      if (mounted) setState(() => _error = '排除重复失败: $e');
    }
  }

  /// 删除此文件外本组重复文件（含缩略图/media 记录/本地文件）。
  /// 目录树视图传 directoryScope 时只删除**该目录之外**的组成员，
  /// 保留当前目录内的全部成员；不传则删除组内除当前文件外的所有成员。
  Future<void> _deleteOthers(
    DuplicateGroupItem group,
    int exceptId, {
    String? directoryScope,
  }) async {
    final List<int> others;
    String title;
    if (directoryScope != null) {
      // 目录树视图：只删其它目录的成员，保留当前目录的全部成员
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
      // 乐观更新：立即从本地状态移除已删成员，避免等 _refreshAll 期间 UI 仍显示旧卡片
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
      await _refreshAll();
    } catch (e) {
      if (mounted) setState(() => _error = '删除失败: $e');
    }
  }

  /// 乐观移除已删除的 media id：同步更新 _allGroups / _groupByMediaId / _page，
  /// 使下一次 build 立即反映删除结果，不必等待后端刷新。
  void _removeMediaFromLocalState(List<int> deletedIds) {
    if (deletedIds.isEmpty) return;
    final idSet = deletedIds.toSet();
    setState(() {
      // 1. 从 _groupByMediaId 移除
      _groupByMediaId.removeWhere((id, _) => idSet.contains(id));
      // 2. 从 _allGroups 的 items 中移除，并丢弃成员 <2 的组
      final updated = <DuplicateGroupItem>[];
      for (final g in _allGroups) {
        final remaining = g.items.where((m) => !idSet.contains(m.id)).toList();
        if (remaining.length >= 2) {
          updated.add(
            DuplicateGroupItem(
              id: g.id,
              groupType: g.groupType,
              directory: g.directory,
              memberCount: remaining.length,
              freedBytes: g.freedBytes,
              items: remaining,
            ),
          );
        }
      }
      _allGroups = updated;
      // 3. 从当前分页 _page 中同样移除
      if (_page != null) {
        final pageItems = <DuplicateGroupItem>[];
        for (final g in _page!.items) {
          final remaining =
              g.items.where((m) => !idSet.contains(m.id)).toList();
          if (remaining.length >= 2) {
            pageItems.add(
              DuplicateGroupItem(
                id: g.id,
                groupType: g.groupType,
                directory: g.directory,
                memberCount: remaining.length,
                freedBytes: g.freedBytes,
                items: remaining,
              ),
            );
          }
        }
        _page = DuplicateGroupPage(
          total: pageItems.length,
          page: _page!.page,
          pageSize: _page!.pageSize,
          totalPages:
              pageItems.isEmpty
                  ? 1
                  : (pageItems.length / _page!.pageSize).ceil(),
          items: pageItems,
        );
      }
    });
  }

  /// 展示某组全部重复缩略图；展开状态下可右键删除此文件外本组/本目录重复文件。
  void _showGroupDetail(DuplicateGroupItem group) {
    final isSameDir = _summary?.isSameDir ?? false;
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
                        showOtherPaths: !isSameDir,
                        emptyText: '无缩略图',
                        showPathTooltip: true,
                        onDeleted: (ids) {
                          // 详情弹窗内删除后关闭弹窗并刷新页面，避免残留已无重复的图片
                          Navigator.of(ctx).pop();
                          _removeMediaFromLocalState(ids);
                        },
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ),
    );
  }

  // ===== 删除 / 清除 =====

  Future<void> _clearDirectory(String dir) async {
    setState(() => _clearingDir = true);
    try {
      final isSameDir = _summary?.isSameDir ?? false;
      final dirItems =
          _allGroups
              .expand((g) => g.items)
              .where((m) => relDir(m.relativePath) == dir)
              .toList();
      final keep = await showKeepDialog(
        context,
        title: '删除此目录下所有重复数据',
        count: dirItems.length,
      );
      if (keep == null || !mounted) return;
      final ok = await confirmClearDialog(
        context,
        title: '删除此目录下所有重复数据',
        keep: keep,
        count: dirItems.length,
      );
      if (!ok || !mounted) return;
      // 乐观更新：立即从本地状态移除已删成员，避免等 _refreshAll 期间 UI 仍显示旧卡片
      int deletedFiles;
      int freedBytes;
      if (isSameDir) {
        final result = await widget.api.clearDuplicates(
          scope: 'directory',
          keep: keep,
          directory: dir,
        );
        deletedFiles = result.deletedFiles;
        freedBytes = result.freedBytes;
        // same_dir 模式后端按目录清理，本地无法精确知道删了哪些 id，直接全量刷新
      } else {
        // all 模式下 _clearDirMembers 删除的是本目录内每组除保留项外的成员
        final deletedIds = _computeDirDeletedIds(dirItems, keep);
        final result = await _clearDirMembers(dirItems, keep);
        _removeMediaFromLocalState(deletedIds);
        deletedFiles = result.deletedFiles;
        freedBytes = result.freedBytes;
      }
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              '已删除 $deletedFiles 个文件，释放 ${formatBytes(freedBytes)}',
            ),
          ),
        );
      }
      await _refreshAll();
    } catch (e) {
      if (mounted) setState(() => _error = '清除失败: $e');
    } finally {
      if (mounted) setState(() => _clearingDir = false);
    }
  }

  /// 全部数据模式：删除本目录下所有重复数据——本目录内互相重复的组
  /// 按保留条件保留 1 个，仅跨目录重复的本目录成员直接删除（其它目录不动）。
  Future<DeleteResult> _clearDirMembers(
    List<DuplicateItem> dirItems,
    String keep,
  ) async {
    final toDelete = _computeDirDeletedIds(dirItems, keep);
    if (toDelete.isEmpty) {
      return DeleteResult();
    }
    return widget.api.deleteMedia(toDelete);
  }

  /// 计算本目录内每组按保留条件应删除的 media id 列表（与 _clearDirMembers 一致）。
  /// 语义：删除本目录下所有重复数据——组内本目录成员 >=2 时按保留条件保留 1 个删其余；
  /// 组内本目录成员 ==1（仅与其它目录文件重复）时直接删除本目录这一份；绝不删除其它目录文件。
  List<int> _computeDirDeletedIds(List<DuplicateItem> dirItems, String keep) {
    final byGroup = <int, List<DuplicateItem>>{};
    for (final item in dirItems) {
      final g = _groupOf(item);
      if (g == null) continue;
      byGroup.putIfAbsent(g.id, () => []).add(item);
    }
    final toDelete = <int>[];
    for (final members in byGroup.values) {
      if (members.isEmpty) continue;
      if (members.length < 2) {
        toDelete.add(members.first.id);
        continue;
      }
      final keepIdx = pickKeepIndex(members, keep);
      for (var i = 0; i < members.length; i++) {
        if (i != keepIdx) toDelete.add(members[i].id);
      }
    }
    return toDelete;
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
      final result = await widget.api.clearDuplicates(
        scope: 'page',
        keep: keep,
        groupIds: groupIds,
      );
      // 乐观更新：立即从本地状态移除本页被删成员
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
      await _refreshAll();
    } catch (e) {
      if (mounted) setState(() => _error = '清除失败: $e');
    }
  }

  // ===== 选项与保留条件对话框 =====

  Future<ReportOptions?> _showOptionsDialog() async {
    final base = _summary;
    // 优先使用后端配置默认值，保证与检测结果一致
    Map<String, dynamic> defaults = {};
    try {
      defaults = await widget.api.getReportDefaults();
    } catch (_) {
      // 获取默认值失败时回退到报告/内置默认
    }
    if (!mounted) return null;
    final options = ReportOptions(
      scope: base?.isSameDir == true ? 'same_dir' : 'all',
      mediaType: base?.mediaType ?? 'all',
      imageThreshold:
          (defaults['image_threshold'] as num?)?.toInt() ??
          base?.imageThreshold ??
          90,
      videoPhashDistance:
          (defaults['video_phash_distance'] as num?)?.toInt() ??
          base?.videoPhashDistance ??
          12,
      videoDurationDiffMs:
          (defaults['video_duration_diff_ms'] as num?)?.toInt() ??
          base?.videoDurationDiffMs ??
          3000,
      oshashFilter:
          defaults['oshash_filter'] as bool? ?? base?.oshashFilter ?? true,
      includeSha1:
          defaults['include_sha1'] as bool? ?? base?.includeSha1 ?? true,
    );
    final phashCtrl = TextEditingController(
      text: '${options.videoPhashDistance}',
    );
    final durCtrl = TextEditingController(
      text: '${options.videoDurationDiffMs}',
    );

    final result = await showDialog<bool>(
      context: context,
      builder:
          (ctx) => StatefulBuilder(
            builder:
                (ctx, setLocal) => AlertDialog(
                  title: const Text('生成重复报告'),
                  content: SizedBox(
                    width: 460,
                    child: SingleChildScrollView(
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          const Text('比较范围', style: TextStyle(fontSize: 13)),
                          const SizedBox(height: 4),
                          SegmentedButton<String>(
                            segments: const [
                              ButtonSegment(value: 'all', label: Text('全部数据')),
                              ButtonSegment(
                                value: 'same_dir',
                                label: Text('仅同一目录'),
                              ),
                            ],
                            selected: {options.scope},
                            onSelectionChanged:
                                (v) => setLocal(() => options.scope = v.first),
                          ),
                          const SizedBox(height: 16),
                          const Text('媒体类型', style: TextStyle(fontSize: 13)),
                          const SizedBox(height: 4),
                          SegmentedButton<String>(
                            segments: const [
                              ButtonSegment(value: 'all', label: Text('全部')),
                              ButtonSegment(value: 'image', label: Text('图片')),
                              ButtonSegment(value: 'video', label: Text('视频')),
                            ],
                            selected: {options.mediaType},
                            onSelectionChanged:
                                (v) =>
                                    setLocal(() => options.mediaType = v.first),
                          ),
                          const SizedBox(height: 16),
                          Text(
                            '图片相似度阈值：${options.imageThreshold}',
                            style: const TextStyle(fontSize: 13),
                          ),
                          Slider(
                            value: options.imageThreshold.toDouble(),
                            min: 0,
                            max: 100,
                            divisions: 100,
                            label: '${options.imageThreshold}',
                            onChanged:
                                (v) => setLocal(
                                  () => options.imageThreshold = v.round(),
                                ),
                          ),
                          const Divider(height: 24),
                          const Text(
                            '视频选项',
                            style: TextStyle(
                              fontSize: 13,
                              fontWeight: FontWeight.w600,
                            ),
                          ),
                          const SizedBox(height: 8),
                          TextField(
                            controller: phashCtrl,
                            keyboardType: TextInputType.number,
                            decoration: const InputDecoration(
                              labelText: 'sprite pHash 最大 Hamming 距离',
                              isDense: true,
                            ),
                          ),
                          const SizedBox(height: 10),
                          TextField(
                            controller: durCtrl,
                            keyboardType: TextInputType.number,
                            decoration: const InputDecoration(
                              labelText: '允许时长差（毫秒）',
                              isDense: true,
                            ),
                          ),
                          const SizedBox(height: 10),
                          SwitchListTile(
                            contentPadding: EdgeInsets.zero,
                            title: const Text(
                              'oshash 粗筛',
                              style: TextStyle(fontSize: 13),
                            ),
                            subtitle: const Text(
                              '开启后按 oshash 预分组加速，不影响召回',
                              style: TextStyle(fontSize: 11),
                            ),
                            value: options.oshashFilter,
                            onChanged:
                                (v) => setLocal(() => options.oshashFilter = v),
                          ),
                          SwitchListTile(
                            contentPadding: EdgeInsets.zero,
                            title: const Text(
                              '包含 SHA1 完全相同结果',
                              style: TextStyle(fontSize: 13),
                            ),
                            value: options.includeSha1,
                            onChanged:
                                (v) => setLocal(() => options.includeSha1 = v),
                          ),
                        ],
                      ),
                    ),
                  ),
                  actions: [
                    TextButton(
                      onPressed: () => Navigator.of(ctx).pop(false),
                      child: const Text('取消'),
                    ),
                    FilledButton(
                      onPressed: () {
                        options.videoPhashDistance =
                            int.tryParse(phashCtrl.text) ??
                            options.videoPhashDistance;
                        options.videoDurationDiffMs =
                            int.tryParse(durCtrl.text) ??
                            options.videoDurationDiffMs;
                        Navigator.of(ctx).pop(true);
                      },
                      child: const Text('生成'),
                    ),
                  ],
                ),
          ),
    );
    phashCtrl.dispose();
    durCtrl.dispose();
    return result == true ? options : null;
  }

}


