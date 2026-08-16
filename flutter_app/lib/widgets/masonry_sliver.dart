// masonry_sliver.dart：自实现瀑布流 sliver。
// flutter_staggered_grid_view 的 SliverMasonryGrid 在内容追加时会通过
// SliverGeometry.scrollOffsetCorrection 修正滚动位置（可能直接减到 0），
// 表现为"拖动滚动条停止后回到起点"。本实现利用 item 高度可预测
// （高度 = 列宽 × heightFactor + fixedExtraHeight）手写瀑布流布局，
// 滚动行为与 SliverList 一致，追加内容不影响滚动位置。
import 'dart:math' as math;

import 'package:flutter/rendering.dart';
import 'package:flutter/widgets.dart';

/// 瀑布流 sliver：按原始宽高比贪心分列，item 高度可精确计算。
///
/// [heightFactor] 返回第 index 个 item 的高度系数（item 高度 = 列宽 × factor
/// + [fixedExtraHeight]），必须与 [itemBuilder] 构建出的实际高度一致。
/// [rebuildToken] 变化时强制重建 child 委托（组/页数据整体替换时使用）。
class MasonrySliver extends StatefulWidget {
  const MasonrySliver({
    super.key,
    required this.childCount,
    required this.itemBuilder,
    required this.crossAxisCount,
    required this.crossAxisSpacing,
    required this.mainAxisSpacing,
    required this.heightFactor,
    this.fixedExtraHeight = 0,
    this.rebuildToken,
  }) : assert(crossAxisCount > 0);

  final int childCount;
  final IndexedWidgetBuilder itemBuilder;
  final int crossAxisCount;
  final double crossAxisSpacing;
  final double mainAxisSpacing;
  final double Function(int index) heightFactor;
  final double fixedExtraHeight;
  final Object? rebuildToken;

  @override
  State<MasonrySliver> createState() => _MasonrySliverState();
}

class _MasonrySliverState extends State<MasonrySliver> {
  late SliverChildBuilderDelegate _delegate = _buildDelegate();

  SliverChildBuilderDelegate _buildDelegate() => SliverChildBuilderDelegate(
    widget.itemBuilder,
    childCount: widget.childCount,
  );

  @override
  void didUpdateWidget(MasonrySliver old) {
    super.didUpdateWidget(old);
    // itemBuilder 通常是每次 build 的新闭包，但其捕获的组/页数据一致；
    // 仅当数据整体替换（rebuildToken）或数量变化时才重建委托。
    if (old.rebuildToken != widget.rebuildToken ||
        old.childCount != widget.childCount) {
      _delegate = _buildDelegate();
    }
  }

  @override
  Widget build(BuildContext context) {
    return _MasonrySliverWidget(
      delegate: _delegate,
      crossAxisCount: widget.crossAxisCount,
      crossAxisSpacing: widget.crossAxisSpacing,
      mainAxisSpacing: widget.mainAxisSpacing,
      heightFactor: widget.heightFactor,
      fixedExtraHeight: widget.fixedExtraHeight,
    );
  }
}

class _MasonrySliverWidget extends SliverMultiBoxAdaptorWidget {
  const _MasonrySliverWidget({
    required super.delegate,
    required this.crossAxisCount,
    required this.crossAxisSpacing,
    required this.mainAxisSpacing,
    required this.heightFactor,
    required this.fixedExtraHeight,
  });

  final int crossAxisCount;
  final double crossAxisSpacing;
  final double mainAxisSpacing;
  final double Function(int index) heightFactor;
  final double fixedExtraHeight;

  @override
  RenderSliverMultiBoxAdaptor createRenderObject(BuildContext context) {
    // 与 SliverList 一致：以自身 Element 作为 childManager
    final element = context as SliverMultiBoxAdaptorElement;
    return _RenderSliverMasonry(
      childManager: element,
      crossAxisCount: crossAxisCount,
      crossAxisSpacing: crossAxisSpacing,
      mainAxisSpacing: mainAxisSpacing,
      heightFactor: heightFactor,
      fixedExtraHeight: fixedExtraHeight,
    );
  }

  @override
  void updateRenderObject(
    BuildContext context,
    _RenderSliverMasonry renderObject,
  ) {
    renderObject
      ..crossAxisCount = crossAxisCount
      ..crossAxisSpacing = crossAxisSpacing
      ..mainAxisSpacing = mainAxisSpacing
      ..heightFactor = heightFactor
      ..fixedExtraHeight = fixedExtraHeight;
  }
}

class _MasonryParentData extends SliverMultiBoxAdaptorParentData {
  int crossAxisIndex = 0;
}

class _RenderSliverMasonry extends RenderSliverMultiBoxAdaptor {
  _RenderSliverMasonry({
    required super.childManager,
    required int crossAxisCount,
    required double crossAxisSpacing,
    required double mainAxisSpacing,
    required double Function(int index) heightFactor,
    required double fixedExtraHeight,
  }) : _crossAxisCount = crossAxisCount,
       _crossAxisSpacing = crossAxisSpacing,
       _mainAxisSpacing = mainAxisSpacing,
       _heightFactor = heightFactor,
       _fixedExtraHeight = fixedExtraHeight;

  int _crossAxisCount;
  double _crossAxisSpacing;
  double _mainAxisSpacing;
  double Function(int index) _heightFactor;
  double _fixedExtraHeight;

  int get crossAxisCount => _crossAxisCount;
  set crossAxisCount(int value) {
    if (_crossAxisCount == value) return;
    _crossAxisCount = value;
    _planDirty = true;
    markNeedsLayout();
  }

  double get crossAxisSpacing => _crossAxisSpacing;
  set crossAxisSpacing(double value) {
    if (_crossAxisSpacing == value) return;
    _crossAxisSpacing = value;
    _planDirty = true;
    markNeedsLayout();
  }

  double get mainAxisSpacing => _mainAxisSpacing;
  set mainAxisSpacing(double value) {
    if (_mainAxisSpacing == value) return;
    _mainAxisSpacing = value;
    _planDirty = true;
    markNeedsLayout();
  }

  double Function(int index) get heightFactor => _heightFactor;
  set heightFactor(double Function(int index) value) {
    _heightFactor = value;
    _planDirty = true;
    markNeedsLayout();
  }

  double get fixedExtraHeight => _fixedExtraHeight;
  set fixedExtraHeight(double value) {
    if (_fixedExtraHeight == value) return;
    _fixedExtraHeight = value;
    _planDirty = true;
    markNeedsLayout();
  }

  double _stride = 0;
  double _colWidth = 0;
  List<double> _heights = const [];
  List<int> _columnOf = const [];
  List<double> _offsetOf = const [];
  double _totalExtent = 0;
  bool _planDirty = true;
  double _planCrossExtent = -1;
  int _planColumns = -1;
  double _planSpacing = -1;

  @override
  void setupParentData(RenderObject child) {
    if (child.parentData is! _MasonryParentData) {
      child.parentData = _MasonryParentData();
    }
  }

  /// 重算分列计划（items 数量/列数/列宽/间距变化时）。
  /// 缓存键覆盖 crossAxisExtent（窗口缩放）、列数、间距与数量，
  /// 避免 resize 后沿用旧列宽导致布局错位。
  void _ensurePlan() {
    final count = childManager.childCount;
    final crossExtent = constraints.crossAxisExtent;
    if (!_planDirty &&
        count == _heights.length &&
        crossExtent == _planCrossExtent &&
        crossAxisCount == _planColumns &&
        mainAxisSpacing == _planSpacing) {
      return;
    }
    _stride = (crossExtent + crossAxisSpacing) / crossAxisCount;
    _colWidth = _stride - crossAxisSpacing;
    _heights = List<double>.generate(
      count,
      (i) => _colWidth * _heightFactor(i) + _fixedExtraHeight,
    );
    final columnHeights = List<double>.filled(crossAxisCount, 0);
    final columnOf = List<int>.filled(count, 0);
    final offsetOf = List<double>.filled(count, 0);
    for (var i = 0; i < count; i++) {
      var best = 0;
      for (var j = 1; j < crossAxisCount; j++) {
        if (columnHeights[j] < columnHeights[best]) best = j;
      }
      columnOf[i] = best;
      offsetOf[i] = columnHeights[best];
      columnHeights[best] += _heights[i] + mainAxisSpacing;
    }
    _columnOf = columnOf;
    _offsetOf = offsetOf;
    _totalExtent = columnHeights.reduce(math.max);
    _planDirty = false;
    _planCrossExtent = crossExtent;
    _planColumns = crossAxisCount;
    _planSpacing = mainAxisSpacing;
  }

  @override
  double childCrossAxisPosition(RenderBox child) {
    return (child.parentData as _MasonryParentData).crossAxisIndex * _stride;
  }

  @override
  void performLayout() {
    childManager.didStartLayout();
    childManager.setDidUnderflow(false);
    _ensurePlan();

    final count = childManager.childCount;
    if (count == 0) {
      geometry = SliverGeometry.zero;
      childManager.didFinishLayout();
      return;
    }

    final scrollOffset = constraints.scrollOffset;
    final targetEnd = scrollOffset + constraints.remainingCacheExtent;
    final childConstraints = constraints.asBoxConstraints(
      crossAxisExtent: _colWidth,
    );

    // 找出可见范围 [first, last]（按全局主轴偏移，跨列）。
    var first = count;
    var last = -1;
    for (var i = 0; i < count; i++) {
      final start = _offsetOf[i];
      final end = start + _heights[i];
      if (end > scrollOffset && start < targetEnd) {
        if (i < first) first = i;
        if (i > last) last = i;
      }
    }
    if (first > last) {
      // 视口外（如滚动到末尾）：保留最后一项以提供末端几何信息。
      first = count - 1;
      last = count - 1;
    }

    // 回收 [first, last] 之外的 children（按实际存在的 children 计数）。
    if (firstChild != null) {
      final leadingGarbage = calculateLeadingGarbage(firstIndex: first);
      final trailingGarbage = calculateTrailingGarbage(lastIndex: last);
      if (leadingGarbage > 0 || trailingGarbage > 0) {
        collectGarbage(leadingGarbage, trailingGarbage);
      }
    }

    // 确保 firstChild 存在且 index == first。
    if (firstChild == null) {
      addInitialChild(index: first, layoutOffset: _offsetOf[first]);
    } else if (indexOf(firstChild!) > first) {
      while (firstChild != null && indexOf(firstChild!) > first) {
        final leading = insertAndLayoutLeadingChild(
          childConstraints,
          parentUsesSize: true,
        );
        if (leading == null) break;
        _applyParentData(leading, indexOf(leading));
      }
    }

    // 布局 first..last（firstChild 可能已由 leading 插入时布局过，统一再布局）。
    final firstBox = firstChild;
    if (firstBox == null) {
      childManager.setDidUnderflow(true);
      childManager.didFinishLayout();
      return;
    }
    var child = firstBox;
    var index = indexOf(child);
    if (index == first) {
      child.layout(childConstraints, parentUsesSize: true);
      _applyParentData(child, index);
    }
    while (index < last) {
      final next = childAfter(child);
      if (next == null || indexOf(next) != index + 1) {
        final inserted = insertAndLayoutChild(
          childConstraints,
          after: child,
          parentUsesSize: true,
        );
        if (inserted == null) break;
        child = inserted;
      } else {
        next.layout(childConstraints, parentUsesSize: true);
        child = next;
      }
      index = indexOf(child);
      _applyParentData(child, index);
    }

    final leadingScrollOffset = _offsetOf[first];
    final endScrollOffset = _offsetOf[last] + _heights[last];
    final paintExtent = calculatePaintOffset(
      constraints,
      from: leadingScrollOffset,
      to: endScrollOffset,
    );
    final cacheExtent = calculateCacheOffset(
      constraints,
      from: leadingScrollOffset,
      to: endScrollOffset,
    );
    final reachedEnd = last == count - 1;
    geometry = SliverGeometry(
      scrollExtent: _totalExtent,
      paintExtent: paintExtent,
      cacheExtent: cacheExtent,
      maxPaintExtent: _totalExtent,
      hasVisualOverflow:
          endScrollOffset > targetEnd || constraints.scrollOffset > 0.0,
    );
    if (reachedEnd && endScrollOffset <= targetEnd) {
      childManager.setDidUnderflow(true);
    }
    childManager.didFinishLayout();
  }

  void _applyParentData(RenderBox child, int index) {
    final parentData = child.parentData as _MasonryParentData;
    parentData.crossAxisIndex = _columnOf[index];
    parentData.layoutOffset = _offsetOf[index];
  }
}
