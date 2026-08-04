// context_menu.dart：右键上下文菜单组件（单 OverlayEntry 管理整棵菜单栈）
// 支持任意层级子菜单；点击菜单外围任意位置一次即可关闭全部层级菜单。
// 代码注释使用中文
import 'package:flutter/material.dart';

/// 菜单项数据
class ContextMenuItem {
  final IconData icon;
  final String label;
  final String? shortcut;
  final VoidCallback? onTap;
  final bool isDestructive;
  final bool isDivider;
  final List<ContextMenuItem>? subItems;

  const ContextMenuItem({
    required this.icon,
    required this.label,
    this.shortcut,
    this.onTap,
    this.isDestructive = false,
    this.isDivider = false,
    this.subItems,
  });

  const ContextMenuItem.divider()
    : icon = Icons.minimize,
      label = '',
      shortcut = null,
      onTap = null,
      isDestructive = false,
      isDivider = true,
      subItems = null;
}

/// 单个菜单的定位与数据（根菜单或某级子菜单）。
class _MenuLayer {
  final Offset position; // 菜单左上角（全局坐标）
  final List<ContextMenuItem> items;
  _MenuLayer(this.position, this.items);
}

/// 显示右键上下文菜单。
/// 整棵菜单（含各级子菜单）共用一个 OverlayEntry，点击外围 barrier 一次全部关闭。
void showContextMenu({
  required BuildContext context,
  required Offset position,
  required List<ContextMenuItem> items,
}) {
  final overlay = Overlay.of(context);
  late final OverlayEntry entry;
  entry = OverlayEntry(
    builder:
        (_) => _MenuOverlay(position: position, items: items, entry: entry),
  );
  overlay.insert(entry);
}

/// 菜单根节点（持 OverlayEntry 引用，关闭时自行移除）。
class _MenuOverlay extends StatefulWidget {
  final Offset position;
  final List<ContextMenuItem> items;
  final OverlayEntry entry;

  const _MenuOverlay({
    required this.position,
    required this.items,
    required this.entry,
  });

  @override
  State<_MenuOverlay> createState() => _MenuOverlayState();
}

class _MenuOverlayState extends State<_MenuOverlay> {
  /// 已展开的子菜单栈（每级一条，顺序即层级）。
  final List<_MenuLayer> _stack = [];

  /// 关闭全部菜单并移除 OverlayEntry。
  void _closeAll() {
    if (widget.entry.mounted) {
      widget.entry.remove();
    }
  }

  /// 在某级菜单的指定项右侧展开子菜单；同层已有子菜单则替换。
  void _openSub(
    int depth,
    Offset parentTopRight,
    List<ContextMenuItem> subItems,
  ) {
    final screen = MediaQuery.of(context).size;
    // 预估菜单宽高：按内容估算（图标 16 + 文字 + 快捷键 + 内边距）。
    final estWidth = _estimateWidth(subItems);
    final estHeight = _estimateHeight(subItems);
    var dx = parentTopRight.dx + 4;
    var dy = parentTopRight.dy;
    // 贴屏幕右缘时左移；超出下缘时上移。
    if (dx + estWidth > screen.width) dx = parentTopRight.dx - estWidth - 4;
    if (dy < 0) dy = 0;
    if (dy + estHeight > screen.height) dy = screen.height - estHeight;
    setState(() {
      // 清掉比该层更深/同级旧的展开，保留更浅层级。
      _stack.removeRange(depth, _stack.length);
      _stack.add(_MenuLayer(Offset(dx, dy), subItems));
    });
  }

  int _estimateHeight(List<ContextMenuItem> items) {
    var h = 8;
    for (final it in items) {
      h += it.isDivider ? 1 : 38;
    }
    return h;
  }

  int _estimateWidth(List<ContextMenuItem> items) {
    var w = 60;
    for (final it in items) {
      final textLen = it.label.length * 14; // 估算：13px 字号
      final iconW = it.isDivider ? 0 : 16 + 10;
      final shortcutW = it.shortcut != null ? 40 : 0;
      final arrowW = it.subItems != null ? 20 : 0;
      final itemW = iconW + textLen + shortcutW + arrowW + 28;
      if (itemW > w) w = itemW;
    }
    return w;
  }

  @override
  Widget build(BuildContext context) {
    return Stack(
      children: [
        // 全屏透明 barrier：点击外围一次关闭全部层级菜单。
        Positioned.fill(
          child: GestureDetector(
            behavior: HitTestBehavior.translucent,
            onTap: _closeAll,
          ),
        ),
        // 根菜单
        Positioned(
          left: widget.position.dx,
          top: widget.position.dy,
          child: _buildMenu(
            0,
            widget.items,
            _MenuLayer(widget.position, widget.items),
          ),
        ),
        // 各层子菜单（从根开始逐个绘制，后画者在上层）
        for (int i = 0; i < _stack.length; i++)
          Positioned(
            left: _stack[i].position.dx,
            top: _stack[i].position.dy,
            child: _buildMenu(i + 1, _stack[i].items, _stack[i]),
          ),
      ],
    );
  }

  /// 构建一层菜单。depth=0 为根菜单。
  Widget _buildMenu(int depth, List<ContextMenuItem> items, _MenuLayer layer) {
    final cs = Theme.of(context).colorScheme;
    return Material(
      color: cs.surfaceContainerLow,
      elevation: 4,
      borderRadius: BorderRadius.circular(8),
      clipBehavior: Clip.antiAlias,
      child: IntrinsicWidth(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            for (int i = 0; i < items.length; i++)
              items[i].isDivider
                  ? const Divider(height: 1)
                  : _MenuItemWidget(
                    item: items[i],
                    colorScheme: cs,
                    depth: depth,
                    layer: layer,
                    onExpand: _openSub,
                    onCloseAll: _closeAll,
                  ),
          ],
        ),
      ),
    );
  }
}

/// 单个菜单项：支持 hover/点击展开子菜单。
class _MenuItemWidget extends StatefulWidget {
  final ContextMenuItem item;
  final ColorScheme colorScheme;
  final int depth; // 所在菜单层级
  final _MenuLayer layer; // 所在菜单层（用于计算子菜单定位）
  final void Function(
    int depth,
    Offset parentTopRight,
    List<ContextMenuItem> subItems,
  )
  onExpand;
  final VoidCallback onCloseAll;

  const _MenuItemWidget({
    required this.item,
    required this.colorScheme,
    required this.depth,
    required this.layer,
    required this.onExpand,
    required this.onCloseAll,
  });

  @override
  State<_MenuItemWidget> createState() => _MenuItemWidgetState();
}

class _MenuItemWidgetState extends State<_MenuItemWidget> {
  final GlobalKey _key = GlobalKey();

  /// 在条目右侧展开子菜单（悬停或点击父项时触发）。
  void _openSub() {
    final ctx = _key.currentContext;
    if (ctx == null || !ctx.mounted) return;
    final box = ctx.findRenderObject() as RenderBox?;
    if (box == null) return;
    final topRight = box.localToGlobal(Offset(box.size.width, 0));
    widget.onExpand(widget.depth, topRight, widget.item.subItems!);
  }

  void _handleTap() {
    final sub = widget.item.subItems;
    if (sub != null) {
      _openSub();
      return;
    }
    final onTap = widget.item.onTap;
    widget.onCloseAll();
    onTap?.call();
  }

  @override
  Widget build(BuildContext context) {
    final color =
        widget.item.isDestructive
            ? const Color(0xFFEF4444)
            : widget.colorScheme.onSurface;
    final hasSub = widget.item.subItems != null;
    return MouseRegion(
      key: _key,
      cursor: hasSub ? SystemMouseCursors.basic : MouseCursor.defer,
      onEnter: hasSub ? (_) => _openSub() : null,
      child: InkWell(
        borderRadius: BorderRadius.circular(6),
        onTap: _handleTap,
        child: Container(
          height: 38,
          padding: const EdgeInsets.symmetric(horizontal: 10),
          child: Row(
            children: [
              SizedBox(
                width: 16,
                child: Icon(widget.item.icon, size: 16, color: color),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  widget.item.label,
                  style: TextStyle(fontSize: 13, color: color),
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              if (widget.item.shortcut != null)
                Text(
                  widget.item.shortcut!,
                  style: TextStyle(
                    fontSize: 11,
                    color: widget.colorScheme.outline,
                  ),
                ),
              if (hasSub) ...[
                const SizedBox(width: 6),
                Icon(
                  Icons.chevron_right,
                  size: 14,
                  color: widget.colorScheme.outline,
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}
