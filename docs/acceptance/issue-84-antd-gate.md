# Issue 84 AntD instance-list gate

- Date: 2026-08-11
- Browser: Playwright Chromium 151, Linux arm64
- Data: 50 representative instance projections returned by an intercepted list API
- Viewports inspected: 1280 x 720 and 390 x 844
- First render to 50 table rows: 518 ms
- Interaction: selecting `CRITICAL` reduced the table to the expected 10 rows without a visible stall
- Visual check: filters, three-layer health content, markers, independent collection columns, and actions did not overlap; the mobile table retained access to off-screen columns through horizontal scrolling

Result: pass. No AntD interaction lag was observed, so the structural wayfinder stop condition was not triggered.

Reproduction:

```sh
E2E_BASE_URL=http://127.0.0.1:4173 npm run e2e -- instance-list-gate.spec.ts
```
