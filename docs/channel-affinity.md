# Claude Code session affinity

Rules in `channel_affinity_setting.rules` can use `{"type":"claude_session"}`
as a key source. It first reads `X-Claude-Code-Session-Id`, then extracts the
session ID from Claude Code's `metadata.user_id`: either a JSON-encoded string
with `session_id`, or the legacy `user_..._session_...` string. An absent or
unrecognized session falls through to the next configured key source.

- `user_ids` restricts a rule to authenticated platform user IDs. Omitted or
  empty means all users; client metadata does not select the platform user.
- `include_token_id: true` isolates bindings by the authenticated API Key ID
  and key source type. Requests without an authenticated Key ID skip the rule.
  The credential itself is never used. Header and body session IDs share the
  `claude_session` source; fallback bindings remain separate.
- Keep `include_model_name`, `include_using_group`, and `include_rule_name`
  enabled when each model, group, and rule should have independent bindings.
- A following `{"type":"context_int","key":"token_id"}` source provides
  per-Key fallback for clients that omit the session ID.
- Successful requests refresh the TTL. A 3600-second TTL expires the binding
  after one hour without a successful request; it does not retain conversation
  contents. With `skip_retry_on_failure: false`, normal channel retry still
  applies. Affinity is a preference, not a guarantee of upstream cache hits.

Both admin themes preserve `user_ids` and `include_token_id` when editing an
existing rule. Configure these two fields in the rules JSON editor.

Deploy support to every replica before enabling rules with these fields.
Older binaries ignore the user filter and Key isolation fields. Before rolling
back the binary, restore a rule compatible with that version. Changing a rule's
name with `include_rule_name: true` starts fresh bindings; old entries expire
through their existing TTL.
