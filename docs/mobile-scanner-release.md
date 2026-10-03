# CESERV Scanner release guide

The scanner app supports rear-camera barcode capture, keyboard-style USB or
Bluetooth scanners, stable device event IDs, and an encrypted local retry queue
for connection failures. CESERV AWBs, package barcodes and manual waybill
numbers are accepted by the same scan endpoint.

## Android signing

1. Create or obtain the protected CESERV Android upload keystore.
2. Copy `mobile/apps/cserve_scanner/android/key.properties.example` to
   `mobile/apps/cserve_scanner/android/key.properties`.
3. Fill in the keystore path, alias and passwords. These files are ignored by
   Git and must be stored in the organization's credential vault.
4. Run the release script with the production HTTPS origin:

   ```bash
   cd mobile/apps/cserve_scanner
   CESERV_API_BASE_URL=https://ceservlogistics.com ./tool/build_release.sh
   ```

5. Distribute the generated APK through the approved internal channel or upload
   the AAB to the managed Google Play track.

The GitHub workflow validates every mobile change and produces an unsigned APK
for QA. Store signing secrets in the release environment before converting that
job into a production publishing job.

## iPhone and iPad

The same Flutter app includes the required camera usage description. Create an
Apple distribution certificate and provisioning profile, set the CESERV bundle
identifier/team in Xcode, and archive with the production
`CESERV_API_BASE_URL` dart define. TestFlight or an Apple Business Manager
private app is recommended for controlled distribution.

## Operator validation

Before promotion, sign in with a restricted scanner test account and verify one
accepted, duplicate and rejected scan for each enabled scan mode. Turn off the
network, scan a valid barcode, restore connectivity and confirm that the queued
event replays exactly once.
