import assert from "node:assert/strict";
import { test } from "node:test";
import { homeCopy } from "../src/lib/home-copy.ts";

test("home copy device FAQ answers match expected copy in Arabic and English", () => {
  const arCopy = homeCopy("ar");
  const enCopy = homeCopy("en");

  const arDeviceFaq = arCopy.faq.items.find((item) => item.q.includes("كام جهاز"));
  assert(arDeviceFaq, "Arabic device FAQ item must exist");
  assert.equal(
    arDeviceFaq.a,
    "على جهازين في نفس الوقت. لو دخلت من جهاز تالت، الجهاز اللي بقاله أطول وقت ما اتستخدمش بيخرج. الدخول من الموقع مش بيتحسب."
  );

  const enDeviceFaq = enCopy.faq.items.find((item) => item.q.includes("devices can I use"));
  assert(enDeviceFaq, "English device FAQ item must exist");
  assert.equal(
    enDeviceFaq.a,
    "Two at the same time. Signing in on a third device signs out the one used least recently. Signing in on the website does not count."
  );
});
