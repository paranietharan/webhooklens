CREATE TABLE IF NOT EXISTS webhook_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    endpoint_id VARCHAR(32) NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    method VARCHAR(16) NOT NULL,
    headers JSONB NOT NULL DEFAULT '{}'::jsonb,
    query_params JSONB NOT NULL DEFAULT '{}'::jsonb,
    body BYTEA,
    ip_address VARCHAR(45) NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_webhook_logs_endpoint_received ON webhook_logs(endpoint_id, received_at DESC);
