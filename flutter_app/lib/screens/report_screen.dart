// report_screen.dart：重复报告界面
// 代码注释使用中文
import 'package:flutter/material.dart';
import '../services/api_service.dart';

class ReportScreen extends StatefulWidget {
  final ApiService api;
  const ReportScreen({super.key, required this.api});

  @override
  State<ReportScreen> createState() => _ReportScreenState();
}

class _ReportScreenState extends State<ReportScreen> {
  bool _loading = false;
  String _lastReport = '';

  Future<void> _generateImageReport() async {
    setState(() => _loading = true);
    try {
      final result = await widget.api.generateImageReport();
      setState(() => _lastReport = '图片报告: ${result["groups"]} 组重复 → ${result["report_path"]}');
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('生成失败: $e')));
      }
    } finally {
      setState(() => _loading = false);
    }
  }

  Future<void> _generateVideoReport() async {
    setState(() => _loading = true);
    try {
      final result = await widget.api.generateVideoReport();
      setState(() => _lastReport = '视频报告: ${result["groups"]} 组重复 → ${result["report_path"]}');
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('生成失败: $e')));
      }
    } finally {
      setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('重复报告')),
      body: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          const Text('生成重复/相似检测报告', style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold)),
          const SizedBox(height: 20),
          if (_loading) const CircularProgressIndicator(),
          if (!_loading) ...[
            ElevatedButton.icon(
              onPressed: _generateImageReport,
              icon: const Icon(Icons.image),
              label: const Text('生成图片重复报告'),
            ),
            const SizedBox(height: 12),
            ElevatedButton.icon(
              onPressed: _generateVideoReport,
              icon: const Icon(Icons.video_library),
              label: const Text('生成视频重复报告'),
            ),
          ],
          if (_lastReport.isNotEmpty) ...[
            const SizedBox(height: 24),
            Card(child: Padding(padding: const EdgeInsets.all(12), child: Text(_lastReport))),
          ],
        ]),
      ),
    );
  }
}
