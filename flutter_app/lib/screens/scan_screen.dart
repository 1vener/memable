// scan_screen.dart：桌面端扫描管理
// 代码注释使用中文
import 'package:flutter/material.dart';
import '../models/models.dart';
import '../services/api_service.dart';

class ScanScreen extends StatefulWidget {
  final ApiService api;
  const ScanScreen({super.key, required this.api});

  @override
  State<ScanScreen> createState() => _ScanScreenState();
}

class _ScanScreenState extends State<ScanScreen> {
  List<Library> _libs = [];
  String? _sessionId;
  String _status = '';
  Map<String, dynamic>? _sessionData;
  bool _scanning = false;

  @override
  void initState() {
    super.initState();
    _loadLibraries();
  }

  Future<void> _loadLibraries() async {
    try {
      final libs = await widget.api.getLibraries();
      setState(() => _libs = libs);
    } catch (_) {}
  }

  Future<void> _startScan(int libraryId, String libName) async {
    setState(() => _scanning = true);
    try {
      final result = await widget.api.scanLibrary(libraryId);
      setState(() {
        _sessionId = result['session_id'];
        _status = result['status'];
      });
      _pollSession();
    } catch (e) {
      _showSnack('启动扫描失败: $e');
      setState(() => _scanning = false);
    }
  }

  Future<void> _startTempScan() async {
    final ctrl = TextEditingController();
    final result = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('临时目录扫描'),
        content: SizedBox(
          width: 400,
          child: TextField(
            controller: ctrl,
            decoration: const InputDecoration(labelText: '目录路径', hintText: '如：D:/Downloads'),
            autofocus: true,
          ),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('取消')),
          FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('开始扫描')),
        ],
      ),
    );
    if (result == true && ctrl.text.isNotEmpty) {
      setState(() => _scanning = true);
      try {
        final r = await widget.api.scanTemporary(ctrl.text);
        setState(() {
          _sessionId = r['session_id'];
          _status = r['status'];
        });
        _pollSession();
      } catch (e) {
        _showSnack('启动临时扫描失败: $e');
        setState(() => _scanning = false);
      }
    }
  }

  Future<void> _pollSession() async {
    while (_sessionId != null && _status == 'running') {
      await Future.delayed(const Duration(seconds: 2));
      try {
        final data = await widget.api.getSession(_sessionId!);
        if (!mounted) return;
        setState(() {
          _sessionData = data;
          _status = (data['session'] as Map?)?['status'] ?? '';
        });
        if (_status != 'running') {
          setState(() => _scanning = false);
          break;
        }
      } catch (_) {
        setState(() => _scanning = false);
        break;
      }
    }
  }

  void _showSnack(String msg) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(msg)));
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Text('扫描管理', style: TextStyle(fontSize: 22, fontWeight: FontWeight.w700)),
                const Spacer(),
                FilledButton.tonalIcon(
                  onPressed: _scanning ? null : _startTempScan,
                  icon: const Icon(Icons.create_new_folder_outlined, size: 18),
                  label: const Text('临时目录扫描'),
                ),
              ],
            ),
            const SizedBox(height: 4),
            const Text('选择收藏库启动扫描，或对任意目录发起临时扫描', style: TextStyle(fontSize: 13, color: Color(0xFF64748B))),
            const SizedBox(height: 20),

            // 当前会话状态
            if (_sessionId != null) _buildSessionCard(),
            if (_sessionId != null) const SizedBox(height: 16),

            // 库列表
            Expanded(
              child: Card(
                clipBehavior: Clip.antiAlias,
                child: ListView.separated(
                  padding: const EdgeInsets.all(16),
                  itemCount: _libs.length,
                  separatorBuilder: (_, __) => const Divider(height: 1, color: Color(0xFFE2E8F0)),
                  itemBuilder: (ctx, i) {
                    final lib = _libs[i];
                    return ListTile(
                      leading: CircleAvatar(
                        backgroundColor: const Color(0xFFEFF6FF),
                        child: Icon(
                          lib.kind == 'image' ? Icons.image_outlined : lib.kind == 'video' ? Icons.video_library_outlined : Icons.folder_outlined,
                          color: const Color(0xFF2563EB),
                        ),
                      ),
                      title: Text(lib.name, style: const TextStyle(fontWeight: FontWeight.w500)),
                      subtitle: Text(lib.path, style: const TextStyle(fontSize: 12, color: Color(0xFF64748B))),
                      trailing: FilledButton.tonal(
                        onPressed: _scanning ? null : () => _startScan(lib.id, lib.name),
                        child: const Text('扫描'),
                      ),
                    );
                  },
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildSessionCard() {
    final session = _sessionData?['session'] as Map? ?? {};
    final mediaCount = _sessionData?['count'] as int? ?? 0;
    final status = session['status'] as String? ?? _status;

    return Card(
      color: status == 'running' ? const Color(0xFFEFF6FF) : const Color(0xFFF0FDF4),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  status == 'running' ? Icons.sync : Icons.check_circle,
                  color: status == 'running' ? const Color(0xFF2563EB) : const Color(0xFF16A34A),
                ),
                const SizedBox(width: 8),
                Text(
                  status == 'running' ? '扫描进行中' : '扫描完成',
                  style: TextStyle(
                    fontWeight: FontWeight.w600,
                    color: status == 'running' ? const Color(0xFF2563EB) : const Color(0xFF16A34A),
                  ),
                ),
                const Spacer(),
                if (status == 'running')
                  TextButton.icon(
                    onPressed: () {
                      widget.api.cancelSession(_sessionId!);
                      setState(() => _scanning = false);
                    },
                    icon: const Icon(Icons.stop, size: 16),
                    label: const Text('取消'),
                    style: TextButton.styleFrom(foregroundColor: const Color(0xFFEF4444)),
                  ),
              ],
            ),
            const SizedBox(height: 8),
            Row(
              children: [
                _infoChip('会话', _sessionId ?? '-'),
                const SizedBox(width: 12),
                _infoChip('已发现', '$mediaCount 个媒体'),
                const SizedBox(width: 12),
                _infoChip('状态', status),
              ],
            ),
            if (status == 'running') ...[
              const SizedBox(height: 12),
              const LinearProgressIndicator(),
            ],
          ],
        ),
      ),
    );
  }

  Widget _infoChip(String label, String value) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      decoration: BoxDecoration(
        color: Colors.white.withValues(alpha: 0.7),
        borderRadius: BorderRadius.circular(6),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Text('$label: ', style: const TextStyle(fontSize: 12, color: Color(0xFF64748B))),
          Text(value, style: const TextStyle(fontSize: 12, fontWeight: FontWeight.w600)),
        ],
      ),
    );
  }
}
