# Hugo Compatibility Internals

Implementation notes for the Hugo compatibility layer in `pkg/render/render.go`. This is written for contributors who need to understand why things work the way they do, or who are debugging a theme that does not render correctly.

For the public API (what fields and functions themes can use), see [hugo-compatibility.md](hugo-compatibility.md).

## Type conversions

### anyToStr

Every string-consuming function in the function map uses `anyToStr(v any) string`:

```go
nil            -> ""
string         -> as-is
template.HTML  -> unwrap to string
template.CSS   -> unwrap to string
template.URL   -> unwrap to string
everything else -> fmt.Sprintf("%v", v)
```

Without this, Hugo themes break in two common ways. First, themes often pass `template.HTML` values into string functions like `replace` or `hasPrefix`. Go's type system would reject that. Second, front matter values that came back as `nil` (field not set) would panic when passed to a function expecting `string`.

### toInt / toFloat64

Math and collection functions use `toInt(v any) (int, bool)` and `toFloat64(v any) float64`. They handle all integer and float variants, plus string parsing via `strconv`. Nil and unparseable values return 0.

This matters because `.Scratch.Get "count"` returns `any`. If a theme does `{{ first (.Scratch.Get "count") .Pages }}` and `first` only accepted `int`, it would fail. Accepting `any` and converting internally avoids this.

## hugoSlice

```go
type hugoSlice []any
```

`slice` and `append` return `hugoSlice` instead of plain `[]any`. The difference is that `hugoSlice` has `.Reverse()`, `.First(n)`, `.Last(n)`, and `.Len()` methods.

Themes do this pattern frequently:

```
{{ $bundles := slice $currentPage }}
{{ $bundles = $bundles | append $moreBundles }}
{{ range $bundles.Reverse }}
```

Before `hugoSlice` existed, `slice` returned `[]interface{}` and the `.Reverse` call would fail with "can't evaluate field Reverse in type []interface{}".

## stubResourceSlice

```go
type stubResourceSlice []*stubResource
```

All `Resources()` methods return `nil` of this type, not a struct pointer.

The reason: a nil slice is falsy in `{{ with .Resources }}` and safely rangeable in `{{ range .Resources }}` (zero iterations). A struct pointer is always truthy, even if the struct is conceptually empty. Before this fix, themes would enter `{{ with .Resources }}` and then try to range over the struct, which panics.

```
{{ with .Resources }}      // enters block only if Resources is non-nil slice
  {{ range . }}            // iterates zero times, no panic
  {{ end }}
{{ end }}
```

The methods on `stubResourceSlice` (GetMatch, Get, ByType, Match, Len) allow themes to chain method calls on a nil slice without panicking because Go method calls on nil slice types work fine.

## safePageRef

`.Parent` and `.FirstSection` return a `*safePageRef` instead of `*content.Page`. Every method has a double nil check:

```go
func (r *safePageRef) RelPermalink() string {
    if r == nil || r.page == nil { return "" }
    return r.page.RelPermalink
}
```

Hugo returns an empty page object when the parent does not exist. tago returns a nil-safe wrapper that produces the same behavior: any field access returns a zero value instead of panicking.

## parsedURL

`urls.Parse(rawurl)` returns a `parsedURL` struct with string fields: Host, Path, Scheme, RawQuery, Fragment. Themes that decompose URLs do:

```
{{ $u := urls.Parse .Permalink }}
<link rel="alternate" href="{{ $u.Scheme }}://{{ $u.Host }}{{ $u.Path }}">
```

## safeDataMap

`.Site.Data` returns `type safeDataMap map[string]any`. The type alias lets us attach methods if needed, and the empty map means chained field access like `.Site.Data.eureka.version` returns nil instead of panicking.

## Persistent Scratch

`HugoPage.Scratch()` and `TemplateData.Scratch()` used to call `newScratch()` on every invocation. That meant `.Scratch.Set "k" "v"` and `.Scratch.Get "k"` returned different objects, so the stored value was always lost.

Now each struct holds a `scratch *Scratch` field that is initialized on first access:

```go
func (d *TemplateData) Scratch() *Scratch {
    if d.scratch == nil {
        d.scratch = newScratch()
    }
    return d.scratch
}
```

Same pattern for `Store()` and for `HugoPage`.

## Template preprocessing

`preprocessTemplate(src string) string` runs on every template file before parsing.

**Rule 1: return without parens**

Hugo 0.117+ allows partials to return values with `{{ return expr }}`. Go templates implement `return` as a function call. The issue is that `{{ return funcname args }}` is parsed as calling `return` with one argument (`funcname`) and then `args` as a separate pipeline step, which is not what we want.

Wrapping in parens fixes it: `{{ return (funcname args) }}`.

The preprocessor uses a regex to detect `{{ return X }}` where X does not start with `(`, and adds the parens.

```
{{ return upper .Title }}          ->  {{ return (upper .Title) }}
{{ return dict "url" $url }}       ->  {{ return (dict "url" $url) }}
{{ return (dict "url" $url) }}     ->  unchanged (already parenthesized)
```

**Rule 2: template "partials/X" to partial**

Some themes call partials using the template directive:

```
{{ template "partials/pagination.html" . }}
```

This is not how tago's partial system works. The preprocessor rewrites it:

```
{{ partial "pagination.html" . }}
```

The regex handles whitespace trimming markers (`{{-` and `-}}`) correctly.

## Partial execution

`renderPartialAny(name, ctx)` looks up and executes a partial:

1. Check in-memory cache -- return cached template if found.
2. Look for `layoutsDir/partials/name` and `layoutsDir/partials/name.html`.
3. Look for `layoutsDir/_partials/name` (fallback directory).
4. If the file is not found, check `currentTmplStore` for the goroutine's current template set (see inline define support below).
5. If nothing is found, return `template.HTML("")` (empty string, not an error).

Before executing the partial, the template set is stored in a goroutine-local map and restored after execution. This is how nested partials work correctly.

## Inline define support

Some themes define private partials inside another partial file using `{{ define }}`:

```html
{{- $result := partial "get-edit-url" . -}}
...
{{ define "partials/get-edit-url" }}
  {{ return (dict "repoEditURL" $url) }}
{{ end }}
```

When `post-edit.html` is parsed, Go's template engine registers `"partials/get-edit-url"` as a named template in that file's template set. But when `partial "get-edit-url"` is called, the file lookup fails (there is no `get-edit-url.html` on disk). Before this was fixed, tago returned an empty string, and the next line that tried to access `$result.repoEditURL` would panic.

The fix uses a goroutine-local map (`currentTmplStore`) that stores a pointer to the currently executing template set. When file lookup fails, tago checks if the goroutine's current template set has a named template called `"partials/get-edit-url"` or `"partials/get-edit-url.html"` and executes it directly.

```go
// Before executing each partial:
prev, hadPrev := currentTmplStore.Load(gid)
currentTmplStore.Store(gid, t)
execErr := t.Execute(&buf, ctx)
// Restore after execution:
if hadPrev {
    currentTmplStore.Store(gid, prev)
} else {
    currentTmplStore.Delete(gid)
}
```

Nested partials work correctly because each level saves and restores the previous value.

## {{ return }} in partials

`{{ return expr }}` is implemented using a panic/recover pair with a goroutine-keyed sync.Map.

When the `return` function is called in a template:

1. It stores the value in `partialReturnStore` keyed by goroutine ID.
2. It panics with a `partialReturn` sentinel struct.

In `renderPartialAny`, the execute call is wrapped in a deferred recover:

```go
defer func() {
    if r := recover(); r != nil {
        if pr, ok := r.(partialReturn); ok {
            result = pr.value
            return
        }
        panic(r)  // re-panic if it's a real error
    }
}()
```

The return value can be any type: string, map, slice, int. The calling template receives it as `any` and accesses fields via normal template field access.

## replace with empty old string

`strings.ReplaceAll(s, "", new)` inserts `new` between every character in `s`. This is standard Go behavior but not what Hugo themes expect when `old` is nil or empty (it comes from a Scratch or Params value that was never set).

The fix is a guard in the `replace` function:

```go
if oldStr == "" { return src }
```

This matches Hugo's behavior.

## Taxonomies

`SiteData.Taxonomies()` builds a `map[string]any` from the site's pages. For each page, it iterates Tags and Categories and counts occurrences. The result maps taxonomy name ("tags", "categories") to a map of term name to TermEntry.

```go
type TermEntry struct {
    Name  string
    Count int
    Term  string   // alias for Name
}
```

Templates use it like this:

```
{{ range $name, $term := .Site.Taxonomies.tags }}
  {{ $term.Name }}: {{ $term.Count }}
{{ end }}
```

`TermEntry.Pages()` returns nil. `TermEntry.Page()` returns nil. Hugo returns real page lists and page objects here; if a theme uses them for anything beyond rendering the term name and count, it will get nothing.

Taxonomy pages also have `.Data.Terms.Alphabetical` and `.Data.Terms.ByCount` which return sorted TermEntry slices.

## where function

`where(collection, field, [op,] value)` supports these operators:

```
=  ==  eq      equality
!= ne          inequality
<  lt          less than
<= le          less than or equal
>  gt          greater than
>= ge          greater than or equal
in             value is in a slice
not in         value is not in slice
intersect      slices have at least one common element
```

Without an operator argument, defaults to `==`.

Field can be a dot-separated path for nested access: `"Params.featured"` calls `getNestedField` which traverses the struct or map.

Works on `[]*content.Page`, `HugoPageList`, and `[]any`.

## Scratch.Add accumulation

`.Add(key, val)` follows these rules:

```
int + int         ->  int
float + float     ->  float64
string + string   ->  string concatenation
slice + element   ->  append
```

If the key does not exist yet, the value is stored as-is (same as Set).

## seq

`seq` accepts 1, 2, or 3 arguments:

```
seq N              ->  [1, 2, ..., N]
seq first last     ->  [first, first+1, ..., last]
seq first incr last ->  [first, first+incr, ..., last]
```

The return type is `[]int`. Increment can be negative for descending sequences. Increment of 0 returns nil to avoid an infinite loop.

## HugoTime named layouts

Hugo 0.87 added named date layout strings matching CLDR patterns. `HugoTime.Format` resolves these before passing to `time.Time.Format`:

```
:date_long    ->  January 2, 2006
:date_medium  ->  Jan 2, 2006
:date_short   ->  01/02/06
:time         ->  15:04:05
```

Without this, themes that set `dateformat: ":date_medium"` in their config would get the literal string ":date_medium" as their date output.

## goroutineID

`currentTmplStore` and `partialReturnStore` are keyed by goroutine ID. tago renders pages concurrently (one goroutine per page), so the key must be goroutine-specific, not a global.

The ID is extracted by reading the goroutine header from the stack with `runtime.Stack`. This is a well-known Go trick that works reliably even though it is not part of the public API. The alternative would be passing a context through every function call, which would require changing the template function signatures.

## Error patterns from the wild

These are the most common failures found when testing Hugo themes:

**"range can't iterate over {}"**

A struct pointer is always truthy. Before the fix, `Resources()` returned `*pageResourcesStub`. Themes would enter `{{ with .Resources }}` and then try to `{{ range . }}` on the struct, which panics. Fix: return `nil stubResourceSlice`.

**"can't evaluate field X in type template.HTML"**

`partial "name"` returned `template.HTML("")` when file not found. If the theme expected to access `$result.field`, it panicked because `template.HTML` has no `.field`. Cause: the partial was defined via inline `{{ define }}` in the calling file. Fix: goroutine-local template set tracking.

**"first called with interface{} count"**

`.Scratch.Get "count"` returns `any`. If `first` only accepted `int`, it would fail. Fix: `first` and `last` accept `any` and call `toInt` internally.

**"invalid value; expected string" for string functions**

The template passed `nil` or `template.HTML` to a function that expected `string`. Fix: all string functions use `anyToStr`.

**"path.Join with nil"**

`path.Join "images" $img` where `$img` is a nil Scratch value. Fix: `path.Join` accepts `...any`.

**"can't evaluate field Reverse in type []interface{}"**

`slice $a | append $b` built a plain `[]any`. Fix: `slice` and `append` return `hugoSlice` which has `.Reverse()`.

**"replace inserts new between every character"**

`replace $s nil "X"` called `strings.ReplaceAll(s, "", "X")`. Fix: guard empty old string.

**"template 'partials/X' not found" at parse time**

The theme used `{{ template "partials/pagination.html" . }}`. Fix: preprocessTemplate rewrites it.
