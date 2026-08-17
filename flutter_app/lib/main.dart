// main.dart：Flutter 应用入口，主题系统与全局配置
// 代码注释使用中文
import 'dart:ui' show AppExitResponse;

import 'package:flutter/foundation.dart' show kDebugMode;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:http/http.dart' as http;
import 'package:shared_preferences/shared_preferences.dart';
import 'desktop/backend_manager.dart'
    if (dart.library.js_interop) 'web/backend_manager_stub.dart';
import 'screens/home_screen.dart';
import 'services/api_service.dart';

/// 后端默认端口与探测范围（覆盖服务端自动避让上限 20 个端口）。
const kDefaultPort = 12358;
const kPortProbeCount = 20;

/// 全局主题管理器（模式 + 自定义主题色，本地持久化）
class ThemeNotifier extends ChangeNotifier {
  ThemeMode _mode = ThemeMode.light; // 默认浅色主题
  Color _seedColor = const Color(0xFF2563EB); // 默认强调色

  ThemeMode get mode => _mode;
  Color get seedColor => _seedColor;

  void setMode(ThemeMode mode) {
    _mode = mode;
    _persist();
    notifyListeners();
  }

  void toggle() {
    _mode = switch (_mode) {
      ThemeMode.light => ThemeMode.dark,
      ThemeMode.dark => ThemeMode.system,
      ThemeMode.system => ThemeMode.light,
    };
    _persist();
    notifyListeners();
  }

  /// 设置自定义主题色（浅色/深色模式统一生效）
  void setSeedColor(Color color) {
    _seedColor = color;
    _persist();
    notifyListeners();
  }

  /// 从本地存储恢复主题设置
  Future<void> load() async {
    try {
      final prefs = await SharedPreferences.getInstance();
      final modeStr = prefs.getString('ui.theme_mode');
      final colorInt = prefs.getInt('ui.accent_color');
      _mode = switch (modeStr) {
        'light' => ThemeMode.light,
        'dark' => ThemeMode.dark,
        'system' => ThemeMode.system,
        _ => ThemeMode.light,
      };
      if (colorInt != null) {
        _seedColor = Color(colorInt);
      }
      notifyListeners();
    } catch (_) {}
  }

  Future<void> _persist() async {
    try {
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString('ui.theme_mode', switch (_mode) {
        ThemeMode.light => 'light',
        ThemeMode.dark => 'dark',
        ThemeMode.system => 'system',
      });
      await prefs.setInt('ui.accent_color', _seedColor.toARGB32());
    } catch (_) {}
  }
}

/// 全局单例
final themeNotifier = ThemeNotifier();

/// 应用退出时清理后端进程的监听器（保持引用防止被 GC，生命周期与进程一致）。
// ignore: unused_element
AppLifecycleListener? _exitListener;

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  themeNotifier.load();
  // 窗口最小尺寸
  SystemChrome.setSystemUIOverlayStyle(
    const SystemUiOverlayStyle(statusBarColor: Colors.transparent),
  );
  // 官方退出钩子：桌面窗口关闭时先停止后端再退出（比 dispose 可靠）
  _exitListener = AppLifecycleListener(
    onExitRequested: () async {
      await BackendManager.stopBackend();
      return AppExitResponse.exit;
    },
  );
  runApp(const MemableApp());
}

/// 自动发现后端服务并返回 baseUrl：
/// 1. 快速探测默认端口；2. 并发探测避让范围（端口被占用时后端 +1 避让）；
/// 3. release 构建仍不通则拉起同目录 server.exe 并等待就绪；4. 全部失败回退默认端口。
Future<String> discoverBackend() async {
  if (await _healthOk(kDefaultPort)) {
    return 'http://localhost:$kDefaultPort';
  }
  final results = await Future.wait([
    for (var p = kDefaultPort + 1; p < kDefaultPort + kPortProbeCount; p++)
      _healthOk(p).then((ok) => ok ? p : null),
  ]);
  for (final p in results) {
    if (p != null) return 'http://localhost:$p';
  }
  // 开发模式（flutter run）不自动拉起后端，避免重复起服务
  if (!kDebugMode) {
    await BackendManager.startBackend();
    // 等待就绪（最多 5 秒，windowsgui server 启动通常 1 秒内就绪）
    for (var i = 0; i < 25; i++) {
      await Future.delayed(const Duration(milliseconds: 200));
      if (await _healthOk(kDefaultPort))
        return 'http://localhost:$kDefaultPort';
    }
  }
  return 'http://localhost:$kDefaultPort';
}

Future<bool> _healthOk(int port) async {
  try {
    final res = await http
        .get(Uri.parse('http://localhost:$port/api/health'))
        .timeout(const Duration(milliseconds: 300));
    return res.statusCode == 200;
  } catch (_) {
    return false;
  }
}

/// 后端进程管理：桌面端实现在 desktop/backend_manager.dart，web 端为
/// backend_manager_stub.dart 空实现（条件导入），此处不再定义。

class MemableApp extends StatefulWidget {
  const MemableApp({super.key});

  @override
  State<MemableApp> createState() => _MemableAppState();
}

class _MemableAppState extends State<MemableApp> {
  String? _baseUrl; // null = 发现中

  @override
  void initState() {
    super.initState();
    discoverBackend().then((url) {
      if (mounted) setState(() => _baseUrl = url);
    });
  }

  @override
  void dispose() {
    // 双保险：正常情况下退出走 AppLifecycleListener.onExitRequested
    BackendManager.stopBackend();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: themeNotifier,
      builder: (context, _) {
        return MaterialApp(
          title: 'memable',
          debugShowCheckedModeBanner: false,
          themeMode: themeNotifier.mode,
          theme: _lightTheme(themeNotifier.seedColor),
          darkTheme: _darkTheme(themeNotifier.seedColor),
          home:
              _baseUrl == null
                  ? const Center(
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        CircularProgressIndicator(),
                        SizedBox(height: 16),
                        Text('正在启动服务...', style: TextStyle(fontSize: 13)),
                      ],
                    ),
                  )
                  : HomeScreen(api: ApiService(baseUrl: _baseUrl!)),
        );
      },
    );
  }
}

/// 亮色主题 — Material 3 定制（中性色打底 + 可自定义强调色）
ThemeData _lightTheme(Color seed) {
  final generated = ColorScheme.fromSeed(
    seedColor: seed,
    brightness: Brightness.light,
  );
  // 保留可自定义强调色，同时将大面积中性色收敛为更柔和的暖白。
  final cs = generated.copyWith(
    surface: const Color(0xFFFFFDFC),
    surfaceContainerLow: const Color(0xFFFFFBF8),
    surfaceContainerHighest: const Color(0xFFF6F1EC),
    outlineVariant: const Color(0xFFE8E0D9),
  );
  return ThemeData(
    colorScheme: cs,
    useMaterial3: true,
    scaffoldBackgroundColor: const Color(0xFFFFFCFA),
    appBarTheme: AppBarTheme(
      backgroundColor: cs.surface,
      foregroundColor: cs.onSurface,
      elevation: 0,
      surfaceTintColor: Colors.transparent,
    ),
    cardTheme: CardTheme(
      color: cs.surfaceContainerLow,
      elevation: 0,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(14),
        side: BorderSide(color: cs.outlineVariant.withValues(alpha: .8)),
      ),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: const Color(0xFFFFFBF8),
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(14),
        borderSide: BorderSide(color: cs.outlineVariant),
      ),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(14),
        borderSide: BorderSide(color: cs.outlineVariant),
      ),
      focusedBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(14),
        borderSide: BorderSide(color: cs.primary, width: 1.4),
      ),
      contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
    ),
    elevatedButtonTheme: ElevatedButtonThemeData(
      style: ElevatedButton.styleFrom(
        elevation: 0,
        padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 14),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      ),
    ),
    dividerTheme: DividerThemeData(
      color: cs.outlineVariant,
      thickness: 1,
      space: 1,
    ),
    tooltipTheme: TooltipThemeData(
      decoration: BoxDecoration(
        color: cs.inverseSurface,
        borderRadius: BorderRadius.circular(6),
      ),
      textStyle: TextStyle(color: cs.onInverseSurface, fontSize: 12),
    ),
  );
}

/// 暗色主题
ThemeData _darkTheme(Color seed) {
  final generated = ColorScheme.fromSeed(
    seedColor: seed,
    brightness: Brightness.dark,
  );
  // 不使用纯黑：石墨色阶在长时间浏览媒体时更柔和，也能保留卡片层级。
  final cs = generated.copyWith(
    surface: const Color(0xFF191816),
    surfaceContainerLow: const Color(0xFF201E1B),
    surfaceContainerHighest: const Color(0xFF2A2724),
    onSurface: const Color(0xFFECE6E0),
    onSurfaceVariant: const Color(0xFFCFC6BE),
    outlineVariant: const Color(0xFF46413C),
  );
  return ThemeData(
    colorScheme: cs,
    useMaterial3: true,
    scaffoldBackgroundColor: const Color(0xFF171614),
    appBarTheme: AppBarTheme(
      backgroundColor: cs.surface,
      foregroundColor: cs.onSurface,
      elevation: 0,
      surfaceTintColor: Colors.transparent,
    ),
    cardTheme: CardTheme(
      color: const Color(0xFF201E1B),
      elevation: 0,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(14),
        side: BorderSide(color: cs.outlineVariant.withValues(alpha: .85)),
      ),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: const Color(0xFF252320),
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(14),
        borderSide: BorderSide(color: cs.outlineVariant),
      ),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(14),
        borderSide: BorderSide(color: cs.outlineVariant),
      ),
      focusedBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(14),
        borderSide: BorderSide(color: cs.primary, width: 1.4),
      ),
      contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
    ),
    elevatedButtonTheme: ElevatedButtonThemeData(
      style: ElevatedButton.styleFrom(
        elevation: 0,
        padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 14),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      ),
    ),
    dividerTheme: DividerThemeData(
      color: cs.outlineVariant,
      thickness: 1,
      space: 1,
    ),
    tooltipTheme: TooltipThemeData(
      decoration: BoxDecoration(
        color: cs.inverseSurface,
        borderRadius: BorderRadius.circular(6),
      ),
      textStyle: TextStyle(color: cs.onInverseSurface, fontSize: 12),
    ),
  );
}
