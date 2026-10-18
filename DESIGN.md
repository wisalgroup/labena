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

The frontmatter and operations sections cover the embedded `/admin/logs/` screen.
Documentation has its own contract below; the third-party queue dashboard retains
its own styles. Contributors follow
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

## Documentation website

The documentation is a content site for Go developers learning and operating
Labena. Its reference is Laravel's progressive guides and persistent topic
navigation, adapted to explicit Go composition. Keep Labena's teal identity and
the existing editor-inspired code palette. The signature is the reading rail:
collapsible topics on the left, the article in the middle and a section outline
on the right. Active-page and active-section markers share a thin teal rule.
Use typography and whitespace for hierarchy, not promotional cards or decorative
illustrations. The introduction is a practical learning path and task directory.

`docs/style.css` is the canonical token owner. Its CSS variables feed the shared
`docs/layout.html`, search, navigation and `docs/visuals.js`; this document mirrors
the accepted values and intent. There is no token generation or theme adapter.
The operations palette above is deliberately separate and remains unchanged.

| Role | Light | Dark | Runtime token |
| --- | --- | --- | --- |
| Canvas | `#ffffff` | `#101a20` | `--bg` |
| Quiet surface | `#f7f9fa` | `#15232b` | `--surface` |
| Reading text | `#26343b` | `#e3edf0` | `--text` |
| Secondary text | `#596871` | `#a5b3bb` | `--muted` |
| Borders | `#e2e8eb` | `#2a3a43` | `--line` |
| Links and current location | `#087a74` | `#73d6c9` | `--accent` |
| Active surface | `#eaf5f3` | `#183337` | `--wash` |
| Inline code | `#f0f3f5` | `#22323c` | `--inline` |
| Code panel / code text | `#24273a` / `#cad3f5` | Same | `--code` / `--code-text` |

Use local system sans for prose and controls, and Fira Code when installed with
SFMono/Consolas fallbacks for code. No font downloads or font-swap shifts. Body text
is 16px with 1.8 line height; page titles use restrained 32–44px semibold sans.
The article is at most 760px wide. Controls and code panels use 7px corners.
The header is 76px tall, or 64px on small screens. Below 1200px the outline becomes
a native disclosure; below 820px navigation moves into a native modal drawer.

The document scrolls naturally. Sidebar and outline independently scroll within
the viewport; code and tables contain their own horizontal overflow. Scrollbars
inherit global thumb, track, hover and active tokens, with a forced-colors fallback.
Only modal dialogs lock background scrolling. No motion is needed for navigation.
Keep a visible focus treatment, semantic controls and readable light/dark contrast.
Use system color preference initially and retain an explicit theme choice locally.

`docs/app.js` owns the single sidebar tree, modal search and navigation history.
Native details/summary supplies topic disclosure; native dialog supplies modality
and focus isolation. Search results remain real links, with arrow-key enhancement
and an IME-safe Clear action. Content queries stay transient in the page, while
topic preferences persist only for the tab. `docs/README.md` owns the navigation
and authoring contract. Static pages, direct URLs, and section links remain usable
without JavaScript. Do not add a frontend framework or remote search dependency
for these interactions.
