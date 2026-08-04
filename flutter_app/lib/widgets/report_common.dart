// report_common.dart：重复报告 / 目录对比共用的展示组件与工具函数。
// 代码注释使用中文。
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../models/models.dart';
import '../services/api_service.dart';
import 'context_menu.dart';

// ===== 工具函数 =====

String formatBytes(int bytes) {
  if (bytes < 1024) return '$bytes B';
  if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)} KB';
  if (bytes < 1024 * 1024 * 1024) {
    return '${(bytes / 1024 / 1024).toStringAsFixed(1)} MB';
  }
  return '${(bytes / 1024 / 1024 / 1024).toStringAsFixed(2)} GB';
}

/// 相对路径的父目录（根目录直属文件返回 ""）。
String relDir(String rel) {
  final norm = rel.replaceAll('\\', '/');
  final idx = norm.lastIndexOf('/');
  return idx < 0 ? '' : norm.substring(0, idx);
}

/// 其它目录菜单项标签：显示父路径（根目录直属文件显示 "/"）。
String dirLabel(DuplicateItem other) {
  final dir = relDir(other.relativePath);
  return dir.isEmpty ? '/' : dir;
}

String fileName(String path) {
  final norm = path.replaceAll('\\', '/');
  return norm.substring(norm.lastIndexOf('/') + 1);
}

String keepLabel(String keep) {
  switch (keep) {
    case 'largest':
      return '保留最大文件';
    case 'smallest':
      return '保留最小文件';
    case 'newest':
      return '保留最大修改时间';
    case 'oldest':
      return '保留最小修改时间';
    case 'longest_name':
      return '保留最长文件名';
    case 'shortest_name':
      return '保留最短文件名';
    case 'keep_current':
      return '保留当前文件';
    default:
      return keep;
  }
}

String meta(DuplicateItem item) {
  final size = formatBytes(item.fileSize);
  if (item.kind == 'video' && item.durationMs != null) {
    final seconds = item.durationMs! ~/ 1000;
    return '${seconds ~/ 60}:${(seconds % 60).toString().padLeft(2, '0')} · $size';
  }
  if (item.width != null && item.height != null) {
    return '${item.width}×${item.height} · $size';
  }
  return size;
}

// ===== 保留条件对话框 =====

/// 六种保留条件选择对话框，返回 keep 值；取消返回 null。
/// 说明文案：本目录内互相重复的文件每组保留 1 个；仅与其它目录重复的文件直接删除本目录这份。
Future<String?> showKeepDialog(
  BuildContext context, {
  required String title,
  required int count,
}) {
  String keep = 'largest';
  return showDialog<String>(
    context: context,
    builder:
        (ctx) => StatefulBuilder(
          builder:
              (ctx, setLocal) => AlertDialog(
                title: Text(title),
                content: SizedBox(
                  width: 360,
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text(
                        '共 $count 个文件；本目录内重复的每组保留 1 个，'
                        '仅与其它目录重复的文件将直接删除',
                        style: const TextStyle(fontSize: 12),
                      ),
                      const SizedBox(height: 8),
                      for (final k in const [
                        'largest',
                        'smallest',
                        'newest',
                        'oldest',
                        'longest_name',
                        'shortest_name',
                      ])
                        RadioListTile<String>(
                          value: k,
                          groupValue: keep,
                          dense: true,
                          title: Text(
                            keepLabel(k),
                            style: const TextStyle(fontSize: 13),
                          ),
                          onChanged: (v) => setLocal(() => keep = v ?? keep),
                        ),
                    ],
                  ),
                ),
                actions: [
                  TextButton(
                    onPressed: () => Navigator.of(ctx).pop(),
                    child: const Text('取消'),
                  ),
                  FilledButton(
                    onPressed: () => Navigator.of(ctx).pop(keep),
                    child: const Text('确定清除'),
                  ),
                ],
              ),
        ),
  );
}

/// 清除确认对话框，确认返回 true。
Future<bool> confirmClearDialog(
  BuildContext context, {
  required String title,
  required String keep,
  required int count,
}) async {
  final result = await showDialog<bool>(
    context: context,
    builder:
        (ctx) => AlertDialog(
          title: Text(title),
          content: Text(
            '将删除 $count 个文件（含生成的缩略图、media 表数据、本地文件）\n'
            '保留条件：${keepLabel(keep)}',
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.of(ctx).pop(false),
              child: const Text('取消'),
            ),
            FilledButton(
              style: FilledButton.styleFrom(
                backgroundColor: Theme.of(ctx).colorScheme.error,
              ),
              onPressed: () => Navigator.of(ctx).pop(true),
              child: const Text('确定清除'),
            ),
          ],
        ),
  );
  return result ?? false;
}

/// 客户端保留条件选择（与后端一致）：最大/最小文件、最新/最旧修改时间、最长/最短文件名。
int pickKeepIndex(List<DuplicateItem> items, String keep) {
  if (items.isEmpty) return 0;
  var best = 0;
  for (var i = 1; i < items.length; i++) {
    final a = items[i];
    final b = items[best];
    final better = switch (keep) {
      'largest' => a.fileSize > b.fileSize,
      'smallest' => a.fileSize < b.fileSize,
      'newest' =>
        (a.mtime != null && b.mtime != null && a.mtime!.isAfter(b.mtime!)) ||
            (a.mtime != null && b.mtime == null),
      'oldest' =>
        (a.mtime != null && b.mtime != null && a.mtime!.isBefore(b.mtime!)) ||
            (b.mtime != null && a.mtime == null),
      'longest_name' => fileName(a.fullPath).length > fileName(b.fullPath).length,
      'shortest_name' => fileName(a.fullPath).length < fileName(b.fullPath).length,
      _ => false,
    };
    if (better) best = i;
  }
  return best;
}

// ===== 展示组件 =====

/// 透明胶囊：分组头部的父路径标签，点击打开该目录下代表文件并选中。
class DirChip extends StatelessWidget {
  final String label;
  final VoidCallback onTap;

  const DirChip({super.key, required this.label, required this.onTap});

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(12),
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
        decoration: BoxDecoration(
          color: cs.primary.withValues(alpha: 0.04),
          border: Border.all(color: cs.outlineVariant),
          borderRadius: BorderRadius.circular(12),
        ),
        child: Text(
          label,
          style: TextStyle(fontSize: 11, color: cs.onSurfaceVariant),
        ),
      ),
    );
  }
}

/// 懒加载缩略图：加载中显示占位骨架，完成后淡入，避免大图库一次性请求全部图片。
class LazyThumb extends StatelessWidget {
  final String url;
  final int cacheWidth;

  const LazyThumb({super.key, required this.url, this.cacheWidth = 400});

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Image.network(
      url,
      fit: BoxFit.cover,
      gaplessPlayback: true,
      cacheWidth: cacheWidth,
      loadingBuilder: (context, child, progress) {
        if (progress == null) return child;
        return ColoredBox(
          color: cs.surfaceContainerHighest,
          child: Center(
            child: SizedBox(
              width: 22,
              height: 22,
              child: CircularProgressIndicator(
                strokeWidth: 2,
                color: cs.outline,
              ),
            ),
          ),
        );
      },
      frameBuilder: (context, child, frame, wasSynchronouslyLoaded) {
        if (wasSynchronouslyLoaded) return child;
        return AnimatedOpacity(
          opacity: frame == null ? 0 : 1,
          duration: const Duration(milliseconds: 200),
          child: child,
        );
      },
      errorBuilder:
          (_, __, ___) => ColoredBox(
            color: cs.surfaceContainerHighest,
            child: Icon(
              Icons.broken_image_outlined,
              color: cs.outline,
              size: 30,
            ),
          ),
    );
  }
}

/// card_swiper 叠卡：同一重复组的多张缩略图沿 45° 方向平移堆叠
/// （每张相对前一张同时向下/向右偏移相同距离），数量徽标显示在图片上方。
class StackedCluster extends StatelessWidget {
  final List<DuplicateItem> items;
  final ApiService api;
  final VoidCallback onTap;
  final ValueChanged<Offset>? onSecondaryTap;
  final double width;

  const StackedCluster({
    super.key,
    required this.items,
    required this.api,
    required this.onTap,
    this.onSecondaryTap,
    this.width = 300,
  });

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    // 叠卡最多展示 3 张，数量只影响内部层数，不改变外框尺寸
    final preview = items.take(3).toList();
    final first = items.first;

    const clusterRadius = 16.0;
    const contentPadding = 10.0;
    const imageW = 248.0;
    const imageH = 166.0;
    const stackStep = 14.0;
    const imageRadius = 12.0;
    const imageAreaH = 208.0;

    return Container(
      width: width,
      height: 272,
      clipBehavior: Clip.antiAlias,
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(clusterRadius),
        border: Border.all(color: cs.outlineVariant),
        boxShadow: [
          BoxShadow(
            color: Colors.black.withValues(alpha: 0.08),
            blurRadius: 8,
            offset: const Offset(0, 3),
          ),
        ],
      ),
      child: GestureDetector(
        onSecondaryTapDown:
            onSecondaryTap == null
                ? null
                : (d) => onSecondaryTap!(d.globalPosition),
        onLongPressStart:
            onSecondaryTap == null
                ? null
                : (d) => onSecondaryTap!(d.globalPosition),
        child: Material(
          color: cs.surface,
          child: InkWell(
            onTap: onTap,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                // 图片叠卡区域（固定高度，内部层数不影响布局）
                SizedBox(
                  height: imageAreaH,
                  child: Stack(
                    clipBehavior: Clip.hardEdge,
                    children: [
                      for (var i = preview.length - 1; i >= 0; i--)
                        Positioned(
                          left: contentPadding + i * stackStep,
                          top: contentPadding + i * stackStep,
                          child: Opacity(
                            opacity: i == 0 ? 1.0 : 0.94,
                            child: Container(
                              width: imageW,
                              height: imageH,
                              clipBehavior: Clip.antiAlias,
                              decoration: BoxDecoration(
                                color: cs.surfaceContainerHighest,
                                borderRadius: BorderRadius.circular(imageRadius),
                                border: Border.all(color: cs.outlineVariant),
                                boxShadow: [
                                  BoxShadow(
                                    color: Colors.black.withValues(
                                      alpha: i == 0 ? 0.12 : 0.06,
                                    ),
                                    blurRadius: i == 0 ? 6 : 3,
                                    offset: const Offset(0, 2),
                                  ),
                                ],
                              ),
                              child: _thumb(preview[i], cs),
                            ),
                          ),
                        ),
                      // 数量信息胶囊：白色文字 + 极淡深色底，保证在图片上有对比度，
                      // 深浅主题下均清晰可读
                      Positioned(
                        right: 12,
                        top: 12,
                        child: DecoratedBox(
                          decoration: BoxDecoration(
                            color: Colors.black.withValues(
                              alpha:
                                  cs.brightness == Brightness.dark ? 0.45 : 0.28,
                            ),
                            borderRadius: BorderRadius.circular(10),
                            border: Border.all(
                              color: cs.primary.withValues(alpha: 0.35),
                            ),
                          ),
                          child: Padding(
                            padding: const EdgeInsets.symmetric(
                              horizontal: 9,
                              vertical: 5,
                            ),
                            child: Row(
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                const Icon(
                                  Icons.layers_outlined,
                                  size: 14,
                                  color: Colors.white,
                                ),
                                const SizedBox(width: 5),
                                Text(
                                  '${items.length} 个文件',
                                  style: TextStyle(
                                    fontSize: 11,
                                    fontWeight: FontWeight.w600,
                                    color: Colors.white,
                                    shadows: [
                                      Shadow(
                                        color: Colors.black.withValues(
                                          alpha: 0.35,
                                        ),
                                        blurRadius: 2,
                                      ),
                                    ],
                                  ),
                                ),
                              ],
                            ),
                          ),
                        ),
                      ),
                    ],
                  ),
                ),
                // 文件信息区（独立区域，避免内容漂浮）
                Expanded(
                  child: Padding(
                    padding: const EdgeInsets.fromLTRB(12, 4, 12, 10),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      mainAxisAlignment: MainAxisAlignment.center,
                      children: [
                        Text(
                          fileName(first.fullPath),
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(
                            fontSize: 12,
                            fontWeight: FontWeight.w500,
                            color: cs.onSurface,
                          ),
                        ),
                        const SizedBox(height: 2),
                        Text(
                          meta(first),
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(
                            fontSize: 10,
                            color: cs.onSurfaceVariant,
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Widget _thumb(DuplicateItem item, ColorScheme cs) {
    if (item.thumbnailPath == null || item.thumbnailPath!.isEmpty) {
      return ColoredBox(
        color: cs.surfaceContainerHighest,
        child: Icon(
          item.kind == 'video' ? Icons.videocam_outlined : Icons.image_outlined,
          color: cs.outline,
        ),
      );
    }
    return LazyThumb(
      url: api.thumbnailUrl(item.kind, item.thumbnailPath!),
      cacheWidth: 400,
    );
  }
}

/// 成员右键菜单构建器（目录对比等其它报告源传入自定义菜单；null 使用默认重复报告菜单）。
typedef MemberMenuBuilder =
    void Function(BuildContext context, Offset position, DuplicateItem item);

/// 单个重复文件卡片（右键菜单按模式区分）。
class DuplicateMemberCard extends StatelessWidget {
  final DuplicateItem item;
  final ApiService api;
  final bool sameDirMode;
  final bool showOtherPaths;
  final DuplicateGroupItem? group;
  final VoidCallback? onTap;
  final ValueChanged<String> onError;
  final void Function(List<int> deletedIds) onDeleted;
  final MemberMenuBuilder? menuBuilder;
  final Widget? badge; // 可选角标（如目录对比的"所选目录"标识）

  const DuplicateMemberCard({
    super.key,
    required this.item,
    required this.api,
    required this.sameDirMode,
    required this.showOtherPaths,
    this.group,
    this.onTap,
    required this.onError,
    required this.onDeleted,
    this.menuBuilder,
    this.badge,
  });

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return GestureDetector(
      onTap: onTap,
      onSecondaryTapDown:
          (details) => _showMenu(context, details.globalPosition),
      onLongPressStart: (details) => _showMenu(context, details.globalPosition),
      child: Card(
        clipBehavior: Clip.antiAlias,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Expanded(
              child: Stack(
                fit: StackFit.expand,
                children: [
                  _thumb(cs),
                  if (badge != null)
                    Positioned(top: 6, left: 6, child: badge!),
                ],
              ),
            ),
            Padding(
              padding: const EdgeInsets.all(8),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    fileName(item.fullPath),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(fontSize: 11),
                  ),
                  const SizedBox(height: 2),
                  Text(
                    meta(item),
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
    );
  }

  Widget _thumb(ColorScheme cs) {
    if (item.thumbnailPath == null || item.thumbnailPath!.isEmpty) {
      return ColoredBox(
        color: cs.surfaceContainerHighest,
        child: Icon(
          item.kind == 'video' ? Icons.videocam_outlined : Icons.image_outlined,
          color: cs.outline,
          size: 36,
        ),
      );
    }
    return LazyThumb(
      url: api.thumbnailUrl(item.kind, item.thumbnailPath!),
      cacheWidth: 400,
    );
  }

  void _showMenu(BuildContext context, Offset position) {
    if (menuBuilder != null) {
      menuBuilder!(context, position, item);
      return;
    }
    if (sameDirMode) {
      // 仅同一目录模式：右键打开文件路径；展开详情后另有删除项
      showContextMenu(
        context: context,
        position: position,
        items: [
          ContextMenuItem(
            icon: Icons.folder_open,
            label: '打开文件路径',
            onTap: () => _open(true),
          ),
          ContextMenuItem(icon: Icons.copy, label: '复制文件路径', onTap: _copyPath),
          ContextMenuItem(
            icon:
                item.kind == 'video'
                    ? Icons.play_circle_outline
                    : Icons.image_outlined,
            label: item.kind == 'video' ? '打开视频' : '打开图片',
            onTap: () => _open(false),
          ),
          const ContextMenuItem.divider(),
          ContextMenuItem(
            icon: Icons.block,
            label: '排除重复',
            onTap: () => _exclude(context, item),
          ),
          if (group != null && group!.items.length > 1)
            ContextMenuItem(
              icon: Icons.delete_forever_outlined,
              label: '删除此文件外本组重复文件',
              isDestructive: true,
              onTap: () => onDeletedExcept(context),
            ),
        ],
      );
      return;
    }

    // 全部数据模式
    // 其它目录菜单项按父路径去重：同一父目录只保留一个代表项，标签显示父路径。
    final otherPaths =
        group == null
            ? <DuplicateItem>[]
            : group!.items
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
          onTap: () => _open(true),
        ),
        ContextMenuItem(icon: Icons.copy, label: '复制文件路径', onTap: _copyPath),
        ContextMenuItem(
          icon:
              item.kind == 'video'
                  ? Icons.play_circle_outline
                  : Icons.image_outlined,
          label: item.kind == 'video' ? '打开视频' : '打开图片',
          onTap: () => _open(false),
        ),
        ContextMenuItem(
          icon: Icons.block,
          label: '排除重复',
          onTap: () => _exclude(context, item),
        ),
        if (showOtherPaths && otherDirItems.isNotEmpty) ...[
          const ContextMenuItem.divider(),
          for (final other in otherDirItems)
            ContextMenuItem(
              icon: Icons.subdirectory_arrow_right,
              label: dirLabel(other),
              onTap: () => _showPathSubmenu(context, position, other),
            ),
        ],
      ],
    );
  }

  /// 二级菜单：对其它目录中的重复文件执行打开/复制/打开媒体。
  void _showPathSubmenu(
    BuildContext context,
    Offset position,
    DuplicateItem other,
  ) {
    showContextMenu(
      context: context,
      position: position,
      items: [
        ContextMenuItem(
          icon: Icons.folder_open,
          label: '打开文件路径',
          onTap: () => _openFor(other, true),
        ),
        ContextMenuItem(
          icon: Icons.copy,
          label: '复制文件路径',
          onTap: () => _copyPathFor(other),
        ),
        ContextMenuItem(
          icon:
              other.kind == 'video'
                  ? Icons.play_circle_outline
                  : Icons.image_outlined,
          label: other.kind == 'video' ? '打开视频' : '打开图片',
          onTap: () => _openFor(other, false),
        ),
        ContextMenuItem(
          icon: Icons.block,
          label: '排除重复',
          onTap: () => _exclude(context, other),
        ),
      ],
    );
  }

  void _copyPath() => _copyPathFor(item);

  void _copyPathFor(DuplicateItem target) async {
    await Clipboard.setData(ClipboardData(text: target.fullPath));
  }

  Future<void> _open(bool directory) async {
    try {
      directory
          ? await api.openMediaDirectory(item.id)
          : await api.openMediaFile(item.id);
    } catch (e) {
      onError('打开失败: $e');
    }
  }

  Future<void> _openFor(DuplicateItem target, bool directory) async {
    try {
      directory
          ? await api.openMediaDirectory(target.id)
          : await api.openMediaFile(target.id);
    } catch (e) {
      onError('打开失败: $e');
    }
  }

  /// 排除重复：人工判定该文件无重复，将其从当前报告中移除。
  /// 仅对当前报告生效——重新生成报告后该文件重新参与检测。
  Future<void> _exclude(BuildContext context, DuplicateItem target) async {
    try {
      await api.excludeDuplicateMedia(target.id);
      // 复用父级乐观更新：从本地状态移除该文件（组员 <2 的组自动丢弃）
      onDeleted([target.id]);
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text('已排除该文件：不再计入当前报告的重复统计，重新生成报告后恢复参与检测'),
          ),
        );
      }
    } catch (e) {
      onError('排除重复失败: $e');
    }
  }

  void onDeletedExcept(BuildContext context) {
    final g = group;
    if (g == null) return;
    // 删除此文件外本组重复文件：调用上层确认与删除
    showDialog<void>(
      context: context,
      builder:
          (ctx) => AlertDialog(
            title: const Text('删除此文件外本组重复文件'),
            content: Text(
              '将删除 ${g.items.length - 1} 个文件（含缩略图、media 记录、本地文件），保留当前文件。',
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
                  _deleteExcept(context, g);
                },
                child: const Text('确定删除'),
              ),
            ],
          ),
    );
  }

  Future<void> _deleteExcept(BuildContext context, DuplicateGroupItem g) async {
    final others =
        g.items.where((m) => m.id != item.id).map((m) => m.id).toList();
    if (others.isEmpty) return;
    try {
      final result = await api.deleteMedia(others);
      onDeleted(others);
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              '已删除 ${result.deletedFiles} 个文件，释放 ${formatBytes(result.freedBytes)}',
            ),
          ),
        );
      }
    } catch (e) {
      onError('删除失败: $e');
    }
  }
}
