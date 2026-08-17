// dashboard_screen.dart：首页媒体浏览、分页列表和统计。
import 'package:fl_chart/fl_chart.dart';
import 'package:flutter/material.dart';

import '../models/models.dart';
import '../services/api_service.dart';
import '../services/display_preferences.dart';
import '../widgets/masonry_sliver.dart';
import '../widgets/media_common.dart';
import 'library_tab.dart';

/// 媒体网格间隔常量：统一调整图片间距时只需修改这里。
const double kMediaCrossSpacing = 2; // 列间距（网格/瀑布流/骨架屏）
const double kMediaMainSpacing = 2; // 行间距（网格/瀑布流/骨架屏）
const double kMediaJustifiedGap = 2; // 自适应（justified）布局行内间隙
const double kMediaJustifiedMainGap = 2; // 自适应（justified）布局行间距

class DashboardScreen extends StatefulWidget {
  final ApiService api;
  final ValueChanged<String>? onTabChanged;
  final String activeTab;
  final VoidCallback? onOpenScan;

  const DashboardScreen({
    super.key,
    required this.api,
    this.onTabChanged,
    this.activeTab = 'library',
    this.onOpenScan,
  });

  @override
  State<DashboardScreen> createState() => _DashboardScreenState();
}

class _DashboardScreenState extends State<DashboardScreen> {
  MediaStatistics? _statistics;

  @override
  void initState() {
    super.initState();
    _loadStatistics();
  }

  Future<void> _loadStatistics() async {
    try {
      final value = await widget.api.getMediaStatistics();
      if (mounted) setState(() => _statistics = value);
    } catch (e) {
      if (mounted) _showError(e);
    }
  }

  void _showError(Object e) =>
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('$e')));

  /// 当前功能对应的 IndexedStack 索引（四个子视图常驻，切换不丢状态）。
  int get _tabIndex => switch (widget.activeTab) {
    'video' => 1,
    'image' || 'photo' || 'photos' => 2,
    'statistics' || 'stats' => 3,
    _ => 0,
  };

  @override
  Widget build(BuildContext context) {
    return IndexedStack(
      index: _tabIndex,
      children: [
        // 库页（翻页/懒加载双模式 + 搜索）在 library_tab.dart
        LibraryTab(api: widget.api, onOpenScan: widget.onOpenScan),
        _PagedMediaTab(
          key: const ValueKey('video'),
          api: widget.api,
          kind: 'video',
          prefs: displayPreferences,
        ),
        _PagedMediaTab(
          key: const ValueKey('image'),
          api: widget.api,
          kind: 'image',
          prefs: displayPreferences,
        ),
        _buildStatisticsTab(),
      ],
    );
  }

  Widget _buildStatisticsTab() {
    final s = _statistics;
    if (s == null) {
      return Center(
        child: FilledButton.icon(
          onPressed: _loadStatistics,
          icon: const Icon(Icons.refresh),
          label: const Text('加载统计'),
        ),
      );
    }
    final totalCount = s.image.count + s.video.count;
    final imageSizeRatio = s.totalSize == 0 ? 0.0 : s.image.size / s.totalSize;
    final videoSizeRatio = s.totalSize == 0 ? 0.0 : s.video.size / s.totalSize;
    return RefreshIndicator(
      onRefresh: _loadStatistics,
      child: ListView(
        physics: const AlwaysScrollableScrollPhysics(),
        padding: const EdgeInsets.fromLTRB(28, 32, 28, 48),
        children: [
          Text(
            formatBytes(s.totalSize),
            style: Theme.of(context).textTheme.displaySmall?.copyWith(
              fontWeight: FontWeight.w800,
              color: Theme.of(context).colorScheme.onSurface,
            ),
          ),
          const SizedBox(height: 4),
          Text(
            '媒体总量 · $totalCount 个文件',
            style: Theme.of(context).textTheme.bodyMedium?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 32),
          LayoutBuilder(
            builder: (context, constraints) {
              final width = (constraints.maxWidth - 32) / 3;
              return Wrap(
                spacing: 16,
                runSpacing: 24,
                children: [
                  _Metric(
                    width: width.clamp(180, 420),
                    label: '照片',
                    value: '${s.image.count}',
                    detail: formatBytes(s.image.size),
                    icon: Icons.image_outlined,
                  ),
                  _Metric(
                    width: width.clamp(180, 420),
                    label: '视频',
                    value: '${s.video.count}',
                    detail: formatBytes(s.video.size),
                    icon: Icons.movie_outlined,
                  ),
                  _Metric(
                    width: width.clamp(180, 420),
                    label: '视频时长',
                    value: formatDuration(s.video.durationMs),
                    detail: '全部已入库视频',
                    icon: Icons.schedule_outlined,
                  ),
                ],
              );
            },
          ),
          const SizedBox(height: 32),
          SizedBox(
            height: 200,
            child:
                s.totalSize == 0
                    ? Center(
                      child: Icon(
                        Icons.pie_chart_outline,
                        size: 72,
                        color: Theme.of(context).colorScheme.outlineVariant,
                      ),
                    )
                    : Stack(
                      alignment: Alignment.center,
                      children: [
                        PieChart(
                          PieChartData(
                            sectionsSpace: 2,
                            centerSpaceRadius: 62,
                            startDegreeOffset: -90,
                            borderData: FlBorderData(show: false),
                            sections: [
                              PieChartSectionData(
                                value: s.video.size.toDouble(),
                                color: Theme.of(context).colorScheme.primary,
                                showTitle: false,
                                radius: 46,
                              ),
                              PieChartSectionData(
                                value: s.image.size.toDouble(),
                                color: Theme.of(context).colorScheme.tertiary,
                                showTitle: false,
                                radius: 46,
                              ),
                            ],
                          ),
                        ),
                        Column(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            Text(
                              formatBytes(s.totalSize),
                              style: Theme.of(context).textTheme.titleMedium
                                  ?.copyWith(fontWeight: FontWeight.w700),
                            ),
                            Text(
                              '总占用',
                              style: Theme.of(
                                context,
                              ).textTheme.bodySmall?.copyWith(
                                color: Theme.of(context).colorScheme.outline,
                              ),
                            ),
                          ],
                        ),
                      ],
                    ),
          ),
          const SizedBox(height: 32),
          Text('空间构成', style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 18),
          _RatioLine(
            label: '视频',
            detail: formatBytes(s.video.size),
            ratio: videoSizeRatio,
            color: Theme.of(context).colorScheme.primary,
          ),
          const SizedBox(height: 20),
          _RatioLine(
            label: '照片',
            detail: formatBytes(s.image.size),
            ratio: imageSizeRatio,
            color: Theme.of(context).colorScheme.tertiary,
          ),
        ],
      ),
    );
  }
}

class _Metric extends StatelessWidget {
  final double width;
  final String label;
  final String value;
  final String detail;
  final IconData icon;

  const _Metric({
    required this.width,
    required this.label,
    required this.value,
    required this.detail,
    required this.icon,
  });

  @override
  Widget build(BuildContext context) => SizedBox(
    width: width,
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Icon(icon, size: 17, color: Theme.of(context).colorScheme.primary),
            const SizedBox(width: 7),
            Text(label, style: Theme.of(context).textTheme.labelLarge),
          ],
        ),
        const SizedBox(height: 10),
        Text(
          value,
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
          style: Theme.of(
            context,
          ).textTheme.headlineMedium?.copyWith(fontWeight: FontWeight.w700),
        ),
        const SizedBox(height: 3),
        Text(
          detail,
          style: Theme.of(context).textTheme.bodySmall?.copyWith(
            color: Theme.of(context).colorScheme.onSurfaceVariant,
          ),
        ),
      ],
    ),
  );
}

class _RatioLine extends StatelessWidget {
  final String label;
  final String detail;
  final double ratio;
  final Color color;

  const _RatioLine({
    required this.label,
    required this.detail,
    required this.ratio,
    required this.color,
  });

  @override
  Widget build(BuildContext context) => Column(
    children: [
      Row(
        children: [
          Expanded(child: Text(label)),
          Text(
            '${(ratio * 100).toStringAsFixed(1)}%  ·  $detail',
            style: Theme.of(context).textTheme.bodySmall,
          ),
        ],
      ),
      const SizedBox(height: 8),
      ClipRRect(
        borderRadius: BorderRadius.circular(3),
        child: LinearProgressIndicator(
          minHeight: 7,
          value: ratio.clamp(0, 1),
          color: color,
          backgroundColor:
              Theme.of(context).colorScheme.surfaceContainerHighest,
        ),
      ),
    ],
  );
}

class _PagedMediaTab extends StatefulWidget {
  final ApiService api;
  final String kind;
  final DisplayPreferences prefs;

  const _PagedMediaTab({
    super.key,
    required this.api,
    required this.kind,
    required this.prefs,
  });

  @override
  State<_PagedMediaTab> createState() => _PagedMediaTabState();
}

class _PagedMediaTabState extends State<_PagedMediaTab> {
  MediaPage? _page;
  int _pageNo = 1;
  bool _loading = false;
  late int _loadedPageSize = _pageSize;
  late final TextEditingController _pageCtrl = TextEditingController(text: '1');
  final _gridScroll = ScrollController();

  int get _pageSize =>
      widget.kind == 'video'
          ? widget.prefs.videoPageSize
          : widget.prefs.imagePageSize;

  double get _extent =>
      widget.kind == 'video'
          ? widget.prefs.videoThumbExtent
          : widget.prefs.imageThumbExtent;

  String get _layout =>
      widget.kind == 'video'
          ? widget.prefs.videoLayout
          : widget.prefs.imageLayout;

  @override
  void initState() {
    super.initState();
    widget.prefs.addListener(_changed);
    _load();
  }

  void _changed() {
    if (!mounted) return;
    final layout = _layout;
    setState(() {});
    if (_loadedPageSize != _pageSize) {
      _loadedPageSize = _pageSize;
      _goToPage(1);
    }
    if (_loadedLayout != layout) {
      _loadedLayout = layout;
      if (_gridScroll.hasClients) _gridScroll.jumpTo(0);
    }
  }

  late String _loadedLayout = _layout;

  @override
  void dispose() {
    widget.prefs.removeListener(_changed);
    _pageCtrl.dispose();
    _gridScroll.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    if (_loading) return;
    setState(() => _loading = true);
    try {
      final page = await widget.api.getMediaPage(
        widget.kind,
        _pageNo,
        _pageSize,
      );
      if (!mounted) return;
      if (page.totalPages > 0 && _pageNo > page.totalPages) {
        _pageNo = page.totalPages;
        _pageCtrl.text = '$_pageNo';
        setState(() => _loading = false);
        await _load();
        return;
      }
      setState(() {
        _page = page;
        _pageNo = page.page;
        _pageCtrl.text = '$_pageNo';
      });
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('$e')));
      }
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  void _goToPage(int page) {
    final maxPage = _page?.totalPages ?? 1;
    _pageNo = page.clamp(1, maxPage < 1 ? 1 : maxPage);
    _pageCtrl.text = '$_pageNo';
    _load();
  }

  @override
  Widget build(BuildContext context) {
    final page = _page;
    final cs = Theme.of(context).colorScheme;
    final isVideo = widget.kind == 'video';
    final layout = _layout;
    return Column(
      children: [
        DisplayToolbar(
          layout: layout,
          onLayoutChanged:
              isVideo
                  ? widget.prefs.setVideoLayout
                  : widget.prefs.setImageLayout,
          thumbExtent: _extent,
          onThumbExtentChanged:
              isVideo
                  ? widget.prefs.setVideoThumbExtent
                  : widget.prefs.setImageThumbExtent,
          pageSize: _pageSize,
          onPageSizeChanged:
              isVideo
                  ? widget.prefs.setVideoPageSize
                  : widget.prefs.setImagePageSize,
        ),
        Expanded(
          child:
              page == null
                  ? LayoutBuilder(
                    builder: (context, constraints) {
                      final columns = columnCount(
                        constraints.maxWidth - 40,
                        _extent,
                      );
                      return GridView.builder(
                        physics: const NeverScrollableScrollPhysics(),
                        padding: const EdgeInsets.fromLTRB(20, 20, 20, 28),
                        itemCount: columns * 3,
                        gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
                          crossAxisCount: columns,
                          crossAxisSpacing: kMediaCrossSpacing,
                          mainAxisSpacing: kMediaMainSpacing,
                          childAspectRatio: 0.75,
                        ),
                        itemBuilder: (_, __) => const SkeletonBlock(),
                      );
                    },
                  )
                  : page.items.isEmpty
                  ? RefreshIndicator(
                    onRefresh: _load,
                    child: ListView(
                      physics: const AlwaysScrollableScrollPhysics(),
                      children: [
                        const SizedBox(height: 200),
                        Icon(
                          isVideo
                              ? Icons.videocam_outlined
                              : Icons.image_outlined,
                          size: 72,
                          color: cs.outlineVariant,
                        ),
                        const SizedBox(height: 16),
                        Center(child: Text(isVideo ? '暂无视频' : '暂无照片')),
                        const SizedBox(height: 8),
                        Center(
                          child: Text(
                            isVideo
                                ? '完成扫描后，这里会展示全部已入库视频'
                                : '完成扫描后，这里会展示全部已入库照片',
                            style: Theme.of(context).textTheme.bodySmall
                                ?.copyWith(color: cs.onSurfaceVariant),
                          ),
                        ),
                      ],
                    ),
                  )
                  : RefreshIndicator(
                    onRefresh: _load,
                    child: Scrollbar(
                      controller: _gridScroll,
                      thumbVisibility: false,
                      thickness: 6,
                      radius: const Radius.circular(3),
                      child: ContentFade(
                        fadeKey: '$_pageNo|$layout',
                        child: LayoutBuilder(
                          builder: (context, constraints) {
                            final columns = columnCount(
                              constraints.maxWidth - 40,
                              _extent,
                            );
                            if (layout == 'masonry') {
                              // 瀑布流：自实现 sliver（同库页，滚动稳定）
                              return CustomScrollView(
                                controller: _gridScroll,
                                physics: const AlwaysScrollableScrollPhysics(),
                                slivers: [
                                  SliverPadding(
                                    padding: const EdgeInsets.fromLTRB(
                                      20,
                                      20,
                                      20,
                                      28,
                                    ),
                                    sliver: SliverLayoutBuilder(
                                      builder: (context, constraints) {
                                        final columns = columnCount(
                                          constraints.crossAxisExtent,
                                          _extent,
                                        );
                                        return MasonrySliver(
                                          rebuildToken: page,
                                          childCount: page.items.length,
                                          itemBuilder: (context, index) {
                                            final media = page.items[index];
                                            return RepaintBoundary(
                                              child: MediaTile(
                                                media: media,
                                                api: widget.api,
                                                imageAspectRatio:
                                                    rawAspectRatio(media),
                                                showName: true,
                                                onOpenViewer:
                                                    () => openMediaViewer(
                                                      context,
                                                      widget.api,
                                                      page.items,
                                                      index,
                                                    ),
                                                onDeleted:
                                                    () =>
                                                        _removeFromPage(media),
                                              ),
                                            );
                                          },
                                          crossAxisCount: columns,
                                          crossAxisSpacing: kMediaCrossSpacing,
                                          mainAxisSpacing: kMediaMainSpacing,
                                          heightFactor:
                                              (i) =>
                                                  1 /
                                                  rawAspectRatio(page.items[i]),
                                          fixedExtraHeight: 26,
                                        );
                                      },
                                    ),
                                  ),
                                ],
                              );
                            }
                            if (layout == 'justified') {
                              // 自适应：按原始宽高比横向铺满整行，无空隙
                              final width = constraints.maxWidth - 40;
                              final targetH = _extent * 0.85;
                              const gap = kMediaJustifiedGap;
                              final rows = buildJustifiedRows(
                                page.items,
                                width,
                                targetH,
                                gap,
                              );
                              return ListView.builder(
                                controller: _gridScroll,
                                physics: const AlwaysScrollableScrollPhysics(),
                                padding: const EdgeInsets.fromLTRB(
                                  20,
                                  20,
                                  20,
                                  28,
                                ),
                                itemCount: rows.length,
                                itemBuilder: (context, rowIndex) {
                                  final row = rows[rowIndex];
                                  final rowH = justifiedRowHeight(
                                    row,
                                    width,
                                    targetH,
                                    gap,
                                  );
                                  final widths = justifiedCellWidths(
                                    row,
                                    rowH,
                                    width,
                                    gap,
                                  );
                                  return Padding(
                                    padding: const EdgeInsets.only(bottom: 14),
                                    child: Row(
                                      crossAxisAlignment:
                                          CrossAxisAlignment.start,
                                      children: [
                                        for (
                                          var c = 0;
                                          c < row.length;
                                          c++
                                        ) ...[
                                          if (c > 0) const SizedBox(width: gap),
                                          SizedBox(
                                            width: widths[c],
                                            child: MediaTile(
                                              media: row[c].$1,
                                              api: widget.api,
                                              imageAspectRatio: rawAspectRatio(
                                                row[c].$1,
                                              ),
                                              showName: true,
                                              imageFit:
                                                  row.length == 1
                                                      ? BoxFit.contain
                                                      : BoxFit.cover,
                                              cellWidth: widths[c],
                                              cellHeight: rowH,
                                              onOpenViewer:
                                                  () => openMediaViewer(
                                                    context,
                                                    widget.api,
                                                    page.items,
                                                    row[c].$2,
                                                  ),
                                              onDeleted:
                                                  () => _removeFromPage(
                                                    row[c].$1,
                                                  ),
                                            ),
                                          ),
                                        ],
                                      ],
                                    ),
                                  );
                                },
                              );
                            }
                            final imageRatio = layout == 'square' ? 1.0 : 4 / 3;
                            // 网格正切：正方形单元，图片裁剪填充不留空隙；自适应网格保留完整画面
                            final imageFit =
                                layout == 'square'
                                    ? BoxFit.cover
                                    : BoxFit.contain;
                            final width =
                                (constraints.maxWidth -
                                    40 -
                                    (columns - 1) * kMediaCrossSpacing) /
                                columns;
                            return GridView.builder(
                              controller: _gridScroll,
                              physics: const AlwaysScrollableScrollPhysics(),
                              padding: const EdgeInsets.fromLTRB(
                                20,
                                20,
                                20,
                                28,
                              ),
                              itemCount: page.items.length,
                              gridDelegate:
                                  SliverGridDelegateWithFixedCrossAxisCount(
                                    crossAxisCount: columns,
                                    crossAxisSpacing: kMediaCrossSpacing,
                                    mainAxisSpacing: kMediaMainSpacing,
                                    mainAxisExtent: width / imageRatio + 39,
                                  ),
                              itemBuilder: (_, index) {
                                final media = page.items[index];
                                return RepaintBoundary(
                                  child: MediaTile(
                                    media: media,
                                    api: widget.api,
                                    imageAspectRatio: imageRatio,
                                    imageFit: imageFit,
                                    showName: true,
                                    onOpenViewer:
                                        () => openMediaViewer(
                                          context,
                                          widget.api,
                                          page.items,
                                          index,
                                        ),
                                    onDeleted: () => _removeFromPage(media),
                                  ),
                                );
                              },
                            );
                          },
                        ),
                      ),
                    ),
                  ),
        ),
        PaginationBar(
          currentPage: _pageNo,
          totalPages: page?.totalPages ?? 0,
          totalItems: page?.total ?? 0,
          loading: _loading,
          controller: _pageCtrl,
          onPrevious: _pageNo > 1 ? () => _goToPage(_pageNo - 1) : null,
          onNext:
              page != null && _pageNo < page.totalPages
                  ? () => _goToPage(_pageNo + 1)
                  : null,
          onSubmit: () => _goToPage(int.tryParse(_pageCtrl.text) ?? 1),
        ),
      ],
    );
  }

  /// 删除媒体后从当前分页列表移除。
  void _removeFromPage(Media media) {
    final p = _page;
    if (p == null) return;
    setState(() => p.items.removeWhere((m) => m.id == media.id));
  }
}
