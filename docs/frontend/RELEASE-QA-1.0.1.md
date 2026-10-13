# Release QA checklist — app 1.0.1 (build 2)

Manual device checks for the 1.0.1+2 store build (stable-release pass,
2026-10-12). Run every row on **one Android phone** (signed release APK from
the wael-app-mobile build-apk workflow) and **one iPhone** (TestFlight build
from the "Build iOS" workflow). Write `pass`, `fail` (with a note) or `n/a` in
the two result columns. Nothing below has been run on a device yet.

*(Added 2026-10-13: the iPhone build to test is TestFlight **1.0.1 build 4**
("Build iOS" run 38353982883, commit `17d1c0d`). iOS build numbers come from
the workflow run number, so they differ from the `+2` in `pubspec.yaml`; the
code is the same 1.0.1+2 release.)*

Each row names the automated test that covers the same function (file: test
name). The tests run against fakes; this checklist is the real-device check.

Before you start:

- Server: the academy-service in use serves the 2026-10-12 app-config fields
  (`update_url_android`, `update_url_ios`, `min_version_ios`,
  `latest_version_ios`; `infrastructure/deploy/SERVER-MANUAL.md` §5,
  `docs/mobile/RELEASE.md` "Update gate values").
- Accounts: one student account with at least one active subject that has
  videos and, with `FEATURES_FILES=true`, at least one file; a second student
  account for the account-switch rows; console access to add a file.
- Settings > About shows version `1.0.1` on both phones.

## Earlier iOS fixes never verified on a device

| # | Check | Expected | Android | iPhone |
|---|-------|----------|---------|--------|
| I1 | Install the TestFlight build on an iPhone running iOS 15, 16 or 17+ (`0009be2`, iOS 15.0 deployment target, ITMS-90068) | App Store Connect accepted the build; it installs and opens | n/a | |
| I2 | Open a playable lesson (`32bc53d`, YouTube frame on iOS) | The video loads and plays inside the app; no "video unavailable" and no Safari | n/a | |
| I3 | Notes & books: download a PDF, tap Open (`b0de4d4`) | The system Quick Look viewer opens the PDF; its own share button works | n/a | |
| I4 | Notes & books: tap Share on a downloaded PDF (`b0de4d4`) | The system share sheet opens with the PDF (save to Files, AirDrop, mail) | n/a | |

## Auth and account

| # | Function | Automated test | Device check | Expected | Android | iPhone |
|---|----------|----------------|--------------|----------|---------|--------|
| A1 | Sign up with consent | `test/signup_screen_test.dart`: consent unticked by default; submit disabled until ticked; valid form: one signup call, then /otp | Sign up a new account; try submitting before ticking consent | Submit stays disabled until consent is ticked; then the OTP screen opens | | |
| A2 | Login | `test/login_screen_test.dart`: valid credentials: one login call, then /main | Log in; the keyboard offers the saved password (autofill) | Main shell opens; password autofill offered | | |
| A3 | OTP and resend | `test/otp_screen_test.dart`: a correct code calls verifyOtp once and goes to /main; resend button sends a new code and starts a cooldown | Use the emailed code; tap resend once | Code from the email is offered by the keyboard (iOS) or pasted; resend starts a cooldown | | |
| A4 | Forgot / reset password | `test/forgot_password_screen_test.dart`: three steps: request, verify with OtpPinInput, confirm | Reset the password, then log in with the new one | Login with the new password works; the old one fails | | |
| A5 | Session refresh | `test/api_client_test.dart`: 401 retry has depth limit of 1: second 401 triggers forced logout without looping | Leave the app logged in past the access-token lifetime, reopen | The app keeps working without a login prompt | | |
| A6 | Forced logout | `test/auth_phase17_test.dart`: session_replaced on refresh clears state and sets Arabic owner message | Log in on a third device so this one is replaced | This phone returns to login with the session-replaced notice | | |
| A7 | Sign out clears tokens, catalog and PDFs | `test/settings_screen_test.dart`: sign out asks first, then clears and goes to /login; `test/account_switch_leakage_test.dart`: AuthProvider logout clears catalog cache; `test/files_download_test.dart`: sign-out clears every copy and cancels running downloads | Download a PDF, sign out, log in again | Login screen; after login the PDF must be downloaded again | | |
| A8 | Account switch, no leakage | `test/account_switch_leakage_test.dart`: ProxyProviders reset on unauthenticated state transition; `test/files_download_test.dart`: another account wipes the previous copies | Sign out of account 1, log in as account 2 | No subject, notification, PDF or name of account 1 shows | | |
| A9 | Profile edit, 30-day limit | `test/settings_profile_test.dart`: editing the name saves with the password; change_too_soon names the allowed date | Edit the name; try again at once | First edit saves; the second names the date it is allowed again | | |
| A10 | Email change | `test/settings_email_test.dart`: confirm success ends at login | Change the email with the code | Ends at login; the new email logs in | | |
| A11 | Password change (ends other sessions) | `test/settings_password_test.dart`: success confirms and leaves | Change the password with a second phone logged in | Success message; the second phone is logged out | | |
| A12 | Devices list and revoke | `test/settings_devices_test.dart`: tile opens the device list; signing out asks, ends, and reloads | Open My devices; sign another device out | The list shows both; the other device is logged out | | |
| A13 | Account deletion request and cancel | `test/settings_delete_test.dart`: success shows the date, then login; login with deletion_cancelled shows the notice | Request deletion, then log in again | The deletion date shows; logging in cancels it and says so | | |

## Catalog

| # | Function | Automated test | Device check | Expected | Android | iPhone |
|---|----------|----------------|--------------|----------|---------|--------|
| C1 | Levels and subjects | `test/courses_screen_test.dart`: all three tabs with data | Open Courses; switch study type and level | Three study types; levels and subjects as in the console | | |
| C2 | Search / filter | `test/courses_screen_test.dart`: selection: study type then level then search | Search part of a subject title | Only matching subjects show | | |
| C3 | Subject detail | `test/course_detail_screen_test.dart`: loads the subject by id and shows the ready state | Open an owned subject | Videos in order, files tab when files are on | | |
| C4 | Locked subject → access request → pending | `test/course_detail_screen_test.dart`: not owned, no request: it is sent on open, then pending | Open a subject you do not own | "Request pending" shows; the console Requests tab lists it | | |
| C5 | Price only when both switches are on | `test/course_detail_screen_test.dart`: a locked subject shows its price when the API sends it; the price stays hidden when show_prices is off | Toggle the console `show_prices` with `EXPOSE_PRICE_TO_STUDENTS` on, then off | Price shows only when both are on; never a payment button | | |
| C6 | Home / Courses freshness | `test/refresh_freshness_test.dart`: Home refetches the catalog when it becomes visible again; Courses refetches the catalog on app resume | Grant a subject in the console; wait 30 s; switch to Home | The subject appears without pull-to-refresh | | |

## Player

| # | Function | Automated test | Device check | Expected | Android | iPhone |
|---|----------|----------------|--------------|----------|---------|--------|
| P1 | Play check on open and resume | `test/video_player_screen_test.dart`: opens: secure first, then play call, then the engine; background pauses; resume asks again and keeps playing | Open a lesson; background and return | Plays; pauses in background; keeps playing after return | | |
| P2 | Unpublished while open | `test/video_player_screen_test.dart`: 404 on resume closes the player as locked | Unpublish the video in the console, background and return | The player closes and the tile is locked | | |
| P3 | Watermark | `test/video_player_screen_test.dart`: full screen hides the header, keeps the watermark | Watch 30 s, enter full screen | Name and phone move about every 20 s, in full screen too | | |
| P4 | Next lesson | `test/video_player_screen_test.dart`: ended starts a 5-second countdown that auto-advances | Seek to the end | 5-second countdown, then the next lesson | | |
| P5 | Speed | `test/video_player_screen_test.dart`: the menu offers every rate and each reaches the engine; the saved speed is applied on load | Pick 1.5x; open another lesson | 1.5x applies and is kept | | |
| P6 | Full-screen back | `test/video_player_screen_test.dart`: system back in full screen exits full screen first | Full screen, then Android back / iOS edge swipe | First back leaves full screen; second closes the player | | |
| P7 | Android FLAG_SECURE | `test/secure_screen_test.dart`: enable and disable call the channel on Android | Screenshot and screen recording during playback; recent apps | Screenshot fails or is black; recording is black; thumbnail blank | | n/a |
| P8 | iOS recording cover | `test/ios_capture_cover_test.dart`: capture starts mid-play: pause, cover, play blocked; capture stops: cover removed, no auto-resume | Control Center screen recording during playback, then stop it | Video pauses, black cover with the message; after stop the cover goes, the video stays paused; the recording shows only the cover | n/a | |
| P9 | iOS recording before play | `test/ios_capture_cover_test.dart`: capture on before play: cover, nothing loads or plays; capture stops before play: the student taps play | Start recording, then open a lesson; stop recording | Only the cover; after stop, "Play lesson" starts it | n/a | |
| P10 | iOS AirPlay / mirroring | `test/ios_capture_cover_test.dart`: capture starts mid-play: pause, cover, play blocked | Screen Mirroring to a TV or Mac during playback | Same as P8 while mirroring | n/a | |
| P11 | iOS resume re-check | `test/ios_capture_cover_test.dart`: app resume re-checks: capture found on resume covers | Start recording while the app is in the background, return | The cover is up on return | n/a | |
| P12 | iOS screenshot (known limit) | none: iOS cannot block screenshots (`docs/frontend/VIDEO_PLAYER.md`) | Screenshot during playback | The screenshot is NOT blocked; the watermark is visible in it | n/a | |
| P13 | Fail closed | `test/video_player_screen_test.dart`: a device that cannot be secured does not play; `test/ios_capture_cover_test.dart`: channel missing: the player refuses to play | n/a on a normal device (covered by tests) | — | n/a | n/a |

## Notes & books

| # | Function | Automated test | Device check | Expected | Android | iPhone |
|---|----------|----------------|--------------|----------|---------|--------|
| N1 | Flag off | `test/files_screen_test.dart`: flag off: today's coming-soon state, no requests | `FEATURES_FILES=false` | Coming soon, no requests | | |
| N2 | Flag on | `test/files_screen_test.dart`: flag on: owned files grouped by subject, locked hidden | `FEATURES_FILES=true` | Owned subjects' files grouped by subject | | |
| N3 | Download, cancel | `test/files_screen_test.dart`: download shows progress and cancel, then Open and Share; cancel returns to Download and keeps nothing | Download a file; cancel another mid-way | Progress then Open/Share; the cancelled one is back to Download | | |
| N4 | Fail / retry | `test/files_screen_test.dart`: download failure network: localized message + retry | Airplane mode mid-download, then retry online | Localized message; retry completes | | |
| N5 | Open and share | `test/files_download_test.dart`: download -> downloaded -> open and share offline | Open and Share (iPhone: I3, I4) | The phone's PDF app / Quick Look; the share sheet | | |
| N6 | One download at a time | `test/files_screen_test.dart`: download failure 429: localized message + retry | Start two downloads at once | The second gets the "wait" message; it works after the first finishes | | |
| N7 | File removed by the server | `test/files_screen_test.dart`: a file removed from the subject drops its copy | Delete a downloaded file in the console; refresh | The file and its copy are gone | | |
| N8 | Offline copies | `test/files_screen_test.dart`: offline: downloaded copies list and open without network | Airplane mode, open the tab and a downloaded file | Offline banner; downloaded files open | | |
| N9 | Freshness: new file without reload | `test/refresh_freshness_test.dart`: cause 1: coming back to the tab shows a file added meanwhile; cause 2: a subject detail cached earlier is refetched; cause 3: a subject listed with zero files is still fetched; cause 4: a late features.files flip turns the tab on | Add a file in the console; wait 30 s; switch to Notes & books (or background and return) | The file appears with a thin progress bar; no pull-to-refresh, no restart | | |

## Notifications

| # | Function | Automated test | Device check | Expected | Android | iPhone |
|---|----------|----------------|--------------|----------|---------|--------|
| T1 | List | `test/notifications_screen_test.dart` (list tests) | Open the bell | The account's notifications, newest first | | |
| T2 | Unread dot | `test/main_shell_test.dart`: the pip follows the unread count | Receive one; open it | Dot shows, then clears | | |
| T3 | SSE live | `test/notification_stream_test.dart`: reads fresh token on initial connect and delivers frames | Approve a request in the console with the app open | The notification arrives without refresh | | |
| T4 | SSE reconnect / resume | `test/notification_stream_test.dart`: a dropped stream reconnects after the backoff and delivers again; `test/refresh_freshness_test.dart`: the list refetches on app resume, keeps items on failure | Background the app for a minute, send one, return | It is in the list on return | | |
| T5 | Time formatting | `test/notification_time_test.dart`: matches the approved example shapes exactly | Look at fresh and old items in ar and en | "now", minutes, hours, yesterday, full date | | |

## App-config

| # | Function | Automated test | Device check | Expected | Android | iPhone |
|---|----------|----------------|--------------|----------|---------|--------|
| G1 | Required update, own link | `test/update_per_platform_test.dart`: required × perPlatform × support=true | Set `MIN_VERSION=1.0.2` / `MIN_VERSION_IOS=1.0.2`, `UPDATE_URL_ANDROID` (Play/APK) and `UPDATE_URL_IOS` (apps.apple.com); cold start | Blocking gate; "Update now" opens Play/APK on Android and the App Store on iPhone | | |
| G2 | iOS never opens an Android link | `test/update_per_platform_test.dart`: iOS never returns a non-App-Store link, whatever the server sends | Leave `UPDATE_URL_IOS` empty, `UPDATE_URL` = APK link; cold start | iPhone: no "Update now", the support WhatsApp button instead; Android: the APK link | | |
| G3 | Required, no link at all | `test/update_per_platform_test.dart`: required × none × support=true | Empty all update URLs | WhatsApp button opens the support chat (no dead end) | | |
| G4 | Update available tile | `test/update_per_platform_test.dart`: available × perPlatform | `LATEST_VERSION(_IOS)=1.0.2`, min below 1.0.1 | Settings > About shows "Update available" opening the platform's link | | |
| G5 | No update | `test/update_per_platform_test.dart`: none × perPlatform | min and latest ≤ 1.0.1 | No gate, no tile | | |
| G6 | Support WhatsApp | `test/settings_screen_test.dart`: Help section is present with URL and opens WhatsApp externally | Settings > Help > WhatsApp | WhatsApp (or the browser) opens the support chat | | |
| G7 | Center info | `test/settings_screen_test.dart`: shows name, address and localized hours | Settings > Help | Name, address, hours in the UI language; map link opens | | |
| G8 | Legal links fallback | `test/legal_links_test.dart`: terms falls back on empty, non-https and malformed config | Settings > About > Terms / Privacy | The legal site opens (English adds `#en`) | | |

## Settings and store items

| # | Function | Automated test | Device check | Expected | Android | iPhone |
|---|----------|----------------|--------------|----------|---------|--------|
| S1 | Language switch (ar/en, RTL) | `test/settings_screen_test.dart`: language tile toggles the locale and says so | Switch language; restart the app | Layout mirrors; the choice survives a restart | | |
| S2 | About / version | `test/settings_about_test.dart`: shows the installed version | Settings > About | Version `1.0.1` | | |
| S3 | App name | none (platform files) | Home screen icon label | "EL METR ACADEMY" on both | | |
| S4 | Autofill on account forms | `test/autofill_forms_test.dart`: password change: current, new, confirm | Settings > Change password | The keyboard offers the saved password and a strong new one; next/done move through the fields | | |
| S5 | iOS swipe back | none (platform gesture) | Swipe from the left edge on every pushed screen (subject, player, notifications, settings pages) | Goes back; the update gate cannot be swiped away | n/a | |
| S6 | Safe areas | none (device check) | Every screen and the full-screen player on a notched iPhone and a gesture-nav Android | Nothing under the notch, status bar or home indicator | | |
| S7 | Text size 200% | `test/text_scale_overflow_test.dart` | Largest system font, then visit each tab | No clipped or overlapping text | | |
