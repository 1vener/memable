// backend_manager.dart：桌面端后端进程管理（dart:io 实现）。
// web 端编译时由 backend_manager_stub.dart 替代（条件导入，见 main.dart）。
// 代码注释使用中文。
import 'dart:io';

/// 后端进程管理：启动同目录 server.exe 并在应用退出时终止。
class BackendManager {
  static Process? _process;

  /// 启动与前端 exe 同目录的 server.exe（仅 release 构建调用）。
  static Future<void> startBackend() async {
    try {
      final exeDir = File(Platform.resolvedExecutable).parent.path;
      final serverExe = File('$exeDir${Platform.pathSeparator}server.exe');
      if (!await serverExe.exists()) return;
      // detached：stdout/stderr 脱离管道，避免服务端日志积压管道缓冲导致阻塞；
      // Process 句柄仍保留，退出时可 kill。
      _process = await Process.start(
        serverExe.path,
        const ['-config', 'config.yaml'],
        mode: ProcessStartMode.detached,
      );
    } catch (_) {
      _process = null;
    }
  }

  /// 停止后端进程（应用退出时调用，幂等）。
  static Future<void> stopBackend() async {
    final p = _process;
    _process = null;
    if (p != null) {
      p.kill();
      await p.exitCode.timeout(const Duration(seconds: 2), onTimeout: () => -1);
    }
  }
}
