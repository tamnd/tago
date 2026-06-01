package render

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
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
	AllPages     []*content.Page // all pages (internal; use Pages() method for templates)
	AllRegularPages []*content.Page // kind=page only (internal; use RegularPages() method)

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

// Param looks up a param key in page params, then site params.
func (hp *HugoPage) Param(key string) any {
	if hp.Page != nil && hp.Page.Params != nil {
		if v, ok := hp.Page.Params[key]; ok {
			return v
		}
	}
	if hp.Site != nil && hp.Site.Params != nil {
		if v, ok := hp.Site.Params[key]; ok {
			return v
		}
	}
	return nil
}

// Params returns the page's front matter params map.
func (hp *HugoPage) Params() map[string]any {
	if hp.Page == nil {
		return nil
	}
	return hp.Page.Params
}

// Language returns the site language.
func (hp *HugoPage) Language() *SiteLanguage {
	if hp.Site != nil {
		return hp.Site.Language
	}
	return nil
}

// Data returns page data (for taxonomy pages).
func (hp *HugoPage) Data() *PageData {
	return &PageData{Terms: &TermsData{}}
}

// Scratch returns a scratch pad for the page.
func (hp *HugoPage) Scratch() *Scratch { return newScratch() }

// Store returns a scratch pad (alias for Scratch in newer Hugo).
func (hp *HugoPage) Store() *Scratch { return newScratch() }

// TableOfContents returns empty (stub).
func (hp *HugoPage) TableOfContents() template.HTML { return "" }

// Summary returns the page summary.
func (hp *HugoPage) Summary() string {
	if hp.Page != nil {
		return hp.Page.Summary
	}
	return ""
}

// Description returns the page description.
func (hp *HugoPage) Description() string {
	if hp.Page != nil {
		return hp.Page.Description
	}
	return ""
}

// Content returns the rendered page content.
func (hp *HugoPage) Content() template.HTML {
	if hp.Page != nil {
		return template.HTML(hp.Page.ContentHTML)
	}
	return ""
}

// WordCount returns the word count.
func (hp *HugoPage) WordCount() int {
	if hp.Page != nil {
		return hp.Page.WordCount
	}
	return 0
}

// ReadingTime returns estimated reading time in minutes.
func (hp *HugoPage) ReadingTime() int {
	if hp.Page != nil {
		return hp.Page.ReadingTime
	}
	return 0
}

// Resources returns an empty page resources stub.
func (hp *HugoPage) Resources() *pageResourcesStub { return &pageResourcesStub{} }

// File returns a stub file info object.
func (hp *HugoPage) File() *pageFileStub {
	if hp.Page == nil {
		return &pageFileStub{}
	}
	return &pageFileStub{filePath: hp.Page.FilePath}
}

// Translations returns empty (no multilingual support).
func (hp *HugoPage) Translations() []any { return nil }

// IsTranslated returns false.
func (hp *HugoPage) IsTranslated() bool { return false }

// AllTranslations returns empty.
func (hp *HugoPage) AllTranslations() []any { return nil }

// Next returns next sibling page.
func (hp *HugoPage) Next() *content.Page {
	if hp.Page != nil {
		return hp.Page.NextPage
	}
	return nil
}

// Prev returns previous sibling page.
func (hp *HugoPage) Prev() *content.Page {
	if hp.Page != nil {
		return hp.Page.PrevPage
	}
	return nil
}

// Kind returns the page kind.
func (hp *HugoPage) Kind() string {
	if hp.Page != nil {
		return hp.Page.Kind
	}
	return ""
}

// Type returns the content type.
func (hp *HugoPage) Type() string {
	if hp.Page != nil {
		return hp.Page.Type
	}
	return ""
}

// Section returns the root section.
func (hp *HugoPage) Section() string {
	if hp.Page != nil {
		return hp.Page.Section
	}
	return ""
}

// LinkTitle returns the link title.
func (hp *HugoPage) LinkTitle() string {
	if hp.Page == nil {
		return ""
	}
	if hp.Page.LinkTitle != "" {
		return hp.Page.LinkTitle
	}
	return hp.Page.Title
}

// Draft returns whether the page is a draft.
func (hp *HugoPage) Draft() bool {
	if hp.Page != nil {
		return hp.Page.Draft
	}
	return false
}

// Weight returns the page weight.
func (hp *HugoPage) Weight() int {
	if hp.Page != nil {
		return hp.Page.Weight
	}
	return 0
}

// Path returns the page URL path.
func (hp *HugoPage) Path() string {
	if hp.Page != nil {
		return hp.Page.RelPermalink
	}
	return ""
}

// Lang returns the language code.
func (hp *HugoPage) Lang() string {
	if hp.Page != nil {
		return hp.Page.Lang
	}
	return ""
}

// GitInfo returns nil (stub).
func (hp *HugoPage) GitInfo() any { return nil }

// Ancestors returns ancestor pages.
func (hp *HugoPage) Ancestors() PageList {
	if hp.Page != nil {
		return PageList(hp.Page.Ancestors)
	}
	return nil
}

// Parent returns the parent page.
func (hp *HugoPage) Parent() *content.Page {
	if hp.Page != nil {
		return hp.Page.Parent
	}
	return nil
}

// BundleType returns empty (stub).
func (hp *HugoPage) BundleType() string { return "" }

// OutputFormats returns a stub.
func (hp *HugoPage) OutputFormats() *outputFormatsStub { return &outputFormatsStub{} }

// AlternativeOutputFormats returns a stub.
func (hp *HugoPage) AlternativeOutputFormats() *outputFormatsStub { return &outputFormatsStub{} }

// RegularPages returns an empty list (stub for sections).
func (hp *HugoPage) RegularPages() HugoPageList { return nil }

// Pages returns an empty list (stub for sections in range).
func (hp *HugoPage) Pages() HugoPageList { return nil }


// IsNode returns true if this is a list/section/home page.
func (hp *HugoPage) IsNode() bool {
	if hp.Page != nil {
		return hp.Page.IsNode()
	}
	return false
}

// FuzzyWordCount returns word count rounded to nearest 100.
func (hp *HugoPage) FuzzyWordCount() int {
	if hp.Page != nil {
		return hp.Page.FuzzyWordCount()
	}
	return 0
}

// Truncated returns whether the page content was truncated for summary.
func (hp *HugoPage) Truncated() bool { return false }

// PageData mirrors Hugo's .Data field on taxonomy/term pages (provides .Pages and .Terms).
type PageData struct {
	Pages    HugoPageList
	Terms    *TermsData
	Singular string // taxonomy type singular name, e.g. "tag"
	Plural   string // taxonomy type plural name, e.g. "tags"
}

// TermsData wraps taxonomy terms and provides Hugo-compatible methods.
type TermsData struct {
	terms []TagCount
}

// Alphabetical returns terms sorted alphabetically with Name and Count.
func (td *TermsData) Alphabetical() []TermEntry {
	sorted := make([]TagCount, len(td.terms))
	copy(sorted, td.terms)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})
	entries := make([]TermEntry, len(sorted))
	for i, t := range sorted {
		entries[i] = TermEntry{Name: t.Name, Count: t.Count}
	}
	return entries
}

// ByCount returns terms sorted by count (descending).
func (td *TermsData) ByCount() []TermEntry {
	sorted := make([]TagCount, len(td.terms))
	copy(sorted, td.terms)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Count > sorted[j].Count
	})
	entries := make([]TermEntry, len(sorted))
	for i, t := range sorted {
		entries[i] = TermEntry{Name: t.Name, Count: t.Count}
	}
	return entries
}

// TermEntry is a single taxonomy term with name and count.
type TermEntry struct {
	Name  string
	Count int
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

// Sorting methods — return a sorted copy of the list.
func (pl HugoPageList) ByDate() HugoPageList        { return pl.sortedBy("Date") }
func (pl HugoPageList) ByLastmod() HugoPageList     { return pl.sortedBy("Lastmod") }
func (pl HugoPageList) ByPublishDate() HugoPageList { return pl.sortedBy("PublishDate") }
func (pl HugoPageList) ByTitle() HugoPageList       { return pl.sortedBy("Title") }
func (pl HugoPageList) ByWeight() HugoPageList      { return pl.sortedBy("Weight") }
func (pl HugoPageList) ByLength() HugoPageList      { return pl.sortedBy("WordCount") }

func (pl HugoPageList) sortedBy(key string) HugoPageList {
	out := make(HugoPageList, len(pl))
	copy(out, pl)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a == nil || b == nil {
			return a != nil
		}
		switch key {
		case "Title":
			return a.Page.Title < b.Page.Title
		case "Weight":
			return a.Page.Weight < b.Page.Weight
		case "Lastmod":
			return a.Lastmod().Time.After(b.Lastmod().Time)
		case "PublishDate":
			return a.PublishDate().Time.After(b.PublishDate().Time)
		default:
			return a.Date().Time.After(b.Date().Time)
		}
	})
	return out
}

func (pl HugoPageList) Reverse() HugoPageList {
	out := make(HugoPageList, len(pl))
	for i, p := range pl {
		out[len(pl)-1-i] = p
	}
	return out
}

func (pl HugoPageList) First(n int) HugoPageList {
	if n >= len(pl) {
		return pl
	}
	return pl[:n]
}

func (pl HugoPageList) Last(n int) HugoPageList {
	if n >= len(pl) {
		return pl
	}
	return pl[len(pl)-n:]
}

func (pl HugoPageList) Len() int { return len(pl) }

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
	Locale            string // BCP 47 locale string (e.g. "en-US")
	Direction         string // "ltr" or "rtl"
}

func (l *SiteLanguage) String() string {
	if l == nil {
		return "en"
	}
	return l.Lang
}

// Name returns the language name (alias for LanguageName).
func (l *SiteLanguage) Name() string {
	if l == nil {
		return ""
	}
	if l.LanguageName != "" {
		return l.LanguageName
	}
	return l.Lang
}

// Params returns an empty map (stub for language params).
func (l *SiteLanguage) Params() map[string]any { return nil }

// IsRTL returns true if the language direction is rtl.
func (l *SiteLanguage) IsRTL() bool {
	if l == nil {
		return false
	}
	return l.LanguageDirection == "rtl" || l.Direction == "rtl"
}

// Get returns a language param (stub for Hugo .Site.Language.Get "key").
func (l *SiteLanguage) Get(key string) any {
	return nil
}

// Hugo returns the hugo namespace (allows {{ .Site.Hugo.Version }} in templates).
func (s *SiteData) Hugo() *hugoNamespace { return &hugoNamespace{} }

// Pages returns all site pages as HugoPageList (with sorting methods).
func (s *SiteData) Pages() HugoPageList { return wrapPages(s.AllPages, s) }

// RegularPages returns kind=page pages as HugoPageList (with sorting methods).
func (s *SiteData) RegularPages() HugoPageList { return wrapPages(s.AllRegularPages, s) }

// GetPage returns a stub page for the given path (used by themes to get section/taxonomy pages).
func (s *SiteData) GetPage(args ...string) *siteHomeStub {
	var path string
	if len(args) == 1 {
		path = args[0]
	} else if len(args) >= 2 {
		path = args[1]
	}
	baseURL := s.BaseURL
	_ = path
	return &siteHomeStub{baseURL: baseURL}
}

// Config returns a stub for site config (e.g. site.Config.Services.RSS.Limit).
func (s *SiteData) Config() *siteConfigStub     { return &siteConfigStub{} }
func (s *SiteData) Store() *Scratch             { return newScratch() }
func (s *SiteData) IsMultilingual() bool         { return false }
func (s *SiteData) IsServer() bool               { return false }
func (s *SiteData) BuildDrafts() bool            { return false }
func (s *SiteData) DisablePathToLower() bool     { return false }
func (s *SiteData) EnableRobotsTXT() bool        { return false }
func (s *SiteData) IsMinifyOutput() bool         { return false }


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

// Home returns a fake home page stub for .Site.Home.RelPermalink etc.
func (s *SiteData) Home() *siteHomeStub {
	return &siteHomeStub{baseURL: s.BaseURL}
}

// siteHomeStub implements Hugo's .Site.Home methods.
type siteHomeStub struct{ baseURL string }

func (h *siteHomeStub) RelPermalink() string      { return "/" }
func (h *siteHomeStub) Permalink() string         { return strings.TrimRight(h.baseURL, "/") + "/" }
func (h *siteHomeStub) Title() string             { return "" }
func (h *siteHomeStub) LinkTitle() string         { return "" }
func (h *siteHomeStub) Name() string              { return "" }
func (h *siteHomeStub) Description() string       { return "" }
func (h *siteHomeStub) Summary() string           { return "" }
func (h *siteHomeStub) Content() template.HTML    { return "" }
func (h *siteHomeStub) IsHome() bool              { return true }
func (h *siteHomeStub) IsPage() bool              { return false }
func (h *siteHomeStub) IsSection() bool           { return false }
func (h *siteHomeStub) IsNode() bool              { return true }
func (h *siteHomeStub) Kind() string              { return "home" }
func (h *siteHomeStub) Type() string              { return "page" }
func (h *siteHomeStub) Section() string           { return "" }
func (h *siteHomeStub) Translations() []any       { return nil }
func (h *siteHomeStub) IsTranslated() bool        { return false }
func (h *siteHomeStub) AllTranslations() []any    { return nil }
func (h *siteHomeStub) Language() *SiteLanguage   { return nil }
func (h *siteHomeStub) Lang() string              { return "" }
func (h *siteHomeStub) Params() map[string]any    { return nil }
func (h *siteHomeStub) Param(key string) any      { return nil }
func (h *siteHomeStub) Date() HugoTime            { return HugoTime{} }
func (h *siteHomeStub) Lastmod() HugoTime          { return HugoTime{} }
func (h *siteHomeStub) File() *homeFileStub        { return nil }
func (h *siteHomeStub) Pages() HugoPageList        { return nil }
func (h *siteHomeStub) RegularPages() HugoPageList { return nil }
func (h *siteHomeStub) Sections() HugoPageList     { return nil }
func (h *siteHomeStub) Weight() int                { return 0 }
func (h *siteHomeStub) Draft() bool                { return false }

// homeFileStub represents Hugo's content.File for the home page (virtual, so nil is returned).
type homeFileStub struct{}

// hugoSitesStub supports hugo.Sites.Default.Home.RelPermalink
type hugoSitesStub struct{}

func (s *hugoSitesStub) Default() *hugoSiteDefaultStub { return &hugoSiteDefaultStub{} }

type hugoSiteDefaultStub struct{}

func (s *hugoSiteDefaultStub) Home() *siteHomeStub { return &siteHomeStub{} }

// LastChange returns zero time (stub for .Site.LastChange).
func (s *SiteData) LastChange() time.Time { return time.Time{} }
func (s *SiteData) Lastmod() HugoTime     { return HugoTime{} }

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

// siteConfigStub is a stub for .Site.Config / site.Config.
type siteConfigStub struct{}

func (c *siteConfigStub) Services() *siteServicesStub { return &siteServicesStub{} }

type siteServicesStub struct{}

func (s *siteServicesStub) RSS() *siteRSSStub           { return &siteRSSStub{} }
func (s *siteServicesStub) Disqus() *siteDisqusStub     { return &siteDisqusStub{} }
func (s *siteServicesStub) GoogleAnalytics() *siteGAStub { return &siteGAStub{} }

type siteRSSStub struct{ Limit int }
type siteDisqusStub struct{ Shortname string }
type siteGAStub struct{ ID string }

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

// PageList is a slice of pages with Hugo-compatible methods.
type PageList []*content.Page

func (pl PageList) Reverse() PageList {
	n := len(pl)
	out := make(PageList, n)
	for i, p := range pl {
		out[n-1-i] = p
	}
	return out
}

func (pl PageList) First(n int) PageList {
	if n >= len(pl) {
		return pl
	}
	return pl[:n]
}

func (pl PageList) Last(n int) PageList {
	if n >= len(pl) {
		return pl
	}
	return pl[len(pl)-n:]
}

func (d *TemplateData) Ancestors() PageList {
	if d.Page == nil {
		return nil
	}
	return PageList(d.Page.Ancestors)
}

// RegularPages returns only kind=page children (for section/home templates).
func (d *TemplateData) RegularPages() HugoPageList {
	var out HugoPageList
	for _, p := range d.Pages {
		if p.Page != nil && p.Page.Kind == "page" {
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

// OutputFormats returns a stub output formats object.
func (d *TemplateData) OutputFormats() *outputFormatsStub {
	return &outputFormatsStub{}
}

// outputFormatsStub implements Hugo's .OutputFormats methods.
type outputFormatsStub struct{}

func (o *outputFormatsStub) Get(name string) *stubResource { return nil }

// Scratch returns a scratch pad for Hugo's .Scratch.
func (d *TemplateData) Scratch() *Scratch { return newScratch() }

// Store returns a scratch pad (Hugo 0.117+: .Store).
func (d *TemplateData) Store() *Scratch { return newScratch() }

// Language returns the site language object.
func (d *TemplateData) Language() *SiteLanguage {
	if d.Site != nil {
		return d.Site.Language
	}
	return nil
}

// Data returns Hugo-compatible .Data field (for taxonomy/term pages).
func (d *TemplateData) Data() *PageData {
	section := ""
	if d.Page != nil {
		section = d.Page.Section
	}
	// Derive singular from plural (naive: strip trailing 's').
	singular := section
	if len(singular) > 0 && singular[len(singular)-1] == 's' {
		singular = singular[:len(singular)-1]
	}
	return &PageData{
		Pages:    d.Pages,
		Terms:    &TermsData{terms: d.Tags},
		Singular: singular,
		Plural:   section,
	}
}

// GitInfo returns nil (stub for Hugo .GitInfo; nil makes {{- if .GitInfo }} false).
func (d *TemplateData) GitInfo() any {
	return nil
}

// Resources returns an empty page resources stub (stub for Hugo .Resources).
func (d *TemplateData) Resources() *pageResourcesStub {
	return &pageResourcesStub{}
}

// IsTranslated returns false (stub for multilingual support).
func (d *TemplateData) IsTranslated() bool { return false }

// Translations returns empty slice (stub).
func (d *TemplateData) Translations() []any { return nil }

// AllTranslations returns empty slice (stub).
func (d *TemplateData) AllTranslations() []any { return nil }

// TranslationKey returns empty string (stub).
func (d *TemplateData) TranslationKey() string { return "" }


// BundleType returns empty string (stub).
func (d *TemplateData) BundleType() string { return "" }

// CurrentSection returns the current page for section context.
func (d *TemplateData) CurrentSection() *TemplateData { return d }

// pageResourcesStub implements Hugo page resources methods.
// All methods return empty/nil stubs — tago does not process page bundle resources.
type pageResourcesStub struct{}

func (r *pageResourcesStub) ByType(t string) *pageResourcesStub          { return r }
func (r *pageResourcesStub) Match(pattern string) *pageResourcesStub      { return r }
func (r *pageResourcesStub) Get(name string) *stubResource                { return nil }
func (r *pageResourcesStub) GetMatch(pattern string) *stubResource        { return nil }
func (r *pageResourcesStub) GetRemote(url string, opts ...any) *stubResource { return nil }
func (r *pageResourcesStub) Len() int                                     { return 0 }

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

// Paginate returns a stub Paginator for the given collection.
func (d *TemplateData) Paginate(pages any, args ...any) *paginatorStub {
	return &paginatorStub{pages: d.Pages}
}

// Paginator returns a stub Paginator object.
func (d *TemplateData) Paginator(args ...any) *paginatorStub {
	return &paginatorStub{pages: d.Pages}
}

// paginatorStub provides Hugo-compatible Paginator interface.
type paginatorStub struct {
	pages HugoPageList
}

func (p *paginatorStub) Pages() HugoPageList      { return p.pages }
func (p *paginatorStub) PageNumber() int           { return 1 }
func (p *paginatorStub) TotalPages() int           { return 1 }
func (p *paginatorStub) TotalNumberOfElements() int { return len(p.pages) }
func (p *paginatorStub) NumberOfElements() int     { return len(p.pages) }
func (p *paginatorStub) HasNext() bool             { return false }
func (p *paginatorStub) HasPrev() bool             { return false }
func (p *paginatorStub) Next() *paginatorStub      { return nil }
func (p *paginatorStub) Prev() *paginatorStub      { return nil }
func (p *paginatorStub) First() *paginatorStub     { return p }
func (p *paginatorStub) Last() *paginatorStub      { return p }
func (p *paginatorStub) Pagers() []*paginatorStub  { return []*paginatorStub{p} }
func (p *paginatorStub) URL() string               { return "" }
func (p *paginatorStub) RelPermalink() string      { return "" }
func (p *paginatorStub) Permalink() string         { return "" }
func (p *paginatorStub) PageSize() int             { return 10 }

// File returns a stub file info object (stub for Hugo .File).
func (d *TemplateData) File() *pageFileStub {
	if d.Page == nil {
		return &pageFileStub{}
	}
	return &pageFileStub{filePath: d.Page.FilePath}
}

// pageFileStub implements Hugo's .File methods.
type pageFileStub struct {
	filePath string
}

func (f *pageFileStub) Path() string        { return f.filePath }
func (f *pageFileStub) LogicalName() string { return filepath.Base(f.filePath) }
func (f *pageFileStub) Dir() string         { return filepath.Dir(f.filePath) }
func (f *pageFileStub) Ext() string         { return filepath.Ext(f.filePath) }
func (f *pageFileStub) Filename() string    { return f.filePath }
func (f *pageFileStub) ContentBaseName() string {
	base := filepath.Base(f.filePath)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}
func (f *pageFileStub) TranslationBaseName() string { return f.ContentBaseName() }
func (f *pageFileStub) BaseFileName() string        { return f.ContentBaseName() }
func (f *pageFileStub) UniqueID() string            { return "" }

// Truncated returns false (stub for Hugo .Truncated; true means Summary != full content).
func (d *TemplateData) Truncated() bool {
	if d.Page == nil {
		return false
	}
	return len(d.Page.Summary) > 0 && d.Page.Summary != d.Page.ContentPlain
}

func (d *TemplateData) Sections() HugoPageList        { return nil }
func (d *TemplateData) GetTerms(taxonomy string) []any { return nil }
func (d *TemplateData) Name() string {
	if d.Page != nil {
		if d.Page.Title != "" {
			return d.Page.Title
		}
		return filepath.Base(d.Page.RelPermalink)
	}
	return ""
}
func (d *TemplateData) RegularPagesRecursive() HugoPageList {
	if d.Pages != nil {
		return d.Pages
	}
	return nil
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
func plainify(v any) string {
	s := fmt.Sprintf("%v", v)
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
func sortPages(collection any, key ...string) any {
	sortKey := "Date"
	if len(key) > 0 && key[0] != "" {
		sortKey = key[0]
	}

	// Handle []*content.Page directly for performance.
	if pages, ok := collection.([]*content.Page); ok {
		result := make([]*content.Page, len(pages))
		copy(result, pages)
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
			default:
				return a.Date.After(b.Date)
			}
		})
		return result
	}

	// Generic: reflect-based sort for HugoPageList, []any, etc.
	rv := reflect.ValueOf(collection)
	if !rv.IsValid() || (rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array) {
		return collection
	}
	n := rv.Len()
	result := make([]any, n)
	for i := 0; i < n; i++ {
		result[i] = rv.Index(i).Interface()
	}
	sort.SliceStable(result, func(i, j int) bool {
		a := getNestedField(result[i], sortKey)
		b := getNestedField(result[j], sortKey)
		return compareValues(a, b) > 0
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
// isTruthy mirrors Go template truthiness: nil, false, 0, "", empty slices/maps are falsy.
func isTruthy(v any) bool {
	if v == nil {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	case int:
		return t != 0
	case int64:
		return t != 0
	case float64:
		return t != 0
	case string:
		return t != ""
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return rv.Len() > 0
	case reflect.Ptr, reflect.Interface:
		return !rv.IsNil()
	}
	return true
}

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
		"delimit": func(v any, delim string, last ...string) string {
			cn := &collectionsNamespace{}
			return cn.Delimit(v, delim, last...)
		},
		"anchorize": func(s string) string {
			return strings.ToLower(regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "-"))
		},
		"htmlEscape": func(v any) string {
			tn := &transformNamespace{}
			return tn.HTMLEscape(v)
		},
		"htmlUnescape": func(v any) template.HTML {
			return template.HTML(fmt.Sprintf("%v", v))
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
		"hasPrefix": func(s, prefix any) bool {
			return strings.HasPrefix(fmt.Sprintf("%v", s), fmt.Sprintf("%v", prefix))
		},
		"hasSuffix": func(s, suffix any) bool {
			return strings.HasSuffix(fmt.Sprintf("%v", s), fmt.Sprintf("%v", suffix))
		},

		// ---- new Hugo-compatible functions ----

		// Page/collection filters
		"where": func(args ...any) any {
			cn := &collectionsNamespace{}
			return cn.Where(args...)
		},
		"first": func(n int, collection any) any {
			if pages, ok := collection.([]*content.Page); ok {
				if n >= len(pages) {
					return pages
				}
				return pages[:n]
			}
			rv := reflect.ValueOf(collection)
			if !rv.IsValid() || (rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array) {
				return collection
			}
			if n >= rv.Len() {
				return collection
			}
			return rv.Slice(0, n).Interface()
		},
		"last": func(n int, collection any) any {
			if pages, ok := collection.([]*content.Page); ok {
				if n >= len(pages) {
					return pages
				}
				return pages[len(pages)-n:]
			}
			rv := reflect.ValueOf(collection)
			if !rv.IsValid() || (rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array) {
				return collection
			}
			if n >= rv.Len() {
				return collection
			}
			return rv.Slice(rv.Len()-n, rv.Len()).Interface()
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
		"emojify": func(s any) string {
			if s == nil {
				return ""
			}
			return fmt.Sprintf("%v", s)
		},
		"humanize":   humanize,
		"title":      titleCase,
		"lower":      strings.ToLower,
		"upper":      strings.ToUpper,
		"trim":       func(v any, cutset string) string { return strings.Trim(fmt.Sprintf("%v", v), cutset) },
		"trimLeft":   func(v any, cutset string) string { return strings.TrimLeft(fmt.Sprintf("%v", v), cutset) },
		"trimRight":  func(v any, cutset string) string { return strings.TrimRight(fmt.Sprintf("%v", v), cutset) },
		"trimPrefix": func(v any, prefix string) string { return strings.TrimPrefix(fmt.Sprintf("%v", v), prefix) },
		"trimSuffix": func(v any, suffix string) string { return strings.TrimSuffix(fmt.Sprintf("%v", v), suffix) },
		"replace": func(s, old, newStr string) string {
			return strings.ReplaceAll(s, old, newStr)
		},
		"findRE": func(pattern string, v any, args ...any) []string {
			s := fmt.Sprintf("%v", v)
			re, err := regexp.Compile(pattern)
			if err != nil {
				return nil
			}
			n := -1
			if len(args) > 0 {
				switch lv := args[0].(type) {
				case int:
					n = lv
				}
			}
			matches := re.FindAllString(s, n)
			if matches == nil {
				return []string{}
			}
			return matches
		},
		"findRESubmatch": func(pattern string, v any, args ...any) [][]string {
			s := fmt.Sprintf("%v", v)
			re, err := regexp.Compile(pattern)
			if err != nil {
				return nil
			}
			n := -1
			if len(args) > 0 {
				if lv, ok := args[0].(int); ok {
					n = lv
				}
			}
			matches := re.FindAllStringSubmatch(s, n)
			if matches == nil {
				return [][]string{}
			}
			return matches
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
		"substr": func(s any, start any, args ...any) string {
			str := fmt.Sprintf("%v", s)
			runes := []rune(str)
			st, _ := start.(int)
			if st < 0 {
				st = max(0, len(runes)+st)
			}
			if st > len(runes) {
				return ""
			}
			if len(args) > 0 {
				length, _ := args[0].(int)
				end := st + length
				if end > len(runes) {
					end = len(runes)
				}
				return string(runes[st:end])
			}
			return string(runes[st:])
		},
		"fileExists": func(path string) bool {
			_, err := os.Stat(path)
			return err == nil
		},
		"os": func() *osNamespace { return &osNamespace{} },
		"readFile": func(path string) string {
			data, err := os.ReadFile(path)
			if err != nil {
				return ""
			}
			return string(data)
		},
		"getenv": os.Getenv,

		// strings.* namespace (dot-notation names)
		"stringsContains":   strings.Contains,
		"stringsHasPrefix":  strings.HasPrefix,
		"stringsHasSuffix":  strings.HasSuffix,
		"stringsTrimPrefix": strings.TrimPrefix,
		"stringsTrimSuffix": strings.TrimSuffix,

		// printf alias
		"printf": fmt.Sprintf,

		// URL functions
		"absURL": func(path any) string {
			if path == nil {
				return ""
			}
			p := fmt.Sprintf("%v", path)
			base := strings.TrimRight(r.site.BaseURL, "/")
			if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
				return p
			}
			if strings.HasPrefix(p, "/") {
				return base + p
			}
			return base + "/" + p
		},
		"relURL": func(path any) string {
			if path == nil {
				return ""
			}
			p := fmt.Sprintf("%v", path)
			if strings.HasPrefix(p, "/") {
				return p
			}
			return "/" + p
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
		"time": func() *timeNamespace { return &timeNamespace{} },

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
		"default": func(args ...any) any {
			if len(args) == 0 {
				return nil
			}
			if len(args) == 1 {
				return args[0]
			}
			dflt, val := args[0], args[1]
			if isZero(val) {
				return dflt
			}
			return val
		},
		"in":        inCollection,
		// Override built-in comparison functions to support type coercion (e.g., int vs string).
		"lt": func(a, b any) bool { return compareValues(a, b) < 0 },
		"le": func(a, b any) bool { return compareValues(a, b) <= 0 },
		"gt": func(a, b any) bool { return compareValues(a, b) > 0 },
		"ge": func(a, b any) bool { return compareValues(a, b) >= 0 },
		"eq": func(a any, b ...any) bool {
			for _, v := range b {
				if reflect.DeepEqual(a, v) || compareValues(a, v) == 0 {
					return true
				}
			}
			return false
		},
		"ne": func(a, b any) bool { return !reflect.DeepEqual(a, b) && compareValues(a, b) != 0 },
		"merge": func(args ...any) map[string]any {
			cn := &collectionsNamespace{}
			if len(args) == 2 {
				return cn.Merge(args[0], args[1])
			}
			return map[string]any{}
		},
		// apply maps a function over a collection. Stub returns empty slice.
		"apply": func(collection any, fn string, args ...any) []any {
			rv := reflect.ValueOf(collection)
			if !rv.IsValid() || (rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array) {
				return nil
			}
			result := make([]any, rv.Len())
			for i := 0; i < rv.Len(); i++ {
				result[i] = rv.Index(i).Interface()
			}
			return result
		},
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
		"append": func(v any, args ...any) []any {
			return hugoAppend(v, args...)
		},
		"slice": func(items ...any) []any {
			return items
		},
		"index": indexInto,
		"isset": func(obj any, key string) bool {
			if obj == nil {
				return false
			}
			rv := reflect.ValueOf(obj)
			for rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface {
				if rv.IsNil() {
					return false
				}
				rv = rv.Elem()
			}
			if rv.Kind() == reflect.Map {
				return rv.MapIndex(reflect.ValueOf(key)).IsValid()
			}
			if rv.Kind() == reflect.Struct {
				return rv.FieldByName(key).IsValid()
			}
			return false
		},
		"seq": func(n int) []int {
			result := make([]int, n)
			for i := range result {
				result[i] = i + 1
			}
			return result
		},
		"cond": func(args ...any) any {
			if len(args) != 3 {
				return nil
			}
			if isTruthy(args[0]) {
				return args[1]
			}
			return args[2]
		},

		// Encoding
		"jsonify": func(args ...any) string {
			if len(args) == 0 {
				return "null"
			}
			var v any
			var indent string
			if len(args) == 1 {
				v = args[0]
			} else {
				// 2-arg form: jsonify options data
				v = args[len(args)-1]
				if opts, ok := args[0].(map[string]any); ok {
					if ind, ok := opts["indent"].(string); ok {
						indent = ind
					}
				}
			}
			var b []byte
			var err error
			if indent != "" {
				b, err = json.MarshalIndent(v, "", indent)
			} else {
				b, err = json.Marshal(v)
			}
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
	fm["partial"] = func(name string, args ...any) (any, error) {
		var ctx any
		if len(args) > 0 {
			ctx = args[0]
		}
		return r.renderPartialAny(name, ctx)
	}

	// partialCached: like partial but caches by key (args[0] is ctx, rest are cache keys)
	fm["partialCached"] = func(name string, args ...any) (any, error) {
		var ctx any
		if len(args) > 0 {
			ctx = args[0]
		}
		return r.renderPartialAny(name, ctx)
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

	// newScratch creates a new Scratch object (Hugo 0.43+ global scratch)
	fm["newScratch"] = func() *Scratch { return newScratch() }

	// complement returns items in the last collection not in any preceding collections.
	// Pipeline-aware: `$pages | complement $subsections` = complement($subsections, $pages)
	fm["complement"] = func(args ...any) any {
		if len(args) == 0 {
			return nil
		}
		// Last arg is the input collection; all others are exclusion sets.
		input := args[len(args)-1]
		exclusions := args[:len(args)-1]
		exclude := make(map[any]bool)
		for _, excl := range exclusions {
			rv := reflect.ValueOf(excl)
			if rv.IsValid() && (rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array) {
				for i := 0; i < rv.Len(); i++ {
					exclude[rv.Index(i).Interface()] = true
				}
			}
		}
		ir := reflect.ValueOf(input)
		if !ir.IsValid() || (ir.Kind() != reflect.Slice && ir.Kind() != reflect.Array) {
			return nil
		}
		// Preserve the input slice type.
		result := reflect.MakeSlice(ir.Type(), 0, ir.Len())
		for i := 0; i < ir.Len(); i++ {
			item := ir.Index(i).Interface()
			if !exclude[item] {
				result = reflect.Append(result, ir.Index(i))
			}
		}
		return result.Interface()
	}

	// try executes a function and recovers from errors (Hugo 0.117+).
	// Returns a map with "err" key (empty string on success) and "value".
	fm["try"] = func(fn any, args ...any) map[string]any {
		result := map[string]any{"err": "", "value": nil}
		func() {
			defer func() {
				if r := recover(); r != nil {
					result["err"] = fmt.Sprintf("%v", r)
				}
			}()
			rfn := reflect.ValueOf(fn)
			if !rfn.IsValid() || rfn.Kind() != reflect.Func {
				return
			}
			rargs := make([]reflect.Value, len(args))
			for i, a := range args {
				rargs[i] = reflect.ValueOf(a)
			}
			res := rfn.Call(rargs)
			if len(res) > 0 {
				result["value"] = res[0].Interface()
			}
		}()
		return result
	}

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

	// md5 as top-level function (Book theme uses it directly)
	fm["md5"] = func(v any) string {
		s := fmt.Sprintf("%v", v)
		return fmt.Sprintf("%x", md5.Sum([]byte(s)))
	}

	// collections namespace
	fm["collections"] = func() *collectionsNamespace { return &collectionsNamespace{} }

	// compare namespace
	fm["compare"] = func() *compareNamespace { return &compareNamespace{} }

	// partials namespace (partials.Include as alternative to partial function)
	fm["partials"] = func() *partialsNamespace { return &partialsNamespace{r: r} }

	// transform namespace
	fm["transform"] = func() *transformNamespace { return &transformNamespace{} }

	// math namespace (math.Add, math.Ceil, etc.)
	fm["math"] = func() *mathNamespace { return &mathNamespace{} }

	// fmt namespace (fmt.Printf, fmt.Print, fmt.Warnf etc.)
	fm["fmt"] = func() *fmtNamespace { return &fmtNamespace{} }

	// lang namespace (lang.Translate, lang.FormatNumber etc.)
	fm["lang"] = func() *langNamespace { return &langNamespace{r: r} }

	// return: Hugo 0.117+ partial return value (stub - no-op in standard Go templates)
	fm["return"] = func(v ...any) string {
		var val any
		if len(v) > 0 {
			val = v[0]
		}
		// Store the return value keyed by goroutine ID so renderPartialAny can retrieve it.
		// Then panic so template execution stops; the template engine catches the panic
		// and returns it as an error, which renderPartialAny detects and suppresses.
		partialReturnStore.Store(goroutineID(), val)
		panic(partialReturn{value: val})
	}

	// strings namespace (Hugo 0.XX+)
	fm["strings"] = func() *stringsNamespace { return &stringsNamespace{} }

	// safe namespace (safe.HTML, safe.CSS, safe.URL etc.)
	fm["safe"] = func() *safeNamespace { return &safeNamespace{} }

	// css namespace (css.Build, css.Sass etc.)
	fm["css"] = func() *cssNamespace { return &cssNamespace{} }

	// js namespace (js.Build etc.)
	fm["js"] = func() *jsNamespace { return &jsNamespace{} }

	// images namespace (images.Filter, images.Resize etc.)
	fm["images"] = func() *imagesNamespace { return &imagesNamespace{} }

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
func (h *hugoNamespace) BuildDate() string      { return "" }
func (h *hugoNamespace) CommitHash() string     { return "" }
func (h *hugoNamespace) Environment() string    { return "development" }
func (h *hugoNamespace) IsMultilingual() bool   { return false }
func (h *hugoNamespace) IsExtended() bool       { return false }
func (h *hugoNamespace) WorkingDir() string     { return "" }
func (h *hugoNamespace) Deps() map[string]any   { return nil }
func (h *hugoNamespace) Sites() *hugoSitesStub  { return &hugoSitesStub{} }
func (h *hugoNamespace) Data() map[string]any   { return map[string]any{} }

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
// Image processing (returns self — no actual image transform in tago)
func (s *stubResource) Resize(spec string) *stubResource         { return s }
func (s *stubResource) Fit(spec string) *stubResource            { return s }
func (s *stubResource) Fill(spec string) *stubResource           { return s }
func (s *stubResource) Crop(spec string) *stubResource           { return s }
func (s *stubResource) Filter(filters ...any) *stubResource      { return s }
func (s *stubResource) Width() int                               { return 0 }
func (s *stubResource) Height() int                              { return 0 }
func (s *stubResource) Exif() map[string]any                     { return nil }
func (s *stubResource) Colors() []string                         { return nil }
func (s *stubResource) Process(spec string) *stubResource        { return s }

type stubMediaType struct{ mime string }
func (m *stubMediaType) Type() string     { return m.mimeStr() }
func (m *stubMediaType) String() string   { return m.mimeStr() }
func (m *stubMediaType) SubType() string  {
	s := m.mimeStr()
	if idx := strings.Index(s, "/"); idx >= 0 {
		return s[idx+1:]
	}
	return s
}
func (m *stubMediaType) MainType() string {
	s := m.mimeStr()
	if idx := strings.Index(s, "/"); idx >= 0 {
		return s[:idx]
	}
	return s
}
func (m *stubMediaType) Suffix() string { return "" }
func (m *stubMediaType) mimeStr() string {
	if m.mime != "" {
		return m.mime
	}
	return "text/html"
}

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
func (rn *resourcesNamespace) Match(pattern string) []*stubResource {
	return nil
}
func (rn *resourcesNamespace) GetMatch(pattern string) *stubResource {
	return &stubResource{path: pattern}
}
func (rn *resourcesNamespace) Minify(r ...any) *stubResource {
	if len(r) > 0 {
		if sr, ok := r[0].(*stubResource); ok {
			return sr
		}
	}
	return &stubResource{}
}
func (rn *resourcesNamespace) Fingerprint(r ...any) *stubResource {
	if len(r) > 0 {
		if sr, ok := r[0].(*stubResource); ok {
			return sr
		}
	}
	return &stubResource{}
}
func (rn *resourcesNamespace) ToCSS(args ...any) *stubResource {
	for _, a := range args {
		if sr, ok := a.(*stubResource); ok {
			return sr
		}
	}
	return &stubResource{}
}
func (rn *resourcesNamespace) PostCSS(args ...any) *stubResource {
	for _, a := range args {
		if sr, ok := a.(*stubResource); ok {
			return sr
		}
	}
	return &stubResource{}
}
func (rn *resourcesNamespace) Babel(args ...any) *stubResource {
	for _, a := range args {
		if sr, ok := a.(*stubResource); ok {
			return sr
		}
	}
	return &stubResource{}
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

// Defer is a stub for Hugo's templates.Defer feature (Hugo 0.134+).
func (tn *templatesNamespace) Defer(args ...any) string { return "" }

// pathNamespace implements the `path` template namespace.
// timeNamespace implements Hugo's `time` template namespace.
type timeNamespace struct{}

// Format formats a time value using the given layout. Supports pipeline: .Date | time.Format "layout".
func (tn *timeNamespace) Format(layout any, t ...any) string {
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
	var tv time.Time
	if len(t) > 0 {
		switch x := t[0].(type) {
		case time.Time:
			tv = x
		case HugoTime:
			tv = x.Time
		}
	}
	return tv.Format(l)
}

func (tn *timeNamespace) Now() time.Time  { return time.Now() }
func (tn *timeNamespace) Unix(sec, ns int64) time.Time {
	return time.Unix(sec, ns)
}
func (tn *timeNamespace) AsTime(v any, args ...any) time.Time {
	switch x := v.(type) {
	case time.Time:
		return x
	case HugoTime:
		return x.Time
	case string:
		t, err := time.Parse(time.RFC3339, x)
		if err == nil {
			return t
		}
	}
	return time.Time{}
}

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

// Scratch implements Hugo's .Scratch / site.Store / .Page.Store mutable scratch pad.
type Scratch struct {
	mu   sync.Mutex
	data map[string]any
}

func newScratch() *Scratch { return &Scratch{data: make(map[string]any)} }

func (s *Scratch) Set(key string, val any) string {
	s.mu.Lock()
	s.data[key] = val
	s.mu.Unlock()
	return ""
}

func (s *Scratch) Get(key string) any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data[key]
}

func (s *Scratch) Add(key string, val any) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.data[key]; ok {
		switch e := existing.(type) {
		case int:
			if v, ok2 := val.(int); ok2 {
				s.data[key] = e + v
				return ""
			}
		case float64:
			if v, ok2 := val.(float64); ok2 {
				s.data[key] = e + v
				return ""
			}
		case string:
			if v, ok2 := val.(string); ok2 {
				s.data[key] = e + v
				return ""
			}
		}
	}
	s.data[key] = val
	return ""
}

func (s *Scratch) Delete(key string) string {
	s.mu.Lock()
	delete(s.data, key)
	s.mu.Unlock()
	return ""
}

func (s *Scratch) SetInMap(key, mapKey string, val any) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.data[key].(map[string]any); ok {
		m[mapKey] = val
	} else {
		s.data[key] = map[string]any{mapKey: val}
	}
	return ""
}

func (s *Scratch) GetSortedMapValues(key string) []any {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.data[key].(map[string]any)
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]any, len(keys))
	for i, k := range keys {
		out[i] = m[k]
	}
	return out
}

func (s *Scratch) Values() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]any, len(s.data))
	for k, v := range s.data {
		out[k] = v
	}
	return out
}

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

func (cn *cryptoNamespace) MD5(s string) string  { return fmt.Sprintf("%x", md5.Sum([]byte(s))) }
func (cn *cryptoNamespace) SHA1(s string) string { return "" }
func (cn *cryptoNamespace) SHA256(s string) string { return "" }
func (cn *cryptoNamespace) FNV32a(s string) string { return "" }
func (cn *cryptoNamespace) HMAC(hash, msg, secret string) string { return "" }

// collectionsNamespace implements Hugo's `collections` namespace.
type collectionsNamespace struct{}

func (cn *collectionsNamespace) Append(v any, items ...any) []any {
	return hugoAppend(v, items...)
}

// hugoAppend mirrors Hugo's append behavior:
// When used in a pipeline ($slice | append $item), Go templates call append($item, $slice).
// So if v is not a slice but the last arg is, we treat the last arg as the base collection.
func hugoAppend(v any, args ...any) []any {
	rv := reflect.ValueOf(v)
	vIsSlice := rv.IsValid() && (rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array)

	var base reflect.Value
	var newItems []any

	if vIsSlice {
		base = rv
		newItems = args
	} else if len(args) > 0 {
		// pipeline case: last arg is the existing slice, v + preceding args are items to add
		lastArg := args[len(args)-1]
		rl := reflect.ValueOf(lastArg)
		if rl.IsValid() && (rl.Kind() == reflect.Slice || rl.Kind() == reflect.Array) {
			base = rl
			newItems = append([]any{v}, args[:len(args)-1]...)
		} else {
			// neither is a slice — just collect all as items
			newItems = append([]any{v}, args...)
		}
	} else {
		newItems = []any{v}
	}

	var result []any
	if base.IsValid() {
		for i := 0; i < base.Len(); i++ {
			result = append(result, base.Index(i).Interface())
		}
	}
	return append(result, newItems...)
}

func (cn *collectionsNamespace) Uniq(v any) any { return v }

// Where filters a collection. Supports both 3-arg (collection, key, value)
// and 4-arg (collection, key, operator, value) forms.
func (cn *collectionsNamespace) Where(args ...any) any {
	if len(args) < 3 {
		return nil
	}
	collection := args[0]
	key, _ := args[1].(string)
	var op, valAny any
	if len(args) == 3 {
		op = "="
		valAny = args[2]
	} else {
		op = args[2]
		valAny = args[3]
	}
	opStr, _ := op.(string)
	rv := reflect.ValueOf(collection)
	if !rv.IsValid() {
		return nil
	}
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return collection
	}
	// Build result preserving the input slice type so downstream functions receive the same type.
	result := reflect.MakeSlice(rv.Type(), 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		item := rv.Index(i).Interface()
		itemVal := getNestedField(item, key)
		if matchOp(itemVal, opStr, valAny) {
			result = reflect.Append(result, rv.Index(i))
		}
	}
	return result.Interface()
}

// getNestedField retrieves a field value from an item by dot-separated key path.
func getNestedField(item any, key string) any {
	if item == nil || key == "" {
		return nil
	}
	parts := strings.SplitN(key, ".", 2)
	rv := reflect.ValueOf(item)
	for rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	var val reflect.Value
	switch rv.Kind() {
	case reflect.Struct:
		val = rv.FieldByName(parts[0])
		if !val.IsValid() {
			// try method
			m := reflect.ValueOf(item).MethodByName(parts[0])
			if m.IsValid() {
				res := m.Call(nil)
				if len(res) > 0 {
					val = res[0]
				}
			}
		}
	case reflect.Map:
		val = rv.MapIndex(reflect.ValueOf(parts[0]))
	default:
		return nil
	}
	if !val.IsValid() {
		return nil
	}
	if len(parts) == 2 {
		return getNestedField(val.Interface(), parts[1])
	}
	return val.Interface()
}

// matchOp compares itemVal to expected using the given operator.
func matchOp(itemVal any, op string, expected any) bool {
	switch op {
	case "!=", "ne":
		return !reflect.DeepEqual(itemVal, expected)
	case ">", "gt":
		return compareValues(itemVal, expected) > 0
	case ">=", "ge":
		return compareValues(itemVal, expected) >= 0
	case "<", "lt":
		return compareValues(itemVal, expected) < 0
	case "<=", "le":
		return compareValues(itemVal, expected) <= 0
	case "in":
		return inCollection(expected, itemVal)
	case "not in":
		return !inCollection(expected, itemVal)
	default: // "=", "eq", ""
		return reflect.DeepEqual(itemVal, expected)
	}
}

// compareValues returns -1, 0, or 1 for numeric/string comparisons.
func compareValues(a, b any) int {
	af := toFloat64(a)
	bf := toFloat64(b)
	if af < bf {
		return -1
	} else if af > bf {
		return 1
	}
	return 0
}

func toFloat64(v any) float64 {
	switch x := v.(type) {
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case float64:
		return x
	case float32:
		return float64(x)
	}
	return 0
}

func (cn *collectionsNamespace) Slice(items ...any) []any { return items }

func (cn *collectionsNamespace) Dictionary(pairs ...any) map[string]any {
	m := make(map[string]any)
	for i := 0; i+1 < len(pairs); i += 2 {
		if k, ok := pairs[i].(string); ok {
			m[k] = pairs[i+1]
		}
	}
	return m
}

func (cn *collectionsNamespace) Merge(base, overlay any) map[string]any {
	m := make(map[string]any)
	if bm, ok := base.(map[string]any); ok {
		for k, v := range bm {
			m[k] = v
		}
	}
	if om, ok := overlay.(map[string]any); ok {
		for k, v := range om {
			m[k] = v
		}
	}
	return m
}

func (cn *collectionsNamespace) In(collection, val any) bool {
	return inCollection(collection, val)
}

func (cn *collectionsNamespace) Index(v any, indices ...any) any {
	if len(indices) == 0 {
		return v
	}
	rv := reflect.ValueOf(v)
	for _, idx := range indices {
		switch rv.Kind() {
		case reflect.Map:
			rv = rv.MapIndex(reflect.ValueOf(idx))
		case reflect.Slice, reflect.Array:
			if i, ok := idx.(int); ok && i >= 0 && i < rv.Len() {
				rv = rv.Index(i)
			} else {
				return nil
			}
		default:
			return nil
		}
		if !rv.IsValid() {
			return nil
		}
	}
	return rv.Interface()
}

func (cn *collectionsNamespace) First(n int, v any) any {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return v
	}
	if n > rv.Len() {
		n = rv.Len()
	}
	out := make([]any, n)
	for i := 0; i < n; i++ {
		out[i] = rv.Index(i).Interface()
	}
	return out
}

func (cn *collectionsNamespace) IsSet(obj any, key string) bool {
	if obj == nil {
		return false
	}
	rv := reflect.ValueOf(obj)
	switch rv.Kind() {
	case reflect.Map:
		return rv.MapIndex(reflect.ValueOf(key)).IsValid()
	case reflect.Slice, reflect.Array:
		i := 0
		fmt.Sscanf(key, "%d", &i)
		return i >= 0 && i < rv.Len()
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return false
		}
		return cn.IsSet(rv.Elem().Interface(), key)
	}
	return false
}

func (cn *collectionsNamespace) Delimit(v any, delim string, last ...string) string {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return fmt.Sprintf("%v", v)
	}
	parts := make([]string, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		parts[i] = fmt.Sprintf("%v", rv.Index(i).Interface())
	}
	if len(last) > 0 && len(parts) > 1 {
		return strings.Join(parts[:len(parts)-1], delim) + last[0] + parts[len(parts)-1]
	}
	return strings.Join(parts, delim)
}

// mathNamespace implements Hugo's `math` namespace.
type mathNamespace struct{}

func (mn *mathNamespace) Add(a, b any) any {
	fa, oka := toFloat(a)
	fb, okb := toFloat(b)
	if oka && okb {
		result := fa + fb
		if result == float64(int64(result)) {
			return int(result)
		}
		return result
	}
	return 0
}

func (mn *mathNamespace) Sub(a, b any) any {
	fa, oka := toFloat(a)
	fb, okb := toFloat(b)
	if oka && okb {
		result := fa - fb
		if result == float64(int64(result)) {
			return int(result)
		}
		return result
	}
	return 0
}

func (mn *mathNamespace) Mul(a, b any) any {
	fa, oka := toFloat(a)
	fb, okb := toFloat(b)
	if oka && okb {
		result := fa * fb
		if result == float64(int64(result)) {
			return int(result)
		}
		return result
	}
	return 0
}

func (mn *mathNamespace) Div(a, b any) any {
	fa, oka := toFloat(a)
	fb, okb := toFloat(b)
	if oka && okb && fb != 0 {
		result := fa / fb
		if result == float64(int64(result)) {
			return int(result)
		}
		return result
	}
	return 0
}

func (mn *mathNamespace) Mod(a, b any) int {
	fa, oka := toFloat(a)
	fb, okb := toFloat(b)
	if oka && okb && fb != 0 {
		return int(fa) % int(fb)
	}
	return 0
}

func (mn *mathNamespace) Ceil(v any) float64 {
	f, ok := toFloat(v)
	if !ok {
		return 0
	}
	return math.Ceil(f)
}

func (mn *mathNamespace) Floor(v any) float64 {
	f, ok := toFloat(v)
	if !ok {
		return 0
	}
	return math.Floor(f)
}

func (mn *mathNamespace) Round(v any) float64 {
	f, ok := toFloat(v)
	if !ok {
		return 0
	}
	return math.Round(f)
}

func (mn *mathNamespace) Abs(v any) float64 {
	f, ok := toFloat(v)
	if !ok {
		return 0
	}
	return math.Abs(f)
}

func (mn *mathNamespace) Log(v any) float64 {
	f, ok := toFloat(v)
	if !ok {
		return 0
	}
	return math.Log(f)
}

func (mn *mathNamespace) Sqrt(v any) float64 {
	f, ok := toFloat(v)
	if !ok {
		return 0
	}
	return math.Sqrt(f)
}

func (mn *mathNamespace) Max(a, b any) any {
	fa, oka := toFloat(a)
	fb, okb := toFloat(b)
	if oka && okb {
		if fa > fb {
			return a
		}
		return b
	}
	return a
}

func (mn *mathNamespace) Min(a, b any) any {
	fa, oka := toFloat(a)
	fb, okb := toFloat(b)
	if oka && okb {
		if fa < fb {
			return a
		}
		return b
	}
	return a
}

// compareNamespace implements Hugo's `compare` namespace.
type compareNamespace struct{}

func (cn *compareNamespace) Default(dflt, val any) any {
	if isZero(val) {
		return dflt
	}
	return val
}

func (cn *compareNamespace) Eq(a, b any) bool {
	return reflect.DeepEqual(a, b)
}

func (cn *compareNamespace) Ne(a, b any) bool {
	return !reflect.DeepEqual(a, b)
}

func toFloat(v any) (float64, bool) {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	}
	return 0, false
}

func (cn *compareNamespace) Ge(a, b any) bool {
	fa, oka := toFloat(a)
	fb, okb := toFloat(b)
	if oka && okb {
		return fa >= fb
	}
	return fmt.Sprintf("%v", a) >= fmt.Sprintf("%v", b)
}

func (cn *compareNamespace) Gt(a, b any) bool {
	fa, oka := toFloat(a)
	fb, okb := toFloat(b)
	if oka && okb {
		return fa > fb
	}
	return fmt.Sprintf("%v", a) > fmt.Sprintf("%v", b)
}

func (cn *compareNamespace) Le(a, b any) bool {
	fa, oka := toFloat(a)
	fb, okb := toFloat(b)
	if oka && okb {
		return fa <= fb
	}
	return fmt.Sprintf("%v", a) <= fmt.Sprintf("%v", b)
}

func (cn *compareNamespace) Lt(a, b any) bool {
	fa, oka := toFloat(a)
	fb, okb := toFloat(b)
	if oka && okb {
		return fa < fb
	}
	return fmt.Sprintf("%v", a) < fmt.Sprintf("%v", b)
}

// cssNamespace implements Hugo's `css` namespace (stub).
type cssNamespace struct{}

func (cn *cssNamespace) Build(opts ...any) *stubResource { return &stubResource{} }
func (cn *cssNamespace) Sass(opts ...any) *stubResource  { return &stubResource{} }

// jsNamespace implements Hugo's `js` namespace (stub).
type jsNamespace struct{}

func (jn *jsNamespace) Build(opts ...any) *stubResource { return &stubResource{} }

// imagesNamespace implements Hugo's `images` namespace (stub).
type imagesNamespace struct{}

func (in *imagesNamespace) Filter(args ...any) *stubResource { return &stubResource{} }
func (in *imagesNamespace) Resize(args ...any) *stubResource { return &stubResource{} }
func (in *imagesNamespace) Fit(args ...any) *stubResource    { return &stubResource{} }
func (in *imagesNamespace) Fill(args ...any) *stubResource   { return &stubResource{} }
func (in *imagesNamespace) Crop(args ...any) *stubResource   { return &stubResource{} }
func (in *imagesNamespace) Config(path string) any           { return nil }

// fmtNamespace implements Hugo's `fmt` namespace.
type fmtNamespace struct{}

func (fn *fmtNamespace) Print(args ...any) string  { return fmt.Sprint(args...) }
func (fn *fmtNamespace) Printf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}
func (fn *fmtNamespace) Println(args ...any) string { return fmt.Sprintln(args...) }
func (fn *fmtNamespace) Warnf(format string, args ...any) string {
	// log but return empty (Hugo .Warnf logs to stderr and returns empty)
	return ""
}
func (fn *fmtNamespace) Errorf(format string, args ...any) string { return "" }

// langNamespace implements Hugo's `lang` namespace.
type langNamespace struct{ r *Renderer }

func (ln *langNamespace) Translate(key string, args ...any) string {
	return ln.r.i18nLookup(key)
}
func (ln *langNamespace) FormatNumber(precision int, number any) string {
	f, _ := toFloat(number)
	return fmt.Sprintf("%.*f", precision, f)
}
func (ln *langNamespace) FormatNumberCustom(precision int, number any, args ...string) string {
	f, _ := toFloat(number)
	return fmt.Sprintf("%.*f", precision, f)
}
func (ln *langNamespace) FormatPercent(precision int, number any) string {
	f, _ := toFloat(number)
	return fmt.Sprintf("%.*f%%", precision, f)
}
func (ln *langNamespace) FormatCurrency(precision int, currency string, number any) string {
	f, _ := toFloat(number)
	return fmt.Sprintf("%s %.*f", currency, precision, f)
}

// partialsNamespace implements Hugo's `partials` namespace.
type partialsNamespace struct {
	r *Renderer
}

func (pn *partialsNamespace) Include(name string, ctx ...any) (template.HTML, error) {
	var c any
	if len(ctx) > 0 {
		c = ctx[0]
	}
	return pn.r.renderPartial(name, c)
}

func (pn *partialsNamespace) IncludeCached(name string, ctx any, keys ...any) (template.HTML, error) {
	return pn.r.renderPartial(name, ctx)
}

// safeNamespace implements Hugo's `safe` namespace.
type safeNamespace struct{}

func (sn *safeNamespace) HTML(v any) template.HTML {
	if v == nil {
		return ""
	}
	if h, ok := v.(template.HTML); ok {
		return h
	}
	return template.HTML(fmt.Sprintf("%v", v))
}

func (sn *safeNamespace) CSS(v any) template.CSS {
	if v == nil {
		return ""
	}
	return template.CSS(fmt.Sprintf("%v", v))
}

func (sn *safeNamespace) URL(v any) template.URL {
	if v == nil {
		return ""
	}
	return template.URL(fmt.Sprintf("%v", v))
}

func (sn *safeNamespace) JS(v any) template.JS {
	if v == nil {
		return ""
	}
	return template.JS(fmt.Sprintf("%v", v))
}

func (sn *safeNamespace) HTMLAttr(v any) template.HTMLAttr {
	if v == nil {
		return ""
	}
	return template.HTMLAttr(fmt.Sprintf("%v", v))
}

// stringsNamespace implements Hugo's `strings` namespace.
type stringsNamespace struct{}

func (sn *stringsNamespace) Contains(s, substr string) bool        { return strings.Contains(s, substr) }
func (sn *stringsNamespace) ContainsAny(s, chars string) bool      { return strings.ContainsAny(s, chars) }
func (sn *stringsNamespace) HasPrefix(s, prefix string) bool       { return strings.HasPrefix(s, prefix) }
func (sn *stringsNamespace) HasSuffix(s, suffix string) bool       { return strings.HasSuffix(s, suffix) }
func (sn *stringsNamespace) TrimPrefix(s, prefix string) string    { return strings.TrimPrefix(s, prefix) }
func (sn *stringsNamespace) TrimSuffix(s, suffix string) string    { return strings.TrimSuffix(s, suffix) }
func (sn *stringsNamespace) TrimSpace(s string) string             { return strings.TrimSpace(s) }
func (sn *stringsNamespace) Trim(s, cutset string) string          { return strings.Trim(s, cutset) }
func (sn *stringsNamespace) ToLower(s string) string               { return strings.ToLower(s) }
func (sn *stringsNamespace) ToUpper(s string) string               { return strings.ToUpper(s) }
func (sn *stringsNamespace) Title(s string) string                 { return titleCase(s) }
func (sn *stringsNamespace) Count(s, substr string) int            { return strings.Count(s, substr) }
func (sn *stringsNamespace) Replace(s, old, new string) string     { return strings.ReplaceAll(s, old, new) }
func (sn *stringsNamespace) ReplaceRE(pattern, repl, s string) string {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return s
	}
	return re.ReplaceAllString(s, repl)
}
func (sn *stringsNamespace) Split(s, sep string) []string  { return strings.Split(s, sep) }
func (sn *stringsNamespace) Join(elems []string, sep string) string { return strings.Join(elems, sep) }
func (sn *stringsNamespace) Repeat(s string, count int) string { return strings.Repeat(s, count) }
func (sn *stringsNamespace) Index(s, substr string) int { return strings.Index(s, substr) }
func (sn *stringsNamespace) Truncate(n int, s string) string {
	return truncateWords(n, s)
}
func (sn *stringsNamespace) Chomp(s string) string { return strings.TrimRight(s, "\n\r") }
func (sn *stringsNamespace) Substr(s string, start int, length ...int) string {
	runes := []rune(s)
	if start < 0 {
		start = len(runes) + start
	}
	if start >= len(runes) {
		return ""
	}
	end := len(runes)
	if len(length) > 0 && length[0] >= 0 {
		end = start + length[0]
		if end > len(runes) {
			end = len(runes)
		}
	}
	return string(runes[start:end])
}
func (sn *stringsNamespace) FindRE(pattern, s string, n ...int) []string {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	limit := -1
	if len(n) > 0 {
		limit = n[0]
	}
	return re.FindAllString(s, limit)
}

// transformNamespace implements Hugo's `transform` namespace.
type transformNamespace struct{}

func (tn *transformNamespace) Plainify(v any) string {
	if v == nil {
		return ""
	}
	s := fmt.Sprintf("%v", v)
	return strings.TrimSpace(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(s, ""))
}

func (tn *transformNamespace) HTMLEscape(v any) string {
	if v == nil {
		return ""
	}
	return template.HTMLEscapeString(fmt.Sprintf("%v", v))
}

func (tn *transformNamespace) Markdownify(v any) template.HTML {
	if v == nil {
		return ""
	}
	return markdownifyString(fmt.Sprintf("%v", v))
}

func (tn *transformNamespace) Unmarshal(v any) any {
	if v == nil {
		return nil
	}
	var out any
	s := fmt.Sprintf("%v", v)
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

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
// partialReturnStore maps goroutine ID -> returned value for partial {{ return }}.
var partialReturnStore sync.Map

// goroutineID returns the current goroutine's numeric ID via runtime.Stack.
func goroutineID() int64 {
	var buf [32]byte
	n := runtime.Stack(buf[:], false)
	s := strings.TrimPrefix(string(buf[:n]), "goroutine ")
	if idx := strings.IndexByte(s, ' '); idx > 0 {
		id, err := strconv.ParseInt(s[:idx], 10, 64)
		if err == nil {
			return id
		}
	}
	return 0
}

// partialReturn is a sentinel panic value used to implement Hugo's {{ return }} in partials.
type partialReturn struct{ value any }

// returnNoParenRe matches {{ return expr }} where expr does NOT start with (.
// Hugo treats `return` as a statement, but we implement it as a function, so
// `{{ return cond X Y }}` causes Go templates to try calling `cond` with 0 args.
// We rewrite it to `{{ return (cond X Y) }}` to make the expression evaluate correctly.
var returnNoParenRe = regexp.MustCompile(`(\{\{[-\s]*)return\s+([^(\s}][^}]*?)([-\s]*\}\})`)

// preprocessTemplate rewrites `{{ return funcname args }}` to `{{ return (funcname args) }}`
// so that Hugo's partial-return pattern works with our function-based implementation.
func preprocessTemplate(src string) string {
	return returnNoParenRe.ReplaceAllStringFunc(src, func(m string) string {
		sub := returnNoParenRe.FindStringSubmatch(m)
		if sub == nil {
			return m
		}
		return sub[1] + "return (" + strings.TrimSpace(sub[2]) + ")" + sub[3]
	})
}

func (r *Renderer) renderPartial(name string, ctx any) (template.HTML, error) {
	result, err := r.renderPartialAny(name, ctx)
	if err != nil {
		return "", err
	}
	switch v := result.(type) {
	case template.HTML:
		return v, nil
	case string:
		return template.HTML(v), nil
	case nil:
		return "", nil
	default:
		// Non-HTML return (e.g. slice/map from {{ return $value }}).
		// Encode as JSON so it can be used in HTML context safely.
		return template.HTML(fmt.Sprintf("%v", v)), nil
	}
}

// renderPartialAny is like renderPartial but returns any (including slices from {{ return }}).
func (r *Renderer) renderPartialAny(name string, ctx any) (result any, err error) {
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
				if data, readErr := os.ReadFile(partialPath); readErr == nil {
					partialContent = string(data)
					break
				}
			}
		}
		if partialContent == "" {
			// Silently return empty for missing partials (common for optional partials like comments)
			return template.HTML(""), nil
		}

		parsed, parseErr := template.New(name).Funcs(r.funcMap).Parse(preprocessTemplate(partialContent))
		if parseErr != nil {
			return nil, fmt.Errorf("parse partial %q: %w", name, parseErr)
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

	gid := goroutineID()
	var buf bytes.Buffer
	execErr := t.Execute(&buf, ctx)

	// Check if {{ return }} was called; if so, use the stored value.
	if val, ok := partialReturnStore.LoadAndDelete(gid); ok {
		return val, nil
	}
	if execErr != nil {
		return nil, fmt.Errorf("execute partial %q: %w", name, execErr)
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

	// Parse each template piece separately into the same set.
	// This avoids "multiple definition" errors when both baseof uses {{block "X"}}
	// and kind templates use {{define "X"}} to override it — concatenation triggers
	// a duplicate error, but separate Parse calls allow override.
	t, err := template.New("baseof").Funcs(r.funcMap).Parse(preprocessTemplate(baseof))
	if err != nil {
		return nil, fmt.Errorf("parse template %q baseof: %w", kind, err)
	}

	// breadcrumb defines {{define "breadcrumb"}} — add it to the set.
	if _, err2 := t.Parse(breadcrumb); err2 != nil {
		_ = err2
	}

	// liveReloadBlock defines {{define "page-live-reload"}} — add it to the set.
	if _, err2 := t.Parse(liveReloadBlock); err2 != nil {
		_ = err2
	}

	// kind template — its {{define}} blocks override baseof's {{block}} defaults.
	if _, err2 := t.Parse(kindTmpl); err2 != nil {
		return nil, fmt.Errorf("parse template %q kind: %w", kind, err2)
	}

	// Register Hugo _internal/ templates into the set.
	for name, tmplContent := range internalTemplates {
		if _, err2 := t.New(name).Parse(tmplContent); err2 != nil {
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
