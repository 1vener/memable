// search_screen.dart：搜索界面
// 代码注释使用中文
import 'package:flutter/material.dart';
import '../models/models.dart';
import '../services/api_service.dart';

class SearchScreen extends StatefulWidget {
  final ApiService api;
  const SearchScreen({super.key, required this.api});

  @override
  State<SearchScreen> createState() => _SearchScreenState();
}

class _SearchScreenState extends State<SearchScreen> {
  final _controller = TextEditingController();
  List<SearchResult> _results = [];
  bool _loading = false;

  Future<void> _search() async {
    if (_controller.text.isEmpty) return;
    setState(() => _loading = true);
    try {
      final results = await widget.api.searchText(_controller.text);
      setState(() => _results = results);
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('搜索失败: $e')));
      }
    } finally {
      setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('搜索')),
      body: Column(children: [
        Padding(
          padding: const EdgeInsets.all(12),
          child: Row(children: [
            Expanded(child: TextField(
              controller: _controller,
              decoration: const InputDecoration(
                labelText: '文件名或 SHA1',
                border: OutlineInputBorder(),
              ),
              onSubmitted: (_) => _search(),
            )),
            const SizedBox(width: 8),
            IconButton(onPressed: _search, icon: const Icon(Icons.search)),
          ]),
        ),
        if (_loading) const LinearProgressIndicator(),
        Expanded(child: ListView.builder(
          itemCount: _results.length,
          itemBuilder: (ctx, i) {
            final r = _results[i];
            return Card(child: ListTile(
              leading: r.media.thumbnailPath != null
                  ? Image.network(widget.api.thumbnailUrl(r.media.thumbnailPath!), width: 56, height: 56, fit: BoxFit.cover)
                  : Icon(r.media.kind == 'image' ? Icons.image : Icons.video_library),
              title: Text(r.fullPath, maxLines: 2, overflow: TextOverflow.ellipsis),
              subtitle: Text('${r.media.kind} · ${r.media.fileSize} B'),
            ));
          },
        )),
      ]),
    );
  }
}
