#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

tmp_file="$(mktemp)"
trap 'rm -f "$tmp_file"' EXIT

git ls-files -co --exclude-standard -z \
  | while IFS= read -r -d '' file; do
      case "$file" in
        .git/*|node_modules/*|*/node_modules/*|web/default/dist/*|web/classic/dist/*|dist/*|build/*|*.png|*.jpg|*.jpeg|*.gif|*.webp|*.mp4|*.mov|*.pdf|*.zip|*.gz|*.tar)
          continue
          ;;
      esac
      [ -f "$file" ] || continue
      if LC_ALL=C grep -Iq . "$file"; then
        printf '%s\0' "$file"
      fi
    done \
  | xargs -0 awk '
      BEGIN {
        credential_url = "(postgres(ql)?|mysql|redis|mongodb|amqp|clickhouse)://[^[:space:]/:@]+:[^[:space:]@]+@"
        sensitive_key = "(SQL_DSN|DATABASE_URL|POSTGRES_PASSWORD|PGPASSWORD|DB_PASSWORD|ACCESS_TOKEN|ADMIN_TOKEN|API_TOKEN|WEBHOOK_URL|CLIENT_SECRET|SESSION_SECRET)"
        assignment = sensitive_key "[[:space:]]*[:=][[:space:]]*[\"'\'']?[^[:space:]\"'\'']{8,}"
        private_key = "-----BEGIN (RSA |OPENSSH |EC |DSA |PGP )?PRIVATE KEY-----"
      }
      function placeholder(line) {
        return line ~ /(replace|placeholder|example|your_|your-|<[^>]+>|xxx|random_string|password@localhost|user:password|sk-\.\.\.|change_me|dev_password|never commit|set .* in \.env|\$\{[^}]+\})/
      }
      function allowed_test_fixture(file) {
        return file ~ /_test\.go$/
      }
      function allowed_doc_example(file, line) {
        return file ~ /^README/ && line ~ /-e[[:space:]]+(SQL_DSN|REDIS_CONN_STRING)=/
      }
      function allowed_compose_template(file, line) {
        return (file == "docker-compose.yml" || file == "docker-compose.dev.yml") && line ~ /(SQL_DSN=postgresql:\/\/root:123456@postgres|IMPORTANT: Change the password in production)/
      }
      function allowed_checker_rule(file, line) {
        return file == "scripts/check-secret-leaks.sh" && line ~ /docker-compose\.dev\.yml/ && line ~ /IMPORTANT: Change the password in production/
      }
      function allowed_private_key_wrapper(file, line) {
        return file == "relay/channel/vertex/service_account.go" && line ~ /(strings\.ReplaceAll|pem\.Decode|PRIVATE KEY)/
      }
      function allowed(file, line) {
        return allowed_test_fixture(file) || allowed_doc_example(file, line) || allowed_compose_template(file, line) || allowed_checker_rule(file, line) || allowed_private_key_wrapper(file, line)
      }
      {
        line = $0
        if (line ~ /^[[:space:]]*#/) next
        if (allowed(FILENAME, line)) next
        kind = ""
        if (line ~ credential_url && !placeholder(line)) kind = "credential-url"
        else if (line ~ assignment && !placeholder(line)) kind = "secret-assignment"
        else if (line ~ private_key) kind = "private-key"
        if (kind != "") {
          redacted = line
          gsub(/:\/\/[^[:space:]\/:@]+:[^[:space:]@]+@/, "://***:***@", redacted)
          gsub(/(SQL_DSN|DATABASE_URL|POSTGRES_PASSWORD|PGPASSWORD|DB_PASSWORD|ACCESS_TOKEN|ADMIN_TOKEN|API_TOKEN|WEBHOOK_URL|CLIENT_SECRET|SESSION_SECRET)[[:space:]]*[:=][[:space:]]*["'\'']?[^[:space:]"'\'']+/, "***=***", redacted)
          printf "%s:%d:%s:%s\n", FILENAME, FNR, kind, redacted
        }
      }
    ' > "$tmp_file" || true

if [ -s "$tmp_file" ]; then
  echo "Potential committed secret material found:" >&2
  cat "$tmp_file" >&2
  exit 1
fi

echo "No obvious secret leaks found."
