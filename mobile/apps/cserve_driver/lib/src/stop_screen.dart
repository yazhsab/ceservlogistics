import 'dart:io';

import 'package:cserve_mobile_core/cserve_mobile_core.dart';
import 'package:flutter/material.dart';
import 'package:image_picker/image_picker.dart';
import 'package:mobile_scanner/mobile_scanner.dart';
import 'package:url_launcher/url_launcher.dart';

import 'runs_screen.dart';

class StopScreen extends StatefulWidget {
  const StopScreen({
    required this.api,
    required this.sessionStore,
    required this.profile,
    required this.runId,
    required this.stop,
    super.key,
  });

  final CServeApiClient api;
  final SessionStore sessionStore;
  final UserProfile profile;
  final String runId;
  final DeliveryStop stop;

  @override
  State<StopScreen> createState() => _StopScreenState();
}

class _StopScreenState extends State<StopScreen> {
  late Future<ShipmentDetail> _shipment;
  final Set<String> _verifiedPieceIds = <String>{};
  final TextEditingController _manualBarcode = TextEditingController();

  @override
  void initState() {
    super.initState();
    _shipment = widget.api.getShipment(widget.stop.shipmentId);
  }

  @override
  void dispose() {
    _manualBarcode.dispose();
    super.dispose();
  }

  String _normalize(String value) =>
      value.toUpperCase().replaceAll(RegExp(r'\s+'), '');

  void _verifyBarcode(ShipmentDetail shipment, String rawBarcode) {
    final barcode = _normalize(rawBarcode);
    if (barcode.isEmpty) return;
    ShipmentPiece? matched;
    for (final piece in shipment.packages) {
      final candidates = <String>{
        _normalize(piece.pieceBarcode),
        if (piece.reference.isNotEmpty) _normalize(piece.reference),
      };
      if (candidates.contains(barcode)) {
        matched = piece;
        break;
      }
    }
    if (matched == null &&
        shipment.packages.length == 1 &&
        _normalize(shipment.awb) == barcode) {
      matched = shipment.packages.first;
    }
    if (matched == null) {
      _feedback(
        'Wrong parcel. This barcode does not belong to ${shipment.awb}.',
        CServeColors.danger,
      );
      return;
    }
    if (_verifiedPieceIds.contains(matched.id)) {
      _feedback(
        'Piece ${matched.sequence} was already verified.',
        CServeColors.warning,
      );
      return;
    }
    setState(() => _verifiedPieceIds.add(matched!.id));
    _manualBarcode.clear();
    _feedback(
      'Piece ${matched.sequence} verified — '
      '${_verifiedPieceIds.length}/${shipment.packages.length}.',
      CServeColors.success,
    );
  }

  void _feedback(String message, Color color) {
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

  Future<void> _scanWithCamera(ShipmentDetail shipment) async {
    final value = await Navigator.of(context).push<String>(
      MaterialPageRoute<String>(
        builder: (_) => const PieceScannerScreen(),
        fullscreenDialog: true,
      ),
    );
    if (value != null && mounted) _verifyBarcode(shipment, value);
  }

  Future<void> _openUrl(Uri uri) async {
    if (!await launchUrl(uri, mode: LaunchMode.externalApplication) &&
        mounted) {
      _feedback('No app is available for this action.', CServeColors.danger);
    }
  }

  @override
  Widget build(BuildContext context) => FutureBuilder<ShipmentDetail>(
    future: _shipment,
    builder: (context, snapshot) => Scaffold(
      appBar: AppBar(
        title: Text('Stop ${widget.stop.stopSequence}'),
        actions: <Widget>[
          Padding(
            padding: const EdgeInsets.only(right: 12),
            child: Center(child: StatusPill(status: widget.stop.status)),
          ),
        ],
      ),
      body: snapshot.connectionState == ConnectionState.waiting
          ? const Center(child: CircularProgressIndicator())
          : snapshot.hasError
          ? _loadError(snapshot.error)
          : _content(snapshot.data!),
    ),
  );

  Widget _loadError(Object? error) => Center(
    child: Padding(
      padding: const EdgeInsets.all(24),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: <Widget>[
          const Icon(Icons.inventory_2_outlined, size: 44),
          const SizedBox(height: 12),
          Text(
            error is ApiFailure
                ? error.message
                : 'Could not load the shipment pieces.',
            textAlign: TextAlign.center,
          ),
          const SizedBox(height: 16),
          FilledButton.tonal(
            onPressed: () => setState(
              () => _shipment = widget.api.getShipment(widget.stop.shipmentId),
            ),
            child: const Text('Try again'),
          ),
        ],
      ),
    ),
  );

  Widget _content(ShipmentDetail shipment) {
    final total = shipment.packages.length;
    final allPiecesVerified = total > 0 && _verifiedPieceIds.length == total;
    final canComplete =
        widget.profile.can('delivery.complete') &&
        widget.stop.status == 'OUT_FOR_DELIVERY';
    return Column(
      children: <Widget>[
        Expanded(
          child: ListView(
            padding: const EdgeInsets.fromLTRB(16, 16, 16, 120),
            children: <Widget>[
              Text(
                widget.stop.recipientName,
                style: const TextStyle(
                  fontSize: 24,
                  fontWeight: FontWeight.w800,
                ),
              ),
              const SizedBox(height: 4),
              Text(
                widget.stop.awb,
                style: const TextStyle(
                  fontFamily: 'monospace',
                  fontWeight: FontWeight.w700,
                  color: Colors.black54,
                ),
              ),
              const SizedBox(height: 18),
              Card(
                child: Padding(
                  padding: const EdgeInsets.all(16),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      Row(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: <Widget>[
                          const Icon(Icons.location_on_outlined),
                          const SizedBox(width: 10),
                          Expanded(child: Text(widget.stop.address)),
                        ],
                      ),
                      const SizedBox(height: 14),
                      Row(
                        children: <Widget>[
                          Expanded(
                            child: OutlinedButton.icon(
                              onPressed: widget.stop.recipientPhone.isEmpty
                                  ? null
                                  : () => _openUrl(
                                      Uri(
                                        scheme: 'tel',
                                        path: widget.stop.recipientPhone,
                                      ),
                                    ),
                              icon: const Icon(Icons.phone_outlined),
                              label: const Text('Call'),
                            ),
                          ),
                          const SizedBox(width: 10),
                          Expanded(
                            child: OutlinedButton.icon(
                              onPressed:
                                  widget.stop.latitude == null ||
                                      widget.stop.longitude == null
                                  ? null
                                  : () => _openUrl(
                                      Uri.https('www.google.com', '/maps/dir/', <
                                        String,
                                        String
                                      >{
                                        'api': '1',
                                        'destination':
                                            '${widget.stop.latitude},${widget.stop.longitude}',
                                      }),
                                    ),
                              icon: const Icon(Icons.navigation_outlined),
                              label: const Text('Navigate'),
                            ),
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
              ),
              if (widget.stop.codAmountMinor > 0) ...<Widget>[
                const SizedBox(height: 12),
                Container(
                  padding: const EdgeInsets.all(16),
                  decoration: BoxDecoration(
                    color: CServeColors.warning.withValues(alpha: 0.1),
                    borderRadius: BorderRadius.circular(12),
                    border: Border.all(
                      color: CServeColors.warning.withValues(alpha: 0.3),
                    ),
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      const Text(
                        'COLLECT EXACTLY',
                        style: TextStyle(
                          color: CServeColors.warning,
                          fontSize: 12,
                          fontWeight: FontWeight.w800,
                        ),
                      ),
                      const SizedBox(height: 4),
                      Text(
                        money(widget.stop.codAmountMinor, widget.stop.currency),
                        style: const TextStyle(
                          fontSize: 28,
                          fontWeight: FontWeight.w900,
                        ),
                      ),
                    ],
                  ),
                ),
              ],
              const SizedBox(height: 24),
              Row(
                children: <Widget>[
                  const Expanded(
                    child: Text(
                      'Verify every parcel',
                      style: TextStyle(
                        fontSize: 20,
                        fontWeight: FontWeight.w800,
                      ),
                    ),
                  ),
                  Text(
                    '${_verifiedPieceIds.length}/$total',
                    style: TextStyle(
                      fontSize: 20,
                      fontWeight: FontWeight.w900,
                      color: allPiecesVerified
                          ? CServeColors.success
                          : CServeColors.warning,
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 5),
              const Text(
                'A split shipment is one stop, but each carton must be present.',
                style: TextStyle(color: Colors.black54),
              ),
              const SizedBox(height: 12),
              if (shipment.packages.isEmpty)
                const _InlineWarning(
                  message:
                      'No package barcodes were returned. Delivery cannot be verified safely.',
                )
              else
                ...shipment.packages.map(
                  (piece) => Padding(
                    padding: const EdgeInsets.only(bottom: 8),
                    child: _PieceRow(
                      piece: piece,
                      verified: _verifiedPieceIds.contains(piece.id),
                    ),
                  ),
                ),
              const SizedBox(height: 8),
              TextField(
                controller: _manualBarcode,
                autocorrect: false,
                textCapitalization: TextCapitalization.characters,
                onSubmitted: (value) => _verifyBarcode(shipment, value),
                decoration: InputDecoration(
                  labelText: 'Scan or enter package barcode',
                  hintText: 'Hardware scanner input',
                  prefixIcon: const Icon(Icons.barcode_reader),
                  suffixIcon: IconButton(
                    tooltip: 'Verify barcode',
                    onPressed: () =>
                        _verifyBarcode(shipment, _manualBarcode.text),
                    icon: const Icon(Icons.arrow_forward),
                  ),
                ),
              ),
              const SizedBox(height: 10),
              FilledButton.tonalIcon(
                onPressed: () => _scanWithCamera(shipment),
                icon: const Icon(Icons.qr_code_scanner),
                label: const Text('Scan with camera'),
              ),
            ],
          ),
        ),
        if (canComplete)
          SafeArea(
            top: false,
            child: Container(
              padding: const EdgeInsets.fromLTRB(16, 12, 16, 12),
              decoration: const BoxDecoration(
                color: Colors.white,
                border: Border(top: BorderSide(color: CServeColors.border)),
              ),
              child: Row(
                children: <Widget>[
                  Expanded(
                    child: OutlinedButton(
                      onPressed: () => _openAttempt(shipment, delivered: false),
                      style: OutlinedButton.styleFrom(
                        minimumSize: const Size(48, 52),
                        foregroundColor: CServeColors.danger,
                      ),
                      child: const Text('Exception'),
                    ),
                  ),
                  const SizedBox(width: 10),
                  Expanded(
                    flex: 2,
                    child: FilledButton.icon(
                      onPressed: allPiecesVerified
                          ? () => _openAttempt(shipment, delivered: true)
                          : null,
                      icon: const Icon(Icons.check_circle_outline),
                      label: Text(
                        allPiecesVerified
                            ? 'Complete delivery'
                            : 'Scan all $total pieces',
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ),
      ],
    );
  }

  Future<void> _openAttempt(ShipmentDetail _, {required bool delivered}) async {
    final completed = await Navigator.of(context).push<bool>(
      MaterialPageRoute<bool>(
        builder: (_) => DeliveryAttemptScreen(
          api: widget.api,
          sessionStore: widget.sessionStore,
          profile: widget.profile,
          runId: widget.runId,
          stop: widget.stop,
          delivered: delivered,
        ),
      ),
    );
    if (completed == true && mounted) Navigator.of(context).pop();
  }
}

class _PieceRow extends StatelessWidget {
  const _PieceRow({required this.piece, required this.verified});

  final ShipmentPiece piece;
  final bool verified;

  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.all(13),
    decoration: BoxDecoration(
      color: verified
          ? CServeColors.success.withValues(alpha: 0.08)
          : Colors.white,
      borderRadius: BorderRadius.circular(10),
      border: Border.all(
        color: verified ? CServeColors.success : CServeColors.border,
      ),
    ),
    child: Row(
      children: <Widget>[
        Icon(
          verified ? Icons.check_circle : Icons.radio_button_unchecked,
          color: verified ? CServeColors.success : Colors.black38,
        ),
        const SizedBox(width: 10),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Text(
                'Piece ${piece.sequence}',
                style: const TextStyle(fontWeight: FontWeight.w800),
              ),
              Text(
                piece.pieceBarcode.isNotEmpty
                    ? piece.pieceBarcode
                    : piece.reference,
                style: const TextStyle(fontFamily: 'monospace', fontSize: 12),
              ),
            ],
          ),
        ),
        Text(verified ? 'Verified' : 'Pending'),
      ],
    ),
  );
}

class _InlineWarning extends StatelessWidget {
  const _InlineWarning({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.all(12),
    decoration: BoxDecoration(
      color: CServeColors.warning.withValues(alpha: 0.1),
      borderRadius: BorderRadius.circular(8),
    ),
    child: Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        const Icon(Icons.warning_amber, color: CServeColors.warning),
        const SizedBox(width: 8),
        Expanded(child: Text(message)),
      ],
    ),
  );
}

class _CameraUnavailable extends StatelessWidget {
  const _CameraUnavailable();

  @override
  Widget build(BuildContext context) => const ColoredBox(
    color: Colors.black,
    child: Center(
      child: Padding(
        padding: EdgeInsets.all(24),
        child: Text(
          'Camera is unavailable. Allow camera access or use the manual barcode field.',
          textAlign: TextAlign.center,
          style: TextStyle(color: Colors.white),
        ),
      ),
    ),
  );
}

class PieceScannerScreen extends StatefulWidget {
  const PieceScannerScreen({super.key});

  @override
  State<PieceScannerScreen> createState() => _PieceScannerScreenState();
}

class _PieceScannerScreenState extends State<PieceScannerScreen> {
  bool _handled = false;

  @override
  Widget build(BuildContext context) => Scaffold(
    backgroundColor: Colors.black,
    appBar: AppBar(
      title: const Text('Scan package'),
      backgroundColor: Colors.black,
      foregroundColor: Colors.white,
    ),
    body: Stack(
      fit: StackFit.expand,
      children: <Widget>[
        MobileScanner(
          errorBuilder: (context, error) => const _CameraUnavailable(),
          onDetect: (capture) {
            if (_handled) return;
            for (final barcode in capture.barcodes) {
              final value = barcode.rawValue;
              if (value != null && value.trim().isNotEmpty) {
                _handled = true;
                Navigator.of(context).pop(value);
                break;
              }
            }
          },
        ),
        Center(
          child: Container(
            width: MediaQuery.sizeOf(context).width * 0.82,
            height: 150,
            decoration: BoxDecoration(
              border: Border.all(color: Colors.white, width: 3),
              borderRadius: BorderRadius.circular(14),
            ),
          ),
        ),
        const Positioned(
          left: 24,
          right: 24,
          bottom: 36,
          child: Text(
            'Place one package barcode inside the frame',
            textAlign: TextAlign.center,
            style: TextStyle(
              color: Colors.white,
              fontSize: 16,
              fontWeight: FontWeight.w700,
            ),
          ),
        ),
      ],
    ),
  );
}

class DeliveryAttemptScreen extends StatefulWidget {
  const DeliveryAttemptScreen({
    required this.api,
    required this.sessionStore,
    required this.profile,
    required this.runId,
    required this.stop,
    required this.delivered,
    super.key,
  });

  final CServeApiClient api;
  final SessionStore sessionStore;
  final UserProfile profile;
  final String runId;
  final DeliveryStop stop;
  final bool delivered;

  @override
  State<DeliveryAttemptScreen> createState() => _DeliveryAttemptScreenState();
}

class _DeliveryAttemptScreenState extends State<DeliveryAttemptScreen> {
  final _formKey = GlobalKey<FormState>();
  final _recipientName = TextEditingController();
  final _otp = TextEditingController();
  final _remarks = TextEditingController();
  String _relationship = 'SELF';
  String _outcome = 'FAILED';
  String _failureReason = '';
  String _codPaymentMode = 'CASH';
  XFile? _photo;
  bool _submitting = false;
  String? _error;
  String? _eventId;
  late Future<List<NdrReason>> _reasons;

  @override
  void initState() {
    super.initState();
    _recipientName.text = widget.stop.recipientName;
    _outcome = widget.delivered ? 'DELIVERED' : 'FAILED';
    _reasons = widget.api.listNdrReasons();
  }

  @override
  void dispose() {
    _recipientName.dispose();
    _otp.dispose();
    _remarks.dispose();
    super.dispose();
  }

  Future<void> _pickPhoto() async {
    final image = await ImagePicker().pickImage(
      source: ImageSource.camera,
      imageQuality: 80,
      maxWidth: 1800,
    );
    if (image != null && mounted) setState(() => _photo = image);
  }

  Future<void> _issueOtp() async {
    try {
      final result = await widget.api.issueDeliveryOtp(widget.stop.awb);
      if (!mounted) return;
      await showDialog<void>(
        context: context,
        barrierDismissible: false,
        builder: (context) => AlertDialog(
          icon: const Icon(Icons.password),
          title: const Text('One-time code'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              Text(
                result['otp']?.toString() ?? '',
                style: const TextStyle(
                  fontSize: 32,
                  fontWeight: FontWeight.w900,
                  letterSpacing: 6,
                ),
              ),
              const SizedBox(height: 10),
              Text('For ${result['sentToMasked'] ?? 'the recipient'}'),
              const SizedBox(height: 8),
              const Text(
                'Shown once. Do not save or screenshot this code.',
                textAlign: TextAlign.center,
              ),
            ],
          ),
          actions: <Widget>[
            FilledButton(
              onPressed: () => Navigator.of(context).pop(),
              child: const Text('I understand'),
            ),
          ],
        ),
      );
    } on ApiFailure catch (error) {
      if (mounted) setState(() => _error = error.message);
    }
  }

  Future<void> _submit() async {
    if (!_formKey.currentState!.validate()) return;
    if (_outcome == 'DELIVERED' && _photo == null) {
      setState(
        () => _error = 'Take a delivery photo before completing this stop.',
      );
      return;
    }
    setState(() {
      _submitting = true;
      _error = null;
    });
    try {
      final deviceId = await widget.sessionStore.deviceId();
      _eventId ??= widget.sessionStore.newEventId();
      final body = <String, dynamic>{
        'barcode': widget.stop.awb,
        'outcome': _outcome,
        'runId': widget.runId,
        'occurredAt': DateTime.now().toUtc().toIso8601String(),
        if (_remarks.text.trim().isNotEmpty) 'remarks': _remarks.text.trim(),
        if (_outcome == 'DELIVERED') ...<String, dynamic>{
          'recipientName': _recipientName.text.trim(),
          'recipientRelationship': _relationship,
          if (_otp.text.trim().isNotEmpty) 'otp': _otp.text.trim(),
          if (widget.stop.codAmountMinor > 0) ...<String, dynamic>{
            'codCollectedMinor': widget.stop.codAmountMinor,
            'codPaymentMode': _codPaymentMode,
          },
        } else ...<String, dynamic>{'failureReasonCode': _failureReason},
      };
      final result = await widget.api.recordDeliveryAttempt(
        body: body,
        deviceId: deviceId,
        deviceEventId: _eventId!,
        operatingUnitId: widget.profile.operatingUnitIds.length == 1
            ? widget.profile.operatingUnitIds.first
            : '',
      );
      if (_outcome == 'DELIVERED' && _photo != null) {
        try {
          await widget.api.submitDeliveryPhoto(
            barcode: widget.stop.awb,
            recipientName: _recipientName.text.trim(),
            recipientRelationship: _relationship,
            photoPath: _photo!.path,
            deviceId: deviceId,
            operatingUnitId: widget.profile.operatingUnitIds.length == 1
                ? widget.profile.operatingUnitIds.first
                : '',
            remarks: _remarks.text.trim(),
          );
        } on ApiFailure catch (podError) {
          if (!mounted) return;
          setState(() {
            _error =
                'Delivery is complete, but proof upload failed: ${podError.message}';
            _submitting = false;
          });
          return;
        }
      }
      if (!mounted) return;
      await showDialog<void>(
        context: context,
        barrierDismissible: false,
        builder: (context) => AlertDialog(
          icon: Icon(
            _outcome == 'DELIVERED'
                ? Icons.check_circle
                : Icons.assignment_late_outlined,
            color: _outcome == 'DELIVERED'
                ? CServeColors.success
                : CServeColors.warning,
            size: 42,
          ),
          title: Text(
            _outcome == 'DELIVERED'
                ? 'Delivery completed'
                : 'Exception recorded',
          ),
          content: Text(result.nextAction),
          actions: <Widget>[
            FilledButton(
              onPressed: () => Navigator.of(context).pop(),
              child: const Text('Back to run'),
            ),
          ],
        ),
      );
      if (mounted) Navigator.of(context).pop(true);
    } on ApiFailure catch (error) {
      if (mounted) setState(() => _error = error.message);
    } finally {
      if (mounted) setState(() => _submitting = false);
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: Text(widget.delivered ? 'Complete delivery' : 'Record exception'),
    ),
    body: Form(
      key: _formKey,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
        children: <Widget>[
          Text(
            widget.stop.awb,
            style: const TextStyle(
              fontFamily: 'monospace',
              fontWeight: FontWeight.w700,
            ),
          ),
          const SizedBox(height: 16),
          if (!widget.delivered)
            SegmentedButton<String>(
              segments: const <ButtonSegment<String>>[
                ButtonSegment<String>(
                  value: 'FAILED',
                  label: Text('Failed'),
                  icon: Icon(Icons.close),
                ),
                ButtonSegment<String>(
                  value: 'RESCHEDULED',
                  label: Text('Reschedule'),
                  icon: Icon(Icons.event_repeat),
                ),
              ],
              selected: <String>{_outcome},
              onSelectionChanged: (value) =>
                  setState(() => _outcome = value.first),
            ),
          if (_outcome == 'DELIVERED')
            ..._deliveryFields()
          else
            ..._failureFields(),
          const SizedBox(height: 14),
          TextFormField(
            controller: _remarks,
            maxLines: 3,
            decoration: const InputDecoration(labelText: 'Remarks (optional)'),
          ),
          if (_error != null) ...<Widget>[
            const SizedBox(height: 14),
            _InlineWarning(message: _error!),
          ],
          const SizedBox(height: 20),
          FilledButton(
            onPressed: _submitting ? null : _submit,
            style: _outcome == 'DELIVERED'
                ? null
                : FilledButton.styleFrom(backgroundColor: CServeColors.danger),
            child: Text(
              _submitting
                  ? 'Saving…'
                  : _outcome == 'DELIVERED'
                  ? 'Confirm delivered'
                  : 'Record exception',
            ),
          ),
        ],
      ),
    ),
  );

  List<Widget> _deliveryFields() => <Widget>[
    const SizedBox(height: 18),
    TextFormField(
      controller: _recipientName,
      decoration: const InputDecoration(labelText: 'Received by'),
      validator: (value) => (value?.trim().isNotEmpty ?? false)
          ? null
          : 'Enter the recipient name.',
    ),
    const SizedBox(height: 14),
    DropdownButtonFormField<String>(
      initialValue: _relationship,
      decoration: const InputDecoration(labelText: 'Relationship'),
      items:
          const <String>[
                'SELF',
                'FAMILY',
                'NEIGHBOUR',
                'SECURITY',
                'RECEPTION',
                'COLLEAGUE',
                'OTHER',
              ]
              .map(
                (value) =>
                    DropdownMenuItem<String>(value: value, child: Text(value)),
              )
              .toList(),
      onChanged: (value) => setState(() => _relationship = value ?? 'SELF'),
    ),
    const SizedBox(height: 14),
    Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Expanded(
          child: TextFormField(
            controller: _otp,
            keyboardType: TextInputType.number,
            decoration: const InputDecoration(labelText: 'OTP (if issued)'),
          ),
        ),
        if (widget.profile.can('delivery.otp_issue')) ...<Widget>[
          const SizedBox(width: 10),
          Padding(
            padding: const EdgeInsets.only(top: 3),
            child: OutlinedButton(
              onPressed: _issueOtp,
              style: OutlinedButton.styleFrom(minimumSize: const Size(82, 52)),
              child: const Text('Issue'),
            ),
          ),
        ],
      ],
    ),
    if (widget.stop.codAmountMinor > 0) ...<Widget>[
      const SizedBox(height: 14),
      Container(
        padding: const EdgeInsets.all(14),
        decoration: BoxDecoration(
          color: CServeColors.warning.withValues(alpha: 0.1),
          borderRadius: BorderRadius.circular(10),
        ),
        child: Text(
          'Confirm ${money(widget.stop.codAmountMinor, widget.stop.currency)} collected',
          style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w800),
        ),
      ),
      const SizedBox(height: 14),
      DropdownButtonFormField<String>(
        initialValue: _codPaymentMode,
        decoration: const InputDecoration(labelText: 'COD payment mode'),
        items:
            const <String>[
                  'CASH',
                  'UPI',
                  'CARD',
                  'WALLET',
                  'BANK_TRANSFER',
                  'CHEQUE',
                ]
                .map(
                  (value) => DropdownMenuItem<String>(
                    value: value,
                    child: Text(value),
                  ),
                )
                .toList(),
        onChanged: (value) => setState(() => _codPaymentMode = value ?? 'CASH'),
      ),
    ],
    const SizedBox(height: 14),
    OutlinedButton.icon(
      onPressed: _pickPhoto,
      style: OutlinedButton.styleFrom(minimumSize: const Size(48, 52)),
      icon: Icon(
        _photo == null ? Icons.camera_alt_outlined : Icons.check_circle,
      ),
      label: Text(
        _photo == null ? 'Take delivery photo' : 'Delivery photo captured',
      ),
    ),
    if (_photo != null) ...<Widget>[
      const SizedBox(height: 10),
      ClipRRect(
        borderRadius: BorderRadius.circular(10),
        child: Image.file(File(_photo!.path), height: 180, fit: BoxFit.cover),
      ),
    ],
  ];

  List<Widget> _failureFields() => <Widget>[
    const SizedBox(height: 18),
    FutureBuilder<List<NdrReason>>(
      future: _reasons,
      builder: (context, snapshot) {
        if (snapshot.hasError) {
          return Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              const _InlineWarning(
                message:
                    'Could not load the approved delivery exception reasons.',
              ),
              const SizedBox(height: 8),
              OutlinedButton.icon(
                onPressed: () =>
                    setState(() => _reasons = widget.api.listNdrReasons()),
                icon: const Icon(Icons.refresh),
                label: const Text('Retry reasons'),
              ),
            ],
          );
        }
        final reasons = snapshot.data ?? const <NdrReason>[];
        return DropdownButtonFormField<String>(
          key: ValueKey<int>(reasons.length),
          initialValue: _failureReason.isEmpty ? null : _failureReason,
          decoration: InputDecoration(
            labelText: snapshot.connectionState == ConnectionState.waiting
                ? 'Loading failure reasons…'
                : 'Failure reason',
          ),
          items: reasons
              .map(
                (reason) => DropdownMenuItem<String>(
                  value: reason.code,
                  child: Text(reason.name),
                ),
              )
              .toList(),
          onChanged: snapshot.connectionState == ConnectionState.waiting
              ? null
              : (value) => setState(() => _failureReason = value ?? ''),
          validator: (value) =>
              (value?.isNotEmpty ?? false) ? null : 'Select a failure reason.',
        );
      },
    ),
  ];
}
