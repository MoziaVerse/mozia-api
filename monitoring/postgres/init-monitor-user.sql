-- Idempotent creation of a least-privilege read-only monitoring user for postgres_exporter.
-- Usage (run as PostgreSQL superuser against the target database, e.g. mozia_api_db):
--
--   psql -U admin -d mozia_api_db -v monitor_password="<STRONG_RANDOM_PASSWORD>" -f init-monitor-user.sql
--
-- Never commit real passwords into Git. Pass the password via -v at runtime only.

\if :{?monitor_password}
\else
\echo 'ERROR: pass -v monitor_password="your_strong_password"'
\quit 1
\endif

SELECT format(
  'CREATE ROLE mozia_monitor WITH LOGIN PASSWORD %L CONNECTION LIMIT 5',
  :'monitor_password'
)
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mozia_monitor')\gexec

SELECT format(
  'ALTER ROLE mozia_monitor WITH LOGIN PASSWORD %L CONNECTION LIMIT 5',
  :'monitor_password'
)\gexec

-- pg_monitor grants read-only access to pg_stat_activity, pg_stat_database,
-- pg_stat_bgwriter, pg_locks, pg_stat_replication, pg_settings, etc.
GRANT pg_monitor TO mozia_monitor;

-- Allow connecting to the mozia_api_db database (and postgres default db for exporter probes).
GRANT CONNECT ON DATABASE postgres TO mozia_monitor;
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_database WHERE datname = current_database()) THEN
    EXECUTE format('GRANT CONNECT ON DATABASE %I TO mozia_monitor', current_database());
  END IF;
END
$$;

-- Optional: enable pg_stat_statements extension if shared_preload_libraries includes it.
-- CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
