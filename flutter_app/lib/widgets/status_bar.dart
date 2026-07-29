// status_bar.dart：底部状态栏组件
// 代码注释使用中文
import 'package:flutter/material.dart';

/// 底部状态栏
class StatusBar extends StatelessWidget {
  final String? apiStatus; // 'ok' | 'error' | null(未知)
  final String? currentLibrary;
  final String? scanProgress;

  const StatusBar({
    super.key,
    this.apiStatus,
    this.currentLibrary,
    this.scanProgress,
  });

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Container(
      height: 28,
      padding: const EdgeInsets.symmetric(horizontal: 16),
      decoration: BoxDecoration(
        color: cs.surface,
        border: Border(top: BorderSide(color: cs.outlineVariant, width: 0.5)),
      ),
      child: Row(
        children: [
          // API 状态
          Icon(
            Icons.circle,
            size: 8,
            color: apiStatus == 'ok'
                ? const Color(0xFF22C55E)
                : apiStatus == 'error'
                    ? const Color(0xFFEF4444)
                    : const Color(0xFF94A3B8),
          ),
          const SizedBox(width: 6),
          Text(
            apiStatus == 'ok' ? '已连接' : apiStatus == 'error' ? '连接失败' : '未知',
            style: TextStyle(fontSize: 12, color: cs.onSurfaceVariant),
          ),
          const SizedBox(width: 20),
          // 当前库
          if (currentLibrary != null) ...[
            Icon(Icons.folder_outlined, size: 14, color: cs.onSurfaceVariant),
            const SizedBox(width: 4),
            Flexible(
              child: Text(
                currentLibrary!,
                style: TextStyle(fontSize: 12, color: cs.onSurfaceVariant),
                overflow: TextOverflow.ellipsis,
              ),
            ),
          ],
          const Spacer(),
          // 扫描进度
          if (scanProgress != null) ...[
            Icon(Icons.sync, size: 14, color: cs.primary),
            const SizedBox(width: 4),
            Flexible(
              child: Text(
                scanProgress!,
                style: TextStyle(fontSize: 12, color: cs.primary),
                overflow: TextOverflow.ellipsis,
              ),
            ),
          ],
          // 版本号
          Text(
            'v1.0.0',
            style: TextStyle(fontSize: 11, color: cs.outline),
          ),
        ],
      ),
    );
  }
}
