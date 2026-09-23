import assert from "node:assert/strict";
import test from "node:test";

import { createServer } from "vite";

const vite = await createServer({
  appType: "custom",
  logLevel: "error",
  server: { middlewareMode: true },
});

const { settlementDestinationLabel, settlementPrimaryActionLabel } =
  await vite.ssrLoadModule("/src/lib/tiktok-settlement-presentation.ts");

test.after(async () => {
  await vite.close();
});

test("describes the configured SML destination without hardcoding RC", () => {
  assert.equal(
    settlementDestinationLabel({
      configured: true,
      doc_format_code: "RC",
      passbook_name: "ธนาคารทดสอบ",
    }),
    "เอกสาร SML · รูปแบบ RC · บัญชี ธนาคารทดสอบ",
  );
  assert.equal(
    settlementDestinationLabel({ configured: false }),
    "ยังไม่ได้ตั้งค่าเส้นทาง SML",
  );
});

test("uses a safe generic SML action label", () => {
  assert.equal(
    settlementPrimaryActionLabel("ready", true),
    "ยืนยันสร้างเอกสารใน SML",
  );
  assert.equal(
    settlementPrimaryActionLabel("needs_review", true),
    "ยังสร้างเอกสารไม่ได้",
  );
  assert.equal(
    settlementPrimaryActionLabel("ready", false),
    "ยังไม่ได้ตั้งค่าเส้นทาง SML",
  );
});
