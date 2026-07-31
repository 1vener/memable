// status_bar.dart：底部状态栏组件
// 代码注释使用中文
import 'package:flutter/material.dart';
import '../models/models.dart';

/// 底部状态栏
class StatusBar extends StatelessWidget {
  final String? apiStatus; // 'ok' | 'error' | null(未知)
  final String? currentLibrary;
  final String? scanProgress;
  final List<BackgroundTask> runningTasks; // 运行中后台任务

  const StatusBar({
    super.key,
    this.apiStatus,
    this.currentLibrary,
    this.scanProgress,
    this.runningTasks = const [],
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
          // 运行中任务进度（IDEA 风格：转圈 + 任务名 + 进度条 + 百分比）
          if (runningTasks.isNotEmpty) ...[
            SizedBox(
              width: 12,
              height: 12,
              child: CircularProgressIndicator(
                strokeWidth: 2,
                color: cs.primary,
              ),
            ),
            const SizedBox(width: 6),
            ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 180),
              child: Text(
                runningTasks.first.title,
                style: TextStyle(fontSize: 12, color: cs.onSurfaceVariant),
                overflow: TextOverflow.ellipsis,
              ),
            ),
            const SizedBox(width: 8),
            SizedBox(
              width: 100,
              child: ClipRRect(
                borderRadius: BorderRadius.circular(2),
                child: LinearProgressIndicator(
                  value: runningTasks.first.totalItems > 0
                      ? runningTasks.first.progress
                      : null,
                  minHeight: 4,
                  backgroundColor: cs.surfaceContainerHighest,
                ),
              ),
            ),
            const SizedBox(width: 6),
            Text(
              runningTasks.first.totalItems > 0
                  ? '${(runningTasks.first.progress * 100).toStringAsFixed(0)}%'
                  : runningTasks.first.phase,
              style: TextStyle(fontSize: 11, color: cs.primary),
            ),
            if (runningTasks.length > 1) ...[
              const SizedBox(width: 4),
              Text(
                '+${runningTasks.length - 1}',
                style: TextStyle(fontSize: 11, color: cs.outline),
              ),
            ],
            const SizedBox(width: 16),
          ],
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
