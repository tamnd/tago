package render

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"

	"github.com/tamnd/tago/pkg/content"
	"github.com/tamnd/tago/pkg/index"
)

// SiteData holds site-wide metadata available to all templates.
type SiteData struct {
	Title        string
	BaseURL      string
	Description  string
	EditURLBase  string
	Params       map[string]any  // .Site.Params — from [params] in tago.toml
	Pages        []*content.Page // all pages (set by Build)
	RegularPages []*content.Page // kind=page only (set by Build)
}

// AssetRefs holds fingerprinted asset URLs.
type AssetRefs struct {
	CSS        string
	JSHead     string
	JS         string
	FlexSearch string
	KaTeX      string
	ChromaCSS  string
	Extra      map[string]string // original basename → fingerprinted URL
}

// TagCount holds a tag and its page count.
type TagCount struct {
	Name  string
	Count int
}

// SidebarItem is a pre-built sidebar navigation node.
type SidebarItem struct {
	Title     string
	URL       string
	Active    bool
	HasActive bool
	IsParent  bool
	Children  []*SidebarItem
}

// TemplateData is the data passed to all templates.
type TemplateData struct {
	Page             *content.Page
	Site             *SiteData
	Assets           AssetRefs
	Pages            []*content.Page // for list pages: children or tag members
	Tags             []TagCount      // for taxonomy page
	GraphDataJSON    string          // JSON string, embedded inline for type=graph
	TreeDataJSON     string          // JSON string, embedded inline for type=tree
	CalendarDataJSON string          // JSON string, embedded inline for type=calendar
	SidebarItems     []*SidebarItem  // pre-built sidebar nav tree
	SidebarRoot      *content.Page   // page whose subtree is shown in sidebar
	SidebarBack      *content.Page   // back-link target (nil = no back link)
	PrevPage         *content.Page   // previous sibling (for pager)
	NextPage         *content.Page   // next sibling (for pager)
}

// Renderer handles template rendering.
type Renderer struct {
	site          *SiteData
	assets        AssetRefs
	layoutsDir    string
	liveReload    bool
	tmplCache     map[string]*template.Template
	partialCache  map[string]*template.Template
	tmplMu        sync.Mutex
	funcMap       template.FuncMap
}

// New creates a new Renderer.
func New(site *SiteData, assets AssetRefs, layoutsDir string, liveReload bool) *Renderer {
	r := &Renderer{
		site:         site,
		assets:       assets,
		layoutsDir:   layoutsDir,
		liveReload:   liveReload,
		tmplCache:    make(map[string]*template.Template),
		partialCache: make(map[string]*template.Template),
	}
	r.funcMap = r.buildFuncMap()
	return r
}

// determineSidebarRoot returns the page whose children tree should fill the sidebar.
func determineSidebarRoot(page *content.Page) *content.Page {
	if page.Depth <= 2 {
		return page
	}
	if len(page.Ancestors) >= 3 {
		return page.Ancestors[2]
	}
	return page
}

// determineSidebarBack returns the page the "back" link should point to.
func determineSidebarBack(page *content.Page) *content.Page {
	switch page.Depth {
	case 0:
		return nil
	case 1:
		return page.Parent
	default:
		if len(page.Ancestors) >= 2 {
			return page.Ancestors[1]
		}
		return page.Parent
	}
}

// buildSidebarItems recursively converts a page's children into SidebarItems.
// Only the active path (the branch containing currentURL) is expanded;
// sibling branches are collapsed to their section link only.
func buildSidebarItems(root *content.Page, currentURL string) []*SidebarItem {
	var items []*SidebarItem
	for _, child := range root.Children {
		if child.Draft {
			continue
		}
		title := child.LinkTitle
		if title == "" {
			title = child.Title
		}
		item := &SidebarItem{
			Title:    title,
			URL:      child.RelPermalink,
			Active:   child.RelPermalink == currentURL,
			IsParent: len(child.Children) > 0,
		}
		if item.IsParent {
			item.Children = buildSidebarItems(child, currentURL)
			for _, c := range item.Children {
				if c.Active || c.HasActive {
					item.HasActive = true
					break
				}
			}
			// Collapse sections not on the active path.
			if !item.HasActive {
				item.Children = nil
			}
		}
		items = append(items, item)
	}
	return items
}

// markdownify renders a markdown snippet to HTML using goldmark.
func markdownifyString(s string) template.HTML {
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(html.WithUnsafe()),
	)
	var buf bytes.Buffer
	if err := md.Convert([]byte(s), &buf); err != nil {
		return template.HTML(s)
	}
	return template.HTML(buf.String())
}

// plainify strips HTML tags from a string.
func plainify(s string) string {
	re := regexp.MustCompile(`<[^>]+>`)
	return re.ReplaceAllString(s, "")
}

// humanize converts "my-post" or "my_post" to "My post".
func humanize(s string) string {
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// titleCase converts a string to Title Case using simple word splitting.
func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			runes := []rune(w)
			runes[0] = unicode.ToUpper(runes[0])
			words[i] = string(runes)
		}
	}
	return strings.Join(words, " ")
}

// truncateWords truncates a string at a word boundary.
func truncateWords(maxLen int, s string) string {
	if len(s) <= maxLen {
		return s
	}
	truncated := s[:maxLen]
	// find last space
	idx := strings.LastIndex(truncated, " ")
	if idx > 0 {
		truncated = truncated[:idx]
	}
	return truncated + "..."
}

// wherePages filters pages where field == value.
func wherePages(pages []*content.Page, field string, value any) []*content.Page {
	var result []*content.Page
	for _, p := range pages {
		if p == nil {
			continue
		}
		if pageFieldMatches(p, field, value) {
			result = append(result, p)
		}
	}
	return result
}

// pageFieldMatches checks if a page field equals the given value.
func pageFieldMatches(p *content.Page, field string, value any) bool {
	// Handle Params.xxx and .Params.xxx
	if strings.HasPrefix(field, "Params.") || strings.HasPrefix(field, ".Params.") {
		key := strings.TrimPrefix(field, ".Params.")
		key = strings.TrimPrefix(key, "Params.")
		if p.Params == nil {
			return false
		}
		v, ok := p.Params[key]
		if !ok {
			return false
		}
		return reflect.DeepEqual(v, value)
	}

	// Use reflection for struct fields
	rv := reflect.ValueOf(p)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	fv := rv.FieldByName(field)
	if !fv.IsValid() {
		return false
	}
	return reflect.DeepEqual(fv.Interface(), value)
}

// sortPages sorts pages by key (default: Date desc).
func sortPages(pages []*content.Page, key ...string) []*content.Page {
	result := make([]*content.Page, len(pages))
	copy(result, pages)

	sortKey := "Date"
	if len(key) > 0 && key[0] != "" {
		sortKey = key[0]
	}

	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if a == nil || b == nil {
			return a != nil
		}
		switch sortKey {
		case "Title":
			return a.Title < b.Title
		case "Weight":
			return a.Weight < b.Weight
		case "Lastmod":
			return a.Lastmod.After(b.Lastmod)
		case "PublishDate":
			return a.PublishDate.After(b.PublishDate)
		default: // Date desc
			return a.Date.After(b.Date)
		}
	})
	return result
}

// indexInto indexes into a map or slice by key.
func indexInto(collection any, key any) any {
	if collection == nil {
		return nil
	}
	rv := reflect.ValueOf(collection)
	switch rv.Kind() {
	case reflect.Map:
		kv := reflect.ValueOf(key)
		val := rv.MapIndex(kv)
		if !val.IsValid() {
			return nil
		}
		return val.Interface()
	case reflect.Slice, reflect.Array:
		idx, ok := toInt(key)
		if !ok || idx < 0 || idx >= rv.Len() {
			return nil
		}
		return rv.Index(idx).Interface()
	}
	return nil
}

// toInt converts any numeric type to int.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case int32:
		return int(n), true
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	}
	return 0, false
}

// inCollection checks if val is in collection.
func inCollection(collection any, val any) bool {
	if collection == nil {
		return false
	}
	rv := reflect.ValueOf(collection)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < rv.Len(); i++ {
			if reflect.DeepEqual(rv.Index(i).Interface(), val) {
				return true
			}
		}
	case reflect.Map:
		kv := reflect.ValueOf(val)
		return rv.MapIndex(kv).IsValid()
	case reflect.String:
		s, ok := val.(string)
		if ok {
			return strings.Contains(rv.String(), s)
		}
	}
	return false
}

// isZero reports whether a value is considered empty/zero.
func isZero(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String:
		return rv.String() == ""
	case reflect.Bool:
		return !rv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return rv.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return rv.Float() == 0
	case reflect.Slice, reflect.Map, reflect.Array:
		return rv.Len() == 0
	case reflect.Pointer, reflect.Interface:
		return rv.IsNil()
	}
	return false
}

// dateFormatLayouts maps Hugo named layouts to Go time format strings.
var dateFormatLayouts = map[string]string{
	":date_long":   "January 2, 2006",
	":date_medium": "Jan 2, 2006",
	":date_short":  "01/02/06",
	":time":        "15:04:05",
}

// buildFuncMap builds the full template function map for this renderer.
func (r *Renderer) buildFuncMap() template.FuncMap {
	fm := template.FuncMap{
		// ---- existing functions ----
		"rawHTML": func(s string) template.HTML {
			return template.HTML(s)
		},
		"safeJS": func(s string) template.JS {
			return template.JS(s)
		},
		"formatDate": func(t time.Time) string {
			if t.IsZero() {
				return ""
			}
			return t.Format("Jan 2, 2006")
		},
		"urlize": func(s string) string {
			return urlize(s)
		},
		"trimSlash": func(s string) string {
			return strings.TrimRight(s, "/")
		},
		"tagFontSize": func(count int) float64 {
			base := 0.8
			scale := math.Log1p(float64(count)) * 0.3
			size := base + scale
			if size > 2.0 {
				size = 2.0
			}
			return math.Round(size*10) / 10
		},
		"safeURL": func(s string) template.URL {
			return template.URL(s)
		},
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
		"mul": func(a, b int) int { return a * b },
		"dict": func(pairs ...any) map[string]any {
			m := make(map[string]any)
			for i := 0; i+1 < len(pairs); i += 2 {
				if key, ok := pairs[i].(string); ok {
					m[key] = pairs[i+1]
				}
			}
			return m
		},
		"hasPrefix": strings.HasPrefix,
		"hasSuffix": strings.HasSuffix,

		// ---- new Hugo-compatible functions ----

		// Page/collection filters
		"where": wherePages,
		"first": func(n int, pages []*content.Page) []*content.Page {
			if n >= len(pages) {
				return pages
			}
			return pages[:n]
		},
		"last": func(n int, pages []*content.Page) []*content.Page {
			if n >= len(pages) {
				return pages
			}
			return pages[len(pages)-n:]
		},
		"after": func(n int, pages []*content.Page) []*content.Page {
			if n >= len(pages) {
				return nil
			}
			return pages[n:]
		},
		"sort": sortPages,

		// String functions
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
		"safeCSS":  func(s string) template.CSS { return template.CSS(s) },
		"markdownify": func(s string) template.HTML {
			return markdownifyString(s)
		},
		"humanize":   humanize,
		"title":      titleCase,
		"lower":      strings.ToLower,
		"upper":      strings.ToUpper,
		"trim":       strings.Trim,
		"trimLeft":   strings.TrimLeft,
		"trimRight":  strings.TrimRight,
		"trimPrefix": strings.TrimPrefix,
		"trimSuffix": strings.TrimSuffix,
		"replace": func(s, old, newStr string) string {
			return strings.ReplaceAll(s, old, newStr)
		},
		"replaceRE": func(pattern, repl, s string) string {
			re, err := regexp.Compile(pattern)
			if err != nil {
				return s
			}
			return re.ReplaceAllString(s, repl)
		},
		"split": func(s, sep string) []string {
			return strings.Split(s, sep)
		},
		"join": func(sep string, ss []string) string {
			return strings.Join(ss, sep)
		},
		"truncate": truncateWords,
		"plainify": plainify,

		// strings.* namespace (dot-notation names)
		"stringsContains":   strings.Contains,
		"stringsHasPrefix":  strings.HasPrefix,
		"stringsHasSuffix":  strings.HasSuffix,
		"stringsTrimPrefix": strings.TrimPrefix,
		"stringsTrimSuffix": strings.TrimSuffix,

		// printf alias
		"printf": fmt.Sprintf,

		// URL functions
		"absURL": func(path string) string {
			base := strings.TrimRight(r.site.BaseURL, "/")
			if strings.HasPrefix(path, "/") {
				return base + path
			}
			return base + "/" + path
		},
		"relURL": func(path string) string {
			if strings.HasPrefix(path, "/") {
				return path
			}
			return "/" + path
		},

		// Date/time
		"dateFormat": func(layout string, t time.Time) string {
			if goLayout, ok := dateFormatLayouts[layout]; ok {
				return t.Format(goLayout)
			}
			return t.Format(layout)
		},
		"now": time.Now,

		// Math
		"div": func(a, b int) int {
			if b == 0 {
				return 0
			}
			return a / b
		},
		"mod": func(a, b int) int {
			if b == 0 {
				return 0
			}
			return a % b
		},
		"modBool": func(a, b int) bool {
			if b == 0 {
				return false
			}
			return a%b == 0
		},
		// math functions — named without dots (Go template FuncMap identifiers must be valid Go identifiers)
		// Hugo templates call these as {{ math.Ceil x }} using Hugo's namespace system;
		// tago exposes them as flat names: mathCeil, mathFloor, etc., plus common aliases.
		"mathAdd":   func(a, b int) int { return a + b },
		"mathSub":   func(a, b int) int { return a - b },
		"mathMul":   func(a, b int) int { return a * b },
		"mathDiv":   func(a, b int) int { if b == 0 { return 0 }; return a / b },
		"mathMod":   func(a, b int) int { if b == 0 { return 0 }; return a % b },
		"mathAbs":   func(x float64) float64 { return math.Abs(x) },
		"mathCeil":  func(x float64) float64 { return math.Ceil(x) },
		"mathFloor": func(x float64) float64 { return math.Floor(x) },
		"mathRound": func(x float64) float64 { return math.Round(x) },
		"mathLog":   func(x float64) float64 { return math.Log(x) },
		"mathSqrt":  func(x float64) float64 { return math.Sqrt(x) },

		// Collections
		"default": func(dflt, val any) any {
			if isZero(val) {
				return dflt
			}
			return val
		},
		"in":        inCollection,
		"intersect": func(a, b []string) []string {
			set := make(map[string]bool, len(b))
			for _, s := range b {
				set[s] = true
			}
			var result []string
			for _, s := range a {
				if set[s] {
					result = append(result, s)
				}
			}
			return result
		},
		"union": func(a, b []string) []string {
			seen := make(map[string]bool)
			var result []string
			for _, s := range a {
				if !seen[s] {
					seen[s] = true
					result = append(result, s)
				}
			}
			for _, s := range b {
				if !seen[s] {
					seen[s] = true
					result = append(result, s)
				}
			}
			return result
		},
		"uniq": func(ss []string) []string {
			seen := make(map[string]bool)
			var result []string
			for _, s := range ss {
				if !seen[s] {
					seen[s] = true
					result = append(result, s)
				}
			}
			return result
		},
		"append": func(ss []string, items ...string) []string {
			return append(ss, items...)
		},
		"slice": func(items ...any) []any {
			return items
		},
		"index": indexInto,
		"isset": func(m map[string]any, key string) bool {
			if m == nil {
				return false
			}
			_, ok := m[key]
			return ok
		},
		"seq": func(n int) []int {
			result := make([]int, n)
			for i := range result {
				result[i] = i + 1
			}
			return result
		},
		"cond": func(condition bool, a, b any) any {
			if condition {
				return a
			}
			return b
		},

		// Encoding
		"jsonify": func(v any) string {
			b, err := json.Marshal(v)
			if err != nil {
				return ""
			}
			return string(b)
		},
		"base64Encode": func(s string) string {
			return base64.StdEncoding.EncodeToString([]byte(s))
		},
		"base64Decode": func(s string) string {
			b, err := base64.StdEncoding.DecodeString(s)
			if err != nil {
				return ""
			}
			return string(b)
		},

		// Type conversion
		"string": func(v any) string {
			return fmt.Sprintf("%v", v)
		},
		"int": func(v any) int {
			if n, ok := toInt(v); ok {
				return n
			}
			return 0
		},
		"float": func(v any) float64 {
			switch n := v.(type) {
			case float64:
				return n
			case float32:
				return float64(n)
			case int:
				return float64(n)
			case int64:
				return float64(n)
			}
			return 0
		},
	}

	// partial: load and render a partial template
	fm["partial"] = func(name string, ctx any) (template.HTML, error) {
		return r.renderPartial(name, ctx)
	}

	return fm
}

// renderPartial renders a named partial template with the given context.
func (r *Renderer) renderPartial(name string, ctx any) (template.HTML, error) {
	if !strings.HasSuffix(name, ".html") {
		name = name + ".html"
	}

	r.tmplMu.Lock()
	t, ok := r.partialCache[name]
	r.tmplMu.Unlock()

	if !ok {
		var partialContent string
		if r.layoutsDir != "" {
			partialPath := filepath.Join(r.layoutsDir, "partials", name)
			if data, err := os.ReadFile(partialPath); err == nil {
				partialContent = string(data)
			}
		}
		if partialContent == "" {
			return "", fmt.Errorf("partial %q not found", name)
		}

		parsed, err := template.New(name).Funcs(r.funcMap).Parse(partialContent)
		if err != nil {
			return "", fmt.Errorf("parse partial %q: %w", name, err)
		}

		r.tmplMu.Lock()
		r.partialCache[name] = parsed
		r.tmplMu.Unlock()
		t = parsed
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("execute partial %q: %w", name, err)
	}
	return template.HTML(buf.String()), nil
}

// hugoLookupOrder returns the ordered list of template paths to check for a given page.
// Each entry is a path relative to layoutsDir.
func hugoLookupOrder(kind, pageType, layout, section, lang string) []string {
	var candidates []string

	// Helper to add both lang-suffixed and plain variants.
	add := func(base string) {
		if lang != "" && lang != "en" {
			ext := filepath.Ext(base)
			without := strings.TrimSuffix(base, ext)
			candidates = append(candidates, without+"."+lang+ext)
		}
		candidates = append(candidates, base)
	}

	switch kind {
	case "page":
		typ := pageType
		if typ == "" {
			typ = section
		}
		if typ != "" {
			if layout != "" {
				add(typ + "/" + layout + ".html")
			}
			add(typ + "/single.html")
		}
		if layout != "" {
			add("_default/" + layout + ".html")
		}
		add("_default/single.html")

	case "section":
		sec := section
		if sec == "" {
			sec = pageType
		}
		if sec != "" {
			if layout != "" {
				add(sec + "/" + layout + ".html")
			}
			add(sec + "/list.html")
		}
		if layout != "" {
			add("_default/" + layout + ".html")
		}
		add("_default/list.html")

	case "home":
		add("index.html")
		add("home.html")
		add("_default/home.html")
		add("_default/list.html")

	case "taxonomy":
		plural := section
		if plural == "" {
			plural = pageType
		}
		if plural != "" {
			add(plural + "/taxonomy.html")
			add(plural + "/list.html")
		}
		add("_default/taxonomy.html")
		add("_default/list.html")

	case "term":
		plural := section
		if plural == "" {
			plural = pageType
		}
		if plural != "" {
			add(plural + "/term.html")
			add(plural + "/list.html")
		}
		add("_default/term.html")
		add("_default/list.html")

	case "404":
		add("404.html")
		add("_default/404.html")

	default:
		// Special/other kinds: try kind.html in _default then at root
		if kind != "" {
			add(kind + ".html")
			add("_default/" + kind + ".html")
		}
	}

	return candidates
}

// hugoBaseofOrder returns the ordered list of baseof template paths to check.
func hugoBaseofOrder(pageType, section string) []string {
	var candidates []string
	typ := pageType
	if typ == "" {
		typ = section
	}
	if typ != "" {
		candidates = append(candidates, typ+"/baseof.html")
	}
	candidates = append(candidates, "_default/baseof.html")
	return candidates
}

// templateCacheKey builds a unique cache key for a template lookup.
func templateCacheKey(kind, pageType, layout, section, lang string) string {
	return kind + "|" + pageType + "|" + layout + "|" + section + "|" + lang
}

// getTemplate returns a compiled template for the given page parameters.
// It implements Hugo's template lookup order and baseof composition.
// Safe for concurrent use.
func (r *Renderer) getTemplate(kind string) (*template.Template, error) {
	return r.getTemplateFor(kind, "", "", "", "")
}

// getTemplateFor returns a compiled template using full Hugo lookup order.
func (r *Renderer) getTemplateFor(kind, pageType, layout, section, lang string) (*template.Template, error) {
	cacheKey := templateCacheKey(kind, pageType, layout, section, lang)

	r.tmplMu.Lock()
	defer r.tmplMu.Unlock()

	if t, ok := r.tmplCache[cacheKey]; ok {
		return t, nil
	}

	// page-live-reload is always defined: JS block when enabled, no-op otherwise.
	liveReloadBlock := `{{define "page-live-reload"}}{{end}}`
	if r.liveReload {
		liveReloadBlock = defaultTemplates["live-reload-js"]
	}

	breadcrumb := defaultTemplates["breadcrumb"]

	// Determine the kind template content.
	kindTmpl := ""
	kindSource := "" // for error messages

	// First: try Hugo lookup order in layoutsDir.
	if r.layoutsDir != "" {
		candidates := hugoLookupOrder(kind, pageType, layout, section, lang)
		for _, candidate := range candidates {
			path := filepath.Join(r.layoutsDir, candidate)
			if data, err := os.ReadFile(path); err == nil {
				kindTmpl = string(data)
				kindSource = candidate
				break
			}
		}
	}

	// Also try legacy flat lookup for built-in types (backward compat):
	// e.g. layouts/page.html, layouts/home.html etc.
	if kindTmpl == "" && r.layoutsDir != "" {
		if data, err := os.ReadFile(filepath.Join(r.layoutsDir, kind+".html")); err == nil {
			kindTmpl = string(data)
			kindSource = kind + ".html"
		}
	}

	// Fall back to embedded default.
	if kindTmpl == "" {
		var ok bool
		kindTmpl, ok = defaultTemplates[kind]
		if !ok {
			return nil, fmt.Errorf("no template for kind %q", kind)
		}
		kindSource = "embedded:" + kind
	}
	_ = kindSource // used only for debugging

	// Determine the baseof template content.
	baseof := defaultTemplates["baseof"]
	if r.layoutsDir != "" {
		baseofCandidates := hugoBaseofOrder(pageType, section)
		for _, candidate := range baseofCandidates {
			path := filepath.Join(r.layoutsDir, candidate)
			if data, err := os.ReadFile(path); err == nil {
				baseof = string(data)
				break
			}
		}
		// Also try flat layouts/baseof.html (legacy).
		if baseof == defaultTemplates["baseof"] {
			if data, err := os.ReadFile(filepath.Join(r.layoutsDir, "baseof.html")); err == nil {
				baseof = string(data)
			}
		}
	}

	// Compose: baseof + breadcrumb + kindTmpl + liveReloadBlock
	combined := baseof + "\n" +
		breadcrumb + "\n" +
		kindTmpl + "\n" +
		liveReloadBlock

	t, err := template.New("baseof").Funcs(r.funcMap).Parse(combined)
	if err != nil {
		return nil, fmt.Errorf("parse template %q: %w", kind, err)
	}
	r.tmplCache[cacheKey] = t
	return t, nil
}

// RenderPage renders a single page to its OutputPath.
func (r *Renderer) RenderPage(page *content.Page, allPages []*content.Page) error {
	kind := page.Kind
	if kind == "" {
		kind = "page"
	}

	// Type overrides kind for special visualization pages
	templateName := kind
	switch page.Type {
	case "graph":
		templateName = "graph"
	case "tree":
		templateName = "tree"
	case "calendar":
		templateName = "calendar"
	}

	data := &TemplateData{Page: page, Site: r.site, Assets: r.assets}

	// Build sidebar navigation data
	sidebarRoot := determineSidebarRoot(page)
	data.SidebarRoot = sidebarRoot
	data.SidebarBack = determineSidebarBack(page)
	if sidebarRoot != nil {
		data.SidebarItems = buildSidebarItems(sidebarRoot, page.RelPermalink)
	}

	// Compute prev/next siblings for pager
	if page.Parent != nil && page.Kind == "page" {
		siblings := page.Parent.Children
		for i, sib := range siblings {
			if sib.RelPermalink == page.RelPermalink {
				if i > 0 {
					data.PrevPage = siblings[i-1]
				}
				if i < len(siblings)-1 {
					data.NextPage = siblings[i+1]
				}
				break
			}
		}
	}

	// Build visualization data when needed
	switch page.Type {
	case "graph":
		gd := index.BuildGraphData(allPages)
		b, _ := json.Marshal(gd)
		data.GraphDataJSON = string(b)
	case "tree":
		td := index.BuildTreeData(allPages)
		b, _ := json.Marshal(td)
		data.TreeDataJSON = string(b)
	case "calendar":
		cd := index.BuildCalendarData(allPages)
		b, _ := json.Marshal(cd)
		data.CalendarDataJSON = string(b)
	default:
		// Normal pages: build list for section/home
		switch kind {
		case "section":
			for _, c := range page.Children {
				if !c.Draft {
					data.Pages = append(data.Pages, c)
				}
			}
		case "home":
			for _, p := range allPages {
				if p.Kind == "page" && !p.Draft && !p.Date.IsZero() {
					data.Pages = append(data.Pages, p)
				}
			}
			sort.Slice(data.Pages, func(i, j int) bool {
				return data.Pages[i].Date.After(data.Pages[j].Date)
			})
		}
	}

	return r.renderToFileFor(templateName, page.OutputPath, data, page.Type, page.Layout, page.Section, page.Lang)
}

// Render404 renders a 404.html page to outputDir/404.html.
func (r *Renderer) Render404(outputDir string, allPages []*content.Page) error {
	outputPath := filepath.Join(outputDir, "404.html")
	fakePage := &content.Page{
		Title:        "404 - Page Not Found",
		Kind:         "404",
		RelPermalink: "/404.html",
		OutputPath:   outputPath,
	}
	data := &TemplateData{
		Page:   fakePage,
		Site:   r.site,
		Assets: r.assets,
	}
	return r.renderToFile("404", outputPath, data)
}

// RenderTagPage renders a tag term page.
func (r *Renderer) RenderTagPage(tag string, pages []*content.Page, outputDir string) error {
	urlized := urlize(tag)
	permalink := "/tags/" + urlized + "/"
	outputPath := filepath.Join(outputDir, "tags", urlized, "index.html")

	fakePage := &content.Page{
		Title:        tag,
		LinkTitle:    tag,
		RelPermalink: permalink,
		Kind:         "term",
		Section:      "tags",
	}

	data := &TemplateData{
		Page:   fakePage,
		Site:   r.site,
		Assets: r.assets,
		Pages:  pages,
	}

	return r.renderToFile("term", outputPath, data)
}

// RenderTaxonomyPage renders the /tags/ overview page.
func (r *Renderer) RenderTaxonomyPage(tags []TagCount, outputDir string) error {
	fakePage := &content.Page{
		Title:        "Tags",
		LinkTitle:    "Tags",
		RelPermalink: "/tags/",
		Kind:         "taxonomy",
		Section:      "tags",
	}

	data := &TemplateData{
		Page:   fakePage,
		Site:   r.site,
		Assets: r.assets,
		Tags:   tags,
	}

	outputPath := filepath.Join(outputDir, "tags", "index.html")
	return r.renderToFile("taxonomy", outputPath, data)
}

// RenderSearchPage renders the /search/ page.
func (r *Renderer) RenderSearchPage(outputDir string) error {
	fakePage := &content.Page{
		Title:        "Search",
		LinkTitle:    "Search",
		RelPermalink: "/search/",
		Kind:         "special",
	}

	data := &TemplateData{
		Page:   fakePage,
		Site:   r.site,
		Assets: r.assets,
	}

	outputPath := filepath.Join(outputDir, "search", "index.html")
	return r.renderToFile("search", outputPath, data)
}

func (r *Renderer) renderToFile(kind, outputPath string, data *TemplateData) error {
	return r.renderToFileFor(kind, outputPath, data, "", "", "", "")
}

func (r *Renderer) renderToFileFor(kind, outputPath string, data *TemplateData, pageType, layout, section, lang string) error {
	t, err := r.getTemplateFor(kind, pageType, layout, section, lang)
	if err != nil {
		return err
	}

	// Render to an in-memory buffer first so template execution (many small writes)
	// stays in RAM, then flush to disk in one WriteFile call (one syscall per page).
	// Clone() is not needed here because Prewarm() pre-initializes html/template's
	// lazy auto-escaping state; subsequent concurrent Executes are safe read-only.
	var buf bytes.Buffer
	buf.Grow(32 * 1024)
	if err := t.Execute(&buf, data); err != nil {
		return fmt.Errorf("execute template %q for %s: %w", kind, outputPath, err)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(outputPath), err)
	}
	if err := os.WriteFile(outputPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("write %s: %w", outputPath, err)
	}
	return nil
}

// Prewarm executes each known template kind once with empty data to trigger
// html/template's lazy auto-escaping initialization. After this, concurrent
// Execute calls on the same template are safe without Clone().
func (r *Renderer) Prewarm() {
	for _, kind := range []string{"page", "section", "home", "404", "term", "taxonomy", "search", "graph", "tree", "calendar"} {
		if t, err := r.getTemplate(kind); err == nil {
			var buf bytes.Buffer
			// Empty TemplateData is sufficient to trigger escaping initialization.
			_ = t.Execute(&buf, &TemplateData{Page: &content.Page{Kind: kind}})
		}
	}
}

// InvalidateCache clears the template cache (useful after layout file changes).
func (r *Renderer) InvalidateCache() {
	r.tmplMu.Lock()
	defer r.tmplMu.Unlock()
	r.tmplCache = make(map[string]*template.Template)
	r.partialCache = make(map[string]*template.Template)
}

func urlize(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
