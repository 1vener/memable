// native_fullscreen.dart：跨平台全屏守卫出口。
// Web 端实现见 native_fullscreen_web.dart（仅文档处于全屏时才退出原生全屏，
// 避免 media_kit 默认回调在非全屏下调用 exitFullscreen 抛 "Document not active"）；
// 其它平台为无操作（保持 media_kit 默认行为）。
// 代码注释使用中文。
export 'native_fullscreen_stub.dart'
    if (dart.library.js_interop) 'native_fullscreen_web.dart';
