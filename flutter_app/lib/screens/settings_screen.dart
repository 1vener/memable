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
  late TextEditingController _hexCtrl;
  bool _testLoading = false;
  String? _testResult;
  bool _pathsLoading = false;
  bool _pathsLoaded = false;
  String? _pathsError;
  String? _imageDir;
  String? _videoDir;
  String _logFile = '';

  @override
  void initState() {
    super.initState();
    _apiUrlCtrl = TextEditingController(text: widget.api.baseUrl);
    _hexCtrl = TextEditingController();
    _syncHex();
    _loadPaths();
  }

  @override
  void dispose() {
    _apiUrlCtrl.dispose();
    _hexCtrl.dispose();
    super.dispose();
  }

  /// 预设主题色板
  static const _presetColors = [
    Color(0xFF2563EB), // 蓝
    Color(0xFF4F46E5), // 靛蓝
    Color(0xFF7C3AED), // 紫
    Color(0xFF0D9488), // 青
    Color(0xFF16A34A), // 绿
    Color(0xFFD97706), // 琥珀
    Color(0xFFEA580C), // 橙
    Color(0xFFDC2626), // 红
    Color(0xFFDB2777), // 粉
    Color(0xFF475569), // 石板灰
  ];

  void _syncHex() {
    final hex = themeNotifier.seedColor
        .toARGB32()
        .toRadixString(16)
        .substring(2)
        .toUpperCase()
        .padLeft(6, '0');
    _hexCtrl.text = '#$hex';
  }

  Color? _parseHex(String text) {
    final s = text.trim().replaceFirst('#', '');
    if (s.length != 6 && s.length != 8) return null;
    final v = int.tryParse(s, radix: 16);
    if (v == null) return null;
    return Color(0xFF000000 | v);
  }

  void _applyHexColor() {
    final color = _parseHex(_hexCtrl.text);
    if (color == null) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('颜色格式无效，请输入 #RRGGBB')),
        );
      }
      return;
    }
    themeNotifier.setSeedColor(color);
    if (mounted) _syncHex();
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

  /// 获取服务端缩略图/日志保存位置。
  Future<void> _loadPaths() async {
    setState(() {
      _pathsLoading = true;
      _pathsError = null;
    });
    try {
      final s = await widget.api.fetchSettings();
      if (mounted) {
        setState(() {
          _imageDir = s['thumbnail_image_dir'] as String?;
          _videoDir = s['thumbnail_video_dir'] as String?;
          _logFile = (s['log_file'] as String?) ?? '';
          _pathsLoaded = true;
        });
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _pathsError = '$e';
          _pathsLoaded = false;
        });
      }
    } finally {
      if (mounted) setState(() => _pathsLoading = false);
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

            // ========== 主题颜色 ==========
            Card(
              child: Padding(
                padding: const EdgeInsets.all(20),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      '主题颜色',
                      style: TextStyle(
                        fontSize: 14,
                        fontWeight: FontWeight.w500,
                        color: cs.onSurface,
                      ),
                    ),
                    const SizedBox(height: 12),
                    Text(
                      '预设色板',
                      style: TextStyle(fontSize: 12, color: cs.outline),
                    ),
                    const SizedBox(height: 8),
                    Wrap(
                      spacing: 10,
                      runSpacing: 10,
                      children: [
                        for (final c in _presetColors)
                          Tooltip(
                            message: '#${c.toARGB32().toRadixString(16).substring(2).toUpperCase()}',
                            child: InkWell(
                              borderRadius: BorderRadius.circular(20),
                              onTap: () {
                                themeNotifier.setSeedColor(c);
                                _syncHex();
                              },
                              child: Container(
                                width: 34,
                                height: 34,
                                decoration: BoxDecoration(
                                  color: c,
                                  shape: BoxShape.circle,
                                  border: themeNotifier.seedColor.toARGB32() ==
                                          c.toARGB32()
                                      ? Border.all(
                                          color: cs.onSurface,
                                          width: 2,
                                        )
                                      : null,
                                ),
                                child: themeNotifier.seedColor.toARGB32() ==
                                        c.toARGB32()
                                    ? const Icon(
                                        Icons.check,
                                        size: 18,
                                        color: Colors.white,
                                      )
                                    : null,
                              ),
                            ),
                          ),
                      ],
                    ),
                    const SizedBox(height: 16),
                    Text(
                      '自定义取色（HEX / RGB）',
                      style: TextStyle(fontSize: 12, color: cs.outline),
                    ),
                    const SizedBox(height: 8),
                    Row(
                      children: [
                        Expanded(
                          child: TextField(
                            controller: _hexCtrl,
                            style: const TextStyle(fontSize: 13),
                            decoration: const InputDecoration(
                              hintText: '#2563EB',
                              prefixIcon: Icon(Icons.colorize, size: 18),
                            ),
                            onSubmitted: (_) => _applyHexColor(),
                          ),
                        ),
                        const SizedBox(width: 12),
                        FilledButton(
                          onPressed: _applyHexColor,
                          child: const Text('应用'),
                        ),
                      ],
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

            // ========== 存储位置 ==========
            _SectionTitle(title: '存储位置', icon: Icons.folder_outlined, cs: cs),
            const SizedBox(height: 16),
            Card(
              child: Padding(
                padding: const EdgeInsets.all(20),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Text(
                          '服务端保存位置',
                          style: TextStyle(
                            fontSize: 14,
                            fontWeight: FontWeight.w500,
                            color: cs.onSurface,
                          ),
                        ),
                        const Spacer(),
                        IconButton(
                          icon: const Icon(Icons.refresh, size: 18),
                          tooltip: '刷新',
                          onPressed: _pathsLoading ? null : _loadPaths,
                        ),
                      ],
                    ),
                    const SizedBox(height: 8),
                    if (_pathsLoading)
                      const Padding(
                        padding: EdgeInsets.all(8),
                        child: Center(
                          child: SizedBox(
                            width: 18,
                            height: 18,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          ),
                        ),
                      ),
                    if (_pathsError != null) ...[
                      Text(
                        '获取失败: $_pathsError',
                        style: TextStyle(fontSize: 12, color: cs.error),
                      ),
                      const SizedBox(height: 8),
                    ],
                    if (_pathsLoaded) ...[
                      _PathRow(
                        icon: Icons.image_outlined,
                        label: '图片缩略图',
                        value: _imageDir ?? '',
                      ),
                      _PathRow(
                        icon: Icons.movie_outlined,
                        label: '视频封面',
                        value: _videoDir ?? '',
                      ),
                      _PathRow(
                        icon: Icons.notes,
                        label: '日志',
                        value: _logFile.isEmpty ? '控制台（标准输出）' : _logFile,
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
            const Card(
              child: Padding(
                padding: EdgeInsets.all(20),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    _AboutRow(label: '版本', value: 'v1.0.0'),
                    SizedBox(height: 8),
                    _AboutRow(label: '技术栈', value: 'Flutter + Go + SQLite'),
                    SizedBox(height: 8),
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
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SizedBox(
          width: 48,
          child: Text(label, style: TextStyle(fontSize: 13, color: cs.outline)),
        ),
        const SizedBox(width: 12),
        Expanded(
          child: Text(value, style: TextStyle(fontSize: 13, color: cs.onSurface), overflow: TextOverflow.ellipsis),
        ),
      ],
    );
  }
}

class _PathRow extends StatelessWidget {
  final IconData icon;
  final String label;
  final String value;
  const _PathRow({required this.icon, required this.label, required this.value});

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, size: 16, color: cs.outline),
          const SizedBox(width: 8),
          SizedBox(
            width: 90,
            child: Text(label, style: TextStyle(fontSize: 12, color: cs.outline)),
          ),
          const SizedBox(width: 8),
          Expanded(
            child: SelectableText(
              value,
              style: TextStyle(fontSize: 12, color: cs.onSurface),
            ),
          ),
        ],
      ),
    );
  }
}
