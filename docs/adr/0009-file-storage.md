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
3. **Path containment enforcement**: Storage operations verify canonical path resolution (immune to sibling-prefix escapes and symlinks targeting locations outside the root) to ensure all operations remain strictly scoped within the configured base directory, rejecting path traversal attempts.
4. **Encryption key management**: `DOCUMENT_ENCRYPTION_KEY` (32 bytes / 64 hex characters) is mandatory in `APP_ENV=production`. If missing, non-hex, or of incorrect length in production, storage initialization fails fast. In non-production environments (`local`, `test`), an ephemeral random key or padded test key is permitted to facilitate development and automated testing.
   - *Amended 2026-09-30 (owner decision)*: `DOCUMENT_ENCRYPTION_KEY` must be exactly 32 bytes (64 hex characters) in every environment. Only `APP_ENV=local` or `test` may omit it, in which case an ephemeral key is generated with a logged warning. Any other `APP_ENV` value, including empty, is treated as production. An invalid key fails startup and is never padded or truncated.
5. **Streaming delivery via academy-service**: Student file downloads stream directly through `academy-service` via `OpenFile` (`Content-Disposition: attachment`, `Cache-Control: private, no-store`, `X-Content-Type-Options: nosniff`), as specified in SPEC.md Section 7 R6. Downloads stream through academy-service with an entitlement check per call (R3); no signed URL mechanism exists. Direct static public URLs, signed tokens, and unauthenticated CDN links are prohibited, ensuring entitlement is re-verified on every download and keeping the delivery architecture compatible with future per-user watermarking (Open Question 2).
6. **Storage abstraction interface**: The `Storage` interface defines backend-agnostic operations (`Upload` and `OpenFile`), allowing future migration to S3-compatible cloud object storage (e.g. AWS S3, Cloudflare R2, MinIO) without changing application handlers.

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

- Verify that `academy-service` configuration enforces `DOCUMENT_ENCRYPTION_KEY` requirement when `APP_ENV=production` (SPEC.md Section 8 Item 7 and Section 9).
- Verify that PDF upload endpoints enforce magic byte validation (`%PDF-`) and `MAX_PDF_BYTES` (SPEC.md Section 8 Item 6, Phase 5.1).
