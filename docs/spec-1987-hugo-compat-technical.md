# Spec 1987: Hugo Compatibility - Technical Details

Technical implementation notes for spec 1985. Updated as we find new
compatibility requirements by testing public Hugo themes.

## Type System

### anyToStr

All string-consuming functions use `anyToStr(v any) string` which handles:
- `nil` → `""`
- `string` → as-is
- `template.HTML` → unwrapped string
- `template.CSS` → unwrapped string
- `template.URL` → unwrapped string
- everything else → `fmt.Sprintf("%v", v)`

This prevents "invalid value; expected string" errors when themes pass nil
or template.HTML values to string functions.

### toInt / toFloat64

Math functions use `toInt(v any) (int, bool)` and `toFloat64(v any) float64`:
- Handles int, int8, int16, int32, int64
- Handles uint, uint8, uint16, uint32, uint64
- Handles float32, float64
- Handles string (strconv.Atoi / strconv.ParseFloat)
- Returns 0/false for nil or unparseable values

### hugoSlice

`type hugoSlice []any`

Used as return type of `slice` and `append` template functions. Provides
`.Reverse()`, `.First(n)`, `.Last(n)`, `.Len()` methods. Allows themes that
build page bundles via `slice` and then call `.Reverse`:

```
{{ $bundles := slice $currentPage }}
{{ $bundles = $bundles | append $currentBundles }}
{{ range $bundles.Reverse }}
```

### stubResourceSlice

`type stubResourceSlice []*stubResource`

All `Resources()` methods return `nil` of this type. Nil slice is:
- Falsy in `{{ with .Resources }}` — skips the block
- Safely rangeable in `{{ range .Resources }}` — zero iterations
- Method-callable: `.ByType("image")`, `.Match("*")`, `.GetMatch("cover.*")`

Previously `Resources()` returned `*pageResourcesStub` (a struct). A struct
pointer is always truthy even when conceptually empty, causing `{{ with .Resources }}`
blocks to enter and then fail when ranging over the struct.

### safePageRef

Returned by `.Parent`, `.FirstSection`. Wraps `*content.Page` with nil checks
on every method. When page is nil, all methods return zero values:

```go
func (r *safePageRef) RelPermalink() string {
    if r == nil || r.page == nil { return "" }
    return r.page.RelPermalink
}
```

Prevents panics in themes that access `.Parent.RelPermalink` without checking
if Parent exists (Hugo returns an empty page object, we return a nil-safe wrapper).

### parsedURL

Returned by `urls.Parse(rawurl)`. Fields: Host, Path, Scheme, RawQuery, Fragment.
All string types. Allows themes to access URL components:

```
{{ $u := urls.Parse .Permalink }}
{{ $u.Host }} {{ $u.Path }}
```

### safeDataMap

`type safeDataMap map[string]any`

Returned by `.Site.Data`. Empty map allows field access patterns like
`{{ .Site.Data.eureka.version }}` to return nil without error.

## Template Preprocessing

`preprocessTemplate(src string) string` is applied before parsing any template.

### Rule 1: return without parens

Regex: `(\{\{[-\s]*)return\s+([^(\s}][^}]*?)([-\s]*\}\})`

Rewrites:
- `{{ return funcname args }}` → `{{ return (funcname args) }}`

Hugo 0.117+ allows `{{ return expr }}` in partials. Go templates do not have
a `return` keyword. We implement `return` as a template function that stores
the value via goroutineID-keyed sync.Map and panics with a sentinel value that
the partial executor catches.

The rewrite is needed because Go templates parse `{{ return funcname args }}`
as calling the `return` function with a pipeline, but without the parentheses,
`funcname args` is not a valid pipeline. With parens it becomes `(funcname args)`,
which IS a valid pipeline passed to `return`.

Only rewrites when `expr` does NOT start with `(` to avoid double-wrapping.

### Rule 2: template "partials/X" → partial "X"

Regex: `\{\{-?\s*template\s+"partials/([^"]+)"\s*(.*?)\s*-?\}\}`

Rewrites:
- `{{ template "partials/pagination.html" . }}` → `{{ partial "pagination.html" . }}`

Applied to both baseof and kind templates (not just baseof). Some themes use
`template` directive for partials expecting Hugo to handle the lookup.

## Partial Execution

### renderPartialAny

Lookup order:
1. Check `r.partialCache[name]` — return cached template
2. Read from `r.layoutsDir/partials/name` or `r.layoutsDir/_partials/name`
3. If file not found, check `currentTmplStore` for goroutine's template set
4. If named template found in set (e.g. `partials/name`), execute it
5. If nothing found, return `template.HTML("")`

### Inline define support (currentTmplStore)

Some themes define private partials inline:

```html
{{- $result := partial "get-edit-url" . -}}
...
{{ define "partials/get-edit-url" }}
{{ return (dict "key" "val") }}
{{ end }}
```

When `post-edit.html` is parsed, Go's template engine registers
`"partials/get-edit-url"` as a named template in the same set. When
`partial "get-edit-url" .` is called during execution, our file lookup fails
(no such file), so we check `currentTmplStore[goroutineID()]` which holds
the currently executing partial's template set. We look for:
- `"partials/" + name` (no .html)
- `"partials/" + name + ".html"`

The executing template set is saved before execution and restored after:

```go
prev, hadPrev := currentTmplStore.Load(gid)
currentTmplStore.Store(gid, t)
execErr := t.Execute(&buf, ctx)
if hadPrev {
    currentTmplStore.Store(gid, prev)
} else {
    currentTmplStore.Delete(gid)
}
```

This handles nested partial calls correctly (restores parent set after child).

### partialReturn

`{{ return expr }}` in partials is implemented as:
1. `fm["return"]` calls `partialReturnStore.Store(goroutineID(), val)` and
   `panic(partialReturn{value: val})`
2. `renderPartialAny` catches the panic:
   ```go
   defer func() {
       if r := recover(); r != nil {
           if pr, ok := r.(partialReturn); ok { result = pr.value; return }
           panic(r) // re-panic unknown panics
       }
   }()
   ```
3. After execution: `partialReturnStore.LoadAndDelete(gid)` gets the value

Return value can be any type: string, map, slice, int. The calling template
assigns it to a variable and accesses fields/methods normally.

## replace() edge case

`replace(s, old, new)` with empty `old` would call `strings.ReplaceAll(s, "", new)`
which inserts `new` between every character. We guard:

```go
if oldStr == "" { return src }
```

This matches Hugo's behavior where `replace "hello" "" "X"` returns `"hello"`.

## Taxonomy rendering

Taxonomy pages (kind="taxonomy") and term pages (kind="term") use the default
taxonomy template if no theme template is found. The taxonomy list (`.Pages`)
contains all pages with that tag/category.

`SiteData.Taxonomies()` returns `map[string]any` where each value is
`map[string]TermEntry`. Built from all pages' Tags and Categories:

```go
func (s *SiteData) Taxonomies() map[string]any {
    // builds tagCounts from p.Tags, catCounts from p.Categories
    // returns {"tags": map[string]TermEntry{...}, "categories": map[string]TermEntry{...}}
}
```

TermEntry used in `range .Site.Taxonomies.tags`:
- `.Name` — term string (e.g. "golang")
- `.Count` — number of pages with this term
- `.Term` — alias for Name (some themes use .Term instead of .Name)
- `.Page()` — nil (Hugo returns a Page; we return nil — stub)
- `.Pages()` — nil HugoPageList (Hugo returns tagged pages; we return nil — stub)

Template usage pattern:
```
{{ range $name, $term := .Site.Taxonomies.tags }}
  {{ $term.Name }}: {{ $term.Count }}
{{ end }}
```

Also accessed via TermsData on taxonomy pages:
```
{{ range .Data.Terms.Alphabetical }}{{ .Name }}{{ end }}
{{ range .Data.Terms.ByCount }}{{ .Count }}{{ end }}
```

## Where function

`where(collection, field, [op], value)` supports:
- Operators: `=`, `==`, `eq`, `!=`, `ne`, `<`, `lt`, `<=`, `le`, `>`, `gt`, `>=`, `ge`
- Operators: `in`, `not in`, `intersect`
- Without operator: defaults to `=`
- Works on `[]*content.Page`, `HugoPageList`, `[]any`
- Uses `getNestedField` for dot-separated field paths (e.g. "Params.draft")

## Persistent Scratch

`HugoPage.Scratch()` and `TemplateData.Scratch()` cache the Scratch object on
first call using a private struct field. This means `.Scratch.Set "k" "v"` and
a subsequent `.Scratch.Get "k"` in the same template execution see the same map.
The same applies to `.Store()`.

Previously these methods called `newScratch()` on every invocation, so Set in
one expression and Get in another returned different objects — values were always
lost. Hugo themes rely on setting scratch values early in a template and reading
them later (e.g., setting a class name based on page position and referencing it
in a class attribute).

## HugoTime Named Layouts

`HugoTime.Format(layout)` resolves Hugo's named layout strings before passing to
`time.Time.Format`. The mapping:

| Named layout | Go format |
|---|---|
| `:date_long` | `January 2, 2006` |
| `:date_medium` | `Jan 2, 2006` |
| `:date_short` | `01/02/06` |
| `:time` | `15:04:05` |

Hugo 0.87+ introduced these named layouts matching CLDR long/medium/short date
formats. Themes use them in front matter: `dateformat: ":date_medium"`.

## seq Variadic Form

Hugo's `seq` accepts 1, 2, or 3 arguments:
- `seq N` → [1, 2, ..., N]
- `seq first last` → [first, first+1, ..., last]
- `seq first incr last` → [first, first+incr, ..., last]

Previous implementation only accepted `seq N` (one int argument). Updated to
variadic `func(...any) []int` using `toIntDef` helper.

## Scratch

`Scratch.Add(key, val)` accumulates:
- int + int = int
- float + float = float64
- string + string = string concatenation
- []T + v = append

`Scratch.GetSortedMapValues(key)` returns values from a nested map sorted by key.
Used by some themes for ordered navigation items.

## Known Stubs

These Hugo features are stubbed with no-op or minimal implementations:

- **Hugo Pipes asset processing**: toCSS/postCSS/minify/fingerprint return the
  same *stubResource, so chained operations work without error. Permalinks return "".
- **Image processing**: .Resize/.Fit/.Fill/.Crop return same stub. Width/Height = 0.
- **Shortcodes**: .HasShortcode always returns false.
- **GetPage**: .GetPage and .Site.GetPage return stub page; not real page lookup.
- **Related**: .Pages.Related returns nil.
- **Pagination**: full stub; all pages appear in single "page" of paginator.
- **i18n**: looks up i18n/ directory; unknown keys return the key.
- **Data files**: .Site.Data is always empty safeDataMap.
- **Output formats**: stub; Permalink/MediaType/Rel are empty strings.
- **Git info**: always nil.
- **hugo.IsProduction**: always false.

## Theme Testing Results

Themes are tested by running `tago build --theme <name>` on a minimal site
with a few content pages and checking for zero render errors.

| Date | Themes Tested | Passing | Notes |
|---|---|---|---|
| 2025-06 | 24 | 24 | Initial batch (ananke, PaperMod, etc.) |
| 2025-06 | 46 | 46 | Added 22 more (coder, eureka, LoveIt, etc.) |
| 2025-06 | 131 | 131 | Expanded with more public themes |
| 2025-06 | 183 | 183 | Further expansion |
| 2026-06 | 198 | 198 | Bootstrap, academic-cv, theme-blog, wowchemy |
| 2026-06 | 250 | 250 | docsy, learn, pickles, casper, soho, and many more |
| 2026-06 | 305 | 305 | hugoSlice, currentTmplStore, replace guard |
| 2026-06 | 623 | 623 | All 370 officially registered Hugo themes + duplicates |

623 themes pass. Themes are drawn from the official Hugo themes registry
(https://github.com/gohugoio/hugoThemes). 10 repositories were not found
(gone or private). The registry has 370 unique entries; the 623 test count
includes duplicates used for regression testing.

## Error Patterns Found in the Wild

### "range can't iterate over {}"

Cause: `Resources()` returned `*pageResourcesStub` (truthy struct), template
entered `{{ with .Resources }}`, then tried to `{{ range $x }}` — structs
are not rangeable.

Fix: Return `nil stubResourceSlice` which is falsy and rangeable.

### "can't evaluate field X in type template.HTML"

Cause: `partial "name"` returned `template.HTML("")` when file not found, but
template tried to access `$result.field`. Happens when the partial is defined
via inline `{{ define }}` in the calling file.

Fix: Goroutine-local template set tracking.

### "first called with interface{} count"

Cause: `.Scratch.Get "count"` returns `any`, but `first` expected `int`.

Fix: `first`/`last` accept `any` count, use toInt internally.

### "invalid value; expected string" for string functions

Cause: Template passes `nil` or `template.HTML` to string functions expecting
bare `string` type.

Fix: All string functions accept `any` via `anyToStr`.

### "path.Join with nil"

Cause: `path.Join "images" $img` where `$img` is nil from Scratch.

Fix: `path.Join` accepts `...any`.

### "can't evaluate field Reverse in type []interface{}"

Cause: `slice $page | append $more` builds `[]any` without methods.

Fix: Return `hugoSlice` from `slice` and `append`; it has `.Reverse()`.

### "template 'partials/X' not found" at parse time

Cause: Theme uses `{{ template "partials/pagination.html" . }}`.

Fix: preprocessTemplate rewrites these to `{{ partial "..." . }}`.

### "replace inserts new between every character"

Cause: `replace $s nil "world"` → old="" → `strings.ReplaceAll(s, "", "world")`.

Fix: Guard empty old string in replace.
