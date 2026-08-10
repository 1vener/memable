// backend_manager_stub.dart：web 端后端进程管理空实现。
// web 无法拉起本地进程（后端由部署方另行托管），所有方法为 no-op。
// 代码注释使用中文。

/// 后端进程管理（web 空实现）。
class BackendManager {
  static Future<void> startBackend() async {}

  static Future<void> stopBackend() async {}
}
