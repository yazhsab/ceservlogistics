import 'package:cserve_mobile_core/cserve_mobile_core.dart';
import 'package:flutter/material.dart';

class LoginScreen extends StatefulWidget {
  const LoginScreen({
    required this.title,
    required this.subtitle,
    required this.initialBaseUrl,
    required this.onSignIn,
    super.key,
  });

  final String title;
  final String subtitle;
  final String initialBaseUrl;
  final Future<void> Function(String baseUrl, String email, String password)
  onSignIn;

  @override
  State<LoginScreen> createState() => _LoginScreenState();
}

class _LoginScreenState extends State<LoginScreen> {
  final _formKey = GlobalKey<FormState>();
  late final TextEditingController _baseUrl;
  final _email = TextEditingController();
  final _password = TextEditingController();
  bool _submitting = false;
  bool _obscurePassword = true;
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
        setState(() => _error = 'Could not sign in. Check the server address.');
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
                        Icons.local_shipping_outlined,
                        color: Colors.white,
                        size: 28,
                      ),
                    ),
                  ),
                  const SizedBox(height: 24),
                  Text(
                    'CServe ${widget.title}',
                    style: Theme.of(context).textTheme.headlineMedium?.copyWith(
                      fontWeight: FontWeight.w800,
                      color: CServeColors.ink,
                    ),
                  ),
                  const SizedBox(height: 8),
                  Text(
                    widget.subtitle,
                    style: Theme.of(context).textTheme.bodyLarge,
                  ),
                  const SizedBox(height: 28),
                  TextFormField(
                    controller: _baseUrl,
                    keyboardType: TextInputType.url,
                    autocorrect: false,
                    decoration: const InputDecoration(
                      labelText: 'CServe server',
                      hintText: 'https://api.example.com',
                      prefixIcon: Icon(Icons.dns_outlined),
                    ),
                    validator: (value) {
                      final uri = Uri.tryParse(value?.trim() ?? '');
                      return uri != null && uri.hasScheme && uri.host.isNotEmpty
                          ? null
                          : 'Enter a complete http or https address.';
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
                    obscureText: _obscurePassword,
                    autofillHints: const <String>[AutofillHints.password],
                    onFieldSubmitted: (_) => _submit(),
                    decoration: InputDecoration(
                      labelText: 'Password',
                      prefixIcon: const Icon(Icons.lock_outline),
                      suffixIcon: IconButton(
                        tooltip: _obscurePassword
                            ? 'Show password'
                            : 'Hide password',
                        onPressed: () => setState(
                          () => _obscurePassword = !_obscurePassword,
                        ),
                        icon: Icon(
                          _obscurePassword
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
                    _ErrorNotice(message: _error!),
                  ],
                  const SizedBox(height: 20),
                  FilledButton(
                    onPressed: _submitting ? null : _submit,
                    child: Text(_submitting ? 'Signing in…' : 'Sign in'),
                  ),
                  const SizedBox(height: 16),
                  Text(
                    'Local development API: ${widget.initialBaseUrl}',
                    textAlign: TextAlign.center,
                    style: const TextStyle(fontSize: 12, color: Colors.black54),
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

class _ErrorNotice extends StatelessWidget {
  const _ErrorNotice({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) => Semantics(
    liveRegion: true,
    child: Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: CServeColors.danger.withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: CServeColors.danger.withValues(alpha: 0.25)),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          const Icon(Icons.error_outline, color: CServeColors.danger),
          const SizedBox(width: 10),
          Expanded(child: Text(message)),
        ],
      ),
    ),
  );
}
