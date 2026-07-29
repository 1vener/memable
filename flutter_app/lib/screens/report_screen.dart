// report_screen.dart：报告页面（提交重复检测报告任务）
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
  bool _submitting = false;
  String? _error;
  String? _successMessage;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;

    return Padding(
      padding: const EdgeInsets.all(32),
      child: SingleChildScrollView(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Card(
              child: Padding(
                padding: const EdgeInsets.all(24),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Icon(Icons.assessment, size: 24, color: cs.primary),
                        const SizedBox(width: 12),
                        Text(
                          '重复检测报告',
                          style: TextStyle(fontSize: 18, fontWeight: FontWeight.w700, color: cs.onSurface),
                        ),
                      ],
                    ),
                    const SizedBox(height: 12),
                    Text(
                      '对所有已扫描的媒体进行重复/相似度检测，生成 HTML 报告。报告将作为后台任务排队执行。',
                      style: TextStyle(fontSize: 14, color: cs.outline),
                    ),
                    const SizedBox(height: 24),
                    FilledButton.icon(
                      onPressed: _submitting ? null : _submitReport,
                      icon: _submitting
                          ? const SizedBox(
                              width: 18, height: 18,
                              child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white),
                            )
                          : const Icon(Icons.play_arrow, size: 20),
                      label: Text(_submitting ? '提交中...' : '生成报告'),
                    ),
                  ],
                ),
              ),
            ),
            const SizedBox(height: 16),

            if (_error != null)
              Card(
                child: Padding(
                  padding: const EdgeInsets.all(24),
                  child: Column(
                    children: [
                      Icon(Icons.error_outline, size: 48, color: cs.error),
                      const SizedBox(height: 12),
                      Text('报告提交失败', style: TextStyle(fontSize: 15, color: cs.onSurface)),
                      const SizedBox(height: 6),
                      Text(_error!, style: TextStyle(fontSize: 13, color: cs.outline), overflow: TextOverflow.ellipsis, maxLines: 3),
                    ],
                  ),
                ),
              ),

            if (_successMessage != null)
              Card(
                child: Padding(
                  padding: const EdgeInsets.all(24),
                  child: Column(
                    children: [
                      const Icon(Icons.check_circle, size: 48, color: Color(0xFF22C55E)),
                      const SizedBox(height: 12),
                      Text('报告任务已提交', style: TextStyle(fontSize: 15, fontWeight: FontWeight.w600, color: cs.onSurface)),
                      const SizedBox(height: 6),
                      Text(_successMessage!, style: TextStyle(fontSize: 13, color: cs.outline)),
                      const SizedBox(height: 8),
                      Text('请在「任务」页面查看进度', style: TextStyle(fontSize: 13, color: cs.primary)),
                    ],
                  ),
                ),
              ),

            if (_error == null && _successMessage == null && !_submitting)
              Card(
                child: Padding(
                  padding: const EdgeInsets.all(48),
                  child: Center(
                    child: Column(
                      children: [
                        Icon(
                          Icons.assessment_outlined,
                          size: 64,
                          color: cs.outline.withValues(alpha: 0.3),
                        ),
                        const SizedBox(height: 16),
                        Text(
                          '点击上方按钮提交报告任务',
                          style: TextStyle(fontSize: 15, color: cs.outline),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
          ],
        ),
      ),
    );
  }

  Future<void> _submitReport() async {
    setState(() {
      _submitting = true;
      _error = null;
      _successMessage = null;
    });

    try {
      await widget.api.submitReport();
      if (mounted) {
        setState(() {
          _successMessage = '图片和视频报告任务已加入队列，请在任务页面查看进度。';
          _submitting = false;
        });
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _error = '$e';
          _submitting = false;
        });
      }
    }
  }
}
