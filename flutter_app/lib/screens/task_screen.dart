// task_screen.dart：任务进度页面（队列 + 进度 + 历史）
// 代码注释使用中文
import 'dart:async';
import 'package:flutter/material.dart';
import '../models/models.dart';
import '../services/api_service.dart';

class TaskScreen extends StatefulWidget {
  final ApiService api;
  const TaskScreen({super.key, required this.api});

  @override
  State<TaskScreen> createState() => _TaskScreenState();
}

class _TaskScreenState extends State<TaskScreen> {
  List<BackgroundTask> _tasks = [];
  bool _loading = true;
  String? _error;
  Timer? _timer;

  @override
  void initState() {
    super.initState();
    _loadTasks();
    _startAutoRefresh();
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  void _startAutoRefresh() {
    _timer = Timer.periodic(const Duration(seconds: 2), (_) {
      if (mounted) _loadTasks(silent: true);
    });
  }

  Future<void> _loadTasks({bool silent = false}) async {
    if (!silent)
      setState(() {
        _loading = true;
        _error = null;
      });
    try {
      final tasks = await widget.api.getTasks();
      if (mounted)
        setState(() {
          _tasks = tasks;
          _loading = false;
        });
    } catch (e) {
      if (mounted && !silent)
        setState(() {
          _error = '$e';
          _loading = false;
        });
    }
  }

  List<BackgroundTask> get _runningTasks =>
      _tasks.where((t) => t.isRunning).toList();
  List<BackgroundTask> get _queuedTasks =>
      _tasks.where((t) => t.isQueued).toList();
  List<BackgroundTask> get _historyTasks =>
      _tasks.where((t) => t.isTerminal).toList();

  Future<void> _cancelTask(BackgroundTask task) async {
    final ok = await showDialog<bool>(
      context: context,
      builder:
          (ctx) => AlertDialog(
            title: const Text('确认取消'),
            content: Text('确定要取消任务「${task.title}」吗？'),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(ctx, false),
                child: const Text('否'),
              ),
              TextButton(
                onPressed: () => Navigator.pop(ctx, true),
                child: const Text(
                  '取消任务',
                  style: TextStyle(color: Color(0xFFEF4444)),
                ),
              ),
            ],
          ),
    );
    if (ok != true) return;
    try {
      await widget.api.cancelTask(task.id);
      await _loadTasks(silent: true);
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('取消失败: $e'),
            backgroundColor: const Color(0xFFEF4444),
          ),
        );
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;

    if (_loading) {
      return const Center(child: CircularProgressIndicator());
    }

    if (_error != null) {
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.error_outline, size: 48, color: cs.error),
            const SizedBox(height: 16),
            Text('加载失败', style: TextStyle(fontSize: 16, color: cs.onSurface)),
            const SizedBox(height: 8),
            Text(
              _error!,
              style: TextStyle(fontSize: 13, color: cs.outline),
              overflow: TextOverflow.ellipsis,
              maxLines: 3,
            ),
            const SizedBox(height: 16),
            FilledButton.icon(
              onPressed: _loadTasks,
              icon: const Icon(Icons.refresh, size: 18),
              label: const Text('重试'),
            ),
          ],
        ),
      );
    }

    final hasActive = _runningTasks.isNotEmpty || _queuedTasks.isNotEmpty;

    return Padding(
      padding: const EdgeInsets.all(24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // 标题栏
          Row(
            children: [
              Icon(Icons.queue, size: 22, color: cs.primary),
              const SizedBox(width: 8),
              Text(
                '任务队列',
                style: TextStyle(
                  fontSize: 18,
                  fontWeight: FontWeight.w600,
                  color: cs.onSurface,
                ),
              ),
              const Spacer(),
              if (hasActive)
                Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 10,
                    vertical: 4,
                  ),
                  decoration: BoxDecoration(
                    color: cs.primary.withValues(alpha: 0.1),
                    borderRadius: BorderRadius.circular(12),
                  ),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      SizedBox(
                        width: 12,
                        height: 12,
                        child: CircularProgressIndicator(
                          strokeWidth: 2,
                          color: cs.primary,
                        ),
                      ),
                      const SizedBox(width: 6),
                      Text(
                        '${_runningTasks.length + _queuedTasks.length} 个活跃任务',
                        style: TextStyle(fontSize: 12, color: cs.primary),
                      ),
                    ],
                  ),
                ),
              const SizedBox(width: 8),
              IconButton(
                icon: const Icon(Icons.refresh, size: 20),
                tooltip: '刷新',
                onPressed: () => _loadTasks(),
              ),
            ],
          ),
          const SizedBox(height: 20),

          // 内容区
          Expanded(
            child:
                _tasks.isEmpty
                    ? Center(
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          Icon(
                            Icons.check_circle_outline,
                            size: 64,
                            color: cs.outline.withValues(alpha: 0.5),
                          ),
                          const SizedBox(height: 16),
                          Text(
                            '暂无任务',
                            style: TextStyle(fontSize: 15, color: cs.outline),
                          ),
                          const SizedBox(height: 8),
                          Text(
                            '同步扫描或报告任务会在此显示',
                            style: TextStyle(fontSize: 13, color: cs.outline),
                          ),
                        ],
                      ),
                    )
                    : ListView(
                      children: [
                        // 运行中任务
                        if (_runningTasks.isNotEmpty) ...[
                          _SectionHeader(
                            title: '运行中',
                            icon: Icons.play_circle,
                            color: cs.primary,
                          ),
                          const SizedBox(height: 8),
                          for (final task in _runningTasks)
                            _TaskCard(
                              task: task,
                              onCancel: () => _cancelTask(task),
                            ),
                          const SizedBox(height: 20),
                        ],

                        // 等待中任务
                        if (_queuedTasks.isNotEmpty) ...[
                          const _SectionHeader(
                            title: '等待中',
                            icon: Icons.schedule,
                            color: Color(0xFFF59E0B),
                          ),
                          const SizedBox(height: 8),
                          for (final task in _queuedTasks)
                            _TaskCard(
                              task: task,
                              onCancel: () => _cancelTask(task),
                            ),
                          const SizedBox(height: 20),
                        ],

                        // 历史任务
                        if (_historyTasks.isNotEmpty) ...[
                          _SectionHeader(
                            title: '历史',
                            icon: Icons.history,
                            color: cs.outline,
                          ),
                          const SizedBox(height: 8),
                          for (final task in _historyTasks)
                            _TaskCard(task: task),
                        ],
                      ],
                    ),
          ),
        ],
      ),
    );
  }
}

class _SectionHeader extends StatelessWidget {
  final String title;
  final IconData icon;
  final Color color;
  const _SectionHeader({
    required this.title,
    required this.icon,
    required this.color,
  });

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        Icon(icon, size: 16, color: color),
        const SizedBox(width: 6),
        Text(
          title,
          style: TextStyle(
            fontSize: 13,
            fontWeight: FontWeight.w600,
            color: color,
          ),
        ),
      ],
    );
  }
}

class _TaskCard extends StatelessWidget {
  final BackgroundTask task;
  final VoidCallback? onCancel;
  const _TaskCard({required this.task, this.onCancel});

  Color _statusColor(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    switch (task.status) {
      case 'running':
        return cs.primary;
      case 'queued':
        return const Color(0xFFF59E0B);
      case 'completed':
        return const Color(0xFF22C55E);
      case 'failed':
        return cs.error;
      case 'cancelled':
        return cs.outline;
      default:
        return cs.outline;
    }
  }

  IconData _kindIcon() {
    switch (task.kind) {
      case 'scan':
        return Icons.scanner;
      case 'repair':
        return Icons.build;
      case 'temporary_scan':
        return Icons.flash_on;
      case 'report_image':
        return Icons.photo_library;
      case 'report_video':
        return Icons.movie;
      case 'promote':
        return Icons.move_up;
      case 'directory_delete':
        return Icons.folder_delete;
      default:
        return Icons.task;
    }
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final statusColor = _statusColor(context);

    return Card(
      margin: const EdgeInsets.only(bottom: 8),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // 任务标题行
            Row(
              children: [
                Icon(_kindIcon(), size: 18, color: statusColor),
                const SizedBox(width: 8),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        task.title,
                        style: TextStyle(
                          fontSize: 14,
                          fontWeight: FontWeight.w500,
                          color: cs.onSurface,
                        ),
                      ),
                      const SizedBox(height: 2),
                      Text(
                        '${task.kindLabel} · ${task.statusLabel}',
                        style: TextStyle(fontSize: 12, color: cs.outline),
                      ),
                    ],
                  ),
                ),
                // 状态标签
                Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 8,
                    vertical: 3,
                  ),
                  decoration: BoxDecoration(
                    color: statusColor.withValues(alpha: 0.1),
                    borderRadius: BorderRadius.circular(8),
                  ),
                  child: Text(
                    task.statusLabel,
                    style: TextStyle(
                      fontSize: 11,
                      fontWeight: FontWeight.w500,
                      color: statusColor,
                    ),
                  ),
                ),
                // 取消按钮
                if (onCancel != null) ...[
                  const SizedBox(width: 8),
                  IconButton(
                    icon: const Icon(Icons.close, size: 18),
                    tooltip: '取消',
                    onPressed: onCancel,
                    visualDensity: VisualDensity.compact,
                  ),
                ],
              ],
            ),

            // 进度条（运行中任务）
            if (task.isRunning && task.totalItems > 0) ...[
              const SizedBox(height: 12),
              Row(
                children: [
                  Expanded(
                    child: ClipRRect(
                      borderRadius: BorderRadius.circular(4),
                      child: LinearProgressIndicator(
                        value: task.progress,
                        minHeight: 6,
                        backgroundColor: cs.surfaceContainerHighest,
                      ),
                    ),
                  ),
                  const SizedBox(width: 10),
                  Text(
                    '${(task.progress * 100).toStringAsFixed(1)}%',
                    style: TextStyle(
                      fontSize: 12,
                      fontWeight: FontWeight.w500,
                      color: cs.onSurface,
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 8),
              Row(
                children: [
                  _ProgressChip(
                    label: '已处理',
                    value: task.processedItems,
                    color: cs.primary,
                  ),
                  const SizedBox(width: 8),
                  _ProgressChip(
                    label: '成功',
                    value: task.succeededItems,
                    color: const Color(0xFF22C55E),
                  ),
                  const SizedBox(width: 8),
                  _ProgressChip(
                    label: '跳过',
                    value: task.skippedItems,
                    color: const Color(0xFFF59E0B),
                  ),
                  const SizedBox(width: 8),
                  _ProgressChip(
                    label: '失败',
                    value: task.failedItems,
                    color: cs.error,
                  ),
                  const Spacer(),
                  Text(
                    '共 ${task.totalItems} 项',
                    style: TextStyle(fontSize: 12, color: cs.outline),
                  ),
                ],
              ),
            ],

            // 运行中阶段提示
            if (task.isRunning && task.totalItems == 0) ...[
              const SizedBox(height: 12),
              Row(
                children: [
                  SizedBox(
                    width: 14,
                    height: 14,
                    child: CircularProgressIndicator(
                      strokeWidth: 2,
                      color: cs.primary,
                    ),
                  ),
                  const SizedBox(width: 8),
                  Text(
                    task.phase == 'discovering' ? '正在扫描目录...' : '正在处理...',
                    style: TextStyle(fontSize: 13, color: cs.primary),
                  ),
                ],
              ),
            ],

            // 排队位置
            if (task.isQueued && task.queuePosition > 0) ...[
              const SizedBox(height: 8),
              Row(
                children: [
                  Icon(Icons.hourglass_top, size: 14, color: cs.outline),
                  const SizedBox(width: 4),
                  Text(
                    '队列中第 ${task.queuePosition} 位',
                    style: TextStyle(fontSize: 12, color: cs.outline),
                  ),
                ],
              ),
            ],

            // 错误信息
            if (task.isFailed && task.errorMessage != null) ...[
              const SizedBox(height: 8),
              Container(
                padding: const EdgeInsets.all(8),
                decoration: BoxDecoration(
                  color: cs.error.withValues(alpha: 0.05),
                  borderRadius: BorderRadius.circular(6),
                ),
                child: Row(
                  children: [
                    Icon(Icons.error_outline, size: 14, color: cs.error),
                    const SizedBox(width: 6),
                    Expanded(
                      child: Text(
                        task.errorMessage!,
                        style: TextStyle(fontSize: 12, color: cs.error),
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _ProgressChip extends StatelessWidget {
  final String label;
  final int value;
  final Color color;
  const _ProgressChip({
    required this.label,
    required this.value,
    required this.color,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(6),
      ),
      child: Text(
        '$label $value',
        style: TextStyle(fontSize: 11, color: color),
      ),
    );
  }
}
