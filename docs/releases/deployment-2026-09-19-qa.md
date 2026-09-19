# Hostinger QA deployment — 19 September 2026

Target: `187.127.143.76`, existing Compose project `courier-os`.
Final release: `/opt/ceservlogistics/releases/20260919-7410087-qa4`.
Images: `courier-os:20260919-7410087-qa4` (API, second API and worker) and `ceserv-web:20260919-7410087-qa4`.

The user authorized fixes and deployment. Shared code was updated; synthetic business mutations were confined to `QA_20260919` (organization 3). Provisioning did not alter CESERVE/DEMO accounts. No migration was applied; schema remains 38. PostgreSQL, Redis and Minio were not replaced. Other VPS applications were untouched.

## Source provenance

The build is base `7410087357b0cbd8982ab27c14385609002f7fc8` plus reviewed overlays applied sequentially. The subsequent source commit captures these changes; `/version` retains its build-time overlay identifier.

| Overlay | SHA-256 |
| --- | --- |
| Main QA corrections | `c0e36f35c3e3370c42ca779a6f3dd76ac742b93418dcb854d589ced1ab43bc33` |
| QA2 commission/cache | `a86afb4dccde4dbdf6510638ff0c80610801f7ac01cec8ecd1993b7b2165649b` |
| QA3 finance/facility | `5ae0828efeddcc2f97a6a0658ff383e341646624859d6f08c9fd51dce189d44c` |
| QA4 COD/note handoff/final UI | `865160b802e294f1e1f68b943242647313cf8091f9d86b1ee021206ae8e868f0` |

Final `/version`: `20260919-7410087-qa4`, commit `7410087357b0cbd8982ab27c14385609002f7fc8+qa-865160b8`, built `2026-09-19T08:03:23Z`, production, API v1. QA4's final two frontend-only changes did not alter API source; the final web image was rebuilt and Compose supplied the final identifier to API/worker processes.

## Backups and rollout

Mode-600 database backups are under `/opt/ceservlogistics/backups`.

| Backup | SHA-256 |
| --- | --- |
| `pre-20260919-qa.dump` | `18812095814c55f53b4bfe74afc1bb485c3ed3f33e7dbd0f4faacb6a5acc5456` |
| `pre-20260919-qa3.dump` | `a6cddf67642998692c2ab8d8f4103d493bd062fce0e1fa6a144f096247277029` |
| `pre-20260919-qa4.dump` | `656b3d3492bcd2e9da854200a1af313646cc338c992f64d77017b1808c526d03` |

The main archive was readable and listed 148 table-data entries. A full restore drill was not performed.

Runtime environment files stayed outside Docker build context. Only application image/version fields changed. The production and VPS Compose files used project `courier-os`. API, API-b, worker and web were replaced sequentially with `--no-deps --wait --wait-timeout 60`.

QA4 completed 60 successful API readiness and 60 successful web HTTP probes. All seven CESERVE containers were healthy afterward. Chrome retests covered credit-note checker handoff, COD custody/duplicates, zero settlement and RTO layout. Logs remain under `/root/ceserve-qa-20260919`; secret-bearing environment files are excluded from the evidence package.

## Rollback and capacity

If application rollback is needed, use QA3 release/images and its runtime environment with the existing database. Replace API, API-b, worker and web sequentially and verify readiness. No schema rollback is needed. Do not restore a pre-test database backup for routine application rollback: that would discard subsequent legitimate data.

Final disk check: root volume 193 GB, 186 GB used, 7.5 GB available, 97% usage. No images, backups or unrelated data were deleted. Capacity maintenance is required before further large builds.
