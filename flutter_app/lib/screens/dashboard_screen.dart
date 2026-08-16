// dashboard_screen.dart：首页媒体浏览、分页列表和统计。
import 'dart:math' as math;

import 'package:fl_chart/fl_chart.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_staggered_grid_view/flutter_staggered_grid_view.dart';

import '../models/models.dart';
import '../services/api_service.dart';
import '../services/display_preferences.dart';
import '../widgets/context_menu.dart';
import '../widgets/image_viewer.dart';
import '../widgets/media_viewer.dart';

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
  final _libraryScroll = ScrollController();
  List<MediaGroup> _groups = [];
  int _groupTotal = 0;
  bool _groupsLoading = false;
  MediaStatistics? _statistics;
  int _loadedDepth = 3;

  @override
  void initState() {
    super.initState();
    displayPreferences.addListener(_preferencesChanged);
    _libraryScroll.addListener(_loadMoreGroups);
    _initializeGroups();
    _loadStatistics();
  }

  Future<void> _initializeGroups() async {
    await displayPreferences.load();
    _loadedDepth = displayPreferences.libraryGroupDepth;
    await _loadGroups(refresh: true);
  }

  void _preferencesChanged() {
    if (!mounted) return;
    setState(() {});
    if (_loadedDepth != displayPreferences.libraryGroupDepth) {
      _loadedDepth = displayPreferences.libraryGroupDepth;
      _loadGroups(refresh: true);
    }
  }

  @override
  void dispose() {
    displayPreferences.removeListener(_preferencesChanged);
    _libraryScroll.dispose();
    super.dispose();
  }

  Future<void> _loadGroups({bool refresh = false}) async {
    if (_groupsLoading) return;
    if (!refresh && _groups.length >= _groupTotal && _groupTotal != 0) return;
    // 记录当前滚动位置：refresh 会清空重载导致内容高度骤变，
    // ScrollPosition 会把像素 clamp 到新的内容范围（通常趋近 0），
    // 加载完成后需恢复原位置，避免"回到起点"。
    final savedOffset =
        _libraryScroll.hasClients ? _libraryScroll.position.pixels : 0.0;
    setState(() => _groupsLoading = true);
    try {
      final result = await widget.api.getMediaGroups(
        displayPreferences.libraryGroupDepth,
        refresh ? 0 : _groups.length,
        20,
      );
      if (!mounted) return;
      setState(() {
        if (refresh) _groups = [];
        _groupTotal = result.total;
        _groups.addAll(result.items);
      });
      if (savedOffset > 0) {
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (!mounted || !_libraryScroll.hasClients) return;
          final maxExtent = _libraryScroll.position.maxScrollExtent;
          if (maxExtent <= 0) return;
          // 触底加载后原位置已超出新内容范围时停在底部，否则恢复原位置
          _libraryScroll.jumpTo(
            savedOffset > maxExtent ? maxExtent : savedOffset,
          );
        });
      }
    } catch (e) {
      if (mounted) _showError(e);
    } finally {
      if (mounted) setState(() => _groupsLoading = false);
    }
  }

  void _loadMoreGroups() {
    if (_libraryScroll.position.extentAfter < 500) _loadGroups();
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
        _buildLibraryTab(),
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

  Widget _buildLibraryTab() {
    final cs = Theme.of(context).colorScheme;
    return Column(
      children: [
        _DisplayToolbar(
          layout: displayPreferences.libraryLayout,
          onLayoutChanged: displayPreferences.setLibraryLayout,
          thumbExtent: displayPreferences.libraryThumbExtent,
          onThumbExtentChanged: displayPreferences.setLibraryThumbExtent,
        ),
        Expanded(
          child: RefreshIndicator(
            onRefresh: () => _loadGroups(refresh: true),
            child: Scrollbar(
              controller: _libraryScroll,
              thumbVisibility: false,
              thickness: 6,
              radius: const Radius.circular(3),
              child: _LayoutFade(
                layout: displayPreferences.libraryLayout,
                child: CustomScrollView(
                  controller: _libraryScroll,
                  physics: const AlwaysScrollableScrollPhysics(),
                  slivers: [
                    const SliverPadding(padding: EdgeInsets.only(top: 18)),
                    for (int i = 0; i < _groups.length; i++) ...[
                      SliverToBoxAdapter(
                        child: Padding(
                          padding: const EdgeInsets.fromLTRB(20, 2, 20, 10),
                          child: Row(
                            children: [
                              Expanded(
                                child: Text(
                                  '${_groups[i].libraryName}${_groups[i].groupPath.isEmpty ? '' : ' / ${_groups[i].groupPath}'}',
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                  style: Theme.of(context).textTheme.titleSmall
                                      ?.copyWith(fontWeight: FontWeight.w700),
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
                      const SliverPadding(padding: EdgeInsets.only(bottom: 26)),
                    ],
                    if (_groups.isEmpty && _groupsLoading)
                      SliverPadding(
                        padding: const EdgeInsets.fromLTRB(20, 4, 20, 0),
                        sliver: SliverGrid.builder(
                          itemCount: 18,
                          gridDelegate:
                              SliverGridDelegateWithMaxCrossAxisExtent(
                                maxCrossAxisExtent:
                                    displayPreferences.libraryThumbExtent,
                                crossAxisSpacing: 12,
                                mainAxisSpacing: 18,
                                childAspectRatio: 0.8,
                              ),
                          itemBuilder: (_, __) => const _SkeletonBlock(),
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
                                Icons.photo_library_outlined,
                                size: 72,
                                color: cs.outlineVariant,
                              ),
                              const SizedBox(height: 16),
                              Text(
                                '暂无已入库媒体',
                                style: Theme.of(context).textTheme.titleMedium
                                    ?.copyWith(fontWeight: FontWeight.w600),
                              ),
                              const SizedBox(height: 8),
                              Text(
                                '添加收藏库并完成扫描后，这里会展示你的照片与视频',
                                style: Theme.of(context).textTheme.bodySmall
                                    ?.copyWith(color: cs.onSurfaceVariant),
                              ),
                              const SizedBox(height: 20),
                              FilledButton.icon(
                                onPressed: widget.onOpenScan,
                                icon: const Icon(Icons.manage_search),
                                label: const Text('前往扫描'),
                              ),
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
      ],
    );
  }

  Widget _buildLibraryGroup(MediaGroup group) {
    final extent = displayPreferences.libraryThumbExtent;
    final layout = displayPreferences.libraryLayout;
    if (layout == 'masonry') {
      return SliverPadding(
        padding: const EdgeInsets.fromLTRB(20, 0, 20, 0),
        sliver: SliverLayoutBuilder(
          builder: (context, constraints) {
            final columns = _columnCount(constraints.crossAxisExtent, extent);
            return SliverMasonryGrid.count(
              crossAxisCount: columns,
              crossAxisSpacing: 12,
              mainAxisSpacing: 18,
              childCount: group.items.length,
              itemBuilder: (context, index) {
                final media = group.items[index];
                return RepaintBoundary(
                  child: _MediaTile(
                    media: media,
                    api: widget.api,
                    imageAspectRatio: _mediaAspectRatio(media),
                    showName: false,
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

    if (layout == 'justified') {
      // 自适应：按原始宽高比横向铺满整行，无空隙（Google Photos 风格）
      return SliverPadding(
        padding: const EdgeInsets.fromLTRB(20, 0, 20, 0),
        sliver: SliverLayoutBuilder(
          builder: (context, constraints) {
            final width = constraints.crossAxisExtent;
            final targetH = extent * 0.85;
            const gap = 10.0;
            final rows = _buildJustifiedRows(group.items, width, targetH, gap);
            return SliverList.builder(
              itemCount: rows.length,
              itemBuilder: (context, rowIndex) {
                final row = rows[rowIndex];
                final rowH = _justifiedRowHeight(row, width, targetH, gap);
                final widths = _justifiedCellWidths(row, rowH, width, gap);
                return Padding(
                  padding: const EdgeInsets.only(bottom: 12),
                  child: Row(
                    children: [
                      for (var c = 0; c < row.length; c++) ...[
                        if (c > 0) const SizedBox(width: gap),
                        SizedBox(
                          width: widths[c],
                          child: _MediaTile(
                            media: row[c].$1,
                            api: widget.api,
                            imageAspectRatio: _rawAspectRatio(row[c].$1),
                            showName: false,
                            imageFit:
                                row.length == 1 ? BoxFit.contain : BoxFit.cover,
                            cellWidth: widths[c],
                            cellHeight: rowH,
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
      padding: const EdgeInsets.fromLTRB(20, 0, 20, 0),
      sliver: SliverLayoutBuilder(
        builder: (context, constraints) {
          final columns = _columnCount(constraints.crossAxisExtent, extent);
          final width =
              (constraints.crossAxisExtent - (columns - 1) * 12) / columns;
          final imageRatio = layout == 'square' ? 1.0 : 4 / 3;
          // 网格正切：正方形单元，图片裁剪填充不留空隙；自适应网格保留完整画面
          final imageFit = layout == 'square' ? BoxFit.cover : BoxFit.contain;
          return SliverGrid.builder(
            itemCount: group.items.length,
            gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
              crossAxisCount: columns,
              crossAxisSpacing: 12,
              mainAxisSpacing: 18,
              mainAxisExtent: width / imageRatio,
            ),
            itemBuilder: (context, index) {
              final media = group.items[index];
              return RepaintBoundary(
                child: _MediaTile(
                  media: media,
                  api: widget.api,
                  imageAspectRatio: imageRatio,
                  imageFit: imageFit,
                  showName: false,
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

  void _openViewer(List<Media> items, int index) {
    _openMediaViewer(context, widget.api, items, index);
  }

  /// 删除媒体后从本地分组列表移除（总数角标保留服务端快照，下次刷新校正）。
  void _removeFromGroups(Media media) {
    setState(() {
      for (final group in _groups) {
        group.items.removeWhere((m) => m.id == media.id);
      }
    });
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
            _formatBytes(s.totalSize),
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
                    detail: _formatBytes(s.image.size),
                    icon: Icons.image_outlined,
                  ),
                  _Metric(
                    width: width.clamp(180, 420),
                    label: '视频',
                    value: '${s.video.count}',
                    detail: _formatBytes(s.video.size),
                    icon: Icons.movie_outlined,
                  ),
                  _Metric(
                    width: width.clamp(180, 420),
                    label: '视频时长',
                    value: _formatDuration(s.video.durationMs),
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
                              _formatBytes(s.totalSize),
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
            detail: _formatBytes(s.video.size),
            ratio: videoSizeRatio,
            color: Theme.of(context).colorScheme.primary,
          ),
          const SizedBox(height: 20),
          _RatioLine(
            label: '照片',
            detail: _formatBytes(s.image.size),
            ratio: imageSizeRatio,
            color: Theme.of(context).colorScheme.tertiary,
          ),
        ],
      ),
    );
  }
}

class _MediaTile extends StatefulWidget {
  final Media media;
  final ApiService api;
  final double imageAspectRatio;
  final bool showName;
  final BoxFit imageFit;
  final double? cellWidth;
  final double? cellHeight;
  final VoidCallback onOpenViewer;
  final VoidCallback? onDeleted;

  const _MediaTile({
    required this.media,
    required this.api,
    required this.imageAspectRatio,
    this.showName = true,
    this.imageFit = BoxFit.contain,
    this.cellWidth,
    this.cellHeight,
    required this.onOpenViewer,
    this.onDeleted,
  });

  @override
  State<_MediaTile> createState() => _MediaTileState();
}

class _MediaTileState extends State<_MediaTile> {
  bool _hovered = false;

  @override
  Widget build(BuildContext context) {
    final media = widget.media;
    final name = media.relativePath.split(RegExp(r'[/\\]')).last;
    final fixedSize = widget.cellWidth != null && widget.cellHeight != null;
    Widget buildImageArea() {
      final stack = Stack(
        fit: StackFit.expand,
        children: [
          _MediaImage(media: media, api: widget.api, fit: widget.imageFit),
          // hover 遮罩（淡入）
          AnimatedOpacity(
            opacity: _hovered ? 1 : 0,
            duration: const Duration(milliseconds: 140),
            child: DecoratedBox(
              decoration: BoxDecoration(
                color: Colors.black.withValues(alpha: .1),
                borderRadius: BorderRadius.circular(10),
              ),
            ),
          ),
          if (media.kind == 'video') ...[
            const Positioned(
              top: 6,
              left: 6,
              child: _MediaBadge(icon: Icons.play_arrow_rounded),
            ),
            Positioned(
              right: 6,
              bottom: 6,
              child: _MediaBadge(
                text:
                    '${_videoQuality(media.height)}  ${_formatClock(media.durationMs ?? 0)}',
              ),
            ),
          ],
          // hover 快捷操作（右上角）
          Positioned(
            top: 6,
            right: 6,
            child: IgnorePointer(
              ignoring: !_hovered,
              child: AnimatedOpacity(
                opacity: _hovered ? 1 : 0,
                duration: const Duration(milliseconds: 140),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    _QuickActionButton(
                      icon: Icons.open_in_full_rounded,
                      tooltip: '打开媒体',
                      onTap: widget.onOpenViewer,
                    ),
                    const SizedBox(width: 6),
                    _QuickActionButton(
                      icon: Icons.more_vert_rounded,
                      tooltip: '更多操作',
                      onTap: _openMoreMenu,
                    ),
                  ],
                ),
              ),
            ),
          ),
        ],
      );
      if (fixedSize) {
        return SizedBox(
          width: widget.cellWidth,
          height: widget.cellHeight,
          child: stack,
        );
      }
      return AspectRatio(aspectRatio: widget.imageAspectRatio, child: stack);
    }

    return MouseRegion(
      onEnter: (_) => setState(() => _hovered = true),
      onExit: (_) => setState(() => _hovered = false),
      child: GestureDetector(
        behavior: HitTestBehavior.opaque,
        onDoubleTap: widget.onOpenViewer,
        onSecondaryTapDown:
            (details) => _showMediaMenu(
              context,
              widget.api,
              media,
              details.globalPosition,
              onOpenViewer: widget.onOpenViewer,
              onDeleted: widget.onDeleted,
            ),
        child: AnimatedScale(
          scale: _hovered ? 1.03 : 1.0,
          duration: const Duration(milliseconds: 160),
          curve: Curves.easeOut,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              buildImageArea(),
              if (widget.showName) ...[
                const SizedBox(height: 6),
                Text(
                  name,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }

  void _openMoreMenu() {
    final box = context.findRenderObject() as RenderBox?;
    if (box == null) return;
    final position = box.localToGlobal(
      Offset(box.size.width / 2, box.size.height / 2),
    );
    _showMediaMenu(
      context,
      widget.api,
      widget.media,
      position,
      onOpenViewer: widget.onOpenViewer,
      onDeleted: widget.onDeleted,
    );
  }
}

/// 悬停时显示的圆形快捷操作按钮（黑底白字，仿相册浮层）。
class _QuickActionButton extends StatelessWidget {
  final IconData icon;
  final String tooltip;
  final VoidCallback onTap;

  const _QuickActionButton({
    required this.icon,
    required this.tooltip,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return Tooltip(
      message: tooltip,
      child: Material(
        color: Colors.black.withValues(alpha: .55),
        borderRadius: BorderRadius.circular(6),
        child: InkWell(
          borderRadius: BorderRadius.circular(6),
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.all(4),
            child: Icon(icon, size: 14, color: Colors.white),
          ),
        ),
      ),
    );
  }
}

class _MediaImage extends StatelessWidget {
  final Media media;
  final ApiService api;
  final BoxFit fit;

  const _MediaImage({
    required this.media,
    required this.api,
    this.fit = BoxFit.contain,
  });

  @override
  Widget build(BuildContext context) {
    if (media.thumbnailPath == null) {
      return const Center(child: Icon(Icons.broken_image_outlined, size: 36));
    }
    return Image.network(
      api.thumbnailUrl(media.kind, media.thumbnailPath!),
      fit: fit,
      loadingBuilder: (_, child, progress) {
        // 加载中骨架垫底，图片就绪后自然覆盖（无需闪烁占位）
        if (progress == null) return child;
        return Stack(
          fit: StackFit.expand,
          children: [const _SkeletonBlock(), child],
        );
      },
      errorBuilder:
          (_, __, ___) =>
              const Center(child: Icon(Icons.broken_image_outlined, size: 36)),
    );
  }
}

/// 脉动骨架块（加载占位 / 首屏骨架网格共用）。
class _SkeletonBlock extends StatefulWidget {
  const _SkeletonBlock();

  @override
  State<_SkeletonBlock> createState() => _SkeletonBlockState();
}

class _SkeletonBlockState extends State<_SkeletonBlock>
    with SingleTickerProviderStateMixin {
  late final AnimationController _controller = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 900),
    lowerBound: 0.3,
    upperBound: 0.8,
  )..repeat(reverse: true);

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return FadeTransition(
      opacity: _controller,
      child: Container(
        decoration: BoxDecoration(
          color: Theme.of(context).colorScheme.surfaceContainerHighest,
          borderRadius: BorderRadius.circular(10),
        ),
      ),
    );
  }
}

/// 布局切换时的淡入容器（sliver 无法直接包动画，包在 CustomScrollView 外层）。
class _LayoutFade extends StatefulWidget {
  final String layout;
  final Widget child;

  const _LayoutFade({required this.layout, required this.child});

  @override
  State<_LayoutFade> createState() => _LayoutFadeState();
}

class _LayoutFadeState extends State<_LayoutFade>
    with SingleTickerProviderStateMixin {
  late final AnimationController _controller = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 240),
    value: 1,
  );

  @override
  void didUpdateWidget(_LayoutFade old) {
    super.didUpdateWidget(old);
    if (old.layout != widget.layout) {
      _controller.forward(from: 0.3);
    }
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return FadeTransition(opacity: _controller, child: widget.child);
  }
}

/// 翻页/布局变化时的淡入容器（单实例替换，避免双滚动视图争用同一 controller）。
class _ContentFade extends StatefulWidget {
  final String fadeKey;
  final Widget child;

  const _ContentFade({required this.fadeKey, required this.child});

  @override
  State<_ContentFade> createState() => _ContentFadeState();
}

class _ContentFadeState extends State<_ContentFade>
    with SingleTickerProviderStateMixin {
  late final AnimationController _controller = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 220),
    value: 1,
  );

  @override
  void didUpdateWidget(_ContentFade old) {
    super.didUpdateWidget(old);
    if (old.fadeKey != widget.fadeKey) {
      _controller.forward(from: 0);
    }
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return FadeTransition(opacity: _controller, child: widget.child);
  }
}

/// 页面顶部显示设置条：布局切换、缩放滑杆、每页数量（视频/照片）。
class _DisplayToolbar extends StatelessWidget {
  final String layout;
  final ValueChanged<String> onLayoutChanged;
  final double thumbExtent;
  final ValueChanged<double> onThumbExtentChanged;
  final int? pageSize;
  final ValueChanged<int>? onPageSizeChanged;

  const _DisplayToolbar({
    required this.layout,
    required this.onLayoutChanged,
    required this.thumbExtent,
    required this.onThumbExtentChanged,
    this.pageSize,
    this.onPageSizeChanged,
  });

  static const _layoutOptions = [
    ('adaptive', '网格自适应', Icons.dashboard_outlined),
    ('square', '网格正切', Icons.grid_view_outlined),
    ('masonry', '瀑布流', Icons.view_column_outlined),
    ('justified', '自适应', Icons.view_day_outlined),
  ];

  String _layoutLabel(String value) {
    for (final (key, label, _) in _layoutOptions) {
      if (key == value) return label;
    }
    return '网格自适应';
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Container(
      height: 48,
      padding: const EdgeInsets.symmetric(horizontal: 20),
      decoration: BoxDecoration(
        border: Border(
          bottom: BorderSide(color: cs.outlineVariant, width: 0.5),
        ),
      ),
      child: Row(
        children: [
          // 布局切换
          PopupMenuButton<String>(
            tooltip: '布局',
            initialValue: layout,
            onSelected: onLayoutChanged,
            itemBuilder:
                (context) => [
                  for (final (key, label, icon) in _layoutOptions)
                    PopupMenuItem<String>(
                      value: key,
                      child: Row(
                        children: [
                          Icon(icon, size: 18, color: cs.onSurfaceVariant),
                          const SizedBox(width: 10),
                          Text(label),
                          if (key == layout) ...[
                            const Spacer(),
                            Icon(Icons.check, size: 16, color: cs.primary),
                          ],
                        ],
                      ),
                    ),
                ],
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 4),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Icon(
                    Icons.view_quilt_outlined,
                    size: 17,
                    color: cs.onSurfaceVariant,
                  ),
                  const SizedBox(width: 6),
                  Text(
                    _layoutLabel(layout),
                    style: TextStyle(
                      fontSize: 12.5,
                      color: cs.onSurfaceVariant,
                    ),
                  ),
                  Icon(Icons.arrow_drop_down, size: 16, color: cs.outline),
                ],
              ),
            ),
          ),
          const SizedBox(width: 20),
          // 缩放滑杆
          Icon(
            Icons.zoom_in_map_outlined,
            size: 16,
            color: cs.onSurfaceVariant,
          ),
          const SizedBox(width: 4),
          Tooltip(
            message: '缩放',
            child: SizedBox(
              width: 150,
              height: 32,
              child: SliderTheme(
                data: SliderThemeData(
                  trackHeight: 2.5,
                  activeTrackColor: cs.primary,
                  inactiveTrackColor: cs.outlineVariant,
                  thumbColor: cs.primary,
                  overlayShape: const RoundSliderOverlayShape(
                    overlayRadius: 10,
                  ),
                  thumbShape: const RoundSliderThumbShape(
                    enabledThumbRadius: 6,
                  ),
                ),
                child: Slider(
                  value: thumbExtent,
                  min: 120,
                  max: 320,
                  divisions: 20,
                  onChanged: onThumbExtentChanged,
                ),
              ),
            ),
          ),
          Text(
            '${thumbExtent.round()}px',
            style: TextStyle(fontSize: 11, color: cs.outline),
          ),
          if (pageSize != null) ...[
            const SizedBox(width: 20),
            Container(width: 1, height: 18, color: cs.outlineVariant),
            const SizedBox(width: 10),
            // 每页数量
            PopupMenuButton<int>(
              tooltip: '每页数量',
              initialValue: pageSize,
              onSelected: onPageSizeChanged,
              itemBuilder:
                  (context) => [
                    for (final size in const [10, 20, 50, 100])
                      PopupMenuItem<int>(
                        value: size,
                        child: Row(
                          children: [
                            Text('$size 项/页'),
                            if (size == pageSize) ...[
                              const Spacer(),
                              Icon(Icons.check, size: 16, color: cs.primary),
                            ],
                          ],
                        ),
                      ),
                  ],
              child: Padding(
                padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 4),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Icon(
                      Icons.format_list_numbered,
                      size: 16,
                      color: cs.onSurfaceVariant,
                    ),
                    const SizedBox(width: 6),
                    Text(
                      '$pageSize/页',
                      style: TextStyle(
                        fontSize: 12.5,
                        color: cs.onSurfaceVariant,
                      ),
                    ),
                    Icon(Icons.arrow_drop_down, size: 16, color: cs.outline),
                  ],
                ),
              ),
            ),
          ],
        ],
      ),
    );
  }
}

class _MediaBadge extends StatelessWidget {
  final IconData? icon;
  final String? text;

  const _MediaBadge({this.icon, this.text});

  @override
  Widget build(BuildContext context) => DecoratedBox(
    decoration: BoxDecoration(
      color: Colors.black.withValues(alpha: .68),
      borderRadius: BorderRadius.circular(4),
      border: Border.all(color: Colors.white.withValues(alpha: .18)),
    ),
    child: Padding(
      padding: EdgeInsets.symmetric(
        horizontal: text == null ? 3 : 6,
        vertical: text == null ? 2 : 3,
      ),
      child:
          icon == null
              ? Text(
                text!,
                style: const TextStyle(
                  color: Colors.white,
                  fontSize: 10,
                  fontWeight: FontWeight.w600,
                ),
              )
              : Icon(icon, color: Colors.white, size: 16),
    ),
  );
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
        _DisplayToolbar(
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
                      final columns = _columnCount(
                        constraints.maxWidth - 40,
                        _extent,
                      );
                      return GridView.builder(
                        physics: const NeverScrollableScrollPhysics(),
                        padding: const EdgeInsets.fromLTRB(20, 20, 20, 28),
                        itemCount: columns * 3,
                        gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
                          crossAxisCount: columns,
                          crossAxisSpacing: 12,
                          mainAxisSpacing: 18,
                          childAspectRatio: 0.75,
                        ),
                        itemBuilder: (_, __) => const _SkeletonBlock(),
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
                      child: _ContentFade(
                        fadeKey: '$_pageNo|$layout',
                        child: LayoutBuilder(
                          builder: (context, constraints) {
                            final columns = _columnCount(
                              constraints.maxWidth - 40,
                              _extent,
                            );
                            if (layout == 'masonry') {
                              return MasonryGridView.count(
                                controller: _gridScroll,
                                physics: const AlwaysScrollableScrollPhysics(),
                                padding: const EdgeInsets.fromLTRB(
                                  20,
                                  20,
                                  20,
                                  28,
                                ),
                                crossAxisCount: columns,
                                crossAxisSpacing: 12,
                                mainAxisSpacing: 18,
                                itemCount: page.items.length,
                                itemBuilder: (_, index) {
                                  final media = page.items[index];
                                  return RepaintBoundary(
                                    child: _MediaTile(
                                      media: media,
                                      api: widget.api,
                                      imageAspectRatio: _mediaAspectRatio(
                                        media,
                                      ),
                                      showName: true,
                                      onOpenViewer:
                                          () => _openMediaViewer(
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
                            }
                            if (layout == 'justified') {
                              // 自适应：按原始宽高比横向铺满整行，无空隙
                              final width = constraints.maxWidth - 40;
                              final targetH = _extent * 0.85;
                              const gap = 10.0;
                              final rows = _buildJustifiedRows(
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
                                  final rowH = _justifiedRowHeight(
                                    row,
                                    width,
                                    targetH,
                                    gap,
                                  );
                                  final widths = _justifiedCellWidths(
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
                                            child: _MediaTile(
                                              media: row[c].$1,
                                              api: widget.api,
                                              imageAspectRatio: _rawAspectRatio(
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
                                                  () => _openMediaViewer(
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
                                    (columns - 1) * 12) /
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
                                    crossAxisSpacing: 12,
                                    mainAxisSpacing: 18,
                                    mainAxisExtent: width / imageRatio + 39,
                                  ),
                              itemBuilder: (_, index) {
                                final media = page.items[index];
                                return RepaintBoundary(
                                  child: _MediaTile(
                                    media: media,
                                    api: widget.api,
                                    imageAspectRatio: imageRatio,
                                    imageFit: imageFit,
                                    showName: true,
                                    onOpenViewer:
                                        () => _openMediaViewer(
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
        _PaginationBar(
          page: page,
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

class _PaginationBar extends StatelessWidget {
  final MediaPage? page;
  final bool loading;
  final TextEditingController controller;
  final VoidCallback? onPrevious;
  final VoidCallback? onNext;
  final VoidCallback onSubmit;

  const _PaginationBar({
    required this.page,
    required this.loading,
    required this.controller,
    required this.onPrevious,
    required this.onNext,
    required this.onSubmit,
  });

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 5, 16, 12),
      child: DecoratedBox(
        decoration: BoxDecoration(
          color: colorScheme.surfaceContainerLow.withValues(alpha: .82),
          border: Border.all(color: colorScheme.outlineVariant),
          borderRadius: BorderRadius.circular(8),
        ),
        child: SizedBox(
          height: 42,
          child: Row(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              IconButton(
                tooltip: '上一页',
                visualDensity: VisualDensity.compact,
                onPressed: loading ? null : onPrevious,
                icon: const Icon(Icons.chevron_left, size: 20),
              ),
              SizedBox(
                width: 46,
                height: 30,
                child: TextField(
                  controller: controller,
                  textAlign: TextAlign.center,
                  keyboardType: TextInputType.number,
                  inputFormatters: [FilteringTextInputFormatter.digitsOnly],
                  onSubmitted: (_) => onSubmit(),
                  decoration: const InputDecoration(
                    isDense: true,
                    contentPadding: EdgeInsets.symmetric(
                      horizontal: 5,
                      vertical: 7,
                    ),
                    border: OutlineInputBorder(),
                  ),
                ),
              ),
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 7),
                child: Text('/ ${page?.totalPages ?? 0}'),
              ),
              IconButton(
                tooltip: '下一页',
                visualDensity: VisualDensity.compact,
                onPressed: loading ? null : onNext,
                icon: const Icon(Icons.chevron_right, size: 20),
              ),
              Container(
                width: 1,
                height: 18,
                margin: const EdgeInsets.symmetric(horizontal: 10),
                color: colorScheme.outlineVariant,
              ),
              Text(
                '共 ${page?.total ?? 0} 项',
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                  color: colorScheme.onSurfaceVariant,
                ),
              ),
              if (loading) ...[
                const SizedBox(width: 12),
                const SizedBox(
                  width: 14,
                  height: 14,
                  child: CircularProgressIndicator(strokeWidth: 2),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

void _showMediaMenu(
  BuildContext context,
  ApiService api,
  Media media,
  Offset position, {
  VoidCallback? onOpenViewer,
  VoidCallback? onDeleted,
}) {
  showContextMenu(
    context: context,
    position: position,
    items: [
      ContextMenuItem(
        icon: Icons.open_in_full_rounded,
        label: '打开媒体',
        onTap: onOpenViewer,
      ),
      const ContextMenuItem.divider(),
      ContextMenuItem(
        icon: Icons.open_in_new,
        label: '打开文件',
        onTap: () => api.openMediaFile(media.id),
      ),
      ContextMenuItem(
        icon: Icons.folder_open,
        label: '打开所在目录',
        onTap: () => api.openMediaDirectory(media.id),
      ),
      ContextMenuItem(
        icon: Icons.copy,
        label: '复制文件路径',
        onTap: () => _copyMediaPath(context, api, media),
      ),
      const ContextMenuItem.divider(),
      ContextMenuItem(
        icon: Icons.delete_outline,
        label: '删除此文件',
        isDestructive: true,
        onTap: () => _confirmDelete(context, api, media, onDeleted),
      ),
    ],
  );
}

/// 复制媒体完整本地路径到剪贴板（服务端路径由 API 提供）。
Future<void> _copyMediaPath(
  BuildContext context,
  ApiService api,
  Media media,
) async {
  try {
    final path = await api.mediaLocalPath(media.id);
    await Clipboard.setData(ClipboardData(text: path));
    if (context.mounted) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('文件路径已复制')));
    }
  } catch (e) {
    if (context.mounted) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text('复制路径失败: $e')));
    }
  }
}

/// 确认并删除单个媒体（源文件移入回收站），成功后回调本地移除。
Future<void> _confirmDelete(
  BuildContext context,
  ApiService api,
  Media media,
  VoidCallback? onDeleted,
) async {
  final name = media.relativePath.split(RegExp(r'[/\\]')).last;
  final ok = await showDialog<bool>(
    context: context,
    builder:
        (dialogContext) => AlertDialog(
          title: const Text('删除此文件'),
          content: Text('确认删除「$name」？源文件将移入系统回收站。'),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(dialogContext, false),
              child: const Text('取消'),
            ),
            FilledButton(
              style: FilledButton.styleFrom(
                backgroundColor: Theme.of(dialogContext).colorScheme.error,
              ),
              onPressed: () => Navigator.pop(dialogContext, true),
              child: const Text('删除'),
            ),
          ],
        ),
  );
  if (ok != true || !context.mounted) return;
  try {
    final result = await api.deleteMedia([media.id]);
    onDeleted?.call();
    if (context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('已删除，释放 ${_formatBytes(result.freedBytes)}')),
      );
    }
  } catch (e) {
    if (context.mounted) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text('删除失败: $e')));
    }
  }
}

void _openMediaViewer(
  BuildContext context,
  ApiService api,
  List<Media> items,
  int index,
) {
  if (items.isEmpty) return;
  Navigator.of(context).push(
    MaterialPageRoute(
      fullscreenDialog: true,
      builder:
          (_) =>
              items[index].kind == 'image'
                  ? ImageViewer(
                    medias: items,
                    initialIndex: index,
                    api: api,
                    preserveQueue: true,
                  )
                  : MediaViewer(
                    medias: items,
                    initialIndex: index,
                    api: api,
                    preserveQueue: true,
                  ),
    ),
  );
}

int _columnCount(double width, double preferredExtent) {
  return ((width + 12) / (preferredExtent + 12)).floor().clamp(1, 12);
}

double _mediaAspectRatio(Media media) {
  final width = media.width ?? 0;
  final height = media.height ?? 0;
  if (width <= 0 || height <= 0) return 4 / 3;
  return (width / height).clamp(.45, 2.4);
}

/// 原始宽高比（不裁剪限制），用于自适应铺满行的宽度计算。
double _rawAspectRatio(Media media) {
  final width = media.width ?? 0;
  final height = media.height ?? 0;
  if (width <= 0 || height <= 0) return 4 / 3;
  return width / height;
}

/// 自适应布局：将媒体按原始宽高比分组为横向铺满的行。
List<List<(Media, int)>> _buildJustifiedRows(
  List<Media> items,
  double totalWidth,
  double targetHeight,
  double gap,
) {
  final rows = <List<(Media, int)>>[];
  var current = <(Media, int)>[];
  var ratioSum = 0.0;
  for (var i = 0; i < items.length; i++) {
    final ratio = _rawAspectRatio(items[i]);
    if (current.isNotEmpty &&
        (ratioSum + ratio) * targetHeight + gap * current.length > totalWidth) {
      rows.add(current);
      current = <(Media, int)>[];
      ratioSum = 0;
    }
    current.add((items[i], i));
    ratioSum += ratio;
  }
  if (current.isNotEmpty) rows.add(current);
  return rows;
}

/// 计算自适应行高：让行内图片总宽（含间隙）恰好等于可用宽度。
/// 单张图片行回退到目标高度（保持原始比例完整展示）。
double _justifiedRowHeight(
  List<(Media, int)> row,
  double totalWidth,
  double targetHeight,
  double gap,
) {
  final ratioSum = row.fold(0.0, (s, item) => s + _rawAspectRatio(item.$1));
  final available = totalWidth - gap * (row.length - 1);
  if (row.length == 1) {
    return math.min(targetHeight, available / ratioSum);
  }
  return available / ratioSum;
}

/// 计算行内每张图片的宽度（末位补足剩余宽度，避免浮点误差产生空隙）。
List<double> _justifiedCellWidths(
  List<(Media, int)> row,
  double rowHeight,
  double totalWidth,
  double gap,
) {
  final widths = <double>[];
  if (row.isEmpty) return widths;
  if (row.length == 1) {
    widths.add(math.min(_rawAspectRatio(row[0].$1) * rowHeight, totalWidth));
    return widths;
  }
  var used = 0.0;
  for (var c = 0; c < row.length; c++) {
    if (c == row.length - 1) {
      widths.add(totalWidth - used);
    } else {
      final w = _rawAspectRatio(row[c].$1) * rowHeight;
      widths.add(w);
      used += w + gap;
    }
  }
  return widths;
}

String _videoQuality(int? height) {
  final value = height ?? 0;
  if (value < 720) return '${value}P';
  if (value < 1080) return '720P';
  if (value < 1440) return '1080P';
  if (value < 2160) return '2K';
  return '4K';
}

String _formatClock(int ms) {
  final seconds = ms ~/ 1000;
  final hours = seconds ~/ 3600;
  final minutes = (seconds % 3600) ~/ 60;
  final remaining = seconds % 60;
  if (hours > 0) {
    return '$hours:${minutes.toString().padLeft(2, '0')}:${remaining.toString().padLeft(2, '0')}';
  }
  return '$minutes:${remaining.toString().padLeft(2, '0')}';
}

String _formatBytes(int bytes) =>
    bytes < 1024
        ? '$bytes B'
        : bytes < 1024 * 1024
        ? '${(bytes / 1024).toStringAsFixed(1)} KB'
        : bytes < 1024 * 1024 * 1024
        ? '${(bytes / 1048576).toStringAsFixed(1)} MB'
        : '${(bytes / 1073741824).toStringAsFixed(2)} GB';

String _formatDuration(int ms) {
  final hours = ms ~/ 3600000;
  final minutes = (ms % 3600000) ~/ 60000;
  return hours == 0 ? '$minutes 分钟' : '$hours 小时 $minutes 分';
}
