#!/usr/bin/env bash
set -euo pipefail

if [[ -z "${CESERV_API_BASE_URL:-}" ]]; then
  echo "CESERV_API_BASE_URL is required and must be an HTTPS origin." >&2
  exit 1
fi
if [[ ! "${CESERV_API_BASE_URL}" =~ ^https:// ]]; then
  echo "CESERV_API_BASE_URL must start with https:// for a release build." >&2
  exit 1
fi

app_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${app_dir}"

flutter pub get
flutter analyze
flutter test
flutter build apk --release --dart-define="CESERV_API_BASE_URL=${CESERV_API_BASE_URL}"
flutter build appbundle --release --dart-define="CESERV_API_BASE_URL=${CESERV_API_BASE_URL}"

echo "APK: ${app_dir}/build/app/outputs/flutter-apk/app-release.apk"
echo "AAB: ${app_dir}/build/app/outputs/bundle/release/app-release.aab"
