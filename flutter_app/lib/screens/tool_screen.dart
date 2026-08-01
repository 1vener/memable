// tool_screen.dart：工具页面（文件统计）
// 代码注释使用中文
import 'dart:math' as math;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:fl_chart/fl_chart.dart';
import 'package:file_picker/file_picker.dart';
import '../services/api_service.dart';
import '../models/models.dart';
import '../utils/time_fmt.dart';

/// 饼图颜色表（固定 8 色 + 灰色表示"其他"）
const _pieColors = [
  Color(0xFF2563EB), // 蓝
  Color(0xFF7C3AED), // 紫
  Color(0xFF059669), // 绿
  Color(0xFFD97706), // 橙
  Color(0xFFDC2626), // 红
  Color(0xFF0891B2), // 青
  Color(0xFFDB2777), // 粉
  Color(0xFF4F46E5), // 靛
];

const _othersColor = Color(0xFF9CA3AF); // 灰

/// 扩展名详情排序字段
enum _ExtSortField { count, bytes }

class ToolScreen extends StatefulWidget {
  final ApiService api;
  const ToolScreen({super.key, required this.api});

  @override
  State<ToolScreen> createState() => _ToolScreenState();
}

class _ToolScreenState extends State<ToolScreen> {
  final _dirPathCtrl = TextEditingController();
  bool _loading = false;
  String? _error;
  FileStats? _currentStats;
  List<FileStats>? _historyList;

  // 饼图触摸交互
  int _touchedCountIdx = -1;
  int _touchedBytesIdx = -1;

  // 文件树状态
  final Set<String> _expandedDirs = {};
  String? _selectedTreePath;

  // 目录差异（diff）状态
  FileStatsDiff? _diff;
  int? _diffStatsId; // diff 对应的统计记录 ID
  bool _diffLoading = false;
  final Set<String> _diffExpanded = {}; // diff 树展开状态，默认全部折叠

  // 扩展名详情排序状态
  _ExtSortField _extSortField = _ExtSortField.count;
  bool _extSortAsc = false;

  @override
  void initState() {
    super.initState();
    _loadHistory();
  }

  @override
  void dispose() {
    _dirPathCtrl.dispose();
    super.dispose();
  }

  Future<void> _loadHistory() async {
    try {
      final list = await widget.api.getFileStatsList();
      if (mounted) setState(() => _historyList = list);
    } catch (_) {}
  }

  Future<void> _browseDir() async {
    final result = await FilePicker.platform.getDirectoryPath();
    if (result != null) {
      _dirPathCtrl.text = result;
    }
  }

  Future<void> _startStats() async {
    final dirPath = _dirPathCtrl.text.trim();
    if (dirPath.isEmpty) return;

    setState(() { _loading = true; _error = null; _expandedDirs.clear(); _selectedTreePath = null; _diff = null; _diffStatsId = null; });
    try {
      final stats = await widget.api.createFileStats(dirPath);
      if (mounted) {
        setState(() { _currentStats = stats; _loading = false; });
        _loadHistory();
      }
    } catch (e) {
      if (mounted) setState(() { _error = '$e'; _loading = false; });
    }
  }

  Future<void> _viewHistory(FileStats fs) async {
    try {
      final stats = await widget.api.getFileStats(fs.id);
      if (mounted) setState(() { _currentStats = stats; _expandedDirs.clear(); _selectedTreePath = null; _diff = null; _diffStatsId = null; });
    } catch (e) {
      if (mounted) setState(() => _error = '加载记录失败: $e');
    }
  }

  /// 对比指定统计记录与目录当前状态，展示新增/删除文件目录树。
  Future<void> _runDiff(FileStats fs) async {
    setState(() { _diffLoading = true; _error = null; _diff = null; _diffStatsId = fs.id; _diffExpanded.clear(); });
    try {
      final diff = await widget.api.getFileStatsDiff(fs.id);
      if (mounted) {
        setState(() { _diff = diff; _diffLoading = false; });
      }
    } catch (e) {
      if (mounted) setState(() { _diffLoading = false; _error = '对比目录差异失败: $e'; });
    }
  }

  /// 导出 diff 结果为 xlsx（两个 sheet：新增/删除文件列表），保存到用户选择的位置。
  Future<void> _exportDiff() async {
    final diff = _diff;
    final id = _diffStatsId;
    if (diff == null || id == null) return;
    setState(() => _diffLoading = true);
    try {
      final bytes = await widget.api.exportFileStatsDiff(id);
      final savePath = await FilePicker.platform.saveFile(
        dialogTitle: '导出目录差异',
        fileName: '文件差异_$id.xlsx',
        type: FileType.custom,
        allowedExtensions: ['xlsx'],
        bytes: bytes,
      );
      if (savePath != null && mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('已导出到 $savePath'),
            duration: const Duration(seconds: 3),
          ),
        );
      }
    } catch (e) {
      if (mounted) setState(() => _error = '导出 Excel 失败: $e');
    } finally {
      if (mounted) setState(() => _diffLoading = false);
    }
  }

  Future<void> _deleteHistory(int id) async {
    try {
      await widget.api.deleteFileStats(id);
      _loadHistory();
      if (_currentStats?.id == id) {
        setState(() => _currentStats = null);
      }
      if (_diffStatsId == id) {
        setState(() { _diff = null; _diffStatsId = null; });
      }
    } catch (e) {
      if (mounted) setState(() => _error = '删除失败: $e');
    }
  }

  /// 获取扩展名对应的颜色索引
  int _extColorIndex(int i, bool hasMore, int totalLen) {
    if (hasMore && i >= 8) return -1; // "其他"走灰色
    return i % _pieColors.length;
  }

  Color _extColor(int i, bool hasMore, int totalLen) {
    final idx = _extColorIndex(i, hasMore, totalLen);
    return idx < 0 ? _othersColor : _pieColors[idx];
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;

    return LayoutBuilder(
      builder: (context, constraints) {
        final w = constraints.maxWidth;
        final pad = w >= 1000 ? 24.0 : (w >= 600 ? 16.0 : 12.0);
        return Padding(
          padding: EdgeInsets.all(pad),
          child: SingleChildScrollView(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                _buildInputSection(cs, w),
                const SizedBox(height: 16),
                if (_error != null) _buildErrorCard(cs),
                if (_currentStats != null) ...[
                  _buildSummaryCard(cs),
                  const SizedBox(height: 16),
                  _buildPieChartWrapper(cs, w),
                  const SizedBox(height: 16),
                  _buildExtDetailTable(cs),
                  const SizedBox(height: 16),
                  _buildFileTreeSection(cs),
                ],
                if (_diff != null) ...[
                  const SizedBox(height: 16),
                  _buildDiffSection(cs),
                ],
                if (_currentStats == null && _error == null && !_loading)
                  _buildEmptyState(cs),
                if (_historyList != null && _historyList!.isNotEmpty) ...[
                  const SizedBox(height: 24),
                  _buildHistorySection(cs),
                ],
              ],
            ),
          ),
        );
      },
    );
  }

  Widget _buildInputSection(ColorScheme cs, double w) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.build_outlined, size: 22, color: cs.primary),
                const SizedBox(width: 10),
                Text('文件统计', style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600, color: cs.onSurface)),
              ],
            ),
            const SizedBox(height: 16),
            if (w >= 700)
              Row(
                children: [
                  Expanded(child: _buildDirTextField()),
                  const SizedBox(width: 10),
                  _buildBrowseButton(),
                  const SizedBox(width: 8),
                  _buildStartButton(),
                ],
              )
            else
              Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  _buildDirTextField(),
                  const SizedBox(height: 8),
                  Row(
                    children: [
                      _buildBrowseButton(),
                      const SizedBox(width: 8),
                      Expanded(child: _buildStartButton()),
                    ],
                  ),
                ],
              ),
          ],
        ),
      ),
    );
  }

  Widget _buildDirTextField() {
    return TextField(
      controller: _dirPathCtrl,
      style: const TextStyle(fontSize: 13),
      decoration: const InputDecoration(
        hintText: r'输入目录路径，例如 C:\Users\Photos',
        prefixIcon: Icon(Icons.folder_outlined, size: 20),
      ),
    );
  }

  Widget _buildBrowseButton() {
    return Tooltip(
      message: '选择目录',
      child: OutlinedButton.icon(
        onPressed: _loading ? null : _browseDir,
        icon: const Icon(Icons.folder_open, size: 18),
        label: const Text('浏览'),
      ),
    );
  }

  Widget _buildStartButton() {
    return FilledButton.icon(
      onPressed: _loading ? null : _startStats,
      icon: _loading
          ? const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
          : const Icon(Icons.analytics, size: 18),
      label: Text(_loading ? '统计中...' : '开始统计'),
    );
  }

  Widget _buildErrorCard(ColorScheme cs) {
    return Card(
      color: cs.error.withValues(alpha: 0.05),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Row(
          children: [
            Icon(Icons.error_outline, size: 20, color: cs.error),
            const SizedBox(width: 10),
            Expanded(child: Text(_error!, style: TextStyle(fontSize: 13, color: cs.error))),
          ],
        ),
      ),
    );
  }

  Widget _buildSummaryCard(ColorScheme cs) {
    final s = _currentStats!;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Wrap(
          spacing: 32,
          runSpacing: 12,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            _StatBadge(icon: Icons.insert_drive_file_outlined, label: '总文件数', value: '${s.totalCount}', cs: cs),
            _StatBadge(icon: Icons.storage_outlined, label: '总大小', value: s.totalBytesFormatted, cs: cs),
            if (s.createdAt != null)
              Text('统计时间: ${formatLocalTime(s.createdAt)}',
                  style: TextStyle(fontSize: 12, color: cs.outline)),
          ],
        ),
      ),
    );
  }

  // ========== 饼图 ==========

  Widget _buildPieChartWrapper(ColorScheme cs, double w) {
    final stats = _currentStats!.extStats;
    if (stats.isEmpty) return const SizedBox.shrink();

    if (w >= 1000) {
      return Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Expanded(child: _buildSinglePie(cs, stats, '文件数量占比', w, useBytes: false)),
          const SizedBox(width: 12),
          Expanded(child: _buildSinglePie(cs, stats, '存储空间占比', w, useBytes: true)),
        ],
      );
    }
    return Column(
      children: [
        _buildSinglePie(cs, stats, '文件数量占比', w, useBytes: false),
        const SizedBox(height: 12),
        _buildSinglePie(cs, stats, '存储空间占比', w, useBytes: true),
      ],
    );
  }

  Widget _buildSinglePie(ColorScheme cs, List<ExtStat> stats, String title, double w, {required bool useBytes}) {
    final top = stats.take(8).toList();
    final hasMore = stats.length > 8;
    final touchedIdx = useBytes ? _touchedBytesIdx : _touchedCountIdx;
    final displayItems = [
      ...top,
      if (hasMore) ExtStat(ext: '其他', bytes: 0, count: 0, pctCount: 0, pctBytes: 0),
    ];
    final pieSize = (w >= 420) ? 180.0 : 140.0;
    final cardW = w >= 1000 ? w / 2 - 30 : w - 24;
    final inlineLegend = cardW >= 420;

    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(title, style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: cs.onSurface)),
            const SizedBox(height: 16),
            if (inlineLegend)
              Row(
                children: [
                  SizedBox(width: pieSize, height: pieSize, child: _buildPieChart(top, hasMore, stats, useBytes, touchedIdx)),
                  const SizedBox(width: 16),
                  Expanded(child: _buildPieLegend(displayItems, hasMore, stats.length, touchedIdx, useBytes)),
                ],
              )
            else
              Column(
                children: [
                  Center(
                    child: SizedBox(width: pieSize, height: pieSize, child: _buildPieChart(top, hasMore, stats, useBytes, touchedIdx)),
                  ),
                  const SizedBox(height: 12),
                  _buildPieLegend(displayItems, hasMore, stats.length, touchedIdx, useBytes),
                ],
              ),
          ],
        ),
      ),
    );
  }

  Widget _buildPieChart(List<ExtStat> top, bool hasMore, List<ExtStat> all, bool useBytes, int touchedIdx) {
    return PieChart(
      PieChartData(
        sections: _buildPieSections(top, hasMore, all, useBytes: useBytes, touchedIdx: touchedIdx),
        centerSpaceRadius: 40,
        sectionsSpace: 2,
        pieTouchData: PieTouchData(
          touchCallback: (event, response) {
            if (!event.isInterestedForInteractions || response == null || response.touchedSection == null) {
              setState(() { if (useBytes) { _touchedBytesIdx = -1; } else { _touchedCountIdx = -1; }});
              return;
            }
            final idx = response.touchedSection!.touchedSectionIndex;
            setState(() { if (useBytes) { _touchedBytesIdx = idx; } else { _touchedCountIdx = idx; }});
          },
        ),
      ),
    );
  }

  Widget _buildPieLegend(List<ExtStat> displayItems, bool hasMore, int totalLen, int touchedIdx, bool useBytes) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        for (int i = 0; i < displayItems.length; i++) ...[
          if (i > 0) const SizedBox(height: 4),
          _buildLegendItem(displayItems[i], _extColor(i, hasMore, totalLen), i == touchedIdx, useBytes),
        ],
      ],
    );
  }

  Widget _buildLegendItem(ExtStat s, Color color, bool highlighted, bool useBytes) {
    final pct = useBytes ? s.pctBytes : s.pctCount;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
      decoration: highlighted
          ? BoxDecoration(
              color: color.withValues(alpha: 0.1),
              borderRadius: BorderRadius.circular(6),
            )
          : null,
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            width: 8,
            height: 8,
            decoration: BoxDecoration(
              color: color,
              shape: BoxShape.circle,
            ),
          ),
          const SizedBox(width: 6),
          Flexible(
            child: Text(
              s.ext,
              style: TextStyle(
                fontSize: 11,
                fontWeight: highlighted ? FontWeight.w600 : FontWeight.w400,
              ),
              overflow: TextOverflow.ellipsis,
            ),
          ),
          const SizedBox(width: 4),
          Text(
            '${pct.toStringAsFixed(1)}%',
            style: TextStyle(
              fontSize: 11,
              color: Colors.grey,
              fontWeight: highlighted ? FontWeight.w600 : FontWeight.w400,
            ),
          ),
        ],
      ),
    );
  }

  List<PieChartSectionData> _buildPieSections(List<ExtStat> top, bool hasMore, List<ExtStat> all,
      {required bool useBytes, required int touchedIdx}) {
    final sections = <PieChartSectionData>[];
    for (int i = 0; i < top.length; i++) {
      final s = top[i];
      final pct = useBytes ? s.pctBytes : s.pctCount;
      final isTouched = i == touchedIdx;
      sections.add(PieChartSectionData(
        value: pct,
        color: _extColor(i, hasMore, all.length),
        title: isTouched ? s.ext : '${pct.toStringAsFixed(1)}%',
        radius: isTouched ? 58 : 50,
        titleStyle: TextStyle(
          fontSize: isTouched ? 12 : 10,
          color: Colors.white,
          fontWeight: FontWeight.w600,
        ),
        badgeWidget: isTouched
            ? Container(
                padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 1),
                decoration: BoxDecoration(
                  color: Colors.black87,
                  borderRadius: BorderRadius.circular(4),
                ),
                child: Text(
                  '${pct.toStringAsFixed(1)}%',
                  style: const TextStyle(fontSize: 9, color: Colors.white),
                ),
              )
            : null,
        badgePositionPercentageOffset: 1.3,
      ));
    }
    if (hasMore) {
      double others = 0;
      for (int i = 8; i < all.length; i++) {
        others += useBytes ? all[i].pctBytes : all[i].pctCount;
      }
      if (others > 0) {
        sections.add(PieChartSectionData(
          value: others,
          color: _othersColor,
          title: '其他\n${others.toStringAsFixed(1)}%',
          radius: 50,
          titleStyle: const TextStyle(fontSize: 9, color: Colors.white, fontWeight: FontWeight.w600),
        ));
      }
    }
    return sections;
  }

  // ========== 扩展名详情 ==========

  List<ExtStat> _sortedExtStats() {
    final stats = List<ExtStat>.from(_currentStats!.extStats);
    stats.sort((a, b) {
      final cmp = _extSortField == _ExtSortField.count
          ? a.count.compareTo(b.count)
          : a.bytes.compareTo(b.bytes);
      return _extSortAsc ? cmp : -cmp;
    });
    return stats;
  }

  Widget _buildExtDetailTable(ColorScheme cs) {
    final stats = _currentStats!.extStats;
    if (stats.isEmpty) return const SizedBox.shrink();
    final sorted = _sortedExtStats();
    final hasMore = stats.length > 8;

    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // 标题 + 排序控件 + 复制按钮
            Wrap(
              spacing: 8,
              runSpacing: 8,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [
                Text('扩展名统计详情', style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: cs.onSurface)),
                _buildSortButton(cs, '按数量', _ExtSortField.count),
                _buildSortButton(cs, '按大小', _ExtSortField.bytes),
                OutlinedButton.icon(
                  onPressed: () => _copyExtStatsToClipboard(cs, sorted),
                  icon: const Icon(Icons.copy, size: 16),
                  label: const Text('复制'),
                  style: OutlinedButton.styleFrom(visualDensity: VisualDensity.compact),
                ),
              ],
            ),
            const SizedBox(height: 12),
            // 表格体：水平滚动避免压缩列
            SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: ConstrainedBox(
                constraints: const BoxConstraints(minWidth: 620),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    // 列标题
                    Padding(
                      padding: const EdgeInsets.only(bottom: 6),
                      child: Row(
                        children: [
                          const SizedBox(width: 16),
                          SizedBox(width: 100, child: Text('扩展名', style: TextStyle(fontSize: 12, color: cs.outline, fontWeight: FontWeight.w500))),
                          SizedBox(width: 80, child: Text('文件数', style: TextStyle(fontSize: 12, color: cs.outline, fontWeight: FontWeight.w500))),
                          SizedBox(width: 100, child: Text('大小', style: TextStyle(fontSize: 12, color: cs.outline, fontWeight: FontWeight.w500))),
                          SizedBox(width: 200, child: Text('数量占比', style: TextStyle(fontSize: 12, color: cs.outline, fontWeight: FontWeight.w500))),
                          SizedBox(width: 100, child: Text('空间占比', style: TextStyle(fontSize: 12, color: cs.outline, fontWeight: FontWeight.w500))),
                        ],
                      ),
                    ),
                    const Divider(height: 1),
                    ...sorted.map((s) {
                      final i = stats.indexOf(s);
                      final color = _extColor(i, hasMore, stats.length);
                      return Padding(
                        padding: const EdgeInsets.symmetric(vertical: 4),
                        child: Row(
                          children: [
                            Container(width: 8, height: 8, decoration: BoxDecoration(color: color, shape: BoxShape.circle)),
                            const SizedBox(width: 8),
                            Tooltip(
                              message: s.ext,
                              child: SizedBox(width: 100, child: Text(s.ext, style: TextStyle(fontSize: 13, color: cs.onSurface), overflow: TextOverflow.ellipsis, maxLines: 1)),
                            ),
                            SizedBox(width: 80, child: Text('${s.count}', style: TextStyle(fontSize: 13, color: cs.onSurface))),
                            SizedBox(width: 100, child: Text(_formatBytes(s.bytes), style: TextStyle(fontSize: 13, color: cs.onSurface))),
                            SizedBox(width: 200, child: _buildMiniBar(s.pctCount, cs)),
                            SizedBox(width: 100, child: Text('${s.pctBytes.toStringAsFixed(1)}%', style: TextStyle(fontSize: 13, color: cs.onSurface))),
                          ],
                        ),
                      );
                    }),
                  ],
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  /// 构建排序切换按钮（再次点击同一字段切换升/降序）
  Widget _buildSortButton(ColorScheme cs, String label, _ExtSortField field) {
    final active = _extSortField == field;
    return OutlinedButton.icon(
      onPressed: () {
        setState(() {
          if (active) {
            _extSortAsc = !_extSortAsc;
          } else {
            _extSortField = field;
            _extSortAsc = false; // 新字段默认降序
          }
        });
      },
      icon: Icon(
        active ? (_extSortAsc ? Icons.arrow_upward : Icons.arrow_downward) : Icons.sort,
        size: 16,
      ),
      label: Text(label),
      style: OutlinedButton.styleFrom(
        visualDensity: VisualDensity.compact,
        foregroundColor: active ? cs.primary : null,
        side: active ? BorderSide(color: cs.primary) : null,
      ),
    );
  }

  /// 一键复制扩展名统计为 Excel 可识别的 TSV（带表头）
  Future<void> _copyExtStatsToClipboard(ColorScheme cs, List<ExtStat> stats) async {
    final buffer = StringBuffer();
    // 表头（Tab 分隔，Excel 粘贴后自动分列）
    buffer.writeln('扩展名\t文件数\t大小(字节)\t大小\t数量占比(%)\t空间占比(%)');
    for (final s in stats) {
      buffer.writeln(
        '${s.ext}\t${s.count}\t${s.bytes}\t${_formatBytes(s.bytes)}\t${s.pctCount.toStringAsFixed(1)}\t${s.pctBytes.toStringAsFixed(1)}',
      );
    }
    await Clipboard.setData(ClipboardData(text: buffer.toString()));
    if (mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text('已复制 ${stats.length} 行到剪贴板，可直接粘贴到 Excel'),
          duration: const Duration(seconds: 2),
        ),
      );
    }
  }

  Widget _buildMiniBar(double pct, ColorScheme cs) {
    return Container(
      height: 16,
      decoration: BoxDecoration(
        color: cs.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(8),
      ),
      clipBehavior: Clip.antiAlias,
      child: FractionallySizedBox(
        widthFactor: pct / 100,
        child: Container(decoration: BoxDecoration(color: cs.primary)),
      ),
    );
  }

  // ========== 文件树（沿用收藏库 _TreeDirTile 样式） ==========

  Widget _buildFileTreeSection(ColorScheme cs) {
    final tree = _currentStats!.fileTree;
    if (tree.isEmpty) return const SizedBox.shrink();

    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.account_tree_outlined, size: 18, color: cs.primary),
                const SizedBox(width: 8),
                Text('文件树', style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: cs.onSurface)),
                const Spacer(),
                Text(_currentStats!.dirPath, style: TextStyle(fontSize: 11, color: cs.outline), overflow: TextOverflow.ellipsis),
              ],
            ),
            const Divider(height: 16),
            ...tree.map((node) => _buildTreeDirTile(node, cs, 0)),
          ],
        ),
      ),
    );
  }

  // ========== 目录差异（diff） ==========

  /// 新增/删除文件差异卡片：两个目录树（叶子为文件名，默认全部折叠）+ 导出 Excel。
  Widget _buildDiffSection(ColorScheme cs) {
    final diff = _diff!;
    final addedTree = _buildPathTree(diff.added);
    final removedTree = _buildPathTree(diff.removed);
    const addedColor = Color(0xFF059669); // 绿：新增
    const removedColor = Color(0xFFDC2626); // 红：删除

    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.compare_arrows, size: 18, color: cs.primary),
                const SizedBox(width: 8),
                Text('目录差异', style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: cs.onSurface)),
                const Spacer(),
                OutlinedButton.icon(
                  onPressed: _diffLoading ? null : _exportDiff,
                  icon: _diffLoading
                      ? const SizedBox(width: 14, height: 14, child: CircularProgressIndicator(strokeWidth: 2))
                      : const Icon(Icons.file_download_outlined, size: 16),
                  label: const Text('导出Excel'),
                  style: OutlinedButton.styleFrom(visualDensity: VisualDensity.compact),
                ),
              ],
            ),
            const SizedBox(height: 4),
            Text(diff.dirPath,
                style: TextStyle(fontSize: 11, color: cs.outline), overflow: TextOverflow.ellipsis),
            const Divider(height: 16),
            Row(
              children: [
                const Icon(Icons.add_circle_outline, size: 16, color: addedColor),
                const SizedBox(width: 6),
                Text('新增文件（${diff.addedCount}）',
                    style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600, color: cs.onSurface)),
              ],
            ),
            const SizedBox(height: 6),
            if (addedTree.isEmpty)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 6),
                child: Text('无新增文件', style: TextStyle(fontSize: 12, color: cs.outline)),
              )
            else
              _buildDiffTree(addedTree, cs, addedColor),
            const SizedBox(height: 12),
            Row(
              children: [
                const Icon(Icons.remove_circle_outline, size: 16, color: removedColor),
                const SizedBox(width: 6),
                Text('删除文件（${diff.removedCount}）',
                    style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600, color: cs.onSurface)),
              ],
            ),
            const SizedBox(height: 6),
            if (removedTree.isEmpty)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 6),
                child: Text('无删除文件', style: TextStyle(fontSize: 12, color: cs.outline)),
              )
            else
              _buildDiffTree(removedTree, cs, removedColor),
          ],
        ),
      ),
    );
  }

  /// 将扁平路径列表构建为目录树（目录→子目录→文件叶子，路径按字典序传入）。
  List<_DiffTreeNode> _buildPathTree(List<String> paths) {
    final roots = <_DiffTreeNode>[];
    for (final p in paths) {
      final parts = p.split('/');
      var level = roots;
      var prefix = '';
      for (var i = 0; i < parts.length; i++) {
        if (parts[i].isEmpty) continue;
        prefix = prefix.isEmpty ? parts[i] : '$prefix/${parts[i]}';
        final isFile = i == parts.length - 1;
        _DiffTreeNode? node;
        for (final n in level) {
          if (n.path == prefix) {
            node = n;
            break;
          }
        }
        if (node == null) {
          node = _DiffTreeNode(name: parts[i], path: prefix, isDir: !isFile);
          level.add(node);
        }
        level = node.children;
      }
    }
    return roots;
  }

  Widget _buildDiffTree(List<_DiffTreeNode> roots, ColorScheme cs, Color accent) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [for (final n in roots) _buildDiffNode(n, cs, accent, 0)],
    );
  }

  /// 单个 diff 树节点：目录可点击展开/折叠（默认折叠），文件叶子显示文件名。
  Widget _buildDiffNode(_DiffTreeNode node, ColorScheme cs, Color accent, int depth) {
    if (!node.isDir) {
      return Padding(
        padding: EdgeInsets.only(left: 12.0 + depth * 16, top: 6, bottom: 6),
        child: Row(
          children: [
            Icon(Icons.insert_drive_file_outlined, size: 15, color: cs.outline),
            const SizedBox(width: 8),
            Expanded(
              child: Text(node.name,
                  style: TextStyle(fontSize: 13, color: cs.onSurface), overflow: TextOverflow.ellipsis),
            ),
          ],
        ),
      );
    }
    final isExpanded = _diffExpanded.contains(node.path);
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        InkWell(
          borderRadius: BorderRadius.circular(8),
          onTap: () {
            setState(() {
              if (isExpanded) {
                _diffExpanded.remove(node.path);
              } else {
                _diffExpanded.add(node.path);
              }
            });
          },
          child: Padding(
            padding: EdgeInsets.only(left: 12.0 + depth * 16, top: 8, bottom: 8),
            child: Row(
              children: [
                Icon(isExpanded ? Icons.keyboard_arrow_down : Icons.keyboard_arrow_right,
                    size: 16, color: cs.outline),
                Icon(isExpanded ? Icons.folder_open : Icons.folder, size: 18, color: accent),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(node.name,
                      style: TextStyle(fontSize: 13, fontWeight: FontWeight.w500, color: cs.onSurface),
                      overflow: TextOverflow.ellipsis),
                ),
              ],
            ),
          ),
        ),
        if (isExpanded)
          ...node.children.map((c) => _buildDiffNode(c, cs, accent, depth + 1)),
      ],
    );
  }

  Widget _buildTreeDirTile(FileTreeStatNode node, ColorScheme cs, int depth) {
    if (!node.isDir) {
      // 文件：走紧凑行样式
      return Material(
        color: Colors.transparent,
        borderRadius: BorderRadius.circular(8),
        child: InkWell(
          borderRadius: BorderRadius.circular(8),
          child: Padding(
            padding: EdgeInsets.only(left: 12.0 + math.min(depth * 16.0, 96.0), right: 12, top: 10, bottom: 10),
            child: Row(
              children: [
                // 用留空保持与目录行对齐
                const SizedBox(width: 20),
                _fileIcon(node.ext),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(node.name, style: TextStyle(fontSize: 13, color: cs.onSurface), overflow: TextOverflow.ellipsis),
                ),
                Text(_formatBytes(node.size ?? 0), style: TextStyle(fontSize: 12, color: cs.outline)),
              ],
            ),
          ),
        ),
      );
    }

    // 目录：严格沿用收藏库 _TreeDirTile 样式
    final isExpanded = _expandedDirs.contains(node.path);
    final isSelected = _selectedTreePath == node.path;
    final hasChildren = node.children != null && node.children!.isNotEmpty;

    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        Padding(
          padding: const EdgeInsets.all(2),
          child: Material(
            color: Colors.transparent,
            borderRadius: BorderRadius.circular(8),
            child: InkWell(
              borderRadius: BorderRadius.circular(8),
              onTap: () {
                setState(() {
                  _selectedTreePath = node.path;
                  // 同时展开/折叠
                  if (hasChildren) {
                    if (isExpanded) {
                      _expandedDirs.remove(node.path);
                    } else {
                      _expandedDirs.add(node.path);
                    }
                  }
                });
              },
              child: AnimatedContainer(
                duration: const Duration(milliseconds: 150),
                padding: EdgeInsets.only(
                  left: 12.0 + depth * 16,
                  right: 12,
                  top: 10,
                  bottom: 10,
                ),
                decoration: BoxDecoration(
                  color: isSelected
                      ? cs.primary.withValues(alpha: Theme.of(context).brightness == Brightness.dark ? 0.15 : 0.1)
                      : Colors.transparent,
                  borderRadius: BorderRadius.circular(8),
                  border: isSelected
                      ? Border.all(color: cs.primary.withValues(alpha: 0.3))
                      : null,
                ),
                child: Row(
                  children: [
                    // 展开/折叠箭头
                    if (hasChildren)
                      Padding(
                        padding: const EdgeInsets.only(right: 4),
                        child: Icon(
                          isExpanded ? Icons.keyboard_arrow_down : Icons.keyboard_arrow_right,
                          size: 16,
                          color: cs.outline,
                        ),
                      )
                    else
                      const SizedBox(width: 20),
                    // 文件夹图标
                    Icon(
                      isExpanded ? Icons.folder_open : Icons.folder,
                      size: 18,
                      color: isSelected ? cs.primary : cs.outline,
                    ),
                    const SizedBox(width: 8),
                    // 名称
                    Expanded(
                      child: Tooltip(
                        message: node.name,
                        child: Text(
                          node.name,
                          style: TextStyle(fontSize: 13, color: cs.onSurface),
                          overflow: TextOverflow.ellipsis,
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
        // 展开的子节点
        if (isExpanded && hasChildren)
          ...node.children!.map((c) => _buildTreeDirTile(c, cs, depth + 1)),
      ],
    );
  }

  // ========== 空态 / 历史 ==========

  Widget _buildEmptyState(ColorScheme cs) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 48),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.analytics_outlined, size: 64, color: cs.outline.withValues(alpha: 0.5)),
            const SizedBox(height: 16),
            Text('输入目录路径并点击「开始统计」', style: TextStyle(fontSize: 15, color: cs.outline)),
            const SizedBox(height: 8),
            Text('系统将递归统计文件数量、大小与格式占比', style: TextStyle(fontSize: 12, color: cs.outline.withValues(alpha: 0.7))),
          ],
        ),
      ),
    );
  }

  Widget _buildHistorySection(ColorScheme cs) {
    final list = _historyList!;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.history, size: 18, color: cs.primary),
                const SizedBox(width: 8),
                Text('历史记录', style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: cs.onSurface)),
                const Spacer(),
                Text('${list.length} 项', style: TextStyle(fontSize: 12, color: cs.outline)),
              ],
            ),
            const SizedBox(height: 12),
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 300),
              child: ListView.separated(
                shrinkWrap: true,
                itemCount: list.length,
                separatorBuilder: (_, __) => Divider(height: 1, color: cs.outlineVariant),
                itemBuilder: (context, index) {
                  final fs = list[index];
                  final selected = _currentStats?.id == fs.id;
                  return ListTile(
                    dense: true,
                    selected: selected,
                    selectedTileColor: cs.primary.withValues(alpha: 0.05),
                    title: Text(
                      fs.dirPath,
                      style: TextStyle(fontSize: 13, fontWeight: selected ? FontWeight.w600 : FontWeight.w400, color: cs.onSurface),
                      overflow: TextOverflow.ellipsis,
                    ),
                    subtitle: Text(
                      '${_formatBytes(fs.totalBytes)} · ${fs.totalCount} 个文件',
                      style: TextStyle(fontSize: 11, color: cs.outline),
                    ),
                    trailing: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        IconButton(
                          icon: Icon(Icons.compare_arrows, size: 18, color: cs.primary.withValues(alpha: 0.7)),
                          onPressed: () => _runDiff(fs),
                          tooltip: '对比新增/删除文件',
                        ),
                        IconButton(
                          icon: Icon(Icons.delete_outline, size: 18, color: cs.error.withValues(alpha: 0.6)),
                          onPressed: () => _deleteHistory(fs.id),
                          tooltip: '删除记录',
                        ),
                      ],
                    ),
                    onTap: () => _viewHistory(fs),
                  );
                },
              ),
            ),
          ],
        ),
      ),
    );
  }
}

// ========== 全局辅助 ==========

/// diff 目录树节点（目录/文件叶子）。
class _DiffTreeNode {
  final String name; // 名称（目录名或文件名）
  final String path; // 相对路径（正斜杠）
  final bool isDir; // true=目录，false=文件叶子
  final List<_DiffTreeNode> children;

  _DiffTreeNode({required this.name, required this.path, required this.isDir})
      : children = [];
}

/// 统计徽章组件
class _StatBadge extends StatelessWidget {
  final IconData icon;
  final String label;
  final String value;
  final ColorScheme cs;

  const _StatBadge({required this.icon, required this.label, required this.value, required this.cs});

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Container(
          width: 44, height: 44,
          decoration: BoxDecoration(
            color: cs.primary.withValues(alpha: 0.1),
            borderRadius: BorderRadius.circular(12),
          ),
          child: Icon(icon, size: 22, color: cs.primary),
        ),
        const SizedBox(width: 12),
        Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(label, style: TextStyle(fontSize: 12, color: cs.outline)),
            const SizedBox(height: 2),
            Text(value, style: TextStyle(fontSize: 18, fontWeight: FontWeight.w700, color: cs.onSurface)),
          ],
        ),
      ],
    );
  }
}

/// 根据扩展名返回图标
Widget _fileIcon(String? ext) {
  final e = ext?.toLowerCase() ?? '';
  IconData icon = Icons.insert_drive_file;
  Color color = Colors.grey;
  if (e == '.jpg' || e == '.jpeg' || e == '.png' || e == '.gif' || e == '.bmp' || e == '.webp') {
    icon = Icons.image; color = Colors.blue;
  } else if (e == '.mp4' || e == '.mkv' || e == '.avi' || e == '.mov' || e == '.wmv' || e == '.flv' || e == '.webm') {
    icon = Icons.movie; color = Colors.purple;
  } else if (e == '.mp3' || e == '.wav' || e == '.flac' || e == '.aac' || e == '.ogg') {
    icon = Icons.audiotrack; color = Colors.orange;
  } else if (e == '.pdf') {
    icon = Icons.picture_as_pdf; color = Colors.red;
  } else if (e == '.zip' || e == '.rar' || e == '.7z' || e == '.tar' || e == '.gz') {
    icon = Icons.archive; color = Colors.brown;
  } else if (e == '.txt' || e == '.md' || e == '.csv') {
    icon = Icons.article; color = Colors.teal;
  } else if (e == '.html' || e == '.css' || e == '.js' || e == '.dart' || e == '.go' || e == '.py') {
    icon = Icons.code; color = Colors.indigo;
  } else if (e == '.exe' || e == '.dll' || e == '.msi') {
    icon = Icons.settings; color = Colors.blueGrey;
  }
  return Icon(icon, size: 18, color: color);
}

/// 格式化字节大小
String _formatBytes(int bytes) {
  if (bytes < 1024) return '$bytes B';
  if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)} KB';
  if (bytes < 1024 * 1024 * 1024) return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
  return '${(bytes / (1024 * 1024 * 1024)).toStringAsFixed(2)} GB';
}
