// display_preferences.dart：首页显示偏好及其共享通知。
import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

const _layoutOptions = {'masonry', 'square', 'adaptive', 'justified'};

class DisplayPreferences extends ChangeNotifier {
  int libraryGroupDepth = 3;
  double libraryThumbExtent = 180;
  String libraryLayout = 'adaptive';
  int libraryGroupPageSize = 20;
  String libraryLoadMode = 'page'; // page=翻页 / lazy=触底懒加载
  int videoPageSize = 20;
  double videoThumbExtent = 180;
  String videoLayout = 'adaptive';
  int imagePageSize = 20;
  double imageThumbExtent = 180;
  String imageLayout = 'adaptive';

  Future<void> load() async {
    final p = await SharedPreferences.getInstance();
    libraryGroupDepth = (p.getInt('ui.home.library_group_depth') ?? 3).clamp(
      1,
      6,
    );
    libraryThumbExtent = (p.getDouble('ui.home.library_thumb_extent') ?? 180)
        .clamp(120, 320);
    libraryGroupPageSize = (p.getInt('ui.home.library_group_page_size') ?? 20)
        .clamp(1, 100);
    final mode = p.getString('ui.home.library_load_mode');
    libraryLoadMode = (mode == 'lazy') ? 'lazy' : 'page';
    videoPageSize = (p.getInt('ui.home.video_page_size') ?? 20).clamp(1, 100);
    videoThumbExtent = (p.getDouble('ui.home.video_thumb_extent') ?? 180).clamp(
      120,
      320,
    );
    imagePageSize = (p.getInt('ui.home.image_page_size') ?? 20).clamp(1, 100);
    imageThumbExtent = (p.getDouble('ui.home.image_thumb_extent') ?? 180).clamp(
      120,
      320,
    );
    libraryLayout = _loadLayout(p, 'ui.home.library_layout');
    videoLayout = _loadLayout(p, 'ui.home.video_layout');
    imageLayout = _loadLayout(p, 'ui.home.image_layout');
    notifyListeners();
  }

  String _loadLayout(SharedPreferences p, String key) {
    final v = p.getString(key);
    if (v != null && _layoutOptions.contains(v)) return v;
    return 'adaptive';
  }

  Future<void> _set(String key, Object value) async {
    final p = await SharedPreferences.getInstance();
    if (value is int) await p.setInt(key, value);
    if (value is double) await p.setDouble(key, value);
    if (value is String) await p.setString(key, value);
  }

  void setLibraryGroupDepth(int v) {
    libraryGroupDepth = v.clamp(1, 6);
    notifyListeners();
    _set('ui.home.library_group_depth', libraryGroupDepth);
  }

  void setLibraryThumbExtent(double v) {
    libraryThumbExtent = v.clamp(120, 320);
    notifyListeners();
    _set('ui.home.library_thumb_extent', libraryThumbExtent);
  }

  void setLibraryLayout(String value) {
    if (!_layoutOptions.contains(value)) return;
    libraryLayout = value;
    notifyListeners();
    _set('ui.home.library_layout', value);
  }

  void setLibraryGroupPageSize(int v) {
    libraryGroupPageSize = v.clamp(1, 100);
    notifyListeners();
    _set('ui.home.library_group_page_size', libraryGroupPageSize);
  }

  void setLibraryLoadMode(String value) {
    if (value != 'page' && value != 'lazy') return;
    libraryLoadMode = value;
    notifyListeners();
    _set('ui.home.library_load_mode', value);
  }

  void setVideoPageSize(int v) {
    videoPageSize = v;
    notifyListeners();
    _set('ui.home.video_page_size', v);
  }

  void setVideoThumbExtent(double v) {
    videoThumbExtent = v.clamp(120, 320);
    notifyListeners();
    _set('ui.home.video_thumb_extent', videoThumbExtent);
  }

  void setVideoLayout(String value) {
    if (!_layoutOptions.contains(value)) return;
    videoLayout = value;
    notifyListeners();
    _set('ui.home.video_layout', value);
  }

  void setImagePageSize(int v) {
    imagePageSize = v;
    notifyListeners();
    _set('ui.home.image_page_size', v);
  }

  void setImageThumbExtent(double v) {
    imageThumbExtent = v.clamp(120, 320);
    notifyListeners();
    _set('ui.home.image_thumb_extent', imageThumbExtent);
  }

  void setImageLayout(String value) {
    if (!_layoutOptions.contains(value)) return;
    imageLayout = value;
    notifyListeners();
    _set('ui.home.image_layout', value);
  }
}

final displayPreferences = DisplayPreferences();
