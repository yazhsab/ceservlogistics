import 'package:cserve_mobile_core/cserve_mobile_core.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

class ScanConsoleScreen extends StatefulWidget {
  const ScanConsoleScreen({
    required this.api,
    required this.sessionStore,
    required this.profile,
    required this.onSignOut,
    super.key,
  });

  final CServeApiClient api;
  final SessionStore sessionStore;
  final UserProfile profile;
  final Future<void> Function() onSignOut;

  @override
  State<ScanConsoleScreen> createState() => _ScanConsoleScreenState();
}

class _ScanConsoleScreenState extends State<ScanConsoleScreen> {
  static const _permissionForMode = <String, String>{
    'RECEIVE': 'scan.inbound',
    'ARRIVAL': 'scan.inbound',
    'DEPARTURE': 'scan.outbound',
    'SORT': 'scan.sort',
    'HOLD': 'scan.hold',
    'RELEASE': 'scan.hold',
    'DAMAGE': 'scan.exception',
    'EXCEPTION': 'scan.exception',
  };

  final OfflineScanQueue _queue = OfflineScanQueue();
  final TextEditingController _barcode = TextEditingController();
  final TextEditingController _operatingUnit = TextEditingController();
  final TextEditingController _reason = TextEditingController();
  final TextEditingController _sortDestination = TextEditingController();
  final FocusNode _barcodeFocus = FocusNode();
  final MobileScannerController _cameraController = MobileScannerController(
    detectionSpeed: DetectionSpeed.noDuplicates,
  );
  final List<_FeedItem> _feed = <_FeedItem>[];

  late final List<String> _availableModes;
  late String _mode;
  bool _cameraEnabled = true;
  bool _busy = false;
  bool _syncing = false;
  int _queuedCount = 0;
  String _lastCameraBarcode = '';
  DateTime? _lastCameraAt;

  @override
  void initState() {
    super.initState();
    _availableModes = _permissionForMode.entries
        .where((entry) => widget.profile.can(entry.value))
        .map((entry) => entry.key)
        .toList(growable: false);
    _mode = _availableModes.first;
    if (widget.profile.operatingUnitIds.length == 1) {
      _operatingUnit.text = widget.profile.operatingUnitIds.first;
    }
    _refreshQueueCount().then((_) => _flushQueue());
  }

  @override
  void dispose() {
    _barcode.dispose();
    _operatingUnit.dispose();
    _reason.dispose();
    _sortDestination.dispose();
    _barcodeFocus.dispose();
    _cameraController.dispose();
    super.dispose();
  }

  bool get _requiresReason =>
      _mode == 'HOLD' || _mode == 'DAMAGE' || _mode == 'EXCEPTION';

  Future<void> _refreshQueueCount() async {
    final queued = await _queue.readAll();
    if (mounted) setState(() => _queuedCount = queued.length);
  }

  Future<void> _submit(String rawBarcode, {bool fromCamera = false}) async {
    final barcode = rawBarcode.trim();
    if (_busy || barcode.length < 4) return;
    if (widget.profile.operatingUnitIds.length > 1 &&
        _operatingUnit.text.trim().isEmpty) {
      _showMessage(
        'Enter the facility for this scanning session.',
        CServeColors.danger,
      );
      return;
    }
    if (_requiresReason && _reason.text.trim().isEmpty) {
      _showMessage('$_mode requires a reason.', CServeColors.danger);
      return;
    }
    setState(() => _busy = true);
    final occurredAt = DateTime.now();
    final eventId = widget.sessionStore.newEventId();
    final queuedScan = QueuedScan(
      eventId: eventId,
      barcode: barcode,
      scanType: _mode,
      occurredAt: occurredAt,
      operatingUnitId: _operatingUnit.text.trim(),
      reason: _requiresReason ? _reason.text.trim() : null,
      sortDestination: _mode == 'SORT' ? _sortDestination.text.trim() : null,
    );
    try {
      final result = await widget.api.performScan(
        barcode: barcode,
        scanType: _mode,
        deviceId: await widget.sessionStore.deviceId(),
        deviceEventId: eventId,
        occurredAt: occurredAt,
        operatingUnitId: queuedScan.operatingUnitId,
        reason: queuedScan.reason,
        sortDestination: queuedScan.sortDestination,
      );
      _recordResult(result);
      await _flushQueue();
    } on ApiFailure catch (error) {
      if (error.retryable) {
        await _queue.enqueue(queuedScan);
        _feed.insert(
          0,
          _FeedItem(
            barcode: barcode,
            outcome: _FeedOutcome.queued,
            message: 'Saved offline. It will retry with the same event ID.',
            timestamp: occurredAt,
          ),
        );
        await _refreshQueueCount();
        _signal(_FeedOutcome.queued);
      } else {
        _feed.insert(
          0,
          _FeedItem(
            barcode: barcode,
            outcome: _FeedOutcome.requestError,
            message: '${error.code}: ${error.message}',
            timestamp: occurredAt,
          ),
        );
        _signal(_FeedOutcome.requestError);
      }
    } finally {
      if (mounted) {
        setState(() {
          _busy = false;
          if (_feed.length > 50) _feed.removeRange(50, _feed.length);
        });
        _barcode.clear();
        if (!fromCamera) _barcodeFocus.requestFocus();
      }
    }
  }

  void _recordResult(ScanResult result) {
    final outcome = switch (result.outcome) {
      ScanOutcome.accepted => _FeedOutcome.accepted,
      ScanOutcome.duplicate => _FeedOutcome.duplicate,
      ScanOutcome.rejected => _FeedOutcome.rejected,
    };
    final message = switch (result.outcome) {
      ScanOutcome.accepted =>
        result.nextAction.isEmpty
            ? '${result.fromStatus} → ${result.toStatus}'
            : result.nextAction,
      ScanOutcome.duplicate => 'Already recorded on this device event.',
      ScanOutcome.rejected =>
        result.rejectionMessage.isEmpty
            ? result.rejectionCode
            : result.rejectionMessage,
    };
    _feed.insert(
      0,
      _FeedItem(
        barcode: result.barcode,
        awb: result.awb,
        outcome: outcome,
        message: message,
        timestamp: result.occurredAt,
      ),
    );
    _signal(outcome);
  }

  Future<void> _flushQueue() async {
    if (_syncing) return;
    setState(() => _syncing = true);
    try {
      final deviceId = await widget.sessionStore.deviceId();
      final queued = await _queue.readAll();
      for (final scan in queued) {
        try {
          final result = await widget.api.replayQueuedScan(scan, deviceId);
          await _queue.remove(scan.eventId);
          _recordResult(result);
        } on ApiFailure {
          break;
        }
      }
      await _refreshQueueCount();
    } finally {
      if (mounted) setState(() => _syncing = false);
    }
  }

  void _onCameraDetect(BarcodeCapture capture) {
    if (_busy) return;
    for (final code in capture.barcodes) {
      final value = code.rawValue?.trim() ?? '';
      if (value.length < 4) continue;
      final now = DateTime.now();
      if (value == _lastCameraBarcode &&
          _lastCameraAt != null &&
          now.difference(_lastCameraAt!) < const Duration(seconds: 3)) {
        return;
      }
      _lastCameraBarcode = value;
      _lastCameraAt = now;
      _submit(value, fromCamera: true);
      return;
    }
  }

  void _signal(_FeedOutcome outcome) {
    switch (outcome) {
      case _FeedOutcome.accepted:
        HapticFeedback.mediumImpact();
        SystemSound.play(SystemSoundType.click);
      case _FeedOutcome.duplicate:
      case _FeedOutcome.queued:
        HapticFeedback.selectionClick();
      case _FeedOutcome.rejected:
      case _FeedOutcome.requestError:
        HapticFeedback.heavyImpact();
        SystemSound.play(SystemSoundType.alert);
    }
  }

  void _showMessage(String message, Color color) {
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(
        SnackBar(
          content: Text(message),
          backgroundColor: color,
          behavior: SnackBarBehavior.floating,
        ),
      );
  }

  @override
  Widget build(BuildContext context) {
    final accepted = _feed
        .where((item) => item.outcome == _FeedOutcome.accepted)
        .length;
    final rejected = _feed
        .where((item) => item.outcome == _FeedOutcome.rejected)
        .length;
    return Scaffold(
      appBar: AppBar(
        title: const Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Text('Scan console', style: TextStyle(fontWeight: FontWeight.w800)),
            Text(
              'CServe Operations',
              style: TextStyle(fontSize: 12, fontWeight: FontWeight.w400),
            ),
          ],
        ),
        actions: <Widget>[
          IconButton(
            tooltip: _syncing ? 'Syncing offline scans' : 'Sync offline scans',
            onPressed: _syncing ? null : _flushQueue,
            icon: Badge(
              isLabelVisible: _queuedCount > 0,
              label: Text('$_queuedCount'),
              child: Icon(_syncing ? Icons.sync : Icons.cloud_sync_outlined),
            ),
          ),
          PopupMenuButton<String>(
            tooltip: 'Account menu',
            onSelected: (value) {
              if (value == 'signout') widget.onSignOut();
            },
            itemBuilder: (_) => <PopupMenuEntry<String>>[
              PopupMenuItem<String>(
                enabled: false,
                child: Text(widget.profile.fullName),
              ),
              const PopupMenuDivider(),
              const PopupMenuItem<String>(
                value: 'signout',
                child: Text('Sign out'),
              ),
            ],
          ),
        ],
      ),
      body: CustomScrollView(
        slivers: <Widget>[
          SliverToBoxAdapter(child: _sessionControls()),
          if (_cameraEnabled)
            SliverToBoxAdapter(
              child: SizedBox(
                height: 240,
                child: Stack(
                  fit: StackFit.expand,
                  children: <Widget>[
                    MobileScanner(
                      controller: _cameraController,
                      errorBuilder: (context, error) => const ColoredBox(
                        color: Colors.black,
                        child: Center(
                          child: Padding(
                            padding: EdgeInsets.all(24),
                            child: Text(
                              'Camera is unavailable. Allow access or switch to the hardware scanner field.',
                              textAlign: TextAlign.center,
                              style: TextStyle(color: Colors.white),
                            ),
                          ),
                        ),
                      ),
                      onDetect: _onCameraDetect,
                    ),
                    Center(
                      child: Container(
                        width: MediaQuery.sizeOf(context).width * 0.78,
                        height: 105,
                        decoration: BoxDecoration(
                          border: Border.all(color: Colors.white, width: 3),
                          borderRadius: BorderRadius.circular(12),
                        ),
                      ),
                    ),
                    Positioned(
                      right: 10,
                      top: 10,
                      child: IconButton.filledTonal(
                        tooltip: 'Toggle torch',
                        onPressed: _cameraController.toggleTorch,
                        icon: const Icon(Icons.flashlight_on_outlined),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          SliverToBoxAdapter(
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: <Widget>[
                  TextField(
                    controller: _barcode,
                    focusNode: _barcodeFocus,
                    autofocus: !_cameraEnabled,
                    autocorrect: false,
                    textCapitalization: TextCapitalization.characters,
                    onSubmitted: _submit,
                    decoration: InputDecoration(
                      labelText: 'Barcode',
                      hintText: 'Scan or enter barcode',
                      prefixIcon: const Icon(Icons.barcode_reader),
                      suffixIcon: IconButton(
                        tooltip: 'Submit barcode',
                        onPressed: _busy ? null : () => _submit(_barcode.text),
                        icon: _busy
                            ? const SizedBox.square(
                                dimension: 20,
                                child: CircularProgressIndicator(
                                  strokeWidth: 2,
                                ),
                              )
                            : const Icon(Icons.arrow_forward),
                      ),
                    ),
                  ),
                  const SizedBox(height: 12),
                  Row(
                    children: <Widget>[
                      Expanded(
                        child: _Counter(
                          label: 'Accepted',
                          value: accepted,
                          color: CServeColors.success,
                        ),
                      ),
                      const SizedBox(width: 8),
                      Expanded(
                        child: _Counter(
                          label: 'Rejected',
                          value: rejected,
                          color: CServeColors.danger,
                        ),
                      ),
                      const SizedBox(width: 8),
                      Expanded(
                        child: _Counter(
                          label: 'Offline',
                          value: _queuedCount,
                          color: CServeColors.warning,
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 20),
                  const Text(
                    'Recent scans',
                    style: TextStyle(fontSize: 18, fontWeight: FontWeight.w800),
                  ),
                ],
              ),
            ),
          ),
          if (_feed.isEmpty)
            const SliverToBoxAdapter(
              child: Padding(
                padding: EdgeInsets.fromLTRB(16, 16, 16, 40),
                child: Center(
                  child: Text(
                    'Ready for the first scan',
                    style: TextStyle(color: Colors.black54),
                  ),
                ),
              ),
            )
          else
            SliverPadding(
              padding: const EdgeInsets.fromLTRB(16, 0, 16, 32),
              sliver: SliverList.separated(
                itemCount: _feed.length,
                separatorBuilder: (_, _) => const SizedBox(height: 8),
                itemBuilder: (context, index) => _FeedTile(item: _feed[index]),
              ),
            ),
        ],
      ),
    );
  }

  Widget _sessionControls() => Container(
    color: Colors.white,
    padding: const EdgeInsets.fromLTRB(16, 12, 16, 14),
    child: Column(
      children: <Widget>[
        Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Expanded(
              child: DropdownButtonFormField<String>(
                initialValue: _mode,
                decoration: const InputDecoration(labelText: 'Operation mode'),
                items: _availableModes
                    .map(
                      (mode) => DropdownMenuItem<String>(
                        value: mode,
                        child: Text(mode),
                      ),
                    )
                    .toList(),
                onChanged: _busy
                    ? null
                    : (value) => setState(() => _mode = value ?? _mode),
              ),
            ),
            const SizedBox(width: 10),
            IconButton.filledTonal(
              tooltip: _cameraEnabled ? 'Use hardware scanner' : 'Use camera',
              onPressed: () {
                setState(() => _cameraEnabled = !_cameraEnabled);
                if (_cameraEnabled) {
                  _cameraController.start();
                } else {
                  _cameraController.stop();
                  _barcodeFocus.requestFocus();
                }
              },
              icon: Icon(
                _cameraEnabled
                    ? Icons.keyboard_outlined
                    : Icons.photo_camera_outlined,
              ),
            ),
          ],
        ),
        const SizedBox(height: 10),
        TextField(
          controller: _operatingUnit,
          readOnly: widget.profile.operatingUnitIds.length == 1,
          decoration: const InputDecoration(
            labelText: 'Facility',
            hintText: 'ou_…',
            prefixIcon: Icon(Icons.warehouse_outlined),
          ),
        ),
        if (_requiresReason) ...<Widget>[
          const SizedBox(height: 10),
          TextField(
            controller: _reason,
            decoration: InputDecoration(
              labelText: 'Reason for $_mode',
              prefixIcon: const Icon(Icons.report_problem_outlined),
            ),
          ),
        ],
        if (_mode == 'SORT') ...<Widget>[
          const SizedBox(height: 10),
          TextField(
            controller: _sortDestination,
            decoration: const InputDecoration(
              labelText: 'Sort destination',
              prefixIcon: Icon(Icons.alt_route),
            ),
          ),
        ],
      ],
    ),
  );
}

enum _FeedOutcome { accepted, duplicate, rejected, queued, requestError }

class _FeedItem {
  const _FeedItem({
    required this.barcode,
    required this.outcome,
    required this.message,
    required this.timestamp,
    this.awb = '',
  });

  final String barcode;
  final String awb;
  final _FeedOutcome outcome;
  final String message;
  final DateTime timestamp;
}

class _FeedTile extends StatelessWidget {
  const _FeedTile({required this.item});

  final _FeedItem item;

  @override
  Widget build(BuildContext context) {
    final (color, icon, label) = switch (item.outcome) {
      _FeedOutcome.accepted => (
        CServeColors.success,
        Icons.check_circle,
        'Accepted',
      ),
      _FeedOutcome.duplicate => (
        CServeColors.warning,
        Icons.content_copy,
        'Duplicate',
      ),
      _FeedOutcome.rejected => (CServeColors.danger, Icons.cancel, 'Rejected'),
      _FeedOutcome.queued => (
        CServeColors.warning,
        Icons.cloud_off,
        'Queued offline',
      ),
      _FeedOutcome.requestError => (
        CServeColors.danger,
        Icons.error,
        'Request error',
      ),
    };
    return Semantics(
      liveRegion: true,
      label: '$label, ${item.barcode}. ${item.message}',
      child: Container(
        padding: const EdgeInsets.all(13),
        decoration: BoxDecoration(
          color: Colors.white,
          borderRadius: BorderRadius.circular(10),
          border: Border(left: BorderSide(color: color, width: 5)),
        ),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Icon(icon, color: color),
            const SizedBox(width: 10),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Row(
                    children: <Widget>[
                      Expanded(
                        child: Text(
                          item.barcode,
                          style: const TextStyle(
                            fontFamily: 'monospace',
                            fontWeight: FontWeight.w800,
                          ),
                        ),
                      ),
                      Text(
                        _time(item.timestamp),
                        style: const TextStyle(
                          fontSize: 11,
                          color: Colors.black45,
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 3),
                  Text(
                    label,
                    style: TextStyle(color: color, fontWeight: FontWeight.w800),
                  ),
                  if (item.message.isNotEmpty)
                    Text(item.message, style: const TextStyle(fontSize: 13)),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  static String _time(DateTime value) {
    final local = value.toLocal();
    return '${local.hour.toString().padLeft(2, '0')}:'
        '${local.minute.toString().padLeft(2, '0')}:'
        '${local.second.toString().padLeft(2, '0')}';
  }
}

class _Counter extends StatelessWidget {
  const _Counter({
    required this.label,
    required this.value,
    required this.color,
  });

  final String label;
  final int value;
  final Color color;

  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 9),
    decoration: BoxDecoration(
      color: color.withValues(alpha: 0.08),
      borderRadius: BorderRadius.circular(8),
    ),
    child: Column(
      children: <Widget>[
        Text(
          '$value',
          style: TextStyle(
            fontSize: 20,
            fontWeight: FontWeight.w900,
            color: color,
          ),
        ),
        Text(label, style: const TextStyle(fontSize: 11)),
      ],
    ),
  );
}
