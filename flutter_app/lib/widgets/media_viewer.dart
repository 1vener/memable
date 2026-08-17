// media_viewer.dart：YouTube 风格的应用内媒体查看器。
// 桌面与 Web 统一走 HTTP，视频使用 media_kit 播放网络流。
// 代码注释使用中文。
import 'dart:async';

import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:media_kit/media_kit.dart' as mk;
import 'package:media_kit_video/media_kit_video.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../models/models.dart';
import '../services/api_service.dart';
import 'native_fullscreen.dart';

/// 全屏媒体查看器：播放器、同目录视频列表、媒体详情。
class MediaViewer extends StatefulWidget {
  final List<Media> medias;
  final int initialIndex;
  final ApiService api;
  final bool preserveQueue;

  const MediaViewer({
    super.key,
    required this.medias,
    required this.initialIndex,
    required this.api,
    this.preserveQueue = false,
  });

  @override
  State<MediaViewer> createState() => _MediaViewerState();
}

class _MediaViewerState extends State<MediaViewer> {
  late List<Media> _queue;
  late int _index;

  VideoController? _videoController;
  bool _videoError = false;
  String _videoErrorMessage = '';
  StreamSubscription<String>? _errorSub;
  StreamSubscription<bool>? _playingSub;
  StreamSubscription<double>? _rateSub;
  StreamSubscription<bool>? _completedSub;

  // 自动连播：当前视频播完后自动播放下一个（持久化）。
  // 用 ValueNotifier 驱动功能栏 Switch，避免 media_kit 控件主题不重建导致状态不刷新。
  final ValueNotifier<bool> _autoPlayNext = ValueNotifier(false);

  // 转码兜底（解码器不支持时自动转码播放）
  bool _transcoding = false;
  String? _transcodeError;
  int? _transcodeTarget; // 正在转码/已转码的媒体 id，防重复触发与切换竞态
  late final ValueNotifier<double> _rate = ValueNotifier(1.0);

  // 视频旋转（mpv video-rotate 属性：0/90/180/270，仅桌面端生效）
  final ValueNotifier<int> _rotation = ValueNotifier(0);

  List<Media> _directoryVideos = [];
  bool _directoryLoading = true;
  Offset? _speedMenuAnchor;
  Offset? _rotationMenuAnchor;
  final ScrollController _pageScrollController = ScrollController();

  static const List<double> _speedSteps = [
    0.5,
    0.75,
    1.0,
    1.25,
    1.5,
    1.75,
    2.0,
  ];

  static String _speedLabelFor(double speed) {
    final value = speed.toStringAsFixed(2).replaceFirst(RegExp(r'\.?0+$'), '');
    return '${value}x';
  }

  Media? get _currentMedia => _queue.isEmpty ? null : _queue[_index];
  Media get _media => _queue[_index];
  String get _url => widget.api.mediaFileUrl(_media.id);
  String get _fileName => _fileNameOf(_media);

  @override
  void initState() {
    super.initState();
    _queue = List<Media>.of(widget.medias);
    _index = _initialIndex(_queue, widget.initialIndex);
    _directoryVideos = _videosInDirectory(_queue, _currentMedia);
    mk.MediaKit.ensureInitialized();
    _loadAutoPlayPref();
    if (_currentMedia?.kind == 'video') _loadVideo();
    if (widget.preserveQueue) {
      _directoryLoading = false;
    } else {
      _loadDirectoryQueue();
    }
  }

  int _initialIndex(List<Media> items, int requested) {
    if (items.isEmpty) return 0;
    if (requested < 0) return 0;
    if (requested >= items.length) return items.length - 1;
    return requested;
  }

  /// 读取自动连播偏好。
  Future<void> _loadAutoPlayPref() async {
    try {
      final prefs = await SharedPreferences.getInstance();
      if (mounted) {
        _autoPlayNext.value = prefs.getBool('ui.viewer.autoplay') ?? false;
      }
    } catch (_) {}
  }

  /// 切换自动连播并持久化。
  void _toggleAutoPlay(bool value) {
    _autoPlayNext.value = value;
    SharedPreferences.getInstance()
        .then((prefs) {
          prefs.setBool('ui.viewer.autoplay', _autoPlayNext.value);
        })
        .catchError((_) {});
  }

  /// 播放完成回调：自动连播时切到下一个视频，已是最后一个则停在末尾。
  void _onCompleted(bool completed) {
    if (!completed || !_autoPlayNext.value || !mounted) return;
    final current = _currentMedia;
    if (current == null) return;
    final videos = _directoryVideos.isEmpty ? _queue : _directoryVideos;
    final index = videos.indexWhere((m) => m.id == current.id);
    if (index < 0 || index >= videos.length - 1) return;
    _goAdjacentVideo(1);
  }

  @override
  void dispose() {
    _errorSub?.cancel();
    _playingSub?.cancel();
    _rateSub?.cancel();
    _rate.dispose();
    _rotation.dispose();
    _autoPlayNext.dispose();
    _pageScrollController.dispose();
    _disposeVideo();
    super.dispose();
  }

  Future<void> _disposeVideo() async {
    _errorSub?.cancel();
    _errorSub = null;
    _playingSub?.cancel();
    _playingSub = null;
    _rateSub?.cancel();
    _rateSub = null;
    _completedSub?.cancel();
    _completedSub = null;
    final controller = _videoController;
    _videoController = null;
    if (controller != null) {
      await controller.player.dispose();
    }
  }

  Future<void> _loadDirectoryQueue() async {
    final current = _currentMedia;
    if (current == null) {
      if (mounted) setState(() => _directoryLoading = false);
      return;
    }
    try {
      final files = await widget.api.getFiles(
        current.libraryId,
        path: _parentDirectory(current.relativePath),
      );
      if (!mounted) return;
      if (files.isEmpty) {
        setState(() => _directoryLoading = false);
        return;
      }
      final index = files.indexWhere((m) => m.id == current.id);
      if (index < 0) return;
      setState(() {
        _queue = files;
        _index = index;
        _directoryVideos = _videosInDirectory(files, current);
        _directoryLoading = false;
      });
    } catch (_) {
      if (mounted) {
        setState(() {
          _directoryVideos = _videosInDirectory(_queue, _currentMedia);
          _directoryLoading = false;
        });
      }
    }
  }

  /// 解析播放源：桌面端优先使用本地文件路径（mpv 本地寻址，moov 在文件尾等
  /// 结构也能正常打开，规避 HTTP 流 seek 问题），失败时回退 HTTP 流；
  /// Web 端只能走 HTTP。
  Future<mk.Media> _resolvePlaybackMedia() async {
    if (!kIsWeb) {
      try {
        final local = await widget.api.mediaLocalPath(_media.id);
        if (local.isNotEmpty) return mk.Media(local);
      } catch (e) {
        debugPrint('[MediaViewer] 获取本地路径失败，回退 HTTP 播放: $e');
      }
    }
    return mk.Media(_url);
  }

  Future<void> _loadVideo({String? localPath, String? httpUrl}) async {
    if (!mounted || _currentMedia?.kind != 'video') return;
    // 释放上一个播放器（转码重载/重复加载场景，避免解码器资源泄漏）
    final previous = _videoController;
    _videoController = null;
    if (previous != null) {
      await previous.player.dispose();
    }
    setState(() {
      _videoError = false;
      _videoErrorMessage = '';
      _videoController = null;
    });
    try {
      final player = mk.Player();
      // 必须先挂接 VideoController 再 open，避免 Windows 首帧渲染黑屏。
      final controller = VideoController(player);
      _errorSub?.cancel();
      _errorSub = player.stream.error.listen((message) {
        debugPrint('[MediaViewer] mpv 错误: $message');
        if (mounted) {
          setState(() {
            _videoError = true;
            _videoErrorMessage = message;
          });
        }
        _maybeAutoTranscode(message);
      });
      // 兜底：若报错后视频仍成功开始播放（非致命错误），自动清除错误提示
      _playingSub?.cancel();
      _playingSub = player.stream.playing.listen((playing) {
        if (playing && mounted && _videoError) {
          setState(() {
            _videoError = false;
            _videoErrorMessage = '';
          });
        }
      });
      _rateSub?.cancel();
      _rateSub = player.stream.rate.listen((rate) {
        if (rate > 0) _rate.value = rate;
      });
      // 自动连播：播放完成（completed=true）时切下一个视频
      _completedSub?.cancel();
      _completedSub = player.stream.completed.listen(_onCompleted);
      _rate.value = player.state.rate > 0 ? player.state.rate : 1.0;
      if (!mounted) {
        await player.dispose();
        return;
      }
      setState(() => _videoController = controller);
      final playbackMedia =
          localPath != null
              ? mk.Media(localPath)
              : httpUrl != null
              ? mk.Media(httpUrl)
              : await _resolvePlaybackMedia();
      if (!mounted) {
        await player.dispose();
        return;
      }
      await player.open(playbackMedia);
      // 转码重载/重复加载后重新应用旋转
      if (_rotation.value != 0) {
        await _applyRotation(player, _rotation.value);
      }
    } catch (e) {
      debugPrint('[MediaViewer] 加载视频失败: $e');
      if (mounted) {
        setState(() {
          _videoError = true;
          _videoErrorMessage = '$e';
        });
      }
    }
  }

  /// 解码器错误时自动启动转码（同一媒体只自动触发一次）。
  void _maybeAutoTranscode(String message) {
    final m = message.toLowerCase();
    final isDecoderIssue =
        m.contains('decoder') ||
        m.contains('failed to initialize') ||
        m.contains('unknown codec') ||
        m.contains('no decoder');
    if (isDecoderIssue && !_transcoding && _transcodeTarget != _media.id) {
      _startTranscode();
    }
  }

  /// 启动转码：产物命中缓存立即播放，否则轮询状态。
  Future<void> _startTranscode() async {
    if (_transcoding) return;
    setState(() {
      _transcoding = true;
      _transcodeError = null;
      _transcodeTarget = _media.id;
    });
    try {
      final resp = await widget.api.transcodeMedia(_media.id);
      if (!mounted) return;
      if (resp['status'] == 'done') {
        _playTranscoded(resp);
      } else {
        _pollTranscode();
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _transcoding = false;
          _transcodeError = '启动转码失败: $e';
        });
      }
    }
  }

  Future<void> _pollTranscode() async {
    while (mounted && _transcoding && _transcodeTarget == _media.id) {
      await Future.delayed(const Duration(seconds: 2));
      if (!mounted || !_transcoding || _transcodeTarget != _media.id) return;
      try {
        final st = await widget.api.transcodeStatus(_media.id);
        if (st['status'] == 'done') {
          _playTranscoded(st);
          return;
        }
        if (st['status'] == 'failed') {
          setState(() {
            _transcoding = false;
            _transcodeError = st['error'] as String? ?? '转码失败';
          });
          return;
        }
      } catch (_) {
        // 网络抖动继续轮询
      }
    }
  }

  /// 播放转码产物：桌面播本地路径，web 播 HTTP 地址。
  void _playTranscoded(Map<String, dynamic> st) {
    if (!mounted) return;
    setState(() {
      _transcoding = false;
      _transcodeError = null;
    });
    final path = st['path'] as String?;
    final name = st['name'] as String?;
    if (kIsWeb) {
      if (name != null && name.isNotEmpty) {
        _loadVideo(httpUrl: widget.api.transcodeFileUrl(name));
      }
    } else if (path != null && path.isNotEmpty) {
      _loadVideo(localPath: path);
    }
  }

  Future<void> _goTo(int next) async {
    if (_queue.isEmpty) return;
    final normalized = (next % _queue.length + _queue.length) % _queue.length;
    if (normalized == _index) return;
    final nextMedia = _queue[normalized];
    await _disposeVideo();
    if (!mounted) return;
    setState(() {
      _index = normalized;
      _videoError = false;
      _videoErrorMessage = '';
      _transcoding = false;
      _transcodeError = null;
      _transcodeTarget = null;
    });
    _rotation.value = 0;
    if (nextMedia.kind == 'video') _loadVideo();
  }

  Future<void> _goAdjacentVideo(int delta) async {
    if (_directoryVideos.isEmpty) {
      await _goTo(_index + delta);
      return;
    }
    final currentVideoIndex = _directoryVideos.indexWhere(
      (m) => m.id == _media.id,
    );
    if (currentVideoIndex < 0) return;
    final next = (currentVideoIndex + delta) % _directoryVideos.length;
    final normalized =
        (next + _directoryVideos.length) % _directoryVideos.length;
    final targetId = _directoryVideos[normalized].id;
    final queueIndex = _queue.indexWhere((m) => m.id == targetId);
    if (queueIndex >= 0) await _goTo(queueIndex);
  }

  Future<void> _selectPlaylistMedia(Media media) async {
    final index = _queue.indexWhere((m) => m.id == media.id);
    if (index >= 0) {
      await _goTo(index);
    }
  }

  KeyEventResult _onKey(FocusNode node, KeyEvent event) {
    if (event is! KeyDownEvent) return KeyEventResult.ignored;
    if (event.logicalKey == LogicalKeyboardKey.escape) {
      Navigator.of(context).pop();
      return KeyEventResult.handled;
    }
    if (event.logicalKey == LogicalKeyboardKey.space) {
      final player = _videoController?.player;
      if (player != null) {
        if (player.state.playing) {
          player.pause();
        } else {
          player.play();
        }
      }
      return KeyEventResult.handled;
    }
    if (event.logicalKey == LogicalKeyboardKey.arrowLeft) {
      _goAdjacentVideo(-1);
      return KeyEventResult.handled;
    }
    if (event.logicalKey == LogicalKeyboardKey.arrowRight) {
      _goAdjacentVideo(1);
      return KeyEventResult.handled;
    }
    return KeyEventResult.ignored;
  }

  @override
  Widget build(BuildContext context) {
    final current = _currentMedia;
    if (current == null) {
      return const Scaffold(
        backgroundColor: Color(0xFF0F0F0F),
        body: Center(
          child: Text('没有可查看的媒体', style: TextStyle(color: Colors.white70)),
        ),
      );
    }
    final cs = Theme.of(context).colorScheme;
    return Scaffold(
      backgroundColor: const Color(0xFF0F0F0F),
      body: Focus(
        autofocus: true,
        onKeyEvent: _onKey,
        child: SafeArea(
          child: Column(
            children: [
              _buildHeader(),
              Expanded(
                child: LayoutBuilder(
                  builder: (context, constraints) {
                    if (constraints.maxWidth >= 900) {
                      return _buildWideLayout(cs, constraints);
                    }
                    return _buildNarrowLayout(cs);
                  },
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildHeader() {
    return Container(
      height: 54,
      padding: const EdgeInsets.symmetric(horizontal: 10),
      color: const Color(0xFF181818),
      child: Row(
        children: [
          IconButton(
            tooltip: '关闭 (Esc)',
            icon: const Icon(Icons.close, color: Colors.white),
            onPressed: () => Navigator.of(context).pop(),
          ),
          const SizedBox(width: 6),
          const Icon(Icons.play_circle_fill, color: Color(0xFFFF0033)),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              _fileName,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: const TextStyle(
                color: Colors.white,
                fontSize: 14,
                fontWeight: FontWeight.w600,
              ),
            ),
          ),
          if (_queue.length > 1)
            Text(
              '${_index + 1}/${_queue.length}',
              style: const TextStyle(color: Colors.white60, fontSize: 12),
            ),
          IconButton(
            tooltip: '用系统默认程序打开',
            icon: const Icon(Icons.open_in_new, color: Colors.white70),
            onPressed: _openSystem,
          ),
          IconButton(
            tooltip: '复制外部播放地址（其它设备/播放器可播）',
            icon: const Icon(Icons.link, color: Colors.white70),
            onPressed: _copyExternalUrl,
          ),
          IconButton(
            tooltip: '打开文件所在目录',
            icon: const Icon(Icons.folder_open, color: Colors.white70),
            onPressed: _openDirectory,
          ),
        ],
      ),
    );
  }

  Widget _buildWideLayout(ColorScheme cs, BoxConstraints viewport) {
    return Scrollbar(
      controller: _pageScrollController,
      thumbVisibility: true,
      child: SingleChildScrollView(
        controller: _pageScrollController,
        padding: const EdgeInsets.all(18),
        child: ConstrainedBox(
          constraints: BoxConstraints(minWidth: viewport.maxWidth - 36),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [_buildPlayerFrame(), _buildDetails(cs)],
                ),
              ),
              const SizedBox(width: 18),
              SizedBox(width: 340, child: _buildPlaylist(cs)),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildNarrowLayout(ColorScheme cs) {
    return Scrollbar(
      controller: _pageScrollController,
      thumbVisibility: true,
      child: SingleChildScrollView(
        controller: _pageScrollController,
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            _buildPlayerFrame(),
            _buildDetails(cs),
            const SizedBox(height: 12),
            _buildPlaylist(cs),
          ],
        ),
      ),
    );
  }

  Widget _buildPlayerFrame() {
    const ratio = 16 / 9;
    return LayoutBuilder(
      builder: (context, constraints) {
        final width = constraints.maxWidth;
        return SizedBox(
          width: double.infinity,
          height: width / ratio,
          child: Container(
            decoration: BoxDecoration(
              color: Colors.black,
              borderRadius: BorderRadius.circular(10),
            ),
            clipBehavior: Clip.antiAlias,
            child: _media.kind == 'video' ? _buildVideo() : _buildImage(),
          ),
        );
      },
    );
  }

  Widget _buildDetails(ColorScheme cs) {
    final media = _media;
    return Padding(
      padding: const EdgeInsets.fromLTRB(2, 16, 2, 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            _fileName,
            style: const TextStyle(
              color: Colors.white,
              fontSize: 18,
              fontWeight: FontWeight.w600,
            ),
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
          ),
          const SizedBox(height: 6),
          Text(
            media.relativePath,
            style: const TextStyle(color: Colors.white54, fontSize: 12),
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
          ),
          const SizedBox(height: 12),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              _detailChip('类型', media.kind == 'video' ? '视频' : '图片'),
              if (media.format != null && media.format!.isNotEmpty)
                _detailChip('格式', media.format!),
              if (media.width != null && media.height != null)
                _detailChip('分辨率', '${media.width} × ${media.height}'),
              _detailChip('大小', _formatBytes(media.fileSize)),
              if (media.durationMs != null && media.durationMs! > 0)
                _detailChip('时长', _formatDuration(media.durationMs!)),
              if (media.bitRate != null && media.bitRate! > 0)
                _detailChip('比特率', _formatBitRate(media.bitRate!)),
              if (media.frameRate != null && media.frameRate! > 0)
                _detailChip('帧率', '${_formatFrameRate(media.frameRate!)} fps'),
              if (media.videoCodec != null && media.videoCodec!.isNotEmpty)
                _detailChip('视频编码', media.videoCodec!),
              if (media.audioCodec != null && media.audioCodec!.isNotEmpty)
                _detailChip('音频编码', media.audioCodec!),
              if (media.mtime != null)
                _detailChip('修改时间', _formatDate(media.mtime!)),
            ],
          ),
        ],
      ),
    );
  }

  Widget _detailChip(String label, String value) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 7),
      decoration: BoxDecoration(
        color: const Color(0xFF202020),
        borderRadius: BorderRadius.circular(7),
        border: Border.all(color: Colors.white12),
      ),
      child: RichText(
        text: TextSpan(
          style: const TextStyle(fontSize: 12),
          children: [
            TextSpan(
              text: '$label  ',
              style: const TextStyle(color: Colors.white38),
            ),
            TextSpan(
              text: value,
              style: const TextStyle(
                color: Colors.white,
                fontWeight: FontWeight.w500,
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildPlaylist(ColorScheme cs) {
    return Container(
      color: const Color(0xFF181818),
      padding: const EdgeInsets.fromLTRB(14, 14, 10, 10),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              const Expanded(
                child: Text(
                  '同目录视频',
                  style: TextStyle(
                    color: Colors.white,
                    fontSize: 15,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ),
              Text(
                _directoryLoading ? '加载中…' : '${_directoryVideos.length} 个',
                style: const TextStyle(color: Colors.white54, fontSize: 12),
              ),
            ],
          ),
          const SizedBox(height: 12),
          _directoryVideos.isEmpty
              ? Padding(
                padding: const EdgeInsets.symmetric(vertical: 32),
                child: Text(
                  _directoryLoading ? '正在读取目录…' : '该目录没有其他视频',
                  style: const TextStyle(color: Colors.white38, fontSize: 12),
                  textAlign: TextAlign.center,
                ),
              )
              : ListView.separated(
                shrinkWrap: true,
                physics: const NeverScrollableScrollPhysics(),
                itemCount: _directoryVideos.length,
                separatorBuilder: (_, __) => const SizedBox(height: 8),
                itemBuilder:
                    (_, index) =>
                        _buildPlaylistItem(_directoryVideos[index], index),
              ),
        ],
      ),
    );
  }

  Widget _buildPlaylistItem(Media media, int index) {
    final active = media.id == _media.id;
    final thumbnail =
        media.thumbnailPath == null
            ? null
            : widget.api.thumbnailUrl('video', media.thumbnailPath!);
    return Material(
      color: active ? const Color(0xFF3A2025) : Colors.transparent,
      borderRadius: BorderRadius.circular(7),
      child: InkWell(
        borderRadius: BorderRadius.circular(7),
        onTap: () => _selectPlaylistMedia(media),
        child: Container(
          padding: const EdgeInsets.all(5),
          decoration: BoxDecoration(
            borderRadius: BorderRadius.circular(7),
            border: Border.all(
              color: active ? const Color(0xFFFF0033) : Colors.transparent,
            ),
          ),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              SizedBox(
                width: 112,
                height: 64,
                child: ClipRRect(
                  borderRadius: BorderRadius.circular(5),
                  child: Stack(
                    fit: StackFit.expand,
                    children: [
                      thumbnail == null
                          ? Container(
                            color: const Color(0xFF303030),
                            child: const Icon(
                              Icons.videocam_outlined,
                              color: Colors.white38,
                            ),
                          )
                          : Image.network(
                            thumbnail,
                            fit: BoxFit.cover,
                            errorBuilder:
                                (_, __, ___) => Container(
                                  color: const Color(0xFF303030),
                                  child: const Icon(
                                    Icons.videocam_outlined,
                                    color: Colors.white38,
                                  ),
                                ),
                          ),
                      if (media.durationMs != null && media.durationMs! > 0)
                        Positioned(
                          right: 4,
                          bottom: 4,
                          child: Container(
                            padding: const EdgeInsets.symmetric(
                              horizontal: 4,
                              vertical: 2,
                            ),
                            color: Colors.black.withValues(alpha: 0.75),
                            child: Text(
                              _formatDuration(media.durationMs!),
                              style: const TextStyle(
                                color: Colors.white,
                                fontSize: 10,
                              ),
                            ),
                          ),
                        ),
                    ],
                  ),
                ),
              ),
              const SizedBox(width: 8),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      _fileNameOf(media),
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        color: active ? Colors.white : Colors.white70,
                        fontSize: 12,
                        fontWeight: active ? FontWeight.w600 : FontWeight.w400,
                      ),
                    ),
                    const SizedBox(height: 5),
                    Text(
                      media.format ?? '视频',
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(
                        color: Colors.white38,
                        fontSize: 11,
                      ),
                    ),
                  ],
                ),
              ),
              if (active)
                const Padding(
                  padding: EdgeInsets.only(left: 4, top: 3),
                  child: Icon(
                    Icons.equalizer,
                    size: 16,
                    color: Color(0xFFFF0033),
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildImage() {
    return InteractiveViewer(
      constrained: true,
      minScale: 1,
      maxScale: 8,
      child: SizedBox.expand(
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
      ),
    );
  }

  Widget _buildVideo() {
    final controller = _videoController;
    if (_transcoding) return _transcodingHint();
    if (_videoError) return _unsupportedHint();
    if (controller == null) {
      return const Center(
        child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white70),
      );
    }
    final bar = <Widget>[
      _barButton(Icons.skip_previous, '上一个', () => _goAdjacentVideo(-1)),
      const MaterialDesktopPlayOrPauseButton(),
      _barButton(Icons.skip_next, '下一个', () => _goAdjacentVideo(1)),
      _barButton(Icons.replay_10, '快退 10 秒', _rewind),
      _barButton(Icons.forward_10, '快进 10 秒', _forward),
      const MaterialDesktopVolumeButton(),
      // 自定义进度指示：切换视频后随当前播放器重新订阅时长/位置流
      const _VideoPositionIndicator(),
      const Spacer(),
      // 自动连播开关（靠右，播完切下一个，到底暂停）。
      // 用 ValueListenableBuilder 驱动，绕过控件主题不重建的限制。
      Tooltip(
        message: '自动连播',
        child: ValueListenableBuilder<bool>(
          valueListenable: _autoPlayNext,
          builder:
              (_, autoPlay, __) => Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  const Text(
                    '连播',
                    style: TextStyle(
                      color: Colors.white,
                      fontSize: 12,
                      fontWeight: FontWeight.w500,
                    ),
                  ),
                  Transform.scale(
                    scale: 0.75,
                    child: Switch(value: autoPlay, onChanged: _toggleAutoPlay),
                  ),
                ],
              ),
        ),
      ),
      Builder(
        builder:
            (ctx) => Listener(
              onPointerDown: (event) {
                final box = ctx.findRenderObject() as RenderBox?;
                if (box != null) {
                  _speedMenuAnchor = box.localToGlobal(event.localPosition);
                }
              },
              child: Tooltip(
                message: '倍速',
                child: IconButton(
                  onPressed: _showSpeedDialog,
                  icon: ValueListenableBuilder<double>(
                    valueListenable: _rate,
                    builder:
                        (_, rate, __) => Text(
                          _speedLabelFor(rate),
                          style: const TextStyle(
                            color: Colors.white,
                            fontSize: 12,
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
      if (!kIsWeb)
        Builder(
          builder:
              (ctx) => Listener(
                onPointerDown: (event) {
                  final box = ctx.findRenderObject() as RenderBox?;
                  if (box != null) {
                    _rotationMenuAnchor = box.localToGlobal(
                      event.localPosition,
                    );
                  }
                },
                child: Tooltip(
                  message: '旋转',
                  child: IconButton(
                    onPressed: _showRotationMenu,
                    icon: ValueListenableBuilder<int>(
                      valueListenable: _rotation,
                      builder:
                          (_, angle, __) => Text(
                            '$angle°',
                            style: const TextStyle(
                              color: Colors.white,
                              fontSize: 12,
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
      normal: MaterialDesktopVideoControlsThemeData(
        playAndPauseOnTap: true,
        bottomButtonBar: bar,
      ),
      fullscreen: MaterialDesktopVideoControlsThemeData(
        playAndPauseOnTap: true,
        bottomButtonBar: bar,
      ),
      child: Video(
        controller: controller,
        fit: BoxFit.contain,
        onEnterFullscreen:
            kIsWeb ? _enterFullscreenWeb : defaultEnterNativeFullscreen,
        onExitFullscreen:
            kIsWeb ? _exitFullscreenWeb : defaultExitNativeFullscreen,
      ),
    );
  }

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
    final player = _videoController?.player;
    if (player == null) return;
    final position = player.state.position - const Duration(seconds: 10);
    await player.seek(position < Duration.zero ? Duration.zero : position);
  }

  Future<void> _forward() async {
    final player = _videoController?.player;
    if (player == null) return;
    final duration = player.state.duration;
    final position = player.state.position + const Duration(seconds: 10);
    await player.seek(
      duration > Duration.zero && position > duration ? duration : position,
    );
  }

  Future<void> _showSpeedDialog() async {
    final player = _videoController?.player;
    final anchor = _speedMenuAnchor;
    if (player == null || anchor == null) return;
    final overlay = Overlay.of(context).context.findRenderObject() as RenderBox;
    const menuWidth = 120.0;
    const rowHeight = 34.0;
    final menuHeight = _speedSteps.length * rowHeight + 8;
    final top = (anchor.dy - menuHeight - 6).clamp(8.0, overlay.size.height);
    final left = anchor.dx - menuWidth / 2;
    final selected = await showMenu<double>(
      context: context,
      position: RelativeRect.fromLTRB(
        left,
        top,
        overlay.size.width - left - menuWidth,
        overlay.size.height - top,
      ),
      constraints: const BoxConstraints(
        minWidth: menuWidth,
        maxWidth: menuWidth,
      ),
      color: Colors.black.withValues(alpha: 0.5),
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(10),
        side: BorderSide(color: Colors.white.withValues(alpha: 0.15)),
      ),
      elevation: 8,
      items: [
        for (final speed in _speedSteps)
          PopupMenuItem<double>(
            value: speed,
            height: rowHeight,
            child: Row(
              children: [
                Text(
                  _speedLabelFor(speed),
                  style: TextStyle(
                    color:
                        (speed - _rate.value).abs() < 0.01
                            ? const Color(0xFFFF5252)
                            : Colors.white,
                    fontSize: 13,
                    fontWeight:
                        (speed - _rate.value).abs() < 0.01
                            ? FontWeight.w700
                            : FontWeight.w400,
                  ),
                ),
                const Spacer(),
                if ((speed - _rate.value).abs() < 0.01)
                  const Icon(Icons.check, size: 16, color: Color(0xFFFF5252)),
              ],
            ),
          ),
      ],
    );
    if (selected != null && mounted) await player.setRate(selected);
  }

  /// 旋转视频画面（mpv video-rotate 属性，0/90/180/270 度）。
  Future<void> _showRotationMenu() async {
    final player = _videoController?.player;
    final anchor = _rotationMenuAnchor;
    if (player == null || anchor == null) return;
    final overlay = Overlay.of(context).context.findRenderObject() as RenderBox;
    const menuWidth = 110.0;
    const rowHeight = 34.0;
    const angles = [0, 90, 180, 270];
    final menuHeight = angles.length * rowHeight + 8;
    final top = (anchor.dy - menuHeight - 6).clamp(8.0, overlay.size.height);
    final left = anchor.dx - menuWidth / 2;
    final selected = await showMenu<int>(
      context: context,
      position: RelativeRect.fromLTRB(
        left,
        top,
        overlay.size.width - left - menuWidth,
        overlay.size.height - top,
      ),
      constraints: const BoxConstraints(
        minWidth: menuWidth,
        maxWidth: menuWidth,
      ),
      color: Colors.black.withValues(alpha: 0.5),
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(10),
        side: BorderSide(color: Colors.white.withValues(alpha: 0.15)),
      ),
      elevation: 8,
      items: [
        for (final angle in angles)
          PopupMenuItem<int>(
            value: angle,
            height: rowHeight,
            child: Row(
              children: [
                Text(
                  '$angle°',
                  style: TextStyle(
                    color:
                        _rotation.value == angle
                            ? const Color(0xFFFF5252)
                            : Colors.white,
                    fontSize: 13,
                    fontWeight:
                        _rotation.value == angle
                            ? FontWeight.w700
                            : FontWeight.w400,
                  ),
                ),
                const Spacer(),
                if (_rotation.value == angle)
                  const Icon(Icons.check, size: 16, color: Color(0xFFFF5252)),
              ],
            ),
          ),
      ],
    );
    if (selected != null && mounted) await _rotate(player, selected);
  }

  /// 设置旋转角度并同步到 mpv（NativePlayer 暴露 setProperty，Web 端无此能力）。
  Future<void> _rotate(mk.Player player, int angle) async {
    _rotation.value = angle;
    await _applyRotation(player, angle);
  }

  Future<void> _applyRotation(mk.Player player, int angle) async {
    if (kIsWeb) return; // Web 端 HTML5 video 不支持 mpv 属性
    final platform = player.platform;
    if (platform == null) return;
    try {
      // dynamic 调用：Web 端 NativePlayer 为 stub 类无此方法，但运行时只在桌面端执行
      await (platform as dynamic).setProperty('video-rotate', '$angle');
    } catch (e) {
      debugPrint('[MediaViewer] 设置旋转失败: $e');
    }
  }

  /// Web 端进入全屏：media_kit 在浏览器原生全屏切换时会暂停 HTML5 video
  /// （上游 issue #935），进入后恢复播放。
  Future<void> _enterFullscreenWeb() async {
    await defaultEnterNativeFullscreen();
    _resumePlaybackAfterFullscreen();
  }

  /// Web 端退出全屏：同上，退出后恢复播放。
  Future<void> _exitFullscreenWeb() async {
    await guardedExitNativeFullscreen();
    _resumePlaybackAfterFullscreen();
  }

  /// 全屏切换后若视频本在播放但被浏览器暂停，延迟恢复（仅 Web 端调用）。
  void _resumePlaybackAfterFullscreen() {
    final player = _videoController?.player;
    if (player == null || !player.state.playing) return;
    Timer(const Duration(milliseconds: 150), () {
      final p = _videoController?.player;
      if (p != null && !p.state.playing) p.play();
    });
  }

  Widget _transcodingHint() {
    return const Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          CircularProgressIndicator(strokeWidth: 2, color: Colors.white70),
          SizedBox(height: 14),
          Text(
            '解码器不支持此编码，正在转码为通用格式…\n首次播放需等待，完成后自动播放',
            style: TextStyle(fontSize: 12, color: Colors.white70),
            textAlign: TextAlign.center,
          ),
        ],
      ),
    );
  }

  Widget _unsupportedHint() {
    final isDecoderIssue =
        _videoErrorMessage.toLowerCase().contains('decoder') ||
        _videoErrorMessage.toLowerCase().contains('failed to initialize') ||
        _videoErrorMessage.toLowerCase().contains('unknown codec');
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Icon(
            Icons.image_not_supported_outlined,
            size: 56,
            color: Colors.white38,
          ),
          const SizedBox(height: 12),
          Text(
            isDecoderIssue ? '此编码无法直接播放（如 ProRes）' : '此格式无法在应用内预览（如 HEIC/CR2）',
            style: const TextStyle(fontSize: 13, color: Colors.white70),
          ),
          if (_videoErrorMessage.isNotEmpty) ...[
            const SizedBox(height: 8),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 32),
              child: Text(
                '播放器错误：$_videoErrorMessage',
                style: const TextStyle(fontSize: 11, color: Colors.white38),
                textAlign: TextAlign.center,
                maxLines: 3,
                overflow: TextOverflow.ellipsis,
              ),
            ),
          ],
          if (_transcodeError != null) ...[
            const SizedBox(height: 8),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 32),
              child: Text(
                '转码失败：$_transcodeError',
                style: const TextStyle(fontSize: 11, color: Color(0xFFFF8A80)),
                textAlign: TextAlign.center,
                maxLines: 3,
                overflow: TextOverflow.ellipsis,
              ),
            ),
          ],
          const SizedBox(height: 12),
          if (isDecoderIssue)
            OutlinedButton.icon(
              onPressed: _transcoding ? null : _startTranscode,
              icon: const Icon(Icons.autorenew, size: 16),
              label: Text(_transcoding ? '转码中…' : '转码后播放'),
            ),
          TextButton.icon(
            onPressed: _openSystem,
            icon: const Icon(Icons.open_in_new, size: 16),
            label: const Text('用系统默认程序打开'),
          ),
        ],
      ),
    );
  }

  Future<void> _openSystem() async {
    try {
      await widget.api.openMediaFile(_media.id);
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('打开失败: $e')));
      }
    }
  }

  /// 复制外部播放地址：局域网内其它设备（浏览器/VLC/手机播放器）直接打开播放。
  /// 地址优先用服务端探测的本机对外 IP，缺失时回退当前 baseUrl。
  Future<void> _copyExternalUrl() async {
    var urls = <String>[];
    try {
      urls = await widget.api.getExternalUrls();
    } catch (_) {}
    final base = urls.isNotEmpty ? urls.first : widget.api.baseUrl;
    final url = '$base/api/media/${_media.id}/file';
    await Clipboard.setData(ClipboardData(text: url));
    if (mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text('外部播放地址已复制，可在其它设备或播放器中打开\n$url'),
          duration: const Duration(seconds: 3),
        ),
      );
    }
  }

  Future<void> _openDirectory() async {
    try {
      await widget.api.openMediaDirectory(_media.id);
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('打开目录失败: $e')));
      }
    }
  }

  List<Media> _videosInDirectory(List<Media> items, Media? current) {
    if (current == null) return [];
    final directory = _parentDirectory(current.relativePath);
    return items
        .where(
          (m) =>
              m.kind == 'video' &&
              _parentDirectory(m.relativePath) == directory,
        )
        .toList();
  }

  static String _parentDirectory(String path) {
    final normalized = path.replaceAll('\\', '/');
    final index = normalized.lastIndexOf('/');
    return index < 0 ? '' : normalized.substring(0, index);
  }

  static String _fileNameOf(Media media) {
    final normalized = media.relativePath.replaceAll('\\', '/');
    return normalized.substring(normalized.lastIndexOf('/') + 1);
  }

  static String _formatBytes(int bytes) {
    if (bytes < 1024) return '$bytes B';
    if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)} KB';
    if (bytes < 1024 * 1024 * 1024) {
      return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
    }
    return '${(bytes / (1024 * 1024 * 1024)).toStringAsFixed(2)} GB';
  }

  static String _formatDuration(int milliseconds) {
    final seconds = (milliseconds / 1000).round();
    final hours = seconds ~/ 3600;
    final minutes = (seconds % 3600) ~/ 60;
    final remainder = seconds % 60;
    if (hours > 0) {
      return '$hours:${minutes.toString().padLeft(2, '0')}:${remainder.toString().padLeft(2, '0')}';
    }
    return '$minutes:${remainder.toString().padLeft(2, '0')}';
  }

  static String _formatBitRate(int bitsPerSecond) {
    if (bitsPerSecond >= 1000 * 1000 * 1000) {
      return '${(bitsPerSecond / (1000 * 1000 * 1000)).toStringAsFixed(2)} Gbps';
    }
    if (bitsPerSecond >= 1000 * 1000) {
      return '${(bitsPerSecond / (1000 * 1000)).toStringAsFixed(2)} Mbps';
    }
    if (bitsPerSecond >= 1000) {
      return '${(bitsPerSecond / 1000).toStringAsFixed(1)} Kbps';
    }
    return '$bitsPerSecond bps';
  }

  static String _formatFrameRate(double value) {
    if ((value - value.round()).abs() < 0.01) return value.round().toString();
    return value.toStringAsFixed(2);
  }

  static String _formatDate(DateTime value) {
    final local = value.toLocal();
    String two(int n) => n.toString().padLeft(2, '0');
    return '${local.year}-${two(local.month)}-${two(local.day)} ${two(local.hour)}:${two(local.minute)}';
  }
}

/// 视频进度指示：显示当前位置 / 总时长。
///
/// media_kit 自带的 `MaterialDesktopPositionIndicator` 只在首次
/// `didChangeDependencies` 订阅一次播放器流，且其控件主题的 bar 因
/// `updateShouldNotify` 恒为 false 而永不重建，导致切换视频后仍监听已
/// dispose 的旧播放器、时长不更新。本组件在依赖变化时通过
/// `VideoStateInheritedWidget` 动态获取当前播放器并重新订阅。
class _VideoPositionIndicator extends StatefulWidget {
  const _VideoPositionIndicator();

  @override
  State<_VideoPositionIndicator> createState() =>
      _VideoPositionIndicatorState();
}

class _VideoPositionIndicatorState extends State<_VideoPositionIndicator> {
  Duration _position = Duration.zero;
  Duration _duration = Duration.zero;
  StreamSubscription<Duration>? _positionSub;
  StreamSubscription<Duration>? _durationSub;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final player =
        VideoStateInheritedWidget.maybeOf(
          context,
        )?.state.widget.controller.player;
    _positionSub?.cancel();
    _durationSub?.cancel();
    _positionSub = null;
    _durationSub = null;
    if (player == null) {
      _position = Duration.zero;
      _duration = Duration.zero;
      return;
    }
    _position = player.state.position;
    _duration = player.state.duration;
    _positionSub = player.stream.position.listen((event) {
      if (mounted) setState(() => _position = event);
    });
    _durationSub = player.stream.duration.listen((event) {
      if (mounted) setState(() => _duration = event);
    });
  }

  @override
  void dispose() {
    _positionSub?.cancel();
    _durationSub?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Text(
      '${_label(_position)} / ${_label(_duration)}',
      style: const TextStyle(height: 1.0, fontSize: 12.0, color: Colors.white),
    );
  }

  static String _label(Duration d) {
    String two(int v) => v.toString().padLeft(2, '0');
    final h = d.inHours;
    final m = d.inMinutes.remainder(60);
    final s = d.inSeconds.remainder(60);
    if (h > 0) return '$h:${two(m)}:${two(s)}';
    return '${two(m)}:${two(s)}';
  }
}
