# admin-console

Thin Go proxy plus static pages for operating the academy (SPEC Phase 6.1,
ADR-0008). It serves the admin UI on `admin.<domain>` behind Caddy and forwards
a fixed set of requests to the internal admin listeners. It makes **no
authorization decisions**: auth-service decides on every call.

The design follows the owner's existing reviewer console (decision 2026-10-02),
with the differences recorded in ADR-0008.

## Routes

Only these routes reach an upstream. There is no generic pass-through and no
route under `/internal/`.

| Console route | Upstream (auth-service admin listener) |
|---|---|
| `GET /api/whoami` | `POST /internal/admin/verify`. Returns `{"name"}` only. |
| `GET /api/accounts?search&status&ids&page&limit` | `GET /internal/admin/accounts` (supports `ids` batch lookup) |
| `POST /api/accounts/suspend` `{id, reason}` | `POST /internal/admin/accounts/{id}/suspend` `{reason}` |
| `POST /api/accounts/reactivate` `{id}` | `POST /internal/admin/accounts/{id}/reactivate` |
| `POST /api/accounts/delete` `{id, reason}` | `DELETE /internal/admin/accounts/{id}` `{reason}` |
| `GET /api/audit?source&page&limit` | `source=academy` goes to the academy listener (see below); anything else goes to auth-service. |

| Console route | Upstream (academy-service admin listener) |
|---|---|
| `GET /api/levels` | `GET /internal/admin/levels` |
| `POST /api/levels/create` `{study_type, name_ar, name_en?, order?, published?}` | `POST /internal/admin/levels` |
| `POST /api/levels/update` `{id, ...}` | `PATCH /internal/admin/levels/{id}` |
| `POST /api/levels/delete` `{id}` | `DELETE /internal/admin/levels/{id}` |
| `GET /api/subjects?level_id&published&page&limit` | `GET /internal/admin/subjects` |
| `POST /api/subjects/create` `{level_id, title_ar, ...}` | `POST /internal/admin/subjects` |
| `POST /api/subjects/update` `{id, ...}` | `PATCH /internal/admin/subjects/{id}` |
| `POST /api/subjects/publish` `{id}` | `POST /internal/admin/subjects/{id}/publish` |
| `POST /api/subjects/unpublish` `{id}` | `POST /internal/admin/subjects/{id}/unpublish` |
| `GET /api/videos?subject_id` | `GET /internal/admin/subjects/{id}/videos` |
| `POST /api/videos/create` `{subject_id, title_ar, youtube, ...}` | `POST /internal/admin/subjects/{id}/videos` |
| `POST /api/videos/update` `{id, ...}` | `PATCH /internal/admin/videos/{id}` |
| `POST /api/videos/reorder` `{subject_id, video_ids}` | `POST /internal/admin/subjects/{id}/videos/reorder` |
| `POST /api/videos/delete` `{id, force?}` | `DELETE /internal/admin/videos/{id}[?force=true]` (`?force=true` only when `force` is true) |
| `GET /api/requests?status&subject_id&page&limit` | `GET /internal/admin/requests` |
| `POST /api/requests/accept` `{id}` | `POST /internal/admin/requests/{id}/accept` |
| `POST /api/requests/reject` `{id, reason}` | `POST /internal/admin/requests/{id}/reject` |
| `GET /api/entitlements?user_id` | `GET /internal/admin/entitlements` |
| `POST /api/entitlements/grant` `{user_id, subject_id}` | `POST /internal/admin/entitlements` |
| `POST /api/entitlements/revoke` `{id, reason}` | `DELETE /internal/admin/entitlements/{id}` |
| `GET /api/settings` | `GET /internal/admin/settings` |
| `POST /api/settings/update` `{show_prices, support_whatsapp, center_...}` | `PUT /internal/admin/settings` |
| `GET /api/files?subject_id` | `GET /internal/admin/subjects/{id}/files` |
| `POST /api/files/upload?subject_id` (multipart: `kind`, `title_ar`, `title_en`, then `file`) | `POST /internal/admin/subjects/{id}/files` (a fresh multipart body, streamed) |
| `POST /api/files/delete` `{subject_id, id}` | `DELETE /internal/admin/subjects/{subject_id}/files/{id}` |

`GET /healthz` is unauthenticated and calls nothing. Everything else is the
embedded static UI (`web/`).

There is no catch-all and nothing under `/internal/`. *(Amended 2026-10-09:
this sentence used to say the files routes did not exist; they are the three
`/api/files` rows above, SPEC Phase 5.)* The audit log is served per source (`source=auth`, the
default, or `source=academy`); the two logs are never merged into one page,
because merged pagination over two sources is wrong without a shared cursor.

Every handler: requires `X-Admin-Token` (401 with no upstream call when
missing or malformed), validates input (UUID id, reason 1-1000 characters with
control characters replaced by spaces, search at most 100 characters, status
from a fixed list, bounded page and limit), builds the upstream request from
scratch, caps bodies at 1 MiB, and times out after 10 s.

The PDF upload is the one exception (SPEC Phase 5): its body is capped at
`MAX_PDF_BYTES` plus 64 KiB (413 `file_too_large` above it, refused before
reading when the declared length is larger). The console reads `kind`,
`title_ar` and `title_en` (the academy's rules), checks that the file starts
with `%PDF-`, then streams a newly built multipart body to the academy (fixed
file name `upload.pdf`, `application/pdf`; no client part, header or file name
is forwarded), so the file is never held in memory. Only this handler extends
its own read and write deadlines to 10 minutes and uses an upload client with
that timeout; the server keeps `ReadTimeout` 15 s and `WriteTimeout` 30 s for
everything else. The academy stays the authority on every rule. A client-supplied
`X-Internal-Token` or `X-Admin-Client-IP` can never reach the upstream:
the console sets `X-Internal-Token` from its environment and
`X-Admin-Client-IP` from the real client address (`X-Forwarded-For` is read
only when the peer is in `TRUSTED_PROXY_IPS`, right-to-left, skipping other
trusted proxies). An upstream failure, timeout, 5xx or unusable body becomes a
safe `503`. Tokens, reasons, queries and bodies are never logged; log lines
carry the route, the validated target id and status codes only.

## Configuration

`config.Load()` (also run by `--check-env`). Only `APP_ENV=local|test` relaxes
anything; an empty `APP_ENV` is production and an unknown value is refused.

| Variable | Required | Notes |
|---|---|---|
| `INTERNAL_SERVICE_TOKEN` | always | Sent as `X-Internal-Token`. Empty or blank fails startup. |
| `AUTH_ADMIN_URL` | outside local/test | `https://auth-service:9001`. Must be `https` outside dev, `scheme://host[:port]` only. |
| `ACADEMY_ADMIN_URL` | outside local/test | `https://academy-service:9002`. The catalog routes forward to it. |
| `TLS_CERT_PATH`, `TLS_KEY_PATH`, `TLS_CA_PATH` | outside local/test | Server certificate for the listener, and the mTLS client certificate and local CA for calls to the admin listeners. |
| `TRUSTED_PROXY_IPS` | outside local/test | Comma-separated IPs or CIDRs (Caddy). `/0` is refused. |
| `PORT` | no | Default `3005`. |
| `MAX_PDF_BYTES` | no | Default `20971520` (20 MB, SPEC D14). Body cap of the upload route only; keep it equal to academy-service's. A non-positive or non-integer value fails startup. |

## Browser side

`web/` is plain HTML, CSS and ES modules served with `go:embed`: no framework,
no build step, no CDN. The admin token lives in a module variable in
`web/js/auth.js` and nowhere else (no `localStorage`, `sessionStorage`, cookie
or URL), so reloading the page signs the admin out. Any `401` clears the token
and returns to the sign-in page. The page is Arabic first (right-to-left) with
an English toggle; the choice is kept in the URL hash only.

Requests, Catalog, Accounts, Audit, Settings and Files tabs are active in
`web/js/tabs.js`. *(Amended 2026-10-09: Files was hidden until the Phase 5
APIs existed.)*

The UI is split into small ES modules:
- Accounts: search, status filter, pagination, suspend/reactivate/delete dialogs (`accounts.js`, `account-dialog.js`), and "المواد" button opening the student entitlements modal (`entitlements-dialog.js`) for active/expired/revoked lists, manual grant, and revoke with mandatory reason.
- Requests: purchase requests queue with student identity join via auth-service, Cairo date formatting, live `pending_count` badge with 60-second polling, accept dialog, and reject dialog with mandatory reason (`requests.js`).
- Catalog: levels, subjects, videos, publish/unpublish, reorder, and force-delete (`catalog.js`, `levels.js`, `subjects.js`, `subject-dialog.js`, `videos.js`, `video-dialog.js`, `confirm.js`).
- Settings: app-facing settings form (`settings.js`): `show_prices` toggle, support WhatsApp number, center name/address/hours (Arabic and English) and map URL, with the last-saved state and update time shown. Unsaved-changes guard, double-submit guard, `429` cooldown, and a toast on save; validation errors (for example `settings_forbidden_word`) offer no Retry button because they need an edit, not a retry.
- Unsaved changes: the catalog add/edit forms and an unsaved video order ask before they are closed, left through the tabs or breadcrumb, or the page is closed or reloaded (`unsaved.js`; the browser's own prompt for `beforeunload`, an in-page dialog for the rest).
- Rate limits: a `429` with a `Retry-After` (whole seconds, relayed by the proxy; capped at an hour in the page) shows "حاول بعد N ثانية", counts down, and keeps the action's button and the banner's retry button disabled until the wait ends (`api.js` `parseRetryAfter`, `ui.js` `createCooldown`).
- Double submit: every save, confirm and publish button is disabled while its request is in flight, a second submit (Enter, a second click, the banner's retry) sends nothing, and Esc cannot close a dialog during the request (`ui.js` `keepOpenWhile`, `unsaved.js` `guardDialog`, the per-dialog `busy`/`inFlight` flags).
- Toasts: every save and confirm ends with a short success note (`ui.js` `toast`): account suspend/reactivate/delete, request accept/reject, catalog saves, publish, delete and reorder. Grant and revoke in the student-subjects dialog use a toast inside that dialog (`#entitlements-toast`), because the page toast would sit under its backdrop. Draft/published badges show on every level card and subject row.
- Audit: per-source audit log switch (`audit.js`).
- Files (SPEC Phase 5): level and subject pickers from the catalog list routes (drafts included), the subject's PDFs (kind, titles, size, upload date), an upload form (kind, Arabic and English titles, one `.pdf`) and delete through the shared confirm dialog (`files.js`). The page pre-checks the name, emptiness and the 20 MB default as a convenience only; if `MAX_PDF_BYTES` is changed, the server's limit applies and the page's check stays at 20 MB. `fetch` reports no upload progress, so an upload shows an "uploading" state with a Cancel button (`api.js` `upload`, AbortController). Unsaved-changes guard on tab switch and on level or subject change, double-submit guard, `Retry-After` cooldown, toasts.
- Idle Lock: 19-minute idle warning with 60-second live countdown dialog and automatic 20-minute logout (`idle.js`).

Server error codes are mapped to Arabic/English text in one map in `web/js/i18n.js` (`err.<code>`); `web/test/errors.test.mjs` fails when a code the academy admin handlers can return (the checked-in list in `internal/proxy/catalog_codes_test.go`) has no message.

## Tests

```bash
cd services/admin-console
go test -race ./...
node --test web/test/*.test.mjs     # Node 20+, no npm packages
```

The JavaScript tests run the real modules on a small fake DOM
(`web/test/fake-dom.mjs`); `web/test/static.test.mjs` guards the shipped files
(no inline script or style, no storage or cookie use, no third-party URL,
contrast of the colour tokens).
