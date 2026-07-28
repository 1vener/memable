// context_menu.dart：右键上下文菜单组件
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

  const ContextMenuItem({
    required this.icon,
    required this.label,
    this.shortcut,
    this.onTap,
    this.isDestructive = false,
    this.isDivider = false,
  });

  const ContextMenuItem.divider()
      : icon = Icons.minimize,
        label = '',
        shortcut = null,
        onTap = null,
        isDestructive = false,
        isDivider = true;
}

/// 显示右键上下文菜单
void showContextMenu({
  required BuildContext context,
  required Offset position,
  required List<ContextMenuItem> items,
}) {
  final cs = Theme.of(context).colorScheme;

  showMenu<int>(
    context: context,
    position: RelativeRect.fromLTRB(
      position.dx, position.dy, position.dx + 1, position.dy + 1,
    ),
    items: [
      for (int i = 0; i < items.length; i++)
        if (items[i].isDivider)
          const PopupMenuDivider(height: 1)
        else
          PopupMenuItem<int>(
            value: i,
            height: 36,
            child: _MenuItemWidget(item: items[i], colorScheme: cs),
          ),
    ],
    shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
    elevation: 4,
  ).then((value) {
    if (value != null && value < items.length) {
      items[value].onTap?.call();
    }
  });
}

class _MenuItemWidget extends StatelessWidget {
  final ContextMenuItem item;
  final ColorScheme colorScheme;

  const _MenuItemWidget({required this.item, required this.colorScheme});

  @override
  Widget build(BuildContext context) {
    final color = item.isDestructive ? const Color(0xFFEF4444) : colorScheme.onSurface;
    return Row(
      children: [
        Icon(item.icon, size: 16, color: color),
        const SizedBox(width: 10),
        Expanded(
          child: Text(
            item.label,
            style: TextStyle(fontSize: 13, color: color),
          ),
        ),
        if (item.shortcut != null)
          Text(
            item.shortcut!,
            style: TextStyle(fontSize: 11, color: colorScheme.outline),
          ),
      ],
    );
  }
}
