# Site and theme settings

Beat stores public presentation settings in SQLite and keeps metrics in MTS.
The settings are intentionally limited to non-executable values: site text,
branding URLs, a default color mode, and two public presentation switches.

## Data model

The singleton `site_settings` row contains:

- site title and description;
- optional HTTP(S) or root-relative logo and favicon URLs;
- default theme: `system`, `light`, or `dark`;
- whether public node responses expose host addresses;
- whether the public dashboard renders network quality.

Custom HTML, CSS, scripts, uploaded theme archives, credentials, and runtime
server settings are out of scope. This avoids creating an XSS or arbitrary-file
surface in the PoC.

The later commercial package boundary preserves this trusted settings model and
isolates executable public bundles from administrator state; see
[`theme-packages-market-design.md`](./theme-packages-market-design.md).

## API and authorization

- `GET /api/v1/settings/site` is public and returns only the fields above.
- `PUT /api/v1/settings/site` requires the existing administrator bearer token.
- Turning off IP display clears `host` in public node REST and metrics WebSocket
  responses. Authenticated node management responses retain the address.

## Frontend behavior

The public settings provider updates the document title, description, favicon,
header branding, and default theme without requiring authentication. A visitor's
explicit local light/dark choice continues to override the server default.
Administrators edit all fields at `/admin/settings` using shadcn form controls.

## Acceptance

- Fresh browsers use the persisted site metadata and default theme.
- Logo and favicon URLs reject unsafe schemes and oversized values.
- Public IP and network-quality switches take effect without exposing an admin
  endpoint or token.
- Settings survive restart, writes require authorization, and existing public
  dashboard access remains login-free.

Community capability evidence:

- Komari site and theme settings: <https://github.com/komari-monitor/komari/blob/main/internal/config/settings.go>
- DStatus persisted site and personalization settings: <https://github.com/fev125/dstatus/blob/main/database/setting.js>
