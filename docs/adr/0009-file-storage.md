# ADR-0009: File Storage (Local Encrypted Storage at Rest)

- **Status**: Proposed
- **Date**: 2026-09-30
- **Related Commit SHA**: none (decision only, initial implementation in shared/infra/storage)
- **Related Audit Finding**: n/a

## Context

The academy catalog attaches study notes, dossiers, and revision booklets (PDFs) to subjects alongside lesson videos (ADR-0003, ADR-0005, ADR-0007, `docs/core-service/SPEC.md` Section 1 Decisions 4 & 5, Section 5, and Section 6). While lesson videos are hosted externally on YouTube (ADR-0001), document files must be stored, managed, and served by the platform.

Access to study notes and files is strictly gated by subject ownership: a student cannot download a file unless an entitlement exists for (`user_id`, `subject_id`) (ADR-0003, ADR-0005, ADR-0007 Decision 17, SPEC.md Section 7 R1, R2, R3). Earlier in the repository's development, `shared/infra/storage` was dropped (commit `9890e98`) because the authentication service had no file upload scope. Phase 0.4 of `docs/core-service/SPEC.md` restores this package and requires recording this architectural decision.

SPEC.md Section 3 Open Question 3 notes: "Where PDFs live long term (ADR-0009 decides the first answer: local encrypted storage)."

## Decision

1. **Local disk storage with AES-256-GCM encryption at rest**: The initial storage driver stores documents on local filesystem disk (`baseDir`, configured via `STORAGE_DIR`) encrypted with AES-256-GCM at rest. Plaintext document bytes are never persisted unencrypted to disk.
2. **Server-generated storage keys**: Files are addressed by server-generated random keys (UUIDs / CSPRNG-generated keys), never user-supplied file names or paths.
3. **Path containment enforcement**: Storage operations verify canonical path resolution using Go standard library `os.Root` containment (immune to sibling-prefix escapes and symlinks targeting locations outside the root) to ensure all operations remain strictly scoped within the configured base directory, rejecting path traversal attempts.
4. **Encryption key management**: `DOCUMENT_ENCRYPTION_KEY` (32 bytes / 64 hex characters) is mandatory in `APP_ENV=production`. If missing, non-hex, or of incorrect length in production, storage initialization fails fast. In non-production environments (`local`, `test`), an ephemeral random key or padded test key is permitted to facilitate development and automated testing.
   - *Amended 2026-09-30 (owner decision)*: `DOCUMENT_ENCRYPTION_KEY` must be exactly 32 bytes (64 hex characters) in every environment. Only `APP_ENV=local` or `test` may omit it, in which case an ephemeral key is generated with a logged warning. Any other `APP_ENV` value, including empty, is treated as production. An invalid key fails startup and is never padded or truncated.
5. **Streaming delivery via academy-service**: Student file downloads stream directly through `academy-service` via `OpenFile` (`Content-Disposition: attachment`, `Cache-Control: private, no-store`, `X-Content-Type-Options: nosniff`), as specified in SPEC.md Section 7 R6. Downloads stream through academy-service with an entitlement check per call (R3); no signed URL mechanism exists. Direct static public URLs, signed tokens, and unauthenticated CDN links are prohibited, ensuring entitlement is re-verified on every download and keeping the delivery architecture compatible with future per-user watermarking (Open Question 2).
6. **Storage abstraction interface**: The `Storage` interface defines backend-agnostic operations (`Upload` and `OpenFile`), allowing future migration to S3-compatible cloud object storage (e.g. AWS S3, Cloudflare R2, MinIO) without changing application handlers.
7. **Single-writer TOCTOU assumption**: The storage containment model via `os.Root` assumes that only the service process writes to `STORAGE_DIR`. No concurrent external process creates symlinks or modifies paths within the storage directory during service operations.

## Consequences

### Positive

- Zero external cloud storage dependencies, costs, or credentials required for initial local development and single-host deployment.
- Strong cryptographic confidentiality for course materials and proprietary study notes on disk (AES-256-GCM).
- Defense-in-depth against directory traversal (canonical lowercase UUID keys combined with symlink-proof containment).
- Direct backend streaming ensures subject ownership is re-checked on every download attempt (R3).
- Interface-driven design allows straightforward extension to S3-compatible backends when scaling requires it.

### Negative and Tradeoffs

- Storage capacity is bounded by the host's local disk volume (sufficient for the single-teacher small catalog).
- Multi-instance deployments across multiple physical hosts will require a shared persistent volume or migration to an S3-compatible object store.
- CPU overhead on the host for encrypting uploads and decrypting streaming downloads.

## Alternatives Considered

- **Immediate S3 / Cloud Object Storage**: Rejected for initial phase to keep local Docker compose and single-host deployment free of external service dependencies.
- **Unencrypted Local Disk Storage**: Rejected; course notes and books are proprietary teacher intellectual property and require encryption at rest (SPEC.md Section 8 Item 7).
- **Signed URLs or static download links**: Rejected per SPEC.md Section 7 R3 and R6; signed URLs would bypass per-call entitlement checks and prevent dynamic streaming watermarking.

## Open Questions

- Per-user dynamic watermark on downloaded PDFs for leak traceability (SPEC.md Section 3 Open Question 2).
- Threshold for transitioning from local encrypted disk storage to S3-compatible object storage.

## To verify

- Verify that `academy-service` configuration enforces `DOCUMENT_ENCRYPTION_KEY` requirement in all non-relaxed environments (fails startup unless `APP_ENV=local` or `test`) (SPEC.md Section 8 Item 7 and Section 9).
  - *Answered 2026-10-09 (branch `feat/phase5-files`, not pushed):* yes. `config.Load()` refuses an empty key unless `APP_ENV=local|test` (empty `APP_ENV` is production) and refuses a set key that is not exactly 64 hex characters in every environment, without echoing it; `--check-env` runs the same code; `NewLocalStorage` checks again at startup. Tests: `TestLoad_FileStorage` (key matrix for production, empty, local and test) and `TestRunCheckEnv_FileStorageValues` in `services/academy-service`; production compose refuses to render without the key and preflight checks the 64-hex shape.
- Verify that PDF upload endpoints enforce magic byte validation (`%PDF-`) and `MAX_PDF_BYTES` (SPEC.md Section 8 Item 6, Phase 5.1).
  - *Answered 2026-10-09 (same branch):* yes. The academy upload sniffs `%PDF-` before anything is stored and caps the file part at exactly `MAX_PDF_BYTES` (413 `file_too_large`; the body at `MAX_PDF_BYTES` + 64 KiB); the console checks both again before streaming. Tests: `TestAdminFiles_UploadValidation`, `TestAdminFiles_SizeCap` (contract 15) and the console's `TestFilesUpload_*`; on the local compose stack a 20 MiB file was accepted and 20 MiB + 1 byte and a non-PDF were refused.
- Verify how much memory concurrent downloads take on the production host (one plaintext copy per download in flight, see the 2026-10-09 note) and whether the academy `mem_limit` (192m, `GOMEMLIMIT` 160MiB) is enough for the expected number of simultaneous downloads of large files. *(Added 2026-10-09.)*
  - *Amended 2026-10-09 (owner decision):* academy-service now caps downloads in flight at `MAX_CONCURRENT_DOWNLOADS` (default 3, positive integer, `--check-env`); a download beyond the cap gets `429` with `Retry-After: 5` and `no-store` in the rate limiter's error shape, and nothing queues. The slot is taken after the ownership decision and released however the download ends (success, client abort, storage error; tests `TestDownloadFile_ConcurrencyCapEnforced`, `TestDownloadFile_SlotReleasedOn*`, contract 16). With the 20 MB cap that bounds download buffers at about 60 MB. *(Amended 2026-10-09, owner review:)* a student has at most one download in flight (same 429; entry removed on release), and a client that stops reading loses its slot after `DOWNLOAD_STALL_TIMEOUT` (default 30s, 5s..5m): the copy is chunked (64 KiB) with a write deadline set before every chunk, because neither academy nor the gateway has a server WriteTimeout (tests `TestDownloadFile_StalledClientFreesSlot`, `TestDownloadFile_OnePerStudent`, `TestDownloadFile_NoPerStudentLeak`, contract 16). Still to verify on the production host: real memory under three simultaneous 20 MB downloads, and that the stall timeout does not cut slow but live phones (raise it if they show `file_download incomplete` with a deadline error). Streaming decryption (a chunked v2 format) stays a later item.

## Note (2026-10-09, Phase 5 implementation, not an owner decision)

Built on branch `feat/phase5-files` (not pushed, held for owner review). The
decisions above are unchanged; this records how they were implemented and
two things the earlier text did not say.

1. **Interface (decision 6).** `Storage` now has `Upload`, `OpenFile`,
   `Size` (plaintext size from the on-disk size, no decryption, for
   `Content-Length`) and `Delete` (idempotent on a missing key, `os.Root`
   contained, refuses a directory, removes a symlink entry and never its
   target). A missing object is `storage.ErrNotFound`.
2. **No streaming decryption with the v1 format (decision 5).** The v1 file
   is one AES-GCM seal over the whole file, so a download is read and
   authenticated in full before its first byte, and an upload is read in full
   before it is sealed. Both now work in place (one buffer per operation
   instead of two or three); the on-disk format is unchanged. Measured for a
   20 MiB file: about 30 ms to the response headers and 20 MiB allocated per
   download. Streaming decryption would need a new, chunked format version
   (the version byte allows one); not built, owner question.
3. **Production layout (decision 7, single writer).** `STORAGE_DIR` in
   `.env.production` is a host directory (mode 700) bind-mounted at
   `/data/files`; academy-service runs as deploybot's uid/gid there, so the
   service writes the objects and `backup.sh`/`restore.sh` (infra branch
   `fix/infra-deploy-readiness`) read and restore them as the same user.
   The single-writer assumption holds: only the service writes while it runs,
   and a restore runs with the services stopped.
   - *Owner decision 2026-10-09 (on the Phase 5 report): accepted.*
     academy-service runs as `WAEL_UID:WAEL_GID` (deploybot) in production.
     W-10 (a pinned uid for every service image) stays open as a separate
     item; this decision covers academy-service only.
