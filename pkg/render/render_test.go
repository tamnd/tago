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
	whereFn := fm["where"].(func(...any) any)

	result, _ := whereFn(pages, "Kind", "page").([]*content.Page)
	if len(result) != 2 {
		t.Errorf("where Kind=page: got %d, want 2", len(result))
	}

	result2, _ := whereFn(pages, "Section", "docs").([]*content.Page)
	if len(result2) != 1 {
		t.Errorf("where Section=docs: got %d, want 1", len(result2))
	}
}

func TestFuncFirst(t *testing.T) {
	pages := []*content.Page{{Title: "A"}, {Title: "B"}, {Title: "C"}}
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	firstFn := fm["first"].(func(any, any) any)

	result := firstFn(2, pages).([]*content.Page)
	if len(result) != 2 {
		t.Errorf("first(2): got %d, want 2", len(result))
	}
	if result[0].Title != "A" {
		t.Errorf("first(2)[0] = %q, want A", result[0].Title)
	}

	// first with n > len should return all
	all := firstFn(10, pages).([]*content.Page) //nolint
	if len(all) != 3 {
		t.Errorf("first(10) of 3: got %d, want 3", len(all))
	}
}

func TestFuncLast(t *testing.T) {
	pages := []*content.Page{{Title: "A"}, {Title: "B"}, {Title: "C"}}
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	lastFn := fm["last"].(func(any, any) any)

	result := lastFn(2, pages).([]*content.Page)
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
	defaultFn := fm["default"].(func(...any) any)

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
	absURLFn := fm["absURL"].(func(any) string)

	got := absURLFn("/posts/hello/")
	if !strings.HasPrefix(got, "http://localhost:1313/") {
		t.Errorf("absURL = %q, should start with site BaseURL", got)
	}
}

func TestFuncRelURL(t *testing.T) {
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	relURLFn := fm["relURL"].(func(any) string)

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
	truncFn := fm["truncate"].(func(int, any) string)

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
	mdFn := fm["markdownify"].(func(any) template.HTML)

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
	sortFn := fm["sort"].(func(any, ...string) any)

	sorted := sortFn(pages, "Weight").([]*content.Page)
	if sorted[0].Title != "A" {
		t.Errorf("sort by Weight: first = %q, want A", sorted[0].Title)
	}

	sortedByTitle := sortFn(pages, "Title").([]*content.Page)
	if sortedByTitle[0].Title != "A" {
		t.Errorf("sort by Title: first = %q, want A", sortedByTitle[0].Title)
	}
}

func TestFuncJsonify(t *testing.T) {
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	jsonifyFn := fm["jsonify"].(func(...any) string)

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
	condFn := fm["cond"].(func(...any) any)

	if condFn(true, "yes", "no") != "yes" {
		t.Error("cond(true, ...) should return first value")
	}
	if condFn(false, "yes", "no") != "no" {
		t.Error("cond(false, ...) should return second value")
	}
	if condFn() != nil {
		t.Error("cond() with 0 args should return nil")
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
	site.AllPages = []*content.Page{p1, p2, p3}
	site.AllRegularPages = []*content.Page{p1, p2}

	if len(site.Pages()) != 3 {
		t.Errorf("Site.Pages = %d, want 3", len(site.Pages()))
	}
	if len(site.RegularPages()) != 2 {
		t.Errorf("Site.RegularPages = %d, want 2", len(site.RegularPages()))
	}
}

func TestSiteParams(t *testing.T) {
	site := makeSite()
	if site.Params["author"] != "Test Author" {
		t.Errorf("Site.Params[author] = %v, want Test Author", site.Params["author"])
	}
}

func TestFuncAppendPipeline(t *testing.T) {
	// Simulates the Ananke pattern: $slice | append $item
	// In Go templates, pipeline value is last arg: append($item, $slice)
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	appendFn := fm["append"].(func(any, ...any) hugoSlice)

	base := []any{"ma0", "avenir", "bg-near-white"}
	// pipeline: base | append "is-home" -> append("is-home", base)
	result := appendFn("is-home", base)
	if len(result) != 4 {
		t.Fatalf("append pipeline: got %d items, want 4: %v", len(result), result)
	}
	if result[3] != "is-home" {
		t.Errorf("append pipeline: last item = %v, want is-home", result[3])
	}

	// direct form: append(base, "is-home") - base is a slice
	result2 := appendFn(base, "is-home")
	if len(result2) != 4 {
		t.Fatalf("append direct: got %d items, want 4: %v", len(result2), result2)
	}
	if result2[3] != "is-home" {
		t.Errorf("append direct: last item = %v, want is-home", result2[3])
	}
}

func TestFuncWhere4Args(t *testing.T) {
	pages := []any{
		&HugoPage{Page: &content.Page{Kind: "page"}, Site: makeSite()},
		&HugoPage{Page: &content.Page{Kind: "section"}, Site: makeSite()},
	}
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	whereFn := fm["where"].(func(...any) any)

	// 4-arg form: where collection key op value
	result, _ := whereFn(pages, "Kind", "!=", "section").([]any)
	if len(result) != 1 {
		t.Errorf("where 4-arg !=: got %d, want 1", len(result))
	}
}

func TestFuncFindRE(t *testing.T) {
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	findFn := fm["findRE"].(func(string, any, ...any) []string)

	result := findFn(`\d+`, "foo 123 bar 456")
	if len(result) != 2 || result[0] != "123" || result[1] != "456" {
		t.Errorf("findRE: got %v, want [123 456]", result)
	}

	// with limit
	result2 := findFn(`\d+`, "foo 123 bar 456", 1)
	if len(result2) != 1 || result2[0] != "123" {
		t.Errorf("findRE limit 1: got %v, want [123]", result2)
	}
}

func TestTermsDataAlphabetical(t *testing.T) {
	td := &TermsData{terms: []TagCount{
		{Name: "zebra", Count: 5},
		{Name: "apple", Count: 2},
		{Name: "mango", Count: 8},
	}}

	entries := td.Alphabetical()
	if len(entries) != 3 {
		t.Fatalf("Alphabetical: got %d entries, want 3", len(entries))
	}
	if entries[0].Name != "apple" || entries[1].Name != "mango" || entries[2].Name != "zebra" {
		t.Errorf("Alphabetical order wrong: %v", entries)
	}

	by := td.ByCount()
	if by[0].Name != "mango" || by[0].Count != 8 {
		t.Errorf("ByCount first = %v, want mango/8", by[0])
	}
}

func TestGetNestedField(t *testing.T) {
	page := &content.Page{Kind: "page", Section: "posts"}
	page.Params = map[string]any{"draft": true, "weight": 10}

	if v := getNestedField(page, "Kind"); v != "page" {
		t.Errorf("getNestedField Kind = %v, want page", v)
	}
	if v := getNestedField(page, "Params"); v == nil {
		t.Error("getNestedField Params = nil, want map")
	}
}

func TestTimeNamespaceFormat(t *testing.T) {
	tn := &timeNamespace{}
	ts := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	result := tn.Format("2006-01-02", ts)
	if result != "2024-03-15" {
		t.Errorf("time.Format = %s, want 2024-03-15", result)
	}

	// with HugoTime
	ht := HugoTime{ts}
	result2 := tn.Format("January 2, 2006", ht)
	if result2 != "March 15, 2024" {
		t.Errorf("time.Format HugoTime = %s, want March 15, 2024", result2)
	}
}

// --- Hugo compatibility integration tests ---

// renderTmpl is declared but unused; kept as a helper for future tests.
var _ = func(t *testing.T, r *Renderer, tmplStr string, ctx any) string {
	return tmplStr
}

// TestHugoCompatStringsFuncs checks strings namespace: HasPrefix, HasSuffix, Contains, TrimSpace, ToLower, etc.
func TestHugoCompatStringsFuncs(t *testing.T) {
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	sn := fm["strings"].(func() *stringsNamespace)()

	if !sn.HasPrefix("foobar", "foo") {
		t.Error("strings.HasPrefix(foobar, foo) should be true")
	}
	if sn.HasPrefix("foobar", "bar") {
		t.Error("strings.HasPrefix(foobar, bar) should be false")
	}
	if !sn.HasSuffix("foobar", "bar") {
		t.Error("strings.HasSuffix(foobar, bar) should be true")
	}
	if !sn.Contains("hello world", "world") {
		t.Error("strings.Contains(hello world, world) should be true")
	}
	// nil input via anyToStr
	if sn.Contains(nil, "x") {
		t.Error("strings.Contains(nil, x) should be false")
	}
	if sn.HasPrefix(nil, "x") {
		t.Error("strings.HasPrefix(nil, x) should be false")
	}
	if sn.TrimLeft(".", ".hello") != "hello" {
		t.Errorf("strings.TrimLeft unexpected: %q", sn.TrimLeft(".", ".hello"))
	}
	if sn.TrimRight(".", "hello.") != "hello" {
		t.Errorf("strings.TrimRight unexpected: %q", sn.TrimRight(".", "hello."))
	}
}

// TestHugoCompatMathFuncs checks arithmetic with any types (int, float, nil, string-int).
func TestHugoCompatMathFuncs(t *testing.T) {
	r := makeRenderer(t)
	fm := r.buildFuncMap()

	add := fm["add"].(func(any, any) any)
	sub := fm["sub"].(func(any, any) any)
	mul := fm["mul"].(func(any, any) any)
	div := fm["div"].(func(any, any) any)

	if add(3, 4) != 7 {
		t.Errorf("add(3,4) = %v, want 7", add(3, 4))
	}
	if sub(10, 3) != 7 {
		t.Errorf("sub(10,3) = %v, want 7", sub(10, 3))
	}
	if mul(3, 4) != 12 {
		t.Errorf("mul(3,4) = %v, want 12", mul(3, 4))
	}
	if div(10, 2) != 5 {
		t.Errorf("div(10,2) = %v, want 5", div(10, 2))
	}
	// nil args should not panic and return zero
	divResult := div(nil, nil)
	if divResult != 0 && divResult != float64(0) && divResult != int(0) {
		t.Errorf("div(nil,nil) = %v (%T), want 0", divResult, divResult)
	}
	// interface{} int (from Scratch.Get)
	var v any = 5
	if add(v, 3) != 8 {
		t.Errorf("add(any(5), 3) = %v, want 8", add(v, 3))
	}
}

// TestHugoCompatSliceAndAppend tests hugoSlice behavior.
func TestHugoCompatSliceAndAppend(t *testing.T) {
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	sliceFn := fm["slice"].(func(...any) hugoSlice)
	appendFn := fm["append"].(func(any, ...any) hugoSlice)

	s := sliceFn("a", "b", "c")
	if len(s) != 3 {
		t.Errorf("slice len = %d, want 3", len(s))
	}
	rev := s.Reverse()
	if rev[0] != "c" || rev[2] != "a" {
		t.Errorf("slice.Reverse = %v, want [c b a]", rev)
	}
	s2 := appendFn(s, "d")
	if len(s2) != 4 || s2[3] != "d" {
		t.Errorf("append = %v, want [a b c d]", s2)
	}
}

// TestHugoCompatSafePageRef tests that safePageRef methods are nil-safe.
func TestHugoCompatSafePageRef(t *testing.T) {
	nilRef := &safePageRef{page: nil}
	if nilRef.RelPermalink() != "" {
		t.Error("safePageRef nil: RelPermalink should be empty")
	}
	if nilRef.Title() != "" {
		t.Error("safePageRef nil: Title should be empty")
	}
	if nilRef.Layout() != "" {
		t.Error("safePageRef nil: Layout should be empty")
	}

	page := &content.Page{
		RelPermalink: "/posts/hello/",
		Title:        "Hello World",
		Layout:       "single",
	}
	ref := &safePageRef{page: page}
	if ref.RelPermalink() != "/posts/hello/" {
		t.Errorf("safePageRef.RelPermalink = %q, want /posts/hello/", ref.RelPermalink())
	}
	if ref.Layout() != "single" {
		t.Errorf("safePageRef.Layout = %q, want single", ref.Layout())
	}
}

// TestHugoCompatResourcesRangeable ensures Resources() returns a type rangeable in templates.
func TestHugoCompatResourcesRangeable(t *testing.T) {
	page := &content.Page{Title: "Test", RelPermalink: "/test/"}
	site := makeSite()
	hp := &HugoPage{Page: page, Site: site}

	res := hp.Resources()
	// Should be nil (falsy), not panic in range
	if res != nil {
		t.Errorf("HugoPage.Resources() should be nil, got %v", res)
	}
	// Ensure ByType and Match return nil (rangeable in template range)
	if res.ByType("image") != nil {
		t.Error("nil stubResourceSlice.ByType should return nil")
	}
	if res.Match("*") != nil {
		t.Error("nil stubResourceSlice.Match should return nil")
	}
}

// TestHugoCompatTermEntryPages tests that TermEntry has Pages() method.
func TestHugoCompatTermEntryPages(t *testing.T) {
	entry := TermEntry{Name: "go", Count: 5, Term: "go"}
	pages := entry.Pages()
	if pages != nil {
		t.Errorf("TermEntry.Pages() should be nil, got %v", pages)
	}
}

// TestHugoCompatSiteHomeStubPage tests siteHomeStub.Page() returns itself.
func TestHugoCompatSiteHomeStubPage(t *testing.T) {
	stub := &siteHomeStub{baseURL: "http://example.com/"}
	p := stub.Page()
	if p != stub {
		t.Error("siteHomeStub.Page() should return self")
	}
	if stub.Resources() != nil {
		t.Error("siteHomeStub.Resources() should be nil")
	}
}

// TestHugoCompatFormatDate checks formatDate accepts both time.Time and HugoTime.
func TestHugoCompatFormatDate(t *testing.T) {
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	fd := fm["formatDate"].(func(any) string)

	ts := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	result := fd(ts)
	if !strings.Contains(result, "2024") {
		t.Errorf("formatDate(time.Time) = %q, should contain 2024", result)
	}
	result2 := fd(HugoTime{ts})
	if !strings.Contains(result2, "2024") {
		t.Errorf("formatDate(HugoTime) = %q, should contain 2024", result2)
	}
	// nil should not panic
	result3 := fd(nil)
	if result3 != "" {
		t.Errorf("formatDate(nil) = %q, want empty", result3)
	}
}

// TestHugoCompatHugoSliceReverse tests hugoSlice.Reverse on empty and non-empty slices.
func TestHugoCompatHugoSliceReverse(t *testing.T) {
	var empty hugoSlice
	rev := empty.Reverse()
	if len(rev) != 0 {
		t.Errorf("empty.Reverse() len = %d, want 0", len(rev))
	}

	s := hugoSlice{1, 2, 3}
	rev = s.Reverse()
	if rev[0] != 3 || rev[1] != 2 || rev[2] != 1 {
		t.Errorf("Reverse = %v, want [3 2 1]", rev)
	}
}

// TestHugoCompatPrintfNilFormat checks printf does not panic on nil format.
func TestHugoCompatPrintfNilFormat(t *testing.T) {
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	pf := fm["printf"].(func(any, ...any) string)

	result := pf(nil)
	if result != "" {
		t.Errorf("printf(nil) = %q, want empty", result)
	}
	result2 := pf("hello %s", "world")
	if result2 != "hello world" {
		t.Errorf("printf(hello %%s, world) = %q, want 'hello world'", result2)
	}
}

// TestHugoCompatReplaceAcceptsNil checks replace does not panic with nil inputs.
func TestHugoCompatReplaceAcceptsNil(t *testing.T) {
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	replaceFn := fm["replace"].(func(any, any, any) string)

	if got := replaceFn(nil, "a", "b"); got != "" {
		t.Errorf("replace(nil, a, b) = %q, want empty", got)
	}
	if got := replaceFn("hello", nil, "world"); got != "hello" {
		t.Errorf("replace(hello, nil, world) = %q, want hello", got)
	}
	if got := replaceFn("hello", "hello", nil); got != "" {
		t.Errorf("replace(hello, hello, nil) = %q, want empty", got)
	}
}

// TestHugoCompatSlicestr tests the slicestr function.
func TestHugoCompatSlicestr(t *testing.T) {
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	slicestrFn := fm["slicestr"].(func(any, any, ...any) string)

	if got := slicestrFn("Hello World", 0, 5); got != "Hello" {
		t.Errorf("slicestr(Hello World, 0, 5) = %q, want Hello", got)
	}
	if got := slicestrFn("Hello", 2); got != "llo" {
		t.Errorf("slicestr(Hello, 2) = %q, want llo", got)
	}
}

// TestHugoCompatSiteDataGetPage checks .Site.GetPage returns a siteHomeStub.
func TestHugoCompatSiteDataGetPage(t *testing.T) {
	site := makeSite()
	result := site.GetPage("homepage")
	if result == nil {
		t.Error("Site.GetPage should not return nil")
	}
	if result.IsHome() != true {
		t.Error("Site.GetPage('homepage').IsHome() should be true")
	}
}

// TestHugoCompatReflectIsSliceNilSafe tests that reflect.IsSlice does not panic on nil.
func TestHugoCompatReflectIsSliceNilSafe(t *testing.T) {
	rn := &reflectNamespace{}
	// nil should not panic
	if rn.IsSlice(nil) {
		t.Error("reflect.IsSlice(nil) should be false")
	}
	if !rn.IsSlice([]string{"a"}) {
		t.Error("reflect.IsSlice([]string) should be true")
	}
	if rn.IsSlice("string") {
		t.Error("reflect.IsSlice(string) should be false")
	}
}

// TestHugoCompatHugoPageList tests HugoPageList methods: Limit, Related, GroupByPublishDate.
func TestHugoCompatHugoPageList(t *testing.T) {
	site := makeSite()
	pages := HugoPageList{
		{Page: &content.Page{Title: "A"}, Site: site},
		{Page: &content.Page{Title: "B"}, Site: site},
		{Page: &content.Page{Title: "C"}, Site: site},
	}

	limited := pages.Limit(2)
	if len(limited) != 2 {
		t.Errorf("Limit(2) len = %d, want 2", len(limited))
	}
	related := pages.Related(nil)
	if related != nil {
		t.Errorf("Related should return nil, got %v", related)
	}
	groups := pages.GroupByPublishDate("2006")
	_ = groups // just ensure no panic
	groups2 := pages.GroupByParam("category")
	_ = groups2
}

// TestHugoCompatInlineDefine tests that partial calls find inline {{ define }} blocks.
func TestHugoCompatInlineDefine(t *testing.T) {
	dir := t.TempDir()
	layoutsDir := filepath.Join(dir, "layouts")
	partialsDir := filepath.Join(layoutsDir, "partials")
	if err := os.MkdirAll(partialsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a partial that has an inline {{ define }} block and calls it.
	partialSrc := `{{ define "partials/helper" }}{{ return (dict "val" "hello") }}{{ end -}}
{{- $result := partial "helper" . -}}
{{- $result.val -}}`
	if err := os.WriteFile(filepath.Join(partialsDir, "mypartial.html"), []byte(partialSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	r := makeRendererWithLayouts(t, layoutsDir)
	out, err := r.renderPartialAny("mypartial", nil)
	if err != nil {
		t.Fatalf("renderPartialAny error: %v", err)
	}
	if string(out.(template.HTML)) != "hello" {
		t.Errorf("inline define result = %q, want hello", out)
	}
}
