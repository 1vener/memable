// media_viewer.dart：应用内全屏媒体查看器（图片/视频）。
// 桌面与 Web 统一走 HTTP（api.mediaFileUrl），视频用 media_kit 播放（网络流 + Range）。
// 支持：左右方向键/侧边按钮上一张下一张、图片缩放平移（InteractiveViewer）、
// Esc/返回关闭；HEIC/CR2 等应用内无法解码的格式提示用系统默认程序打开。
// 代码注释使用中文。
import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:media_kit/media_kit.dart' as mk;
import 'package:media_kit_video/media_kit_video.dart';

import '../models/models.dart';
import '../services/api_service.dart';

/// 全屏媒体查看器。medias 为可切换的媒体列表（通常传当前目录排序后的列表）。
class MediaViewer extends StatefulWidget {
  final List<Media> medias;
  final int initialIndex;
  final ApiService api;

  const MediaViewer({
    super.key,
    required this.medias,
    required this.initialIndex,
    required this.api,
  });

  @override
  State<MediaViewer> createState() => _MediaViewerState();
}

class _MediaViewerState extends State<MediaViewer> {
  late int _index = widget.initialIndex;
  VideoController? _videoController;
  bool _videoError = false;
  StreamSubscription<String>? _errorSub;
  StreamSubscription<double>? _rateSub;

  // 播放倍速：以 mpv 实际上报速率为准（ValueNotifier，ValueListenableBuilder 驱动
  // 按钮文字，不依赖外层控制栏重建，避免显示停在 1x）
  late final ValueNotifier<double> _rate = ValueNotifier(1.0);
  static const List<double> _speedSteps = [
    0.5, 0.75, 1.0, 1.25, 1.5, 1.75, 2.0,
  ];
  // 倍速按钮本次点击的全局坐标（锚定菜单用）。不用 GlobalKey：全屏模式下
  // normal/fullscreen 两份控制栏会同时挂载，共享同一 GlobalKey 会触发
  // "Duplicate GlobalKey" 异常。
  Offset? _speedMenuAnchor;

  static String _speedLabelFor(double s) {
    final v = s.toStringAsFixed(2).replaceFirst(RegExp(r'\.?0+$'), '');
    return '${v}x';
  }

  Media get _media => widget.medias[_index];
  String get _url => widget.api.mediaFileUrl(_media.id);
  String get _fileName =>
      _media.relativePath.split('/').last.split('\\').last;

  @override
  void initState() {
    super.initState();
    mk.MediaKit.ensureInitialized();
    if (widget.medias[_index].kind == 'video') {
      _loadVideo();
    }
  }

  @override
  void dispose() {
    _errorSub?.cancel();
    _rateSub?.cancel();
    _rate.dispose();
    _disposeVideo();
    super.dispose();
  }

  Future<void> _disposeVideo() async {
    _errorSub?.cancel();
    _errorSub = null;
    _rateSub?.cancel();
    _rateSub = null;
    final c = _videoController;
    _videoController = null;
    if (c != null) {
      await c.player.dispose();
    }
  }

  Future<void> _loadVideo() async {
    setState(() {
      _videoError = false;
      _videoController = null;
    });
    try {
      final player = mk.Player();
      // 必须先把 VideoController 挂接到 Player 上再 open：视频输出（texture）在
      // 挂接时注册，Windows 上先 open 后挂接会丢失首帧/渲染初始化导致黑屏。
      // 个别 GPU 驱动硬解黑屏时可将 VideoController 改为
      // VideoController(player, configuration: VideoControllerConfiguration(
      //   enableHardwareAcceleration: false)) 复测。
      final controller = VideoController(player);
      _errorSub?.cancel();
      _errorSub = player.stream.error.listen((_) {
        if (mounted) setState(() => _videoError = true);
      });
      // 外部（快捷键等）改变倍速时同步按钮文案（以 mpv 上报为准）
      _rateSub?.cancel();
      _rateSub = player.stream.rate.listen((r) {
        if (r > 0) _rate.value = r;
      });
      _rate.value = player.state.rate > 0 ? player.state.rate : 1.0;
      if (!mounted) {
        await player.dispose();
        return;
      }
      setState(() => _videoController = controller);
      await player.open(mk.Media(_url));
    } catch (_) {
      if (mounted) setState(() => _videoError = true);
    }
  }

  Future<void> _goTo(int next) async {
    if (widget.medias.isEmpty) return;
    final n = (next % widget.medias.length + widget.medias.length) %
        widget.medias.length;
    if (n == _index) return;
    await _disposeVideo();
    if (!mounted) return;
    setState(() {
      _index = n;
      _videoController = null;
      _videoError = false;
    });
    if (_media.kind == 'video') {
      _loadVideo();
    }
  }

  KeyEventResult _onKey(FocusNode node, KeyEvent event) {
    if (event is! KeyDownEvent) return KeyEventResult.ignored;
    if (event.logicalKey == LogicalKeyboardKey.escape) {
      Navigator.of(context).pop();
      return KeyEventResult.handled;
    }
    if (event.logicalKey == LogicalKeyboardKey.space) {
      // 空格播放/暂停
      final p = _videoController?.player;
      if (p != null) {
        if (p.state.playing) {
          p.pause();
        } else {
          p.play();
        }
      }
      return KeyEventResult.handled;
    }
    if (event.logicalKey == LogicalKeyboardKey.arrowLeft) {
      _goTo(_index - 1);
      return KeyEventResult.handled;
    }
    if (event.logicalKey == LogicalKeyboardKey.arrowRight) {
      _goTo(_index + 1);
      return KeyEventResult.handled;
    }
    return KeyEventResult.ignored;
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final isVideo = _media.kind == 'video';

    return Scaffold(
      backgroundColor: Colors.black,
      body: Focus(
        autofocus: true,
        onKeyEvent: _onKey,
        child: Stack(
          children: [
            // 主内容
            Positioned.fill(
              child: isVideo ? _buildVideo() : _buildImage(),
            ),
            // 图片无播放栏，保留侧边导航；视频的上一/下一个在播放栏内
            if (!isVideo && widget.medias.length > 1) ...[
              Positioned(
                left: 12,
                top: 0,
                bottom: 0,
                child: Center(
                  child: _navButton(Icons.chevron_left, () => _goTo(_index - 1)),
                ),
              ),
              Positioned(
                right: 12,
                top: 0,
                bottom: 0,
                child: Center(
                  child: _navButton(Icons.chevron_right, () => _goTo(_index + 1)),
                ),
              ),
            ],
            // 顶栏
            Positioned(
              top: 0,
              left: 0,
              right: 0,
              child: _buildTopBar(cs),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildTopBar(ColorScheme cs) {
    return Container(
      decoration: BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topCenter,
          end: Alignment.bottomCenter,
          colors: [
            Colors.black.withValues(alpha: 0.75),
            Colors.transparent,
          ],
        ),
      ),
      padding: const EdgeInsets.fromLTRB(8, 8, 12, 40),
      child: Row(
        children: [
          IconButton(
            tooltip: '关闭 (Esc)',
            icon: const Icon(Icons.close, color: Colors.white, size: 22),
            onPressed: () => Navigator.of(context).pop(),
          ),
          Expanded(
            child: Text(
              _fileName,
              style: const TextStyle(fontSize: 13, color: Colors.white),
              overflow: TextOverflow.ellipsis,
            ),
          ),
          if (widget.medias.length > 1)
            Text(
              '${_index + 1}/${widget.medias.length}',
              style: const TextStyle(fontSize: 12, color: Colors.white70),
            ),
          IconButton(
            tooltip: '用系统默认程序打开',
            icon: const Icon(Icons.open_in_new, color: Colors.white, size: 20),
            onPressed: () => _openSystem(),
          ),
          IconButton(
            tooltip: '打开文件所在目录',
            icon: const Icon(Icons.folder_open, color: Colors.white, size: 20),
            onPressed: () => _openDirectory(),
          ),
        ],
      ),
    );
  }

  Widget _navButton(IconData icon, VoidCallback onTap) {
    return IconButton(
      onPressed: onTap,
      icon: Icon(icon, color: Colors.white, size: 36),
      style: IconButton.styleFrom(
        backgroundColor: Colors.black.withValues(alpha: 0.35),
      ),
    );
  }

  Widget _buildImage() {
    return InteractiveViewer(
      maxScale: 8,
      child: Center(
        child: Image.network(
          _url,
          fit: BoxFit.contain,
          loadingBuilder: (_, child, progress) {
            if (progress == null) return child;
            return const Center(
              child: CircularProgressIndicator(
                strokeWidth: 2,
                color: Colors.white70,
              ),
            );
          },
          errorBuilder: (_, __, ___) => _unsupportedHint(),
        ),
      ),
    );
  }

  Widget _buildVideo() {
    final c = _videoController;
    if (_videoError) {
      return _unsupportedHint();
    }
    if (c == null) {
      return const Center(
        child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white70),
      );
    }
    // 自定义播放栏：上一个/快退10s/播放暂停/快进10s/下一个/倍速 + 默认音量/进度/全屏
    final bar = <Widget>[
      _barButton(Icons.skip_previous, '上一个', () => _goTo(_index - 1)),
      const MaterialDesktopPlayOrPauseButton(),
      _barButton(Icons.skip_next, '下一个', () => _goTo(_index + 1)),
      _barButton(Icons.replay_10, '快退 10 秒', _rewind),
      _barButton(Icons.forward_10, '快进 10 秒', _forward),
      const MaterialDesktopVolumeButton(),
      const MaterialDesktopPositionIndicator(),
      const Spacer(),
      // 倍速按钮：文字以 mpv 实际速率为准（ValueListenableBuilder 独立刷新）；
      // Listener 捕获点击坐标供菜单锚定（不参与手势竞技场，不影响按钮点击）
      Builder(
        builder: (ctx) => Listener(
          onPointerDown: (e) {
            final box = ctx.findRenderObject() as RenderBox?;
            if (box != null) {
              _speedMenuAnchor = box.localToGlobal(e.position);
            }
          },
          child: Tooltip(
            message: '倍速',
            child: IconButton(
              onPressed: _showSpeedDialog,
              icon: ValueListenableBuilder<double>(
                valueListenable: _rate,
                builder: (_, r, __) => Text(
                  _speedLabelFor(r),
                  style: const TextStyle(
                    fontSize: 12,
                    color: Colors.white,
                    fontWeight: FontWeight.w500,
                  ),
                ),
              ),
              iconSize: 28,
              color: Colors.white,
            ),
          ),
        ),
      ),
      const MaterialDesktopFullscreenButton(),
    ];
    return MaterialDesktopVideoControlsTheme(
      // playAndPauseOnTap：点击视频画面播放/暂停（按钮区域点击仍走按钮）
      normal: MaterialDesktopVideoControlsThemeData(
        playAndPauseOnTap: true,
        bottomButtonBar: bar,
      ),
      fullscreen: MaterialDesktopVideoControlsThemeData(
        playAndPauseOnTap: true,
        bottomButtonBar: bar,
      ),
      child: Video(controller: c),
    );
  }

  /// 播放栏按钮（白图标 + 提示）
  Widget _barButton(
    IconData icon,
    String tooltip,
    VoidCallback onTap, {
    Widget? customIcon,
  }) {
    return Tooltip(
      message: tooltip,
      child: IconButton(
        onPressed: onTap,
        icon: customIcon ?? Icon(icon),
        iconSize: 28,
        color: Colors.white,
      ),
    );
  }

  Future<void> _rewind() async {
    final p = _videoController?.player;
    if (p == null) return;
    final pos = p.state.position - const Duration(seconds: 10);
    await p.seek(pos < Duration.zero ? Duration.zero : pos);
  }

  Future<void> _forward() async {
    final p = _videoController?.player;
    if (p == null) return;
    final dur = p.state.duration;
    final pos = p.state.position + const Duration(seconds: 10);
    await p.seek(dur > Duration.zero && pos > dur ? dur : pos);
  }

  /// 倍速菜单：锚定在倍速按钮上方弹出（按钮在底部控制栏），
  /// 半透明背景（可透见视频）。选中后 setRate，按钮文字由 mpv 速率流驱动刷新。
  Future<void> _showSpeedDialog() async {
    final p = _videoController?.player;
    if (p == null) return;
    final anchor = _speedMenuAnchor;
    if (anchor == null) return;
    final overlay =
        Overlay.of(context).context.findRenderObject() as RenderBox;
    // 菜单置于按钮上方：顶部 = 锚点上方一个菜单高度；右缘对齐按钮中心
    const menuW = 120.0;
    const rowH = 34.0;
    final menuH = _speedSteps.length * rowH + 8;
    var top = anchor.dy - menuH - 6;
    if (top < 8) top = 8; // 顶部空间不足时贴顶（仍在按钮附近）
    final left = anchor.dx - menuW + 28;
    final position = RelativeRect.fromLTRB(
      left,
      top,
      overlay.size.width - left,
      overlay.size.height - top,
    );

    final selected = await showMenu<double>(
      context: context,
      position: position,
      constraints: const BoxConstraints(minWidth: menuW),
      color: Colors.black.withValues(alpha: 0.5),
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(10),
        side: BorderSide(color: Colors.white.withValues(alpha: 0.15)),
      ),
      elevation: 8,
      items: [
        for (final s in _speedSteps)
          PopupMenuItem<double>(
            value: s,
            height: 34,
            child: Row(
              children: [
                Text(
                  _speedLabelFor(s),
                  style: TextStyle(
                    fontSize: 13,
                    color: (s - _rate.value).abs() < 0.01
                        ? const Color(0xFFFF5252)
                        : Colors.white,
                    fontWeight: (s - _rate.value).abs() < 0.01
                        ? FontWeight.w700
                        : FontWeight.w400,
                  ),
                ),
                const Spacer(),
                if ((s - _rate.value).abs() < 0.01)
                  const Icon(
                    Icons.check,
                    size: 16,
                    color: Color(0xFFFF5252),
                  ),
              ],
            ),
          ),
      ],
    );
    if (selected == null || !mounted) return;
    final player = _videoController?.player;
    if (player == null) return;
    await player.setRate(selected);
  }

  Future<void> _openSystem() async {
    try {
      await widget.api.openMediaFile(_media.id);
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('打开失败: $e')),
        );
      }
    }
  }

  Future<void> _openDirectory() async {
    try {
      await widget.api.openMediaDirectory(_media.id);
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('打开目录失败: $e')),
        );
      }
    }
  }

  Widget _unsupportedHint() {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Icon(Icons.image_not_supported_outlined,
              size: 56, color: Colors.white38),
          const SizedBox(height: 12),
          const Text(
            '此格式无法在应用内预览（如 HEIC/CR2）',
            style: TextStyle(fontSize: 13, color: Colors.white70),
          ),
          const SizedBox(height: 8),
          TextButton.icon(
            onPressed: _openSystem,
            icon: const Icon(Icons.open_in_new, size: 16),
            label: const Text('用系统默认程序打开'),
          ),
        ],
      ),
    );
  }
}
