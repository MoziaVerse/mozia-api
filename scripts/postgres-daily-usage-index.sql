-- Run once with psql -v ON_ERROR_STOP=1 against the PostgreSQL LOG database,
-- outside a transaction. The existing report also works without this index.
-- Building it reads historical log metadata; schedule the one-time I/O load.
-- If interrupted, inspect pg_index.indisvalid before retrying: a failed
-- concurrent build can leave an invalid index. Do not silently skip it.
CREATE INDEX CONCURRENTLY idx_logs_subscription_usage
    ON logs (created_at, user_id, model_name, type, quota)
    WHERE type IN (2, 6) AND other LIKE '%"billing_source":"subscription"%';
