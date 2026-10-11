DROP TABLE IF EXISTS email_verifications;

ALTER TABLE users
DROP COLUMN IF EXISTS is_verified,
DROP COLUMN IF EXISTS last_name,
DROP COLUMN IF EXISTS first_name;
