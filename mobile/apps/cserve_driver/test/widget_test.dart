import 'package:cserve_driver/src/runs_screen.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('money formats authoritative minor units', () {
    expect(money(57310, 'NGN'), 'NGN 573.10');
    expect(money(0, 'AUD'), 'AUD 0.00');
  });
}
