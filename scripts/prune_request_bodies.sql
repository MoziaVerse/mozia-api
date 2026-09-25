-- PostgreSQL operations only. Never deletes log rows or changes financial fields.
BEGIN;
SET LOCAL statement_timeout = '5s';
SET LOCAL lock_timeout = '500ms';
CREATE OR REPLACE FUNCTION pg_temp.without_request_body(payload text) RETURNS text
LANGUAGE plpgsql AS $$
DECLARE parsed jsonb;
BEGIN
    parsed := payload::jsonb;
    IF jsonb_typeof(parsed) <> 'object' OR NOT parsed ? 'request_body' THEN
        RETURN NULL;
    END IF;
    RETURN (parsed - 'request_body')::text;
EXCEPTION WHEN data_exception THEN
    -- Some legacy payloads contain Unicode PostgreSQL JSONB cannot represent.
    -- Keep those rows intact; the operator receives a skipped count.
    RETURN NULL;
END;
$$;
WITH batch AS MATERIALIZED (
    SELECT id, created_at, other FROM logs
    WHERE (created_at, id) > (:cursor_time, :cursor)
        AND created_at < :cutoff AND type IN (2, 5)
    ORDER BY created_at, id LIMIT 50 FOR UPDATE
), candidates AS MATERIALIZED (
    SELECT id, other, CASE WHEN strpos(other, '"request_body"') > 0
        THEN pg_temp.without_request_body(other) END AS pruned FROM batch
), updated AS (
    UPDATE logs SET other = candidates.pruned FROM candidates
    WHERE logs.id = candidates.id AND candidates.pruned IS NOT NULL
    RETURNING logs.id
)
SELECT json_build_object(
    'cursor', COALESCE((SELECT id FROM batch ORDER BY created_at DESC, id DESC LIMIT 1), :cursor),
    'cursor_time', COALESCE((SELECT created_at FROM batch ORDER BY created_at DESC, id DESC LIMIT 1), :cursor_time),
    'examined', (SELECT count(*) FROM batch),
    'updated', (SELECT count(*) FROM updated),
    'skipped', (SELECT count(*) FROM candidates
        WHERE strpos(other, '"request_body"') > 0 AND pruned IS NULL),
    'removed_text_bytes', COALESCE((SELECT sum(octet_length(other) - octet_length(pruned))
        FROM candidates WHERE pruned IS NOT NULL), 0)
);
COMMIT;
