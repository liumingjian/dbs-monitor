# Acceptance TLS fixtures

- `tls-expired.*` expired on 2026-08-14 and exercises the expired-certificate path.
- `tls-expiring-20d.*` expires 20 days after generation on 2026-09-04 and exercises the warning window.

Both are self-signed localhost certificate/key pairs used for real TLS fault injection. Rotate the 20-day pair when the acceptance candidate changes; do not replace it with a clock or test-only switch.

# Acceptance role fixtures

`roles.yaml` is the harness input for role-aware acceptance runs. The harness creates the three long-lived accounts once through the user-management API and uses them read-only across entries. Tests that mutate an account create a disposable user from the declared pattern, replacing `{entry_id}` with the lowercase matrix entry ID and `{sequence}` with a per-entry counter.

`roles.yaml` intentionally contains no passwords. The harness consumes the one-time password returned by `POST /api/v1/users` and never writes `app_user` or `user_session` directly.
