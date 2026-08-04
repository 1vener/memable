// scan_screen.dart：扫描页面（库选择 + 临时扫描 + 进度 + 历史）
// 代码注释使用中文
import 'dart:async';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:file_picker/file_picker.dart';
import '../models/models.dart';
import '../services/api_service.dart';
import '../widgets/path_dialog.dart';

class ScanScreen extends StatefulWidget {
  final ApiService api;
  final String? currentLibrary;
  const ScanScreen({super.key, required this.api, this.currentLibrary});

  @override
  State<ScanScreen> createState() => _ScanScreenState();
}

class _ScanScreenState extends State<ScanScreen> {
  List<Library> _libraries = [];
  int? _selectedLibraryId;
  bool _scanning = false;
  String? _taskId;
  String? _statusMessage;
  String? _error;
  Timer? _pollTimer;

  // 临时扫描路径
  String _tempPath = '';
  bool _useExistingLib = true;
  bool _force = false;

  @override
  void initState() {
    super.initState();
    _loadLibraries();
  }

  @override
  void dispose() {
    _pollTimer?.cancel();
    super.dispose();
  }

  @override
  void didUpdateWidget(covariant ScanScreen oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.currentLibrary != null &&
        widget.currentLibrary != oldWidget.currentLibrary) {
      final match =
          _libraries.where((l) => l.name == widget.currentLibrary).toList();
      if (match.isNotEmpty) {
        setState(() => _selectedLibraryId = match.first.id);
      } else {
        // 库不存在于当前列表中，重置选择
        setState(() => _selectedLibraryId = null);
      }
    }
  }

  Future<void> _loadLibraries() async {
    try {
      var data = await widget.api.getLibraries();
      // 过滤掉 ID ≤ 0 的无效库（SQLite AUTOINCREMENT 从 1 开始）
      data = data.where((l) => l.id > 0).toList();
      if (mounted) {
        // 合并一次 setState，避免中间态
        int? newId = _selectedLibraryId;
        final currentIds = data.map((l) => l.id).toList();
        // 如果当前选中值不在返回的列表中，重置为第一个有效值
        if (widget.currentLibrary != null) {
          final match =
              data.where((l) => l.name == widget.currentLibrary).toList();
          newId = match.isNotEmpty ? match.first.id : null;
        } else if (newId == null || !currentIds.contains(newId)) {
          newId = data.isNotEmpty ? data.first.id : null;
        }
        setState(() {
          _libraries = data;
          _selectedLibraryId = newId;
        });
      }
    } catch (e) {
      if (mounted) setState(() => _error = '$e');
    }
  }

  Future<void> _startScan() async {
    setState(() {
      _scanning = true;
      _statusMessage = '正在提交任务...';
      _error = null;
    });

    try {
      final libraryId = _useExistingLib ? _selectedLibraryId : null;
      final scanPath = _useExistingLib ? null : _tempPath;

      if (libraryId == null && scanPath == null) {
        throw Exception('请选择一个库或指定临时扫描路径');
      }

      final result = await widget.api.startScan(
        libraryId: libraryId,
        scanPath: scanPath,
        force: _useExistingLib && _force,
      );

      final taskId = result['task_id'] as String?;
      final queuePos = result['queue_position'] ?? 0;
      if (mounted) {
        setState(() {
          _taskId = taskId;
          _statusMessage = '任务已提交 · 排队中第 $queuePos 位';
        });
      }
      // 轮询任务状态：排队 → 运行（含进度）→ 完成/失败；
      // _scanning 保持 true 直到终态，期间显示进度与取消按钮。
      if (taskId != null) _startPolling();
    } catch (e) {
      if (mounted) {
        setState(() {
          _scanning = false;
          _error = '提交失败: $e';
        });
      }
    }
  }

  /// 每 1 秒轮询任务状态，页面只读期间持续更新进度消息。
  void _startPolling() {
    _pollTimer?.cancel();
    _pollTimer = Timer.periodic(const Duration(seconds: 1), (_) => _pollTask());
  }

  Future<void> _pollTask() async {
    final taskId = _taskId;
    if (taskId == null) return;
    try {
      final task = await widget.api.getTask(taskId);
      if (!mounted) return;
      if (task.isCompleted) {
        _pollTimer?.cancel();
        _pollTimer = null;
        setState(() {
          _scanning = false;
          _taskId = null;
          _statusMessage = '扫描完成：新增 ${task.succeededItems}，'
              '跳过 ${task.skippedItems}，失败 ${task.failedItems}';
        });
        // 临时扫描完成后刷新库列表（可能新增了临时库）
        if (!_useExistingLib) await _loadLibraries();
        return;
      }
      if (task.isFailed || task.isCancelled) {
        _pollTimer?.cancel();
        _pollTimer = null;
        setState(() {
          _scanning = false;
          _taskId = null;
          _statusMessage = task.isFailed
              ? '扫描失败：${task.errorMessage ?? '未知错误'}'
              : '扫描已取消';
        });
        return;
      }
      // 运行中/排队中：展示阶段与进度
      String msg;
      if (task.totalItems > 0 && task.phase != 'queued') {
        final pct = (task.progress * 100).toStringAsFixed(1);
        msg = '扫描中 · $pct%（已处理 ${task.processedItems}/${task.totalItems}）';
      } else if (task.phase == 'discovering') {
        msg = '正在扫描目录...';
      } else if (task.phase == 'cleaning') {
        msg = '正在清理缺失文件...';
      } else {
        msg = task.isQueued ? '任务已提交 · 排队中' : '扫描中...';
      }
      setState(() => _statusMessage = msg);
    } catch (_) {
      // 查询失败不中断轮询
    }
  }

  Future<void> _cancelScan() async {
    final taskId = _taskId;
    try {
      if (taskId != null) {
        await widget.api.cancelTask(taskId);
      }
    } catch (e) {
      if (mounted) {
        setState(() => _error = '取消失败: $e');
      }
    }
    _pollTimer?.cancel();
    _pollTimer = null;
    if (mounted) {
      setState(() {
        _statusMessage = '扫描已取消';
        _scanning = false;
        _taskId = null;
      });
    }
  }

  Future<void> _pickTempPath() async {
    String? dir;

    // Web 无法通过浏览器获得服务端本机的真实目录路径，改为手动输入。
    if (kIsWeb) {
      dir = await showDialog<String>(
        context: context,
        builder:
            (ctx) => const PathDialog(
              title: '输入临时扫描目录',
              description: '请输入 Go 服务端所在电脑可访问的绝对路径。',
            ),
      );
    } else {
      try {
        dir = await FilePicker.platform.getDirectoryPath(
          dialogTitle: '选择临时扫描目录',
        );
      } on UnimplementedError {
        // 部分平台未实现目录选择接口时，回退到手动输入。
        if (!mounted) return;
        dir = await showDialog<String>(
          context: context,
          builder:
              (ctx) => const PathDialog(
                title: '输入临时扫描目录',
                description: '目录选择器不可用，请手动输入 Go 服务端所在电脑可访问的绝对路径。',
              ),
        );
      } catch (e) {
        if (!mounted) return;
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('目录选择器不可用，请手动输入路径：$e'),
            backgroundColor: const Color(0xFFF59E0B),
          ),
        );
        dir = await showDialog<String>(
          context: context,
          builder:
              (ctx) => const PathDialog(
                title: '输入临时扫描目录',
                description: '目录选择器不可用，请手动输入 Go 服务端所在电脑可访问的绝对路径。',
              ),
        );
      }
    }

    if (dir == null || dir.trim().isEmpty || !mounted) return;
    final path = dir.trim();
    setState(() => _tempPath = path);
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;

    return Padding(
      padding: const EdgeInsets.all(32),
      child: SingleChildScrollView(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // ========== 扫描模式选择 ==========
            Card(
              child: Padding(
                padding: const EdgeInsets.all(20),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      '扫描模式',
                      style: TextStyle(
                        fontSize: 15,
                        fontWeight: FontWeight.w600,
                        color: cs.onSurface,
                      ),
                    ),
                    const SizedBox(height: 16),
                    // 模式切换
                    Row(
                      children: [
                        Expanded(
                          child: _ModeCard(
                            icon: Icons.folder,
                            title: '扫描已有库',
                            subtitle: '从左侧选择一个库进行扫描',
                            selected: _useExistingLib,
                            onTap: () => setState(() => _useExistingLib = true),
                          ),
                        ),
                        const SizedBox(width: 12),
                        Expanded(
                          child: _ModeCard(
                            icon: Icons.flash_on,
                            title: '临时扫描',
                            subtitle: '选择任意目录快速扫描',
                            selected: !_useExistingLib,
                            onTap:
                                () => setState(() => _useExistingLib = false),
                          ),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
            ),
            const SizedBox(height: 16),

            // ========== 库选择 / 临时路径 ==========
            if (_useExistingLib)
              Card(
                child: Padding(
                  padding: const EdgeInsets.all(20),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        '选择库',
                        style: TextStyle(
                          fontSize: 15,
                          fontWeight: FontWeight.w600,
                          color: cs.onSurface,
                        ),
                      ),
                      const SizedBox(height: 12),
                      if (_libraries.isEmpty)
                        Center(
                          child: Padding(
                            padding: const EdgeInsets.all(24),
                            child: Column(
                              children: [
                                Icon(
                                  Icons.folder_off,
                                  size: 36,
                                  color: cs.outline,
                                ),
                                const SizedBox(height: 8),
                                Text(
                                  '暂无库，请先在收藏库页面添加',
                                  style: TextStyle(
                                    fontSize: 13,
                                    color: cs.outline,
                                  ),
                                ),
                              ],
                            ),
                          ),
                        )
                      else if (_selectedLibraryId == null)
                        Center(
                          child: Padding(
                            padding: const EdgeInsets.all(24),
                            child: Text(
                              '正在加载...',
                              style: TextStyle(fontSize: 13, color: cs.outline),
                            ),
                          ),
                        )
                      else
                        DropdownButtonFormField<int>(
                          value: _selectedLibraryId,
                          isExpanded: true,
                          decoration: const InputDecoration(hintText: '选择一个库'),
                          items:
                              _libraries.where((l) => l.id > 0).map((lib) {
                                return DropdownMenuItem(
                                  value: lib.id,
                                  child: Text(lib.name),
                                );
                              }).toList(),
                          onChanged: (v) {
                            if (v != null && v > 0) {
                              setState(() => _selectedLibraryId = v);
                            }
                          },
                        ),
                      const SizedBox(height: 12),
                      SwitchListTile(
                        contentPadding: EdgeInsets.zero,
                        title: const Text('强制重新处理全部文件'),
                        subtitle: const Text('用于彻底修复数据或算法升级后的重新计算'),
                        value: _force,
                        onChanged: (value) => setState(() => _force = value),
                      ),
                    ],
                  ),
                ),
              )
            else
              Card(
                child: Padding(
                  padding: const EdgeInsets.all(20),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        '扫描路径',
                        style: TextStyle(
                          fontSize: 15,
                          fontWeight: FontWeight.w600,
                          color: cs.onSurface,
                        ),
                      ),
                      const SizedBox(height: 12),
                      Row(
                        children: [
                          Expanded(
                            child: TextField(
                              readOnly: true,
                              controller: TextEditingController(
                                text: _tempPath,
                              ),
                              style: const TextStyle(fontSize: 13),
                              decoration: InputDecoration(
                                hintText: '点击选择目录',
                                suffixIcon: IconButton(
                                  icon: const Icon(Icons.folder_open, size: 18),
                                  onPressed: _pickTempPath,
                                ),
                              ),
                              onTap: _pickTempPath,
                            ),
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
              ),
            const SizedBox(height: 24),

            // ========== 操作按钮 ==========
            Row(
              children: [
                FilledButton.icon(
                  onPressed: _scanning ? null : _startScan,
                  icon:
                      _scanning
                          ? const SizedBox(
                            width: 18,
                            height: 18,
                            child: CircularProgressIndicator(
                              strokeWidth: 2,
                              color: Colors.white,
                            ),
                          )
                          : const Icon(Icons.play_arrow, size: 20),
                  label: Text(_scanning ? '同步中...' : '同步扫描'),
                ),
                if (_scanning) ...[
                  const SizedBox(width: 12),
                  OutlinedButton(
                    onPressed: _cancelScan,
                    child: const Text('取消'),
                  ),
                ],
              ],
            ),
            const SizedBox(height: 16),

            // ========== 状态消息 ==========
            if (_statusMessage != null)
              Container(
                padding: const EdgeInsets.all(16),
                decoration: BoxDecoration(
                  color: cs.primary.withValues(alpha: 0.05),
                  borderRadius: BorderRadius.circular(10),
                  border: Border.all(color: cs.primary.withValues(alpha: 0.2)),
                ),
                child: Row(
                  children: [
                    if (_scanning)
                      const SizedBox(
                        width: 18,
                        height: 18,
                        child: CircularProgressIndicator(
                          strokeWidth: 2,
                          color: Color(0xFF2563EB),
                        ),
                      )
                    else
                      Icon(Icons.info_outline, size: 18, color: cs.primary),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Text(
                        _statusMessage!,
                        style: TextStyle(fontSize: 13, color: cs.onSurface),
                      ),
                    ),
                  ],
                ),
              ),
            if (_error != null)
              Container(
                padding: const EdgeInsets.all(16),
                decoration: BoxDecoration(
                  color: const Color(0xFFEF4444).withValues(alpha: 0.05),
                  borderRadius: BorderRadius.circular(10),
                  border: Border.all(
                    color: const Color(0xFFEF4444).withValues(alpha: 0.2),
                  ),
                ),
                child: Row(
                  children: [
                    const Icon(
                      Icons.error_outline,
                      size: 18,
                      color: Color(0xFFEF4444),
                    ),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Text(
                        _error!,
                        style: const TextStyle(
                          fontSize: 13,
                          color: Color(0xFFEF4444),
                        ),
                      ),
                    ),
                  ],
                ),
              ),
          ],
        ),
      ),
    );
  }
}

/// 模式选择卡片
class _ModeCard extends StatelessWidget {
  final IconData icon;
  final String title;
  final String subtitle;
  final bool selected;
  final VoidCallback onTap;

  const _ModeCard({
    required this.icon,
    required this.title,
    required this.subtitle,
    required this.selected,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final isDark = Theme.of(context).brightness == Brightness.dark;
    return Material(
      color: Colors.transparent,
      borderRadius: BorderRadius.circular(12),
      child: InkWell(
        borderRadius: BorderRadius.circular(12),
        onTap: onTap,
        child: AnimatedContainer(
          duration: const Duration(milliseconds: 150),
          padding: const EdgeInsets.all(20),
          decoration: BoxDecoration(
            color:
                selected
                    ? cs.primary.withValues(alpha: isDark ? 0.12 : 0.08)
                    : cs.surfaceContainerHighest.withValues(alpha: 0.3),
            borderRadius: BorderRadius.circular(12),
            border: Border.all(
              color:
                  selected
                      ? cs.primary.withValues(alpha: 0.4)
                      : cs.outlineVariant,
              width: selected ? 1.5 : 1,
            ),
          ),
          child: Column(
            children: [
              Icon(
                icon,
                size: 32,
                color: selected ? cs.primary : cs.onSurfaceVariant,
              ),
              const SizedBox(height: 10),
              Text(
                title,
                style: TextStyle(
                  fontSize: 14,
                  fontWeight: FontWeight.w600,
                  color: selected ? cs.primary : cs.onSurface,
                ),
              ),
              const SizedBox(height: 4),
              Text(
                subtitle,
                style: TextStyle(fontSize: 12, color: cs.outline),
                textAlign: TextAlign.center,
              ),
            ],
          ),
        ),
      ),
    );
  }
}
