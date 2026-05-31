-- Migration 053: ลบ sml_db.* และ sml.guid settings ที่ไม่จำเป็นออกจาก app_settings
-- sml-api-byboss จัดการ DB connection เองผ่าน .env (SML_DB_*) ไม่ได้รับ X-DB-* headers
-- sml.guid เป็นค่าตายตัวจาก .env ไม่ควรอยู่ใน DB
DELETE FROM app_settings WHERE key LIKE 'sml_db.%';
DELETE FROM app_settings WHERE key = 'sml.guid';
