-- Add user-level cache transfer configuration and group-level probability
-- Migration: 046
-- Description: Add probability-based cache transfer and user-level override for cache transfer settings

-- 用户表添加转移配置（NULL 表示使用分组配置）
ALTER TABLE users
ADD COLUMN cache_read_transfer_ratio DECIMAL(5,4) DEFAULT NULL,
ADD COLUMN cache_read_transfer_probability DECIMAL(5,4) DEFAULT NULL;

-- 分组表添加概率字段（默认 1.0 = 100% 触发，向后兼容）
ALTER TABLE groups
ADD COLUMN cache_read_transfer_probability DECIMAL(5,4) NOT NULL DEFAULT 1.0;

COMMENT ON COLUMN users.cache_read_transfer_ratio IS '用户级缓存转移比例(0~1)，覆盖分组配置';
COMMENT ON COLUMN users.cache_read_transfer_probability IS '用户级转移触发概率(0~1)，覆盖分组配置';
COMMENT ON COLUMN groups.cache_read_transfer_probability IS '转移触发概率(0~1)，默认1.0始终触发';
