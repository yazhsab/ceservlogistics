# Manual operations and scanner release — 3 October 2026

This release enables the operational workflows requested for areas with weak or
no network coverage.

## Delivered

- A manually issued paper waybill number can be recorded during booking. It is
  unique, searchable, printable, publicly trackable, and accepted by facility
  scanning while CESERV continues to issue the immutable official AWB.
- **Operations → Manual upload** accepts up to 250 CSV shipment rows, validates
  every row, distributes a shipment declared value exactly across its packages,
  and stores valid rows as encrypted device drafts.
- Booking forms can be saved as encrypted offline drafts. Explicit synchronization
  obtains a fresh serviceability, route, price, customs and insurance preview
  before booking. Stable idempotency keys prevent retry duplicates.
- The web scanner can use a phone/tablet rear camera. Keyboard-mode USB and
  Bluetooth scanners remain supported.
- The installable CESERV Scanner Android app is release-packaged with a
  production server default, managed signing configuration, camera capture and
  its existing offline scan replay queue.
- The web application has an installable manifest and caches its application
  shell and previously loaded static assets. API responses and credentials are
  never service-worker cached.

## Important operating rules

- Offline shipment drafts are not authoritative bookings. The operator must
  reconnect and choose **Validate & book**; failed serviceability or changed
  commercial configuration remains visible for correction.
- Insurance is booked only when customer acceptance was explicitly captured.
  The current server quote fingerprint is attached at synchronization time.
- CSV monetary values are integer minor units: `100000` means NGN 1,000.00.
- Keep the Android signing keystore in the organizational credential vault. The
  same key is required for every future app update.

## Validation evidence

- `go test ./...`
- `npm run typecheck`, `npm run lint`, `npm test`, and `npm run build`
- Flutter scanner `flutter analyze`, `flutter test`, and signed release APK build
- APK signing certificate and SHA-256 artifact digest verified with Android
  `apksigner`
