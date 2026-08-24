import 'package:cserve_mobile_core/cserve_mobile_core.dart';
import 'package:flutter/material.dart';

import 'login_screen.dart';
import 'runs_screen.dart';

class DriverApp extends StatefulWidget {
  const DriverApp({super.key});

  @override
  State<DriverApp> createState() => _DriverAppState();
}

class _DriverAppState extends State<DriverApp> {
  final SessionStore _sessionStore = SessionStore();
  CServeApiClient? _api;
  UserProfile? _profile;
  String _baseUrl = '';
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    _restore();
  }

  Future<void> _restore() async {
    final baseUrl = await _sessionStore.readBaseUrl();
    final storedProfile = await _sessionStore.readProfile();
    final api = CServeApiClient(baseUrl: baseUrl, sessionStore: _sessionStore);
    UserProfile? profile = storedProfile;
    if (storedProfile != null) {
      try {
        profile = await api.me();
      } on ApiFailure catch (error) {
        if (error.statusCode == 401 || error.code == 'SESSION_EXPIRED') {
          profile = null;
        }
      }
    }
    if (!mounted) return;
    setState(() {
      _baseUrl = baseUrl;
      _api = api;
      _profile = profile;
      _loading = false;
    });
  }

  Future<void> _signIn(String baseUrl, String email, String password) async {
    await _sessionStore.saveBaseUrl(baseUrl);
    final api = CServeApiClient(baseUrl: baseUrl, sessionStore: _sessionStore);
    final result = await api.login(email: email, password: password);
    if (!result.user.can('delivery.read')) {
      await api.logout();
      throw const ApiFailure(
        code: 'DELIVERY_PERMISSION_REQUIRED',
        message: 'This account does not have access to delivery runs.',
      );
    }
    if (!mounted) return;
    setState(() {
      _api = api;
      _profile = result.user;
      _baseUrl = baseUrl;
    });
  }

  Future<void> _signOut() async {
    await _api?.logout();
    if (!mounted) return;
    setState(() => _profile = null);
  }

  @override
  Widget build(BuildContext context) => MaterialApp(
    title: 'CServe Driver',
    debugShowCheckedModeBanner: false,
    theme: cserveTheme(),
    home: _loading
        ? const _StartupScreen()
        : _profile == null || _api == null
        ? LoginScreen(
            title: 'Driver',
            subtitle: 'Assigned runs, parcel checks and proof of delivery',
            initialBaseUrl: _baseUrl,
            onSignIn: _signIn,
          )
        : RunsScreen(
            api: _api!,
            sessionStore: _sessionStore,
            profile: _profile!,
            onSignOut: _signOut,
          ),
  );
}

class _StartupScreen extends StatelessWidget {
  const _StartupScreen();

  @override
  Widget build(BuildContext context) => Scaffold(
    body: Center(
      child: Semantics(
        label: 'Loading CServe Driver',
        child: const CircularProgressIndicator(),
      ),
    ),
  );
}
