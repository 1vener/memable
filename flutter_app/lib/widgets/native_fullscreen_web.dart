// native_fullscreen_web.dart：Web 平台的全屏守卫实现。
// media_kit 默认回调无条件调用 document.exitFullscreen()，文档未处于全屏时
// 会抛 "TypeError: Failed to execute 'exitFullscreen' on 'Document': Document not active"
// （如浏览器 Esc 已自行退出全屏、再关闭查看器触发 PopScope 时）。
// 这里先检查 fullscreenElement，非全屏直接跳过，消除该错误输出。
// 代码注释使用中文。
import 'dart:js_interop';

import 'package:web/web.dart' as web;

/// 仅在文档处于全屏状态时退出原生全屏。
Future<void> guardedExitNativeFullscreen() async {
  if (web.document.fullscreenElement == null) return;
  try {
    await web.document.exitFullscreen().toDart;
  } catch (_) {
    // 竞态下浏览器可能已退出全屏，忽略即可
  }
}
