# Localization parity

Updated: 2026-07-30

Status: reviewed design; implementation requires explicit approval

## Goal and evidence

Komari Web stable `1.3.2` ships English, Simplified Chinese, Traditional
Chinese, Japanese, and Indonesian catalogs. Beat currently ships only English
and Simplified Chinese. Commercial parity requires every public and
administrator workflow to be usable in the same five locale families.

Evidence:

- <https://github.com/komari-monitor/komari-web/tree/14f4067e9b69813b24a0255e565f7f49bff0a1bd/src/i18n/locales>

Beat will add `zh-TW`, `ja`, and `id` while keeping English the source locale
and preserving the existing concise Simplified Chinese wording.

## Invariants

1. Every source-locale key exists in every shipped catalog. Unknown keys,
   duplicate keys, malformed placeholders, and source/target interpolation
   mismatch fail CI and production build.
2. Runtime fallback is exact locale to language family to English. It never
   renders a raw key in a production build.
3. Locale selection affects visible labels, dates, times, numbers, units,
   accessibility names, validation errors, empty/error states, confirmations,
   notifications, and downloaded human-readable reports. It never changes API
   keys, metric names, IDs, enum values, paths, commands, or audit action codes.
4. Security copy preserves meaning. Translation cannot weaken a warning,
   confirmation target, role, recent-auth requirement, destructive effect, or
   data-privacy statement.
5. User-provided node/group/site names remain unchanged and safe. Locale choice
   is local browser presentation state only in version one and is not sent as
   visitor identity or written to SQLite.
6. Public access remains no-login in every locale; administration remains
   authenticated in every locale.

## Catalog and tooling

Replace the current fragmented hand-merged dictionaries with one typed catalog
registry that still allows feature-owned files. A build-time script loads every
catalog through a structured parser and verifies:

- exact key-set equivalence with English;
- string values only and bounded per-entry/catalog sizes;
- balanced named placeholders with identical names and escaping contract;
- no blank translations, control characters, HTML, script, or URL injection;
- no locale file or generated artifact outside the frontend source tree.

Generated type declarations make `t()` accept only known keys. Dynamic key
construction is forbidden unless it passes through a typed enum-to-key map.
Plural/count messages use `Intl.PluralRules`; dates, numbers, byte/rate units,
percentages, and relative time use the corresponding `Intl` APIs rather than
translated string concatenation.

The first revision has no remote translation download, runtime catalog upload,
administrator translation editor, HTML messages, or locale-specific executable
code.

## Frontend experience

The public and admin headers replace the binary language toggle with a compact
globe icon `DropdownMenu`. Items display native names plus an unambiguous code:

- English (`EN`)
- 简体中文 (`简`)
- 繁體中文 (`繁`)
- 日本語 (`JA`)
- Bahasa Indonesia (`ID`)

The trigger is an icon button with tooltip and accessible name; it does not
become a rounded text pill. The selected locale uses a check icon. Mobile uses
the same menu in the existing header/navigation surface, not a separate card.

Initial locale resolution is: valid local choice, then exact browser locale,
then supported language family, then English. Changing locale updates the
current screen without reload and preserves navigation, form state, selected
node, chart range, and active dialog. Persisting the locale handles storage
denial without breaking rendering.

Layouts use the existing Geist/shadcn `base-nova` system and semantic tokens.
No font is downloaded by locale. Text wraps naturally; compact controls have
stable responsive dimensions, tooltips for unavoidable truncation, and no
negative letter spacing or viewport-scaled type.

## Translation lifecycle

English copy is the review source. Each feature change adds all locale keys in
the same change; placeholder English in a non-English catalog fails review.
Security/restore/remote-operation translations require a second reviewer or a
recorded language-quality check before release.

CI generates a pseudo-locale that expands text, adds delimiters, and exercises
long unbroken values without shipping it. It renders representative public,
admin, dialog, table, card, chart, mobile, empty, loading, and error states to
detect clipping and untranslated keys.

## Tests and acceptance

Unit tests cover locale parsing/fallback, storage failures, typed keys, catalog
equivalence, placeholders, plural rules, dates, numbers, byte/rate units,
relative time, and untranslated-key failure. Component tests cover the language
menu, no page reload, state preservation, accessible names, and selector labels
remaining names rather than IDs.

Browser tests exercise setup, login/TOTP, public Fleet, node history, settings,
alerts, terminal, backup/restore, operations, PWA/wallboard, and every destructive
confirmation in all five locales at desktop and mobile widths. Pseudo-locale
screenshots and DOM overflow assertions cover text clipping and overlap.

Acceptance requires:

- exact complete catalogs for `en`, `zh-CN`, `zh-TW`, `ja`, and `id`;
- no raw translation key, English placeholder, overflow, or broken control in
  production browser sweeps;
- correct localized units/date/number behavior without changing API values;
- no reload or workflow-state loss on locale change;
- at least 90 percent frontend line coverage, lint/typecheck/build, dependency
  audit, browser smoke/E2E, accessibility checks, and IPv4/IPv6 public/admin
  acceptance aligned with CI.

## Version and approval boundary

This is schema-free release batch `localization-parity` in
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md).
It should follow `fleet-public` so the full default public experience is covered
and must accompany later batches with keys added in the same change. It changes
no API, SQLite migration, backup format, or MTS measurement.

Implementation requires explicit approval because it changes visible behavior,
locale resolution, catalog tooling, and the language control across every page.
