// resizable_split.dart：可拖拽水平分栏，分隔线紧凑可见。
// 代码注释使用中文
import 'dart:math' as math;
import 'package:flutter/material.dart';

/// 可拖拽调整宽度的水平分栏。
///
/// 分隔线为紧凑 1px 竖条（全高度），4px 宽命中区。
/// [minLeftWidth]/[maxLeftWidth]/[minRightWidth] 限制面板宽度范围。
/// 极窄窗口按比例压缩，不会出现 RenderFlex overflow。
class ResizableSplit extends StatefulWidget {
  final Widget left;
  final Widget right;
  final double initialRatio;
  final double minLeftWidth;
  final double maxLeftWidth;
  final double minRightWidth;
  final double hitAreaWidth;

  const ResizableSplit({
    super.key,
    required this.left,
    required this.right,
    this.initialRatio = 0.3,
    this.minLeftWidth = 160,
    this.maxLeftWidth = 600,
    this.minRightWidth = 120,
    this.hitAreaWidth = 4,
  });

  @override
  State<ResizableSplit> createState() => _ResizableSplitState();
}

class _ResizableSplitState extends State<ResizableSplit> {
  double _ratio = 0.3;
  bool _draggable = true;

  @override
  void initState() {
    super.initState();
    _ratio = widget.initialRatio;
  }

  @override
  void didUpdateWidget(ResizableSplit old) {
    super.didUpdateWidget(old);
    // 若 initialRatio 变化则同步
    if (widget.initialRatio != old.initialRatio) {
      _ratio = widget.initialRatio.clamp(0.0, 1.0);
    }
    // 规范化比例，避免超出合法范围
    if (_ratio < 0.0) _ratio = 0.0;
    if (_ratio > 1.0) _ratio = 1.0;
  }

  void _onDrag(DragUpdateDetails d, double totalWidth) {
    final available = math.max(0.0, totalWidth - widget.hitAreaWidth);
    if (available <= 0) return;

    setState(() {
      final raw = _ratio + d.delta.dx / available;
      final leftW = raw * available;

      // clamp 到合法范围
      final maxAllowedLeft = math.min(
        widget.maxLeftWidth,
        available - widget.minRightWidth,
      );
      final clamped = leftW.clamp(widget.minLeftWidth, maxAllowedLeft);
      _ratio = clamped / available;
    });
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;

    return LayoutBuilder(
      builder: (context, constraints) {
        final totalW = constraints.maxWidth;
        final available = math.max(0.0, totalW - widget.hitAreaWidth);

        double leftW, rightW;
        final canMeet =
            available >= widget.minLeftWidth + widget.minRightWidth;

        if (canMeet) {
          // 正常情况：左侧在 [minLeft, max(minLeft, available-minRight)] 范围内
          final maxAllowedLeft = math.min(
            widget.maxLeftWidth,
            available - widget.minRightWidth,
          );
          leftW = (available * _ratio).clamp(
            widget.minLeftWidth,
            maxAllowedLeft,
          );
          rightW = available - leftW; // 右侧不独立 clamp，确保和 = available
          _draggable = true;
        } else {
          // 极窄窗口：按比例压缩
          final ratioLeft =
              widget.minLeftWidth /
              (widget.minLeftWidth + widget.minRightWidth);
          leftW = available * ratioLeft;
          rightW = available - leftW;
          _draggable = false;
        }

        return Row(
          children: [
            SizedBox(width: leftW, child: ClipRect(child: widget.left)),
            _buildDivider(cs, totalW),
            SizedBox(width: rightW, child: ClipRect(child: widget.right)),
          ],
        );
      },
    );
  }

  Widget _buildDivider(ColorScheme cs, double totalWidth) {
    final cursor = _draggable
        ? SystemMouseCursors.resizeColumn
        : SystemMouseCursors.basic;
    return MouseRegion(
      cursor: cursor,
      child: GestureDetector(
        onHorizontalDragUpdate:
            _draggable ? (d) => _onDrag(d, totalWidth) : null,
        child: Container(
          width: widget.hitAreaWidth,
          color: Colors.transparent,
          alignment: Alignment.center,
          child: Container(
            width: 1,
            height: double.infinity,
            decoration: BoxDecoration(color: cs.outlineVariant),
          ),
        ),
      ),
    );
  }
}
