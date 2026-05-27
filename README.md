# tago

A fast, incremental static site generator written in Go.

tago is built around one idea: only rebuild what changed. It hashes every content file and skips pages whose inputs haven't changed since the last run. On a 5000-page site, a typical incremental build takes 10-30 seconds instead of 5 minutes.

## How it works

tago stores a SQLite cache (`public/.tago-cache.db`) that maps each content file's SHA-256 hash to its rendered output path. On each build it scans the content directory, computes hashes, and renders only the pages whose hash changed or whose output file is missing. Special pages (search index, graph, tree, calendar, sitemap, tags) are always rebuilt since they aggregate content.

## Features

- Incremental builds via SQLite hash cache
- Markdown with front matter (goldmark + goldmark-meta)
- Go `html/template` for layouts with full template inheritance
- Knowledge graph, mindmap tree, and calendar views (D3.js)
- Client-side full-text search (FlexSearch)
- Fingerprinted static assets for cache busting
- Live reload dev server (watches content and layouts)
- KaTeX math rendering
- Tags and taxonomy pages
- Sitemap generation

## Install

```sh
CGO_ENABLED=1 go install github.com/tamnd/tago/cmd/tago@latest
```

CGO is required for the SQLite cache (`go-sqlite3`).

## Usage

```sh
# Build the site
tago build

# Start dev server with live reload
tago serve

# Remove output directory
tago clean
```

Build flags:

```
--content <dir>    Content directory (default: content)
--output <dir>     Output directory (default: public)
--static <dir>     Static assets directory (default: static)
--layouts <dir>    Custom layouts directory (default: layouts)
--base-url <url>   Base URL (default: http://localhost:1313/)
--title <title>    Site title (default: My Site)
--clean            Delete output before building
```

## Site structure

```
mysite/
  tago.toml          # site config (optional)
  content/en/        # markdown content
    _index.md        # home page
    about.md
    blog/
      _index.md      # section page
      post-1.md
  layouts/           # custom html/template files (optional)
    baseof.html
    page.html
    section.html
  static/            # static files copied to output
    css/
    js/
  public/            # generated output
```

## tago.toml

```toml
title       = "My Site"
baseURL     = "https://example.com/"
description = "A site built with tago"
contentDir  = "content/en"
outputDir   = "public"
staticDir   = "static"
layoutsDir  = "layouts"
```

## Themes

tago's default templates are minimal. You can override any template by placing a file with the same name in your `layouts/` directory.

Available layout templates: `baseof.html`, `page.html`, `section.html`, `home.html`, `taxonomy.html`, `term.html`, `search.html`, `404.html`, `graph.html`, `tree.html`, `calendar.html`.

Template data available in all layouts:

```
.Page.Title, .Page.Description, .Page.ContentHTML
.Page.RelPermalink, .Page.Kind, .Page.Type
.Page.Date, .Page.Tags, .Page.WordCount, .Page.ReadingTime
.Page.Ancestors, .Page.Children, .Page.Parent
.Site.Title, .Site.BaseURL, .Site.Description
.SidebarItems, .SidebarRoot, .SidebarBack
.PrevPage, .NextPage
.Assets.CSS, .Assets.JS, .Assets.Extra
```

## License

MIT
