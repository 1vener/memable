// library_tab.dart：首页“库”功能页（翻页/懒加载双模式 + 文件搜索）。
// 触底懒加载代码自 dashboard_screen.dart 迁入并保留（懒加载模式），
// 新增翻页模式（页码分页、页大小可配置）与文件搜索（命中文件所属
// 分组整组返回并高亮命中项）。
import 'dart:async';

import 'package:flutter/material.dart';

import '../models/models.dart';
import '../services/api_service.dart';
import '../services/display_preferences.dart';
import '../widgets/masonry_sliver.dart';
import '../widgets/media_common.dart';

/// 库页：按目录分组展示全部正式媒体。
class LibraryTab extends StatefulWidget {
  final ApiService api;
  final VoidCallback? onOpenScan;

  const LibraryTab({super.key, required this.api, this.onOpenScan});

  @override
  State<LibraryTab> createState() => _LibraryTabState();
}

class _LibraryTabState extends State<LibraryTab> {
  final _libraryScroll = ScrollController();
  final TextEditingController _searchCtrl = TextEditingController();
  final TextEditingController _pageCtrl = TextEditingController(text: '1');
  Timer? _searchDebounce;
  List<MediaGroup> _groups = [];
  int _groupTotal = 0;
  bool _groupsLoading = false;
  int _loadedDepth = 3;
  int _loadedPageSize = 20;
  String _loadedMode = 'page';
  int _groupPageNo = 1;
  String _groupQuery = '';

  ApiService get api => widget.api;

  int get _groupPageSize => displayPreferences.libraryGroupPageSize;
  String get _loadMode => displayPreferences.libraryLoadMode;
  int get _totalPages =>
      _groupPageSize <= 0
          ? 1
          : ((_groupTotal + _groupPageSize - 1) ~/ _groupPageSize);

  @override
  void initState() {
    super.initState();
    displayPreferences.addListener(_preferencesChanged);
    _initializeGroups();
  }

  Future<void> _initializeGroups() async {
    await displayPreferences.load();
    _loadedDepth = displayPreferences.libraryGroupDepth;
    _loadedPageSize = displayPreferences.libraryGroupPageSize;
    _loadedMode = displayPreferences.libraryLoadMode;
    await _loadGroups(refresh: true);
  }

  void _preferencesChanged() {
    if (!mounted) return;
    final depth = displayPreferences.libraryGroupDepth;
    final depthChanged = _loadedDepth != depth;
    final sizeChanged =
        _loadedPageSize != displayPreferences.libraryGroupPageSize;
    final modeChanged = _loadedMode != displayPreferences.libraryLoadMode;
    if (depthChanged) _loadedDepth = depth;
    if (sizeChanged) _loadedPageSize = displayPreferences.libraryGroupPageSize;
    if (modeChanged) _loadedMode = displayPreferences.libraryLoadMode;
    setState(() {});
    if (depthChanged || sizeChanged || modeChanged) {
      _groupPageNo = 1;
      _loadGroups(refresh: true);
    }
  }

  @override
  void dispose() {
    displayPreferences.removeListener(_preferencesChanged);
    _searchDebounce?.cancel();
    _searchCtrl.dispose();
    _libraryScroll.dispose();
    super.dispose();
  }

  /// 加载分组：懒加载模式追加批次；翻页模式替换当前页。
  Future<void> _loadGroups({bool refresh = false}) async {
    if (_groupsLoading) return;
    final mode = _loadMode;
    if (mode == 'lazy') {
      if (!refresh && _groups.length >= _groupTotal && _groupTotal != 0) return;
    }
    setState(() => _groupsLoading = true);
    try {
      final result = await api.getMediaGroups(
        displayPreferences.libraryGroupDepth,
        refresh || mode == 'page'
            ? (_groupPageNo - 1) * _groupPageSize
            : _groups.length,
        mode == 'lazy' ? 20 : _groupPageSize,
        query: _groupQuery.isEmpty ? null : _groupQuery,
      );
      if (!mounted) return;
      setState(() {
        if (refresh || mode == 'page') _groups = [];
        _groupTotal = result.total;
        _groups.addAll(result.items);
      });
      if (mode == 'page') {
        _pageCtrl.text = '$_groupPageNo';
        if (_libraryScroll.hasClients) {
          // 翻页后回到顶部
          WidgetsBinding.instance.addPostFrameCallback((_) {
            if (mounted && _libraryScroll.hasClients) {
              _libraryScroll.jumpTo(0);
            }
          });
        }
      }
    } catch (e) {
      if (mounted) _showError(e);
    } finally {
      if (mounted) setState(() => _groupsLoading = false);
    }
  }

  /// 触底懒加载（仅懒加载模式保留）：停止滚动后接近底部时追加下一批。
  /// 翻页模式不响应触底（否则下拉到底会触发重载并回到顶部）。
  bool _onLibraryScroll(ScrollNotification notification) {
    if (_loadMode != 'lazy') return false;
    if (notification is ScrollEndNotification &&
        notification.metrics.extentAfter < 800) {
      _loadGroups();
    }
    return false;
  }

  void _goToPage(int page) {
    final max = _totalPages < 1 ? 1 : _totalPages;
    _groupPageNo = page.clamp(1, max);
    _pageCtrl.text = '$_groupPageNo';
    _loadGroups(refresh: true);
  }

  /// 搜索输入：防抖 350ms；清空立即复位。
  void _onSearchChanged(String text) {
    _searchDebounce?.cancel();
    final q = text.trim();
    if (q.isEmpty) {
      _clearSearch();
      return;
    }
    _searchDebounce = Timer(const Duration(milliseconds: 350), () {
      _runSearch(q);
    });
  }

  Future<void> _runSearch(String q) async {
    if (_groupQuery == q) return;
    _groupQuery = q;
    _groupPageNo = 1;
    await _loadGroups(refresh: true);
  }

  void _clearSearch() {
    _searchDebounce?.cancel();
    _searchCtrl.clear();
    if (_groupQuery.isEmpty) return;
    _groupQuery = '';
    _groupPageNo = 1;
    _loadGroups(refresh: true);
  }

  void _onLoadModeChanged(String mode) {
    // 偏好变化经 _preferencesChanged 自动重置并重载
    displayPreferences.setLibraryLoadMode(mode);
  }

  void _onPageSizeChanged(int size) {
    displayPreferences.setLibraryGroupPageSize(size);
  }

  void _showError(Object e) =>
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('$e')));

  void _openViewer(List<Media> items, int index) {
    openMediaViewer(context, api, items, index);
  }

  /// 删除媒体后从本地分组列表移除（总数角标保留服务端快照，下次刷新校正）。
  void _removeFromGroups(Media media) {
    setState(() {
      for (final group in _groups) {
        group.items.removeWhere((m) => m.id == media.id);
      }
    });
  }

  bool _isHit(Media media) {
    if (_groupQuery.isEmpty) return false;
    return media.relativePath.toLowerCase().contains(_groupQuery.toLowerCase());
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final pageMode = _loadMode == 'page';
    return Column(
      children: [
        DisplayToolbar(
          layout: displayPreferences.libraryLayout,
          onLayoutChanged: displayPreferences.setLibraryLayout,
          thumbExtent: displayPreferences.libraryThumbExtent,
          onThumbExtentChanged: displayPreferences.setLibraryThumbExtent,
          pageSize: _groupPageSize,
          onPageSizeChanged: _onPageSizeChanged,
          searchQuery: _groupQuery,
          searchController: _searchCtrl,
          onSearchChanged: _onSearchChanged,
          onSearchClear: _clearSearch,
          loadMode: _loadMode,
          onLoadModeChanged: _onLoadModeChanged,
        ),
        Expanded(
          child: RefreshIndicator(
            onRefresh: () {
              _groupPageNo = 1;
              return _loadGroups(refresh: true);
            },
            child: ScrollConfiguration(
              // 桌面端 ScrollBehavior 会自动注入滚动条；此处关闭自动注入，
              // 只保留下面这一条显式滚动条，避免双滚动条争用同一位置。
              behavior: ScrollConfiguration.of(
                context,
              ).copyWith(scrollbars: false),
              child: NotificationListener<ScrollNotification>(
                onNotification: _onLibraryScroll,
                child: Scrollbar(
                  controller: _libraryScroll,
                  thumbVisibility: false,
                  thickness: 6,
                  radius: const Radius.circular(3),
                  child: CustomScrollView(
                    controller: _libraryScroll,
                    physics: const AlwaysScrollableScrollPhysics(),
                    slivers: [
                      const SliverPadding(padding: EdgeInsets.only(top: 22)),
                      for (int i = 0; i < _groups.length; i++) ...[
                        SliverToBoxAdapter(
                          child: Padding(
                            padding: const EdgeInsets.fromLTRB(24, 4, 24, 13),
                            child: Row(
                              children: [
                                Container(
                                  width: 24,
                                  height: 24,
                                  margin: const EdgeInsets.only(right: 9),
                                  decoration: BoxDecoration(
                                    color: cs.primary.withValues(alpha: .1),
                                    borderRadius: BorderRadius.circular(7),
                                  ),
                                  child: Icon(
                                    Icons.folder_rounded,
                                    size: 15,
                                    color: cs.primary,
                                  ),
                                ),
                                Expanded(
                                  child: Text(
                                    '${_groups[i].libraryName}${_groups[i].groupPath.isEmpty ? '' : ' / ${_groups[i].groupPath}'}',
                                    maxLines: 1,
                                    overflow: TextOverflow.ellipsis,
                                    style: Theme.of(
                                      context,
                                    ).textTheme.titleSmall?.copyWith(
                                      fontWeight: FontWeight.w700,
                                      fontSize: 14,
                                    ),
                                  ),
                                ),
                                Text(
                                  '${_groups[i].total} 项',
                                  style: Theme.of(context).textTheme.labelSmall
                                      ?.copyWith(color: cs.outline),
                                ),
                              ],
                            ),
                          ),
                        ),
                        _buildLibraryGroup(_groups[i]),
                        const SliverPadding(
                          padding: EdgeInsets.only(bottom: 30),
                        ),
                      ],
                      if (_groups.isEmpty && _groupsLoading)
                        SliverPadding(
                          padding: const EdgeInsets.fromLTRB(24, 4, 24, 0),
                          sliver: SliverGrid.builder(
                            itemCount: 18,
                            gridDelegate:
                                SliverGridDelegateWithMaxCrossAxisExtent(
                                  maxCrossAxisExtent:
                                      displayPreferences.libraryThumbExtent,
                                  crossAxisSpacing: kMediaCrossSpacing,
                                  mainAxisSpacing: kMediaMainSpacing,
                                  childAspectRatio: 0.8,
                                ),
                            itemBuilder: (_, __) => const SkeletonBlock(),
                          ),
                        ),
                      if (!_groupsLoading && _groups.isEmpty)
                        SliverFillRemaining(
                          hasScrollBody: false,
                          child: Center(
                            child: Column(
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                Icon(
                                  _groupQuery.isEmpty
                                      ? Icons.photo_library_outlined
                                      : Icons.search_off_outlined,
                                  size: 72,
                                  color: cs.outlineVariant,
                                ),
                                const SizedBox(height: 16),
                                Text(
                                  _groupQuery.isEmpty ? '暂无已入库媒体' : '无匹配分组',
                                  style: Theme.of(context).textTheme.titleMedium
                                      ?.copyWith(fontWeight: FontWeight.w600),
                                ),
                                const SizedBox(height: 8),
                                Text(
                                  _groupQuery.isEmpty
                                      ? '添加收藏库并完成扫描后，这里会展示你的照片与视频'
                                      : '没有文件名包含「$_groupQuery」的媒体',
                                  style: Theme.of(context).textTheme.bodySmall
                                      ?.copyWith(color: cs.onSurfaceVariant),
                                ),
                                if (_groupQuery.isEmpty) ...[
                                  const SizedBox(height: 20),
                                  FilledButton.icon(
                                    onPressed: widget.onOpenScan,
                                    icon: const Icon(Icons.manage_search),
                                    label: const Text('前往扫描'),
                                  ),
                                ],
                              ],
                            ),
                          ),
                        ),
                      const SliverPadding(padding: EdgeInsets.only(bottom: 32)),
                    ],
                  ),
                ),
              ),
            ),
          ),
        ),
        if (pageMode)
          PaginationBar(
            currentPage: _groupPageNo,
            totalPages: _totalPages,
            totalItems: _groupTotal,
            loading: _groupsLoading,
            controller: _pageCtrl,
            onPrevious:
                _groupPageNo > 1 ? () => _goToPage(_groupPageNo - 1) : null,
            onNext:
                _groupPageNo < _totalPages
                    ? () => _goToPage(_groupPageNo + 1)
                    : null,
            onSubmit: () => _goToPage(int.tryParse(_pageCtrl.text) ?? 1),
          ),
      ],
    );
  }

  Widget _buildLibraryGroup(MediaGroup group) {
    final extent = displayPreferences.libraryThumbExtent;
    final layout = displayPreferences.libraryLayout;
    if (layout == 'masonry') {
      // 瀑布流：自实现 sliver（flutter_staggered_grid_view 的
      // SliverMasonryGrid 在内容追加时会拉回滚动位置）。
      return SliverPadding(
        padding: const EdgeInsets.fromLTRB(24, 0, 24, 0),
        sliver: SliverLayoutBuilder(
          builder: (context, constraints) {
            final columns = columnCount(constraints.crossAxisExtent, extent);
            return MasonrySliver(
              rebuildToken: group,
              childCount: group.items.length,
              itemBuilder: (context, index) {
                final media = group.items[index];
                return RepaintBoundary(
                  child: MediaTile(
                    media: media,
                    api: api,
                    imageAspectRatio: rawAspectRatio(media),
                    showName: false,
                    highlighted: _isHit(media),
                    onOpenViewer: () => _openViewer(group.items, index),
                    onDeleted: () => _removeFromGroups(media),
                  ),
                );
              },
              crossAxisCount: columns,
              crossAxisSpacing: kMediaCrossSpacing,
              mainAxisSpacing: kMediaMainSpacing,
              heightFactor: (i) => 1 / rawAspectRatio(group.items[i]),
            );
          },
        ),
      );
    }

    if (layout == 'justified') {
      // 自适应：按原始宽高比横向铺满整行，无空隙（Google Photos 风格）
      return SliverPadding(
        padding: const EdgeInsets.fromLTRB(24, 0, 24, 0),
        sliver: SliverLayoutBuilder(
          builder: (context, constraints) {
            final width = constraints.crossAxisExtent;
            final targetH = extent * 0.85;
            const gap = kMediaJustifiedGap;
            final rows = buildJustifiedRows(group.items, width, targetH, gap);
            return SliverList.builder(
              itemCount: rows.length,
              itemBuilder: (context, rowIndex) {
                final row = rows[rowIndex];
                final rowH = justifiedRowHeight(row, width, targetH, gap);
                final widths = justifiedCellWidths(row, rowH, width, gap);
                return Padding(
                  padding: const EdgeInsets.only(
                    bottom: kMediaJustifiedMainGap,
                  ),
                  child: Row(
                    children: [
                      for (var c = 0; c < row.length; c++) ...[
                        if (c > 0) const SizedBox(width: gap),
                        SizedBox(
                          width: widths[c],
                          child: MediaTile(
                            media: row[c].$1,
                            api: api,
                            imageAspectRatio: rawAspectRatio(row[c].$1),
                            showName: false,
                            imageFit:
                                row.length == 1 ? BoxFit.contain : BoxFit.cover,
                            cellWidth: widths[c],
                            cellHeight: rowH,
                            highlighted: _isHit(row[c].$1),
                            onOpenViewer:
                                () => _openViewer(group.items, row[c].$2),
                            onDeleted: () => _removeFromGroups(row[c].$1),
                          ),
                        ),
                      ],
                    ],
                  ),
                );
              },
            );
          },
        ),
      );
    }

    return SliverPadding(
      padding: const EdgeInsets.fromLTRB(24, 0, 24, 0),
      sliver: SliverLayoutBuilder(
        builder: (context, constraints) {
          final columns = columnCount(constraints.crossAxisExtent, extent);
          final width =
              (constraints.crossAxisExtent -
                  (columns - 1) * kMediaCrossSpacing) /
              columns;
          final imageRatio = layout == 'square' ? 1.0 : 4 / 3;
          // 网格正切：正方形单元，图片裁剪填充不留空隙；自适应网格保留完整画面
          final imageFit = layout == 'square' ? BoxFit.cover : BoxFit.contain;
          return SliverGrid.builder(
            itemCount: group.items.length,
            gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
              crossAxisCount: columns,
              crossAxisSpacing: kMediaCrossSpacing,
              mainAxisSpacing: kMediaMainSpacing,
              mainAxisExtent: width / imageRatio,
            ),
            itemBuilder: (context, index) {
              final media = group.items[index];
              return RepaintBoundary(
                child: MediaTile(
                  media: media,
                  api: api,
                  imageAspectRatio: imageRatio,
                  imageFit: imageFit,
                  showName: false,
                  highlighted: _isHit(media),
                  onOpenViewer: () => _openViewer(group.items, index),
                  onDeleted: () => _removeFromGroups(media),
                ),
              );
            },
          );
        },
      ),
    );
  }
}
