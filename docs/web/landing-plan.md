# Landing page plan (`/` and `/en`)

UI/UX brief 2026-11-07, step 1. For owner review before step 2 builds it.
*(Amended 2026-11-07: the owner approved the plan ("Go to step 2") without
sending facts or assets yet; step 2 built the page from it. Hero: the brand
art, pending item 1. Teacher tile: the logo until item 3. Store links: text
buttons to the two URLs until items 7 and 8. Shown on the live site today:
hero, teacher (name and the one-teacher fact only), subjects by year, the
three steps, the app section without screenshots, the two answered FAQ
items (payment, devices — both from the built system and SPEC decision 22),
closing call to action. Left out of production until the owner answers:
center, testimonials and the three unanswered FAQ items.)*
Design rules: `web/DESIGN.md`. Copy below is a **draft** (Arabic first,
English second); nothing in it is a fact the owner has not given. Every
`[OWNER: …]` is a gap that ships as a visible TODO block in non-production
builds, and the section hides or degrades gracefully in production (noted
per section).

The page stays static (`○`): the catalog section reads the same server-side
catalog call as `/subjects` (one request per build or 300 s revalidation),
the account state in the header is the existing client `AuthProvider`.
No new API call, route, script host or embed.

## The one job of the page

Turn a law student who has never heard of EL METR into one who opens the
catalog (primary) or installs the app (secondary). Every section either
answers "is this for me?", "can I trust it?" or "how do I start?".

## Section order

The brief's order is kept. Considered and rejected: moving "How to start"
above "About the teacher" — it answers "how", but a new visitor first needs
"who is this"; the teacher section is short, so the steps still come early.
Testimonials, when they exist, sit after the center (proof after the
facts); the FAQ stays right before the closing call to action, so the last
doubts are answered just before the final button.

| # | Section | Job | Needs from owner |
|---|---|---|---|
| 1 | Hero | Say who it is for and what they get; one primary action | Hero photo (or OK to use the brand art), one-line promise OK |
| 2 | About the teacher | Trust: a real person teaches this | OK to use the app portrait on the web, short bio |
| 3 | Subjects by year | Show it covers *my* year; go to the catalog | — (live catalog) |
| 4 | How to start | Remove fear of the payment step | Activation time, center payment wording |
| 5 | The app | Where studying happens; install it | App screenshots for the web, store URLs confirmed, store badges |
| 6 | The center | It is a real place; visit or pay there | Address, hours, map link, photos |
| 7 | Testimonials | Social proof | Only real quotes with written consent — else omitted |
| 8 | FAQ | Answer the four questions that block a purchase | Answers (payment, activation time, devices, center students) |
| 9 | Closing call to action | Second chance to act | — |

---

### 1. Hero

- **Job:** in one line, who it is for and what they get. One primary action.
- **Copy (draft):**
  - Eyebrow: `EL METR ACADEMY`
  - h1 (the only h1 on the page):
    - ar: «شرح مواد كلية الحقوق… مع EL METR Academy»
    - en: "Law school subjects, explained — with EL METR Academy"
  - Lead:
    - ar: «فيديوهات ومذكرات لكل فرقة ودبلومة، تذاكرها على موبايلك في أي وقت.»
    - en: "Videos and notes for every year and diploma, on your phone, any time."
    (Same promise as the app's store screenshots: «فيديوهات ومذكرات لكل فرقة ودبلومة».)
  - Primary: «تصفّح المواد» / "Browse subjects" → `/subjects`
  - Secondary: «حمّل التطبيق» / "Get the app" → scrolls to section 5 (`#app`)
- **Visual:** full-width dark band; photo at the end edge fading into the
  canvas toward the text (app `HomeHeroBanner` pattern), text at the start
  edge. Mobile: photo on top (16:10), text below. Crimson rule above the
  eyebrow. Hero image preloaded, AVIF/WebP, explicit size — it is the LCP
  element.
- **Facts / assets:** `[OWNER: hero photo — a real photo of the center or of
  Ustaz Wael. Until then: may the gentleman brand art
  (frontend/assets/branding/el_metr_character_art.png, owner-approved
  2026-10-02) be used as the hero on the website?]`
  `[OWNER: confirm the one-line promise, or give your own.]`
- **Production without the photo:** uses the approved brand art.

### 2. About the teacher

- **Job:** trust — a named, real teacher.
- **Copy (draft):**
  - h2: «عن الأستاذ وائل السعيد» / "About Ustaz Wael El-Saeed"
  - Title chips: the same titles the app's director card shows
    (`InstructorDossierCard`; read from the app's profile data at build,
    not retyped) `[OWNER: confirm the titles to show on the website]`
  - Bio: `[OWNER: 2–4 sentences: years teaching, what he teaches, the
    center. Not written by us.]`
- **Visual:** portrait (square, 320 px, radius 12, hairline ring) at the
  start edge, text beside it on tablet/desktop, stacked on mobile.
- **Facts / assets:** `[OWNER: the portrait in the repo
  (frontend/assets/images/director_portrait.jpg, v3) was consented for the
  app; confirm the director's and the photographer's consent for the
  website too.]`
- **Production without the bio:** shows name + titles + portrait only; no
  invented text.

### 3. Subjects by year (live catalog)

- **Job:** "they cover my year" in one glance; send to the catalog.
- **Copy (draft):** h2 «المواد حسب الفرقة» / "Subjects by year"; lead
  «اختر فرقتك وشوف المواد المتاحة.» / "Pick your year and see what is
  available."; link «كل المواد» / "All subjects" → `/subjects`.
- **Content:** from the catalog API, never hard-coded: one group per study
  type (today: «ليسانس الحقوق» / LL.B., «دبلومات الدراسات العليا» /
  Postgraduate Diplomas, «التدريب المهني والعملي» / Vocational Training),
  one card per level with its subject count and a link to that level on
  `/subjects` (anchor). Prices are not repeated here (the catalog shows them).
- **Data rules seen in the live catalog (2026-11-07):** levels with no
  subjects are hidden; a level whose English title is empty (one diploma
  level today) shows its Arabic title on `/en` with `lang="ar"`; if the
  catalog is unavailable at build, the section shows only the "All
  subjects" link.
- **Visual:** horizontal pill tabs per study type (app `PillFilterBar`
  look) are client JS; to stay script-free, use stacked groups with
  level cards in a 1 / 2 / 3-column grid instead.

### 4. How to start (3 steps)

- **Job:** make the payment step feel simple and safe.
- **Copy (draft):** h2 «تبدأ إزاي؟» / "How to start"
  1. «اختار المادة وادفع» — «أونلاين بتحويل (محفظة أو InstaPay) من الموقع، أو في السنتر.»
     / "Choose and pay — online by transfer (wallet or InstaPay) on this site, or at the center."
  2. «المادة بتتفعّل على حسابك» — «بعد ما نراجع التحويل.» `[OWNER: how long
     activation usually takes, e.g. "within 24 hours"? We will not state a
     time until you give one.]`
     / "The subject is activated on your account — after we check the transfer."
  3. «ذاكر من التطبيق» — «الفيديوهات والمذكرات على التطبيق بنفس الحساب.»
     / "Study in the app — videos and notes, same account."
- **Visual:** three numbered cards (crimson numeral, large display size so
  crimson passes 3:1), a thin connector line between them on desktop; on
  mobile a vertical list.
- **Store safety:** this is the website; it may describe buying. The app
  never links here (unchanged).

### 5. The app (`#app`)

- **Job:** show where the studying happens; get the install.
- **Copy (draft):** h2 «كل محاضراتك في مكان واحد» / "All your lectures in
  one place" (the app's own store headline); lead «فيديوهات ومذكرات
  مرتبة لكل مادة، وحسابك وأجهزتك تحت إيدك.» / "Videos and notes organised
  by subject, and your account in your hands."
- **Visual:** 2–3 app screenshots in phone frames (ar screenshots on `/`,
  en on `/en`), the two official store badges below.
- **Facts / assets:**
  `[OWNER: OK to use the store screenshots (Claude outputs/store-assets/screenshots,
  not in git today) on the website? They must be added to asset-provenance.]`
  `[OWNER: confirm both store URLs are live — the site uses
  play.google.com/store/apps/details?id=app.elmetracademy.elmetr and
  apps.apple.com/app/el-metr-academy/id6737155342.]`
  `[OWNER: the official "Get it on Google Play" and "Download on the App
  Store" badges — download them from Google's and Apple's badge pages (we
  must not redraw them), or approve that the agent downloads them.]`
- **Production without screenshots:** badges + headline only.

### 6. The center

- **Job:** it is a real place; you can study or pay there.
- **Copy (draft):** h2 «السنتر» / "The center"; lead «تقدر تدفع وتفعّل
  المادة في السنتر كمان.» / "You can also pay and activate a subject at
  the center."
- **Content:** address, opening hours, phone (the number already on the
  brand art, 0122 27 007 27, only if the owner confirms it is the center's
  number), a plain link "Open in Maps" (no embedded map, no map script).
- **Visual:** photo strip (2–3 photos, lazy), details card beside it.
- **Facts / assets:** `[OWNER: center address (ar + en), hours, map link,
  phone/WhatsApp to show, 2–3 photos with consent of anyone visible.]`
- **Production without facts:** the section is omitted.

### 7. Testimonials

- **Only with real quotes** the owner gives, each with the student's
  written consent and the name form they agreed to (full, first name, or
  initials). `[OWNER: none received yet.]`
- **Production without them:** the section is omitted entirely (no
  placeholder quotes, no ratings, no counts).

### 8. FAQ

- **Job:** answer the questions that stop a purchase.
- **Markup:** native `<details>`/`<summary>` (keyboard and screen-reader
  friendly, no script), h2 «أسئلة شائعة» / "FAQ".
- **Questions (draft) — answers needed:**
  1. «الدفع بيتم إزاي؟» / "How does payment work?" — draft from the built
     flow: transfer to a wallet or InstaPay shown on the subject page, then
     submit the transfer reference, sender number and amount; or pay at
     the center. `[OWNER: confirm wording; which wallets.]`
  2. «التفعيل بياخد قد إيه؟» / "How long does activation take?" —
     `[OWNER: answer]`
  3. «أقدر أذاكر على كام جهاز؟» / "On how many devices can I study?" —
     the app allows 2 devices per account (SPEC; the website does not
     count). `[OWNER: confirm this is how you want it said.]`
  4. «أنا بذاكر في السنتر، أعمل إيه؟» / "I study at the center — what do I
     do?" — `[OWNER: answer: does a center student need the app account,
     who activates it?]`
  5. «لو التحويل اتعمل غلط؟» / "What if my transfer was wrong?" —
     `[OWNER: answer; links to the refund policy (still a TODO page).]`
- **Production:** only questions with an owner answer are shown.

### 9. Closing call to action

- **Copy (draft):** h2 «جاهز تبدأ؟» / "Ready to start?"; primary «تصفّح
  المواد» / "Browse subjects"; text link «حمّل التطبيق» / "Get the app".
- **Visual:** crimson-washed band (`--crimson-tinted` over canvas, text
  passes at 9.6:1), centred.

## Page-wide

- **Meta:** title «أكاديمية المتر — شرح مواد كلية الحقوق» / "EL METR
  Academy — Law school subjects, explained"; description = the hero lead.
  `noindex` stays until the owner says the site may be indexed.
- **Open Graph image:** static 1200 × 630 made from the brand art and the
  wordmark (no new photo needed). Favicon set from `logo-512.png`
  (16/32/180/192/512 + SVG mask if feasible).
- **Performance:** hero image ≤ 120 KB AVIF at 1440 width, WOFF2 fonts,
  only the hero preloaded; JS added to `/` = 0 (all sections are server
  rendered; only the existing header/cart/auth client code runs).
- **Accessibility:** one h1 (hero), h2 per section, skip link, 44 px
  targets, alt text on every image (portrait: «الأستاذ وائل السعيد» /
  "Ustaz Wael El-Saeed").

## Facts and assets needed from the owner

| # | Item | Section | Blocks production? |
|---|---|---|---|
| 1 | Hero photo of the center or the teacher — or OK to use the brand art | 1 | No (falls back to brand art if approved) |
| 2 | Confirm the hero promise line | 1 | No |
| 3 | Website consent for the director portrait (director + photographer) | 2 | Yes for the portrait |
| 4 | Teacher bio (2–4 sentences, ar + en) and titles to show | 2 | No (section shows name only) |
| 5 | Typical activation time | 4, 8 | No (step text says "after we check") |
| 6 | Use of the store screenshots on the website | 5 | No |
| 7 | Store URLs confirmed live (Android, iOS) | 5, footer | Yes for the badges |
| 8 | Official store badges (or approval for the agent to download them) | 5, footer | Yes for the badges |
| 9 | Center address, hours, map link, phone/WhatsApp | 6 | Section omitted without it |
| 10 | Center photos (with consent of people shown) | 6 | No |
| 11 | Testimonials with written consent | 7 | Section omitted without them |
| 12 | FAQ answers (payment wording, activation, devices, center students, wrong transfer) | 8 | Unanswered questions hidden |
| 13 | Refund policy text (still a TODO page) | 8, footer | Existing gap |
| 14 | OK to allow search indexing (drop `noindex`) | page | No |
