// scan_screen.dart：扫描界面
// 代码注释使用中文
import 'package:flutter/material.dart';
import '../services/api_service.dart';

class ScanScreen extends StatefulWidget {
  final ApiService api;
  const ScanScreen({super.key, required this.api});

  @override
  State<ScanScreen> createState() => _ScanScreenState();
}

class _ScanScreenState extends State<ScanScreen> {
  String _sessionId = '';
  String _status = '';
  Map<String, dynamic>? _sessionData;

  Future<void> _startScan(int libraryId) async {
    try {
      final result = await widget.api.scanLibrary(libraryId);
      setState(() {
        _sessionId = result['session_id'];
        _status = result['status'];
      });
      _pollSession();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('扫描失败: $e')));
      }
    }
  }

  Future<void> _pollSession() async {
    while (_sessionId.isNotEmpty && _status == 'running') {
      await Future.delayed(const Duration(seconds: 2));
      try {
        final data = await widget.api.getSession(_sessionId);
        setState(() {
          _sessionData = data;
          _status = (data['session'] as Map?)?['status'] ?? '';
        });
        if (_status != 'running') break;
      } catch (_) {
        break;
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('扫描')),
      body: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          const Text('扫描状态', style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold)),
          const SizedBox(height: 12),
          if (_sessionId.isNotEmpty)
            Card(child: ListTile(
              title: Text('会话: $_sessionId'),
              subtitle: Text('状态: $_status'),
              trailing: _status == 'running'
                  ? TextButton(onPressed: () => widget.api.cancelSession(_sessionId), child: const Text('取消'))
                  : null,
            )),
          if (_sessionData != null)
            Padding(padding: const EdgeInsets.only(top: 8), child: Text('已发现 ${_sessionData!['count'] ?? 0} 个媒体文件')),
        ]),
      ),
    );
  }
}
