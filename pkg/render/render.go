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

	// Hugo-compatible fields
	LanguageCode string            // e.g. "en-us"
	Language     *SiteLanguage     // language metadata object
	Copyright    string
	Author       map[string]any
	Menus        map[string][]SiteMenuItem // .Site.Menus.main etc.
}

// SiteMenuItem represents a single navigation menu item.
type SiteMenuItem struct {
	Name        string
	URL         string
	Weight      int
	Identifier  string
	HasChildren bool
	Children    []SiteMenuItem
}

// HugoPage wraps *content.Page with Site for use inside range loops in Hugo themes.
// Hugo themes do {{range .Pages}}...{{.Site.Params.x}}...{{end}} expecting .Site on each page.
type HugoPage struct {
	*content.Page
	Site *SiteData
}

// Date returns HugoTime for flexible Format calls in templates.
func (hp *HugoPage) Date() HugoTime { return newHugoTime(hp.Page.Date) }

// PublishDate returns HugoTime.
func (hp *HugoPage) PublishDate() HugoTime { return newHugoTime(hp.Page.PublishDate) }

// Lastmod returns HugoTime.
func (hp *HugoPage) Lastmod() HugoTime { return newHugoTime(hp.Page.Lastmod) }

// ExpiryDate returns HugoTime.
func (hp *HugoPage) ExpiryDate() HugoTime { return newHugoTime(hp.Page.ExpiryDate) }

// Permalink returns the full URL.
func (hp *HugoPage) Permalink() string { return hp.Page.Permalink }

// RelPermalink returns the relative URL.
func (hp *HugoPage) RelPermalink() string { return hp.Page.RelPermalink }

// IsPage returns true if page kind is "page".
func (hp *HugoPage) IsPage() bool { return hp.Page.Kind == "page" }

// IsHome returns true if page kind is "home".
func (hp *HugoPage) IsHome() bool { return hp.Page.Kind == "home" }

// IsSection returns true if page kind is "section".
func (hp *HugoPage) IsSection() bool { return hp.Page.Kind == "section" }

// PageData mirrors Hugo's .Data field on taxonomy/term pages (provides .Pages).
type PageData struct {
	Pages HugoPageList
}

// HugoPageList wraps []*HugoPage with Hugo-compatible methods.
type HugoPageList []*HugoPage

// GroupByDate groups pages by a date format string, returning [{Key, Pages}] sorted descending.
func (pl HugoPageList) GroupByDate(layout string) []HugoPageGroup {
	groups := map[string]HugoPageList{}
	order := []string{}
	for _, p := range pl {
		key := p.Date().Format(layout)
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], p)
	}
	result := make([]HugoPageGroup, 0, len(order))
	for _, k := range order {
		result = append(result, HugoPageGroup{Key: k, Pages: groups[k]})
	}
	return result
}

// HugoPageGroup is a group of pages with a common key (e.g. year).
type HugoPageGroup struct {
	Key   string
	Pages HugoPageList
}

// wrapPages wraps []*content.Page into HugoPageList, attaching site data to each page.
func wrapPages(pages []*content.Page, site *SiteData) HugoPageList {
	out := make(HugoPageList, len(pages))
	for i, p := range pages {
		out[i] = &HugoPage{Page: p, Site: site}
	}
	return out
}

// SiteLanguage holds language-specific site metadata for .Site.Language
type SiteLanguage struct {
	Lang              string // language code (e.g. "en")
	LanguageCode      string
	LanguageName      string
	LanguageDirection string
	Weight            int
}

func (l *SiteLanguage) String() string {
	if l == nil {
		return "en"
	}
	return l.Lang
}

// Get returns a language param (stub for Hugo .Site.Language.Get "key").
func (l *SiteLanguage) Get(key string) any {
	return nil
}

// Hugo returns the hugo namespace (allows {{ .Site.Hugo.Version }} in templates).
func (s *SiteData) Hugo() *hugoNamespace { return &hugoNamespace{} }

// DisqusShortname returns empty string (stub).
func (s *SiteData) DisqusShortname() string { return "" }

// GoogleAnalytics returns empty string (stub).
func (s *SiteData) GoogleAnalytics() string { return "" }

// RSSLink returns the RSS feed URL for the site.
func (s *SiteData) RSSLink() string {
	return strings.TrimRight(s.BaseURL, "/") + "/index.xml"
}

// Data returns empty map (stub for .Site.Data).
func (s *SiteData) Data() map[string]any { return nil }

// Taxonomies returns empty map (stub for .Site.Taxonomies).
func (s *SiteData) Taxonomies() map[string]any { return nil }

// Param looks up a param in Site.Params (Hugo .Param method falls through to site).
func (s *SiteData) Param(key string) any {
	if s.Params != nil {
		return s.Params[key]
	}
	return nil
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

// HugoTime wraps time.Time with a Format method that accepts any (string or nil).
// Hugo themes use .Date.Format .Site.Params.dateformShort where the format is interface{}.
type HugoTime struct {
	time.Time
}

func newHugoTime(t time.Time) HugoTime { return HugoTime{t} }

// Format overrides time.Time.Format to accept any format value (string, nil, etc.).
func (ht HugoTime) Format(layout any) string {
	if ht.IsZero() {
		return ""
	}
	var l string
	switch v := layout.(type) {
	case string:
		l = v
	case nil:
		l = "2006-01-02"
	default:
		l = fmt.Sprintf("%v", v)
	}
	if l == "" {
		l = "2006-01-02"
	}
	return ht.Time.Format(l)
}

// TemplateData is the data passed to all templates.
type TemplateData struct {
	Page             *content.Page
	Site             *SiteData
	Assets           AssetRefs
	Pages            HugoPageList    // for list pages: children or tag members
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

// Hugo-compatible page field forwarding methods on TemplateData.
// Hugo themes use .Title, .Content, .Permalink, etc. directly on . (the context).
// These methods delegate to .Page so Hugo themes work unchanged.

func (d *TemplateData) Title() string {
	if d.Page == nil {
		return ""
	}
	return d.Page.Title
}

func (d *TemplateData) LinkTitle() string {
	if d.Page == nil {
		return ""
	}
	if d.Page.LinkTitle != "" {
		return d.Page.LinkTitle
	}
	return d.Page.Title
}

func (d *TemplateData) Description() string {
	if d.Page == nil {
		return ""
	}
	return d.Page.Description
}

func (d *TemplateData) Summary() string {
	if d.Page == nil {
		return ""
	}
	return d.Page.Summary
}

func (d *TemplateData) Content() template.HTML {
	if d.Page == nil {
		return ""
	}
	return template.HTML(d.Page.ContentHTML)
}

func (d *TemplateData) Permalink() string {
	if d.Page == nil {
		return ""
	}
	return d.Page.Permalink
}

func (d *TemplateData) RelPermalink() string {
	if d.Page == nil {
		return ""
	}
	return d.Page.RelPermalink
}

func (d *TemplateData) Date() HugoTime {
	if d.Page == nil {
		return HugoTime{}
	}
	return newHugoTime(d.Page.Date)
}

func (d *TemplateData) PublishDate() HugoTime {
	if d.Page == nil {
		return HugoTime{}
	}
	return newHugoTime(d.Page.PublishDate)
}

func (d *TemplateData) ExpiryDate() HugoTime {
	if d.Page == nil {
		return HugoTime{}
	}
	return newHugoTime(d.Page.ExpiryDate)
}

func (d *TemplateData) Lastmod() HugoTime {
	if d.Page == nil {
		return HugoTime{}
	}
	return newHugoTime(d.Page.Lastmod)
}

func (d *TemplateData) Draft() bool {
	if d.Page == nil {
		return false
	}
	return d.Page.Draft
}

func (d *TemplateData) Weight() int {
	if d.Page == nil {
		return 0
	}
	return d.Page.Weight
}

func (d *TemplateData) Kind() string {
	if d.Page == nil {
		return ""
	}
	return d.Page.Kind
}

func (d *TemplateData) Type() string {
	if d.Page == nil {
		return ""
	}
	return d.Page.Type
}

func (d *TemplateData) Layout() string {
	if d.Page == nil {
		return ""
	}
	return d.Page.Layout
}

func (d *TemplateData) Section() string {
	if d.Page == nil {
		return ""
	}
	return d.Page.Section
}

func (d *TemplateData) Lang() string {
	if d.Page == nil {
		return ""
	}
	return d.Page.Lang
}

func (d *TemplateData) Slug() string {
	if d.Page == nil {
		return ""
	}
	return d.Page.Slug
}

func (d *TemplateData) Params() map[string]any {
	if d.Page == nil {
		return nil
	}
	return d.Page.Params
}

func (d *TemplateData) Param(key string) any {
	if d.Page != nil {
		if v, ok := d.Page.Params[key]; ok {
			return v
		}
	}
	if d.Site != nil {
		if v, ok := d.Site.Params[key]; ok {
			return v
		}
	}
	return nil
}

func (d *TemplateData) IsPage() bool {
	if d.Page == nil {
		return false
	}
	return d.Page.Kind == "page"
}

func (d *TemplateData) IsHome() bool {
	if d.Page == nil {
		return false
	}
	return d.Page.Kind == "home"
}

func (d *TemplateData) IsSection() bool {
	if d.Page == nil {
		return false
	}
	return d.Page.Kind == "section"
}

func (d *TemplateData) IsNode() bool {
	if d.Page == nil {
		return false
	}
	k := d.Page.Kind
	return k == "home" || k == "section" || k == "taxonomy" || k == "term"
}

func (d *TemplateData) WordCount() int {
	if d.Page == nil {
		return 0
	}
	return d.Page.WordCount
}

func (d *TemplateData) ReadingTime() int {
	if d.Page == nil {
		return 0
	}
	wc := d.Page.WordCount
	if wc == 0 {
		return 1
	}
	rt := wc / 200
	if rt == 0 {
		return 1
	}
	return rt
}

func (d *TemplateData) FuzzyWordCount() int {
	if d.Page == nil {
		return 0
	}
	return d.Page.FuzzyWordCount()
}

func (d *TemplateData) Parent() *content.Page {
	if d.Page == nil {
		return nil
	}
	return d.Page.Parent
}

func (d *TemplateData) Ancestors() []*content.Page {
	if d.Page == nil {
		return nil
	}
	return d.Page.Ancestors
}

// RegularPages returns only kind=page children (for section/home templates).
func (d *TemplateData) RegularPages() HugoPageList {
	var out HugoPageList
	for _, p := range d.Pages {
		if p.Kind == "page" {
			out = append(out, p)
		}
	}
	return out
}

// Next returns the next page in the sequence (Hugo: .Next).
func (d *TemplateData) Next() *content.Page {
	return d.NextPage
}

// Prev returns the previous page in the sequence (Hugo: .Prev).
func (d *TemplateData) Prev() *content.Page {
	return d.PrevPage
}

// AlternativeOutputFormats returns empty slice (stub for Hugo compatibility).
func (d *TemplateData) AlternativeOutputFormats() []any {
	return nil
}

// Scratch returns a map stub for Hugo's .Scratch.
func (d *TemplateData) Scratch() map[string]any {
	return make(map[string]any)
}

// Language returns the site language object.
func (d *TemplateData) Language() *SiteLanguage {
	if d.Site != nil {
		return d.Site.Language
	}
	return nil
}

// Data returns Hugo-compatible .Data field (for taxonomy/term pages).
func (d *TemplateData) Data() *PageData {
	return &PageData{Pages: d.Pages}
}

// GitInfo returns nil (stub for Hugo .GitInfo; nil makes {{- if .GitInfo }} false).
func (d *TemplateData) GitInfo() any {
	return nil
}

// NextInSection returns the next page in the section (alias for .Next).
func (d *TemplateData) NextInSection() *content.Page {
	return d.NextPage
}

// PrevInSection returns the prev page in the section (alias for .Prev).
func (d *TemplateData) PrevInSection() *content.Page {
	return d.PrevPage
}

// TableOfContents returns empty string (stub for Hugo .TableOfContents).
func (d *TemplateData) TableOfContents() template.HTML {
	return ""
}

// Truncated returns false (stub for Hugo .Truncated; true means Summary != full content).
func (d *TemplateData) Truncated() bool {
	if d.Page == nil {
		return false
	}
	return len(d.Page.Summary) > 0 && d.Page.Summary != d.Page.ContentPlain
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
	i18nData      map[string]string // loaded from i18n/*.toml or *.yaml
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
		"safeJS": func(v any) template.JS {
			if v == nil {
				return ""
			}
			return template.JS(fmt.Sprintf("%v", v))
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
		"safeURL": func(v any) template.URL {
			if v == nil {
				return ""
			}
			return template.URL(fmt.Sprintf("%v", v))
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

		// String functions — accept any to handle nil params, template.HTML, etc.
		"safeHTML": func(v any) template.HTML {
			if v == nil {
				return ""
			}
			if h, ok := v.(template.HTML); ok {
				return h
			}
			return template.HTML(fmt.Sprintf("%v", v))
		},
		"safeCSS": func(v any) template.CSS {
			if v == nil {
				return ""
			}
			return template.CSS(fmt.Sprintf("%v", v))
		},
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
		"replaceRE": func(pattern, repl string, v any) any {
			var s string
			switch sv := v.(type) {
			case string:
				s = sv
			case template.HTML:
				re, err := regexp.Compile(pattern)
				if err != nil {
					return v
				}
				return template.HTML(re.ReplaceAllString(string(sv), repl))
			default:
				s = fmt.Sprintf("%v", v)
			}
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
		"dateFormat": func(layout any, t any) string {
			var l string
			switch v := layout.(type) {
			case string:
				l = v
			case nil:
				l = "2006-01-02"
			default:
				l = fmt.Sprintf("%v", v)
			}
			if goLayout, ok := dateFormatLayouts[l]; ok {
				l = goLayout
			}
			if l == "" {
				l = "2006-01-02"
			}
			switch tv := t.(type) {
			case time.Time:
				return tv.Format(l)
			case HugoTime:
				return tv.Time.Format(l)
			}
			return fmt.Sprintf("%v", t)
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

	// Hugo Pipes standalone functions (for pipeline syntax: {{ $r | toCSS }})
	fm["toCSS"] = func(r *stubResource, opts ...any) *stubResource {
		if r == nil {
			return &stubResource{}
		}
		return r
	}
	fm["minify"] = func(r *stubResource) *stubResource {
		if r == nil {
			return &stubResource{}
		}
		return r
	}
	fm["fingerprint"] = func(args ...any) *stubResource {
		for _, a := range args {
			if sr, ok := a.(*stubResource); ok {
				return sr
			}
		}
		return &stubResource{}
	}
	fm["postCSS"] = func(r *stubResource, opts ...any) *stubResource {
		if r == nil {
			return &stubResource{}
		}
		return r
	}
	fm["babel"] = func(r *stubResource, opts ...any) *stubResource {
		if r == nil {
			return &stubResource{}
		}
		return r
	}

	// partial: load and render a partial template
	fm["partial"] = func(name string, args ...any) (template.HTML, error) {
		var ctx any
		if len(args) > 0 {
			ctx = args[0]
		}
		return r.renderPartial(name, ctx)
	}

	// partialCached: like partial but caches by key (args[0] is ctx, rest are cache keys)
	fm["partialCached"] = func(name string, args ...any) (template.HTML, error) {
		var ctx any
		if len(args) > 0 {
			ctx = args[0]
		}
		return r.renderPartial(name, ctx)
	}

	// i18n: return translated string or the key itself (stub without actual i18n files)
	fm["i18n"] = func(key string, args ...any) string {
		return r.i18nLookup(key)
	}
	fm["T"] = fm["i18n"] // Hugo alias

	// safeHTMLAttr wraps a string as a safe HTML attribute value
	fm["safeHTMLAttr"] = func(v any) template.HTMLAttr {
		if v == nil {
			return ""
		}
		return template.HTMLAttr(fmt.Sprintf("%v", v))
	}

	// absLangURL is like absURL but also intended for multilingual sites
	fm["absLangURL"] = func(path string) string {
		base := strings.TrimRight(r.site.BaseURL, "/")
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		return base + path
	}

	// relLangURL is like relURL but also intended for multilingual sites
	fm["relLangURL"] = func(path string) string {
		if strings.HasPrefix(path, "/") {
			return path
		}
		return "/" + path
	}

	// hugo namespace stub — returns an object with a Version field
	fm["hugo"] = func() *hugoNamespace { return &hugoNamespace{} }

	// site global — returns SiteData (allows {{ site.Params.xxx }} in newer Hugo themes)
	fm["site"] = func() *SiteData { return r.site }

	// resources namespace stub — all methods return a stub resource
	fm["resources"] = func() *resourcesNamespace { return &resourcesNamespace{} }

	// templates namespace stub
	fm["templates"] = func() *templatesNamespace { return &templatesNamespace{layoutsDir: r.layoutsDir} }

	// path namespace
	fm["path"] = func() *pathNamespace { return &pathNamespace{} }

	// reflect namespace stub
	fm["reflect"] = func() *reflectNamespace { return &reflectNamespace{} }

	// os stub
	fm["os"] = func() *osNamespace { return &osNamespace{} }

	// warnf/errorf/debugf — log and continue (don't panic)
	fm["warnf"] = func(format string, args ...any) string {
		return "" // silently ignore
	}
	fm["errorf"] = func(format string, args ...any) string {
		return "" // silently ignore (don't fatalf)
	}
	fm["debugf"] = func(format string, args ...any) string { return "" }

	// .Param helper — looks up a param in page then site
	fm["param"] = func(key string) any { return nil }

	// urls namespace
	fm["urls"] = func() *urlsNamespace { return &urlsNamespace{baseURL: r.site.BaseURL} }

	// crypto stub
	fm["crypto"] = func() *cryptoNamespace { return &cryptoNamespace{} }

	// collections namespace
	fm["collections"] = func() *collectionsNamespace { return &collectionsNamespace{} }

	return fm
}

// --- Namespace stubs for Hugo-compatible template evaluation ---

// hugoNamespace provides the `hugo` template namespace (hugo.Version, hugo.IsProduction, etc.)
type hugoNamespace struct{}

func (h *hugoNamespace) Version() string     { return "0.147.0" }
func (h *hugoNamespace) IsProduction() bool  { return false }
func (h *hugoNamespace) IsDevelopment() bool { return true }
func (h *hugoNamespace) IsServer() bool      { return false }
func (h *hugoNamespace) Generator() template.HTML {
	return template.HTML(`<meta name="generator" content="Hugo 0.147.0">`)
}
func (h *hugoNamespace) BuildDate() string { return "" }
func (h *hugoNamespace) CommitHash() string { return "" }
func (h *hugoNamespace) Environment() string { return "development" }

// stubResource is the result of any resources pipeline step.
// All methods return the same stub so pipelines like
//   resources.Get "x" | resources.ExecuteAsTemplate "y" . | toCSS | minify | fingerprint
// can be chained without crashing.
type stubResource struct {
	path string
}

func (s *stubResource) Permalink() string         { return "" }
func (s *stubResource) RelPermalink() string      { return "" }
func (s *stubResource) Content() template.HTML    { return "" }
func (s *stubResource) String() string             { return "" }
func (s *stubResource) MediaType() *stubMediaType { return &stubMediaType{} }
func (s *stubResource) Data() *stubResourceData   { return &stubResourceData{} }
func (s *stubResource) Name() string              { return s.path }
func (s *stubResource) Params() map[string]any    { return nil }
// Transformation chains
func (s *stubResource) ToCSS(...any) *stubResource               { return s }
func (s *stubResource) Minify() *stubResource                    { return s }
func (s *stubResource) Fingerprint(...string) *stubResource      { return s }
func (s *stubResource) ExecuteAsTemplate(name string, ctx any) *stubResource { return s }
func (s *stubResource) PostProcess() *stubResource               { return s }

type stubMediaType struct{}
func (m *stubMediaType) Type() string   { return "text/css" }
func (m *stubMediaType) String() string { return "text/css" }

type stubResourceData struct{}
func (d *stubResourceData) Integrity() string { return "" }
func (d *stubResourceData) TransitionKey() string { return "" }

// resourcesNamespace implements the `resources` template namespace.
type resourcesNamespace struct{}

func (rn *resourcesNamespace) Get(path string) *stubResource {
	return &stubResource{path: path}
}
func (rn *resourcesNamespace) GetRemote(url string, opts ...any) *stubResource {
	return &stubResource{path: url}
}
func (rn *resourcesNamespace) FromString(name, content string) *stubResource {
	return &stubResource{path: name}
}
func (rn *resourcesNamespace) ExecuteAsTemplate(name string, ctx any, r ...*stubResource) *stubResource {
	if len(r) > 0 {
		return r[0]
	}
	return &stubResource{path: name}
}
func (rn *resourcesNamespace) Concat(name string, resources ...any) *stubResource {
	return &stubResource{path: name}
}
func (rn *resourcesNamespace) Copy(name string, src *stubResource) *stubResource {
	return &stubResource{path: name}
}

// Hugo pipe functions (standalone, for pipeline syntax: {{ $res | toCSS }})
// These are also registered as flat funcMap entries below.

// templatesNamespace implements the `templates` template namespace.
type templatesNamespace struct {
	layoutsDir string
}

func (tn *templatesNamespace) Exists(name string) bool {
	if tn.layoutsDir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(tn.layoutsDir, name))
	return err == nil
}

// pathNamespace implements the `path` template namespace.
type pathNamespace struct{}

func (pn *pathNamespace) Join(elems ...string) string  { return filepath.Join(elems...) }
func (pn *pathNamespace) Base(p string) string         { return filepath.Base(p) }
func (pn *pathNamespace) Dir(p string) string          { return filepath.Dir(p) }
func (pn *pathNamespace) Ext(p string) string          { return filepath.Ext(p) }
func (pn *pathNamespace) Clean(p string) string        { return filepath.Clean(p) }

// reflectNamespace implements the `reflect` template namespace.
type reflectNamespace struct{}

func (rn *reflectNamespace) IsMap(v any) bool    { return reflect.TypeOf(v).Kind() == reflect.Map }
func (rn *reflectNamespace) IsSlice(v any) bool  { return reflect.TypeOf(v).Kind() == reflect.Slice }
func (rn *reflectNamespace) IsString(v any) bool { _, ok := v.(string); return ok }

// osNamespace is a stub for the `os` namespace.
type osNamespace struct{}

func (on *osNamespace) Getenv(key string) string { return "" }
func (on *osNamespace) FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
func (on *osNamespace) ReadFile(path string) (string, error) { return "", nil }
func (on *osNamespace) ReadDir(path string) ([]os.DirEntry, error) { return nil, nil }
func (on *osNamespace) Stat(path string) (os.FileInfo, error) { return nil, nil }

// urlsNamespace is a stub for the `urls` namespace.
type urlsNamespace struct {
	baseURL string
}

func (un *urlsNamespace) Parse(rawurl string) any { return nil }
func (un *urlsNamespace) AbsURL(path string) string {
	base := strings.TrimRight(un.baseURL, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}
func (un *urlsNamespace) RelURL(path string) string {
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}
func (un *urlsNamespace) JoinPath(elems ...string) string { return strings.Join(elems, "/") }

// cryptoNamespace is a stub for the `crypto` namespace.
type cryptoNamespace struct{}

func (cn *cryptoNamespace) MD5(s string) string  { return "" }
func (cn *cryptoNamespace) SHA1(s string) string { return "" }
func (cn *cryptoNamespace) SHA256(s string) string { return "" }
func (cn *cryptoNamespace) FNV32a(s string) string { return "" }
func (cn *cryptoNamespace) HMAC(hash, msg, secret string) string { return "" }

// collectionsNamespace is a stub for the `collections` namespace.
type collectionsNamespace struct{}

func (cn *collectionsNamespace) Append(v any, items ...any) []any {
	result, _ := v.([]any)
	return append(result, items...)
}
func (cn *collectionsNamespace) Uniq(v any) any     { return v }
func (cn *collectionsNamespace) Where(v any, key, val string) any { return v }

// i18nLookup returns the i18n translation for a key, or the key itself if not found.
func (r *Renderer) i18nLookup(key string) string {
	if r.i18nData != nil {
		if v, ok := r.i18nData[key]; ok {
			return v
		}
	}
	// Return key as-is — makes templates readable even without translation files
	return key
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
			// Try both classic (partials/) and newer Hugo convention (_partials/)
			for _, dir := range []string{"partials", "_partials"} {
				partialPath := filepath.Join(r.layoutsDir, dir, name)
				if data, err := os.ReadFile(partialPath); err == nil {
					partialContent = string(data)
					break
				}
			}
		}
		if partialContent == "" {
			// Silently return empty for missing partials (common for optional partials like comments)
			return "", nil
		}

		parsed, err := template.New(name).Funcs(r.funcMap).Parse(partialContent)
		if err != nil {
			return "", fmt.Errorf("parse partial %q: %w", name, err)
		}

		// Register Hugo _internal/ templates into this partial's set.
		for iname, itmpl := range internalTemplates {
			if _, err2 := parsed.New(iname).Parse(itmpl); err2 != nil {
				_ = err2
			}
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

	// Register Hugo _internal/ templates into the set so themes can call
	// {{ template "_internal/opengraph.html" . }} etc.
	for name, tmplContent := range internalTemplates {
		if _, err2 := t.New(name).Parse(tmplContent); err2 != nil {
			// non-fatal: skip broken internal template
			_ = err2
		}
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
					data.Pages = append(data.Pages, &HugoPage{Page: c, Site: r.site})
				}
			}
		case "home":
			for _, p := range allPages {
				if p.Kind == "page" && !p.Draft && !p.Date.IsZero() {
					data.Pages = append(data.Pages, &HugoPage{Page: p, Site: r.site})
				}
			}
			sort.Slice(data.Pages, func(i, j int) bool {
				return data.Pages[i].Page.Date.After(data.Pages[j].Page.Date)
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
		Pages:  wrapPages(pages, r.site),
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
