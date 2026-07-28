// top_search_bar.dart：顶部搜索栏组件
// 代码注释使用中文
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

/// 顶部搜索栏，支持 Ctrl+F 快捷键聚焦
class TopSearchBar extends StatefulWidget {
  final String hintText;
  final ValueChanged<String>? onSubmitted;
  final TextEditingController? controller;

  const TopSearchBar({
    super.key,
    this.hintText = '搜索...',
    this.onSubmitted,
    this.controller,
  });

  @override
  State<TopSearchBar> createState() => _TopSearchBarState();
}

class _TopSearchBarState extends State<TopSearchBar> {
  late final TextEditingController _ctrl;
  final _focusNode = FocusNode();
  bool _focused = false;

  @override
  void initState() {
    super.initState();
    _ctrl = widget.controller ?? TextEditingController();
    _focusNode.addListener(() {
      setState(() => _focused = _focusNode.hasFocus);
    });
  }

  @override
  void dispose() {
    if (widget.controller == null) _ctrl.dispose();
    _focusNode.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Shortcuts(
      shortcuts: {
        const SingleActivator(LogicalKeyboardKey.keyF, control: true):
            const _FocusSearchIntent(),
      },
      child: Actions(
        actions: {
          _FocusSearchIntent: CallbackAction<_FocusSearchIntent>(
            onInvoke: (_) {
              _focusNode.requestFocus();
              return null;
            },
          ),
        },
        child: SizedBox(
          height: 36,
          child: TextField(
            controller: _ctrl,
            focusNode: _focusNode,
            style: const TextStyle(fontSize: 13),
            decoration: InputDecoration(
              hintText: widget.hintText,
              hintStyle: TextStyle(color: cs.outline),
              prefixIcon: Icon(Icons.search, size: 18, color: cs.outline),
              suffixIcon: _focused
                  ? IconButton(
                      icon: const Icon(Icons.close, size: 16),
                      onPressed: () {
                        _ctrl.clear();
                        _focusNode.unfocus();
                      },
                    )
                  : Padding(
                      padding: const EdgeInsets.only(right: 8),
                      child: Text(
                        'Ctrl+F',
                        style: TextStyle(fontSize: 11, color: cs.outline),
                      ),
                    ),
              filled: true,
              fillColor: _focused ? cs.surface : cs.surfaceContainerHighest.withValues(alpha: 0.3),
              border: OutlineInputBorder(
                borderRadius: BorderRadius.circular(8),
                borderSide: _focused
                    ? BorderSide(color: cs.primary, width: 1.5)
                    : BorderSide.none,
              ),
              enabledBorder: OutlineInputBorder(
                borderRadius: BorderRadius.circular(8),
                borderSide: BorderSide.none,
              ),
              focusedBorder: OutlineInputBorder(
                borderRadius: BorderRadius.circular(8),
                borderSide: BorderSide(color: cs.primary, width: 1.5),
              ),
              contentPadding: const EdgeInsets.symmetric(horizontal: 12, vertical: 0),
            ),
            onSubmitted: widget.onSubmitted,
          ),
        ),
      ),
    );
  }
}

class _FocusSearchIntent extends Intent {
  const _FocusSearchIntent();
}
