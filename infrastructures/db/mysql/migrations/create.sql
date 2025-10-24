-- create.sql
-- 目标：创建数据库 ipang（utf8mb4，MySQL 8.0+ 使用 utf8mb4_0900_ai_ci），
--      并创建 conversation_records 表（生成列 + 唯一约束 + 常用索引）

-- 1) 创建数据库（幂等）
CREATE DATABASE IF NOT EXISTS `ipang`
  DEFAULT CHARACTER SET = utf8mb4
  DEFAULT COLLATE = utf8mb4_0900_ai_ci;

-- 如果已经事先创建了ipang数据库，运行下面的命令修改字符集
ALTER DATABASE ipang CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;


-- 切换至目标库
USE `ipang`;

-- Create conversation_records table for new deployment
-- Purpose: Store user-AI conversation history with deduplication
-- 若为 MySQL 8.0+，将库级排序规则设置为 utf8mb4_0900_ai_ci（与设计对齐）

CREATE TABLE IF NOT EXISTS conversation_records (
  id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id          VARCHAR(64)     NOT NULL COMMENT 'External user ID from WeCom',
  source           ENUM('USER','AI') NOT NULL COMMENT 'Message source: USER question or AI answer',
  content          MEDIUMTEXT      NOT NULL COMMENT 'Message content',
  occurred         DATETIME(3)     NOT NULL COMMENT 'When this conversation happened (from Kafka header)',

  -- Generated column: SHA-256 digest of content for deduplication
  -- Database auto-computes this, application does not need to write it
  content_digest   BINARY(32)
    GENERATED ALWAYS AS (UNHEX(SHA2(content, 256))) STORED
    COMMENT 'SHA-256 hash of content, used for deduplication',

  created_at       DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at       DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
                                   ON UPDATE CURRENT_TIMESTAMP(3),

  PRIMARY KEY (id),

  -- Idempotent unique constraint: same user_id + source + occurred + content_digest = duplicate
  -- OnConflict{DoNothing:true} in GORM will silently ignore duplicate rows
  UNIQUE KEY uk_rec_dedup (user_id, source, occurred, content_digest),

  -- Secondary indexes for common queries
  KEY idx_user_occurred (user_id, occurred),
  KEY idx_occurred (occurred)
)
ENGINE = InnoDB
DEFAULT CHARSET = utf8mb4
COLLATE = utf8mb4_0900_ai_ci
COMMENT = 'Conversation records from qywx-recorder Kafka topic';
