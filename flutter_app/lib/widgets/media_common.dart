// media_common.dart：首页媒体浏览的公共组件与工具函数。
// 从 dashboard_screen.dart 迁出并公开化，供库页（library_tab）与
// 视频/照片/统计页共同使用。
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../models/models.dart';
import '../services/api_service.dart';
import 'context_menu.dart';
import 'image_viewer.dart';
import 'media_viewer.dart';

/// 媒体网格间隔常量：统一调整图片间距时只需修改这里。
const double kMediaCrossSpacing = 2; // 列间距（网格/瀑布流/骨架屏）
const double kMediaMainSpacing = 2; // 行间距（网格/瀑布流/骨架屏）
const double kMediaJustifiedGap = 2; // 自适应（justified）布局行内间隙
const double kMediaJustifiedMainGap = 2; // 自适应（justified）布局行间距

/// 媒体缩略图卡片（无卡片背景，图片 contain/cover 显示）。
class MediaTile extends StatefulWidget {
  final Media media;
  final ApiService api;
  final double imageAspectRatio;
  final bool showName;
  final BoxFit imageFit;
  final double? cellWidth;
  final double? cellHeight;
  final VoidCallback onOpenViewer;
  final VoidCallback? onDeleted;
  final bool highlighted; // 搜索命中高亮（主色描边）

  const MediaTile({
    super.key,
    required this.media,
    required this.api,
    required this.imageAspectRatio,
    this.showName = true,
    this.imageFit = BoxFit.contain,
    this.cellWidth,
    this.cellHeight,
    required this.onOpenViewer,
    this.onDeleted,
    this.highlighted = false,
  });

  @override
  State<MediaTile> createState() => MediaTileState();
}

class MediaTileState extends State<MediaTile> {
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
          MediaImage(media: media, api: widget.api, fit: widget.imageFit),
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
              child: MediaBadge(icon: Icons.play_arrow_rounded),
            ),
            Positioned(
              right: 6,
              bottom: 6,
              child: MediaBadge(
                text:
                    '${videoQuality(media.height)}  ${formatClock(media.durationMs ?? 0)}',
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
                    QuickActionButton(
                      icon: Icons.open_in_full_rounded,
                      tooltip: '打开媒体',
                      onTap: widget.onOpenViewer,
                    ),
                    const SizedBox(width: 6),
                    QuickActionButton(
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
      Widget area;
      if (fixedSize) {
        area = SizedBox(
          width: widget.cellWidth,
          height: widget.cellHeight,
          child: stack,
        );
      } else {
        // 图片区裁剪：hover 放大（AnimatedScale）会把图片绘制到 cell 之外，
        // 在瀑布流等紧凑布局中会遮盖相邻项目，ClipRect 限制在 cell 内。
        area = ClipRect(
          child: AspectRatio(
            aspectRatio: widget.imageAspectRatio,
            child: stack,
          ),
        );
      }
      if (widget.highlighted) {
        area = Container(
          decoration: BoxDecoration(
            border: Border.all(
              color: Theme.of(context).colorScheme.primary,
              width: 2,
            ),
            borderRadius: BorderRadius.circular(10),
          ),
          child: area,
        );
      }
      return area;
    }

    return MouseRegion(
      onEnter: (_) => setState(() => _hovered = true),
      onExit: (_) => setState(() => _hovered = false),
      child: GestureDetector(
        behavior: HitTestBehavior.opaque,
        onDoubleTap: widget.onOpenViewer,
        onSecondaryTapDown:
            (details) => showMediaMenu(
              context,
              widget.api,
              media,
              details.globalPosition,
              onOpenViewer: widget.onOpenViewer,
              onDeleted: widget.onDeleted,
            ),
        // ClipRect 必须在 AnimatedScale 外层：hover 放大（1.03）会同时
        // 放大内部裁剪边界，图片仍会溢出 cell 遮盖相邻项目；
        // 用固定裁剪区域把放大绘制限制在 cell 内。
        child: ClipRect(
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
      ),
    );
  }

  void _openMoreMenu() {
    final box = context.findRenderObject() as RenderBox?;
    if (box == null) return;
    final position = box.localToGlobal(
      Offset(box.size.width / 2, box.size.height / 2),
    );
    showMediaMenu(
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
class QuickActionButton extends StatelessWidget {
  final IconData icon;
  final String tooltip;
  final VoidCallback onTap;

  const QuickActionButton({
    super.key,
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

/// 媒体缩略图（骨架垫底、错误占位）。
class MediaImage extends StatelessWidget {
  final Media media;
  final ApiService api;
  final BoxFit fit;

  const MediaImage({
    super.key,
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
          children: [const SkeletonBlock(), child],
        );
      },
      errorBuilder:
          (_, __, ___) =>
              const Center(child: Icon(Icons.broken_image_outlined, size: 36)),
    );
  }
}

/// 脉动骨架块（加载占位 / 首屏骨架网格共用）。
class SkeletonBlock extends StatefulWidget {
  const SkeletonBlock({super.key});

  @override
  State<SkeletonBlock> createState() => SkeletonBlockState();
}

class SkeletonBlockState extends State<SkeletonBlock>
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

/// 翻页/布局变化时的淡入容器（单实例替换，避免双滚动视图争用同一 controller）。
class ContentFade extends StatefulWidget {
  final String fadeKey;
  final Widget child;

  const ContentFade({super.key, required this.fadeKey, required this.child});

  @override
  State<ContentFade> createState() => ContentFadeState();
}

class ContentFadeState extends State<ContentFade>
    with SingleTickerProviderStateMixin {
  late final AnimationController _controller = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 220),
    value: 1,
  );

  @override
  void didUpdateWidget(ContentFade oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.fadeKey != widget.fadeKey) {
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

/// 页面顶部显示设置条：布局切换、缩放滑杆、每页数量、搜索框、加载模式。
/// 库页可传入 [searchQuery]/[onSearchChanged]/[onSearchClear] 显示搜索框，
/// 传入 [loadMode]/[onLoadModeChanged] 显示加载模式（翻页/懒加载）切换。
class DisplayToolbar extends StatelessWidget {
  final String layout;
  final ValueChanged<String> onLayoutChanged;
  final double thumbExtent;
  final ValueChanged<double> onThumbExtentChanged;
  final int? pageSize;
  final ValueChanged<int>? onPageSizeChanged;
  final String? searchQuery;
  final ValueChanged<String>? onSearchChanged;
  final VoidCallback? onSearchClear;
  final String? loadMode;
  final ValueChanged<String>? onLoadModeChanged;

  const DisplayToolbar({
    super.key,
    required this.layout,
    required this.onLayoutChanged,
    required this.thumbExtent,
    required this.onThumbExtentChanged,
    this.pageSize,
    this.onPageSizeChanged,
    this.searchQuery,
    this.onSearchChanged,
    this.onSearchClear,
    this.loadMode,
    this.onLoadModeChanged,
  });

  static const _layoutOptions = [
    ('adaptive', '网格自适应', Icons.dashboard_outlined),
    ('square', '网格正切', Icons.grid_view_outlined),
    ('masonry', '瀑布流', Icons.view_column_outlined),
    ('justified', '自适应', Icons.view_day_outlined),
  ];

  static const _modeOptions = [
    ('page', '翻页', Icons.pages_outlined),
    ('lazy', '懒加载', Icons.swap_vert_outlined),
  ];

  String _layoutLabel(String value) {
    for (final (key, label, _) in _layoutOptions) {
      if (key == value) return label;
    }
    return '网格自适应';
  }

  String _modeLabel(String value) {
    for (final (key, label, _) in _modeOptions) {
      if (key == value) return label;
    }
    return '翻页';
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
          if (searchQuery != null) ...[
            // 搜索框（库页专用，防抖由调用方处理）
            Expanded(
              child: SizedBox(
                height: 32,
                child: TextField(
                  style: const TextStyle(fontSize: 13),
                  onChanged: onSearchChanged,
                  textInputAction: TextInputAction.search,
                  decoration: InputDecoration(
                    isDense: true,
                    hintText: '搜索文件名',
                    hintStyle: TextStyle(fontSize: 13, color: cs.outline),
                    prefixIcon: Icon(Icons.search, size: 16, color: cs.outline),
                    suffixIcon:
                        searchQuery!.isNotEmpty
                            ? IconButton(
                              icon: Icon(
                                Icons.close,
                                size: 16,
                                color: cs.outline,
                              ),
                              tooltip: '清除',
                              onPressed: onSearchClear,
                            )
                            : null,
                    contentPadding: const EdgeInsets.symmetric(vertical: 0),
                    border: OutlineInputBorder(
                      borderRadius: BorderRadius.circular(8),
                      borderSide: BorderSide.none,
                    ),
                    filled: true,
                    fillColor: cs.surfaceContainerHighest.withValues(alpha: .5),
                  ),
                ),
              ),
            ),
            const SizedBox(width: 16),
          ],
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
          if (loadMode != null) ...[
            const SizedBox(width: 20),
            Container(width: 1, height: 18, color: cs.outlineVariant),
            const SizedBox(width: 10),
            // 加载模式（翻页 / 懒加载）
            PopupMenuButton<String>(
              tooltip: '加载方式',
              initialValue: loadMode,
              onSelected: onLoadModeChanged,
              itemBuilder:
                  (context) => [
                    for (final (key, label, icon) in _modeOptions)
                      PopupMenuItem<String>(
                        value: key,
                        child: Row(
                          children: [
                            Icon(icon, size: 18, color: cs.onSurfaceVariant),
                            const SizedBox(width: 10),
                            Text(label),
                            if (key == loadMode) ...[
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
                    Icon(Icons.swap_vert, size: 16, color: cs.onSurfaceVariant),
                    const SizedBox(width: 6),
                    Text(
                      _modeLabel(loadMode!),
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

/// 黑色半透明角标（视频播放图标/清晰度/时长）。
class MediaBadge extends StatelessWidget {
  final IconData? icon;
  final String? text;

  const MediaBadge({super.key, this.icon, this.text});

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

/// 紧凑分页栏：页码输入、上下页、总页数与总数。
class PaginationBar extends StatelessWidget {
  final int currentPage;
  final int totalPages;
  final int totalItems;
  final bool loading;
  final TextEditingController controller;
  final VoidCallback? onPrevious;
  final VoidCallback? onNext;
  final VoidCallback onSubmit;

  const PaginationBar({
    super.key,
    required this.currentPage,
    required this.totalPages,
    required this.totalItems,
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
                child: Text('/ $totalPages'),
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
                '共 $totalItems 项',
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

/// 媒体右键菜单（打开媒体/文件/目录、复制路径、删除）。
void showMediaMenu(
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
        onTap: () => copyMediaPath(context, api, media),
      ),
      const ContextMenuItem.divider(),
      ContextMenuItem(
        icon: Icons.delete_outline,
        label: '删除此文件',
        isDestructive: true,
        onTap: () => confirmDelete(context, api, media, onDeleted),
      ),
    ],
  );
}

/// 复制媒体完整本地路径到剪贴板（服务端路径由 API 提供）。
Future<void> copyMediaPath(
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
Future<void> confirmDelete(
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
        SnackBar(content: Text('已删除，释放 ${formatBytes(result.freedBytes)}')),
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

/// 打开应用内查看器（图片/视频按类型分流，保持传入列表作为队列）。
void openMediaViewer(
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

/// 按目标列宽估算列数。
int columnCount(double width, double preferredExtent) {
  return ((width + kMediaCrossSpacing) / (preferredExtent + kMediaCrossSpacing))
      .floor()
      .clamp(1, 12);
}

/// 原始宽高比（不裁剪限制），用于自适应铺满行与瀑布流高度计算。
double rawAspectRatio(Media media) {
  final width = media.width ?? 0;
  final height = media.height ?? 0;
  if (width <= 0 || height <= 0) return 4 / 3;
  return width / height;
}

/// 自适应布局：将媒体按原始宽高比分组为横向铺满的行。
List<List<(Media, int)>> buildJustifiedRows(
  List<Media> items,
  double totalWidth,
  double targetHeight,
  double gap,
) {
  final rows = <List<(Media, int)>>[];
  var current = <(Media, int)>[];
  var ratioSum = 0.0;
  for (var i = 0; i < items.length; i++) {
    final ratio = rawAspectRatio(items[i]);
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
double justifiedRowHeight(
  List<(Media, int)> row,
  double totalWidth,
  double targetHeight,
  double gap,
) {
  final ratioSum = row.fold(0.0, (s, item) => s + rawAspectRatio(item.$1));
  final available = totalWidth - gap * (row.length - 1);
  if (row.length == 1) {
    return math.min(targetHeight, available / ratioSum);
  }
  return available / ratioSum;
}

/// 计算行内每张图片的宽度（末位补足剩余宽度，避免浮点误差产生空隙）。
List<double> justifiedCellWidths(
  List<(Media, int)> row,
  double rowHeight,
  double totalWidth,
  double gap,
) {
  final widths = <double>[];
  if (row.isEmpty) return widths;
  if (row.length == 1) {
    widths.add(math.min(rawAspectRatio(row[0].$1) * rowHeight, totalWidth));
    return widths;
  }
  var used = 0.0;
  for (var c = 0; c < row.length; c++) {
    if (c == row.length - 1) {
      widths.add(totalWidth - used);
    } else {
      final w = rawAspectRatio(row[c].$1) * rowHeight;
      widths.add(w);
      used += w + gap;
    }
  }
  return widths;
}

/// 视频清晰度标签（按高度分级）。
String videoQuality(int? height) {
  final value = height ?? 0;
  if (value < 720) return '${value}P';
  if (value < 1080) return '720P';
  if (value < 1440) return '1080P';
  if (value < 2160) return '2K';
  return '4K';
}

/// 时长时钟格式（mm:ss 或 h:mm:ss）。
String formatClock(int ms) {
  final seconds = ms ~/ 1000;
  final hours = seconds ~/ 3600;
  final minutes = (seconds % 3600) ~/ 60;
  final remaining = seconds % 60;
  if (hours > 0) {
    return '$hours:${minutes.toString().padLeft(2, '0')}:${remaining.toString().padLeft(2, '0')}';
  }
  return '$minutes:${remaining.toString().padLeft(2, '0')}';
}

/// 字节数可读格式。
String formatBytes(int bytes) =>
    bytes < 1024
        ? '$bytes B'
        : bytes < 1024 * 1024
        ? '${(bytes / 1024).toStringAsFixed(1)} KB'
        : bytes < 1024 * 1024 * 1024
        ? '${(bytes / 1048576).toStringAsFixed(1)} MB'
        : '${(bytes / 1073741824).toStringAsFixed(2)} GB';

/// 时长可读格式（分钟/小时）。
String formatDuration(int ms) {
  final hours = ms ~/ 3600000;
  final minutes = (ms % 3600000) ~/ 60000;
  return hours == 0 ? '$minutes 分钟' : '$hours 小时 $minutes 分';
}
