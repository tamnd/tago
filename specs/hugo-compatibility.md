# Hugo Compatibility

tago renders Hugo themes by implementing the same template API that Hugo exposes. Hugo is the reference: when tago and Hugo differ, Hugo wins.

This document covers what tago implements and how far the compatibility goes. It does not cover markdown rendering (handled by goldmark) or the asset pipeline (SCSS, PostCSS -- stubs only).

## Theme testing

623 themes have been tested against the official Hugo theme registry (gohugoio/hugoThemes). All 623 pass with zero render errors. The registry lists 370 unique themes; 10 repositories could not be cloned (deleted or made private). The 623 count includes duplicates used for regression testing.

Testing method: `tago build --theme <name>` on a minimal site with a handful of content pages. Zero exit code and no template errors in stdout = pass.

## Page template data

When a page template executes, `.` (dot) is a struct with these fields and methods.

**Identity and content**

| Field | Type | Notes |
|---|---|---|
| `.Title` | string | from front matter |
| `.Description` | string | from front matter |
| `.Summary` | string | auto-generated, or from front matter `summary` |
| `.Content` | template.HTML | rendered markdown body |
| `.RawContent` | string | raw markdown source before rendering |
| `.WordCount` | int | word count of content |
| `.FuzzyWordCount` | int | word count rounded to nearest 100 |
| `.ReadingTime` | int | estimated minutes to read |
| `.TableOfContents` | template.HTML | auto-generated from headings |

**URLs**

| Field | Type | Notes |
|---|---|---|
| `.Permalink` | string | absolute URL |
| `.RelPermalink` | string | site-relative URL |

**Classification**

| Field | Type | Notes |
|---|---|---|
| `.Kind` | string | page, home, section, taxonomy, term |
| `.Type` | string | content type from front matter, or section name |
| `.Layout` | string | layout name from front matter |
| `.Section` | string | top-level section |
| `.IsPage` | bool | Kind == "page" |
| `.IsHome` | bool | Kind == "home" |
| `.IsSection` | bool | Kind == "section" |
| `.IsNode` | bool | not IsPage |

**Front matter**

| Field | Type | Notes |
|---|---|---|
| `.Date` | HugoTime | from front matter `date` |
| `.PublishDate` | HugoTime | alias for Date |
| `.Lastmod` | HugoTime | from front matter or file mtime |
| `.ExpiryDate` | HugoTime | from front matter `expiryDate` |
| `.Draft` | bool | from front matter |
| `.Weight` | int | from front matter |
| `.Tags` | []string | from front matter |
| `.Categories` | []string | from front matter |
| `.Keywords` | []string | alias for Tags |
| `.Params` | map[string]any | all front matter fields |
| `.Param(key)` | any | looks up front matter first, then site params |

**Navigation**

| Field | Type | Notes |
|---|---|---|
| `.Parent` | safePageRef | parent page, nil-safe |
| `.Pages` | HugoPageList | child pages (for sections) |
| `.RegularPages` | HugoPageList | non-section children |
| `.Sections` | HugoPageList | section children |
| `.AllPages` | HugoPageList | all site pages |
| `.Next` | nil | stub |
| `.Prev` | nil | stub |
| `.NextInSection` | nil | stub |
| `.PrevInSection` | nil | stub |
| `.Ancestors` | HugoPageList | empty stub |

**Resources and assets**

| Field | Type | Notes |
|---|---|---|
| `.Resources` | stubResourceSlice | page resources, always nil (falsy, safely rangeable) |
| `.File` | fileInfo | file metadata |
| `.File.Path` | string | relative path from content dir |
| `.File.Dir` | string | directory |
| `.File.Ext` | string | extension with dot |
| `.File.LogicalName` | string | filename |
| `.File.BaseFileName` | string | filename without extension |
| `.File.TranslationBaseName` | string | base name without language code |
| `.File.UniqueID` | string | MD5 of path |

**State and utilities**

| Field | Type | Notes |
|---|---|---|
| `.Scratch` | Scratch | per-page mutable key-value store |
| `.Store` | Scratch | alias for Scratch (Hugo 0.117+) |
| `.Site` | SiteData | site-wide data |
| `.Language` | SiteLanguage | current language |
| `.TranslationKey` | string | empty stub |
| `.Translations` | []any | empty stub |
| `.AllTranslations` | []any | empty stub |
| `.OutputFormats` | stub | format list stub |
| `.AlternativeOutputFormats` | stub | format list stub |
| `.GitInfo` | nil | stub |
| `.BundleType` | string | empty stub |
| `.HasShortcode(name)` | bool | always false (stub) |
| `.GetPage(args...)` | nil | stub |
| `.Paginator(n...)` | paginatorStub | pagination stub |

## HugoPage

When you range over `.Pages` or `.Site.RegularPages`, each element is a `HugoPage`. It has the same fields as the page data above, plus a `.Site` field so you can access `.Site.Params` from inside a range loop. Hugo themes rely on this heavily.

```
{{ range .Site.RegularPages }}
  {{ .Title }} -- {{ .Site.Params.author }}
{{ end }}
```

## safePageRef

`.Parent` and `.FirstSection` return a `safePageRef` instead of a raw page pointer. Every method on it has a nil check, so accessing `.Parent.RelPermalink` when there is no parent returns an empty string instead of panicking.

Fields: RelPermalink, Permalink, Title, Kind, IsHome, IsSection, Layout, Type, Params, Description.

## Site data (.Site)

| Field | Type | Notes |
|---|---|---|
| `.Site.Title` | string | from tago.toml |
| `.Site.BaseURL` | string | from tago.toml |
| `.Site.Description` | string | from tago.toml |
| `.Site.Author` | map[string]any | from tago.toml [author] |
| `.Site.Params` | map[string]any | from tago.toml [params] |
| `.Site.Pages` | HugoPageList | all pages |
| `.Site.RegularPages` | HugoPageList | non-section pages |
| `.Site.AllPages` | HugoPageList | alias for Pages |
| `.Site.Taxonomies` | TaxonomyList | see Taxonomies section |
| `.Site.Menus` | map[string]MenuList | see Menus section |
| `.Site.Data` | map[string]any | from data/ directory (empty stub) |
| `.Site.Language` | SiteLanguage | current language config |
| `.Site.Languages` | []SiteLanguage | all languages |
| `.Site.LanguagePrefix` | string | empty stub |
| `.Site.IsMultiLingual` | bool | false |
| `.Site.Home` | siteHomeStub | home page object |
| `.Site.GetPage(args...)` | siteHomeStub | page lookup (stub) |
| `.Site.GoogleAnalytics` | string | from .Site.Params.googleAnalytics |
| `.Site.BuildDrafts` | bool | false |
| `.Site.RSSLink` | string | /index.xml |

## HugoPageList methods

`HugoPageList` is `[]*HugoPage` with these methods:

| Method | Notes |
|---|---|
| `.ByWeight()` | sort by Weight ascending, then by Title |
| `.ByTitle()` | alphabetical |
| `.ByDate()` | by Date ascending |
| `.ByPublishDate()` | by PublishDate ascending |
| `.ByLastmod()` | by Lastmod ascending |
| `.ByLength()` | by word count |
| `.ByParam(key)` | by .Params[key] |
| `.Reverse()` | reverse order |
| `.First(n)` | take first n |
| `.Last(n)` | take last n |
| `.Limit(n)` | alias for First |
| `.Where(field, op, val)` | filter; op optional (defaults to ==) |
| `.Where(field, val)` | shorthand |
| `.Related(page)` | nil stub |
| `.GroupByDate(layout)` | []HugoPageGroup |
| `.GroupByPublishDate(layout)` | []HugoPageGroup |
| `.GroupByLastmod(layout)` | []HugoPageGroup |
| `.GroupByParam(key)` | nil stub |
| `.Filter(fn)` | pass-through stub |

## HugoTime

All date fields return a `HugoTime` which wraps `time.Time`. The `Format` method accepts both standard Go time layouts and Hugo's named layouts:

| Named layout | Produces |
|---|---|
| `:date_long` | January 2, 2006 |
| `:date_medium` | Jan 2, 2006 |
| `:date_short` | 1/2/06 |
| `:time_long` | 3:04:05 PM MST |
| `:time_medium` | 3:04:05 PM |
| `:time_short` | 3:04 PM |

```
{{ .Date.Format ":date_medium" }}    --> Jun 1, 2026
{{ .Date.Format "2006-01-02" }}      --> 2026-06-01
```

Other methods: `Unix()`, `IsZero()`, `Before()`, `After()`, `AddDate()`.

## Scratch

`.Scratch` and `.Store` are per-page key-value stores. The same Scratch object is returned every time you access `.Scratch` within one page render, so you can set a value in one place and read it in another.

| Method | Notes |
|---|---|
| `.Set(key, val)` | store any value |
| `.Get(key)` | retrieve value; returns nil if not set |
| `.Add(key, val)` | accumulate: adds numbers, concatenates strings, appends to slices |
| `.SetInMap(key, mapKey, val)` | store into a nested map |
| `.GetSortedMapValues(key)` | values of nested map sorted by key |
| `.Delete(key)` | remove key |

## Taxonomies

`.Site.Taxonomies` is a map of taxonomy name to term map. Both tags and categories are built from the site's pages.

```
{{ range $name, $term := .Site.Taxonomies.tags }}
  {{ $term.Name }}: {{ $term.Count }} posts
{{ end }}
```

Each term entry has: `Name` (string), `Count` (int), `Term` (alias for Name), `Pages()` (nil stub), `Page()` (nil stub).

## Menus

`.Site.Menus["main"]` returns a list of menu entries. Configure menus in front matter or tago.toml.

Each `MenuEntry` has: Name, URL, Weight, Identifier, Title, Pre, Post, HasChildren, Children, Page, IsMenuCurrent, HasMenuCurrent.

```
{{ range .Site.Menus.main }}
  <a href="{{ .URL }}">{{ .Name }}</a>
{{ end }}
```

## Pagination

`.Paginator()` and `.Paginator(n)` return a `paginatorStub`. All pages appear in a single page; Next and Prev are nil. This is enough for most themes to render without errors.

Fields: Pages, PageNumber, TotalPages, TotalNumberOfElements, HasPrev, HasNext, Prev, Next, Pagers, First, Last.

## Template functions

### Strings

```
lower upper title trim trimPrefix trimSuffix
replace replaceRE split substr slicestr
printf sprintf print println truncate
markdownify humanize pluralize singularize
urlize anchorize plainify emojify
htmlEscape htmlUnescape
safeHTML safeCSS safeURL safeJS safeHTMLAttr
```

All string functions accept `any` -- if you pass `nil` or `template.HTML` they handle it without erroring.

### strings namespace

```
strings.HasPrefix strings.HasSuffix strings.Contains
strings.Count strings.Repeat
strings.Replace strings.ReplaceAll
strings.TrimPrefix strings.TrimSuffix
strings.TrimLeft strings.TrimRight
strings.ToLower strings.ToUpper strings.Title strings.Trim
strings.RuneCount strings.Substr
```

### Math

```
add sub mul div mod modBool
math.Abs math.Ceil math.Floor math.Round
math.Log math.Log2 math.Sqrt
math.Max math.Min math.Pow math.Sum
```

`div` returns int when both operands are int, float64 otherwise. Division by zero returns 0.

### Collections

| Function | Notes |
|---|---|
| `where col field [op] val` | filter; operators: ==, !=, <, <=, >, >=, in, not in, intersect |
| `first n col` | first n items |
| `last n col` | last n items |
| `after n col` | skip first n |
| `index col keys...` | nested map/slice access |
| `slice items...` | build a hugoSlice (has .Reverse/.First/.Last/.Len) |
| `append v args...` | append to slice, returns hugoSlice |
| `union a b` | union of two string slices |
| `intersect a b` | intersection |
| `complement base others...` | items in base not in others |
| `symdiff a b` | symmetric difference |
| `uniq a` | deduplicate |
| `flatten vals...` | flatten nested slices |
| `sort col args...` | sort a collection |
| `shuffle col` | deterministic shuffle |
| `in set val` | membership test |
| `len v` | length of string/slice/map |
| `seq args...` | integer sequence: seq N, seq first last, seq first incr last |
| `dict k v ...` | build a map from key-value pairs |
| `merge maps...` | merge maps, last wins |
| `keys m` | sorted keys of a map |
| `values m` | values of a map |

### Type conversion

```
int float string bool
default isset
reflect.IsSlice reflect.IsMap reflect.IsArray
reflect.IsFloat reflect.IsInt reflect.IsBool
reflect.IsInvalid reflect.TypeOf reflect.KindOf
```

### URLs and paths

```
absURL relURL urlize
urls.Parse(url)           returns struct with Host, Path, Scheme, RawQuery, Fragment
path.Join(elems...)       accepts any type
path.Base(s) path.Dir(s) path.Ext(s) path.IsAbs(s)
```

### Dates

```
now                       HugoTime for current time
time(v)                   parse or convert to HugoTime
time.Now                  same as now
time.Format(layout, t)    format a time
dateFormat(layout, v)     alias
```

### Partials

```
partial "name" ctx             render partials/name.html
partialCached "name" ctx key   same, cache key ignored
```

`{{ return expr }}` works inside partials. The result is what the partial call evaluates to:

```
{{ $data := partial "getdata" . }}
{{ $data.url }}
```

Inline `{{ define "partials/name" }}` blocks inside a partial file are found when `partial "name"` is called from within that same file.

### Resources (stubs)

All resource functions return stubs that support method chaining so themes do not error. No actual asset processing happens.

```
resources.Get resources.GetMatch resources.GetRemote
resources.Match resources.ExecuteAsTemplate resources.Concat
toCSS postCSS minify fingerprint babel js.Build
```

Each stub resource has: Permalink, RelPermalink, Content, String, MediaType, Data, Name, Params, Width, Height, Publish.

Image processing methods (.Resize, .Fit, .Fill, .Crop, .Filter) return the same stub. Width and Height are 0.

### Other functions

```
readFile(path)         read file from data/ directory
getenv(key)            environment variable
md5(s)                 MD5 hex hash
sha256(s)              SHA-256 hex hash
base64Encode(s)
base64Decode(s)
jsonify(v)             JSON encode
unmarshal(s)           parse JSON, TOML, or YAML
findRE(pat, v)         find all regex matches
findREIndex(pat, v)    find with start/end positions
highlight(code, lang)  stub (returns plain code block)
i18n(key, args...)     translate string from i18n/ files
T(key, args...)        alias for i18n
hugo.IsServer          true when running tago serve
hugo.IsProduction      false
hugo.IsDevelopment     true when running tago serve
hugo.Generator         <meta> generator tag HTML
hugo.Version           version string
templates.Exists(name) check if template file exists
site                   global alias for .Site
page                   global alias for the current page
newScratch()           create an independent scratch pad
cond(test, a, b)       if test then a else b
ternary(a, b, test)    Hugo arg order: if test then a else b
errorf(fmt, args...)   log error and continue
warnf(fmt, args...)    log warning and continue
```

## preprocessTemplate

Before parsing any template file, tago rewrites two patterns.

**return without parens**

Hugo 0.117 allows `{{ return funcname args }}` in partials. Go templates require the argument to `return` to be a pipeline, which means it needs parentheses. tago rewrites:

```
{{ return upper .Title }}       ->  {{ return (upper .Title) }}
{{ return dict "a" "b" }}      ->  {{ return (dict "a" "b") }}
```

Already-parenthesized expressions are left alone.

**template "partials/X" to partial**

Some themes use the `template` directive to call partials:

```
{{ template "partials/pagination.html" . }}
```

tago rewrites this to:

```
{{ partial "pagination.html" . }}
```

## Stubs

These Hugo features are stubbed. They return something safe to prevent render errors, but do not have real implementations.

| Feature | Behavior |
|---|---|
| Hugo Pipes (toCSS, minify, fingerprint) | return same stub resource |
| Image processing (.Resize, .Fit, .Fill, .Crop) | return same stub, Width/Height = 0 |
| .HasShortcode | always false |
| .GetPage | returns nil |
| .Pages.Related | returns nil |
| Pagination | single page, no Prev/Next |
| .Site.Data | always empty map |
| Output formats | empty Permalink/MediaType/Rel |
| .GitInfo | nil |
| hugo.IsProduction | false |

## Verified compatible themes

All 370 officially registered Hugo themes from gohugoio/hugoThemes plus regression duplicates (623 builds total) pass with zero errors.

A partial list of well-known ones:

ananke, PaperMod, PaperModX, Blowfish, Congo, Docsy, Book, Stack, Relearn, Hextra, Toha, LoveIt, DoIt, Even, Meme, Terminal, Hello Friend, Hello Friend NG, Hermit, Coder, Anatole, Clarity, Mainroad, Archie, Bilberry, Blist, Casper, Clean White, Codex, Cactus, Etch, Jane, Ladder, M10c, Minimo, Minos, Paper, PaperCSS, Sam, Serif, Smol, Soho, Swift, Whisper, Whiteplain, Winston, Zen, Zzo, Universal, Bootstrap, Doks, Learn, Relearn, Geekdoc, Geekblog, Hugo Eureka, Hugo Geo, Hugo Profile, Hugo Blog Awesome, Hugo Ink, Hugo Mini, Gokarna, Harbor, Gruvbox, Kiss, Klise, Lithium, Liva, Northendlab, Novela, Paper, Parsa, Pickles, Portio, Simplest, Smol, Stack, Tale, Vanilla Bootstrap, Vitae, Wowchemy.
