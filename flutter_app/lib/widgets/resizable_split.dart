// resizable_split.dart：可拖拽水平分栏，分隔线紧凑可见。
// 代码注释使用中文
import 'package:flutter/material.dart';

/// 可拖拽调整宽度的水平分栏。
///
/// 分隔线为紧凑的 2px 竖条（全高度），4px 宽命中区。
/// [minLeftWidth]/[maxLeftWidth]/[minRightWidth] 限制面板宽度范围。
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

  @override
  void initState() {
    super.initState();
    _ratio = widget.initialRatio;
  }

  void _onDrag(DragUpdateDetails d, double totalWidth) {
    setState(() {
      final raw = _ratio + d.delta.dx / totalWidth;
      final leftW = raw * totalWidth;
      final minLeft = widget.minLeftWidth;
      final maxLeft = widget.maxLeftWidth;

      if (leftW < minLeft) {
        _ratio = minLeft / totalWidth;
      } else if (leftW > maxLeft) {
        _ratio = maxLeft / totalWidth;
      } else {
        final maxLeftByRight = totalWidth - widget.minRightWidth;
        if (leftW > maxLeftByRight) {
          _ratio = maxLeftByRight / totalWidth;
        } else {
          _ratio = raw;
        }
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;

    return LayoutBuilder(
      builder: (context, constraints) {
        final totalW = constraints.maxWidth;
        final leftW = (totalW * _ratio)
            .clamp(widget.minLeftWidth, widget.maxLeftWidth)
            .toDouble();
        final rightW = (totalW - leftW - widget.hitAreaWidth)
            .clamp(widget.minRightWidth, double.infinity)
            .toDouble();

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
    return MouseRegion(
      cursor: SystemMouseCursors.resizeColumn,
      child: GestureDetector(
        onHorizontalDragUpdate: (d) => _onDrag(d, totalWidth),
        child: Container(
          width: widget.hitAreaWidth,
          color: Colors.transparent,
          alignment: Alignment.center,
          child: Container(
            width: 1,
            height: double.infinity,
            decoration: BoxDecoration(
              color: cs.outlineVariant,
            ),
          ),
        ),
      ),
    );
  }
}
