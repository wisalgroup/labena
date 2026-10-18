# Documentation

Start with the [introduction](overview.md) to explore the framework. This file
explains how contributors organize, build and verify the documentation website.

The website is built from Markdown and served with Go. It uses local CSS,
JavaScript, and a pinned syntax highlighter; no CDN or application backend is
required. Open http://127.0.0.1:8091/overview.html after starting the preview.

## Build and preview

Run from the repository root:

```sh
go run docs/build.go
go run docs/serve.go
```

Rebuild after Markdown, navigation, layout, CSS or JavaScript changes, then refresh.
Generated pages use content-versioned assets so browsers load the current search
index and page tools after an update. Stop the server with Ctrl+C. The server
binds to loopback, not a public interface. Static HTML also works directly from disk.

Over HTTP, internal links load articles without reloading the shared layout. Browser
Back and Forward restore the page and scroll position; section links, search results,
code tools, and Markdown downloads follow the current page. Direct URLs still work
on ordinary static hosting without rewrite rules. Opening files from disk, disabling
JavaScript, or a failed page fetch uses normal full-page navigation.

## Canonical conventions

[Project conventions](conventions.md) defines architecture, ownership, naming and
lifecycle. Source code and compiled examples define the shipped API. A guide should
explain those choices, not introduce a competing convention.

Historical files in `review/`, `plans/` and `REWRITE_PLAN.md` record earlier decisions;
they are not part of the published learning path. Do not describe a planned feature
as available or use a historical example instead of its current implementation.

## Content map and learning paths

Readers should be able to follow the documentation from their first request to a
running application, or jump directly to an operation they need.

| Category | Reader's question | Main content |
| --- | --- | --- |
| Getting started | How do I adopt, run and update this? | Introduction, installation, configuration, first module, local development, adoption and updates |
| Architecture | Where does this code belong? | Structure, entry points, lifecycle, registration, dependency injection, shared types and conventions |
| HTTP APIs | How do I expose a use case or call another API? | Routes, handlers, requests, responses, middleware, application errors, HTTP client and WebSocket extension guidance |
| Data and storage | How do I read, save and coordinate data? | Connections, queries, transactions, migrations, seeders, cache, file storage and media |
| Security | How does a user sign in and keep a session? | OTP, social sign-in, profiles, authorization, refresh and logout |
| Background work | How do I run work later or react to a change? | Console commands, tasks, workers, retries, events and recurring schedules |
| Communication | How do I send a message? | Mail views, mail, notifications and push |
| Recipes | How do the components work together? | Persisted HTTP/CLI operation, transactional notification intent, onboarding |
| Testing | How do I verify behavior? | HTTP, modules, database transactions, tasks and events |
| Operations | How do I run and diagnose this? | Deployment, logging, observability, troubleshooting and release checks |
| Reference | What are the options and maintenance commitments? | Configuration, live API reference, support policy and security reporting |

The introduction offers a short learning path and a task directory. The sidebar
is the complete catalog: keep each guide in one group, ordered from first use to
advanced work. Put complete cross-capability examples in Recipes; keep their
individual API explanations with the owning topic. Configuration and API reference
pages answer lookup questions, while contributor history stays outside navigation.

### Navigation contract

`navigation.json` is the single source for topic groups, page order and titles.
The generated sidebar, previous/next links and search use it together. The current
topic opens automatically; other groups can be expanded independently. Group
choices persist in session storage for this tab, and the current page is always
revealed after navigation. Expand all and Collapse all are explicit controls.

On narrow screens, Menu opens the same navigation tree in a native modal dialog.
Escape, the close button and selecting a guide dismiss it. The document remains
the article's scroll owner; the sidebar, outline, code blocks and search results
scroll locally when necessary. The page outline follows the active section.

Search supports Cmd/Ctrl+K, arrow keys, Enter, Escape and an explicit Clear button.
It indexes local content only, highlights matching text and exposes useful guides
before a query is entered. It does not persist queries or send them to a server.
IME composition must finish before search or shortcuts run. A failed search-index
load provides recovery instructions; ordinary navigation remains available.

Native disclosure groups, section links and Markdown downloads work without
JavaScript. Keep the old `index.html` installation URL and all published guide
slugs and anchors. The brand and README lead new readers to `overview.html`.
The header displays the documented release series as a link to the support policy;
do not add a version selector until separately hosted versions exist.

`style.css` owns documentation tokens and component styles; `layout.html` owns
the shared page structure, and `app.js` owns navigation and search behavior.
The documentation section in the root `DESIGN.md` records their design contract.

Keep one authoritative explanation per concept. The queues guide owns task creation,
workers and failures; old queue pages lead readers to the matching section. The mail
guide owns mail views. Filesystems and media remain separate because raw storage and
managed assets have different responsibilities. Package READMEs may be the canonical
published source; do not duplicate them into a second manual under `docs/`.

Reuse a few complete workflows across guides. The Catalog starter introduces module
construction and registration. Profile updates demonstrate request mapping and
transactions. Onboarding connects events, jobs, notifications and mail. Each topic
explains its own boundary and links to the full workflow.

## Add or move a page

`navigation.json` defines category order, page titles, Markdown sources and output
slugs. Sources can live anywhere in the repository, including package READMEs:

```json
{
  "title": "Media assets",
  "slug": "media",
  "source": "internal/platform/media/README.md"
}
```

Edit source Markdown, never generated `*.html`, `markdown/*` or `search-index.js`.
The build renders those outputs and validates local links and exact heading anchors.

When consolidating content, preserve the old source, slug and important headings as
a concise linking page. Add `"hidden": true` to that page's navigation entry. It will
still build and receive a Markdown download, but is omitted from the sidebar,
Previous/Next sequence and search. Do not hide a page until its replacement covers
the material and its section links work. Do not silently delete established URLs.

Links to registered Markdown sources are rewritten to published HTML, including
relative links from package READMEs. Prefer those links when the Markdown must also
work on a source hosting site. HTML links such as
`configuration.html#understand-precedence` are supported too. Use relative paths so
the site works beneath a hosting path prefix. External links are not checked by the
builder; verify referenced external instructions separately.

## Write a practical guide

A substantial guide normally follows this progression. Adapt it to the capability;
small references do not need empty sections to satisfy a template.

1. **Purpose.** State the problem, when to use the capability, and meaningful choices.
2. **First success.** Show prerequisites, a working example and its expected outcome.
3. **Common operations.** Use task-based headings: “Delay a task” or “Return a private URL.”
4. **Configuration and integration.** Put relevant settings and module wiring beside the operation.
5. **Failures and guarantees.** Explain acceptance, persistence, retries, cancellation and transaction boundaries where they matter.
6. **Verification.** Show a useful test or diagnostic, symptoms and recovery steps.
7. **Advanced use.** Explain independent construction, extension points and native-library escape hatches.
8. **Continue.** Link to a few specific next operations or complete workflows.

Use one H1, H2 for major sections, and H3 for details. Keep headings descriptive:
“Configure retries” is easier to find than “Advanced.” Avoid manually maintaining a
second table of contents: the builder creates section outlines and heading links.
Search indexes each H2/H3 separately, including its code, page and parent-section
context. API symbols such as `response.JSON` are searchable.

Keep prose short and purposeful. Use a table for choices, snippets for operations,
and a diagram when ownership or sequence needs a picture. Keep essential limits in
view; a reader should not need the final paragraph to discover that an operation is
best-effort or cannot join a SQL transaction.

### Make examples usable

Every example should answer **where it goes**, **what it needs**, and **what happens**.

- Give a filename and full imports for a first complete file.
- Label shorter snippets as an existing function body or a focused change; identify assumed values such as `ctx`, `resources` or `userID`.
- Use a preceding label such as **File:** `internal/modules/catalog/register.go`. It remains readable in downloaded Markdown and without JavaScript.
- Show meaningful error handling and the expected response, command output or stored result.
- Fence code with `go`, `sh`, `dotenv`, `json`, `yaml`, `sql`, `http` or `text`.
- Use placeholders for credentials; never publish values from a local environment.
- Link complete runnable examples to their maintained source instead of copying many variations.
- State when manual wiring is required. Distinguish reusable `pkg` imports from application-owned `internal/platform` imports.

Similar names do not imply identical guarantees. For example, mail enqueueing and
notification enqueueing have different persistence boundaries. Explain the actual
contract beside each call, including what successful return acknowledges.

### Complete one topic before moving on

A finished guide has:

- Verified APIs, defaults, prerequisite services and lifecycle ownership.
- A first-success path plus its useful variations and failure behavior.
- Checked examples, with complete files linked to or compared against compiled code.
- Working onward links, preserved legacy URLs and no conflicting duplicate explanation.
- Usable code blocks, section navigation and search on desktop and narrow screens.

Run the relevant example and configuration checks below, then rebuild to check all
local links. Record any checks that depend on unavailable infrastructure rather than
claiming they passed.

## Visual explanations

Use a diagram when relationships, branches or boundaries are harder to follow in
prose. Use a walkthrough for an ordered process where each step has a different
responsibility or guarantee. Keep directory trees as text and comparisons as tables.
Prefer one focused visual to a diagram of the entire system; retain the surrounding
explanation and link to the deeper guide.

Fence diagrams with `mermaid`. Include `accTitle` and `accDescr` so each diagram has
a visible title, a plain-language explanation and an accessible SVG description:

```mermaid
flowchart LR
    accTitle: Intent before delivery
    accDescr: Commit notification intent to PostgreSQL before a worker attempts external delivery.
    Intent[(Committed intent)] --> Worker[Delivery worker]
    Worker --> Provider[External provider]
```

Prefer top-to-bottom flows for long processes. Label arrows with what happens and
make failure paths explicit. Do not encode a guarantee solely in color. Use an
expanded view for complex diagrams; smaller screens can scroll within the figure.
The renderer uses the site's light/dark palette and runs again after SPA navigation.
Source stays available below each diagram, including when rendering fails.

A walkthrough is an ordinary Markdown blockquote with a marker, a short introduction
and an ordered list. Start each step with a bold title:

```markdown
> [!WALKTHROUGH] Follow one operation
>
> Each step introduces one boundary.
>
> 1. **Save intent.** Persist the work in the business transaction.
>
>    No external provider is called yet.
>
> 2. **Deliver later.** A worker attempts the committed work.
>
>    The handler must tolerate repeated delivery.
```

The builder preserves the full ordered explanation. JavaScript adds step selection,
Previous/Next controls and an announced position; print and no-JavaScript views show
every step. Walkthroughs reuse the article palette, typography and spacing from
`style.css`; `visuals.js` owns their shared behavior and diagram rendering.

Mermaid Tiny **11.17.2** is pinned locally in `vendor/mermaid.min.js`, with its MIT
license in `vendor/mermaid.LICENSE` and bundled third-party notices retained. It loads
only when an article contains a diagram; no browser CDN, remote fonts or rendering
service is required. This build supports the flowcharts, sequences and state diagrams
used here, but excludes mindmaps, architecture diagrams, KaTeX and ELK layout.
Rendering uses strict security mode, with diagram-provided click actions disabled.

Upstream: [Mermaid usage](https://mermaid.js.org/config/usage.html),
[accessibility](https://mermaid.js.org/config/accessibility), and
[pinned bundle](https://cdn.jsdelivr.net/npm/@mermaid-js/tiny@11.17.2/dist/mermaid.tiny.js).

After changing visuals, rebuild and check direct loads, internal navigation,
Back/Forward and anchors, light/dark themes, keyboard controls, narrow layouts,
expanded diagrams and the source fallback. Run the builder tests below too.

Before publishing, build the site, check local links and anchors, verify snippets
against their callers, and inspect desktop and narrow layouts. Search, copying,
the theme switch, and keyboard navigation should work without external requests.
Static section outlines, heading links and the sidebar must remain usable with
JavaScript disabled; search and clipboard enhancements require JavaScript.
Do not claim unrun database or broker checks passed.

## Code theme and dependencies

Code panels use the editor's Catppuccin Macchiato palette: `#24273a` background,
`#cad3f5` text, purple keywords, green strings, blue italic functions, yellow
italic types, pink italic parameters, and teal operators. Nested brackets use
its six-color cycle. Fira Code is used when installed, with monospace fallbacks.

Go snippets are highlighted at build time using the standard Go scanner and AST.
The parser recognizes complete files, declarations, and statement fragments.
This distinguishes calls, types, package qualifiers, and scoped parameters without
changing copied text. It does not run gopls or load dependencies: incomplete
snippets and unresolved external symbols cannot reproduce every editor semantic
token. Import underlines match the editor styling but are not navigation links.

Other languages use Highlight.js 11.11.1, vendored in `vendor/highlight.min.js`
with its BSD license. Unsupported languages remain plain text. The browser skips
Go blocks that were already highlighted by the builder.

CI also checks complete starter, entry-point, task and mail examples against their
compiled source files, including the transactional profile example, and fails when generated guides differ from committed output.
Run `go test ./tests/modules` after editing those examples.

The configuration reference is maintained with its owner: `go test ./internal/app/config -run TestConfigurationReferenceMatchesDefaults` checks
key coverage and defaults, including named queue priorities and filesystem overrides.
Update the reference tables alongside settings changes; do not add a second
runtime configuration schema to generate documentation.

Verify the highlighter after changes:

```sh
go test docs/build.go docs/highlight_test.go
go vet docs/build.go
go run docs/build.go
```

The optional browser regression suite exercises the actual generated pages under
a hosting path prefix. It checks search, keyboard navigation, mobile focus,
Back/Forward, clipboard, themes, overflow and static fallbacks. It requires Node.js
and Playwright for verification only; neither is needed to build or serve the site.
Install the browser tools outside the repository:

```sh
npm install --prefix /tmp/labena-docs-browser --no-save playwright
/tmp/labena-docs-browser/node_modules/.bin/playwright install chromium
NODE_PATH=/tmp/labena-docs-browser/node_modules node docs/navigation.test.cjs
```

To use an existing Chrome installation, set
`PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH` to its executable. Set
`DOCS_SCREENSHOT_DIR` to save representative screenshots. The suite starts and
stops its own loopback server. Run it after rebuilding when navigation, layout,
search or visual behavior changes. A manual browser review should also include
a long guide, light/dark themes and JavaScript disabled.

Set `DOCS_SITE_DIR` to a packaged site's directory to run the same checks against
the exact files prepared for static hosting, instead of the working `docs/` tree.

Vendored source: https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.11.1/highlight.min.js
License: https://github.com/highlightjs/highlight.js/blob/11.11.1/LICENSE

## Static hosting

Deploy generated `*.html`, `style.css`, `app.js`, `visuals.js`, `search-index.js`, `markdown/`,
and `vendor/` together, preserving relative paths. No version selector or server
rendering is required. Build tools and source Markdown do not need to be public.
Keep legacy landing pages alongside current guides when content is consolidated.
The site needs no redirects or server rewrite rules to preserve those URLs.

## Publish with GitHub Pages

The repository includes [Publish documentation](../.github/workflows/docs.yml), a
manually triggered GitHub Actions workflow. It builds the existing static site;
no Jekyll conversion, Node.js build or application server is needed.

### Enable and publish

1. Commit and push the documentation and workflow to the repository's default
   branch (`main` for Labena).
2. Open the repository's **Settings → Pages**. Under **Build and deployment**, set
   **Source** to **GitHub Actions**. You do not need to add a suggested Jekyll workflow.
3. Open **Settings → Environments → github-pages** and restrict deployment branches
   to the default branch. Create the environment if it is not present yet.
4. Open **Actions → Publish documentation → Run workflow**, select the default
   branch and run it.
5. After both jobs succeed, open the deployment URL shown by the workflow or
   **Settings → Pages → Visit site**.

For a public project site with the current owner and repository name, the expected
address is [wisalgroup.github.io/labena/](https://wisalgroup.github.io/labena/).
This address is not live merely because the workflow exists. Its root keeps the
existing installation page; link new readers to
[the introduction](https://wisalgroup.github.io/labena/overview.html).
Custom domains and private Pages sites may have different URLs; use the address
GitHub reports after deployment.

The workflow deploys only from the default branch and requires an explicit run for
each publication. Its build job has read access; only its deploy job can publish
Pages and request the deployment identity token. Actions are pinned to commit SHAs.
No personal access token is needed for a normal deployment from this repository.

See GitHub's [publishing-source instructions](https://docs.github.com/en/pages/getting-started-with-github-pages/configuring-a-publishing-source-for-your-github-pages-site)
and [custom-workflow requirements](https://docs.github.com/en/pages/getting-started-with-github-pages/using-custom-workflows-with-github-pages).

### Private repositories and site visibility

For an organization-owned private repository such as Labena, GitHub Pages requires
an eligible plan, such as GitHub Team or Enterprise Cloud. Public repositories can
use Pages on GitHub Free. If Pages settings are unavailable, check the organization's
plan and policies. See [GitHub Pages availability](https://docs.github.com/en/pages/getting-started-with-github-pages/creating-a-github-pages-site).

**A private repository does not make its Pages site private.** Ordinary Pages sites
are publicly readable. Private project-site access requires an organization on
GitHub Enterprise Cloud and explicit access-control configuration. Decide the
intended visibility in Pages settings before running the publishing workflow.
See [private Pages access](https://docs.github.com/en/enterprise-cloud@latest/pages/getting-started-with-github-pages/changing-the-visibility-of-your-github-pages-site).

### What gets published

The workflow uploads the pages listed in `navigation.json`, their Markdown
downloads, shared CSS/JavaScript and vendored browser assets with their licenses.
Hidden navigation pages are included to preserve existing URLs. It excludes the
repository's application source, environment files, build tools and documentation
review/plan files. Every published guide and download is visible to the site's
audience, including any examples or configuration values in those guides.

Relative links already support a project prefix such as `/labena/`. The search
index, diagrams and Markdown downloads travel with the site. Do not upload the
whole repository or point branch-based Pages at the complete `docs/` directory;
use the prepared artifact so only the intended site files are published.

GitHub Pages serves the **framework guides**. The application's live `/docs` and
`/openapi.json` endpoints still require a running Go service. Publishing the static
manual does not deploy the application, its API or its database.

### Publish automatically later

To publish on pushes as well as manual runs, change the workflow trigger to:

```yaml
on:
  push:
    branches: [main]
  workflow_dispatch:
```

Use your actual default branch name. Retain the deploy environment and branch
restriction. Build on every push, or include all guide sources in any path filter:
published content also comes from package READMEs, `SUPPORT.md`, `SECURITY.md` and
`deployments/`, so filtering only `docs/**` can leave the site stale.

### Troubleshoot a publication

| Symptom | Check |
| --- | --- |
| The workflow is absent | The workflow file must be pushed to the default branch |
| The jobs are skipped | Run the workflow from the repository's default branch |
| Pages configuration or deployment fails | Pages source is GitHub Actions, the plan allows Pages, and organization policies permit the workflow/actions |
| The deployment waits | Review the `github-pages` environment's protection rules |
| The site or a guide returns 404 | Wait for deployment success, then use GitHub's reported URL and retain the `.html` filename |
| The site shows older content | Re-run the workflow after pushing changes and reload the page |

Optional custom domains are configured in **Settings → Pages**, with matching DNS
records. Enable **Enforce HTTPS** when GitHub makes it available. See
[custom domain setup](https://docs.github.com/en/pages/configuring-a-custom-domain-for-your-github-pages-site).
