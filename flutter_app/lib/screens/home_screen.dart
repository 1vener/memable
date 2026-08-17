// home_screen.dart：主框架，可收起侧边栏 + 顶部工具栏 + 内容区 + 底部状态栏
// 代码注释使用中文
import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:shared_preferences/shared_preferences.dart';
import '../services/api_service.dart';
import '../main.dart';
import '../models/models.dart';
import '../widgets/status_bar.dart';
import '../widgets/context_menu.dart';
import 'library_screen.dart';
import 'scan_screen.dart';
import 'task_screen.dart';
import 'search_screen.dart';
import 'report_screen.dart';
import 'dir_compare_screen.dart';
import 'settings_screen.dart';
import 'tool_screen.dart';
import 'dashboard_screen.dart';
import '../services/display_preferences.dart';

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
    icon: Icons.home_outlined,
    selectedIcon: Icons.home,
    label: '首页',
    tooltip: '浏览全部正式媒体',
    shortcut: 'Ctrl+1',
  ),
  _NavDestination(
    icon: Icons.folder_outlined,
    selectedIcon: Icons.folder,
    label: '收藏库',
    tooltip: '管理媒体收藏库',
    shortcut: 'Ctrl+2',
  ),
  _NavDestination(
    icon: Icons.play_circle_outline,
    selectedIcon: Icons.play_circle,
    label: '扫描',
    tooltip: '扫描媒体文件',
    shortcut: 'Ctrl+3',
  ),
  _NavDestination(
    icon: Icons.queue_outlined,
    selectedIcon: Icons.queue,
    label: '任务',
    tooltip: '任务队列与进度',
    shortcut: 'Ctrl+4',
  ),
  _NavDestination(
    icon: Icons.search,
    selectedIcon: Icons.search,
    label: '搜索',
    tooltip: '文字搜索 / 以图搜图',
    shortcut: 'Ctrl+5',
  ),
  _NavDestination(
    icon: Icons.assessment_outlined,
    selectedIcon: Icons.assessment,
    label: '报告',
    tooltip: '重复检测报告',
    shortcut: 'Ctrl+6',
  ),
  _NavDestination(
    icon: Icons.folder_copy_outlined,
    selectedIcon: Icons.folder_copy,
    label: '目录对比',
    tooltip: '所选目录与存量数据对比',
    shortcut: 'Ctrl+7',
  ),
  _NavDestination(
    icon: Icons.build_outlined,
    selectedIcon: Icons.build,
    label: '工具',
    tooltip: '文件统计等实用工具',
    shortcut: 'Ctrl+8',
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
  String _dashboardTab = 'library';
  String? _currentLibrary;
  String? _scanProgress;
  String _apiStatus = 'unknown';
  List<BackgroundTask> _runningTasks = [];
  Timer? _taskTimer;
  static const double _sidebarExpandedWidth = 148;
  static const double _sidebarCollapsedWidth = 64;

  bool _userCollapsed = false;
  final bool _autoCollapseThreshold = true;

  // 收藏库页文件搜索状态（工具栏搜索框 + 详情页结果面板）
  final TextEditingController _libSearchCtrl = TextEditingController();
  String _libSearchQuery = '';
  List<LibrarySearchResult> _libSearchResults = [];
  bool _libSearchLoading = false;
  Timer? _libSearchDebounce;

  ApiService get api => widget.api;

  @override
  void initState() {
    super.initState();
    _checkApi();
    _loadSidebarPref();
    _startTaskPolling();
  }

  @override
  void dispose() {
    _taskTimer?.cancel();
    _libSearchDebounce?.cancel();
    _libSearchCtrl.dispose();
    super.dispose();
  }

  /// 每 2 秒轮询一次后台任务，供底部状态栏展示运行中任务进度。
  void _startTaskPolling() {
    _loadRunningTasks();
    _taskTimer = Timer.periodic(const Duration(seconds: 2), (_) {
      if (mounted) _loadRunningTasks();
    });
  }

  Future<void> _loadRunningTasks() async {
    try {
      final tasks = await api.getTasks();
      if (!mounted) return;
      setState(() {
        _runningTasks = tasks.where((t) => t.isRunning).toList();
      });
    } catch (_) {
      // 任务查询失败不影响主界面
    }
  }

  Future<void> _loadSidebarPref() async {
    try {
      final prefs = await SharedPreferences.getInstance();
      if (mounted) {
        setState(
          () => _userCollapsed = prefs.getBool('ui.sidebar_collapsed') ?? false,
        );
      }
    } catch (_) {}
  }

  Future<void> _toggleSidebar() async {
    final next = !_userCollapsed;
    setState(() => _userCollapsed = next);
    try {
      final prefs = await SharedPreferences.getInstance();
      await prefs.setBool('ui.sidebar_collapsed', next);
    } catch (_) {}
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

  void _selectDashboardTab(String tab) {
    setState(() {
      _selectedIndex = 0;
      _dashboardTab = tab;
    });
  }

  void _onLibrarySelected(String name) {
    setState(() {
      _currentLibrary = name;
      _selectedIndex = 2; // 切换到扫描页
    });
  }

  /// 收藏库搜索框输入：防抖 350ms 后发起搜索；清空时立即复位。
  void _onLibSearchChanged(String text) {
    _libSearchDebounce?.cancel();
    final q = text.trim();
    if (q.isEmpty) {
      setState(() {
        _libSearchQuery = '';
        _libSearchResults = [];
        _libSearchLoading = false;
      });
      return;
    }
    _libSearchDebounce = Timer(const Duration(milliseconds: 350), () {
      _runLibrarySearch(q);
    });
  }

  /// 执行收藏库文件搜索（防抖触发 / 回车立即）。
  Future<void> _runLibrarySearch(String q) async {
    setState(() {
      _libSearchQuery = q;
      _libSearchLoading = true;
    });
    try {
      final results = await api.searchLibraries(q);
      if (!mounted || _libSearchCtrl.text.trim() != q) return; // 输入已变化，丢弃过期结果
      setState(() {
        _libSearchResults = results;
        _libSearchLoading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _libSearchResults = [];
        _libSearchLoading = false;
      });
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text('搜索失败: $e'),
          backgroundColor: const Color(0xFFEF4444),
        ),
      );
    }
  }

  /// 退出搜索模式：清空搜索框与结果（点击搜索结果跳转后调用）。
  void _exitLibrarySearch() {
    _libSearchDebounce?.cancel();
    _libSearchCtrl.clear();
    setState(() {
      _libSearchQuery = '';
      _libSearchResults = [];
      _libSearchLoading = false;
    });
  }

  /// 当前页面标题
  String get _currentPageTitle {
    if (_selectedIndex < _destinations.length) {
      return _destinations[_selectedIndex].label;
    }
    if (_selectedIndex == _destinations.length) return '设置';
    return '';
  }

  /// 当前页面内容
  Widget _buildContent() {
    return switch (_selectedIndex) {
      0 => DashboardScreen(
        api: api,
        activeTab: _dashboardTab,
        onTabChanged: _selectDashboardTab,
        onOpenScan: () => _onSelectPage(2),
      ),
      1 => LibraryScreen(
        api: api,
        onLibrarySelected: _onLibrarySelected,
        searchQuery: _libSearchQuery,
        searchResults: _libSearchResults,
        searchLoading: _libSearchLoading,
        onSearchExit: _exitLibrarySearch,
      ),
      2 => ScanScreen(api: api, currentLibrary: _currentLibrary),
      3 => TaskScreen(api: api),
      4 => SearchScreen(api: api),
      5 => ReportScreen(api: api),
      6 => DirCompareScreen(api: api),
      7 => ToolScreen(api: api),
      8 => SettingsScreen(api: api),
      _ => const SizedBox.shrink(),
    };
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final isDark = Theme.of(context).brightness == Brightness.dark;

    return Shortcuts(
      shortcuts: const {
        SingleActivator(
          LogicalKeyboardKey.digit1,
          control: true,
        ): _SelectPageIntent(0),
        SingleActivator(
          LogicalKeyboardKey.digit2,
          control: true,
        ): _SelectPageIntent(1),
        SingleActivator(
          LogicalKeyboardKey.digit3,
          control: true,
        ): _SelectPageIntent(2),
        SingleActivator(
          LogicalKeyboardKey.digit4,
          control: true,
        ): _SelectPageIntent(3),
        SingleActivator(
          LogicalKeyboardKey.digit5,
          control: true,
        ): _SelectPageIntent(4),
        SingleActivator(
          LogicalKeyboardKey.digit6,
          control: true,
        ): _SelectPageIntent(5),
        SingleActivator(
          LogicalKeyboardKey.digit7,
          control: true,
        ): _SelectPageIntent(6),
        SingleActivator(
          LogicalKeyboardKey.digit8,
          control: true,
        ): _SelectPageIntent(7),
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
            body: LayoutBuilder(
              builder: (context, constraints) {
                final collapsed =
                    _userCollapsed ||
                    (constraints.maxWidth < 900 && _autoCollapseThreshold);
                return Row(
                  children: [
                    _buildSidebar(cs, isDark, collapsed),
                    Expanded(
                      child: Column(
                        children: [
                          _buildToolbar(cs, collapsed),
                          Expanded(child: _buildContent()),
                          StatusBar(
                            apiStatus: _apiStatus,
                            currentLibrary: _currentLibrary,
                            scanProgress: _scanProgress,
                            runningTasks: _runningTasks,
                          ),
                        ],
                      ),
                    ),
                  ],
                );
              },
            ),
          ),
        ),
      ),
    );
  }

  /// 构建左侧导航栏
  Widget _buildSidebar(ColorScheme cs, bool isDark, bool collapsed) {
    return AnimatedContainer(
      duration: const Duration(milliseconds: 180),
      curve: Curves.easeOutCubic,
      width: collapsed ? _sidebarCollapsedWidth : _sidebarExpandedWidth,
      decoration: BoxDecoration(
        color: cs.surfaceContainerLow,
        border: Border(right: BorderSide(color: cs.outlineVariant, width: 0.5)),
      ),
      child: Column(
        children: [
          // 品牌区：与内容区顶部保留相同的呼吸感。
          Container(
            height: 76,
            padding: EdgeInsets.symmetric(horizontal: collapsed ? 16 : 18),
            alignment: Alignment.centerLeft,
            child:
                collapsed
                    ? Tooltip(
                      message: '展开导航栏',
                      child: InkWell(
                        borderRadius: BorderRadius.circular(8),
                        onTap: _toggleSidebar,
                        child: Icon(
                          Icons.grid_view_rounded,
                          size: 21,
                          color: cs.primary,
                        ),
                      ),
                    )
                    : Row(
                      children: [
                        Icon(
                          Icons.grid_view_rounded,
                          size: 21,
                          color: cs.primary,
                        ),
                        const SizedBox(width: 10),
                        const Spacer(),
                        Tooltip(
                          message: '收起导航栏',
                          child: InkWell(
                            borderRadius: BorderRadius.circular(8),
                            onTap: _toggleSidebar,
                            child: Icon(
                              Icons.chevron_left,
                              size: 18,
                              color: cs.outline,
                            ),
                          ),
                        ),
                      ],
                    ),
          ),
          const SizedBox(height: 10),
          // 导航项
          for (int i = 0; i < _destinations.length; i++)
            _buildNavItem(i, cs, isDark, collapsed: collapsed),
          const Spacer(),
          // 底部设置
          Padding(
            padding: const EdgeInsets.all(12),
            child: Column(
              children: [
                Divider(height: 1, color: cs.outlineVariant),
                const SizedBox(height: 8),
                _buildBottomItem(
                  icon: Icons.settings_outlined,
                  label: '设置',
                  selected: _selectedIndex == _destinations.length,
                  cs: cs,
                  onTap: () => _onSelectPage(_destinations.length),
                  collapsed: collapsed,
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  /// 构建侧边栏导航项
  Widget _buildNavItem(
    int index,
    ColorScheme cs,
    bool isDark, {
    bool collapsed = false,
  }) {
    final dest = _destinations[index];
    final selected = _selectedIndex == index;

    return Padding(
      padding: EdgeInsets.symmetric(
        horizontal: collapsed ? 12 : 10,
        vertical: 3,
      ),
      child: Tooltip(
        message:
            collapsed
                ? '${dest.tooltip} (${dest.shortcut})'
                : '${dest.tooltip} (${dest.shortcut})',
        child: Material(
          color: Colors.transparent,
          borderRadius: BorderRadius.circular(10),
          child: InkWell(
            borderRadius: BorderRadius.circular(10),
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
              padding: EdgeInsets.symmetric(
                horizontal: collapsed ? 0 : 13,
                vertical: 12,
              ),
              decoration: BoxDecoration(
                color:
                    selected
                        ? cs.primary.withValues(alpha: isDark ? 0.15 : 0.1)
                        : Colors.transparent,
                borderRadius: BorderRadius.circular(10),
              ),
              child:
                  collapsed
                      ? Center(
                        child: Icon(
                          selected ? dest.selectedIcon : dest.icon,
                          size: 19,
                          color: selected ? cs.primary : cs.onSurfaceVariant,
                        ),
                      )
                      : Row(
                        children: [
                          Icon(
                            selected ? dest.selectedIcon : dest.icon,
                            size: 19,
                            color: selected ? cs.primary : cs.onSurfaceVariant,
                          ),
                          const SizedBox(width: 10),
                          Expanded(
                            child: Text(
                              dest.label,
                              style: TextStyle(
                                fontSize: 13.5,
                                fontWeight:
                                    selected
                                        ? FontWeight.w600
                                        : FontWeight.w400,
                                color:
                                    selected ? cs.primary : cs.onSurfaceVariant,
                              ),
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

  /// 底部导航项
  Widget _buildBottomItem({
    required IconData icon,
    required String label,
    required bool selected,
    required ColorScheme cs,
    required VoidCallback onTap,
    bool collapsed = false,
  }) {
    return Material(
      color: Colors.transparent,
      borderRadius: BorderRadius.circular(10),
      child: InkWell(
        borderRadius: BorderRadius.circular(10),
        onTap: onTap,
        child: Container(
          padding: EdgeInsets.symmetric(
            horizontal: collapsed ? 0 : 13,
            vertical: 12,
          ),
          child:
              collapsed
                  ? Center(
                    child: Icon(icon, size: 20, color: cs.onSurfaceVariant),
                  )
                  : Row(
                    children: [
                      Icon(icon, size: 20, color: cs.onSurfaceVariant),
                      const SizedBox(width: 10),
                      Text(
                        label,
                        style: TextStyle(
                          fontSize: 13.5,
                          color: cs.onSurfaceVariant,
                        ),
                      ),
                    ],
                  ),
        ),
      ),
    );
  }

  /// 顶部工具栏
  Widget _buildToolbar(ColorScheme cs, bool collapsed) {
    return Container(
      height: 96,
      padding: const EdgeInsets.fromLTRB(22, 20, 22, 16),
      decoration: BoxDecoration(
        color: Theme.of(context).scaffoldBackgroundColor,
        border: Border(
          bottom: BorderSide(
            color: cs.outlineVariant.withValues(alpha: .8),
            width: 0.5,
          ),
        ),
      ),
      child: Row(
        children: [
          if (collapsed) ...[
            Tooltip(
              message: '展开导航栏',
              child: IconButton(
                icon: Icon(Icons.menu, size: 20, color: cs.outline),
                onPressed: _toggleSidebar,
              ),
            ),
            const SizedBox(width: 8),
          ],
          if (_selectedIndex == 0)
            _buildDashboardTabs(cs)
          else
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
          if (_selectedIndex == 1) ...[
            // 搜索框居中：左右各一个 Expanded 对称占位，中间固定宽 360
            _buildLibrarySearchField(cs),
          ],
          if (_selectedIndex == 1)
            // 右侧对称占位：主题按钮靠右，保证搜索框精确居中
            Expanded(
              child: Row(
                mainAxisAlignment: MainAxisAlignment.end,
                children: [_buildThemeButton(cs)],
              ),
            )
          else ...[
            if (_selectedIndex == 0 && _dashboardTab == 'library')
              _buildLibraryLayoutMenu(cs),
            const SizedBox(width: 10),
            _buildThemeButton(cs),
          ],
        ],
      ),
    );
  }

  /// 主题切换按钮（圆形描边）。
  Widget _buildThemeButton(ColorScheme cs) {
    return DecoratedBox(
      decoration: BoxDecoration(
        color: cs.surfaceContainerLow,
        border: Border.all(color: cs.outlineVariant),
        borderRadius: BorderRadius.circular(12),
      ),
      child: IconButton(
        icon: Icon(
          Theme.of(context).brightness == Brightness.dark
              ? Icons.light_mode_outlined
              : Icons.dark_mode_outlined,
          size: 19,
        ),
        tooltip: '切换主题',
        onPressed: () => themeNotifier.toggle(),
      ),
    );
  }

  Widget _buildDashboardTabs(ColorScheme cs) {
    const tabs = <String, String>{
      'library': '库',
      'video': '视频',
      'image': '照片',
      'statistics': '统计',
    };
    final compact = MediaQuery.sizeOf(context).width < 820;
    return Expanded(
      child: SegmentedButton<String>(
        segments: [
          for (final entry in tabs.entries)
            ButtonSegment<String>(
              value: entry.key,
              label: compact ? null : Text(entry.value),
              icon: Icon(switch (entry.key) {
                'library' => Icons.folder_outlined,
                'video' => Icons.movie_outlined,
                'image' => Icons.image_outlined,
                _ => Icons.query_stats_outlined,
              }),
            ),
        ],
        selected: {_dashboardTab},
        onSelectionChanged: (value) => _selectDashboardTab(value.first),
        showSelectedIcon: false,
        style: ButtonStyle(
          minimumSize: const WidgetStatePropertyAll(Size.fromHeight(46)),
          padding: const WidgetStatePropertyAll(
            EdgeInsets.symmetric(horizontal: 20),
          ),
          side: WidgetStatePropertyAll(BorderSide(color: cs.outlineVariant)),
          shape: WidgetStatePropertyAll(
            RoundedRectangleBorder(borderRadius: BorderRadius.circular(14)),
          ),
          backgroundColor: WidgetStateProperty.resolveWith((states) {
            // 判断是否处于选中状态
            if (states.contains(WidgetState.selected)) {
              return cs.primary.withValues(
                alpha:
                    Theme.of(context).brightness == Brightness.dark ? .24 : .13,
              );
            }
            // 默认背景色
            return cs.surfaceContainerLow;
          }),
          textStyle: const WidgetStatePropertyAll(
            TextStyle(fontSize: 13, fontWeight: FontWeight.w600),
          ),
          foregroundColor: WidgetStatePropertyAll(cs.onSurface),
        ),
      ),
    );
  }

  Widget _buildLibraryLayoutMenu(ColorScheme cs) {
    final layout = displayPreferences.libraryLayout;
    return PopupMenuButton<String>(
      tooltip: '库布局',
      initialValue: layout,
      onSelected: displayPreferences.setLibraryLayout,
      icon: Icon(Icons.view_quilt_outlined, color: cs.onSurfaceVariant),
      itemBuilder:
          (context) => const [
            PopupMenuItem(
              value: 'masonry',
              child: ListTile(
                leading: Icon(Icons.view_column_outlined),
                title: Text('瀑布流'),
                contentPadding: EdgeInsets.zero,
              ),
            ),
            PopupMenuItem(
              value: 'square',
              child: ListTile(
                leading: Icon(Icons.grid_view_outlined),
                title: Text('网格正切'),
                contentPadding: EdgeInsets.zero,
              ),
            ),
            PopupMenuItem(
              value: 'adaptive',
              child: ListTile(
                leading: Icon(Icons.dashboard_outlined),
                title: Text('网格自适应'),
                contentPadding: EdgeInsets.zero,
              ),
            ),
          ],
    );
  }

  /// 收藏库页文件搜索框：圆角胶囊 + 搜索图标 + 加载/清除后缀。
  Widget _buildLibrarySearchField(ColorScheme cs) {
    final hasText = _libSearchCtrl.text.isNotEmpty;
    return SizedBox(
      width: 360,
      height: 36,
      child: TextField(
        controller: _libSearchCtrl,
        onChanged: _onLibSearchChanged,
        onSubmitted: _runLibrarySearch,
        textInputAction: TextInputAction.search,
        style: TextStyle(fontSize: 13, color: cs.onSurface),
        decoration: InputDecoration(
          hintText: '搜索文件名或文件夹',
          hintStyle: TextStyle(fontSize: 13, color: cs.outline),
          isDense: true,
          contentPadding: const EdgeInsets.symmetric(vertical: 8),
          // border: InputBorder.none,
          prefixIcon: Icon(Icons.search, size: 18, color: cs.outline),
          suffixIcon:
              _libSearchLoading
                  ? Padding(
                    padding: const EdgeInsets.all(9),
                    child: SizedBox(
                      width: 14,
                      height: 14,
                      child: CircularProgressIndicator(
                        strokeWidth: 1.8,
                        color: cs.primary,
                      ),
                    ),
                  )
                  : hasText
                  ? IconButton(
                    icon: Icon(Icons.close, size: 16, color: cs.outline),
                    tooltip: '清除',
                    onPressed: () {
                      _libSearchCtrl.clear();
                      _onLibSearchChanged('');
                    },
                  )
                  : null,
        ),
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
