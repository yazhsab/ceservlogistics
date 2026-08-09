# ADR 0010 — A hand-written S3 client rather than an SDK

**Status:** accepted · Release 2

## Context

Release 2 stores POD photos, signatures and generated manifests outside
PostgreSQL, in S3-compatible object storage (§31). The surface actually needed
is four verbs: PUT, GET, DELETE, and presigned GET.

The AWS SDK for Go v2 brings a large dependency tree — credential providers,
retry middleware, endpoint resolution, a paginator framework, and a transitive
set of internal modules — for those four verbs. The Constitution (§3) asks for
"small focused dependencies" and warns against heavy libraries.

## Decision

`internal/platform/storage` implements AWS Signature Version 4 directly over
`net/http`, in roughly two hundred lines. It provides a `Store` interface with
two implementations:

- **`S3Store`** for production, speaking the S3 protocol with SigV4.
- **`FSStore`** for development and tests, writing to a local directory.

The interface exists so that an integration test can exercise real upload,
checksum and retrieval without a MinIO container, while production gets a real
object store. Configuration refuses `filesystem` in production, because objects
that live in a container do not survive a deploy.

## Consequences

The dependency tree stays small and the code is auditable: a reader can see
exactly what is sent to the storage provider, which matters when the payload is
delivery evidence.

The risk is that SigV4 has edge cases — unusual characters in keys, multipart
uploads, region-specific endpoints. Three mitigations:

1. Object keys are **server-generated** from hex-encoded random bytes and a
   sanitised prefix, so the character set is narrow and known.
2. Uploads are bounded well below the 5 GB single-PUT limit, so multipart is
   never needed.
3. The signing path is unit-tested against an HTTP test server that asserts the
   canonical request, the signed headers and the payload hash.

If a future release needs versioning, lifecycle rules, or multipart uploads for
large exports, revisit this: at that point an SDK earns its weight.
