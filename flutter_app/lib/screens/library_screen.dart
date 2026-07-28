// library_screen.dart：桌面端收藏库管理
// 代码注释使用中文
import 'package:flutter/material.dart';
import '../models/models.dart';
import '../services/api_service.dart';

class LibraryScreen extends StatefulWidget {
  final ApiService api;
  const LibraryScreen({super.key, required this.api});

  @override
  State<LibraryScreen> createState() => _LibraryScreenState();
}

class _LibraryScreenState extends State<LibraryScreen> {
  List<Library> _libs = [];
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    _loadLibraries();
  }

  Future<void> _loadLibraries() async {
    setState(() => _loading = true);
    try {
      final libs = await widget.api.getLibraries();
      setState(() => _libs = libs);
    } catch (e) {
      _showError('加载失败', e);
    } finally {
      setState(() => _loading = false);
    }
  }

  void _showError(String title, Object e) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text('$title: $e'), backgroundColor: const Color(0xFFEF4444)),
    );
  }

  Future<void> _addLibrary() async {
    final nameCtrl = TextEditingController();
    final pathCtrl = TextEditingController();
    String kind = 'mixed';

    final result = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('新建收藏库'),
        content: SizedBox(
          width: 400,
          child: Column(mainAxisSize: MainAxisSize.min, children: [
            TextField(
              controller: nameCtrl,
              decoration: const InputDecoration(labelText: '名称', hintText: '如：照片库'),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: pathCtrl,
              decoration: const InputDecoration(labelText: '根目录路径', hintText: '如：D:/Pictures'),
            ),
            const SizedBox(height: 12),
            StatefulBuilder(builder: (ctx, setState) {
              return DropdownButtonFormField<String>(
                value: kind,
                decoration: const InputDecoration(labelText: '类型'),
                items: const [
                  DropdownMenuItem(value: 'image', child: Text('图片')),
                  DropdownMenuItem(value: 'video', child: Text('视频')),
                  DropdownMenuItem(value: 'mixed', child: Text('混合')),
                ],
                onChanged: (v) => setState(() => kind = v ?? 'mixed'),
              );
            }),
          ]),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('取消')),
          FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('创建')),
        ],
      ),
    );

    if (result == true && nameCtrl.text.isNotEmpty && pathCtrl.text.isNotEmpty) {
      try {
        await widget.api.createLibrary(nameCtrl.text, pathCtrl.text, kind);
        _loadLibraries();
      } catch (e) {
        _showError('创建失败', e);
      }
    }
  }

  Future<void> _deleteLibrary(Library lib) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        icon: const Icon(Icons.warning_amber, color: Color(0xFFF59E0B), size: 48),
        title: const Text('确认删除'),
        content: Text('删除收藏库「${lib.name}」？\n\n将级联删除该库下所有媒体记录和缩略图文件。'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('取消')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: const Color(0xFFEF4444)),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('删除'),
          ),
        ],
      ),
    );
    if (confirmed == true) {
      try {
        await widget.api.deleteLibrary(lib.id);
        _loadLibraries();
      } catch (e) {
        _showError('删除失败', e);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // 页面标题栏
            Row(
              children: [
                const Text('收藏库管理', style: TextStyle(fontSize: 22, fontWeight: FontWeight.w700)),
                const Spacer(),
                FilledButton.icon(
                  onPressed: _addLibrary,
                  icon: const Icon(Icons.add, size: 18),
                  label: const Text('新建收藏库'),
                ),
              ],
            ),
            const SizedBox(height: 4),
            Text(
              '共 ${_libs.length} 个收藏库',
              style: const TextStyle(fontSize: 13, color: Color(0xFF64748B)),
            ),
            const SizedBox(height: 20),

            // 表格
            Expanded(
              child: _loading
                  ? const Center(child: CircularProgressIndicator())
                  : _libs.isEmpty
                      ? _buildEmptyState()
                      : _buildTable(),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildEmptyState() {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(Icons.folder_open, size: 64, color: Colors.grey[300]),
          const SizedBox(height: 16),
          const Text('暂无收藏库', style: TextStyle(fontSize: 16, color: Color(0xFF94A3B8))),
          const SizedBox(height: 8),
          const Text('点击右上角「新建收藏库」添加媒体目录', style: TextStyle(fontSize: 13, color: Color(0xFF94A3B8))),
        ],
      ),
    );
  }

  Widget _buildTable() {
    return Card(
      clipBehavior: Clip.antiAlias,
      child: SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        child: ConstrainedBox(
          constraints: BoxConstraints(minWidth: MediaQuery.of(context).size.width - 290),
          child: DataTable(
            headingRowColor: WidgetStateProperty.all(const Color(0xFFF8FAFC)),
            headingTextStyle: const TextStyle(fontSize: 13, fontWeight: FontWeight.w600, color: Color(0xFF475569)),
            dataTextStyle: const TextStyle(fontSize: 13, color: Color(0xFF1E293B)),
            columnSpacing: 24,
            horizontalMargin: 20,
            columns: const [
              DataColumn(label: Text('ID')),
              DataColumn(label: Text('名称')),
              DataColumn(label: Text('路径')),
              DataColumn(label: Text('类型')),
              DataColumn(label: Text('操作')),
            ],
            rows: _libs.map((lib) {
              return DataRow(cells: [
                DataCell(Text('#${lib.id}')),
                DataCell(Row(children: [
                  Icon(
                    lib.kind == 'image' ? Icons.image_outlined : lib.kind == 'video' ? Icons.video_library_outlined : Icons.folder_outlined,
                    size: 18,
                    color: const Color(0xFF64748B),
                  ),
                  const SizedBox(width: 8),
                  Text(lib.name, style: const TextStyle(fontWeight: FontWeight.w500)),
                ])),
                DataCell(
                  SizedBox(width: 300, child: Text(lib.path, overflow: TextOverflow.ellipsis)),
                ),
                DataCell(_kindChip(lib.kind)),
                DataCell(Row(mainAxisSize: MainAxisSize.min, children: [
                  IconButton(
                    icon: const Icon(Icons.edit_outlined, size: 18),
                    tooltip: '编辑路径',
                    onPressed: () => _editPath(lib),
                  ),
                  IconButton(
                    icon: const Icon(Icons.delete_outline, size: 18),
                    tooltip: '删除',
                    onPressed: () => _deleteLibrary(lib),
                  ),
                ])),
              ]);
            }).toList(),
          ),
        ),
      ),
    );
  }

  Widget _kindChip(String kind) {
    final (label, color) = switch (kind) {
      'image' => ('图片', const Color(0xFF3B82F6)),
      'video' => ('视频', const Color(0xFF8B5CF6)),
      _ => ('混合', const Color(0xFF6B7280)),
    };
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.1),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Text(label, style: TextStyle(fontSize: 12, color: color, fontWeight: FontWeight.w500)),
    );
  }

  Future<void> _editPath(Library lib) async {
    final ctrl = TextEditingController(text: lib.path);
    final result = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('迁移根目录'),
        content: SizedBox(
          width: 400,
          child: TextField(
            controller: ctrl,
            decoration: const InputDecoration(labelText: '新路径'),
            autofocus: true,
          ),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('取消')),
          FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('迁移')),
        ],
      ),
    );
    if (result == true && ctrl.text.isNotEmpty && ctrl.text != lib.path) {
      try {
        await widget.api.updateLibraryPath(lib.id, ctrl.text);
        _loadLibraries();
      } catch (e) {
        _showError('迁移失败', e);
      }
    }
  }
}
