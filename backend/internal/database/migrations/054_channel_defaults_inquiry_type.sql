-- Migration 054: เพิ่ม inquiry_type ใน channel_defaults
-- ใช้ sentinel -1 = ยังไม่ได้ตั้งค่า (เหมือน vat_type, vat_rate)
-- admin ตั้งค่าได้ใน /settings/channels → dialog ส่ง SML จะ pre-fill ให้
ALTER TABLE channel_defaults
  ADD COLUMN IF NOT EXISTS inquiry_type INT NOT NULL DEFAULT -1;
