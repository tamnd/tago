# tago

tago is a static site generator written in Go. It builds incrementally: each run only re-renders pages whose source files changed since the last build. On a site with no changes, a build completes in under 5ms. On a 1000-page site with one changed file, you typically see 10-30ms.

It works with Hugo themes out of the box. Drop any of the 370+ officially registered Hugo themes into a `themes/` directory and tago renders it correctly, running the same template logic Hugo does.

## Install

```sh
go install github.com/tamnd/tago/cmd/tago@latest
```

You need a C compiler (gcc or clang) because the cache uses SQLite via CGO.

## Quick start

```sh
mkdir mysite && cd mysite

cat > content/index.md << 'EOF'
---
title: Home
---
Hello from tago.
EOF

tago build
tago serve
```

The site is at `localhost:1313`. Edit a file and it rebuilds in the background; the browser refreshes automatically.

## Performance

tago stores a SHA-256 hash for each content file in a SQLite database at `public/.tago-cache.db`. On each build it reads the hashes, skips files that have not changed, and only renders what is new or modified.

Some real numbers from local benchmarks:

| Scenario | Time |
|---|---|
| No changes (4-page site) | 4ms |
| Full build, 4 pages | 16ms |
| Single file changed, 100-page site | ~20ms |
| Full build, 100-page site | ~150ms |

Aggregate pages like the search index, site graph, sitemap, and tag pages are always rebuilt because their input is the full page set. Everything else is skipped when its hash matches.

When you upgrade tago or change a layout file, a `render_version` stamp in the cache causes every page to be re-rendered on the next build. You do not need to run `tago clean` manually.

## Hugo theme compatibility

tago implements the Hugo template API. You can use any Hugo theme:

```sh
git clone https://github.com/adityatelange/hugo-PaperMod themes/hugo-PaperMod
tago build --theme hugo-PaperMod
```

623 themes have been tested against the official Hugo theme registry (gohugoio/hugoThemes) with zero render errors. The compatibility layer handles:

- Page methods: `.Title`, `.Content`, `.Date`, `.Params`, `.RelPermalink`, `.Pages`, `.Site`, `.Scratch`, `.File`, and 40+ more
- Site methods: `.Site.Params`, `.Site.Menus`, `.Site.Taxonomies`, `.Site.Home`, `.Site.RegularPages`
- Partials: `{{ partial "name" . }}` with `{{ return }}` support
- Inline `{{ define }}` blocks inside partial files
- All standard template functions: `where`, `first`, `last`, `dict`, `slice`, `append`, `markdownify`, `i18n`, `urlize`, `path.Join`, `urls.Parse`, and 100+ more
- Date formatting with Hugo named layouts (`:date_long`, `:date_medium`, `:date_short`)
- Taxonomies, menus, pagination stubs, scratch pads, resource stubs

See [specs/hugo-compatibility.md](specs/hugo-compatibility.md) for the full list.

## Site structure

```
mysite/
  tago.toml
  content/
    _index.md         home page
    about.md
    blog/
      _index.md       section index
      first-post.md
  layouts/            overrides theme templates
    baseof.html
    page.html
  static/             copied verbatim to output
  themes/
    my-theme/
      layouts/
      static/
  public/             generated output
```

## tago.toml

```toml
title       = "My Site"
baseURL     = "https://example.com/"
description = "A site about things"
contentDir  = "content"
outputDir   = "public"
staticDir   = "static"
layoutsDir  = "layouts"
lang        = "en"

[params]
author = "Jane"
```

## Commands

```sh
tago build [flags]    Build the site
tago serve [flags]    Dev server with live reload
tago clean            Remove the output directory
tago version          Print version
```

Build flags:

```
--content <dir>       Content directory (default: content)
--output <dir>        Output directory (default: public)
--static <dir>        Static assets directory (default: static)
--layouts <dir>       Custom layouts directory (default: layouts)
--theme <name>        Load theme from themes/<name>/
--base-url <url>      Base URL (default: http://localhost:1313/)
--title <title>       Site title
--clean               Delete output before building
--buildDrafts         Include pages with draft: true
--buildFuture         Include future-dated pages
--buildExpired        Include expired pages
```

Serve flags take all the same options plus `--port`.

## Writing templates

tago uses Go's `html/template`. Templates have access to:

```
.Title          .Description     .Summary
.Content        .RawContent      .ReadingTime
.Date           .PublishDate     .Lastmod
.Permalink      .RelPermalink    .Section
.Kind           .Type            .Layout
.IsPage         .IsHome          .IsSection
.Draft          .Weight          .WordCount
.Tags           .Categories      .Params
.Pages          .RegularPages    .Sections
.Resources      .Paginator       .Scratch
.File           .Parent          .TableOfContents

.Site.Title     .Site.BaseURL    .Site.Description
.Site.Params    .Site.Pages      .Site.RegularPages
.Site.Taxonomies .Site.Menus     .Site.Home
.Site.Data      .Site.Language
```

## Built-in pages

tago generates a few pages that Hugo does not have natively:

- `/search/` - client-side full-text search (FlexSearch)
- `/graph/` - knowledge graph linking pages by their internal links (D3.js)
- `/tree/` - content hierarchy as a mindmap (D3.js)
- `/calendar/` - pages laid out on a calendar by date

Override any of them by putting a template in `layouts/` with the same name.

## License

MIT
