# Acceptance TLS fixtures

- `tls-expired.*` expired on 2026-08-14 and exercises the expired-certificate path.
- `tls-expiring-20d.*` expires 20 days after generation on 2026-09-04 and exercises the warning window.

Both are self-signed localhost certificate/key pairs used for real TLS fault injection. Rotate the 20-day pair when the acceptance candidate changes; do not replace it with a clock or test-only switch.
