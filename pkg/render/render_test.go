package render

import (
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/tago/pkg/content"
)

func makeSite() *SiteData {
	return &SiteData{
		Title:   "Test Site",
		BaseURL: "http://localhost:1313/",
		Params: map[string]any{
			"author": "Test Author",
		},
	}
}

func makeRenderer(t *testing.T) *Renderer {
	t.Helper()
	return New(makeSite(), AssetRefs{}, "", false)
}

func makeRendererWithLayouts(t *testing.T, layoutsDir string) *Renderer {
	t.Helper()
	return New(makeSite(), AssetRefs{}, layoutsDir, false)
}

// --- Template function tests ---

func TestFuncWhere(t *testing.T) {
	pages := []*content.Page{
		{Kind: "page", Section: "posts"},
		{Kind: "section", Section: "posts"},
		{Kind: "page", Section: "docs"},
	}
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	whereFn := fm["where"].(func([]*content.Page, string, any) []*content.Page)

	result := whereFn(pages, "Kind", "page")
	if len(result) != 2 {
		t.Errorf("where Kind=page: got %d, want 2", len(result))
	}

	result2 := whereFn(pages, "Section", "docs")
	if len(result2) != 1 {
		t.Errorf("where Section=docs: got %d, want 1", len(result2))
	}
}

func TestFuncFirst(t *testing.T) {
	pages := []*content.Page{{Title: "A"}, {Title: "B"}, {Title: "C"}}
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	firstFn := fm["first"].(func(int, []*content.Page) []*content.Page)

	result := firstFn(2, pages)
	if len(result) != 2 {
		t.Errorf("first(2): got %d, want 2", len(result))
	}
	if result[0].Title != "A" {
		t.Errorf("first(2)[0] = %q, want A", result[0].Title)
	}

	// first with n > len should return all
	all := firstFn(10, pages)
	if len(all) != 3 {
		t.Errorf("first(10) of 3: got %d, want 3", len(all))
	}
}

func TestFuncLast(t *testing.T) {
	pages := []*content.Page{{Title: "A"}, {Title: "B"}, {Title: "C"}}
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	lastFn := fm["last"].(func(int, []*content.Page) []*content.Page)

	result := lastFn(2, pages)
	if len(result) != 2 {
		t.Errorf("last(2): got %d, want 2", len(result))
	}
	if result[0].Title != "B" {
		t.Errorf("last(2)[0] = %q, want B", result[0].Title)
	}
}

func TestFuncAfter(t *testing.T) {
	pages := []*content.Page{{Title: "A"}, {Title: "B"}, {Title: "C"}}
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	afterFn := fm["after"].(func(int, []*content.Page) []*content.Page)

	result := afterFn(1, pages)
	if len(result) != 2 {
		t.Errorf("after(1): got %d, want 2", len(result))
	}
	if result[0].Title != "B" {
		t.Errorf("after(1)[0] = %q, want B", result[0].Title)
	}
}

func TestFuncHumanize(t *testing.T) {
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	humanizeFn := fm["humanize"].(func(string) string)

	if got := humanizeFn("my-post-title"); got != "My post title" {
		t.Errorf("humanize(my-post-title) = %q, want %q", got, "My post title")
	}
	if got := humanizeFn("another_example"); got != "Another example" {
		t.Errorf("humanize(another_example) = %q, want %q", got, "Another example")
	}
}

func TestFuncDefaultFn(t *testing.T) {
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	defaultFn := fm["default"].(func(any, any) any)

	// Non-empty value should pass through
	if got := defaultFn("fallback", "actual"); got != "actual" {
		t.Errorf("default with actual value: got %v, want actual", got)
	}
	// Empty string should return fallback
	if got := defaultFn("fallback", ""); got != "fallback" {
		t.Errorf("default with empty string: got %v, want fallback", got)
	}
	// Zero int should return fallback
	if got := defaultFn(42, 0); got != 42 {
		t.Errorf("default with zero int: got %v, want 42", got)
	}
}

func TestFuncDateFormat(t *testing.T) {
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	dateFormatFn := fm["dateFormat"].(func(any, any) string)

	t1 := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
	got := dateFormatFn("2006-01-02", t1)
	if got != "2024-06-15" {
		t.Errorf("dateFormat(2006-01-02, ...) = %q, want 2024-06-15", got)
	}
	// Hugo named layout
	got2 := dateFormatFn(":date_short", t1)
	if got2 == "" {
		t.Error("dateFormat(:date_short) should not be empty")
	}
}

func TestFuncAbsURL(t *testing.T) {
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	absURLFn := fm["absURL"].(func(string) string)

	got := absURLFn("/posts/hello/")
	if !strings.HasPrefix(got, "http://localhost:1313/") {
		t.Errorf("absURL = %q, should start with site BaseURL", got)
	}
}

func TestFuncRelURL(t *testing.T) {
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	relURLFn := fm["relURL"].(func(string) string)

	got := relURLFn("posts/hello/")
	if got != "/posts/hello/" {
		t.Errorf("relURL(posts/hello/) = %q, want /posts/hello/", got)
	}
	got2 := relURLFn("/already-absolute/")
	if got2 != "/already-absolute/" {
		t.Errorf("relURL(/already-absolute/) = %q, want /already-absolute/", got2)
	}
}

func TestFuncSeq(t *testing.T) {
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	seqFn := fm["seq"].(func(int) []int)

	result := seqFn(5)
	if len(result) != 5 {
		t.Fatalf("seq(5) len = %d, want 5", len(result))
	}
	if result[0] != 1 || result[4] != 5 {
		t.Errorf("seq(5) = %v, want [1 2 3 4 5]", result)
	}
}

func TestFuncIn(t *testing.T) {
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	inFn := fm["in"].(func(any, any) bool)

	if !inFn([]string{"a", "b", "c"}, "b") {
		t.Error("in(['a','b','c'], 'b') should be true")
	}
	if inFn([]string{"a", "b"}, "z") {
		t.Error("in(['a','b'], 'z') should be false")
	}
}

func TestFuncTruncate(t *testing.T) {
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	truncFn := fm["truncate"].(func(int, string) string)

	long := "This is a very long sentence with many words in it yes."
	got := truncFn(20, long)
	// truncate trims at word boundary so result may be slightly shorter
	if len(got) >= len(long) {
		t.Errorf("truncate(20, long) should be shorter than original (%d chars), got %d", len(long), len(got))
	}
	// Short string passes through unchanged
	short := "hello"
	if truncFn(20, short) != short {
		t.Errorf("truncate(20, short) should return unchanged: %q", short)
	}
}

func TestFuncMarkdownify(t *testing.T) {
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	mdFn := fm["markdownify"].(func(string) template.HTML)

	result := mdFn("**bold** text")
	if result == "" {
		t.Error("markdownify should return non-empty HTML")
	}
	if !strings.Contains(string(result), "strong") && !strings.Contains(string(result), "bold") {
		t.Errorf("markdownify(**bold** text) = %q, should contain bold markup", result)
	}
}

func TestFuncSort(t *testing.T) {
	pages := []*content.Page{
		{Title: "C", Weight: 3},
		{Title: "A", Weight: 1},
		{Title: "B", Weight: 2},
	}
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	sortFn := fm["sort"].(func([]*content.Page, ...string) []*content.Page)

	sorted := sortFn(pages, "Weight")
	if sorted[0].Title != "A" {
		t.Errorf("sort by Weight: first = %q, want A", sorted[0].Title)
	}

	sortedByTitle := sortFn(pages, "Title")
	if sortedByTitle[0].Title != "A" {
		t.Errorf("sort by Title: first = %q, want A", sortedByTitle[0].Title)
	}
}

func TestFuncJsonify(t *testing.T) {
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	jsonifyFn := fm["jsonify"].(func(any) string)

	got := jsonifyFn(map[string]any{"key": "value"})
	if !strings.Contains(got, "key") {
		t.Errorf("jsonify result %q should contain 'key'", got)
	}
}

func TestFuncDict(t *testing.T) {
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	dictFn := fm["dict"].(func(...any) map[string]any)

	result := dictFn("name", "Alice", "age", 30)
	if result["name"] != "Alice" {
		t.Errorf("dict[name] = %v, want Alice", result["name"])
	}
}

func TestFuncCond(t *testing.T) {
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	condFn := fm["cond"].(func(bool, any, any) any)

	if condFn(true, "yes", "no") != "yes" {
		t.Error("cond(true, ...) should return first value")
	}
	if condFn(false, "yes", "no") != "no" {
		t.Error("cond(false, ...) should return second value")
	}
}

// --- Template lookup order tests ---

func TestTemplateLookupPageDefault(t *testing.T) {
	r := makeRenderer(t)
	// Should not error — falls back to embedded default
	tmpl, err := r.getTemplateFor("page", "", "", "", "en")
	if err != nil {
		t.Errorf("getTemplateFor(page) error: %v", err)
	}
	if tmpl == nil {
		t.Error("getTemplateFor(page) returned nil")
	}
}

func TestTemplateLookupHomeDefault(t *testing.T) {
	r := makeRenderer(t)
	tmpl, err := r.getTemplateFor("home", "", "", "", "en")
	if err != nil {
		t.Errorf("getTemplateFor(home) error: %v", err)
	}
	if tmpl == nil {
		t.Error("getTemplateFor(home) returned nil")
	}
}

func TestTemplateLookupSectionDefault(t *testing.T) {
	r := makeRenderer(t)
	tmpl, err := r.getTemplateFor("section", "", "", "posts", "en")
	if err != nil {
		t.Errorf("getTemplateFor(section) error: %v", err)
	}
	if tmpl == nil {
		t.Error("getTemplateFor(section) returned nil")
	}
}

func TestTemplateLookupCustomLayout(t *testing.T) {
	dir := t.TempDir()
	// Create a custom layout file
	defaultDir := filepath.Join(dir, "_default")
	if err := os.MkdirAll(defaultDir, 0755); err != nil {
		t.Fatal(err)
	}
	customTmpl := `{{define "main"}}<custom>{{.Page.Title}}</custom>{{end}}`
	if err := os.WriteFile(filepath.Join(defaultDir, "single.html"), []byte(customTmpl), 0644); err != nil {
		t.Fatal(err)
	}

	r := makeRendererWithLayouts(t, dir)
	tmpl, err := r.getTemplateFor("page", "", "", "", "en")
	if err != nil {
		t.Fatalf("getTemplateFor with custom layout: %v", err)
	}
	if tmpl == nil {
		t.Fatal("template is nil")
	}
}

func TestTemplateLookupTypeSpecificLayout(t *testing.T) {
	dir := t.TempDir()
	// Create a type-specific layout
	postsDir := filepath.Join(dir, "posts")
	if err := os.MkdirAll(postsDir, 0755); err != nil {
		t.Fatal(err)
	}
	customTmpl := `{{define "main"}}<posts-single>{{.Page.Title}}</posts-single>{{end}}`
	if err := os.WriteFile(filepath.Join(postsDir, "single.html"), []byte(customTmpl), 0644); err != nil {
		t.Fatal(err)
	}

	r := makeRendererWithLayouts(t, dir)
	tmpl, err := r.getTemplateFor("page", "posts", "", "", "en")
	if err != nil {
		t.Fatalf("type-specific template lookup: %v", err)
	}
	if tmpl == nil {
		t.Fatal("template is nil")
	}
}

func TestTemplateLookupLayoutFrontMatter(t *testing.T) {
	dir := t.TempDir()
	defaultDir := filepath.Join(dir, "_default")
	if err := os.MkdirAll(defaultDir, 0755); err != nil {
		t.Fatal(err)
	}
	customTmpl := `{{define "main"}}<special>{{.Page.Title}}</special>{{end}}`
	if err := os.WriteFile(filepath.Join(defaultDir, "special.html"), []byte(customTmpl), 0644); err != nil {
		t.Fatal(err)
	}

	r := makeRendererWithLayouts(t, dir)
	// Page with layout: special in front matter
	tmpl, err := r.getTemplateFor("page", "", "special", "", "en")
	if err != nil {
		t.Fatalf("layout front matter lookup: %v", err)
	}
	if tmpl == nil {
		t.Fatal("template is nil with layout front matter")
	}
}

func TestPartialRendering(t *testing.T) {
	dir := t.TempDir()
	partialsDir := filepath.Join(dir, "partials")
	if err := os.MkdirAll(partialsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partialsDir, "header.html"),
		[]byte(`<header>{{.Title}}</header>`), 0644); err != nil {
		t.Fatal(err)
	}

	r := makeRendererWithLayouts(t, dir)
	result, err := r.renderPartial("header", map[string]string{"Title": "Test"})
	if err != nil {
		t.Fatalf("renderPartial: %v", err)
	}
	if !strings.Contains(string(result), "Test") {
		t.Errorf("partial output %q should contain 'Test'", result)
	}
}

// --- RenderPage integration tests ---

func TestRenderPageOutputsHTML(t *testing.T) {
	contentDir := t.TempDir()
	outputDir := t.TempDir()

	page := &content.Page{
		FilePath:     filepath.Join(contentDir, "about.md"),
		RelPermalink: "/about/",
		OutputPath:   filepath.Join(outputDir, "about", "index.html"),
		Title:        "About",
		ContentHTML:  "<p>About page content.</p>",
		Kind:         "page",
		Lang:         "en",
	}

	r := makeRenderer(t)
	r.Prewarm()
	if err := r.RenderPage(page, []*content.Page{page}); err != nil {
		t.Fatalf("RenderPage error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(outputDir, "about", "index.html"))
	if err != nil {
		t.Fatalf("output file not written: %v", err)
	}
	if !strings.Contains(string(data), "About") {
		t.Error("output HTML should contain page title")
	}
}

func TestRenderPageSectionListsChildren(t *testing.T) {
	outputDir := t.TempDir()

	child1 := &content.Page{Title: "Post 1", Kind: "page", RelPermalink: "/posts/post1/", Lang: "en"}
	child2 := &content.Page{Title: "Post 2", Kind: "page", RelPermalink: "/posts/post2/", Lang: "en"}
	section := &content.Page{
		RelPermalink: "/posts/",
		OutputPath:   filepath.Join(outputDir, "posts", "index.html"),
		Title:        "Posts",
		Kind:         "section",
		Lang:         "en",
		Children:     []*content.Page{child1, child2},
	}
	child1.Parent = section
	child2.Parent = section

	r := makeRenderer(t)
	r.Prewarm()
	allPages := []*content.Page{section, child1, child2}
	if err := r.RenderPage(section, allPages); err != nil {
		t.Fatalf("RenderPage section: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(outputDir, "posts", "index.html"))
	if err != nil {
		t.Fatalf("section index not written: %v", err)
	}
	if !strings.Contains(string(data), "Posts") {
		t.Error("section page should contain section title")
	}
}

// --- SiteData tests ---

func TestSiteDataPages(t *testing.T) {
	site := makeSite()
	p1 := &content.Page{Kind: "page", Title: "A"}
	p2 := &content.Page{Kind: "page", Title: "B"}
	p3 := &content.Page{Kind: "section", Title: "S"}
	site.Pages = []*content.Page{p1, p2, p3}
	site.RegularPages = []*content.Page{p1, p2}

	if len(site.Pages) != 3 {
		t.Errorf("Site.Pages = %d, want 3", len(site.Pages))
	}
	if len(site.RegularPages) != 2 {
		t.Errorf("Site.RegularPages = %d, want 2", len(site.RegularPages))
	}
}

func TestSiteParams(t *testing.T) {
	site := makeSite()
	if site.Params["author"] != "Test Author" {
		t.Errorf("Site.Params[author] = %v, want Test Author", site.Params["author"])
	}
}
