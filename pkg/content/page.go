package content

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"html/template"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	highlighting "github.com/yuin/goldmark-highlighting/v2"
	meta "github.com/yuin/goldmark-meta"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
)

// Page represents a single content page or section.
type Page struct {
	FilePath  string
	FileHash  string
	FileSize  int64
	FileMtime int64

	// URL fields
	RelPermalink string
	Permalink    string // full URL with base URL (set by build)
	OutputPath   string
	Slug         string   // front matter slug: overrides last URL segment
	URL          string   // front matter url: overrides entire permalink
	Aliases      []string // redirect aliases

	// Core metadata
	Title       string
	LinkTitle   string
	Description string
	Summary     string
	Date        time.Time
	PublishDate time.Time
	ExpiryDate  time.Time
	Lastmod     time.Time
	Draft       bool
	Weight      int
	Type        string // content type: front matter or root section name
	Layout      string // explicit template name override

	// Taxonomy fields
	Tags       []string
	Categories []string
	Keywords   []string
	Taxonomies map[string][]string // plural name -> terms

	// Visibility
	NoIndex       bool
	ExcludeSearch bool

	// Arbitrary front matter params
	Params map[string]any

	// Cascade: front matter values to inherit down to descendant pages.
	// Only set on section/home pages. Applied in BuildTree.
	Cascade map[string]any

	// Page classification
	Kind    string // "page"|"section"|"home"|"term"|"taxonomy"
	Lang    string
	Section string // root section name (first dir under content/)
	Depth   int

	// Tree structure
	Parent    *Page
	Children  []*Page
	Ancestors []*Page

	// Siblings (populated by build after tree is built)
	NextPage *Page
	PrevPage *Page

	// Rendered content
	ContentHTML  string
	ContentPlain string
	ReadingTime  int
	WordCount    int
}

// IsHome reports whether this is the site home page.
func (p *Page) IsHome() bool { return p.Kind == "home" }

// Path returns the logical path of the page (its RelPermalink).
func (p *Page) Path() string { return p.RelPermalink }

// PageScratch is a lightweight scratch pad used by content.Page.Store.
type PageScratch struct {
	mu   sync.Mutex
	data map[string]any
}

func (s *PageScratch) Get(key string) any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		return nil
	}
	return s.data[key]
}

func (s *PageScratch) Set(key string, val any) string {
	s.mu.Lock()
	if s.data == nil {
		s.data = make(map[string]any)
	}
	s.data[key] = val
	s.mu.Unlock()
	return ""
}

func (s *PageScratch) SetInMap(key, mapKey string, val any) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		s.data = make(map[string]any)
	}
	if m, ok := s.data[key].(map[string]any); ok {
		m[mapKey] = val
	} else {
		s.data[key] = map[string]any{mapKey: val}
	}
	return ""
}

func (s *PageScratch) Delete(key string) string {
	s.mu.Lock()
	if s.data != nil {
		delete(s.data, key)
	}
	s.mu.Unlock()
	return ""
}

// Store returns a per-page scratch pad (Hugo 0.117+).
func (p *Page) Store() *PageScratch {
	return &PageScratch{data: make(map[string]any)}
}

// PageResourcesStub is a minimal stub for page-level resources (image processing etc).
// The actual resource implementation lives in pkg/render to avoid circular imports,
// but content.Page needs to expose Resources() for direct template access via .Page.Resources.
type PageResourcesStub struct{}

func (r *PageResourcesStub) GetMatch(pattern string) any { return nil }
func (r *PageResourcesStub) Match(pattern string) []any  { return nil }
func (r *PageResourcesStub) Get(name string) any         { return nil }
func (r *PageResourcesStub) ByType(t string) []any       { return nil }

// Resources returns a stub resources object for this page.
func (p *Page) Resources() *PageResourcesStub {
	return &PageResourcesStub{}
}

// Content returns the HTML content for direct template access via .Page.Content.
func (p *Page) Content() template.HTML { return template.HTML(p.ContentHTML) }

// ContentBaseName returns the base name of the content file (without extension).
func (p *Page) ContentBaseName() string {
	base := filepath.Base(p.FilePath)
	ext := filepath.Ext(base)
	return base[:len(base)-len(ext)]
}

// IsPage reports whether this is a regular content page.
func (p *Page) IsPage() bool { return p.Kind == "page" }

// IsSection reports whether this is a section index page.
func (p *Page) IsSection() bool { return p.Kind == "section" }

// IsNode reports whether this is a list page (home, section, taxonomy, term).
func (p *Page) IsNode() bool {
	return p.Kind == "home" || p.Kind == "section" || p.Kind == "taxonomy" || p.Kind == "term"
}

// Plain returns plain-text content for templates.
func (p *Page) Plain() string { return p.ContentPlain }

// FuzzyWordCount returns WordCount rounded to the nearest 100.
func (p *Page) FuzzyWordCount() int {
	if p.WordCount < 100 {
		return p.WordCount
	}
	return int(math.Round(float64(p.WordCount)/100.0)) * 100
}

// Pages returns direct children (for list templates).
func (p *Page) Pages() []*Page { return p.Children }

// RegularPages returns only Kind=="page" children.
func (p *Page) RegularPages() []*Page {
	var out []*Page
	for _, c := range p.Children {
		if c.Kind == "page" {
			out = append(out, c)
		}
	}
	return out
}

// Sections returns only Kind=="section" children.
func (p *Page) Sections() []*Page {
	var out []*Page
	for _, c := range p.Children {
		if c.Kind == "section" {
			out = append(out, c)
		}
	}
	return out
}

// HashFile computes SHA-256 of a file and returns hex string.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// tagCodeFences rewrites bare ``` fences to add a language tag.
func tagCodeFences(data []byte) []byte {
	lines := bytes.Split(data, []byte("\n"))
	out := make([][]byte, 0, len(lines))
	lastHeading := ""
	i := 0

	for i < len(lines) {
		line := lines[i]
		trimmed := bytes.TrimSpace(line)

		if len(trimmed) > 0 && trimmed[0] == '#' {
			lastHeading = strings.ToLower(string(trimmed))
		}

		if bytes.Equal(trimmed, []byte("```")) {
			body := [][]byte{}
			j := i + 1
			for j < len(lines) && !bytes.Equal(bytes.TrimSpace(lines[j]), []byte("```")) {
				body = append(body, lines[j])
				j++
			}
			lang := fenceLang(lastHeading, body)
			out = append(out, []byte("```"+lang))
			out = append(out, body...)
			if j < len(lines) {
				out = append(out, lines[j])
			}
			i = j + 1
			continue
		}

		if bytes.HasPrefix(trimmed, []byte("```")) && len(trimmed) > 3 {
			out = append(out, line)
			i++
			for i < len(lines) {
				out = append(out, lines[i])
				if bytes.Equal(bytes.TrimSpace(lines[i]), []byte("```")) {
					i++
					break
				}
				i++
			}
			continue
		}

		out = append(out, line)
		i++
	}
	return bytes.Join(out, []byte("\n"))
}

var (
	rePython = regexp.MustCompile(`(?m)^\s*def\s+\w+|^\s*class\s+\w+\s*[:(]|^\s*(?:import|from)\s+\w+`)
	reGo     = regexp.MustCompile(`(?m)\bfunc\s+\w+|\bpackage\s+\w+|:=\s*`)
	reJava   = regexp.MustCompile(`(?m)^\s*(?:public|private|protected)\s+(?:static\s+)?\w`)
	reSQL    = regexp.MustCompile(`(?im)\bSELECT\b|\bFROM\b`)
	reCPP    = regexp.MustCompile(`(?m)#include\s*<|std::|cout\s*<<`)
)

func fenceLang(heading string, bodyLines [][]byte) string {
	h := heading
	switch {
	case strings.Contains(h, "python"):
		return "python"
	case strings.Contains(h, "golang") || strings.Contains(h, "go solution") ||
		strings.Contains(h, "## go") || strings.HasSuffix(h, " go"):
		return "go"
	case strings.Contains(h, "java"):
		return "java"
	case strings.Contains(h, "sql"):
		return "sql"
	case strings.Contains(h, "c++") || strings.Contains(h, "cpp"):
		return "cpp"
	case strings.Contains(h, "javascript") || strings.Contains(h, " js "):
		return "javascript"
	case strings.Contains(h, "typescript"):
		return "typescript"
	case strings.Contains(h, "rust"):
		return "rust"
	}
	code := string(bytes.Join(bodyLines, []byte("\n")))
	switch {
	case rePython.MatchString(code):
		return "python"
	case reGo.MatchString(code):
		return "go"
	case reJava.MatchString(code):
		return "java"
	case reSQL.MatchString(code):
		return "sql"
	case reCPP.MatchString(code):
		return "cpp"
	}
	return ""
}

// mdLinkRewriter strips ".md" from local link destinations.
type mdLinkRewriter struct{}

func (mdLinkRewriter) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var dest []byte
		switch v := n.(type) {
		case *ast.Link:
			dest = v.Destination
		case *ast.Image:
			dest = v.Destination
		default:
			return ast.WalkContinue, nil
		}
		d := string(dest)
		if strings.Contains(d, "://") || !strings.HasSuffix(d, ".md") {
			return ast.WalkContinue, nil
		}
		clean := strings.TrimSuffix(d, ".md") + "/"
		switch v := n.(type) {
		case *ast.Link:
			v.Destination = []byte(clean)
		case *ast.Image:
			v.Destination = []byte(clean)
		}
		return ast.WalkContinue, nil
	})
}

type mdLinkRewriterExt struct{}

func (mdLinkRewriterExt) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithASTTransformers(
			util.Prioritized(mdLinkRewriter{}, 999),
		),
	)
}

// SyntaxHighlight controls whether Chroma server-side syntax highlighting is enabled.
var SyntaxHighlight bool

var mdRendererPool = sync.Pool{
	New: func() any { return newMDRenderer() },
}

func newMDRenderer() goldmark.Markdown {
	exts := []goldmark.Extender{
		extension.GFM,
		meta.Meta,
		mdLinkRewriterExt{},
	}
	if SyntaxHighlight {
		exts = append(exts, highlighting.NewHighlighting(
			highlighting.WithStyle("github"),
			highlighting.WithGuessLanguage(true),
			highlighting.WithFormatOptions(
				chromahtml.WithClasses(true),
				chromahtml.WithLineNumbers(false),
			),
		))
	}
	return goldmark.New(
		goldmark.WithExtensions(exts...),
		goldmark.WithRendererOptions(html.WithUnsafe()),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	)
}

// ParseAndRender reads a markdown file, parses front matter, and renders HTML.
func ParseAndRender(filePath string) (*Page, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filePath, err)
	}

	h := sha256.New()
	h.Write(data)
	fileHash := fmt.Sprintf("%x", h.Sum(nil))

	rendered := tagCodeFences(data)

	md := mdRendererPool.Get().(goldmark.Markdown)
	var buf bytes.Buffer
	ctx := parser.NewContext()
	err = md.Convert(rendered, &buf, parser.WithContext(ctx))
	mdRendererPool.Put(md)
	if err != nil {
		return nil, fmt.Errorf("render %s: %w", filePath, err)
	}
	contentHTML := buf.String()

	fm := meta.Get(ctx)

	page := &Page{
		FilePath:    filePath,
		FileHash:    fileHash,
		ContentHTML: contentHTML,
		Params:      make(map[string]any),
		Taxonomies:  make(map[string][]string),
	}

	// Core metadata
	if v, ok := fm["title"]; ok {
		page.Title = toString(v)
	}
	if v, ok := fm["linkTitle"]; ok {
		page.LinkTitle = toString(v)
	} else if v, ok := fm["link_title"]; ok {
		page.LinkTitle = toString(v)
	}
	if page.LinkTitle == "" {
		page.LinkTitle = page.Title
	}
	if v, ok := fm["description"]; ok {
		page.Description = smartTruncate(toString(v), 280)
	}
	if v, ok := fm["summary"]; ok {
		page.Summary = toString(v)
	}

	// Dates with Hugo-compatible aliases
	if v, ok := fm["date"]; ok {
		page.Date = parseDate(toString(v))
	}
	for _, key := range []string{"publishDate", "publishdate", "pubdate", "published"} {
		if v, ok := fm[key]; ok {
			if d := parseDate(toString(v)); !d.IsZero() {
				page.PublishDate = d
				break
			}
		}
	}
	if page.PublishDate.IsZero() {
		page.PublishDate = page.Date
	}
	for _, key := range []string{"expiryDate", "expirydate", "unpublishdate"} {
		if v, ok := fm[key]; ok {
			if d := parseDate(toString(v)); !d.IsZero() {
				page.ExpiryDate = d
				break
			}
		}
	}
	for _, key := range []string{"lastmod", "modified"} {
		if v, ok := fm[key]; ok {
			if d := parseDate(toString(v)); !d.IsZero() {
				page.Lastmod = d
				break
			}
		}
	}
	if page.Lastmod.IsZero() {
		page.Lastmod = page.Date
	}

	if v, ok := fm["draft"]; ok {
		page.Draft = toBool(v)
	}
	if v, ok := fm["weight"]; ok {
		page.Weight = toInt(v)
	}
	if v, ok := fm["type"]; ok {
		page.Type = toString(v)
	}
	if v, ok := fm["layout"]; ok {
		page.Layout = toString(v)
	}

	// URL overrides
	if v, ok := fm["slug"]; ok {
		page.Slug = toString(v)
	}
	if v, ok := fm["url"]; ok {
		page.URL = toString(v)
	}
	if v, ok := fm["aliases"]; ok {
		page.Aliases = toStringSlice(v)
	}

	// Cascade: propagate these front matter values to descendant pages
	if v, ok := fm["cascade"]; ok {
		switch cv := v.(type) {
		case map[string]any:
			page.Cascade = cv
		case map[any]any:
			m := make(map[string]any, len(cv))
			for k, val := range cv {
				m[toString(k)] = val
			}
			page.Cascade = m
		}
	}

	// Taxonomy
	if v, ok := fm["tags"]; ok {
		page.Tags = toStringSlice(v)
		page.Taxonomies["tags"] = page.Tags
	}
	if v, ok := fm["categories"]; ok {
		page.Categories = toStringSlice(v)
		page.Taxonomies["categories"] = page.Categories
	}
	if v, ok := fm["keywords"]; ok {
		page.Keywords = toStringSlice(v)
	}

	// Visibility
	for _, key := range []string{"noIndex", "no_index", "noindex"} {
		if v, ok := fm[key]; ok {
			page.NoIndex = toBool(v)
			break
		}
	}
	for _, key := range []string{"excludeSearch", "exclude_search"} {
		if v, ok := fm[key]; ok {
			page.ExcludeSearch = toBool(v)
			break
		}
	}

	// Everything else -> Params
	known := map[string]bool{
		"title": true, "linkTitle": true, "link_title": true,
		"description": true, "summary": true,
		"date": true, "publishDate": true, "publishdate": true,
		"pubdate": true, "published": true,
		"expiryDate": true, "expirydate": true, "unpublishdate": true,
		"lastmod": true, "modified": true,
		"draft": true, "weight": true, "type": true, "layout": true,
		"slug": true, "url": true, "aliases": true,
		"tags": true, "categories": true, "keywords": true,
		"noIndex": true, "no_index": true, "noindex": true,
		"excludeSearch": true, "exclude_search": true,
		"cascade": true,
	}
	for k, v := range fm {
		if !known[k] {
			page.Params[k] = v
		}
	}

	// Word count and reading time
	plainText := stripHTML(contentHTML)
	plainText = strings.Join(strings.Fields(plainText), " ")
	words := countWords(plainText)
	page.WordCount = words
	page.ReadingTime = int(math.Ceil(float64(words) / 200.0))
	page.ContentPlain = plainText

	if page.Summary == "" {
		page.Summary = summarize(plainText, 70)
	}

	return page, nil
}

// isAlpha reports whether s contains only lowercase ASCII letters.
func isAlpha(s string) bool {
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

// PermalinkFromPath derives the base permalink from a file path.
func PermalinkFromPath(contentDir, filePath, defaultLang string) (string, string) {
	rel, _ := filepath.Rel(contentDir, filePath)
	rel = filepath.ToSlash(rel)

	parts := strings.SplitN(rel, "/", 2)

	var lang, rest string
	if len(parts[0]) == 2 && isAlpha(parts[0]) {
		lang = parts[0]
		if len(parts) > 1 {
			rest = parts[1]
		}
	} else {
		lang = defaultLang
		rest = rel
	}

	ext := filepath.Ext(rest)
	rest = strings.TrimSuffix(rest, ext)

	if strings.HasSuffix(rest, "/_index") {
		rest = strings.TrimSuffix(rest, "/_index")
	} else if rest == "_index" {
		rest = ""
	}
	// Leaf bundle: index.md in a subdir -> parent dir permalink
	if strings.HasSuffix(rest, "/index") {
		rest = strings.TrimSuffix(rest, "/index")
	}

	var permalink string
	if lang == defaultLang {
		if rest == "" {
			permalink = "/"
		} else {
			permalink = "/" + rest + "/"
		}
	} else {
		if rest == "" {
			permalink = "/" + lang + "/"
		} else {
			permalink = "/" + lang + "/" + rest + "/"
		}
	}

	for strings.Contains(permalink, "//") {
		permalink = strings.ReplaceAll(permalink, "//", "/")
	}
	return permalink, lang
}

// ApplySlugURL applies slug and url front matter overrides to a base permalink.
// page.Kind must be set before calling.
func ApplySlugURL(base string, page *Page) string {
	if page.URL != "" {
		u := page.URL
		if !strings.HasPrefix(u, "/") {
			u = "/" + u
		}
		if !strings.HasSuffix(u, "/") {
			u += "/"
		}
		return u
	}
	if page.Slug != "" && page.Kind == "page" {
		trimmed := strings.TrimRight(base, "/")
		lastSlash := strings.LastIndex(trimmed, "/")
		if lastSlash >= 0 {
			return trimmed[:lastSlash+1] + slugify(page.Slug) + "/"
		}
		return "/" + slugify(page.Slug) + "/"
	}
	return base
}

// slugify converts a string to a URL-safe slug.
func slugify(s string) string {
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

// OutputPathFromPermalink converts a permalink to an output file path.
func OutputPathFromPermalink(outputDir, permalink string) string {
	clean := strings.Trim(permalink, "/")
	if clean == "" {
		return filepath.Join(outputDir, "index.html")
	}
	return filepath.Join(outputDir, filepath.FromSlash(clean), "index.html")
}

// smartTruncate cuts text at a sentence boundary within maxChars.
func smartTruncate(text string, maxChars int) string {
	if len(text) <= maxChars {
		return text
	}
	window := text[:maxChars]
	last := -1
	for _, ch := range []byte{'.', '!', '?'} {
		if idx := strings.LastIndexByte(window, ch); idx > last {
			last = idx
		}
	}
	if last >= maxChars*2/5 {
		return strings.TrimSpace(text[:last+1])
	}
	if cut := strings.LastIndex(window, " "); cut > 0 {
		return strings.TrimRight(window[:cut], ",:;") + "…"
	}
	return window + "…"
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	default:
		return fmt.Sprintf("%v", v)
	}
}

func toBool(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return strings.EqualFold(b, "true") || b == "1"
	case int:
		return b != 0
	}
	return false
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		var i int
		fmt.Sscanf(n, "%d", &i)
		return i
	}
	return 0
}

func toStringSlice(v any) []string {
	switch s := v.(type) {
	case []any:
		result := make([]string, 0, len(s))
		for _, item := range s {
			result = append(result, toString(item))
		}
		return result
	case []string:
		return s
	case string:
		if s == "" {
			return nil
		}
		return []string{s}
	}
	return nil
}

func parseDate(s string) time.Time {
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02",
		"2006/01/02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
		} else if r == '>' {
			inTag = false
			b.WriteRune(' ')
		} else if !inTag {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func countWords(s string) int {
	return len(strings.Fields(s))
}

func summarize(text string, maxWords int) string {
	words := strings.Fields(text)
	if len(words) <= maxWords {
		return strings.Join(words, " ")
	}
	return strings.Join(words[:maxWords], " ") + "..."
}

func langStrip(parts []string) []string {
	if len(parts) > 0 && len(parts[0]) == 2 && isAlpha(parts[0]) {
		return parts[1:]
	}
	return parts
}

// KindFromPath determines the page kind from its file path.
func KindFromPath(contentDir, filePath string) string {
	rel, _ := filepath.Rel(contentDir, filePath)
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	parts = langStrip(parts)

	base := ""
	if len(parts) > 0 {
		base = parts[len(parts)-1]
	}

	if base == "_index.md" {
		if len(parts) == 1 {
			return "home"
		}
		return "section"
	}
	if base == "index.md" {
		if len(parts) == 1 {
			return "home"
		}
		return "page" // leaf bundle
	}
	return "page"
}

// SectionFromPath extracts the top-level section name.
func SectionFromPath(contentDir, filePath string) string {
	rel, _ := filepath.Rel(contentDir, filePath)
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	rest := langStrip(parts)
	if len(rest) == 0 || rest[0] == "_index.md" || rest[0] == "index.md" {
		return ""
	}
	if len(rest) == 1 {
		return ""
	}
	return rest[0]
}

// DepthFromPath calculates depth: home=0, top-level=1, nested=2, etc.
func DepthFromPath(contentDir, filePath string) int {
	rel, _ := filepath.Rel(contentDir, filePath)
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	parts = langStrip(parts)
	if len(parts) > 0 {
		parts = parts[:len(parts)-1]
	}
	return len(parts)
}

// WriteChromaCSS writes a combined light+dark syntax-highlight stylesheet.
func WriteChromaCSS(path string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}

	formatter := chromahtml.New(chromahtml.WithClasses(true))
	var buf bytes.Buffer

	buf.WriteString("/* chroma syntax highlighting -- light (github) */\n")
	if err := formatter.WriteCSS(&buf, styles.Get("github")); err != nil {
		return "", fmt.Errorf("light css: %w", err)
	}

	buf.WriteString("\n/* chroma syntax highlighting -- dark (dracula) */\n")
	var darkBuf bytes.Buffer
	if err := formatter.WriteCSS(&darkBuf, styles.Get("dracula")); err != nil {
		return "", fmt.Errorf("dark css: %w", err)
	}
	for _, line := range strings.Split(darkBuf.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if idx := strings.Index(line, "*/"); idx >= 0 {
			line = strings.TrimSpace(line[idx+2:])
		}
		if line == "" {
			continue
		}
		buf.WriteString(".dark " + line + "\n")
	}

	data := buf.Bytes()
	h := sha256.New()
	h.Write(data)
	hash := fmt.Sprintf("%x", h.Sum(nil))

	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}
	return hash, nil
}
