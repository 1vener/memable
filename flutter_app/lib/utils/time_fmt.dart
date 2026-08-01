/// 时间显示工具：把后端返回的 UTC 时间统一转换为本地时间显示。
library;

/// 解析后端时间并格式化为本地时间 `yyyy-MM-dd HH:mm`。
/// 支持 RFC3339（含 Z/时区偏移）与 SQLite 空格格式
/// （`yyyy-MM-dd HH:mm:ss`，无时区信息时按 UTC 解析）。
String formatLocalTime(String? raw) {
  if (raw == null || raw.trim().isEmpty) return '';
  final s = raw.trim();
  final t = s.replaceFirst(' ', 'T');
  final hasZone = RegExp(r'[zZ]$|[+-]\d{2}:\d{2}$').hasMatch(t);
  final dt = DateTime.tryParse(hasZone ? t : '$t' 'Z');
  if (dt == null) return s;
  final l = dt.toLocal();
  String two(int v) => v.toString().padLeft(2, '0');
  return '${l.year}-${two(l.month)}-${two(l.day)} ${two(l.hour)}:${two(l.minute)}';
}
