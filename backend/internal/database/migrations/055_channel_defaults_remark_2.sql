-- Migration 055: เพิ่ม remark_2 ใน channel_defaults
-- sentinel '' = ไม่ระบุ (เหมือน wh_code/shelf_code)
ALTER TABLE channel_defaults
  ADD COLUMN IF NOT EXISTS remark_2 VARCHAR(20) NOT NULL DEFAULT '';
