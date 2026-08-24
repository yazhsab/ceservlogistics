import 'package:cserve_mobile_core/cserve_mobile_core.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('queued scan keeps its device event id across retries', () {
    final scan = QueuedScan(
      eventId: 'device-event-1',
      barcode: 'CSV260824000001-01',
      scanType: 'RECEIVE',
      occurredAt: DateTime.utc(2026, 8, 24, 8),
      operatingUnitId: 'ou_branch',
    );
    final restored = QueuedScan.fromJson(scan.toJson());

    expect(restored.eventId, 'device-event-1');
    expect(restored.barcode, 'CSV260824000001-01');
    expect(restored.scanType, 'RECEIVE');
  });
}
