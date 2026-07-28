// scan_screen.dart：扫描页面（库选择 + 临时扫描 + 进度 + 历史）
// 代码注释使用中文
import 'package:flutter/material.dart';
import 'package:file_picker/file_picker.dart';
import '../models/models.dart';
import '../services/api_service.dart';

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
  String? _statusMessage;
  String? _error;

  // 临时扫描路径
  String _tempPath = '';
  bool _useExistingLib = true;

  @override
  void initState() {
    super.initState();
    _loadLibraries();
  }

  @override
  void didUpdateWidget(covariant ScanScreen oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.currentLibrary != null && widget.currentLibrary != oldWidget.currentLibrary) {
      final match = _libraries.where((l) => l.name == widget.currentLibrary).toList();
      if (match.isNotEmpty) {
        setState(() => _selectedLibraryId = match.first.id);
      }
    }
  }

  Future<void> _loadLibraries() async {
    try {
      final data = await widget.api.getLibraries();
      if (mounted) {
        setState(() {
          _libraries = data;
          if (_selectedLibraryId == null && _libraries.isNotEmpty) {
            _selectedLibraryId = _libraries.first.id;
          }
        });
        // 自动选中 currentLibrary
        if (widget.currentLibrary != null) {
          final match = _libraries.where((l) => l.name == widget.currentLibrary).toList();
          if (match.isNotEmpty) {
            setState(() => _selectedLibraryId = match.first.id);
          }
        }
      }
    } catch (e) {
      if (mounted) setState(() => _error = '$e');
    }
  }

  Future<void> _startScan() async {
    setState(() {
      _scanning = true;
      _statusMessage = '正在扫描...';
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
      );

      final sessionId = result['session_id'] ?? '';
      if (mounted) {
        setState(() => _statusMessage = '扫描完成 · 会话: $sessionId');
      }
    } catch (e) {
      if (mounted) {
        setState(() => _error = '扫描失败: $e');
      }
    } finally {
      if (mounted) setState(() => _scanning = false);
    }
  }

  Future<void> _pickTempPath() async {
    final dir = await FilePicker.platform.getDirectoryPath(
      dialogTitle: '选择临时扫描目录',
    );
    if (dir != null) {
      setState(() => _tempPath = dir);
    }
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
                    Text('扫描模式', style: TextStyle(fontSize: 15, fontWeight: FontWeight.w600, color: cs.onSurface)),
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
                            onTap: () => setState(() => _useExistingLib = false),
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
                      Text('选择库', style: TextStyle(fontSize: 15, fontWeight: FontWeight.w600, color: cs.onSurface)),
                      const SizedBox(height: 12),
                      if (_libraries.isEmpty)
                        Center(
                          child: Padding(
                            padding: const EdgeInsets.all(24),
                            child: Column(
                              children: [
                                Icon(Icons.folder_off, size: 36, color: cs.outline),
                                const SizedBox(height: 8),
                                Text('暂无库，请先在收藏库页面添加', style: TextStyle(fontSize: 13, color: cs.outline)),
                              ],
                            ),
                          ),
                        )
                      else
                        DropdownButtonFormField<int>(
                          value: _selectedLibraryId,
                          isExpanded: true,
                          decoration: const InputDecoration(hintText: '选择一个库'),
                          items: _libraries.map((lib) {
                            return DropdownMenuItem(value: lib.id, child: Text(lib.name));
                          }).toList(),
                          onChanged: (v) => setState(() => _selectedLibraryId = v),
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
                      Text('扫描路径', style: TextStyle(fontSize: 15, fontWeight: FontWeight.w600, color: cs.onSurface)),
                      const SizedBox(height: 12),
                      Row(
                        children: [
                          Expanded(
                            child: TextField(
                              readOnly: true,
                              controller: TextEditingController(text: _tempPath),
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
                  icon: _scanning
                      ? const SizedBox(
                          width: 18, height: 18,
                          child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white),
                        )
                      : const Icon(Icons.play_arrow, size: 20),
                  label: Text(_scanning ? '扫描中...' : '开始扫描'),
                ),
                if (_scanning) ...[
                  const SizedBox(width: 12),
                  OutlinedButton(
                    onPressed: () => setState(() => _scanning = false),
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
                    Icon(Icons.info_outline, size: 18, color: cs.primary),
                    const SizedBox(width: 10),
                    Expanded(child: Text(_statusMessage!, style: TextStyle(fontSize: 13, color: cs.onSurface))),
                  ],
                ),
              ),
            if (_error != null)
              Container(
                padding: const EdgeInsets.all(16),
                decoration: BoxDecoration(
                  color: const Color(0xFFEF4444).withValues(alpha: 0.05),
                  borderRadius: BorderRadius.circular(10),
                  border: Border.all(color: const Color(0xFFEF4444).withValues(alpha: 0.2)),
                ),
                child: Row(
                  children: [
                    const Icon(Icons.error_outline, size: 18, color: Color(0xFFEF4444)),
                    const SizedBox(width: 10),
                    Expanded(child: Text(_error!, style: const TextStyle(fontSize: 13, color: Color(0xFFEF4444)))),
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
            color: selected
                ? cs.primary.withValues(alpha: isDark ? 0.12 : 0.08)
                : cs.surfaceContainerHighest.withValues(alpha: 0.3),
            borderRadius: BorderRadius.circular(12),
            border: Border.all(
              color: selected ? cs.primary.withValues(alpha: 0.4) : cs.outlineVariant,
              width: selected ? 1.5 : 1,
            ),
          ),
          child: Column(
            children: [
              Icon(icon, size: 32, color: selected ? cs.primary : cs.onSurfaceVariant),
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
