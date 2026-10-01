# Protected video player

Lesson videos are YouTube unlisted videos shown in an embedded player
(ADR-0001, `youtube_player_iframe`, Android first). Owner decision: the
protection target is **ordinary students**, not a determined attacker. This page
records what the app does, what the package can and cannot do, and what to check
on a phone.

## Flow

1. The subject screen shows a video tile unlocked when the server marked it
   `playable: true`. Subject detail never contains a YouTube id.
2. Tapping an unlocked tile opens `/video-player` with `VideoPlayerArgs`
   (the academy's own video id and the text to show; never a YouTube id).
3. The player screen, in this order: turns on Android `FLAG_SECURE`, calls
   `POST /api/v1/academy/videos/{video_id}/play`, and only then hands the
   returned `youtube_video_id` to the embedded player.
4. On every app resume it calls play again. A 404 (on open or resume) closes the
   player with `PlayerExit.locked`; the subject screen shows the tile locked,
   refetches the subject and says the content is restricted. Other failures
   (network, 5xx) pause the video and show a banner with Retry.

Backend contract (from the backend lane, **not verified against the real
service yet**): subject detail has `videos[].playable`; the play endpoint answers
200 `{"video_id","youtube_video_id"}` or a generic 404.

## The YouTube id

Held in one field of the player screen for as long as that screen exists
(`_youtubeId`), and inside the engine. It is never stored, cached, logged,
shown, put in a route or a deep link, or compared in text. `VideoPlayback.toString`
redacts it. Tests: it is absent from every widget text and semantics label, from
route arguments, from `debugPrint` output, from token storage and from the
diagnostics call log; a static test lists the only files allowed to name it and
forbids it near `print`, `log`, `throw` or `Text`. Nothing in `lib/` builds a
YouTube URL (a test scans for `youtube.com`, `youtu.be`, `watch?v=`, `embed/`).

## What the package can hide or block

| Control | Result |
|---|---|
| Embedded controls (play bar, settings, captions button) | Hidden: `controls=0`. The app draws its own bar. |
| Fullscreen button / "full screen into the YouTube app" | Hidden: `fs=0`; the app has its own full screen (landscape, immersive) over the same widget. |
| Keyboard shortcuts | Off: `disablekb=1`. |
| Annotations / cards | Off: `iv_load_policy=3`. |
| Caption overlay | Off: `cc_load_policy=0`. |
| Taps, long-press, text selection, drag, copy on the embed | Blocked three ways: `pointer-events: none` on the embed page, a script that disables selection, callouts, context menu, drag, copy and cut, and an opaque Flutter gesture layer above the WebView. |
| Links (title, logo, share, watch-later, "watch on YouTube", related videos) | Cannot be tapped (above) and every WebView navigation is refused. The package's own policy would open some in the browser or load another video id taken from the URL; it is replaced by "never navigate". |
| Related videos | Limited to the same channel (`rel=0`); the end screen is covered by an opaque cover with a replay button. |
| Remote debugging | Off (the package disables it). |

## What it cannot do

- Remove YouTube's own passive overlays. The title bar when the video is not
  playing, the logo in the bottom corner and end-screen suggestions are drawn
  inside YouTube's cross-origin iframe, which the app cannot style. They are
  inert (no touches reach them) and the app masks the usual places: a bar over
  the top edge while not playing, a patch over the bottom-right corner (the
  physical right edge in every language), and the ended cover. Whether those
  masks land exactly where YouTube draws on every phone needs a device check.
- Stop a rooted device, a camera pointed at the screen, or someone who obtains
  the unlisted URL by other means. The watermark and the per-play check are the
  deterrent.
- Work off Android: iOS is deferred, and desktop has no WebView (the screen then
  shows an error instead of playing).

## Screen capture protection (Android)

`MainActivity.kt` exposes a `com.wael.app/secure_screen` channel (`enable`,
`disable`, `isEnabled`). `FLAG_SECURE` is set when the player opens and cleared
when it closes, nowhere else. If it cannot be set on Android the player does not
play (fail closed). It also blanks the recent-apps thumbnail. Re-asserted on every
resume.

## Watermark

The student's full name and phone, white at 35% with a soft shadow, jumping to a
new spot (never the same twice in a row) every 20 seconds. It ignores touches, has
no close action, is hidden from screen readers, and is the last child of the
player surface, so it stays above everything, including in full screen and on the
"ended" cover. With no name or phone it falls back to the email; with nothing at
all the player refuses to start.

**Backend ask:** `GET /auth/me` returns only `id`, `email`, `role`,
`email_verified`. It must also return `full_name` and `phone` for the watermark to
show them after a restart (until then they are known only in the session that
signed up). The app already reads both fields when present.

## Manual check on a phone (Android)

1. Open a playable video: the watermark shows name and phone and moves after
   about 20 seconds; it stays when you enter full screen and when the video ends.
2. Take a screenshot on the player screen: it must fail or come out black. Start a
   screen recording: the video area must record black. Leave the player: both work
   again.
3. Open recent apps with the player open: the thumbnail is blank.
4. Tap and long-press everywhere on the video, including the corners and the top:
   nothing opens, nothing is selected, YouTube never launches.
5. Pause, and look at the top and bottom-right corners for YouTube's title bar and
   logo; let the video end and look for end-screen suggestions.
6. Send the app to the background and back: playback pauses, then the app asks
   again (a video the admin unpublished meanwhile closes the player and shows the
   tile locked).
