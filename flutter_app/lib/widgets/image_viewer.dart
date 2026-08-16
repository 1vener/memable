// image_viewer.dart：图片专用查看器（视频仍由 media_viewer.dart 的 MediaViewer 负责）。
// 全屏黑底、不显示标题/文件名：左侧本目录全部图片正方形缩略图条、中央图片区
// （缩放/平移/旋转）、右上角鸟瞰图（放大超出屏幕时显示当前视口位置）、底部功能栏
// （鼠标移到正下方才显示）、右侧详情面板；键盘 ↑↓ 缩放、←→ 上一张/下一张、Esc 关闭。
// 代码注释使用中文。
import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:media_kit_video/media_kit_video.dart'
    show defaultEnterNativeFullscreen, defaultExitNativeFullscreen;

import '../models/models.dart';
import '../services/api_service.dart';
import '../utils/time_fmt.dart';
import 'native_fullscreen.dart';

/// 图片专用查看器。
class ImageViewer extends StatefulWidget {
  final List<Media> medias;
  final int initialIndex;
  final ApiService api;
  final bool preserveQueue;

  const ImageViewer({
    super.key,
    required this.medias,
    required this.initialIndex,
    required this.api,
    this.preserveQueue = false,
  });

  @override
  State<ImageViewer> createState() => _ImageViewerState();
}

class _ImageViewerState extends State<ImageViewer> {
  static const double _maxScale = 10;
  static const double _minScale = 1;
  static const double _barZoneHeight = 76; // 底部悬停显示功能栏的区域高度
  static const double _thumbWidth = 92; // 左侧缩略图条宽度
  static const double _thumbItemExtent = 92; // 缩略图条目固定高度（正方形）
  static const double _maxMinimapW = 200; // 鸟瞰图最大宽
  static const double _maxMinimapH = 150; // 鸟瞰图最大高

  late List<Media> _queue; // 当前目录的全部图片
  late int _index;
  bool _loading = true;

  final TransformationController _transform = TransformationController();
  int _rotation = 0; // 0/90/180/270
  bool _barVisible = false;
  bool _mouseInBarZone = false;
  bool _detailsOpen = false;
  bool _thumbVisible = true; // 左侧缩略图条显隐
  bool _fullscreen = false;
  Timer? _barTimer;
  final FocusNode _focusNode = FocusNode();
  final ScrollController _thumbScroll = ScrollController();

  Size _viewportSize = Size.zero; // 图片区尺寸（LayoutBuilder 缓存）
  double _screenHeight = 0;

  Media? get _media => _queue.isEmpty ? null : _queue[_index];

  @override
  void initState() {
    super.initState();
    _queue = widget.medias.where((m) => m.kind == 'image').toList();
    final requested = widget.initialIndex;
    final targetId =
        requested >= 0 && requested < widget.medias.length
            ? widget.medias[requested].id
            : null;
    _index = targetId == null ? 0 : _queue.indexWhere((m) => m.id == targetId);
    if (_index < 0) _index = 0;
    _transform.addListener(_onTransformChanged);
    _showBarBriefly();
    if (widget.preserveQueue) {
      _loading = false;
      WidgetsBinding.instance.addPostFrameCallback(
        (_) => _scrollThumbTo(_index),
      );
    } else {
      _loadDirectoryImages();
    }
  }

  @override
  void dispose() {
    _barTimer?.cancel();
    _transform.removeListener(_onTransformChanged);
    _transform.dispose();
    _thumbScroll.dispose();
    _focusNode.dispose();
    if (_fullscreen) {
      // 关闭时若处于全屏先退出（异步执行，不阻塞 dispose）
      unawaited(_exitFullscreen());
    }
    super.dispose();
  }

  // ===== 目录图片加载 =====

  Future<void> _loadDirectoryImages() async {
    final current = _media;
    if (current == null) {
      if (mounted) setState(() => _loading = false);
      return;
    }
    try {
      final files = await widget.api.getFiles(
        current.libraryId,
        path: _parentDirectory(current.relativePath),
      );
      if (!mounted) return;
      final images = files.where((m) => m.kind == 'image').toList();
      final idx = images.indexWhere((m) => m.id == current.id);
      if (idx < 0) {
        if (mounted) setState(() => _loading = false);
        return;
      }
      setState(() {
        _queue = images;
        _index = idx;
        _loading = false;
      });
      _scrollThumbTo(_index);
    } catch (_) {
      if (mounted) setState(() => _loading = false);
    }
  }

  // ===== 导航 / 缩放 / 旋转 =====

  Future<void> _goTo(int next) async {
    if (_queue.isEmpty) return;
    final n = (next % _queue.length + _queue.length) % _queue.length;
    if (n == _index) return;
    setState(() {
      _index = n;
      _rotation = 0; // 切换图片：缩放与旋转全部重置
      _transform.value = Matrix4.identity();
    });
    _scrollThumbTo(n);
  }

  void _scrollThumbTo(int index) {
    if (!_thumbScroll.hasClients) return;
    _thumbScroll.animateTo(
      math.max(0.0, index * _thumbItemExtent - 120),
      duration: const Duration(milliseconds: 150),
      curve: Curves.easeOut,
    );
  }

  /// 绕视口中心缩放（按钮/键盘共用）。
  void _zoomBy(double factor) {
    final size = _viewportSize;
    if (size.width <= 0 || size.height <= 0) return;
    final current = _transform.value.getMaxScaleOnAxis();
    final target = (current * factor).clamp(_minScale, _maxScale);
    if ((target - current).abs() < 0.001) return;
    final f = target / current;
    final center = Offset(size.width / 2, size.height / 2);
    final matrix =
        Matrix4.identity()
          ..translate(center.dx, center.dy)
          ..scale(f)
          ..translate(-center.dx, -center.dy);
    _transform.value = matrix * _transform.value;
  }

  void _rotate() {
    setState(() => _rotation = (_rotation + 90) % 360);
  }

  /// 一键复原缩放：回到 100% 且居中（旋转保持不变）。
  void _resetZoom() {
    _transform.value = Matrix4.identity();
  }

  void _onTransformChanged() {
    _clampTransform();
    setState(() {});
  }

  /// 按鸟瞰图上的拖动位移平移主视图。
  /// [delta] 为鸟瞰图像素位移，[kx]/[ky] 为图片坐标到鸟瞰图像素的映射比例；
  /// 视口框向右拖动 Δ 像素，等价于把可见区域向图片右侧移动 Δ/kx，
  /// 即对变换矩阵右乘一个 (-Δ/kx, -Δ/ky) 的平移；边界由 _clampTransform 统一钳制。
  void _panByMinimapDelta(Offset delta, double kx, double ky) {
    if (kx <= 0 || ky <= 0) return;
    final t = Matrix4.identity()..translate(-delta.dx / kx, -delta.dy / ky);
    _transform.value = _transform.value * t;
  }

  /// 限制变换矩阵：可见区域（子坐标系中的视口包围盒）不得超出**图片区域**
  /// （图片在视口大小的子区域中由 BoxFit.contain 居中，四周可能有留边）。
  /// 可见区域小于图片时居中；大于图片（整图可见）的维度不做约束。
  /// InteractiveViewer 内置钳制只限制视口窗口不超出子区域，会允许图片边缘
  /// 滑出屏幕露出黑边，因此这里在矩阵变化时统一钳制到图片边界
  /// （覆盖手势拖动、惯性动画、缩放、鸟瞰图拖动所有路径）。
  void _clampTransform() {
    _transform.value = clampMatrixToImage(
      _transform.value,
      _viewportSize,
      _displaySize(_viewportSize.width, _viewportSize.height),
    );
  }

  // ===== 全屏 =====

  Future<void> _toggleFullscreen() async {
    if (_fullscreen) {
      await _exitFullscreen();
    } else {
      await _enterFullscreen();
    }
    if (mounted) setState(() => _fullscreen = !_fullscreen);
  }

  Future<void> _enterFullscreen() async {
    try {
      await defaultEnterNativeFullscreen();
    } catch (_) {}
  }

  Future<void> _exitFullscreen() async {
    try {
      if (kIsWeb) {
        await guardedExitNativeFullscreen();
      } else {
        await defaultExitNativeFullscreen();
      }
    } catch (_) {}
  }

  // ===== 键盘 =====

  KeyEventResult _onKey(FocusNode node, KeyEvent event) {
    if (event is! KeyDownEvent) return KeyEventResult.ignored;
    switch (event.logicalKey) {
      case LogicalKeyboardKey.escape:
        _close();
        return KeyEventResult.handled;
      case LogicalKeyboardKey.arrowUp:
        _zoomBy(1.25);
        return KeyEventResult.handled;
      case LogicalKeyboardKey.arrowDown:
        _zoomBy(0.8);
        return KeyEventResult.handled;
      case LogicalKeyboardKey.arrowLeft:
        _goTo(_index - 1);
        return KeyEventResult.handled;
      case LogicalKeyboardKey.arrowRight:
        _goTo(_index + 1);
        return KeyEventResult.handled;
    }
    return KeyEventResult.ignored;
  }

  void _close() {
    if (_fullscreen) unawaited(_exitFullscreen());
    Navigator.of(context).maybePop();
  }

  // ===== 功能栏显示控制 =====

  void _showBarBriefly() {
    _barTimer?.cancel();
    setState(() => _barVisible = true);
    _barTimer = Timer(const Duration(seconds: 2), () {
      if (mounted && !_mouseInBarZone) {
        setState(() => _barVisible = false);
      }
    });
  }

  void _onHover(PointerHoverEvent e) {
    final inZone = e.localPosition.dy >= _screenHeight - _barZoneHeight;
    if (inZone != _mouseInBarZone) {
      _mouseInBarZone = inZone;
      setState(() => _barVisible = inZone);
    }
  }

  // ===== 布局 =====

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: Colors.black,
      body: Focus(
        focusNode: _focusNode,
        autofocus: true,
        onKeyEvent: _onKey,
        child: LayoutBuilder(
          builder: (context, constraints) {
            _screenHeight = constraints.maxHeight;
            return MouseRegion(
              onHover: _onHover,
              child: Stack(
                children: [
                  Row(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      if (_thumbVisible) _buildThumbStrip(),
                      Expanded(child: _buildImageViewport()),
                    ],
                  ),
                  if (_detailsOpen) _buildDetailsPanel(),
                  if (_barVisible) _buildBar(),
                ],
              ),
            );
          },
        ),
      ),
    );
  }

  // ===== 左侧缩略图条 =====

  Widget _buildThumbStrip() {
    return Container(
      width: _thumbWidth,
      color: const Color(0xFF101010),
      child:
          _loading
              ? const Center(
                child: SizedBox(
                  width: 18,
                  height: 18,
                  child: CircularProgressIndicator(
                    strokeWidth: 2,
                    color: Colors.white38,
                  ),
                ),
              )
              : _queue.isEmpty
              ? const SizedBox.shrink()
              : ListView.builder(
                controller: _thumbScroll,
                itemExtent: _thumbItemExtent,
                itemCount: _queue.length,
                itemBuilder: (_, i) => _buildThumbItem(_queue[i], i),
              ),
    );
  }

  Widget _buildThumbItem(Media m, int i) {
    final active = i == _index;
    final thumb =
        m.thumbnailPath == null
            ? null
            : widget.api.thumbnailUrl('image', m.thumbnailPath!);
    return Padding(
      padding: const EdgeInsets.all(5),
      child: AspectRatio(
        aspectRatio: 1,
        child: GestureDetector(
          onTap: () => _goTo(i),
          child: Container(
            foregroundDecoration: BoxDecoration(
              borderRadius: BorderRadius.circular(5),
              border: Border.all(
                color: active ? const Color(0xFFFF0033) : Colors.transparent,
                width: 2,
              ),
            ),
            child: ClipRRect(
              borderRadius: BorderRadius.circular(4),
              child:
                  thumb == null
                      ? _thumbPlaceholder()
                      : Image.network(
                        thumb,
                        fit: BoxFit.cover,
                        errorBuilder: (_, __, ___) => _thumbPlaceholder(),
                      ),
            ),
          ),
        ),
      ),
    );
  }

  Widget _thumbPlaceholder() {
    return Container(
      color: const Color(0xFF303030),
      child: const Icon(Icons.image_outlined, color: Colors.white24, size: 20),
    );
  }

  // ===== 中央图片区 =====

  Widget _buildImageViewport() {
    return LayoutBuilder(
      builder: (context, constraints) {
        _viewportSize = constraints.biggest;
        final vw = constraints.maxWidth;
        final vh = constraints.maxHeight;
        final m = _media;
        if (m == null) {
          return const Center(
            child: Text(
              '没有可查看的图片',
              style: TextStyle(color: Colors.white38, fontSize: 14),
            ),
          );
        }
        final ds = _displaySize(vw, vh);
        return Stack(
          children: [
            Positioned.fill(
              child: InteractiveViewer(
                transformationController: _transform,
                minScale: _minScale,
                maxScale: _maxScale,
                child: SizedBox(
                  width: ds.width,
                  height: ds.height,
                  child: _buildRotatedImage(ds),
                ),
              ),
            ),
            if (!_detailsOpen) _buildMinimap(vw, vh, ds),
          ],
        );
      },
    );
  }

  /// 图片在当前视口内的显示尺寸（BoxFit.contain，旋转后宽高互换）。
  Size _displaySize(double vw, double vh) {
    final m = _media;
    if (m == null ||
        m.width == null ||
        m.height == null ||
        m.width! <= 0 ||
        m.height! <= 0) {
      return Size(vw, vh);
    }
    final imageAr = m.width! / m.height!;
    final viewAr = vw / vh;
    double w, h;
    if (imageAr > viewAr) {
      w = vw;
      h = vw / imageAr;
    } else {
      h = vh;
      w = vh * imageAr;
    }
    final odd = (_rotation ~/ 90).isOdd;
    return odd ? Size(h, w) : Size(w, h);
  }

  Widget _buildRotatedImage(Size ds) {
    final turns = _rotation ~/ 90;
    Widget img = Image.network(
      widget.api.mediaFileUrl(_media!.id),
      fit: BoxFit.contain,
      loadingBuilder: (_, child, progress) {
        if (progress == null) return child;
        return const Center(
          child: CircularProgressIndicator(
            strokeWidth: 2,
            color: Colors.white38,
          ),
        );
      },
      errorBuilder:
          (_, __, ___) => const Center(
            child: Text('图片加载失败', style: TextStyle(color: Colors.white38)),
          ),
    );
    if (turns == 0) return img;
    // Transform.rotate 不改变布局尺寸（SizedBox 已是旋转后的尺寸），鸟瞰图映射保持一致
    return Transform.rotate(angle: turns * math.pi / 2, child: img);
  }

  // ===== 鸟瞰图 =====

  Widget _buildMinimap(double vw, double vh, Size ds) {
    final scale = _transform.value.getMaxScaleOnAxis();
    if (scale <= 1.05 || ds.width <= 0 || ds.height <= 0) {
      return const SizedBox.shrink();
    }
    // 按图纵横比适配鸟瞰图尺寸
    final ar = ds.width / ds.height;
    double mw, mh;
    if (ar >= _maxMinimapW / _maxMinimapH) {
      mw = _maxMinimapW;
      mh = mw / ar;
    } else {
      mh = _maxMinimapH;
      mw = mh * ar;
    }
    // 视口四角反解到子坐标系（子区域与视口同尺寸），取包围盒即当前可见区域；
    // 图片在子区域中居中（BoxFit.contain，四周可能有留边），
    // 扣除留边偏移后才是图片坐标，再映射到鸟瞰图
    final inv = Matrix4.inverted(_transform.value);
    final corners = [
      MatrixUtils.transformPoint(inv, Offset.zero),
      MatrixUtils.transformPoint(inv, Offset(vw, 0)),
      MatrixUtils.transformPoint(inv, Offset(0, vh)),
      MatrixUtils.transformPoint(inv, Offset(vw, vh)),
    ];
    var minX = corners.first.dx, maxX = corners.first.dx;
    var minY = corners.first.dy, maxY = corners.first.dy;
    for (final c in corners.skip(1)) {
      minX = math.min(minX, c.dx);
      maxX = math.max(maxX, c.dx);
      minY = math.min(minY, c.dy);
      maxY = math.max(maxY, c.dy);
    }
    final offsetX = (vw - ds.width) / 2;
    final offsetY = (vh - ds.height) / 2;
    final kx = mw / ds.width, ky = mh / ds.height;
    final imgLeft = math.max(0.0, minX - offsetX);
    final imgTop = math.max(0.0, minY - offsetY);
    final imgRight = math.min(ds.width, maxX - offsetX);
    final imgBottom = math.min(ds.height, maxY - offsetY);
    final rl = imgLeft * kx;
    final rt = imgTop * ky;
    final rw = math.max(0.0, (imgRight - imgLeft) * kx);
    final rh = math.max(0.0, (imgBottom - imgTop) * ky);
    return Positioned(
      top: 12,
      right: 12,
      child: Container(
        padding: const EdgeInsets.all(6),
        decoration: BoxDecoration(
          color: Colors.black.withValues(alpha: 0.55),
          borderRadius: BorderRadius.circular(8),
          border: Border.all(color: Colors.white24),
        ),
        // 整块鸟瞰图可拖动：按住拖动会平移主视图，视口框跟随
        child: MouseRegion(
          cursor: SystemMouseCursors.move,
          child: GestureDetector(
            onPanUpdate: (details) => _panByMinimapDelta(details.delta, kx, ky),
            child: Stack(
              children: [
                SizedBox(
                  width: mw,
                  height: mh,
                  child: ClipRRect(
                    borderRadius: BorderRadius.circular(4),
                    child: Image.network(
                      widget.api.mediaFileUrl(_media!.id),
                      fit: BoxFit.fill,
                      errorBuilder:
                          (_, __, ___) =>
                              Container(color: const Color(0xFF222222)),
                    ),
                  ),
                ),
                Positioned(
                  left: rl,
                  top: rt,
                  width: rw,
                  height: rh,
                  child: Container(
                    decoration: BoxDecoration(
                      border: Border.all(
                        color: const Color(0xFFFF0033),
                        width: 1.5,
                      ),
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
  // ===== 底部功能栏 =====

  Widget _buildBar() {
    return Positioned(
      left: 0,
      right: 0,
      bottom: 10,
      child: Center(
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
          decoration: BoxDecoration(
            color: Colors.black.withValues(alpha: 0.6),
            borderRadius: BorderRadius.circular(24),
            border: Border.all(color: Colors.white12),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              _barButton(
                Icons.chevron_left,
                '上一张 (←)',
                () => _goTo(_index - 1),
              ),
              _barButton(Icons.zoom_out, '缩小 (↓)', () => _zoomBy(0.8)),
              _barButton(Icons.zoom_in, '放大 (↑)', () => _zoomBy(1.25)),
              _barButton(
                Icons.fit_screen,
                '复原缩放',
                _transform.value.getMaxScaleOnAxis() > 1.01 ? _resetZoom : null,
              ),
              _barButton(Icons.rotate_90_degrees_cw, '旋转', _rotate),
              _barButton(
                _thumbVisible
                    ? Icons.view_sidebar
                    : Icons.view_sidebar_outlined,
                _thumbVisible ? '隐藏左侧列表' : '显示左侧列表',
                () => setState(() => _thumbVisible = !_thumbVisible),
              ),
              _barButton(
                Icons.info_outline,
                '详情',
                () => setState(() => _detailsOpen = !_detailsOpen),
              ),
              _barButton(
                _fullscreen ? Icons.fullscreen_exit : Icons.fullscreen,
                _fullscreen ? '退出全屏' : '全屏',
                () => _toggleFullscreen(),
              ),
              _barButton(
                Icons.chevron_right,
                '下一张 (→)',
                () => _goTo(_index + 1),
              ),
              _barButton(Icons.close, '关闭 (Esc)', _close),
            ],
          ),
        ),
      ),
    );
  }

  Widget _barButton(IconData icon, String tooltip, VoidCallback? onTap) {
    return Tooltip(
      message: tooltip,
      child: IconButton(
        onPressed: onTap,
        icon: Icon(icon, size: 20),
        color: Colors.white,
      ),
    );
  }

  // ===== 详情面板 =====

  Widget _buildDetailsPanel() {
    final m = _media;
    if (m == null) return const SizedBox.shrink();
    return Positioned(
      top: 0,
      right: 0,
      bottom: 0,
      child: Container(
        width: 320,
        // color: const Color(0xEE303030),
        color: Colors.black.withValues(alpha: 0.45),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const SizedBox(width: 14),
                const Expanded(
                  child: Text(
                    '详细信息',
                    style: TextStyle(
                      color: Colors.white,
                      fontSize: 14,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ),
                IconButton(
                  icon: const Icon(
                    Icons.close,
                    size: 18,
                    color: Colors.white54,
                  ),
                  onPressed: () => setState(() => _detailsOpen = false),
                ),
              ],
            ),
            const Divider(color: Colors.white12, height: 1),
            Expanded(
              child: ListView(
                padding: const EdgeInsets.all(14),
                children: [
                  _detailRow('类型', '图片'),
                  if (m.format != null && m.format!.isNotEmpty)
                    _detailRow('格式', m.format!),
                  if (m.width != null && m.height != null)
                    _detailRow('分辨率', '${m.width} × ${m.height}'),
                  _detailRow('大小', _formatBytes(m.fileSize)),
                  if (m.mtime != null)
                    _detailRow(
                      '修改时间',
                      formatLocalTime(m.mtime!.toIso8601String()),
                    ),
                  const SizedBox(height: 12),
                  _detailRow('完整路径', m.relativePath),
                  const SizedBox(height: 12),
                  if (m.sha1 != null && m.sha1!.isNotEmpty)
                    _detailRow('SHA1', m.sha1!, mono: true),
                  if (m.phash != null && m.phash!.isNotEmpty)
                    _detailRow('pHash', m.phash!, mono: true),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _detailRow(String label, String value, {bool mono = false}) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 5),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            label,
            style: const TextStyle(color: Colors.white38, fontSize: 11),
          ),
          const SizedBox(height: 2),
          SelectableText(
            value,
            style: TextStyle(
              color: Colors.white70,
              fontSize: 12,
              fontFamily: mono ? 'monospace' : null,
            ),
          ),
        ],
      ),
    );
  }

  // ===== 工具函数 =====

  static String _parentDirectory(String path) {
    final normalized = path.replaceAll('\\', '/');
    final index = normalized.lastIndexOf('/');
    return index < 0 ? '' : normalized.substring(0, index);
  }

  static String _formatBytes(int bytes) {
    if (bytes < 1024) return '$bytes B';
    if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)} KB';
    if (bytes < 1024 * 1024 * 1024) {
      return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
    }
    return '${(bytes / (1024 * 1024 * 1024)).toStringAsFixed(2)} GB';
  }
}

/// 把变换矩阵钳制到图片可见区域（纯函数，供单元测试）：
/// 可见区域（子坐标系中的视口包围盒）不得超出**图片区域**——图片在视口大小的
/// 子区域中由 BoxFit.contain 居中（四周可能有留边 [imgLeft,imgTop] 起）。
/// 可见区域小于图片的维度钳制到图片边缘；大于图片（整图可见）的维度居中。
Matrix4 clampMatrixToImage(Matrix4 matrix, Size viewport, Size ds) {
  final vw = viewport.width, vh = viewport.height;
  if (vw <= 0 || vh <= 0 || ds.width <= 0 || ds.height <= 0) {
    return matrix.clone();
  }
  final imgLeft = (vw - ds.width) / 2;
  final imgTop = (vh - ds.height) / 2;
  final imgRight = imgLeft + ds.width;
  final imgBottom = imgTop + ds.height;
  final inv = Matrix4.inverted(matrix);
  final corners = [
    MatrixUtils.transformPoint(inv, Offset.zero),
    MatrixUtils.transformPoint(inv, Offset(vw, 0)),
    MatrixUtils.transformPoint(inv, Offset(0, vh)),
    MatrixUtils.transformPoint(inv, Offset(vw, vh)),
  ];
  var minX = corners.first.dx, maxX = corners.first.dx;
  var minY = corners.first.dy, maxY = corners.first.dy;
  for (final c in corners.skip(1)) {
    minX = math.min(minX, c.dx);
    maxX = math.max(maxX, c.dx);
    minY = math.min(minY, c.dy);
    maxY = math.max(maxY, c.dy);
  }
  final w = maxX - minX, h = maxY - minY;
  final desiredMinX =
      w > ds.width
          ? imgLeft + (ds.width - w) / 2
          : math.min(math.max(minX, imgLeft), imgRight - w);
  final desiredMinY =
      h > ds.height
          ? imgTop + (ds.height - h) / 2
          : math.min(math.max(minY, imgTop), imgBottom - h);
  final dx = desiredMinX - minX, dy = desiredMinY - minY;
  if (dx.abs() < 0.001 && dy.abs() < 0.001) return matrix.clone();
  // 可见包围盒平移 (dx, dy) 等价于矩阵右乘 (-dx, -dy) 的平移
  return matrix * Matrix4.translationValues(-dx, -dy, 0);
}
