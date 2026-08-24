import 'package:cserve_mobile_core/cserve_mobile_core.dart';
import 'package:flutter/material.dart';

import 'scan_console_screen.dart';

class ScannerApp extends StatefulWidget {
  const ScannerApp({super.key});

  @override
  State<ScannerApp> createState() => _ScannerAppState();
}

class _ScannerAppState extends State<ScannerApp> {
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
    const scanPermissions = <String>{
      'scan.inbound',
      'scan.outbound',
      'scan.sort',
      'scan.hold',
      'scan.exception',
    };
    if (!result.user.permissions.any(scanPermissions.contains)) {
      await api.logout();
      throw const ApiFailure(
        code: 'SCAN_PERMISSION_REQUIRED',
        message:
            'This account does not have an operational scanning permission.',
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
    title: 'CServe Scanner',
    debugShowCheckedModeBanner: false,
    theme: cserveTheme(),
    home: _loading
        ? const Scaffold(body: Center(child: CircularProgressIndicator()))
        : _profile == null || _api == null
        ? _ScannerLoginScreen(initialBaseUrl: _baseUrl, onSignIn: _signIn)
        : ScanConsoleScreen(
            api: _api!,
            sessionStore: _sessionStore,
            profile: _profile!,
            onSignOut: _signOut,
          ),
  );
}

class _ScannerLoginScreen extends StatefulWidget {
  const _ScannerLoginScreen({
    required this.initialBaseUrl,
    required this.onSignIn,
  });

  final String initialBaseUrl;
  final Future<void> Function(String baseUrl, String email, String password)
  onSignIn;

  @override
  State<_ScannerLoginScreen> createState() => _ScannerLoginScreenState();
}

class _ScannerLoginScreenState extends State<_ScannerLoginScreen> {
  final _formKey = GlobalKey<FormState>();
  late final TextEditingController _baseUrl;
  final _email = TextEditingController();
  final _password = TextEditingController();
  bool _submitting = false;
  bool _obscure = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _baseUrl = TextEditingController(text: widget.initialBaseUrl);
  }

  @override
  void dispose() {
    _baseUrl.dispose();
    _email.dispose();
    _password.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (!_formKey.currentState!.validate()) return;
    setState(() {
      _submitting = true;
      _error = null;
    });
    try {
      await widget.onSignIn(_baseUrl.text, _email.text, _password.text);
    } on ApiFailure catch (error) {
      if (mounted) setState(() => _error = error.message);
    } on Object {
      if (mounted) {
        setState(() => _error = 'Could not reach the CServe server.');
      }
    } finally {
      if (mounted) setState(() => _submitting = false);
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    body: SafeArea(
      child: Center(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(24),
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 480),
            child: Form(
              key: _formKey,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: <Widget>[
                  Align(
                    alignment: Alignment.centerLeft,
                    child: Container(
                      width: 52,
                      height: 52,
                      alignment: Alignment.center,
                      decoration: BoxDecoration(
                        color: CServeColors.primaryDark,
                        borderRadius: BorderRadius.circular(12),
                      ),
                      child: const Icon(
                        Icons.qr_code_scanner,
                        color: Colors.white,
                        size: 28,
                      ),
                    ),
                  ),
                  const SizedBox(height: 24),
                  Text(
                    'CServe Scanner',
                    style: Theme.of(context).textTheme.headlineMedium?.copyWith(
                      fontWeight: FontWeight.w900,
                    ),
                  ),
                  const SizedBox(height: 8),
                  const Text(
                    'Fast facility scanning with offline-safe retries',
                  ),
                  const SizedBox(height: 28),
                  TextFormField(
                    controller: _baseUrl,
                    keyboardType: TextInputType.url,
                    autocorrect: false,
                    decoration: const InputDecoration(
                      labelText: 'CServe server',
                      prefixIcon: Icon(Icons.dns_outlined),
                    ),
                    validator: (value) {
                      final uri = Uri.tryParse(value?.trim() ?? '');
                      return uri != null && uri.hasScheme && uri.host.isNotEmpty
                          ? null
                          : 'Enter a complete server address.';
                    },
                  ),
                  const SizedBox(height: 14),
                  TextFormField(
                    controller: _email,
                    keyboardType: TextInputType.emailAddress,
                    autofillHints: const <String>[AutofillHints.username],
                    decoration: const InputDecoration(
                      labelText: 'Email',
                      prefixIcon: Icon(Icons.person_outline),
                    ),
                    validator: (value) => (value?.contains('@') ?? false)
                        ? null
                        : 'Enter your work email.',
                  ),
                  const SizedBox(height: 14),
                  TextFormField(
                    controller: _password,
                    obscureText: _obscure,
                    autofillHints: const <String>[AutofillHints.password],
                    onFieldSubmitted: (_) => _submit(),
                    decoration: InputDecoration(
                      labelText: 'Password',
                      prefixIcon: const Icon(Icons.lock_outline),
                      suffixIcon: IconButton(
                        tooltip: _obscure ? 'Show password' : 'Hide password',
                        onPressed: () => setState(() => _obscure = !_obscure),
                        icon: Icon(
                          _obscure
                              ? Icons.visibility_outlined
                              : Icons.visibility_off_outlined,
                        ),
                      ),
                    ),
                    validator: (value) => (value?.isNotEmpty ?? false)
                        ? null
                        : 'Enter your password.',
                  ),
                  if (_error != null) ...<Widget>[
                    const SizedBox(height: 14),
                    Container(
                      padding: const EdgeInsets.all(12),
                      decoration: BoxDecoration(
                        color: CServeColors.danger.withValues(alpha: 0.08),
                        borderRadius: BorderRadius.circular(8),
                      ),
                      child: Text(_error!),
                    ),
                  ],
                  const SizedBox(height: 20),
                  FilledButton(
                    onPressed: _submitting ? null : _submit,
                    child: Text(_submitting ? 'Signing in…' : 'Sign in'),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    ),
  );
}
