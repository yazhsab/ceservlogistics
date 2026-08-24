# CServe Logistics mobile apps

This workspace contains two Flutter applications backed by the repository's
authoritative `docs/openapi.yaml` contract.

| App | Directory | Primary users | Core workflow |
| --- | --- | --- | --- |
| CServe Driver | `apps/cserve_driver` | Delivery agents | Sign in, view assigned runs, open stops, verify every package in a multi-piece shipment, record delivery/NDR outcomes, collect exact COD and upload photo POD. |
| CServe Scanner | `apps/cserve_scanner` | Branch and hub operators | Select an authorized scan mode, scan with the camera or a keyboard-style handheld, see accepted/rejected feedback, and replay connection failures with stable device event IDs. |

Shared authenticated API access, secure token storage, DTOs, offline scan
storage, device identity and design tokens live in
`packages/cserve_mobile_core`.

## Run locally

The API and its dependencies must be running first (see the root `README.md`).

```bash
cd mobile/apps/cserve_driver
flutter pub get
flutter run

cd mobile/apps/cserve_scanner
flutter pub get
flutter run
```

The sign-in screen asks for the API origin. Use `http://10.0.2.2:8080` from an
Android emulator and `http://127.0.0.1:8080` from an iOS simulator. Release
builds should use an HTTPS origin. Cleartext traffic is enabled only in Android
debug/profile manifests.

## Quality checks

```bash
cd mobile/packages/cserve_mobile_core && flutter analyze && flutter test
cd mobile/apps/cserve_driver && flutter analyze && flutter test
cd mobile/apps/cserve_scanner && flutter analyze && flutter test
```

Android release bundles can be produced with `flutter build appbundle`; iOS
archives require the normal Apple signing setup.

## Multi-piece delivery rule

One shipment can have many physical packages. The driver app loads the
shipment's `packages` array and matches scans against `pieceBarcode` or the
package `reference`. An AWB scan is accepted as package verification only when
the shipment contains exactly one package. A multi-piece stop cannot expose the
"Complete delivery" action until every distinct package is verified in the
current workflow.

This is deliberately presented as a client-side safety gate, not backend truth.
The contract gap that must be closed before the rule is authoritative across
devices is recorded in
`docs/frontend-backend-gaps/mobile-multi-piece-delivery.md`.

## Security and reliability

- Access and refresh tokens are kept in platform secure storage.
- Refresh tokens are rotated through `/api/v1/auth/refresh`.
- Operational requests use persistent device IDs and unique device event IDs.
- Scanner connection failures are queued locally; business rejections are not.
- Rejected scans remain visible with the server's domain-specific reason.
- Delivery COD amounts are sent as server-provided integer minor units.
- OTP values are shown once and are never persisted by the app.
- Camera permission messages are declared for Android and iOS.
