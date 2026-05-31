-- 015_chat_quick_replies.sql — saved reply templates for the chat inbox
--
-- Admin manages a list of canned responses (Phase 4.4). Composer in
-- /messages opens a popover to pick a template and inject into the textarea.

CREATE TABLE IF NOT EXISTS chat_quick_replies (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  label       TEXT NOT NULL,                 -- short display name in the picker
  body        TEXT NOT NULL,                 -- full text injected into composer
  sort_order  INT  NOT NULL DEFAULT 0,
  created_by  UUID REFERENCES users(id),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS chat_quick_replies_order_idx
  ON chat_quick_replies(sort_order, label);

-- Seed a few starter templates so admins see the feature working out of the
-- box. Idempotent — only inserts when the table is empty.
INSERT INTO chat_quick_replies (label, body, sort_order)
SELECT * FROM (VALUES
  ('ทักทาย', 'สวัสดีค่ะ ยินดีให้บริการนะคะ 🙏', 10),
  ('เช็คสต๊อก', 'ขอเช็คสต๊อกให้สักครู่นะคะ 🙏', 20),
  ('แจ้งราคา', 'ราคารวมทั้งหมด ___ บาทค่ะ ส่งเลขบัญชี/QR ให้ทางใต้นะคะ', 30),
  ('ปิดบิล', 'ขอบคุณที่อุดหนุนค่ะ บิลส่งให้แล้วนะคะ 🙏', 40)
) AS s(label, body, sort_order)
WHERE NOT EXISTS (SELECT 1 FROM chat_quick_replies);
