-- 添加使用日志错误记录字段
-- Add error recording fields to usage_logs table

-- 错误标志
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS is_error BOOLEAN NOT NULL DEFAULT FALSE;

-- 错误类型（如 billing_error, rate_limit, no_account, upstream_error 等）
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS error_type VARCHAR(64);

-- 返回给客户端的 HTTP 状态码
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS error_status_code INTEGER;

-- 错误消息摘要
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS error_message VARCHAR(2048);

-- 完整错误响应体（JSON）
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS error_body TEXT;

-- 请求头快照（JSON，白名单过滤后）
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS request_headers TEXT;

-- 上游返回的 HTTP 状态码
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS upstream_status_code INTEGER;

-- 上游错误消息
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS upstream_error_message TEXT;

-- 上游错误事件列表（JSON 数组，SSE error 事件）
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS upstream_errors TEXT;

-- 创建索引以支持错误日志查询
CREATE INDEX IF NOT EXISTS usage_logs_is_error_idx ON usage_logs (is_error);
CREATE INDEX IF NOT EXISTS usage_logs_error_type_idx ON usage_logs (error_type);
CREATE INDEX IF NOT EXISTS usage_logs_is_error_created_at_idx ON usage_logs (is_error, created_at);
