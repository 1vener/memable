// library_screen.dart：收藏库管理页面（左列表 + 右文件树 + 右键菜单）
// 代码注释使用中文
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:file_picker/file_picker.dart';
import '../models/models.dart';
import '../services/api_service.dart';
import '../widgets/context_menu.dart';
import '../widgets/path_dialog.dart';

/// 文件树节点
class FileTreeNode {
  final String name;
  final String fullPath;
  final bool isDir;
  final List<FileTreeNode> children;

  FileTreeNode({
    required this.name,
    required this.fullPath,
    required this.isDir,
    this.children = const [],
  });
}

class LibraryScreen extends StatefulWidget {
  final ApiService api;
  final ValueChanged<String> onLibrarySelected;

  const LibraryScreen({
    super.key,
    required this.api,
    required this.onLibrarySelected,
  });

  @override
  State<LibraryScreen> createState() => _LibraryScreenState();
}

class _LibraryScreenState extends State<LibraryScreen> {
  List<Library> _libraries = [];
  Library? _selected;
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _loadLibraries();
  }

  Future<void> _loadLibraries() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final data = await widget.api.getLibraries();
      if (mounted) {
        setState(() {
          _libraries = data;
          _loading = false;
          if (_libraries.isNotEmpty && _selected == null) {
            _selected = _libraries.first;
          }
        });
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _error = '$e';
          _loading = false;
        });
      }
    }
  }

  Future<void> _addLibrary() async {
    String? dir;

    // Web 无法通过浏览器获得服务端本机的真实目录路径，改为手动输入。
    if (kIsWeb) {
      dir = await _showPathDialog();
    } else {
      try {
        dir = await FilePicker.platform.getDirectoryPath(
          dialogTitle: '选择媒体库目录',
        );
      } on UnimplementedError {
        // 部分平台未实现目录选择接口时，回退到手动输入。
        dir = await _showPathDialog();
      } catch (e) {
        if (!mounted) return;
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('目录选择器不可用，请手动输入路径：$e'),
            backgroundColor: const Color(0xFFF59E0B),
          ),
        );
        dir = await _showPathDialog();
      }
    }

    if (dir == null || dir.trim().isEmpty || !mounted) return;
    dir = dir.trim();

    final name = await showDialog<String>(
      context: context,
      builder: (ctx) => _NameDialog(defaultName: dir!.split('/').last.split('\\').last),
    );
    if (name == null || name.isEmpty) return;

    try {
      await widget.api.createLibrary(name, dir);
      await _loadLibraries();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('创建失败: $e'), backgroundColor: const Color(0xFFEF4444)),
        );
      }
    }
  }

  // 手动输入服务端可访问的本地绝对路径。
  Future<String?> _showPathDialog() {
    return showDialog<String>(
      context: context,
      builder: (ctx) => const PathDialog(
        title: '输入媒体库目录',
        description: '请输入 Go 服务端所在电脑可访问的绝对路径。',
      ),
    );
  }

  Future<void> _repairLibrary(Library lib) async {
    try {
      await widget.api.repairLibrary(lib.id);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('修复扫描已启动: ${lib.name}'),
            backgroundColor: const Color(0xFF2563EB),
          ),
        );
      }
      widget.onLibrarySelected(lib.name);
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('启动修复扫描失败: $e'), backgroundColor: const Color(0xFFEF4444)),
        );
      }
    }
  }

  Future<void> _deleteLibrary(Library lib) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('确认删除'),
        content: Text('确定要删除库「${lib.name}」吗？此操作不可恢复。'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('取消')),
          TextButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('删除', style: TextStyle(color: Color(0xFFEF4444))),
          ),
        ],
      ),
    );
    if (ok != true) return;

    try {
      await widget.api.deleteLibrary(lib.id);
      setState(() => _selected = null);
      await _loadLibraries();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('删除失败: $e'), backgroundColor: const Color(0xFFEF4444)),
        );
      }
    }
  }

  void _onLibraryTap(Library lib) {
    setState(() => _selected = lib);
    widget.onLibrarySelected(lib.name);
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
            Text(_error!, style: TextStyle(fontSize: 13, color: cs.outline)),
            const SizedBox(height: 16),
            FilledButton.icon(
              onPressed: _loadLibraries,
              icon: const Icon(Icons.refresh, size: 18),
              label: const Text('重试'),
            ),
          ],
        ),
      );
    }

    return Row(
      children: [
        // ========== 左侧：库列表 ==========
        SizedBox(
          width: 280,
          child: Column(
            children: [
              // 工具栏
              Container(
                height: 52,
                padding: const EdgeInsets.symmetric(horizontal: 16),
                decoration: BoxDecoration(
                  border: Border(bottom: BorderSide(color: cs.outlineVariant, width: 0.5)),
                ),
                child: Row(
                  children: [
                    Text(
                      '收藏库 (${_libraries.length})',
                      style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: cs.onSurface),
                    ),
                    const Spacer(),
                    IconButton(
                      icon: const Icon(Icons.add, size: 20),
                      tooltip: '添加库',
                      onPressed: _addLibrary,
                    ),
                    IconButton(
                      icon: const Icon(Icons.refresh, size: 20),
                      tooltip: '刷新',
                      onPressed: _loadLibraries,
                    ),
                  ],
                ),
              ),
              // 库列表
              Expanded(
                child: _libraries.isEmpty
                    ? Center(
                        child: Column(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            Icon(Icons.folder_off, size: 48, color: cs.outline),
                            const SizedBox(height: 12),
                            Text('暂无收藏库', style: TextStyle(fontSize: 14, color: cs.outline)),
                            const SizedBox(height: 16),
                            FilledButton.tonalIcon(
                              onPressed: _addLibrary,
                              icon: const Icon(Icons.add, size: 18),
                              label: const Text('添加库'),
                            ),
                          ],
                        ),
                      )
                    : ListView.builder(
                        padding: const EdgeInsets.all(8),
                        itemCount: _libraries.length,
                        itemBuilder: (context, index) {
                          final lib = _libraries[index];
                          final selected = _selected?.id == lib.id;
                          return _LibraryCard(
                            library: lib,
                            selected: selected,
                            onTap: () => _onLibraryTap(lib),
                            onDelete: () => _deleteLibrary(lib),
                            onRepair: () => _repairLibrary(lib),
                          );
                        },
                      ),
              ),
            ],
          ),
        ),
        // ========== 分隔线 ==========
        VerticalDivider(width: 1, color: cs.outlineVariant),
        // ========== 右侧：库详情 ==========
        Expanded(
          child: _selected == null
              ? Center(
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Icon(Icons.folder_open, size: 64, color: cs.outline.withValues(alpha: 0.5)),
                      const SizedBox(height: 16),
                      Text('选择一个库查看详情', style: TextStyle(fontSize: 15, color: cs.outline)),
                    ],
                  ),
                )
              : _LibraryDetail(library: _selected!, api: widget.api),
        ),
      ],
    );
  }
}

/// 库卡片
class _LibraryCard extends StatelessWidget {
  final Library library;
  final bool selected;
  final VoidCallback onTap;
  final VoidCallback onDelete;
  final VoidCallback onRepair;

  const _LibraryCard({
    required this.library,
    required this.selected,
    required this.onTap,
    required this.onDelete,
    required this.onRepair,
  });

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Padding(
      padding: const EdgeInsets.all(4),
      child: Tooltip(
        message: library.path,
        child: Material(
          color: Colors.transparent,
          borderRadius: BorderRadius.circular(10),
          child: InkWell(
            borderRadius: BorderRadius.circular(10),
            onTap: onTap,
            onSecondaryTapDown: (details) {
              showContextMenu(
                context: context,
                position: details.globalPosition,
                items: [
                  ContextMenuItem(
                    icon: Icons.play_arrow,
                    label: '开始扫描',
                    onTap: onTap,
                  ),
                  ContextMenuItem(
                    icon: Icons.healing,
                    label: '修复扫描',
                    onTap: onRepair,
                  ),
                  ContextMenuItem(
                    icon: Icons.delete_outline,
                    label: '删除库',
                    isDestructive: true,
                    onTap: onDelete,
                  ),
                ],
              );
            },
            child: AnimatedContainer(
              duration: const Duration(milliseconds: 150),
              padding: const EdgeInsets.all(14),
              decoration: BoxDecoration(
                color: selected
                    ? cs.primary.withValues(alpha: Theme.of(context).brightness == Brightness.dark ? 0.15 : 0.1)
                    : Colors.transparent,
                borderRadius: BorderRadius.circular(10),
                border: selected
                    ? Border.all(color: cs.primary.withValues(alpha: 0.3))
                    : null,
              ),
              child: Row(
                children: [
                  Container(
                    width: 42,
                    height: 42,
                    decoration: BoxDecoration(
                      color: cs.primary.withValues(alpha: 0.1),
                      borderRadius: BorderRadius.circular(10),
                    ),
                    child: Icon(Icons.folder, color: cs.primary, size: 22),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          library.name,
                          style: TextStyle(
                            fontSize: 14,
                            fontWeight: FontWeight.w600,
                            color: cs.onSurface,
                          ),
                          overflow: TextOverflow.ellipsis,
                        ),
                        const SizedBox(height: 2),
                        Text(
                          library.path,
                          style: TextStyle(fontSize: 12, color: cs.outline),
                          overflow: TextOverflow.ellipsis,
                        ),
                      ],
                    ),
                  ),
                  Icon(Icons.chevron_right, size: 18, color: cs.outline),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// 库详情面板
class _LibraryDetail extends StatelessWidget {
  final Library library;
  final ApiService api;

  const _LibraryDetail({required this.library, required this.api});

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;

    return Padding(
      padding: const EdgeInsets.all(32),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // 库名称
          Row(
            children: [
              Icon(Icons.folder, size: 28, color: cs.primary),
              const SizedBox(width: 12),
              Text(
                library.name,
                style: TextStyle(fontSize: 22, fontWeight: FontWeight.w700, color: cs.onSurface),
              ),
            ],
          ),
          const SizedBox(height: 24),

          // 基本信息卡片
          Card(
            child: Padding(
              padding: const EdgeInsets.all(20),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text('基本信息', style: TextStyle(fontSize: 15, fontWeight: FontWeight.w600, color: cs.onSurface)),
                  const SizedBox(height: 16),
                  _InfoRow(label: '库 ID', value: library.id.toString()),
                  const SizedBox(height: 8),
                  _InfoRow(label: '路径', value: library.path),
                ],
              ),
            ),
          ),
          const SizedBox(height: 16),
          // 操作按钮
          Row(
            children: [
              FilledButton.icon(
                onPressed: () async {
                  try {
                    await api.repairLibrary(library.id);
                    if (context.mounted) {
                      ScaffoldMessenger.of(context).showSnackBar(
                        SnackBar(
                          content: Text('修复扫描已启动: ${library.name}'),
                          backgroundColor: const Color(0xFF2563EB),
                        ),
                      );
                    }
                  } catch (e) {
                    if (context.mounted) {
                      ScaffoldMessenger.of(context).showSnackBar(
                        SnackBar(content: Text('修复扫描失败: $e'), backgroundColor: const Color(0xFFEF4444)),
                      );
                    }
                  }
                },
                icon: const Icon(Icons.healing, size: 18),
                label: const Text('修复扫描'),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

class _InfoRow extends StatelessWidget {
  final String label;
  final String value;
  const _InfoRow({required this.label, required this.value});

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SizedBox(
          width: 80,
          child: Text(label, style: TextStyle(fontSize: 13, color: cs.outline)),
        ),
        Expanded(
          child: SelectableText(value, style: TextStyle(fontSize: 13, color: cs.onSurface)),
        ),
      ],
    );
  }
}

/// 名称输入对话框
class _NameDialog extends StatefulWidget {
  final String defaultName;
  const _NameDialog({required this.defaultName});

  @override
  State<_NameDialog> createState() => _NameDialogState();
}

class _NameDialogState extends State<_NameDialog> {
  late final TextEditingController _ctrl;

  @override
  void initState() {
    super.initState();
    _ctrl = TextEditingController(text: widget.defaultName);
  }

  @override
  void dispose() {
    _ctrl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('库名称'),
      content: TextField(
        controller: _ctrl,
        autofocus: true,
        decoration: const InputDecoration(hintText: '输入库名称'),
        onSubmitted: (_) => Navigator.pop(context, _ctrl.text.trim()),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.pop(context), child: const Text('取消')),
        FilledButton(onPressed: () => Navigator.pop(context, _ctrl.text.trim()), child: const Text('确认')),
      ],
    );
  }
}
