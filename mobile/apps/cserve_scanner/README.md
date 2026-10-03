# CESERV Scanner

Flutter facility app for camera and keyboard-style barcode scanning, explicit
accepted/duplicate/rejected feedback, and offline-safe scan replay.

```bash
flutter pub get
flutter run
flutter analyze
flutter test
```

See the parent [`mobile/README.md`](../../README.md) for architecture, API setup
and operational behavior.

## Production build

Copy `android/key.properties.example` to `android/key.properties`, point it to
the protected Android upload keystore, then run:

```bash
CESERV_API_BASE_URL=https://your-ceserv-domain.example ./tool/build_release.sh
```

The HTTPS server address is embedded as the first-run default. Operators can
still change it from the sign-in screen. The script analyzes and tests the app,
then produces an installable APK and a Play Store AAB. `key.properties` and the
keystore remain local and must never be committed.
