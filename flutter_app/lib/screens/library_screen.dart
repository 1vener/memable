// library_screen.dart：收藏库管理页面（左列表 + 右文件树 + 右键菜单）
// 代码注释使用中文
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:file_picker/file_picker.dart';
import '../models/models.dart';
import '../services/api_service.dart';
import '../widgets/context_menu.dart';
import '../widgets/path_dialog.dart';
import '../widgets/resizable_split.dart';

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
      builder:
          (ctx) =>
              _NameDialog(defaultName: dir!.split('/').last.split('\\').last),
    );
    if (name == null || name.isEmpty) return;

    try {
      await widget.api.createLibrary(name, dir);
      await _loadLibraries();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('创建失败: $e'),
            backgroundColor: const Color(0xFFEF4444),
          ),
        );
      }
    }
  }

  // 手动输入服务端可访问的本地绝对路径。
  Future<String?> _showPathDialog() {
    return showDialog<String>(
      context: context,
      builder:
          (ctx) => const PathDialog(
            title: '输入媒体库目录',
            description: '请输入 Go 服务端所在电脑可访问的绝对路径。',
          ),
    );
  }

  Future<void> _syncLibrary(Library lib) async {
    try {
      await widget.api.scanLibrary(lib.id);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('同步扫描已启动: ${lib.name}'),
            backgroundColor: const Color(0xFF2563EB),
          ),
        );
      }
      widget.onLibrarySelected(lib.name);
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('启动同步扫描失败: $e'),
            backgroundColor: const Color(0xFFEF4444),
          ),
        );
      }
    }
  }

  Future<void> _deleteLibrary(Library lib) async {
    final ok = await showDialog<bool>(
      context: context,
      builder:
          (ctx) => AlertDialog(
            title: const Text('确认删除'),
            content: Text('确定要删除库「${lib.name}」吗？此操作不可恢复。'),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(ctx, false),
                child: const Text('取消'),
              ),
              TextButton(
                onPressed: () => Navigator.pop(ctx, true),
                child: const Text(
                  '删除',
                  style: TextStyle(color: Color(0xFFEF4444)),
                ),
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
          SnackBar(
            content: Text('删除失败: $e'),
            backgroundColor: const Color(0xFFEF4444),
          ),
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
              child: Text(
                _error!,
                style: TextStyle(fontSize: 13, color: cs.outline),
                overflow: TextOverflow.ellipsis,
                maxLines: 3,
              ),
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

    return ResizableSplit(
      initialRatio: 0.28,
      minLeftWidth: 200,
      maxLeftWidth: 500,
      minRightWidth: 300,
      hitAreaWidth: 4,
      left: Column(
        children: [
          // 工具栏
          Container(
            height: 52,
            padding: const EdgeInsets.symmetric(horizontal: 16),
            decoration: BoxDecoration(
              border: Border(
                bottom: BorderSide(color: cs.outlineVariant, width: 0.5),
              ),
            ),
            child: Row(
              children: [
                Text(
                  '收藏库 (${_libraries.length})',
                  style: TextStyle(
                    fontSize: 14,
                    fontWeight: FontWeight.w600,
                    color: cs.onSurface,
                  ),
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
            child:
                _libraries.isEmpty
                    ? Center(
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          Icon(Icons.folder_off, size: 48, color: cs.outline),
                          const SizedBox(height: 12),
                          Text(
                            '暂无收藏库',
                            style: TextStyle(fontSize: 14, color: cs.outline),
                          ),
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
                          onSync: () => _syncLibrary(lib),
                        );
                      },
                    ),
          ),
        ],
      ),
      right:
          _selected == null
              ? Center(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Icon(
                      Icons.folder_open,
                      size: 64,
                      color: cs.outline.withValues(alpha: 0.5),
                    ),
                    const SizedBox(height: 16),
                    Text(
                      '选择一个库查看详情',
                      style: TextStyle(fontSize: 15, color: cs.outline),
                    ),
                  ],
                ),
              )
              : _FileTreePanel(library: _selected!, api: widget.api),
    );
  }
}

/// 库卡片
class _LibraryCard extends StatelessWidget {
  final Library library;
  final bool selected;
  final VoidCallback onTap;
  final VoidCallback onDelete;
  final VoidCallback onSync;

  const _LibraryCard({
    required this.library,
    required this.selected,
    required this.onTap,
    required this.onDelete,
    required this.onSync,
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
                    icon: Icons.sync,
                    label: '同步扫描',
                    onTap: onSync,
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
                color:
                    selected
                        ? cs.primary.withValues(
                          alpha:
                              Theme.of(context).brightness == Brightness.dark
                                  ? 0.15
                                  : 0.1,
                        )
                        : Colors.transparent,
                borderRadius: BorderRadius.circular(10),
                border:
                    selected
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

/// 文件树 + 缩略图浏览面板（IDEA 风格懒加载）
class _FileTreePanel extends StatefulWidget {
  final Library library;
  final ApiService api;

  const _FileTreePanel({required this.library, required this.api});

  @override
  State<_FileTreePanel> createState() => _FileTreePanelState();
}

class _FileTreePanelState extends State<_FileTreePanel> {
  // 根目录直属子项（懒加载的第一层）
  List<FileTreeNode> _rootChildren = [];
  bool _loadingTree = true;
  String? _error;

  // 已展开的目录路径 -> 子节点列表
  final Map<String, List<FileTreeNode>> _expandedChildren = {};
  // 正在加载子项的路径
  final Set<String> _loadingPaths = {};
  // 展开状态
  final Set<String> _expandedPaths = {};

  // 选中的目录
  String _selectedDir = '';
  List<Media> _files = [];
  bool _loadingFiles = false;
  String? _typeFilter; // null=全部, image=图片, video=视频

  @override
  void initState() {
    super.initState();
    _loadRootChildren();
  }

  Future<void> _loadRootChildren() async {
    setState(() {
      _loadingTree = true;
      _error = null;
    });
    try {
      final children = await widget.api.getFileTree(widget.library.id);
      if (mounted)
        setState(() {
          _rootChildren = children;
          _loadingTree = false;
        });
    } catch (e) {
      if (mounted)
        setState(() {
          _error = '$e';
          _loadingTree = false;
        });
    }
  }

  /// 展开/折叠目录节点
  Future<void> _toggleDir(String path) async {
    if (_expandedPaths.contains(path)) {
      setState(() {
        _expandedPaths.remove(path);
      });
      return;
    }
    // 懒加载子节点
    setState(() {
      _loadingPaths.add(path);
    });
    try {
      final children = await widget.api.getFileTree(
        widget.library.id,
        path: path,
      );
      if (mounted) {
        setState(() {
          _expandedPaths.add(path);
          _expandedChildren[path] = children;
          _loadingPaths.remove(path);
        });
      }
    } catch (e) {
      if (mounted)
        setState(() {
          _loadingPaths.remove(path);
        });
    }
  }

  Future<void> _selectDir(String dir) async {
    setState(() {
      _selectedDir = dir;
      _loadingFiles = true;
      _files = [];
    });
    try {
      final files = await widget.api.getFiles(widget.library.id, path: dir);
      if (mounted)
        setState(() {
          _files = files;
          _loadingFiles = false;
        });
    } catch (e) {
      if (mounted)
        setState(() {
          _files = [];
          _loadingFiles = false;
        });
    }
  }

  void _showDeleteDialog(String dirPath, String dirName) {
    showDialog(
      context: context,
      builder:
          (ctx) => AlertDialog(
            title: const Text('删除目录'),
            content: Text(
              '确定要永久删除目录「$dirName」及其所有内容吗？\n\n此操作不可恢复，将同时删除本地文件和数据库记录。',
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(ctx),
                child: const Text('取消'),
              ),
              FilledButton(
                style: FilledButton.styleFrom(
                  backgroundColor: const Color(0xFFEF4444),
                ),
                onPressed: () async {
                  Navigator.pop(ctx);
                  await _deleteDir(dirPath, dirName);
                },
                child: const Text('永久删除'),
              ),
            ],
          ),
    );
  }

  Future<void> _deleteDir(String dirPath, String dirName) async {
    try {
      final result = await widget.api.deleteDirectory(
        widget.library.id,
        dirPath,
      );
      if (mounted) {
        final pos = result['queue_position'] ?? 0;
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('删除「$dirName」任务已提交 (排队第$pos位)'),
            backgroundColor: const Color(0xFFF59E0B),
          ),
        );
        // 刷新树和右侧文件列表
        _expandedPaths.remove(dirPath);
        _expandedChildren.remove(dirPath);
        await _loadRootChildren();
        if (_selectedDir == dirPath || _selectedDir.startsWith('$dirPath/')) {
          _selectedDir = '';
          _files = [];
        }
        await _selectDir(_selectedDir);
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('删除失败: $e'),
            backgroundColor: const Color(0xFFEF4444),
          ),
        );
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;

    return ResizableSplit(
      initialRatio: 0.35,
      minLeftWidth: 180,
      maxLeftWidth: 500,
      minRightWidth: 300,
      hitAreaWidth: 4,
      left:
          _loadingTree
              ? const Center(child: CircularProgressIndicator())
              : _error != null
              ? Center(child: Text(_error!, style: TextStyle(color: cs.error)))
              : Column(
                children: [
                  // 头部：库名 + 操作
                  Container(
                    height: 52,
                    padding: const EdgeInsets.symmetric(horizontal: 16),
                    decoration: BoxDecoration(
                      border: Border(
                        bottom: BorderSide(
                          color: cs.outlineVariant,
                          width: 0.5,
                        ),
                      ),
                    ),
                    child: Row(
                      children: [
                        Expanded(
                          child: Text(
                            widget.library.name,
                            style: TextStyle(
                              fontSize: 14,
                              fontWeight: FontWeight.w600,
                              color: cs.onSurface,
                            ),
                            overflow: TextOverflow.ellipsis,
                          ),
                        ),
                        IconButton(
                          icon: const Icon(Icons.sync, size: 18),
                          tooltip: '同步扫描',
                          onPressed: () async {
                            try {
                              await widget.api.scanLibrary(widget.library.id);
                              if (context.mounted) {
                                ScaffoldMessenger.of(context).showSnackBar(
                                  const SnackBar(
                                    content: Text('同步扫描已启动'),
                                    backgroundColor: Color(0xFF2563EB),
                                  ),
                                );
                              }
                            } catch (e) {
                              if (context.mounted) {
                                ScaffoldMessenger.of(context).showSnackBar(
                                  SnackBar(
                                    content: Text('同步扫描失败: $e'),
                                    backgroundColor: const Color(0xFFEF4444),
                                  ),
                                );
                              }
                            }
                          },
                        ),
                        IconButton(
                          icon: const Icon(Icons.refresh, size: 18),
                          tooltip: '刷新文件树',
                          onPressed: () {
                            _expandedPaths.clear();
                            _expandedChildren.clear();
                            _loadingPaths.clear();
                            _loadRootChildren();
                          },
                        ),
                      ],
                    ),
                  ),
                  Expanded(
                    child: ListView(
                      padding: const EdgeInsets.all(8),
                      children: [
                        // 根目录节点：显示完整路径，浅色
                        _RootDirTile(
                          fullPath: widget.library.path,
                          selected: _selectedDir == '',
                          onTap: () => _selectDir(''),
                        ),
                        // 懒加载子目录
                        ..._buildDirTiles(_rootChildren, 0),
                      ],
                    ),
                  ),
                ],
              ),
      right:
          _loadingFiles
              ? const Center(child: CircularProgressIndicator())
              : _files.isEmpty
              ? Center(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Icon(
                      Icons.image_outlined,
                      size: 64,
                      color: cs.outline.withValues(alpha: 0.5),
                    ),
                    const SizedBox(height: 16),
                    Text(
                      '此目录暂无媒体文件',
                      style: TextStyle(fontSize: 15, color: cs.outline),
                    ),
                  ],
                ),
              )
              : _buildThumbnailGrid(cs),
    );
  }

  List<Widget> _buildDirTiles(List<FileTreeNode> nodes, int depth) {
    final tiles = <Widget>[];
    for (final node in nodes) {
      if (!node.isDir) continue;
      final isExpanded = _expandedPaths.contains(node.path);
      final isLoading = _loadingPaths.contains(node.path);
      final children = _expandedChildren[node.path];

      tiles.add(
        _TreeDirTile(
          name: node.name,
          path: node.path,
          depth: depth,
          selected: _selectedDir == node.path,
          expanded: isExpanded,
          loading: isLoading,
          hasChildren: node.hasChildren,
          onTap: () => _selectDir(node.path),
          onToggle: node.hasChildren ? () => _toggleDir(node.path) : null,
          onContextMenu: (details) {
            showContextMenu(
              context: context,
              position: details.globalPosition,
              items: [
                ContextMenuItem(
                  icon: Icons.delete_outline,
                  label: '删除目录',
                  isDestructive: true,
                  onTap: () => _showDeleteDialog(node.path, node.name),
                ),
              ],
            );
          },
        ),
      );

      // 展开时显示子节点
      if (isExpanded && children != null) {
        tiles.addAll(_buildDirTiles(children, depth + 1));
      }
    }
    return tiles;
  }

  Widget _buildThumbnailGrid(ColorScheme cs) {
    final displayFiles = _filterFiles();
    final totalSize = displayFiles.fold<int>(
      0,
      (s, m) => s + (m.fileSize > 0 ? m.fileSize : 0),
    );
    final imageCount = displayFiles.where((m) => m.kind == 'image').length;
    final videoCount = displayFiles.where((m) => m.kind == 'video').length;

    return RefreshIndicator(
      onRefresh: () => _selectDir(_selectedDir),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // 面包屑/路径
            Padding(
              padding: const EdgeInsets.only(bottom: 12),
              child: Text(
                _selectedDir.isEmpty
                    ? widget.library.path
                    : '${widget.library.path}/$_selectedDir',
                style: TextStyle(fontSize: 13, color: cs.outline),
                overflow: TextOverflow.ellipsis,
              ),
            ),
            // 类型筛选标签
            Padding(
              padding: const EdgeInsets.only(bottom: 12),
              child: Row(
                children: [
                  _FilterChip(
                    label: '全部',
                    count: _files.length,
                    selected: _typeFilter == null,
                    color: cs.outline,
                    onTap: () => setState(() => _typeFilter = null),
                  ),
                  const SizedBox(width: 8),
                  _FilterChip(
                    label: '图片',
                    count: imageCount,
                    selected: _typeFilter == 'image',
                    color: cs.primary,
                    onTap: () => setState(() => _typeFilter = 'image'),
                  ),
                  const SizedBox(width: 8),
                  _FilterChip(
                    label: '视频',
                    count: videoCount,
                    selected: _typeFilter == 'video',
                    color: const Color(0xFF7C3AED),
                    onTap: () => setState(() => _typeFilter = 'video'),
                  ),
                ],
              ),
            ),
            // 网格
            Expanded(
              child: GridView.builder(
                gridDelegate: const SliverGridDelegateWithMaxCrossAxisExtent(
                  maxCrossAxisExtent: 180,
                  crossAxisSpacing: 12,
                  mainAxisSpacing: 12,
                  childAspectRatio: 0.88,
                ),
                itemCount: displayFiles.length,
                itemBuilder: (context, index) {
                  final m = displayFiles[index];
                  final thumbUrl =
                      m.thumbnailPath != null
                          ? widget.api.thumbnailUrl(m.thumbnailPath!)
                          : null;
                  return _MediaThumbCard(
                    media: m,
                    thumbUrl: thumbUrl,
                    onOpenFile: () => _openMediaFile(m.id, context),
                    onOpenDirectory: () => _openMediaDirectory(m.id, context),
                  );
                },
              ),
            ),
            // 底部统计栏
            Container(
              height: 44,
              padding: const EdgeInsets.symmetric(horizontal: 12),
              decoration: BoxDecoration(
                color: cs.surfaceContainerLow,
                border: Border(
                  top: BorderSide(color: cs.outlineVariant, width: 0.5),
                ),
                borderRadius: const BorderRadius.vertical(
                  bottom: Radius.circular(12),
                ),
              ),
              child: Row(
                children: [
                  Text(
                    '共 ${displayFiles.length} 项',
                    style: TextStyle(fontSize: 12, color: cs.onSurfaceVariant),
                  ),
                  if (imageCount > 0) ...[
                    const SizedBox(width: 8),
                    Icon(Icons.image_rounded, size: 13, color: cs.primary),
                    const SizedBox(width: 3),
                    Text(
                      '$imageCount',
                      style: TextStyle(
                        fontSize: 12,
                        color: cs.onSurfaceVariant,
                      ),
                    ),
                  ],
                  if (videoCount > 0) ...[
                    const SizedBox(width: 8),
                    const Icon(
                      Icons.videocam_rounded,
                      size: 13,
                      color: Color(0xFF7C3AED),
                    ),
                    const SizedBox(width: 3),
                    Text(
                      '$videoCount',
                      style: TextStyle(
                        fontSize: 12,
                        color: cs.onSurfaceVariant,
                      ),
                    ),
                  ],
                  const Spacer(),
                  Text(
                    _formatBytes(totalSize),
                    style: TextStyle(fontSize: 12, color: cs.onSurfaceVariant),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  List<Media> _filterFiles() {
    if (_typeFilter == null) return _files;
    return _files.where((m) => m.kind == _typeFilter).toList();
  }

  String _formatBytes(int bytes) {
    if (bytes < 1024) return '$bytes B';
    if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)} KB';
    if (bytes < 1024 * 1024 * 1024) {
      return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
    }
    return '${(bytes / (1024 * 1024 * 1024)).toStringAsFixed(2)} GB';
  }

  Future<void> _openMediaFile(int mediaId, BuildContext context) async {
    debugPrint('[打开文件] mediaId=$mediaId');
    try {
      await widget.api.openMediaFile(mediaId);
    } catch (e) {
      debugPrint('[打开文件] 失败: $e');
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('$e'),
            backgroundColor: const Color(0xFFEF4444),
          ),
        );
      }
    }
  }

  Future<void> _openMediaDirectory(int mediaId, BuildContext context) async {
    debugPrint('[打开目录] mediaId=$mediaId');
    try {
      await widget.api.openMediaDirectory(mediaId);
    } catch (e) {
      debugPrint('[打开目录] 失败: $e');
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('$e'),
            backgroundColor: const Color(0xFFEF4444),
          ),
        );
      }
    }
  }
}

/// 根目录节点：显示完整路径，浅色
class _RootDirTile extends StatelessWidget {
  final String fullPath;
  final bool selected;
  final VoidCallback onTap;

  const _RootDirTile({
    required this.fullPath,
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
              color:
                  selected
                      ? cs.primary.withValues(
                        alpha:
                            Theme.of(context).brightness == Brightness.dark
                                ? 0.15
                                : 0.1,
                      )
                      : Colors.transparent,
              borderRadius: BorderRadius.circular(8),
              border:
                  selected
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
                    fullPath,
                    style: TextStyle(fontSize: 12, color: cs.outline),
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

/// 目录树条目（IDEA 风格：展开/折叠 + 缩进 + 右键菜单）
class _TreeDirTile extends StatelessWidget {
  final String name;
  final String path;
  final int depth;
  final bool selected;
  final bool expanded;
  final bool loading;
  final bool hasChildren;
  final VoidCallback onTap;
  final VoidCallback? onToggle;
  final GestureLongPressStartCallback? onContextMenu;

  const _TreeDirTile({
    required this.name,
    required this.path,
    this.depth = 0,
    required this.selected,
    this.expanded = false,
    this.loading = false,
    this.hasChildren = false,
    required this.onTap,
    this.onToggle,
    this.onContextMenu,
  });

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Padding(
      padding: const EdgeInsets.all(2),
      child: Material(
        color: Colors.transparent,
        borderRadius: BorderRadius.circular(8),
        child: GestureDetector(
          onLongPressStart: onContextMenu,
          child: InkWell(
            borderRadius: BorderRadius.circular(8),
            onTap: onTap,
            child: AnimatedContainer(
              duration: const Duration(milliseconds: 150),
              padding: EdgeInsets.only(
                left: 12.0 + depth * 16,
                right: 12,
                top: 10,
                bottom: 10,
              ),
              decoration: BoxDecoration(
                color:
                    selected
                        ? cs.primary.withValues(
                          alpha:
                              Theme.of(context).brightness == Brightness.dark
                                  ? 0.15
                                  : 0.1,
                        )
                        : Colors.transparent,
                borderRadius: BorderRadius.circular(8),
                border:
                    selected
                        ? Border.all(color: cs.primary.withValues(alpha: 0.3))
                        : null,
              ),
              child: Row(
                children: [
                  // 展开/折叠箭头
                  if (hasChildren)
                    GestureDetector(
                      onTap: onToggle,
                      child: Padding(
                        padding: const EdgeInsets.only(right: 4),
                        child:
                            loading
                                ? SizedBox(
                                  width: 14,
                                  height: 14,
                                  child: CircularProgressIndicator(
                                    strokeWidth: 1.5,
                                    color: cs.outline,
                                  ),
                                )
                                : Icon(
                                  expanded
                                      ? Icons.keyboard_arrow_down
                                      : Icons.keyboard_arrow_right,
                                  size: 16,
                                  color: cs.outline,
                                ),
                      ),
                    )
                  else
                    const SizedBox(width: 20),
                  // 文件夹图标
                  Icon(
                    expanded ? Icons.folder_open : Icons.folder,
                    size: 18,
                    color: selected ? cs.primary : cs.outline,
                  ),
                  const SizedBox(width: 8),
                  // 名称
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
      ),
    );
  }
}

/// 类型筛选标签
class _FilterChip extends StatelessWidget {
  final String label;
  final int count;
  final bool selected;
  final Color color;
  final VoidCallback onTap;

  const _FilterChip({
    required this.label,
    required this.count,
    required this.selected,
    required this.color,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return GestureDetector(
      onTap: onTap,
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 150),
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 5),
        decoration: BoxDecoration(
          color: selected ? color.withValues(alpha: 0.12) : Colors.transparent,
          borderRadius: BorderRadius.circular(14),
          border: Border.all(
            color: selected ? color.withValues(alpha: 0.5) : cs.outlineVariant,
            width: selected ? 1.5 : 1,
          ),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(
              label,
              style: TextStyle(
                fontSize: 12,
                fontWeight: selected ? FontWeight.w600 : FontWeight.normal,
                color: selected ? color : cs.onSurfaceVariant,
              ),
            ),
            if (count > 0) ...[
              const SizedBox(width: 4),
              Text(
                '$count',
                style: TextStyle(
                  fontSize: 11,
                  color: selected ? color.withValues(alpha: 0.7) : cs.outline,
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

/// 缩略图卡片
class _MediaThumbCard extends StatelessWidget {
  final Media media;
  final String? thumbUrl;
  final VoidCallback? onOpenFile;
  final VoidCallback? onOpenDirectory;

  const _MediaThumbCard({
    required this.media,
    required this.thumbUrl,
    this.onOpenFile,
    this.onOpenDirectory,
  });

  // 视频时长格式化
  static String formatDuration(int? ms) {
    if (ms == null || ms <= 0) return '';
    final totalSec = (ms / 1000).round();
    final h = totalSec ~/ 3600;
    final m = (totalSec % 3600) ~/ 60;
    final s = totalSec % 60;
    if (h > 0)
      return '$h:${m.toString().padLeft(2, '0')}:${s.toString().padLeft(2, '0')}';
    return '$m:${s.toString().padLeft(2, '0')}';
  }

  // 文件大小格式化
  static String formatSize(int? bytes) {
    if (bytes == null || bytes < 0) return '';
    if (bytes < 1024) return '$bytes B';
    if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)} KB';
    if (bytes < 1024 * 1024 * 1024) {
      return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
    }
    return '${(bytes / (1024 * 1024 * 1024)).toStringAsFixed(2)} GB';
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final isVideo = media.kind == 'video';
    final typeColor = isVideo ? const Color(0xFF7C3AED) : cs.primary;

    return GestureDetector(
      onSecondaryTapDown:
          (details) => _showContextMenu(context, details.globalPosition),
      onLongPressStart:
          (details) => _showContextMenu(context, details.globalPosition),
      onDoubleTap: onOpenFile,
      child: Card(
        clipBehavior: Clip.antiAlias,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            // 缩略图区域
            Expanded(
              child: Stack(
                fit: StackFit.expand,
                children: [
                  thumbUrl != null
                      ? Image.network(
                        thumbUrl!,
                        fit: BoxFit.cover,
                        errorBuilder:
                            (_, __, ___) => _placeholder(cs, typeColor),
                        loadingBuilder: (_, child, progress) {
                          if (progress == null) return child;
                          return const Center(
                            child: CircularProgressIndicator(strokeWidth: 2),
                          );
                        },
                      )
                      : _placeholder(cs, typeColor),
                  // 视频播放图标
                  if (isVideo)
                    Center(
                      child: Container(
                        width: 36,
                        height: 36,
                        decoration: BoxDecoration(
                          color: Colors.black.withValues(alpha: 0.45),
                          shape: BoxShape.circle,
                        ),
                        child: Icon(
                          Icons.play_arrow_rounded,
                          size: 24,
                          color: Colors.white.withValues(alpha: 0.85),
                        ),
                      ),
                    ),
                  // 类型图标（左上角）
                  Positioned(
                    top: 6,
                    left: 6,
                    child: Container(
                      padding: const EdgeInsets.all(3),
                      decoration: BoxDecoration(
                        color: typeColor.withValues(alpha: 0.85),
                        borderRadius: BorderRadius.circular(4),
                      ),
                      child: Icon(
                        isVideo ? Icons.videocam_rounded : Icons.image_rounded,
                        size: 11,
                        color: Colors.white,
                      ),
                    ),
                  ),
                  // 视频时长角标（右下角）
                  if (isVideo &&
                      media.durationMs != null &&
                      media.durationMs! > 0)
                    Positioned(
                      bottom: 6,
                      right: 6,
                      child: Container(
                        padding: const EdgeInsets.symmetric(
                          horizontal: 5,
                          vertical: 2,
                        ),
                        decoration: BoxDecoration(
                          color: Colors.black.withValues(alpha: 0.7),
                          borderRadius: BorderRadius.circular(3),
                        ),
                        child: Text(
                          formatDuration(media.durationMs),
                          style: const TextStyle(
                            fontSize: 10,
                            color: Colors.white,
                            fontWeight: FontWeight.w500,
                          ),
                        ),
                      ),
                    ),
                ],
              ),
            ),
            // 信息区域
            Padding(
              padding: const EdgeInsets.fromLTRB(8, 6, 8, 8),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    media.relativePath.split('/').last.split('\\').last,
                    style: TextStyle(fontSize: 11, color: cs.onSurface),
                    overflow: TextOverflow.ellipsis,
                    maxLines: 1,
                  ),
                  const SizedBox(height: 2),
                  Text(
                    _buildInfoLine(),
                    style: TextStyle(fontSize: 10, color: cs.outline),
                    overflow: TextOverflow.ellipsis,
                    maxLines: 1,
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  String _buildInfoLine() {
    final parts = <String>[];
    if (media.width != null && media.height != null) {
      parts.add('${media.width}×${media.height}');
    }
    if (media.fileSize > 0) {
      parts.add(formatSize(media.fileSize));
    }
    return parts.isEmpty ? '' : parts.join(' · ');
  }

  void _showContextMenu(BuildContext context, Offset position) {
    showContextMenu(
      context: context,
      position: position,
      items: [
        ContextMenuItem(
          icon:
              media.kind == 'video'
                  ? Icons.play_circle_outline
                  : Icons.image_outlined,
          label: media.kind == 'video' ? '打开视频' : '打开图片',
          onTap: onOpenFile,
        ),
        ContextMenuItem(
          icon: Icons.folder_open,
          label: '打开文件所在目录',
          onTap: onOpenDirectory,
        ),
        const ContextMenuItem.divider(),
        ContextMenuItem(
          icon: Icons.copy,
          label: '复制文件路径',
          onTap: () {
            // 使用 flutter 的 Clipboard
          },
        ),
      ],
    );
  }

  Widget _placeholder(ColorScheme cs, Color typeColor) {
    return Container(
      color: cs.surfaceContainerHighest,
      child: Icon(
        media.kind == 'video' ? Icons.videocam_outlined : Icons.image_outlined,
        size: 40,
        color: typeColor.withValues(alpha: 0.5),
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
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: const Text('取消'),
        ),
        FilledButton(
          onPressed: () => Navigator.pop(context, _ctrl.text.trim()),
          child: const Text('确认'),
        ),
      ],
    );
  }
}
