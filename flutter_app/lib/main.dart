// main.dart：Flutter 应用入口
// 代码注释使用中文
import 'package:flutter/material.dart';
import 'screens/home_screen.dart';
import 'services/api_service.dart';

void main() {
  runApp(const MemableApp());
}

class MemableApp extends StatelessWidget {
  const MemableApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'memable',
      debugShowCheckedModeBanner: false,
      theme: ThemeData(
        colorScheme: ColorScheme.fromSeed(seedColor: Colors.indigo),
        useMaterial3: true,
      ),
      home: HomeScreen(api: ApiService()),
    );
  }
}
