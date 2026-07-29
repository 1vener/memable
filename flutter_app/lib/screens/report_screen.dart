// report_screen.dart：报告页面（生成重复检测报告 + 浏览器打开）
// 代码注释使用中文
import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';
import '../models/models.dart';
import '../services/api_service.dart';

class ReportScreen extends StatefulWidget {
  final ApiService api;
  const ReportScreen({super.key, required this.api});

  @override
  State<ReportScreen> createState() => _ReportScreenState();
}

class _ReportScreenState extends State<ReportScreen> {
  bool _generating = false;
  DuplicateReport? _report;
  String? _error;
  String? _reportPath;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;

    return Padding(
      padding: const EdgeInsets.all(32),
      child: SingleChildScrollView(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // ========== 生成报告 ==========
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
                      '对所有已扫描的媒体进行重复/相似度检测，生成 HTML 报告。',
                      style: TextStyle(fontSize: 14, color: cs.outline),
                    ),
                    const SizedBox(height: 24),
                    // 生成按钮
                    Row(
                      children: [
                        FilledButton.icon(
                          onPressed: _generating ? null : _generateReport,
                          icon: _generating
                              ? const SizedBox(
                                  width: 18, height: 18,
                                  child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white),
                                )
                              : const Icon(Icons.play_arrow, size: 20),
                          label: Text(_generating ? '生成中...' : '生成报告'),
                        ),
                        if (_generating) ...[
                          const SizedBox(width: 12),
                          OutlinedButton(
                            onPressed: () => setState(() => _generating = false),
                            child: const Text('取消'),
                          ),
                        ],
                      ],
                    ),
                  ],
                ),
              ),
            ),
            const SizedBox(height: 16),

            // ========== 结果展示 ==========
            if (_error != null)
              Card(
                child: Padding(
                  padding: const EdgeInsets.all(24),
                  child: Column(
                    children: [
                      Icon(Icons.error_outline, size: 48, color: cs.error),
                      const SizedBox(height: 12),
                      Text('报告生成失败', style: TextStyle(fontSize: 15, color: cs.onSurface)),
                      const SizedBox(height: 6),
                      Text(_error!, style: TextStyle(fontSize: 13, color: cs.outline), overflow: TextOverflow.ellipsis, maxLines: 3),
                    ],
                  ),
                ),
              ),

            if (_report != null)
              Card(
                child: Padding(
                  padding: const EdgeInsets.all(24),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Row(
                        children: [
                          const Icon(Icons.check_circle, size: 24, color: Color(0xFF22C55E)),
                          const SizedBox(width: 10),
                          Text(
                            '报告已生成',
                            style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600, color: cs.onSurface),
                          ),
                        ],
                      ),
                      const SizedBox(height: 20),
                      // 统计信息
                      Row(
                        children: [
                          _StatCard(
                            label: '重复组数',
                            value: '${_report!.groupCount}',
                            icon: Icons.group_work,
                            color: cs.primary,
                          ),
                        ],
                      ),
                      const SizedBox(height: 20),
                      // 报告路径
                      Container(
                        padding: const EdgeInsets.all(14),
                        decoration: BoxDecoration(
                          color: cs.surfaceContainerHighest.withValues(alpha: 0.4),
                          borderRadius: BorderRadius.circular(8),
                        ),
                        child: Row(
                          children: [
                            Icon(Icons.insert_drive_file, size: 16, color: cs.outline),
                            const SizedBox(width: 8),
                            Expanded(
                              child: SelectableText(
                                _report!.path,
                                style: TextStyle(fontSize: 13, color: cs.onSurface),
                              ),
                            ),
                          ],
                        ),
                      ),
                      const SizedBox(height: 16),
                      // 操作按钮
                      Row(
                        children: [
                          FilledButton.tonalIcon(
                            onPressed: _openInBrowser,
                            icon: const Icon(Icons.open_in_browser, size: 18),
                            label: const Text('在浏览器中打开'),
                          ),
                          const SizedBox(width: 10),
                          OutlinedButton.icon(
                            onPressed: _copyPath,
                            icon: const Icon(Icons.copy, size: 18),
                            label: const Text('复制路径'),
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
              ),

            // ========== 空状态 ==========
            if (_report == null && _error == null && !_generating)
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
                          '点击上方按钮生成重复检测报告',
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

  Future<void> _generateReport() async {
    setState(() {
      _generating = true;
      _error = null;
      _report = null;
    });

    try {
      final result = await widget.api.generateReport();
      if (mounted) {
        setState(() {
          _report = result;
          _reportPath = result.path;
          _generating = false;
        });
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _error = '$e';
          _generating = false;
        });
      }
    }
  }

  Future<void> _openInBrowser() async {
    if (_reportPath == null) return;
    final uri = Uri.file(_reportPath!);
    if (await canLaunchUrl(uri)) {
      await launchUrl(uri);
    } else if (mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('无法打开浏览器'), backgroundColor: Color(0xFFEF4444)),
      );
    }
  }

  void _copyPath() {
    if (_reportPath == null) return;
    // 桌面端不使用 Clipboard，直接复制
    // ignore: deprecated_member_use
    // ignore: unnecessary_import
    // 实际项目中应使用 Clipboard.setData
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text('路径: $_reportPath')),
    );
  }
}

/// 统计卡片
class _StatCard extends StatelessWidget {
  final String label;
  final String value;
  final IconData icon;
  final Color color;

  const _StatCard({
    required this.label,
    required this.value,
    required this.icon,
    required this.color,
  });

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Expanded(
      child: Container(
        padding: const EdgeInsets.all(16),
        decoration: BoxDecoration(
          color: color.withValues(alpha: 0.06),
          borderRadius: BorderRadius.circular(10),
          border: Border.all(color: color.withValues(alpha: 0.15)),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Icon(icon, size: 20, color: color),
            const SizedBox(height: 10),
            Text(
              value,
              style: TextStyle(fontSize: 22, fontWeight: FontWeight.w700, color: cs.onSurface),
              overflow: TextOverflow.ellipsis,
            ),
            const SizedBox(height: 2),
            Text(label, style: TextStyle(fontSize: 12, color: cs.outline)),
          ],
        ),
      ),
    );
  }
}
