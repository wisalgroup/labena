---
version: alpha
name: Labena operations
description: A quiet, readable local log inspection surface for application operators.
colors:
  canvas: "#f2f5fa"
  surface: "#ffffff"
  ink: "#18283e"
  muted: "#52647b"
  border: "#ccd5e1"
  accent: "#2457b8"
  soft: "#e9effb"
  danger: "#ad2435"
  warning: "#825400"
  success: "#166047"
typography:
  sans:
    fontFamily: '"Segoe UI", system-ui, sans-serif'
  mono:
    fontFamily: '"SFMono-Regular", Consolas, "Liberation Mono", monospace'
rounded:
  DEFAULT: "8px"
spacing:
  page-max: "1440px"
  page-padding: "36px"
components:
  button: {}
  input: {}
  select: {}
  log-entry: {}
---

# Labena operations UI

This contract covers the embedded `/admin/logs/` screen. Documentation and the
third-party queue dashboard retain their own styles. Contributors follow
`docs/conventions.md`; viewer access and data scope follow `docs/logging.md`.

## Context and direction

Operators need to find an error, correlate its request, and inspect its fields.
This is a dense diagnostic tool, not a marketing surface. The signature is a
calm, aligned log ledger: severity, channel and message share predictable columns,
with full records disclosed inline. Avoid decorative charts and status claims
that imply distributed coverage. English labels; browser-local date formatting.
No geographic market assumptions or external fonts/assets.

## Runtime ownership

`internal/platform/logviewer/web/app.css` is the canonical token source; this file
mirrors its light palette and intent. CSS custom properties feed controls and log
rows directly. The dark media query supplies the corresponding readable dark
palette. No generated token adapters or application component library is needed.
Compare the properties with this document whenever changing the visual language.

Use system sans for controls and local monospace for record content. Type spans
11–14px for dense controls/content and 36px for the page title. Eight-pixel corners,
tonal grouping and borders supply hierarchy; no floating overlays or shadows.
Wide screens use a four-column record summary; narrow screens put the message on
its own row. The document is the single scroll owner. Stable scrollbar space and
fixed action labels avoid layout shifts. System light/dark preference is respected.

## Canonical UI map

| Capability | Canonical owner | Source of truth | Allowed variants | Verification |
|---|---|---|---|---|
| Select/Listbox | Native select in viewer HTML | This contract and app.css | OS-owned popup geometry and keyboard interaction | Browser selection and narrow viewport |
| Scrollbar | app.css document styles | This contract | One document scroller, no fixed-height panels | Browser overflow checks |
| Toast | Persistent inline status and alert regions in app.js | docs/logging.md and this contract | Polite status, assertive failure | Browser refresh/failure checks |

## Interaction and accessibility

Use native buttons, labeled inputs/selects and details/summary disclosures. Focus
is visible; severity has a text label as well as color. Errors keep existing rows
available and expose Refresh as the recovery action. Initial empty, filtered empty,
loading, busy, paused and failure states have distinct text. Opening an entry pauses
updates to preserve reading and keyboard focus. No animations are needed.

Search is debounced, IME-safe and cancels stale requests; clearing is immediate.
Queries intentionally stay out of navigation URLs and persistent browser storage
because log searches can contain sensitive values. The viewer is read-only.
Export applies to visible previews only; Copy copies the selected entry. No delete,
configuration editing, or claim to complete history is offered.

Basic Auth uses the browser's authentication prompt, as requested. Every viewer
route requires the same credentials. No credentials are stored by the page.
The same-origin and file-access policies remain enforced by the Go handler.
