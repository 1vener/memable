// settings_screen.dart：设置页面
// 代码注释使用中文
import 'package:flutter/material.dart';
import '../main.dart';
import '../services/api_service.dart';

class SettingsScreen extends StatefulWidget {
  final ApiService api;
  const SettingsScreen({super.key, required this.api});

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  late TextEditingController _apiUrlCtrl;
  bool _testLoading = false;
  String? _testResult;

  @override
  void initState() {
    super.initState();
    _apiUrlCtrl = TextEditingController(text: widget.api.baseUrl);
  }

  @override
  void dispose() {
    _apiUrlCtrl.dispose();
    super.dispose();
  }

  Future<void> _testConnection() async {
    setState(() {
      _testLoading = true;
      _testResult = null;
    });
    try {
      final api = ApiService(baseUrl: _apiUrlCtrl.text.trim());
      await api.health();
      if (mounted) setState(() => _testResult = 'ok');
    } catch (e) {
      if (mounted) setState(() => _testResult = 'error: $e');
    } finally {
      if (mounted) setState(() => _testLoading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;

    return Padding(
      padding: const EdgeInsets.all(32),
      child: SingleChildScrollView(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // ========== 外观 ==========
            _SectionTitle(title: '外观', icon: Icons.palette_outlined, cs: cs),
            const SizedBox(height: 16),
            Card(
              child: Padding(
                padding: const EdgeInsets.all(20),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text('主题', style: TextStyle(fontSize: 14, fontWeight: FontWeight.w500, color: cs.onSurface)),
                    const SizedBox(height: 12),
                    _ThemeOption(
                      label: '亮色',
                      icon: Icons.light_mode,
                      mode: ThemeMode.light,
                      groupValue: themeNotifier.mode,
                      onChanged: (v) => themeNotifier.setMode(v),
                    ),
                    const SizedBox(height: 8),
                    _ThemeOption(
                      label: '暗色',
                      icon: Icons.dark_mode,
                      mode: ThemeMode.dark,
                      groupValue: themeNotifier.mode,
                      onChanged: (v) => themeNotifier.setMode(v),
                    ),
                    const SizedBox(height: 8),
                    _ThemeOption(
                      label: '跟随系统',
                      icon: Icons.brightness_auto,
                      mode: ThemeMode.system,
                      groupValue: themeNotifier.mode,
                      onChanged: (v) => themeNotifier.setMode(v),
                    ),
                  ],
                ),
              ),
            ),
            const SizedBox(height: 32),

            // ========== 连接 ==========
            _SectionTitle(title: '连接', icon: Icons.wifi_outlined, cs: cs),
            const SizedBox(height: 16),
            Card(
              child: Padding(
                padding: const EdgeInsets.all(20),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text('API 地址', style: TextStyle(fontSize: 14, fontWeight: FontWeight.w500, color: cs.onSurface)),
                    const SizedBox(height: 12),
                    Row(
                      children: [
                        Expanded(
                          child: TextField(
                            controller: _apiUrlCtrl,
                            style: const TextStyle(fontSize: 13),
                            decoration: const InputDecoration(
                              hintText: 'http://localhost:8080',
                            ),
                          ),
                        ),
                        const SizedBox(width: 12),
                        FilledButton.icon(
                          onPressed: _testLoading ? null : _testConnection,
                          icon: _testLoading
                              ? const SizedBox(
                                  width: 16, height: 16,
                                  child: CircularProgressIndicator(strokeWidth: 2),
                                )
                              : const Icon(Icons.wifi_find, size: 18),
                          label: Text(_testLoading ? '测试中...' : '测试连接'),
                        ),
                      ],
                    ),
                    if (_testResult != null) ...[
                      const SizedBox(height: 12),
                      Container(
                        padding: const EdgeInsets.all(12),
                        decoration: BoxDecoration(
                          color: _testResult == 'ok'
                              ? const Color(0xFF22C55E).withValues(alpha: 0.1)
                              : const Color(0xFFEF4444).withValues(alpha: 0.1),
                          borderRadius: BorderRadius.circular(8),
                        ),
                        child: Row(
                          children: [
                            Icon(
                              _testResult == 'ok' ? Icons.check_circle : Icons.error,
                              size: 18,
                              color: _testResult == 'ok'
                                  ? const Color(0xFF22C55E)
                                  : const Color(0xFFEF4444),
                            ),
                            const SizedBox(width: 8),
                            Expanded(
                              child: Text(
                                _testResult == 'ok' ? '连接成功' : _testResult!,
                                style: TextStyle(
                                  fontSize: 13,
                                  color: _testResult == 'ok'
                                      ? const Color(0xFF22C55E)
                                      : const Color(0xFFEF4444),
                                ),
                              ),
                            ),
                          ],
                        ),
                      ),
                    ],
                  ],
                ),
              ),
            ),
            const SizedBox(height: 32),

            // ========== 关于 ==========
            _SectionTitle(title: '关于', icon: Icons.info_outline, cs: cs),
            const SizedBox(height: 16),
            Card(
              child: Padding(
                padding: const EdgeInsets.all(20),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    _AboutRow(label: '版本', value: 'v1.0.0'),
                    const SizedBox(height: 8),
                    _AboutRow(label: '技术栈', value: 'Flutter + Go + SQLite'),
                    const SizedBox(height: 8),
                    _AboutRow(label: '功能', value: '本地媒体重复检测与相似度搜索'),
                  ],
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _SectionTitle extends StatelessWidget {
  final String title;
  final IconData icon;
  final ColorScheme cs;

  const _SectionTitle({required this.title, required this.icon, required this.cs});

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        Icon(icon, size: 20, color: cs.primary),
        const SizedBox(width: 8),
        Text(
          title,
          style: TextStyle(
            fontSize: 16,
            fontWeight: FontWeight.w600,
            color: cs.onSurface,
          ),
        ),
      ],
    );
  }
}

class _ThemeOption extends StatelessWidget {
  final String label;
  final IconData icon;
  final ThemeMode mode;
  final ThemeMode groupValue;
  final ValueChanged<ThemeMode> onChanged;

  const _ThemeOption({
    required this.label,
    required this.icon,
    required this.mode,
    required this.groupValue,
    required this.onChanged,
  });

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final selected = mode == groupValue;
    return Material(
      color: Colors.transparent,
      borderRadius: BorderRadius.circular(8),
      child: InkWell(
        borderRadius: BorderRadius.circular(8),
        onTap: () => onChanged(mode),
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
          decoration: BoxDecoration(
            color: selected ? cs.primary.withValues(alpha: 0.1) : Colors.transparent,
            borderRadius: BorderRadius.circular(8),
            border: selected ? Border.all(color: cs.primary.withValues(alpha: 0.3)) : null,
          ),
          child: Row(
            children: [
              Icon(icon, size: 18, color: selected ? cs.primary : cs.onSurfaceVariant),
              const SizedBox(width: 10),
              Text(label, style: TextStyle(fontSize: 14, color: cs.onSurface)),
              const Spacer(),
              if (selected) Icon(Icons.check, size: 18, color: cs.primary),
            ],
          ),
        ),
      ),
    );
  }
}

class _AboutRow extends StatelessWidget {
  final String label;
  final String value;
  const _AboutRow({required this.label, required this.value});

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Row(
      children: [
        Text(label, style: TextStyle(fontSize: 13, color: cs.outline)),
        const SizedBox(width: 12),
        Text(value, style: TextStyle(fontSize: 13, color: cs.onSurface)),
      ],
    );
  }
}
