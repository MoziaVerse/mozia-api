\set ON_ERROR_STOP on
SET search_path = pg_temp;
CREATE TEMP TABLE logs (id bigint PRIMARY KEY, created_at bigint, type int, other text, quota bigint);
INSERT INTO logs VALUES
 (1, 100, 2, '{"request_body":{"prompt":"private"},"model_price":0.1234567890123456789,"quota":123,"request_summary":{"_has_video":true},"effective_model":"routed"}', 123),
 (2, 100, 5, '{"request_body":{"prompt":"failed"},"error_code":"upstream_error"}', 0),
 (3, 100, 1, '{"request_body":{"note":"topup evidence"},"amount":100}', 100),
 (4, 2000, 2, '{"request_body":{"prompt":"recent"},"quota":9}', 9),
 (5, 100, 2, '{"request_body":{"prompt":"\u0000"},"quota":9}', 9),
 (6, 100, 2, '{"quota":9}', 9),
 (7, 100, 3, '{"request_body":{"note":"management evidence"}}', 0);
\set cursor 0
\set cursor_time 0
\set cutoff 1000
\ir prune_request_bodies.sql
DO $$
BEGIN
 IF (SELECT count(*) FROM logs) <> 7 THEN RAISE EXCEPTION 'Log rows were deleted'; END IF;
 IF (SELECT other::jsonb FROM logs WHERE id=1) <>
   '{"model_price":0.1234567890123456789,"quota":123,"request_summary":{"_has_video":true},"effective_model":"routed"}'::jsonb
 THEN RAISE EXCEPTION 'Financial or routing metadata changed'; END IF;
 IF (SELECT quota FROM logs WHERE id=1) <> 123 THEN RAISE EXCEPTION 'Quota changed'; END IF;
 IF (SELECT other::jsonb FROM logs WHERE id=2) <> '{"error_code":"upstream_error"}'::jsonb
 THEN RAISE EXCEPTION 'Error metadata changed'; END IF;
 IF EXISTS (SELECT 1 FROM logs WHERE id IN (3,4,5,7) AND strpos(other,'"request_body"')=0)
 THEN RAISE EXCEPTION 'Protected, recent or unsupported rows changed'; END IF;
END;
$$;
-- When time advances, a lower ID with a newer timestamp must still expire.
\set cursor 6
\set cursor_time 100
\set cutoff 3000
\ir prune_request_bodies.sql
DO $$
BEGIN
 IF (SELECT other::jsonb FROM logs WHERE id=4) <> '{"quota":9}'::jsonb
 THEN RAISE EXCEPTION 'Timestamp-ordered retention skipped an out-of-order ID'; END IF;
END;
$$;
\echo PASS: expired bodies only; financial precision, routing, recent logs and audit records preserved
