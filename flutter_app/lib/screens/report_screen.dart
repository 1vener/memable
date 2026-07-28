// report_screen.dart：桌面端重复报告
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

  Future<void> _generateReport(bool isImage) async {
    setState(() => _loading = true);
    try {
      final result = isImage
          ? await widget.api.generateImageReport()
          : await widget.api.generateVideoReport();
      final groups = result['groups'] as int? ?? 0;
      final path = result['report_path'] as String? ?? '';
      setState(() => _lastReport = '${isImage ? "图片" : "视频"}报告已生成：$groups 组重复 → $path');
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('生成失败: $e'), backgroundColor: const Color(0xFFEF4444)),
        );
      }
    } finally {
      setState(() => _loading = false);
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
            const Text('重复报告', style: TextStyle(fontSize: 22, fontWeight: FontWeight.w700)),
            const SizedBox(height: 4),
            const Text('对收藏库进行重复/相似检测，生成 HTML 报告', style: TextStyle(fontSize: 13, color: Color(0xFF64748B))),
            const SizedBox(height: 32),

            // 报告类型卡片
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                _reportCard(
                  icon: Icons.image_outlined,
                  title: '图片重复报告',
                  description: 'SHA1 精确重复 + pHash/dHash/aHash 相似检测',
                  color: const Color(0xFF3B82F6),
                  onTap: _loading ? null : () => _generateReport(true),
                ),
                const SizedBox(width: 16),
                _reportCard(
                  icon: Icons.video_library_outlined,
                  title: '视频重复报告',
                  description: 'SHA1 + oshash 粗筛 + sprite pHash 相似检测',
                  color: const Color(0xFF8B5CF6),
                  onTap: _loading ? null : () => _generateReport(false),
                ),
              ],
            ),

            if (_loading) ...[
              const SizedBox(height: 24),
              const Center(child: CircularProgressIndicator()),
              const SizedBox(height: 8),
              const Center(child: Text('正在分析媒体库...', style: TextStyle(color: Color(0xFF64748B)))),
            ],

            if (_lastReport.isNotEmpty) ...[
              const SizedBox(height: 24),
              Card(
                color: const Color(0xFFF0FDF4),
                child: Padding(
                  padding: const EdgeInsets.all(16),
                  child: Row(
                    children: [
                      const Icon(Icons.check_circle, color: Color(0xFF16A34A)),
                      const SizedBox(width: 12),
                      Expanded(child: Text(_lastReport, style: const TextStyle(fontSize: 14))),
                    ],
                  ),
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }

  Widget _reportCard({
    required IconData icon,
    required String title,
    required String description,
    required Color color,
    required VoidCallback? onTap,
  }) {
    return Expanded(
      child: Card(
        child: InkWell(
          borderRadius: BorderRadius.circular(12),
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Container(
                  width: 48,
                  height: 48,
                  decoration: BoxDecoration(
                    color: color.withValues(alpha: 0.1),
                    borderRadius: BorderRadius.circular(12),
                  ),
                  child: Icon(icon, color: color, size: 24),
                ),
                const SizedBox(height: 16),
                Text(title, style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
                const SizedBox(height: 6),
                Text(description, style: const TextStyle(fontSize: 13, color: Color(0xFF64748B), height: 1.4)),
                const SizedBox(height: 16),
                Row(
                  children: [
                    Text('生成报告', style: TextStyle(fontSize: 14, fontWeight: FontWeight.w500, color: color)),
                    const SizedBox(width: 4),
                    Icon(Icons.arrow_forward, size: 16, color: color),
                  ],
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
