# ADR 0007 — Object storage is deferred to Release 2

**Status:** accepted · Release 1

## Context

The Constitution calls for S3-compatible object storage for POD photos,
signatures, labels, exports and backups. Release 1 has exactly one candidate
consumer: the bulk-import error file.

## Decision

No object storage client in Release 1. Bulk import streams the uploaded CSV
directly into an `import_rows` staging table, and rejected rows are streamed
back out as CSV from a keyset cursor. The S3 driver lands in Release 2 with POD.

## Why

- **The single Release 1 use case does not need it.** Rejected rows are
  structured data, naturally a table, bounded by the import size cap, and
  streaming them from a cursor uses constant memory — the same property object
  storage would have given.
- **Adding the AWS SDK now** would bring four modules and a credential surface
  for one feature that works better without it.
- **The constraint that mattered is met**: nothing loads a whole dataset into
  memory. Upload streams row by row into batched COPY; processing walks a
  keyset cursor in chunks; the error export streams.

## Consequences

- Release 2 must add `internal/platform/storage` before POD. That is expected
  work in that release, not debt from this one.
- Database growth from staged import rows is bounded by the 250,000-row cap per
  job, and completed jobs' rows are candidates for a retention sweep.
- Backups remain a database concern, handled by `deploy/` scripts rather than by
  the application.
