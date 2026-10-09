# Incident response

Decision source: ADR-0001 (leak response), ADR-0008 (admin tokens), SPEC R7/R9
(suspension, blocklist). Copy-paste server commands live in
`infrastructure/deploy/SERVER-MANUAL.md` (§9 tokens, §10 operations) — this
file only says when to run which.

## Leaked YouTube id

Decision (ADR-0001): replace the video and change `youtube_video_id`.

1. In the console Catalog tab, open the subject and edit the leaked video,
   pasting the replacement upload's YouTube URL or id (only the validated
   11-char id is stored; students never see it except at play time).
2. Confirm: as an entitled test student, play returns the new id; catalog,
   detail, error bodies and logs still carry no id.
3. TODO(owner): delete or privatize the leaked upload in YouTube Studio — the
   old link stays watchable outside the app until then (accepted risk D3).
4. Note the swap in the audit log; no deploy needed.

## Leaked secret

1. Rekey the value in `$WAEL_HOME/.env.production` (generate on the server
   only), then re-deploy the current `release.env` so every container picks it
   up together (manual §10 "Secret rotation").
2. Effects, so the owner expects them:
   - `JWT_SECRET`: logs EVERYONE out (all access + refresh tokens invalidate).
   - `GATEWAY_SECRET`: must land on the gateway AND all user-facing services
     atomically — a partial rollout returns 401s on proxied calls.
   - `INTERNAL_SERVICE_TOKEN`: must land on auth/notification/academy AND the
     console together, or admin calls fail.
   - `BLOCKLIST_HMAC_KEY`: NEVER rotate casually — existing blocklist entries
     stop matching and banned identities could re-register.

## Stolen admin token

1. Revoke it by id (manual §9):

```bash
[server azureuser] sudo docker exec wael-auth-service-1 /bin/revoke-admin --id <id>
```

2. Revocation applies on the next request. Mint a replacement with
   `onboard-admin` (prints id + token ONCE; store in the password manager).
3. Review the audit log (console Audit tab, both sources) for actions taken by
   that operator between theft and revocation.

## Compromised student account

1. Suspend the account in the console Accounts tab with a recorded reason.
   Suspension is immediate: tokens die on next use and open SSE streams close.
2. Ask the student to change the password (change ends the other sessions) or
   end the sessions from the account flow; reactivate only after the owner
   confirms the account is back in the right hands.
3. For abuse or leakage: delete/ban instead — the email and phone are
   blocklisted and cannot re-register (SPEC R9; self-deletion is different and
   does NOT blocklist).

## When to restore (data rollback)

- Every deploy records a verified pre-deploy backup, and a failed deploy
  prints the exact `restore.sh` command for it (RUNBOOK "Known gaps").
- Rollback covers images only. Restoring discards every write made since the
  backup, so it stays a human decision — never automatic.
- Restore with `scripts/restore.sh <archive>.archive.gz --yes` (manual §10):
  without `--yes` it touches nothing; it stops the app services (mongo and
  redis stay up), restores with `--drop`, restarts, and must pass the public
  health gate.
