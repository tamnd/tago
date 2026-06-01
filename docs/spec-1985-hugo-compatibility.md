# Spec 1985: Hugo Compatibility

This document defines what tago must implement to render any public Hugo theme
without errors. Hugo is the reference implementation. When tago behavior differs
from Hugo, Hugo wins.

## Scope

This spec covers Go template execution compatibility only. It does not cover:
- Markdown rendering differences (handled by goldmark)
- Asset pipeline (PostCSS, SCSS, etc.) - stubs only
- i18n file loading - stubs only
- Module system - not planned

## Template Data

### Page object (.Page, .)

When a page template executes, `.` is a `TemplateData` struct. It must expose:

| Field/Method | Type | Notes |
|---|---|---|
| `.Title` | string | from front matter |
| `.Description` | string | from front matter |
| `.Summary` | string | auto-generated from content |
| `.Content` | template.HTML | rendered markdown |
| `.RawContent` | string | raw markdown source |
| `.Date` | HugoTime | parsed from front matter `date` |
| `.PublishDate` | HugoTime | alias for Date |
| `.Lastmod` | HugoTime | alias for Date |
| `.ExpiryDate` | HugoTime | zero value |
| `.Permalink` | string | absolute URL |
| `.RelPermalink` | string | site-relative URL |
| `.Section` | string | top-level section |
| `.Kind` | string | page, home, section, taxonomy, term |
| `.Type` | string | content type (section name or front matter) |
| `.Layout` | string | from front matter |
| `.IsPage` | bool | Kind == "page" |
| `.IsHome` | bool | Kind == "home" |
| `.IsSection` | bool | Kind == "section" |
| `.IsNode` | bool | !IsPage |
| `.Draft` | bool | from front matter |
| `.Weight` | int | from front matter |
| `.ReadingTime` | int | estimated reading time in minutes |
| `.WordCount` | int | word count of content |
| `.FuzzyWordCount` | int | rounded word count |
| `.Tags` | []string | from front matter |
| `.Categories` | []string | from front matter |
| `.Params` | map[string]any | all front matter |
| `.Param(key)` | any | front matter lookup with site fallback |
| `.Parent` | *safePageRef | parent page (nil-safe) |
| `.Site` | *SiteData | site-wide data |
| `.Pages` | HugoPageList | child pages for sections |
| `.RegularPages` | HugoPageList | non-section child pages |
| `.Sections` | HugoPageList | section children |
| `.AllPages` | HugoPageList | all site pages |
| `.Resources` | stubResourceSlice | page resources (empty) |
| `.Paginator(...)` | *paginatorStub | pagination (stub) |
| `.HasShortcode(name)` | bool | always false (stub) |
| `.GetPage(...)` | *TemplateData | nil (stub) |
| `.File` | fileInfoStub | file metadata |
| `.File.Path` | string | relative file path |
| `.File.Dir` | string | directory of file |
| `.File.Ext` | string | file extension |
| `.File.LogicalName` | string | filename |
| `.File.BaseFileName` | string | filename without extension |
| `.File.TranslationBaseName` | string | base name without lang |
| `.Scratch` | *Scratch | per-page scratch space |
| `.Store` | *Scratch | alias for Scratch |
| `.Keywords` | []string | alias for Tags |
| `.Next` | *TemplateData | nil (stub) |
| `.Prev` | *TemplateData | nil (stub) |
| `.NextInSection` | *TemplateData | nil (stub) |
| `.PrevInSection` | *TemplateData | nil (stub) |
| `.Ancestors` | HugoPageList | empty (stub) |
| `.BundleType` | string | empty |
| `.GitInfo` | any | nil |
| `.TableOfContents` | template.HTML | generated from headings |
| `.OutputFormats` | *outputFormatsStub | stub |
| `.AlternativeOutputFormats` | *outputFormatsStub | stub |
| `.Translations` | []any | empty |
| `.AllTranslations` | []any | empty |
| `.Language` | *SiteLanguage | from site config |
| `.TranslationKey` | string | empty |

### HugoPage (used in range over .Pages, .Site.Pages, etc.)

`HugoPage` wraps `*content.Page` and adds a `.Site` field. All methods of
`TemplateData` that make sense on a page also exist on `HugoPage`. Used when
ranging over page lists.

### safePageRef (returned by .Parent, .FirstSection)

All methods are nil-safe (return zero value when page is nil):
- RelPermalink, Permalink, Title, Kind, IsHome, IsSection
- Layout, Type, Params, Description

### SiteData (.Site)

| Field/Method | Type | Notes |
|---|---|---|
| `.Site.Title` | string | from config |
| `.Site.BaseURL` | string | from config |
| `.Site.Description` | string | from config |
| `.Site.Author` | map[string]any | from config |
| `.Site.Params` | map[string]any | from [params] config |
| `.Site.Pages` | HugoPageList | all pages |
| `.Site.RegularPages` | HugoPageList | non-section pages |
| `.Site.AllPages` | HugoPageList | alias for Pages |
| `.Site.Taxonomies` | TaxonomyList | taxonomy data |
| `.Site.Menus` | map[string]MenuList | menus from config |
| `.Site.Data` | safeDataMap | from data/ dir (stub: empty) |
| `.Site.Language` | *SiteLanguage | language config |
| `.Site.Languages` | []*SiteLanguage | all languages |
| `.Site.LanguagePrefix` | string | empty (stub) |
| `.Site.IsMultiLingual` | bool | false |
| `.Site.Home` | *siteHomeStub | home page stub |
| `.Site.GetPage(args...)` | *siteHomeStub | page by path (stub) |
| `.Site.GoogleAnalytics` | string | from params |
| `.Site.BuildDrafts` | bool | false |
| `.Site.RSSLink` | string | /index.xml |

### siteHomeStub (.Site.Home, .Site.GetPage)

Methods: RelPermalink, Permalink, Title, LinkTitle, Name, Description,
Summary, Content, IsHome, IsPage, IsSection, Kind, Type, Params, Tags,
Date, Lastmod, Section, Layout, Draft, File, Scratch, Paginator,
Resources, GetPage, GitInfo, HasShortcode, Page.

`Page()` returns self (for `.Page.Title` access patterns).

### HugoPageList ([]*HugoPage with methods)

| Method | Signature | Notes |
|---|---|---|
| `.ByWeight()` | HugoPageList | sort by Weight asc, then Title |
| `.ByTitle()` | HugoPageList | sort alphabetically |
| `.ByDate()` | HugoPageList | sort by Date asc |
| `.ByPublishDate()` | HugoPageList | sort by Date asc |
| `.ByLastmod()` | HugoPageList | sort by Date asc |
| `.ByLength()` | HugoPageList | sort by word count |
| `.ByParam(key)` | HugoPageList | sort by .Params[key] |
| `.Reverse()` | HugoPageList | reverse slice |
| `.First(n)` | HugoPageList | first n pages |
| `.Last(n)` | HugoPageList | last n pages |
| `.Limit(n)` | HugoPageList | alias for First |
| `.Where(field, op, val)` or `Where(field, val)` | HugoPageList | filter |
| `.Related(page)` | HugoPageList | nil (stub) |
| `.GroupByDate(layout)` | []HugoPageGroup | group by date format |
| `.GroupByPublishDate(layout)` | []HugoPageGroup | alias |
| `.GroupByLastmod(layout)` | []HugoPageGroup | alias |
| `.GroupByParam(key)` | []HugoPageGroup | nil (stub) |
| `.Filter(fn)` | HugoPageList | pass-through stub |

### hugoSlice (return type of slice/append functions)

A generic `[]any` slice with methods:
- `Reverse() hugoSlice`
- `First(n int) hugoSlice`
- `Last(n int) hugoSlice`
- `Len() int`

Used by themes that build page bundles via `slice` and then call `.Reverse`.

### Scratch (.Scratch, .Store)

| Method | Notes |
|---|---|
| `.Set(key, val)` | store any value |
| `.Get(key)` | retrieve value (returns any/nil) |
| `.Add(key, val)` | add to existing value (int/float/string/slice) |
| `.SetInMap(key, mapKey, val)` | store in nested map |
| `.GetSortedMapValues(key)` | sorted values of nested map |
| `.Delete(key)` | remove key |

### HugoTime

Wraps `time.Time`. Methods:
- `Format(layout any) string` — accepts Go layout or Hugo named layouts
  (:date_long, :date_medium, :date_short, :time_long, :time_medium, :time_short)
- `Unix() int64`
- `IsZero() bool`
- `Before(other HugoTime) bool`, `After(other HugoTime) bool`
- `AddDate(years, months, days int) HugoTime`
- Standard time.Time field access via embedding

Named Hugo layouts:
- `:date_long` → "January 2, 2006"
- `:date_medium` → "Jan 2, 2006"
- `:date_short` → "1/2/06"
- `:time_long` → "3:04:05 PM MST"
- `:time_medium` → "3:04:05 PM"
- `:time_short` → "3:04 PM"

### TermEntry (used in range over .Site.Taxonomies)

Fields: Name string, Count int, Term string (alias for Name)
Methods: `Page() *content.Page`, `Pages() HugoPageList`

## Template Functions

### String functions

| Function | Signature | Notes |
|---|---|---|
| `lower` | `(any) string` | toLower |
| `upper` | `(any) string` | toUpper |
| `title` | `(any) string` | title case |
| `trim` | `(s, cutset any) string` | trim both sides |
| `trimPrefix` | `(prefix, s any) string` | trim prefix |
| `trimSuffix` | `(suffix, s any) string` | trim suffix |
| `replace` | `(s, old, new any) string` | empty old = no-op |
| `replaceRE` | `(pattern, repl, s any, n ...any) any` | regex replace |
| `split` | `(s, sep string) []string` | split string |
| `substr` | `(s string, start, end int) string` | substring |
| `slicestr` | `(s, start any, end ...any) string` | Unicode-aware slice |
| `printf` | `(format any, args ...any) string` | nil format = "" |
| `sprintf` | `(format string, args ...any) string` | standard |
| `print` | variadic | concat |
| `println` | variadic | concat with newline |
| `truncate` | `(maxLen int, s any) string` | word-boundary truncation |
| `markdownify` | `(s any) template.HTML` | render markdown |
| `humanize` | `(s any) string` | humanize string |
| `pluralize` | `(s any) string` | pluralize |
| `singularize` | `(s any) string` | singularize |
| `urlize` | `(s any) string` | URL-safe slug |
| `anchorize` | `(s any) string` | anchor slug |
| `emojify` | `(s any) template.HTML` | no-op stub |
| `plainify` | `(s any) string` | strip HTML tags |
| `htmlEscape` | `(s any) string` | HTML escape |
| `htmlUnescape` | `(s any) string` | HTML unescape |
| `safeHTML` | `(s any) template.HTML` | mark as safe HTML |
| `safeCSS` | `(s any) template.CSS` | mark as safe CSS |
| `safeURL` | `(s any) template.URL` | mark as safe URL |
| `safeJS` | `(s any) template.JS` | mark as safe JS |
| `safeHTMLAttr` | `(s any) template.HTMLAttr` | mark as safe attr |

### strings namespace

`{{ strings.HasPrefix }}`, `{{ strings.HasSuffix }}`, `{{ strings.Contains }}`,
`{{ strings.Count }}`, `{{ strings.Repeat }}`, `{{ strings.Replace }}`,
`{{ strings.ReplaceAll }}`, `{{ strings.TrimPrefix }}`, `{{ strings.TrimSuffix }}`,
`{{ strings.TrimLeft }}`, `{{ strings.TrimRight }}`, `{{ strings.ToLower }}`,
`{{ strings.ToUpper }}`, `{{ strings.Title }}`, `{{ strings.Trim }}`,
`{{ strings.RuneCount }}`, `{{ strings.Substr }}`

All accept `any` (converted via anyToStr). Hugo arg order for TrimLeft/TrimRight:
cutset first, string second.

### Math functions

| Function | Notes |
|---|---|
| `add` | accepts any, returns int or float64 |
| `sub` | same |
| `mul` | same |
| `div` | integer div when both int; float otherwise; div by zero = 0 |
| `mod` | integer modulo |
| `modBool` | mod == 0 |
| `math.Abs`, `math.Ceil`, `math.Floor`, `math.Round`, `math.Log`, `math.Log2`, `math.Sqrt`, `math.Max`, `math.Min`, `math.Pow`, `math.Sum` | standard |
| `math.Counter` | namespace method |

### Collection functions

| Function | Signature | Notes |
|---|---|---|
| `where` | `(collection, field, [op,] value) any` | filter with op: ==, !=, <, <=, >, >=, in, not in, intersect |
| `first` | `(n any, collection any) any` | n accepts any (toInt) |
| `last` | `(n any, collection any) any` | same |
| `after` | `(n int, collection any) any` | skip first n |
| `index` | `(collection any, keys ...any) any` | nested access |
| `slice` | `(items ...any) hugoSlice` | build slice with methods |
| `append` | `(v any, args ...any) hugoSlice` | append to slice |
| `union` | `(a, b []string) []string` | |
| `intersect` | `(a, b []string) []string` | |
| `complement` | variadic | |
| `symdiff` | `(a, b any) any` | symmetric difference |
| `uniq` | `(a any) any` | deduplicate |
| `flatten` | variadic | |
| `sort` | `(collection any, args ...any) any` | |
| `shuffle` | `(collection any) any` | deterministic (seeded by position) |
| `in` | `(set, val any) bool` | membership |
| `len` | `(v any) int` | length |
| `seq` | `(args ...any) []int` | integer sequence |
| `dict` | `(args ...any) map[string]any` | build map from k,v pairs |
| `merge` | `(args ...any) map[string]any` | merge maps |
| `keys` | `(m any) []string` | map keys |
| `values` | `(m any) []any` | map values |

### Type conversion

| Function | Notes |
|---|---|
| `int` | convert to int |
| `float` | convert to float64 |
| `string` | convert to string |
| `bool` | convert to bool |
| `default` | `(defaultVal, val any) any` — return val if truthy, else default |
| `isset` | `(obj any, key string) bool` |
| `reflect.IsSlice` | nil-safe |
| `reflect.IsMap` | |
| `reflect.IsArray` | |
| `reflect.IsFloat` | |
| `reflect.IsInt` | |
| `reflect.IsBool` | |
| `reflect.IsInvalid` | |
| `reflect.TypeOf` | |
| `reflect.KindOf` | |

### URL functions

| Function | Notes |
|---|---|
| `absURL` | prepend baseURL |
| `relURL` | make site-relative |
| `urlize` | slug-safe string |
| `urls.Parse(url)` | returns parsedURL with Host/Path/Scheme/RawQuery/Fragment |

### Path functions

| Function | Notes |
|---|---|
| `path.Join(elems ...any) string` | accepts any |
| `path.Base(s string) string` | base name |
| `path.Dir(s string) string` | directory |
| `path.Ext(s string) string` | extension |
| `path.IsAbs(s string) bool` | |

### Date/time functions

| Function | Notes |
|---|---|
| `now` | returns HugoTime for current time |
| `time` | `(v any) HugoTime` — parse or convert |
| `time.Now` | returns HugoTime |
| `time.Format(layout any, t any) string` | HugoTime or time.Time |
| `dateFormat` | `(layout, v any) string` — alias |
| `formatDate` | `(v any) string` — fallback format |
| `duration` | `(unit, n any) time.Duration` |

### Partials and blocks

| Function | Notes |
|---|---|
| `partial(name, ctx)` | render partial; look in partials/ and _partials/ |
| `partialCached(name, ctx, keys...)` | same; cache ignored |
| `block` | native Go template block |
| `template` | native Go template call |
| `define` | native Go template define |

Inline `{{ define "partials/name" }}` blocks within a partial file are
registered in the template set and found when `partial "name"` is called from
within the same partial execution (via goroutine-local template set tracking).

### Resource functions (stubs)

All resource functions return stubs that allow method chaining:
- `resources.Get(path)`, `resources.GetMatch(pattern)`, `resources.GetRemote(url)`
- `resources.Match(pattern)` → `[]*stubResource`
- `toCSS`, `postCSS`, `minify`, `fingerprint`, `babel`, `js.Build`
- `resources.ExecuteAsTemplate`, `resources.Concat`

`stubResource` methods: Permalink, RelPermalink, Content, String, MediaType,
Data, Name, Params, ToCSS, Minify, Fingerprint, ExecuteAsTemplate, PostProcess,
Resize, Fit, Fill, Crop, Filter, Width, Height, Publish.

`stubResourceSlice` methods: GetMatch, Get, GetRemote, ByType, Match, Len.

### Conditional functions

| Function | Notes |
|---|---|
| `cond(test, a, b)` | if test then a else b |
| `ternary(a, b, test)` | Hugo reversed: if test then a else b |
| `and`, `or`, `not` | logical |
| `eq`, `ne`, `lt`, `le`, `gt`, `ge` | comparison |
| `errorf(format, args...)` | log error, continue |
| `warnf(format, args...)` | log warning, continue |

### Other functions

| Function | Notes |
|---|---|
| `readFile(path)` | read from data/ directory |
| `getenv(key)` | get environment variable |
| `md5(s)` | MD5 hash |
| `sha256(s)` | SHA256 hash |
| `base64Encode(s)` | base64 encode |
| `base64Decode(s)` | base64 decode |
| `jsonify(v)` | JSON encode |
| `unmarshal(s)` | parse JSON/TOML/YAML |
| `findRE(pattern, v)` | find all regex matches |
| `findREIndex(pattern, v)` | find with index |
| `highlight(code, lang, opts)` | stub (returns plain code) |
| `i18n(key, args...)` | lookup i18n string |
| `T(key, args...)` | alias for i18n |
| `hugo.IsServer` | bool from tago server mode |
| `hugo.IsProduction` | false |
| `hugo.IsDevelopment` | true when serving |
| `hugo.Generator` | generator meta tag HTML |
| `hugo.Version` | version string |
| `hugo.Data` | empty safeDataMap |
| `templates.Exists(name)` | check if template file exists |
| `os.Stat(path)` | file stat stub |
| `site` | global alias for .Site |
| `page` | global alias for current page |

## preprocessTemplate Rewrites

Before parsing any template, tago rewrites:

1. `{{ return funcname args }}` → `{{ return (funcname args) }}`
   Matches `{{ return X }}` where X does NOT start with `(`.

2. `{{ template "partials/X" ctx }}` → `{{ partial "X" ctx }}`
   Allows themes that use the `template` directive for partials.

## Taxonomy System

`.Site.Taxonomies` returns `TaxonomyList` (a map of taxonomy name to term map).
Each term is a `TermData` with entries `[]TermEntry`.
`TermEntry.Page()` returns nil, `TermEntry.Pages()` returns nil.

Taxonomy page `.Pages` returns all pages tagged with the term.

## Pagination (stub)

`.Paginator()` and `.Paginator(n)` return a `paginatorStub` with:
- Pages HugoPageList, PageNumber int, TotalPages int, TotalNumberOfElements int
- HasPrev, HasNext bool; Prev, Next *paginatorStub
- Pagers []*paginatorStub; PageGroups []pageGroup; First, Last *paginatorStub

## Menu System

`.Site.Menus["main"]` returns a `MenuList` (slice of `*MenuEntry`).
`MenuEntry` has: Name, URL, Weight, Identifier, Title, Pre, Post, HasChildren,
Children (MenuList), Page (*content.Page), IsMenuCurrent, HasMenuCurrent.

## Language/i18n

`.Site.Language` returns `*SiteLanguage` with: Lang, LanguageName, Locale,
ContentDir, Weight, Params, Get(key).

i18n files from the site's `i18n/` directory are loaded. Unknown keys return
the key itself.

## File Info (.File)

`.File.Path` — relative to content dir
`.File.Dir` — directory portion
`.File.Ext` — extension (with dot)
`.File.LogicalName` — filename
`.File.BaseFileName` — filename without extension
`.File.TranslationBaseName` — base name without language code
`.File.UniqueID` — MD5 of path

## Output Formats

`.OutputFormats` and `.AlternativeOutputFormats` return stubs.
Individual format: `.Rel` string, `.MediaType.Type` string, `.Permalink` string.

## Error Handling

Template errors are logged and the page is skipped (other pages still render).
`errorf` and `warnf` log the message and continue execution.
Missing partials return empty string (no error).
Missing templates cause the page to be skipped with an error log.

## Verified Compatible Themes

The following public Hugo themes have been tested and render with zero errors.
Tested by running `tago build --theme <name>` on a minimal site.
Last verified: 2026-06-01 (305 builds, 305 passing).

| Theme | Repository |
|---|---|
| academia | gethugothemes/academia-hugo |
| academic-cv | HugoBlox/theme-academic-cv |
| academic-starter | wowchemy/starter-hugo-academic |
| agency | digitalcraftsman/hugo-agency-theme |
| anatole | lxndrblz/anatole |
| ananke | theNewDynamic/gohugo-theme-ananke |
| anubis | Junyi-99/hugo-theme-anubis2, Mitrichius/hugo-theme-anubis |
| apero | hugo-apero/hugo-apero |
| archie | athul/archie |
| beautifulhugo | halogenica/beautifulhugo |
| bilberry | Lednerb/bilberry-hugo-theme |
| blackburn | yoshiharuyamashita/blackburn |
| blist | apvarun/blist-hugo-theme |
| blowfish | nunocoracao/blowfish |
| blox | HugoBlox/hugo-blox-builder |
| book | alex-shpak/hugo-book |
| bookworm | gethugothemes/bookworm |
| bootstrap | filipecarneiro/hugo-bootstrap-theme |
| cactus | monkeyWzr/hugo-theme-cactus |
| calligraphy | pacollins/calligraphy |
| casper | vjeantet/hugo-theme-casper |
| chunky-poster | puresyntax71/hugo-theme-chunky-poster |
| clarity | chipzoller/hugo-clarity |
| cleanwhite | zhaohuabing/hugo-theme-cleanwhite |
| codex | jakewies/hugo-theme-codex |
| compose | onweru/compose |
| congo | jpanther/congo |
| contrast | niklasbuschmann/contrast-hugo |
| creative | digitalcraftsman/hugo-creative-theme |
| cupper | zwbetz-gh/cupper-hugo-theme |
| digital-garden | apvarun/digital-garden-hugo-theme |
| docsy | google/docsy |
| doit | HEIGE-PCloud/DoIt |
| doks | h-enk/doks |
| etch | LukasJoswiak/etch |
| even | olOwOlo/hugo-theme-even |
| geeky | statichunt/geeky-hugo |
| ghostwriter | jbub/ghostwriter |
| gokarna | 526avijitgupta/gokarna |
| gruvbox | schnerring/hugo-theme-gruvbox |
| harbor | matsuyoshi30/harbor |
| hello-friend | panr/hugo-theme-hello-friend |
| hello-friend-ng | rhazdon/hugo-theme-hello-friend-ng |
| hermit | Track3/hermit |
| hextra | imfing/hextra |
| heyo | LucasVadilho/heyo-hugo-theme |
| holy | serkodev/holy |
| hugo-blog-awesome | hugo-sid/hugo-blog-awesome |
| hugo-coder | luizdepra/hugo-coder |
| hugo-docker | hugomods/docker |
| hugo-eureka | wangchucheng/hugo-eureka |
| hugo-geo | alexurquhart/hugo-geo |
| hugo-hero | zerostaticthemes/hugo-hero-theme |
| hugo-ink | knadh/hugo-ink |
| hugo-mini | nodejh/hugo-theme-mini |
| hugo-PaperMod | adityatelange/hugo-PaperMod |
| hugo-PaperModX | reorx/hugo-PaperModX |
| hugo-profile | gurusabarish/hugo-profile |
| hugo-pwa | davidsneighbour/hugo-pwa |
| hugo-xmin | yihui/hugo-xmin |
| hugomods-base | hugomods/base |
| hugomods-bs | hugomods/bootstrap |
| hulga | wlh320/hugo-theme-hulga |
| hyde | spf13/hyde |
| icarus | digitalcraftsman/hugo-icarus-theme |
| ink-free | chollinger93/ink-free |
| introduction | victoriadrake/hugo-theme-introduction |
| jane | xianmin/hugo-theme-jane |
| kiss | ribice/kiss |
| klise | piharpi/jekyll-klise |
| ladder | guangzhengli/hugo-theme-ladder |
| learn | matcornic/hugo-theme-learn |
| lithium | yihui/hugo-lithium |
| liva | gethugothemes/liva-hugo |
| LoveIt | dillonzq/LoveIt |
| m10c | vaga/hugo-theme-m10c |
| mainroad | Vimux/Mainroad |
| meme | reuixiy/hugo-theme-meme |
| mini | nodejh/hugo-theme-mini |
| minima | mivinci/hugo-theme-minima |
| minimal | calintat/minimal |
| minimo | MunifTanjim/minimo |
| minos | carsonip/hugo-theme-minos |
| newsroom | onweru/newsroom |
| northendlab | gethugothemes/northendlab-hugo |
| novela | forestryio/novela-hugo-starter |
| online-course | wowchemy/starter-hugo-online-course |
| paper | nanxiaobei/hugo-paper |
| papercss | zwbetz-gh/papercss-hugo-theme |
| parsa | gethugothemes/parsa-hugo |
| pickles | mismith0227/hugo_theme_pickles |
| portio | StaticMania/portio-hugo |
| relearn | McShelby/hugo-theme-relearn |
| research-group | wowchemy/starter-hugo-research-group |
| sam | victoriadrake/hugo-theme-sam |
| serif | zerostaticthemes/hugo-serif-theme |
| simplest | nandomoreirame/simplest |
| simplicity | marcanuy/simplicity |
| smol | colorchestra/smol |
| soho | alexandrevicenzi/soho |
| stack | CaiJimmy/hugo-theme-stack |
| strata | digitalcraftsman/hugo-strata-theme |
| swift | onweru/hugo-swift-theme |
| tale | EmielH/tale-hugo |
| terminal | panr/hugo-theme-terminal |
| theme-blog | HugoBlox/theme-blog |
| theme-docs | HugoBlox/theme-documentation |
| theme-landing | HugoBlox/theme-landing-page |
| theme-portfolio | HugoBlox/theme-portfolio |
| theme-rg | HugoBlox/theme-research-group |
| toha | hugo-toha/toha |
| universal | devcows/hugo-universal-theme |
| vanilla-bootstrap | zwbetz-gh/vanilla-bootstrap-hugo-theme |
| vitae | dataCobra/hugo-vitae |
| whisper | zerostaticthemes/hugo-whisper-theme |
| whiteplain | taikii/whiteplain |
| winston | zerostaticthemes/hugo-winston-theme |
| wowchemy | wowchemy/wowchemy-hugo-themes |
| wowchemy-blog | wowchemy/starter-hugo-blog |
| zen | frjo/hugo-theme-zen |
| zzo | zzossig/hugo-theme-zzo |
