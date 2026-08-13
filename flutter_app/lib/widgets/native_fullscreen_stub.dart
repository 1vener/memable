// native_fullscreen_stub.dart：非 Web 平台的空实现（桌面端沿用 media_kit 默认全屏行为）。
// 代码注释使用中文。

/// 桌面端无操作，全屏退出由 media_kit 默认回调（窗口全屏 MethodChannel）处理。
Future<void> guardedExitNativeFullscreen() async {}
