// library_screen.dart：收藏库管理界面
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
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('加载失败: $e')));
      }
    } finally {
      setState(() => _loading = false);
    }
  }

  Future<void> _addLibrary() async {
    final nameCtrl = TextEditingController();
    final pathCtrl = TextEditingController();
    String kind = 'mixed';

    final result = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('新建收藏库'),
        content: Column(mainAxisSize: MainAxisSize.min, children: [
          TextField(controller: nameCtrl, decoration: const InputDecoration(labelText: '名称')),
          TextField(controller: pathCtrl, decoration: const InputDecoration(labelText: '根目录路径')),
          StatefulBuilder(builder: (ctx, setState) {
            return DropdownButton<String>(
              value: kind,
              items: const [
                DropdownMenuItem(value: 'image', child: Text('图片')),
                DropdownMenuItem(value: 'video', child: Text('视频')),
                DropdownMenuItem(value: 'mixed', child: Text('混合')),
              ],
              onChanged: (v) => setState(() => kind = v ?? 'mixed'),
            );
          }),
        ]),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('取消')),
          TextButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('创建')),
        ],
      ),
    );

    if (result == true && nameCtrl.text.isNotEmpty && pathCtrl.text.isNotEmpty) {
      try {
        await widget.api.createLibrary(nameCtrl.text, pathCtrl.text, kind);
        _loadLibraries();
      } catch (e) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('创建失败: $e')));
        }
      }
    }
  }

  Future<void> _deleteLibrary(Library lib) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('确认删除'),
        content: Text('删除收藏库 "${lib.name}"？\n关联的媒体记录和缩略图将一并删除。'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('取消')),
          TextButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('删除', style: TextStyle(color: Colors.red))),
        ],
      ),
    );
    if (confirmed == true) {
      try {
        await widget.api.deleteLibrary(lib.id);
        _loadLibraries();
      } catch (e) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('删除失败: $e')));
        }
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('收藏库管理')),
      floatingActionButton: FloatingActionButton(onPressed: _addLibrary, child: const Icon(Icons.add)),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : RefreshIndicator(
              onRefresh: _loadLibraries,
              child: ListView.builder(
                itemCount: _libs.length,
                itemBuilder: (ctx, i) {
                  final lib = _libs[i];
                  return Card(
                    child: ListTile(
                      leading: Icon(lib.kind == 'image' ? Icons.image : lib.kind == 'video' ? Icons.video_library : Icons.folder),
                      title: Text(lib.name),
                      subtitle: Text(lib.path),
                      trailing: IconButton(icon: const Icon(Icons.delete), onPressed: () => _deleteLibrary(lib)),
                    ),
                  );
                },
              ),
            ),
    );
  }
}
