# Notification channels

Status: Deployed and verified on 2026-07-30

## Goal

Beat sends triggered and resolved alert events through webhook, Telegram and
email channels. Administrators can test a saved channel and inspect its latest
delivery result without creating a real alert event.

## Storage boundary

SQLite continues to store channel identity, type, enabled state and the existing
`config` JSON string. No schema or dependency was added. Agent metrics remain in
MTS and are not copied into SQLite.

Telegram bot tokens and SMTP passwords are currently stored as plaintext inside
channel config. List, create and update responses replace those values with an
empty field plus a `has_bot_token` or `has_password` marker, but response
redaction is not encryption at rest. Leaving the secret blank during an edit
preserves the existing value. Changing the channel type requires a valid
configuration for the new type. The shared `v5` key lifecycle and `v6` channel
backfill are reviewed in
[`application-secret-lifecycle-design.md`](./application-secret-lifecycle-design.md).

## Channel contracts

- Webhook sends the complete alert event as JSON to an HTTP or HTTPS URL.
- Telegram sends a text summary through the Bot API `sendMessage` method.
- Email sends a text message through SMTP using STARTTLS, implicit TLS, or an
  unauthenticated plaintext loopback relay. Plaintext SMTP is rejected for
  non-loopback hosts.

All outbound HTTP clients and SMTP connections have bounded timeouts. Telegram
request errors do not include the token-bearing URL in logs or API responses.

## Delivery status

Real alert delivery and test-send use the same delivery service. The service
records `success` or `failed`, a sanitized message and timestamp for each
channel. Status is intentionally process-local in this PoC, so it resets to
"not sent yet" after a server restart and does not create operational records in
SQLite.

`POST /api/v1/alerts/channels/{id}/test` is protected by the same administrator
authorization as all other channel management operations. A test send does not
create an alert event in SQLite.

## Acceptance

1. Webhook, Telegram and email configurations are validated before storage.
2. Telegram tokens and SMTP passwords never appear in management responses.
3. Blank secrets during same-type edits retain the stored credential.
4. Test-send and real alerts update the same last-delivery status.
5. Triggered and resolved events route through every enabled channel.
6. Public endpoints remain unchanged and channel operations require admin auth.

Deployed acceptance used the IPv6 service endpoint. The public dashboard and
node API returned `200` without a token, while channel list and test-send
returned `401` without administrator authorization. A temporary loopback
webhook test returned `success` and appeared as the channel's latest delivery.
A temporary Telegram config verified response redaction and blank-token update
preservation. Both channels and the listener were removed after verification.

The reviewed commercial expansion for Bark, ServerChan, safe custom HTTP,
encrypted channel revisions, durable retries, delivery history, and outbound
request policy is maintained separately in
[`notification-provider-breadth-design.md`](./notification-provider-breadth-design.md).
It is not implemented by this deployed baseline.
