import 'package:cserve_mobile_core/cserve_mobile_core.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('shipment detail keeps every piece barcode', () {
    final shipment = ShipmentDetail.fromJson(<String, dynamic>{
      'id': 'shp_1',
      'awb': 'CSV260824000001',
      'status': 'OUT_FOR_DELIVERY',
      'pieceCount': 2,
      'packages': <Map<String, dynamic>>[
        <String, dynamic>{
          'id': 'pkg_1',
          'sequence': 1,
          'pieceBarcode': 'CSV260824000001-01',
          'reference': '1Z8827AA0444901684',
        },
        <String, dynamic>{
          'id': 'pkg_2',
          'sequence': 2,
          'pieceBarcode': 'CSV260824000001-02',
          'reference': '1Z8827AA0445904894',
        },
      ],
    });

    expect(shipment.pieceCount, 2);
    expect(shipment.packages, hasLength(2));
    expect(shipment.packages.last.sequence, 2);
    expect(shipment.packages.last.reference, '1Z8827AA0445904894');
  });

  test('scan outcomes preserve rejection details', () {
    final result = ScanResult.fromJson(<String, dynamic>{
      'barcode': 'CSV260824000001-02',
      'outcome': 'REJECTED',
      'rejectionCode': 'WRONG_FACILITY',
      'rejectionMessage': 'This parcel belongs at another branch.',
    });

    expect(result.outcome, ScanOutcome.rejected);
    expect(result.rejectionCode, 'WRONG_FACILITY');
  });
}
