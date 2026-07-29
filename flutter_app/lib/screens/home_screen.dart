// home_screen.dart：主框架，固定侧边栏 + 顶部工具栏 + 内容区 + 底部状态栏
// 代码注释使用中文
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../services/api_service.dart';
import '../main.dart';
import '../widgets/status_bar.dart';
import '../widgets/context_menu.dart';
import 'library_screen.dart';
import 'scan_screen.dart';
import 'search_screen.dart';
import 'report_screen.dart';
import 'settings_screen.dart';

/// 侧边栏导航项
class _NavDestination {
  final IconData icon;
  final IconData selectedIcon;
  final String label;
  final String tooltip;
  final String shortcut;

  const _NavDestination({
    required this.icon,
    required this.selectedIcon,
    required this.label,
    required this.tooltip,
    required this.shortcut,
  });
}

const _destinations = [
  _NavDestination(
    icon: Icons.folder_outlined,
    selectedIcon: Icons.folder,
    label: '收藏库',
    tooltip: '管理媒体收藏库',
    shortcut: 'Ctrl+1',
  ),
  _NavDestination(
    icon: Icons.play_circle_outline,
    selectedIcon: Icons.play_circle,
    label: '扫描',
    tooltip: '扫描媒体文件',
    shortcut: 'Ctrl+2',
  ),
  _NavDestination(
    icon: Icons.search,
    selectedIcon: Icons.search,
    label: '搜索',
    tooltip: '文字搜索 / 以图搜图',
    shortcut: 'Ctrl+3',
  ),
  _NavDestination(
    icon: Icons.assessment_outlined,
    selectedIcon: Icons.assessment,
    label: '报告',
    tooltip: '重复检测报告',
    shortcut: 'Ctrl+4',
  ),
];

class HomeScreen extends StatefulWidget {
  final ApiService api;
  const HomeScreen({super.key, required this.api});

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  int _selectedIndex = 0;
  String? _currentLibrary;
  String? _scanProgress;
  String _apiStatus = 'unknown';

  ApiService get api => widget.api;

  @override
  void initState() {
    super.initState();
    _checkApi();
  }

  Future<void> _checkApi() async {
    try {
      await api.health();
      if (mounted) setState(() => _apiStatus = 'ok');
    } catch (_) {
      if (mounted) setState(() => _apiStatus = 'error');
    }
  }

  void _onSelectPage(int index) {
    setState(() => _selectedIndex = index);
  }

  void _onLibrarySelected(String name) {
    setState(() {
      _currentLibrary = name;
      _selectedIndex = 1; // 切换到扫描页
    });
  }

  /// 当前页面标题
  String get _currentPageTitle {
    if (_selectedIndex < _destinations.length) {
      return _destinations[_selectedIndex].label;
    }
    return '设置';
  }

  /// 当前页面内容
  Widget _buildContent() {
    return switch (_selectedIndex) {
      0 => LibraryScreen(
        api: api,
        onLibrarySelected: _onLibrarySelected,
      ),
      1 => ScanScreen(api: api, currentLibrary: _currentLibrary),
      2 => SearchScreen(api: api),
      3 => ReportScreen(api: api),
      4 => SettingsScreen(api: api),
      _ => const SizedBox.shrink(),
    };
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final isDark = Theme.of(context).brightness == Brightness.dark;

    return Shortcuts(
      shortcuts: const {
        SingleActivator(LogicalKeyboardKey.digit1, control: true):
            _SelectPageIntent(0),
        SingleActivator(LogicalKeyboardKey.digit2, control: true):
            _SelectPageIntent(1),
        SingleActivator(LogicalKeyboardKey.digit3, control: true):
            _SelectPageIntent(2),
        SingleActivator(LogicalKeyboardKey.digit4, control: true):
            _SelectPageIntent(3),
        SingleActivator(LogicalKeyboardKey.digit5, control: true):
            _SelectPageIntent(4),
        SingleActivator(LogicalKeyboardKey.f5): _RefreshIntent(),
      },
      child: Actions(
        actions: {
          _SelectPageIntent: CallbackAction<_SelectPageIntent>(
            onInvoke: (intent) {
              _onSelectPage(intent.index);
              return null;
            },
          ),
          _RefreshIntent: CallbackAction<_RefreshIntent>(
            onInvoke: (_) {
              _checkApi();
              return null;
            },
          ),
        },
        child: Focus(
          autofocus: true,
          child: Scaffold(
            body: Row(
              children: [
                // ========== 固定侧边栏 ==========
                _buildSidebar(cs, isDark),
                // ========== 右侧区域 ==========
                Expanded(
                  child: Column(
                    children: [
                      // 顶部工具栏
                      _buildToolbar(cs),
                      // 内容区
                      Expanded(child: _buildContent()),
                      // 底部状态栏
                      StatusBar(
                        apiStatus: _apiStatus,
                        currentLibrary: _currentLibrary,
                        scanProgress: _scanProgress,
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  /// 构建固定侧边栏
  Widget _buildSidebar(ColorScheme cs, bool isDark) {
    return Container(
      width: 220,
      decoration: BoxDecoration(
        color: isDark ? const Color(0xFF1E293B) : Colors.white,
        border: Border(
          right: BorderSide(color: cs.outlineVariant, width: 0.5),
        ),
      ),
      child: Column(
        children: [
          // 应用标题
          Container(
            height: 56,
            padding: const EdgeInsets.symmetric(horizontal: 20),
            alignment: Alignment.centerLeft,
            child: Row(
              children: [
                Icon(Icons.grid_view_rounded, size: 22, color: cs.primary),
                const SizedBox(width: 10),
                Text(
                  'memable',
                  style: TextStyle(
                    fontSize: 17,
                    fontWeight: FontWeight.w700,
                    color: cs.onSurface,
                    letterSpacing: -0.3,
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(height: 4),
          // 导航项
          for (int i = 0; i < _destinations.length; i++)
            _buildNavItem(i, cs, isDark),
          const Spacer(),
          // 底部：主题切换 + 设置
          Padding(
            padding: const EdgeInsets.all(12),
            child: Column(
              children: [
                Divider(height: 1, color: cs.outlineVariant),
                const SizedBox(height: 8),
                // 主题切换
                _buildThemeToggle(cs, isDark),
                const SizedBox(height: 4),
                // 设置按钮
                _buildBottomItem(
                  icon: Icons.settings_outlined,
                  label: '设置',
                  selected: _selectedIndex == 4,
                  cs: cs,
                  onTap: () => _onSelectPage(4),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  /// 构建侧边栏导航项
  Widget _buildNavItem(int index, ColorScheme cs, bool isDark) {
    final dest = _destinations[index];
    final selected = _selectedIndex == index;

    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 2),
      child: Tooltip(
        message: '${dest.tooltip} (${dest.shortcut})',
        child: Material(
          color: Colors.transparent,
          borderRadius: BorderRadius.circular(8),
          child: InkWell(
            borderRadius: BorderRadius.circular(8),
            onTap: () => _onSelectPage(index),
            onSecondaryTapDown: (details) {
              showContextMenu(
                context: context,
                position: details.globalPosition,
                items: [
                  ContextMenuItem(
                    icon: Icons.open_in_new,
                    label: '打开${dest.label}',
                    shortcut: dest.shortcut,
                    onTap: () => _onSelectPage(index),
                  ),
                  const ContextMenuItem.divider(),
                  ContextMenuItem(
                    icon: Icons.refresh,
                    label: '刷新',
                    shortcut: 'Ctrl+R',
                    onTap: _checkApi,
                  ),
                ],
              );
            },
            child: AnimatedContainer(
              duration: const Duration(milliseconds: 150),
              padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
              decoration: BoxDecoration(
                color: selected
                    ? cs.primary.withValues(alpha: isDark ? 0.15 : 0.1)
                    : Colors.transparent,
                borderRadius: BorderRadius.circular(8),
              ),
              child: Row(
                children: [
                  Icon(
                    selected ? dest.selectedIcon : dest.icon,
                    size: 20,
                    color: selected ? cs.primary : cs.onSurfaceVariant,
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Text(
                      dest.label,
                      style: TextStyle(
                        fontSize: 14,
                        fontWeight: selected ? FontWeight.w600 : FontWeight.w400,
                        color: selected ? cs.primary : cs.onSurfaceVariant,
                      ),
                    ),
                  ),
                  Text(
                    dest.shortcut.replaceAll('Ctrl+', ''),
                    style: TextStyle(
                      fontSize: 11,
                      color: selected
                          ? cs.primary.withValues(alpha: 0.6)
                          : cs.outline,
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }

  /// 主题切换按钮
  Widget _buildThemeToggle(ColorScheme cs, bool isDark) {
    final mode = themeNotifier.mode;
    final icon = switch (mode) {
      ThemeMode.light => Icons.light_mode_outlined,
      ThemeMode.dark => Icons.dark_mode_outlined,
      ThemeMode.system => Icons.brightness_auto_outlined,
    };
    final tooltip = switch (mode) {
      ThemeMode.light => '当前：亮色（点击切换暗色）',
      ThemeMode.dark => '当前：暗色（点击跟随系统）',
      ThemeMode.system => '当前：跟随系统（点击切换亮色）',
    };

    return Tooltip(
      message: tooltip,
      child: Material(
        color: Colors.transparent,
        borderRadius: BorderRadius.circular(8),
        child: InkWell(
          borderRadius: BorderRadius.circular(8),
          onTap: () => themeNotifier.toggle(),
          child: Container(
            padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
            child: Row(
              children: [
                Icon(icon, size: 20, color: cs.onSurfaceVariant),
                const SizedBox(width: 12),
                Text(
                  switch (mode) {
                    ThemeMode.light => '亮色模式',
                    ThemeMode.dark => '暗色模式',
                    ThemeMode.system => '跟随系统',
                  },
                  style: TextStyle(fontSize: 14, color: cs.onSurfaceVariant),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  /// 底部导航项
  Widget _buildBottomItem({
    required IconData icon,
    required String label,
    required bool selected,
    required ColorScheme cs,
    required VoidCallback onTap,
  }) {
    return Material(
      color: Colors.transparent,
      borderRadius: BorderRadius.circular(8),
      child: InkWell(
        borderRadius: BorderRadius.circular(8),
        onTap: onTap,
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
          child: Row(
            children: [
              Icon(icon, size: 20, color: cs.onSurfaceVariant),
              const SizedBox(width: 12),
              Text(
                label,
                style: TextStyle(fontSize: 14, color: cs.onSurfaceVariant),
              ),
            ],
          ),
        ),
      ),
    );
  }

  /// 顶部工具栏
  Widget _buildToolbar(ColorScheme cs) {
    return Container(
      height: 56,
      padding: const EdgeInsets.symmetric(horizontal: 24),
      decoration: BoxDecoration(
        color: Theme.of(context).scaffoldBackgroundColor,
        border: Border(bottom: BorderSide(color: cs.outlineVariant, width: 0.5)),
      ),
      child: Row(
        children: [
          // 页面标题
          Expanded(
            child: Text(
              _currentPageTitle,
              style: TextStyle(
                fontSize: 18,
                fontWeight: FontWeight.w600,
                color: cs.onSurface,
              ),
              overflow: TextOverflow.ellipsis,
            ),
          ),
          const SizedBox(width: 20),
          // 刷新按钮
          IconButton(
            icon: const Icon(Icons.refresh, size: 20),
            tooltip: '刷新 (F5)',
            onPressed: _checkApi,
          ),
          const Spacer(),
          // 主题切换快捷按钮
          IconButton(
            icon: Icon(
              Theme.of(context).brightness == Brightness.dark
                  ? Icons.light_mode_outlined
                  : Icons.dark_mode_outlined,
              size: 20,
            ),
            tooltip: '切换主题',
            onPressed: () => themeNotifier.toggle(),
          ),
        ],
      ),
    );
  }
}

/// 页面切换意图
class _SelectPageIntent extends Intent {
  final int index;
  const _SelectPageIntent(this.index);
}

/// 刷新意图
class _RefreshIntent extends Intent {
  const _RefreshIntent();
}
