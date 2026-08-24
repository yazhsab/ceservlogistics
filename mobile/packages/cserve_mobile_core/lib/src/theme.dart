import 'package:flutter/material.dart';

abstract final class CServeColors {
  static const primary = Color(0xFF1A5C4B);
  static const primaryDark = Color(0xFF123F36);
  static const background = Color(0xFFF5F7F8);
  static const surfaceMuted = Color(0xFFE9EEF0);
  static const border = Color(0xFFD9E0E4);
  static const success = Color(0xFF208A61);
  static const warning = Color(0xFFC77A08);
  static const danger = Color(0xFFC63737);
  static const ink = Color(0xFF122033);
}

ThemeData cserveTheme() {
  final scheme = ColorScheme.fromSeed(
    seedColor: CServeColors.primary,
    brightness: Brightness.light,
    primary: CServeColors.primary,
    surface: Colors.white,
    error: CServeColors.danger,
  );
  return ThemeData(
    useMaterial3: true,
    colorScheme: scheme,
    scaffoldBackgroundColor: CServeColors.background,
    appBarTheme: const AppBarTheme(
      centerTitle: false,
      backgroundColor: Colors.white,
      foregroundColor: CServeColors.ink,
      elevation: 0,
      scrolledUnderElevation: 1,
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: Colors.white,
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(10),
        borderSide: const BorderSide(color: CServeColors.border),
      ),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(10),
        borderSide: const BorderSide(color: CServeColors.border),
      ),
      contentPadding: const EdgeInsets.symmetric(horizontal: 14, vertical: 14),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        minimumSize: const Size(48, 52),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
        textStyle: const TextStyle(fontSize: 16, fontWeight: FontWeight.w700),
      ),
    ),
    cardTheme: CardThemeData(
      color: Colors.white,
      elevation: 0,
      margin: EdgeInsets.zero,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(12),
        side: const BorderSide(color: CServeColors.border),
      ),
    ),
    dividerColor: CServeColors.border,
  );
}
