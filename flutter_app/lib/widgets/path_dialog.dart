// path_dialog.dart：路径手动输入对话框（Web 或目录选择器不可用时使用）
// 代码注释使用中文
import 'package:flutter/material.dart';

/// 手动输入服务端可访问的本地绝对路径。
///
/// Flutter Web 无法通过浏览器获取服务端本机的真实目录路径，
/// 或某些平台未实现 `file_picker.getDirectoryPath()` 时使用此对话框。
class PathDialog extends StatefulWidget {
  final String title;
  final String labelText;
  final String hintText;
  final String confirmText;
  final String description;

  const PathDialog({
    super.key,
    this.title = '输入目录路径',
    this.labelText = '目录路径',
    this.hintText = r'D:\Pictures 或 E:\Videos',
    this.confirmText = '确认',
    this.description = '请输入 Go 服务端所在电脑可访问的绝对路径。',
  });

  @override
  State<PathDialog> createState() => _PathDialogState();
}

class _PathDialogState extends State<PathDialog> {
  final TextEditingController _ctrl = TextEditingController();

  @override
  void dispose() {
    _ctrl.dispose();
    super.dispose();
  }

  void _submit() {
    final path = _ctrl.text.trim();
    if (path.isNotEmpty) {
      Navigator.pop(context, path);
    }
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: Text(widget.title),
      content: SizedBox(
        width: 440,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(widget.description, style: const TextStyle(fontSize: 13)),
            const SizedBox(height: 12),
            TextField(
              controller: _ctrl,
              autofocus: true,
              decoration: InputDecoration(
                labelText: widget.labelText,
                hintText: widget.hintText,
              ),
              onSubmitted: (_) => _submit(),
            ),
          ],
        ),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.pop(context), child: const Text('取消')),
        FilledButton(onPressed: _submit, child: Text(widget.confirmText)),
      ],
    );
  }
}
