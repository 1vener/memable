// library_screen.dart：收藏库管理页面（左列表 + 右文件树 + 右键菜单）
// 代码注释使用中文
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:file_picker/file_picker.dart';
import '../models/models.dart';
import '../services/api_service.dart';
import '../widgets/context_menu.dart';
import '../widgets/path_dialog.dart';

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
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 32),
              child: Text(_error!, style: TextStyle(fontSize: 13, color: cs.outline), overflow: TextOverflow.ellipsis, maxLines: 3),
            ),
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
              : _FileTreePanel(library: _selected!, api: widget.api),
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

/// 文件树 + 缩略图浏览面板
class _FileTreePanel extends StatefulWidget {
  final Library library;
  final ApiService api;

  const _FileTreePanel({required this.library, required this.api});

  @override
  State<_FileTreePanel> createState() => _FileTreePanelState();
}

class _FileTreePanelState extends State<_FileTreePanel> {
  List<FileTreeNode>? _tree;
  String? _error;
  bool _loadingTree = true;

  String _selectedDir = '';
  List<Media> _files = [];
  bool _loadingFiles = false;

  @override
  void initState() {
    super.initState();
    _loadTree();
  }

  Future<void> _loadTree() async {
    setState(() { _loadingTree = true; _error = null; });
    try {
      final tree = await widget.api.getFileTree(widget.library.id);
      if (mounted) setState(() { _tree = tree; _loadingTree = false; });
    } catch (e) {
      if (mounted) setState(() { _error = '$e'; _loadingTree = false; });
    }
  }

  Future<void> _selectDir(String dir) async {
    setState(() { _selectedDir = dir; _loadingFiles = true; _files = []; });
    try {
      final files = await widget.api.getFiles(widget.library.id, path: dir);
      if (mounted) setState(() { _files = files; _loadingFiles = false; });
    } catch (e) {
      if (mounted) setState(() { _files = []; _loadingFiles = false; });
    }
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;

    return Row(
      children: [
        // 左侧：目录树
        SizedBox(
          width: 260,
          child: _loadingTree
              ? const Center(child: CircularProgressIndicator())
              : _error != null
                  ? Center(child: Text(_error!, style: TextStyle(color: cs.error)))
                  : _tree == null || _tree!.isEmpty
                      ? Center(child: Text('目录为空', style: TextStyle(color: cs.outline)))
                      : Column(
                          children: [
                            // 头部：库名 + 操作
                            Container(
                              height: 52,
                              padding: const EdgeInsets.symmetric(horizontal: 16),
                              decoration: BoxDecoration(
                                border: Border(bottom: BorderSide(color: cs.outlineVariant, width: 0.5)),
                              ),
                              child: Row(
                                children: [
                                  Expanded(
                                    child: Text(
                                      widget.library.name,
                                      style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: cs.onSurface),
                                      overflow: TextOverflow.ellipsis,
                                    ),
                                  ),
                                  IconButton(
                                    icon: const Icon(Icons.healing, size: 18),
                                    tooltip: '修复扫描',
                                    onPressed: () async {
                                      try {
                                        await widget.api.repairLibrary(widget.library.id);
                                        if (context.mounted) {
                                          ScaffoldMessenger.of(context).showSnackBar(
                                            SnackBar(content: const Text('修复扫描已启动'), backgroundColor: const Color(0xFF2563EB)),
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
                                  ),
                                  IconButton(
                                    icon: const Icon(Icons.refresh, size: 18),
                                    tooltip: '刷新文件树',
                                    onPressed: _loadTree,
                                  ),
                                ],
                              ),
                            ),
                            Expanded(
                              child: ListView(
                                padding: const EdgeInsets.all(8),
                                children: [
                                  _TreeDirTile(
                                    name: '(根目录)',
                                    path: '',
                                    selected: _selectedDir == '',
                                    onTap: () => _selectDir(''),
                                  ),
                                  ..._buildDirTiles(_tree!),
                                ],
                              ),
                            ),
                          ],
                        ),
        ),
        // 分隔线
        VerticalDivider(width: 1, color: cs.outlineVariant),
        // 右侧：缩略图网格
        Expanded(
          child: _loadingFiles
              ? const Center(child: CircularProgressIndicator())
              : _files.isEmpty
                  ? Center(child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Icon(Icons.image_outlined, size: 64, color: cs.outline.withValues(alpha: 0.5)),
                        const SizedBox(height: 16),
                        Text('此目录暂无媒体文件', style: TextStyle(fontSize: 15, color: cs.outline)),
                      ],
                    ))
                  : _buildThumbnailGrid(cs),
        ),
      ],
    );
  }

  List<Widget> _buildDirTiles(List<FileTreeNode> nodes) {
    final tiles = <Widget>[];
    for (final node in nodes) {
      if (!node.isDir) continue;
      tiles.add(_TreeDirTile(
        name: node.name,
        path: node.path,
        selected: _selectedDir == node.path,
        onTap: () => _selectDir(node.path),
      ));
      if (node.children.isNotEmpty) {
        tiles.add(Padding(
          padding: const EdgeInsets.only(left: 16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: _buildDirTiles(node.children),
          ),
        ));
      }
    }
    return tiles;
  }

  Widget _buildThumbnailGrid(ColorScheme cs) {
    return RefreshIndicator(
      onRefresh: () => _selectDir(_selectedDir),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // 面包屑/路径
            Padding(
              padding: const EdgeInsets.only(bottom: 16),
              child: Text(
                _selectedDir.isEmpty ? '(根目录)' : _selectedDir,
                style: TextStyle(fontSize: 13, color: cs.outline),
                overflow: TextOverflow.ellipsis,
              ),
            ),
            Expanded(
              child: GridView.builder(
                gridDelegate: const SliverGridDelegateWithMaxCrossAxisExtent(
                  maxCrossAxisExtent: 180,
                  crossAxisSpacing: 12,
                  mainAxisSpacing: 12,
                  childAspectRatio: 1,
                ),
                itemCount: _files.length,
                itemBuilder: (context, index) {
                  final m = _files[index];
                  final thumbUrl = m.thumbnailPath != null
                      ? widget.api.thumbnailUrl(m.thumbnailPath!)
                      : null;
                  return _MediaThumbCard(
                    media: m,
                    thumbUrl: thumbUrl,
                  );
                },
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// 目录树条目
class _TreeDirTile extends StatelessWidget {
  final String name;
  final String path;
  final bool selected;
  final VoidCallback onTap;

  const _TreeDirTile({
    required this.name,
    required this.path,
    required this.selected,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Padding(
      padding: const EdgeInsets.all(2),
      child: Material(
        color: Colors.transparent,
        borderRadius: BorderRadius.circular(8),
        child: InkWell(
          borderRadius: BorderRadius.circular(8),
          onTap: onTap,
          child: AnimatedContainer(
            duration: const Duration(milliseconds: 150),
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
            decoration: BoxDecoration(
              color: selected
                  ? cs.primary.withValues(alpha: Theme.of(context).brightness == Brightness.dark ? 0.15 : 0.1)
                  : Colors.transparent,
              borderRadius: BorderRadius.circular(8),
              border: selected
                  ? Border.all(color: cs.primary.withValues(alpha: 0.3))
                  : null,
            ),
            child: Row(
              children: [
                Icon(
                  selected ? Icons.folder_open : Icons.folder,
                  size: 18,
                  color: selected ? cs.primary : cs.outline,
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    name,
                    style: TextStyle(fontSize: 13, color: cs.onSurface),
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// 缩略图卡片
class _MediaThumbCard extends StatelessWidget {
  final Media media;
  final String? thumbUrl;

  const _MediaThumbCard({required this.media, required this.thumbUrl});

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Card(
      clipBehavior: Clip.antiAlias,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Expanded(
            child: thumbUrl != null
                ? Image.network(
                    thumbUrl!,
                    fit: BoxFit.cover,
                    errorBuilder: (_, __, ___) => _placeholder(cs),
                    loadingBuilder: (_, child, progress) {
                      if (progress == null) return child;
                      return Center(child: CircularProgressIndicator(strokeWidth: 2));
                    },
                  )
                : _placeholder(cs),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(8, 4, 8, 8),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  media.relativePath.split('/').last.split('\\').last,
                  style: TextStyle(fontSize: 11, color: cs.onSurface),
                  overflow: TextOverflow.ellipsis,
                  maxLines: 1,
                ),
                if (media.width != null && media.height != null)
                  Text(
                    '${media.width}×${media.height}',
                    style: TextStyle(fontSize: 10, color: cs.outline),
                  ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _placeholder(ColorScheme cs) {
    return Container(
      color: cs.surfaceContainerHighest,
      child: Icon(
        media.kind == 'video' ? Icons.videocam_outlined : Icons.image_outlined,
        size: 40,
        color: cs.outline,
      ),
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
