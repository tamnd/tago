package content

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
	FilePath     string
	FileHash     string
	FileSize     int64
	FileMtime    int64
	RelPermalink string
	OutputPath   string

	Title         string
	LinkTitle     string
	Description   string
	Date          time.Time
	Tags          []string
	Draft         bool
	Weight        int
	Type          string
	NoIndex       bool
	ExcludeSearch bool
	Params        map[string]any

	Kind      string // "page"|"section"|"home"|"term"|"taxonomy"|"special"
	Lang      string
	Section   string
	Depth     int
	Parent    *Page
	Children  []*Page
	Ancestors []*Page

	ContentHTML  string
	ContentPlain string // plain text version for search
	Summary      string
	ReadingTime  int
	WordCount    int
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

// tagCodeFences rewrites bare ``` code fences to add a language tag.
// It detects the language from: (1) the most-recent heading ("## Python Solution"),
// (2) code content patterns (def/class → python, func/:= → go, etc.).
// This runs on the raw markdown bytes before goldmark parses, so the language
// info is available to the chroma highlighter.
func tagCodeFences(data []byte) []byte {
	lines := bytes.Split(data, []byte("\n"))
	out := make([][]byte, 0, len(lines))
	lastHeading := ""
	i := 0

	for i < len(lines) {
		line := lines[i]
		trimmed := bytes.TrimSpace(line)

		// Track the most recent heading for language context.
		if len(trimmed) > 0 && trimmed[0] == '#' {
			lastHeading = strings.ToLower(string(trimmed))
		}

		// Bare opening fence — no language tag, not inside another fence.
		if bytes.Equal(trimmed, []byte("```")) {
			// Read ahead to collect the fence body.
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
				out = append(out, lines[j]) // closing ```
			}
			i = j + 1
			continue
		}

		// Tagged fence (```python, ```go …) — skip its body unchanged.
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

// fenceLang picks a language tag from heading text or code content patterns.
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

// mdLinkRewriter strips ".md" from local link destinations so that markdown
// files can reference each other with relative .md paths while the rendered
// HTML links point to the clean URL (e.g. "00xx/1.md" → "00xx/1/").
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
		// Only rewrite local relative paths that end with ".md"
		d := string(dest)
		if strings.Contains(d, "://") || !strings.HasSuffix(d, ".md") {
			return ast.WalkContinue, nil
		}
		// Strip .md; add trailing slash for clean URL
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

// mdLinkRewriterExt is a goldmark extension that registers mdLinkRewriter.
type mdLinkRewriterExt struct{}

func (mdLinkRewriterExt) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithASTTransformers(
			util.Prioritized(mdLinkRewriter{}, 999),
		),
	)
}

// mdRenderer is a shared goldmark instance.
var mdRenderer goldmark.Markdown

func init() {
	mdRenderer = goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			meta.Meta,
			mdLinkRewriterExt{},
			highlighting.NewHighlighting(
				highlighting.WithStyle("github"),
				highlighting.WithGuessLanguage(true),
				highlighting.WithFormatOptions(
					chromahtml.WithClasses(true),
					chromahtml.WithLineNumbers(false),
				),
			),
		),
		goldmark.WithRendererOptions(
			html.WithUnsafe(),
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
	)
}

// ParseAndRender reads a markdown file, parses frontmatter, and renders to HTML.
func ParseAndRender(filePath string) (*Page, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filePath, err)
	}

	// Compute hash from original source (before preprocessing).
	h := sha256.New()
	h.Write(data)
	fileHash := fmt.Sprintf("%x", h.Sum(nil))

	// Pre-process: tag bare code fences with detected language before goldmark parses.
	rendered := tagCodeFences(data)

	// Render markdown → HTML
	var buf bytes.Buffer
	ctx := parser.NewContext()
	if err := mdRenderer.Convert(rendered, &buf, parser.WithContext(ctx)); err != nil {
		return nil, fmt.Errorf("render %s: %w", filePath, err)
	}
	contentHTML := buf.String()

	// Extract frontmatter
	fm := meta.Get(ctx)

	page := &Page{
		FilePath:    filePath,
		FileHash:    fileHash,
		ContentHTML: contentHTML,
		Params:      make(map[string]any),
	}

	// Parse known frontmatter fields
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
	if v, ok := fm["date"]; ok {
		page.Date = parseDate(toString(v))
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
	if v, ok := fm["noIndex"]; ok {
		page.NoIndex = toBool(v)
	} else if v, ok := fm["no_index"]; ok {
		page.NoIndex = toBool(v)
	}
	if v, ok := fm["excludeSearch"]; ok {
		page.ExcludeSearch = toBool(v)
	} else if v, ok := fm["exclude_search"]; ok {
		page.ExcludeSearch = toBool(v)
	}
	if v, ok := fm["tags"]; ok {
		page.Tags = toStringSlice(v)
	}

	// Everything else goes to Params
	known := map[string]bool{
		"title": true, "linkTitle": true, "link_title": true,
		"description": true, "date": true, "draft": true,
		"weight": true, "type": true, "noIndex": true, "no_index": true,
		"excludeSearch": true, "exclude_search": true, "tags": true,
	}
	for k, v := range fm {
		if !known[k] {
			page.Params[k] = v
		}
	}

	// Compute word count and reading time
	plainText := stripHTML(contentHTML)
	plainText = strings.Join(strings.Fields(plainText), " ") // normalize whitespace
	words := countWords(plainText)
	page.WordCount = words
	page.ReadingTime = int(math.Ceil(float64(words) / 200.0))

	// Store plain text for search
	page.ContentPlain = plainText

	// Generate summary: first 70 words
	page.Summary = summarize(plainText, 70)

	return page, nil
}

// isAlpha returns true if s consists only of lowercase ASCII letters.
func isAlpha(s string) bool {
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

// PermalinkFromPath derives the permalink from file path + lang + content root.
// contentDir: root content directory
// filePath:   absolute path to the .md file
// defaultLang: language that gets no URL prefix (usually "en")
func PermalinkFromPath(contentDir, filePath, defaultLang string) (string, string) {
	rel, _ := filepath.Rel(contentDir, filePath)
	rel = filepath.ToSlash(rel)

	parts := strings.SplitN(rel, "/", 2)

	var lang, rest string
	// Detect if first segment is a language code (2-letter alpha)
	if len(parts[0]) == 2 && isAlpha(parts[0]) {
		lang = parts[0]
		if len(parts) > 1 {
			rest = parts[1]
		}
	} else {
		// contentDir is already language-specific, use defaultLang
		lang = defaultLang
		rest = rel
	}

	// Strip extension
	ext := filepath.Ext(rest)
	rest = strings.TrimSuffix(rest, ext)

	// _index → parent dir permalink
	if strings.HasSuffix(rest, "/_index") {
		rest = strings.TrimSuffix(rest, "/_index")
	} else if rest == "_index" {
		rest = ""
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

	// Clean double slashes
	for strings.Contains(permalink, "//") {
		permalink = strings.ReplaceAll(permalink, "//", "/")
	}
	return permalink, lang
}

// OutputPathFromPermalink converts a permalink to an output file path under outputDir.
func OutputPathFromPermalink(outputDir, permalink string) string {
	clean := strings.Trim(permalink, "/")
	if clean == "" {
		return filepath.Join(outputDir, "index.html")
	}
	return filepath.Join(outputDir, filepath.FromSlash(clean), "index.html")
}

// smartTruncate cuts text at a sentence boundary (. ! ?) within maxChars.
// Falls back to word boundary + ellipsis when no sentence end is found.
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
	if last >= maxChars*2/5 { // at least 40% in
		return strings.TrimSpace(text[:last+1])
	}
	if cut := strings.LastIndex(window, " "); cut > 0 {
		return strings.TrimRight(window[:cut], ",:;") + "…"
	}
	return window + "…"
}

// --- helper conversion functions ---

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
	return strings.Join(words[:maxWords], " ") + "…"
}

// langStrip removes the language prefix from path parts if present.
// Returns the parts without the lang prefix.
func langStrip(parts []string) []string {
	if len(parts) > 0 && len(parts[0]) == 2 && isAlpha(parts[0]) {
		return parts[1:]
	}
	return parts
}

// KindFromPath determines the page kind from its path.
func KindFromPath(contentDir, filePath string) string {
	rel, _ := filepath.Rel(contentDir, filePath)
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")

	// Remove lang prefix if present
	parts = langStrip(parts)

	base := ""
	if len(parts) > 0 {
		base = parts[len(parts)-1]
	}

	if base == "_index.md" || base == "index.md" {
		if len(parts) == 1 {
			return "home"
		}
		return "section"
	}
	return "page"
}

// SectionFromPath extracts the top-level section name.
func SectionFromPath(contentDir, filePath string) string {
	rel, _ := filepath.Rel(contentDir, filePath)
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	// Skip lang prefix if present
	rest := langStrip(parts)
	if len(rest) == 0 {
		return ""
	}
	if rest[0] == "_index.md" || rest[0] == "index.md" {
		return ""
	}
	// If there's only one segment and it's a file, section is empty (top-level page)
	if len(rest) == 1 {
		return ""
	}
	return rest[0]
}

// DepthFromPath calculates depth: home=0, top-level pages/sections=1, nested=2, etc.
func DepthFromPath(contentDir, filePath string) int {
	rel, _ := filepath.Rel(contentDir, filePath)
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	// Remove lang prefix if present
	parts = langStrip(parts)
	// Remove filename
	if len(parts) > 0 {
		parts = parts[:len(parts)-1]
	}
	return len(parts)
}

// WriteChromaCSS writes a combined light+dark syntax-highlight stylesheet.
// Light rules use the "github" chroma style; dark rules are prefixed with ".dark".
// Returns the SHA-256 hex hash of the written file (for fingerprinting).
func WriteChromaCSS(path string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}

	formatter := chromahtml.New(chromahtml.WithClasses(true))

	var buf bytes.Buffer

	// Light theme (github)
	buf.WriteString("/* chroma syntax highlighting — light (github) */\n")
	if err := formatter.WriteCSS(&buf, styles.Get("github")); err != nil {
		return "", fmt.Errorf("light css: %w", err)
	}

	// Dark theme (dracula): prefix every CSS rule with ".dark ".
	// Chroma emits lines like: "/* Keyword */ .chroma .k { color: #... }"
	// We must strip the "/* ... */" comment before checking for the selector,
	// otherwise HasPrefix(".") never matches and dark rules leak into light mode.
	buf.WriteString("\n/* chroma syntax highlighting — dark (dracula) */\n")
	var darkBuf bytes.Buffer
	if err := formatter.WriteCSS(&darkBuf, styles.Get("dracula")); err != nil {
		return "", fmt.Errorf("dark css: %w", err)
	}
	for _, line := range strings.Split(darkBuf.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Strip block comment prefix ("/* ... */ ") to reach the CSS selector.
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
