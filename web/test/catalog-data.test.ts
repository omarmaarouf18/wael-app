// Unit tests for the public catalog parser (src/lib/catalog-data.ts).
// Run: npm test (node --test; Node 24 strips the TypeScript types).

import assert from "node:assert/strict";
import { test } from "node:test";

import { allSubjects, findSubject, parseCatalog, pick } from "../src/lib/catalog-data.ts";

function subject(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: "s-1",
    level_key: "bachelor-y1",
    term: "first",
    title: { ar: "القانون المدني", en: "Civil Law" },
    description: { ar: "وصف", en: "About" },
    price: 1800,
    currency: "EGP",
    instructor: { ar: "وائل السعيد", en: "Wael El Saeed" },
    counts: { videos: 12, books: 1, notes: 3 },
    access_expires_at: "2027-02-15T21:59:00Z",
    ...over,
  };
}

function catalogWith(subjects: unknown[]): Record<string, unknown> {
  return {
    study_types: [
      {
        key: "bachelor",
        title: { ar: "ليسانس الحقوق", en: "LL.B." },
        levels: [{ key: "bachelor-y1", study_type: "bachelor", title: { ar: "الفرقة الأولى", en: "Year 1" }, position: 1, subjects }],
      },
    ],
  };
}

test("parses the API shape into the site shape", () => {
  const c = parseCatalog(catalogWith([subject()]));
  assert.ok(c);
  const [s] = allSubjects(c);
  assert.deepEqual(s, {
    id: "s-1",
    levelKey: "bachelor-y1",
    term: "first",
    title: { ar: "القانون المدني", en: "Civil Law" },
    description: { ar: "وصف", en: "About" },
    price: 1800,
    currency: "EGP",
    instructor: { ar: "وائل السعيد", en: "Wael El Saeed" },
    counts: { videos: 12, books: 1, notes: 3 },
    accessExpiresAt: "2027-02-15T21:59:00Z",
  });
});

test("drops every field the site does not render, content fields included", () => {
  const c = parseCatalog(
    catalogWith([subject({ youtube_video_id: "LEAK", videos: [{ id: "v1" }], files: [{ storage_key: "k" }], owned: true })]),
  );
  assert.ok(c);
  const json = JSON.stringify(c);
  for (const banned of ["LEAK", "youtube", "videos\":[", "storage_key", "owned"]) {
    assert.ok(!json.includes(banned), `parsed catalog contains ${banned}`);
  }
});

test("skips a malformed subject and keeps the others", () => {
  const bad = [
    subject({ id: "../etc" }),
    subject({ id: "s-2", price: -1 }),
    subject({ id: "s-3", price: 1.5 }),
    subject({ id: "s-4", currency: "USD" }),
    subject({ id: "s-5", term: "third" }),
    subject({ id: "s-6", title: { ar: "", en: "No Arabic" } }),
    subject({ id: "s-7", counts: { videos: 1, books: 1 } }),
    subject({ id: "s-8", level_key: "bachelor-y2" }),
    "not an object",
  ];
  const c = parseCatalog(catalogWith([...bad, subject({ id: "ok" })]));
  assert.ok(c);
  assert.deepEqual(
    allSubjects(c).map((s) => s.id),
    ["ok"],
  );
});

test("a repeated subject id keeps its first occurrence", () => {
  const c = parseCatalog(catalogWith([subject({ price: 1 }), subject({ price: 2 })]));
  assert.ok(c);
  assert.deepEqual(
    allSubjects(c).map((s) => s.price),
    [1],
  );
});

test("a wrong top-level shape is null, not an empty catalog", () => {
  for (const raw of [null, [], "x", {}, { study_types: {} }]) {
    assert.equal(parseCatalog(raw), null);
  }
  assert.deepEqual(parseCatalog({ study_types: [] }), { studyTypes: [] });
});

test("a level without a subjects array is dropped; an empty one stays", () => {
  const c = parseCatalog({
    study_types: [
      {
        key: "bachelor",
        title: { ar: "ل", en: "" },
        levels: [
          { key: "bachelor-y1", study_type: "bachelor", title: { ar: "أ", en: "" } },
          { key: "bachelor-y2", study_type: "bachelor", title: { ar: "ب", en: "" }, subjects: [] },
        ],
      },
    ],
  });
  assert.ok(c);
  assert.deepEqual(
    c.studyTypes[0]?.levels.map((l) => l.key),
    ["bachelor-y2"],
  );
});

test("findSubject and pick", () => {
  const c = parseCatalog(catalogWith([subject({ title: { ar: "عربي", en: "" } })]));
  assert.ok(c);
  const found = findSubject(c, "s-1");
  assert.equal(found?.level.key, "bachelor-y1");
  assert.equal(found?.studyType.key, "bachelor");
  assert.equal(findSubject(c, "missing"), null);
  assert.equal(pick({ ar: "عربي", en: "" }, "en"), "عربي");
  assert.equal(pick({ ar: "عربي", en: "English" }, "en"), "English");
  assert.equal(pick({ ar: "عربي", en: "English" }, "ar"), "عربي");
});
