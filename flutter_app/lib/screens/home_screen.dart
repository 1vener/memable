// home_screen.dart：桌面端主框架 — 左侧导航栏 + 右侧内容区
// 代码注释使用中文
import 'package:flutter/material.dart';
import '../services/api_service.dart';
import 'library_screen.dart';
import 'search_screen.dart';
import 'report_screen.dart';
import 'scan_screen.dart';

class HomeScreen extends StatefulWidget {
  final ApiService api;
  const HomeScreen({super.key, required this.api});

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  int _selectedIndex = 0;

  static const _navItems = [
    _NavItem(icon: Icons.folder_outlined, selectedIcon: Icons.folder, label: '收藏库'),
    _NavItem(icon: Icons.sync_outlined, selectedIcon: Icons.sync, label: '扫描'),
    _NavItem(icon: Icons.search_outlined, selectedIcon: Icons.search, label: '搜索'),
    _NavItem(icon: Icons.assessment_outlined, selectedIcon: Icons.assessment, label: '报告'),
  ];

  late final List<Widget> _screens = [
    LibraryScreen(api: widget.api),
    ScanScreen(api: widget.api),
    SearchScreen(api: widget.api),
    ReportScreen(api: widget.api),
  ];

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Row(
        children: [
          // 左侧导航栏
          _buildSidebar(),
          // 垂直分隔线
          const VerticalDivider(width: 1, color: Color(0xFFE2E8F0)),
          // 右侧内容区
          Expanded(
            child: _screens[_selectedIndex],
          ),
        ],
      ),
    );
  }

  Widget _buildSidebar() {
    return Container(
      width: 220,
      color: Colors.white,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // Logo / 标题
          Container(
            padding: const EdgeInsets.fromLTRB(20, 24, 20, 20),
            child: Row(
              children: [
                Container(
                  width: 32,
                  height: 32,
                  decoration: BoxDecoration(
                    color: const Color(0xFF2563EB),
                    borderRadius: BorderRadius.circular(8),
                  ),
                  child: const Icon(Icons.photo_library, color: Colors.white, size: 18),
                ),
                const SizedBox(width: 10),
                const Text(
                  'memable',
                  style: TextStyle(fontSize: 18, fontWeight: FontWeight.w700, letterSpacing: -0.5),
                ),
              ],
            ),
          ),
          const Divider(height: 1, color: Color(0xFFE2E8F0)),
          const SizedBox(height: 8),
          // 导航项
          ...List.generate(_navItems.length, (i) {
            final item = _navItems[i];
            final selected = i == _selectedIndex;
            return Padding(
              padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
              child: Material(
                color: selected ? const Color(0xFFEFF6FF) : Colors.transparent,
                borderRadius: BorderRadius.circular(10),
                child: InkWell(
                  borderRadius: BorderRadius.circular(10),
                  onTap: () => setState(() => _selectedIndex = i),
                  child: Container(
                    padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
                    child: Row(
                      children: [
                        Icon(
                          selected ? item.selectedIcon : item.icon,
                          size: 20,
                          color: selected ? const Color(0xFF2563EB) : const Color(0xFF64748B),
                        ),
                        const SizedBox(width: 10),
                        Text(
                          item.label,
                          style: TextStyle(
                            fontSize: 14,
                            fontWeight: selected ? FontWeight.w600 : FontWeight.w400,
                            color: selected ? const Color(0xFF2563EB) : const Color(0xFF334155),
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            );
          }),
          const Spacer(),
          // 底部信息
          const Padding(
            padding: EdgeInsets.all(16),
            child: Text(
              'v1.0.0',
              style: TextStyle(fontSize: 12, color: Color(0xFF94A3B8)),
            ),
          ),
        ],
      ),
    );
  }
}

class _NavItem {
  final IconData icon;
  final IconData selectedIcon;
  final String label;
  const _NavItem({required this.icon, required this.selectedIcon, required this.label});
}
