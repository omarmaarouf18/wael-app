# ADR-0001: Lesson Videos Hosted on YouTube

- **Status**: Accepted
- **Date**: 2026-09-29
- **Amended**: 2026-09-30
- **Related Commit SHA**: none (decision only, no implementation yet)
- **Related Audit Finding**: n/a

## Context

The academy catalog is video-led (recorded lessons, revision dossiers, live
session replays). Serving that catalog from infrastructure we operate would
mean building and funding an upload pipeline, transcoding, a CDN footprint,
and per-view bandwidth for long-form Arabic video. As of this ADR, no code
exists for video upload, storage, or playback anywhere in the repo.

## Decision

1. Lesson videos are hosted on YouTube. The platform operates no video
   server of its own.
2. The backend stores references to videos (identifiers/links plus catalog
   metadata), never video bytes.
3. The mobile app plays videos through YouTube embedded playback only.

## Consequences

### Positive

- No video infrastructure to build, operate, or scale (upload, transcode,
  CDN, bandwidth).
- Playback quality adaptation and device compatibility come from the
  YouTube clients.

### Negative and Tradeoffs

- The catalog depends on a third party for availability, player behavior,
  and policy enforcement.
- Access control is only as strong as the reference model the backend
  exposes; player-level obscurity is not enforcement (see To verify).

## Alternatives Considered

- **Self-hosted video server with object storage and CDN**: rejected for
  operational cost and build time relative to the catalog's needs.

## To verify

- YouTube's current terms for embedded players and API services, and whether
  this catalog's usage complies.
- Confirm that unlisted links are treated as obscurity, not access control.
- Check exactly what the backend exposes to a student before they have
  access (identifiers, metadata, thumbnails), so unentitled references leak
  nothing playable.

## Amendment (2026-09-30): leak response

When a lesson video leaks, the response is to **replace the video and change
`youtube_video_id`**. The same leak-response note goes into `RUNBOOK.md`
when it is written (`docs/core-service/SPEC.md`, Phase 8); no RUNBOOK exists
yet as of this date.

## Amendment (2026-10-09): RUNBOOK exists; leak response moves to the incident doc

The 2026-09-30 note above ("no RUNBOOK exists yet as of this date") is
superseded: `RUNBOOK.md` now exists at `infrastructure/deploy/RUNBOOK.md`
(with the full manual in `SERVER-MANUAL.md`, same folder). The decision itself
(replace the video, change `youtube_video_id`) is unchanged. The step-by-step
leak-response procedure now lives in `docs/ops/INCIDENTS.md` ("Leaked YouTube
id"); this ADR keeps the decision, the incident doc carries the procedure.

## Amendment (2026-10-03, owner decision D3): leaked ids are an accepted risk

Amended 2026-10-03 (owner decision D3), recorded 2026-10-05. This adds to
Negative and Tradeoffs and To verify above; the earlier text is unchanged.

The backend releases the YouTube id to a student who owns the subject (ADR-0007,
`POST /academy/videos/{id}/play`), so a leaked id can be watched outside the app.
Current mitigations: the 2-device session cap, the moving watermark,
`FLAG_SECURE`, ids never in catalog, detail, route arguments or logs, and the
append-only `video_plays` audit. The risk is accepted for launch; revisit after
launch with a private or signed player, or a protected host.

Obscuring YouTube's player UI is a ToS risk. The top-overlay mask stays
cosmetic and keeps the YouTube logo.

*Status (2026-10-05, owner decision on the overlay masks):* the code now matches (commit 90edff3).
`frontend/lib/widgets/protected_video_surface.dart` no longer masks the logo
corner; the top mask shows while the video is not playing and for 4 seconds
after every start, resume, replay and seek, with a height that scales with the
player height. Not yet verified on a real phone: whether 4 seconds and that
height fully cover YouTube's title bar (owner to check).
