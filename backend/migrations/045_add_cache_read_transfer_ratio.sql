-- Add cache_read_transfer_ratio to groups table
-- Migration: 045
-- Description: Add cache_read_transfer_ratio field to support transferring cache_read tokens to cache_creation for billing adjustment

ALTER TABLE groups
ADD COLUMN cache_read_transfer_ratio DECIMAL(5,4) NOT NULL DEFAULT 0;

COMMENT ON COLUMN groups.cache_read_transfer_ratio IS '缓存读取 token 转移为缓存创建的比例，0~1';
