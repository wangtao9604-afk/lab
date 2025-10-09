-- Migration for existing deployment
-- Purpose: Add conversation_records table or upgrade it with deduplication support

-- 1) If content_digest column does not exist, add it as a generated column
SET @col_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
   WHERE TABLE_SCHEMA = DATABASE()
     AND TABLE_NAME = 'conversation_records'
     AND COLUMN_NAME = 'content_digest'
);
SET @sql := IF(
  @col_exists = 0,
  'ALTER TABLE conversation_records
     ADD COLUMN content_digest BINARY(32)
     GENERATED ALWAYS AS (UNHEX(SHA2(content, 256))) STORED
     COMMENT ''SHA-256 hash of content, used for deduplication''
     AFTER occurred;',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- 2) If unique constraint uk_rec_dedup does not exist, add it
SET @idx_exists := (
  SELECT COUNT(*) FROM information_schema.STATISTICS
   WHERE TABLE_SCHEMA = DATABASE()
     AND TABLE_NAME = 'conversation_records'
     AND INDEX_NAME = 'uk_rec_dedup'
);
SET @sql := IF(
  @idx_exists = 0,
  'ALTER TABLE conversation_records
     ADD UNIQUE KEY uk_rec_dedup (user_id, source, occurred, content_digest);',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- 3) Optional: Add idx_user_occurred if missing
SET @i1 := (
  SELECT COUNT(*) FROM information_schema.STATISTICS
   WHERE TABLE_SCHEMA = DATABASE()
     AND TABLE_NAME = 'conversation_records'
     AND INDEX_NAME = 'idx_user_occurred'
);
SET @sql := IF(
  @i1 = 0,
  'ALTER TABLE conversation_records
     ADD KEY idx_user_occurred (user_id, occurred);',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- 4) Optional: Add idx_occurred if missing
SET @i2 := (
  SELECT COUNT(*) FROM information_schema.STATISTICS
   WHERE TABLE_SCHEMA = DATABASE()
     AND TABLE_NAME = 'conversation_records'
     AND INDEX_NAME = 'idx_occurred'
);
SET @sql := IF(
  @i2 = 0,
  'ALTER TABLE conversation_records
     ADD KEY idx_occurred (occurred);',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
