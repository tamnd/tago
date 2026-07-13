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
	return New(makeSite(), AssetRefs{}, "", false, 0, "")
}

func makeRendererWithLayouts(t *testing.T, layoutsDir string) *Renderer {
	t.Helper()
	return New(makeSite(), AssetRefs{}, layoutsDir, false, 0, "")
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
	seqFn := fm["seq"].(func(...any) []int)

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

// TestHugoCompatPartialReturnDict tests partial {{ return (dict ...) }} returning a map.
func TestHugoCompatPartialReturnDict(t *testing.T) {
	dir := t.TempDir()
	layoutsDir := filepath.Join(dir, "layouts")
	partialsDir := filepath.Join(layoutsDir, "partials")
	if err := os.MkdirAll(partialsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	dictPartial := `{{ $a := "alpha" }}{{ $b := "beta" }}{{ return (dict "first" $a "second" $b) }}`
	if err := os.WriteFile(filepath.Join(partialsDir, "getpair.html"), []byte(dictPartial), 0o644); err != nil {
		t.Fatal(err)
	}

	callerPartial := `{{- $p := partial "getpair" . -}}{{ $p.first }}-{{ $p.second }}`
	if err := os.WriteFile(filepath.Join(partialsDir, "caller.html"), []byte(callerPartial), 0o644); err != nil {
		t.Fatal(err)
	}

	r := makeRendererWithLayouts(t, layoutsDir)
	out, err := r.renderPartialAny("caller", nil)
	if err != nil {
		t.Fatalf("renderPartialAny error: %v", err)
	}
	if string(out.(template.HTML)) != "alpha-beta" {
		t.Errorf("partial return dict = %q, want alpha-beta", out)
	}
}

// TestHugoCompatPartialReturnSlice tests partial {{ return (slice ...) }} returning a slice.
func TestHugoCompatPartialReturnSlice(t *testing.T) {
	dir := t.TempDir()
	layoutsDir := filepath.Join(dir, "layouts")
	partialsDir := filepath.Join(layoutsDir, "partials")
	if err := os.MkdirAll(partialsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	slicePartial := `{{ return (slice "a" "b" "c") }}`
	if err := os.WriteFile(filepath.Join(partialsDir, "getslice.html"), []byte(slicePartial), 0o644); err != nil {
		t.Fatal(err)
	}

	callerPartial := `{{- $s := partial "getslice" . -}}{{ range $s }}{{ . }}{{ end }}`
	if err := os.WriteFile(filepath.Join(partialsDir, "caller2.html"), []byte(callerPartial), 0o644); err != nil {
		t.Fatal(err)
	}

	r := makeRendererWithLayouts(t, layoutsDir)
	out, err := r.renderPartialAny("caller2", nil)
	if err != nil {
		t.Fatalf("renderPartialAny error: %v", err)
	}
	if string(out.(template.HTML)) != "abc" {
		t.Errorf("partial return slice = %q, want abc", out)
	}
}

// TestHugoCompatScratchAdd tests Scratch.Add accumulation.
func TestHugoCompatScratchAdd(t *testing.T) {
	s := newScratch()
	s.Set("count", 0)
	s.Add("count", 1)
	s.Add("count", 1)
	s.Add("count", 1)
	if v, ok := s.Get("count").(int); !ok || v != 3 {
		t.Errorf("Scratch.Add int: got %v, want 3", s.Get("count"))
	}

	s.Set("name", "hello")
	s.Add("name", " world")
	if v, ok := s.Get("name").(string); !ok || v != "hello world" {
		t.Errorf("Scratch.Add string: got %v, want 'hello world'", s.Get("name"))
	}
}

// TestHugoCompatScratchSetInMap tests Scratch.SetInMap and GetSortedMapValues.
func TestHugoCompatScratchSetInMap(t *testing.T) {
	s := newScratch()
	s.SetInMap("nav", "about", "About Us")
	s.SetInMap("nav", "home", "Home")
	s.SetInMap("nav", "blog", "Blog")

	vals := s.GetSortedMapValues("nav")
	if len(vals) != 3 {
		t.Errorf("GetSortedMapValues len = %d, want 3", len(vals))
	}
	// Should be sorted by key: about, blog, home
	if vals[0] != "About Us" {
		t.Errorf("GetSortedMapValues[0] = %v, want 'About Us'", vals[0])
	}
}

// TestHugoCompatWhereOperators tests the where function with various operators.
func TestHugoCompatWhereOperators(t *testing.T) {
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	whereFn := fm["where"].(func(...any) any)

	site := makeSite()
	pages := HugoPageList{
		{Page: &content.Page{Title: "A", Weight: 1}, Site: site},
		{Page: &content.Page{Title: "B", Weight: 5}, Site: site},
		{Page: &content.Page{Title: "C", Weight: 10}, Site: site},
	}

	// Filter with != operator
	result := whereFn(pages, "Title", "!=", "A")
	if list, ok := result.(HugoPageList); !ok || len(list) != 2 {
		t.Errorf("where Title != A: got %v, want 2 items", result)
	}

	// Filter with in operator
	result2 := whereFn(pages, "Title", "in", []string{"A", "C"})
	if list, ok := result2.(HugoPageList); !ok || len(list) != 2 {
		t.Errorf("where Title in [A,C]: got %v, want 2 items", result2)
	}
}

// TestHugoCompatDictAndMerge tests dict and merge functions.
func TestHugoCompatDictAndMerge(t *testing.T) {
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	dictFn := fm["dict"].(func(...any) map[string]any)
	mergeFn := fm["merge"].(func(...any) map[string]any)

	d := dictFn("key", "value", "num", 42)
	if d["key"] != "value" {
		t.Errorf("dict[key] = %v, want value", d["key"])
	}
	if d["num"] != 42 {
		t.Errorf("dict[num] = %v, want 42", d["num"])
	}

	merged := mergeFn(d, dictFn("extra", "bonus"))
	if merged["key"] != "value" || merged["extra"] != "bonus" {
		t.Errorf("merge result unexpected: %v", merged)
	}
}

// TestHugoCompatCond tests the cond function.
func TestHugoCompatCond(t *testing.T) {
	r := makeRenderer(t)
	fm := r.buildFuncMap()
	condFn := fm["cond"].(func(...any) any)

	if condFn(true, "yes", "no") != "yes" {
		t.Error("cond(true, yes, no) should be yes")
	}
	if condFn(false, "yes", "no") != "no" {
		t.Error("cond(false, yes, no) should be no")
	}
}

// TestHugoCompatHugoPageMethods tests HugoPage accessor methods.
func TestHugoCompatHugoPageMethods(t *testing.T) {
	site := makeSite()
	ts := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	page := &content.Page{
		Title:       "Test Post",
		Kind:        "page",
		Section:     "posts",
		RelPermalink: "/posts/test-post/",
		Tags:        []string{"go", "hugo"},
		Date:        ts,
	}
	hp := &HugoPage{Page: page, Site: site}

	if hp.Title != "Test Post" {
		t.Errorf("HugoPage.Title = %q, want 'Test Post'", hp.Title)
	}
	if hp.RelPermalink() != "/posts/test-post/" {
		t.Errorf("HugoPage.RelPermalink() = %q, want /posts/test-post/", hp.RelPermalink())
	}
	if !hp.IsPage() {
		t.Error("HugoPage.IsPage() should be true for kind=page")
	}
	if hp.IsHome() {
		t.Error("HugoPage.IsHome() should be false for kind=page")
	}
	kws := hp.Keywords()
	if len(kws) != 2 || kws[0] != "go" {
		t.Errorf("HugoPage.Keywords() = %v, want [go hugo]", kws)
	}
	if hp.HasShortcode("foo") {
		t.Error("HugoPage.HasShortcode should always return false")
	}
	if hp.GetPage("some/path") != nil {
		t.Error("HugoPage.GetPage should return nil")
	}
}

// TestHugoCompatTaxonomyList tests site taxonomy access.
func TestHugoCompatTaxonomyList(t *testing.T) {
	site := makeSite()
	site.AllRegularPages = []*content.Page{
		{Title: "Page 1", Tags: []string{"go", "test"}},
		{Title: "Page 2", Tags: []string{"go"}},
		{Title: "Page 3", Tags: []string{"test"}},
	}

	taxRaw := site.Taxonomies()
	tagsRaw, ok := taxRaw["tags"]
	if !ok {
		t.Fatal("Taxonomies should have 'tags' key")
	}
	tags, ok2 := tagsRaw.(map[string]TermEntry)
	if !ok2 {
		t.Fatalf("tags type = %T, want map[string]TermEntry", tagsRaw)
	}
	if tags["go"].Count != 2 {
		t.Errorf("tags[go].Count = %d, want 2", tags["go"].Count)
	}
	if tags["test"].Count != 2 {
		t.Errorf("tags[test].Count = %d, want 2", tags["test"].Count)
	}

	// TermEntry should have Name and Count
	entry := tags["go"]
	if entry.Name != "go" {
		t.Errorf("TermEntry.Name = %q, want go", entry.Name)
	}
	if entry.Term != "go" {
		t.Errorf("TermEntry.Term = %q, want go", entry.Term)
	}
	// Pages and Page methods should not panic
	_ = entry.Pages()
	_ = entry.Page()
}

// TestHugoCompatSiteMenus tests site menu access.
func TestHugoCompatSiteMenus(t *testing.T) {
	site := makeSite()
	site.Menus = map[string][]SiteMenuItem{
		"main": {
			{Name: "Home", URL: "/", Weight: 1},
			{Name: "About", URL: "/about/", Weight: 2},
		},
	}

	menus := site.Menus
	main, ok := menus["main"]
	if !ok {
		t.Fatal("Menus should have 'main' key")
	}
	if len(main) != 2 {
		t.Errorf("main menu len = %d, want 2", len(main))
	}
	if main[0].Name != "Home" {
		t.Errorf("main[0].Name = %q, want Home", main[0].Name)
	}
}

// TestHugoCompatHugoPageListSorting tests HugoPageList sorting methods.
func TestHugoCompatHugoPageListSorting(t *testing.T) {
	site := makeSite()
	ts1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ts2 := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	ts3 := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	pages := HugoPageList{
		{Page: &content.Page{Title: "Banana", Weight: 2, Date: ts2}, Site: site},
		{Page: &content.Page{Title: "Apple", Weight: 1, Date: ts1}, Site: site},
		{Page: &content.Page{Title: "Cherry", Weight: 3, Date: ts3}, Site: site},
	}

	byTitle := pages.ByTitle()
	if byTitle[0].Title != "Apple" {
		t.Errorf("ByTitle[0] = %q, want Apple", byTitle[0].Title)
	}

	byWeight := pages.ByWeight()
	if byWeight[0].Title != "Apple" {
		t.Errorf("ByWeight[0] = %q, want Apple", byWeight[0].Title)
	}

	reversed := byTitle.Reverse()
	if reversed[0].Title != "Cherry" {
		t.Errorf("Reverse[0] = %q, want Cherry", reversed[0].Title)
	}

	first2 := pages.First(2)
	if len(first2) != 2 {
		t.Errorf("First(2) len = %d, want 2", len(first2))
	}

	last1 := pages.Last(1)
	if len(last1) != 1 {
		t.Errorf("Last(1) len = %d, want 1", len(last1))
	}
}

// --- Integration tests: actual template rendering ---

// renderPartialStr is a test helper that renders a partial template string using a temp layout dir.
func renderPartialStr(t *testing.T, tmplSrc string, ctx any) string {
	t.Helper()
	dir := t.TempDir()
	layoutsDir := filepath.Join(dir, "layouts")
	partialsDir := filepath.Join(layoutsDir, "partials")
	if err := os.MkdirAll(partialsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partialsDir, "test.html"), []byte(tmplSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	r := makeRendererWithLayouts(t, layoutsDir)
	out, err := r.renderPartialAny("test", ctx)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}
	switch v := out.(type) {
	case template.HTML:
		return strings.TrimSpace(string(v))
	case string:
		return strings.TrimSpace(v)
	}
	return ""
}

// TestIntegrationRangePages tests ranging over .Pages in a template.
func TestIntegrationRangePages(t *testing.T) {
	site := makeSite()
	site.AllRegularPages = []*content.Page{
		{Title: "Post A", Kind: "page", Section: "posts", RelPermalink: "/posts/a/"},
		{Title: "Post B", Kind: "page", Section: "posts", RelPermalink: "/posts/b/"},
	}

	tmpl := `{{ range .Site.RegularPages }}{{ .Title }},{{ end }}`
	got := renderPartialStr(t, tmpl, &TemplateData{
		Page: &content.Page{Kind: "home"},
		Site: site,
	})
	if !strings.Contains(got, "Post A") || !strings.Contains(got, "Post B") {
		t.Errorf("range .Site.RegularPages = %q, should contain Post A and Post B", got)
	}
}

// TestIntegrationWhereFilter tests where filter in templates.
func TestIntegrationWhereFilter(t *testing.T) {
	site := makeSite()
	site.AllRegularPages = []*content.Page{
		{Title: "Draft Post", Kind: "page", Draft: true},
		{Title: "Published Post", Kind: "page", Draft: false},
	}

	tmpl := `{{ range where .Site.RegularPages "Draft" false }}{{ .Title }}{{ end }}`
	got := renderPartialStr(t, tmpl, &TemplateData{
		Page: &content.Page{Kind: "home"},
		Site: site,
	})
	if !strings.Contains(got, "Published Post") {
		t.Errorf("where Draft=false should include Published Post, got: %q", got)
	}
	if strings.Contains(got, "Draft Post") {
		t.Errorf("where Draft=false should not include Draft Post, got: %q", got)
	}
}

// TestIntegrationScratchInTemplate tests Scratch.Set/Get in template rendering.
func TestIntegrationScratchInTemplate(t *testing.T) {
	tmpl := `{{ .Scratch.Set "msg" "hello" }}{{ .Scratch.Get "msg" }}`
	page := &content.Page{Kind: "page", Title: "Test"}
	site := makeSite()
	got := renderPartialStr(t, tmpl, &TemplateData{
		Page: page,
		Site: site,
	})
	if got != "hello" {
		t.Errorf("Scratch.Set/Get in template = %q, want hello", got)
	}
}

// TestIntegrationScratchAdd tests Scratch.Add accumulation in template.
func TestIntegrationScratchAdd(t *testing.T) {
	tmpl := `{{ .Scratch.Set "n" 0 }}{{ .Scratch.Add "n" 1 }}{{ .Scratch.Add "n" 2 }}{{ .Scratch.Get "n" }}`
	page := &content.Page{Kind: "page"}
	site := makeSite()
	got := renderPartialStr(t, tmpl, &TemplateData{Page: page, Site: site})
	if got != "3" {
		t.Errorf("Scratch.Add in template = %q, want 3", got)
	}
}

// TestIntegrationPartialReturn tests {{ return }} in partials from template execution.
func TestIntegrationPartialReturn(t *testing.T) {
	dir := t.TempDir()
	layoutsDir := filepath.Join(dir, "layouts")
	partialsDir := filepath.Join(layoutsDir, "partials")
	if err := os.MkdirAll(partialsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Partial that returns a value
	if err := os.WriteFile(filepath.Join(partialsDir, "getval.html"), []byte(`{{ return "computed" }}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Caller that uses the returned value
	if err := os.WriteFile(filepath.Join(partialsDir, "caller.html"), []byte(`{{ $v := partial "getval" . }}result:{{ $v }}`), 0o644); err != nil {
		t.Fatal(err)
	}

	r := makeRendererWithLayouts(t, layoutsDir)
	out, err := r.renderPartialAny("caller", nil)
	if err != nil {
		t.Fatalf("renderPartialAny error: %v", err)
	}
	if string(out.(template.HTML)) != "result:computed" {
		t.Errorf("partial return = %q, want result:computed", out)
	}
}

// TestIntegrationHugoTimeFormat tests date formatting in templates.
func TestIntegrationHugoTimeFormat(t *testing.T) {
	ts := time.Date(2024, 3, 15, 12, 30, 0, 0, time.UTC)
	page := &content.Page{Kind: "page", Date: ts}
	site := makeSite()

	tmpl := `{{ .Date.Format "2006-01-02" }}`
	got := renderPartialStr(t, tmpl, &TemplateData{Page: page, Site: site})
	if got != "2024-03-15" {
		t.Errorf("Date.Format = %q, want 2024-03-15", got)
	}

	tmpl2 := `{{ .Date.Format ":date_long" }}`
	got2 := renderPartialStr(t, tmpl2, &TemplateData{Page: page, Site: site})
	if !strings.Contains(got2, "2024") || !strings.Contains(got2, "March") {
		t.Errorf("Date.Format :date_long = %q, should contain March 2024", got2)
	}
}

// TestIntegrationSiteParams tests accessing site params in templates.
func TestIntegrationSiteParams(t *testing.T) {
	site := makeSite()
	site.Params["brand"] = "MyBlog"
	site.Params["colors"] = []string{"red", "blue"}

	page := &content.Page{Kind: "home"}
	tmpl := `{{ .Site.Params.brand }}`
	got := renderPartialStr(t, tmpl, &TemplateData{Page: page, Site: site})
	if got != "MyBlog" {
		t.Errorf("Site.Params.brand = %q, want MyBlog", got)
	}
}

// TestIntegrationPageParam tests .Param with site fallback.
func TestIntegrationPageParam(t *testing.T) {
	site := makeSite()
	site.Params["sitewide"] = "fromsite"

	page := &content.Page{
		Kind:   "page",
		Params: map[string]any{"local": "frompage"},
	}

	tmpl := `{{ .Param "local" }}/{{ .Param "sitewide" }}`
	got := renderPartialStr(t, tmpl, &TemplateData{Page: page, Site: site})
	if got != "frompage/fromsite" {
		t.Errorf(".Param = %q, want frompage/fromsite", got)
	}
}

// TestIntegrationConditionalWith tests {{ with }} and {{ if }} with nil values.
func TestIntegrationConditionalWith(t *testing.T) {
	site := makeSite()
	page := &content.Page{Kind: "page", Description: "A post"}

	// {{ with }} on non-empty string
	tmpl := `{{ with .Description }}yes{{ else }}no{{ end }}`
	got := renderPartialStr(t, tmpl, &TemplateData{Page: page, Site: site})
	if got != "yes" {
		t.Errorf("with .Description = %q, want yes", got)
	}

	// {{ with }} on nil Resources (should be falsy)
	tmpl2 := `{{ with .Resources }}has-resources{{ else }}no-resources{{ end }}`
	got2 := renderPartialStr(t, tmpl2, &TemplateData{Page: page, Site: site})
	if got2 != "no-resources" {
		t.Errorf("with .Resources = %q, want no-resources (nil slice is falsy)", got2)
	}
}

// TestIntegrationRangeResources tests {{ range .Resources }} does not panic.
func TestIntegrationRangeResources(t *testing.T) {
	site := makeSite()
	page := &content.Page{Kind: "page"}
	tmpl := `{{ $count := 0 }}{{ range .Resources }}{{ $count = add $count 1 }}{{ end }}count={{ $count }}`
	got := renderPartialStr(t, tmpl, &TemplateData{Page: page, Site: site})
	if got != "count=0" {
		t.Errorf("range .Resources = %q, want count=0", got)
	}
}

// TestIntegrationFirstLast tests first/last functions in templates.
func TestIntegrationFirstLast(t *testing.T) {
	site := makeSite()
	site.AllRegularPages = []*content.Page{
		{Title: "A", Kind: "page"},
		{Title: "B", Kind: "page"},
		{Title: "C", Kind: "page"},
		{Title: "D", Kind: "page"},
		{Title: "E", Kind: "page"},
	}

	page := &content.Page{Kind: "home"}
	tmpl := `{{ range first 3 .Site.RegularPages }}{{ .Title }}{{ end }}`
	got := renderPartialStr(t, tmpl, &TemplateData{Page: page, Site: site})
	if got != "ABC" {
		t.Errorf("first 3 pages = %q, want ABC", got)
	}

	tmpl2 := `{{ range last 2 .Site.RegularPages }}{{ .Title }}{{ end }}`
	got2 := renderPartialStr(t, tmpl2, &TemplateData{Page: page, Site: site})
	if got2 != "DE" {
		t.Errorf("last 2 pages = %q, want DE", got2)
	}
}

// TestIntegrationSliceAndRange tests slice + range with .Reverse.
func TestIntegrationSliceAndRange(t *testing.T) {
	tmpl := `{{ $s := slice "a" "b" "c" }}{{ range $s.Reverse }}{{ . }}{{ end }}`
	site := makeSite()
	got := renderPartialStr(t, tmpl, &TemplateData{
		Page: &content.Page{Kind: "page"},
		Site: site,
	})
	if got != "cba" {
		t.Errorf("slice.Reverse range = %q, want cba", got)
	}
}

// TestIntegrationDictAccess tests dict creation and field access in templates.
func TestIntegrationDictAccess(t *testing.T) {
	tmpl := `{{ $d := dict "name" "alice" "age" 30 }}{{ $d.name }}/{{ $d.age }}`
	site := makeSite()
	got := renderPartialStr(t, tmpl, &TemplateData{
		Page: &content.Page{Kind: "page"},
		Site: site,
	})
	if got != "alice/30" {
		t.Errorf("dict access = %q, want alice/30", got)
	}
}

// TestIntegrationStringsFuncs tests strings namespace functions in templates.
func TestIntegrationStringsFuncs(t *testing.T) {
	site := makeSite()
	ctx := &TemplateData{Page: &content.Page{Kind: "page"}, Site: site}

	tests := []struct {
		tmpl string
		want string
	}{
		{`{{ strings.HasPrefix "foobar" "foo" }}`, "true"},
		{`{{ strings.HasSuffix "foobar" "bar" }}`, "true"},
		{`{{ strings.Contains "hello world" "world" }}`, "true"},
		{`{{ lower "HELLO" }}`, "hello"},
		{`{{ upper "hello" }}`, "HELLO"},
		{`{{ trim " hello " " " }}`, "hello"},
		{`{{ replace "hello" "l" "r" }}`, "herro"},
		{`{{ len "hello" }}`, "5"},
	}

	for _, tt := range tests {
		t.Run(tt.tmpl, func(t *testing.T) {
			got := renderPartialStr(t, tt.tmpl, ctx)
			if got != tt.want {
				t.Errorf("template %q = %q, want %q", tt.tmpl, got, tt.want)
			}
		})
	}
}

// TestIntegrationMathFuncs tests math operations in templates.
func TestIntegrationMathFuncs(t *testing.T) {
	site := makeSite()
	ctx := &TemplateData{Page: &content.Page{Kind: "page"}, Site: site}

	tests := []struct {
		tmpl string
		want string
	}{
		{`{{ add 3 4 }}`, "7"},
		{`{{ sub 10 3 }}`, "7"},
		{`{{ mul 3 4 }}`, "12"},
		{`{{ div 10 2 }}`, "5"},
		{`{{ mod 7 3 }}`, "1"},
		{`{{ modBool 6 3 }}`, "true"},
		{`{{ modBool 7 3 }}`, "false"},
	}

	for _, tt := range tests {
		t.Run(tt.tmpl, func(t *testing.T) {
			got := renderPartialStr(t, tt.tmpl, ctx)
			if got != tt.want {
				t.Errorf("template %q = %q, want %q", tt.tmpl, got, tt.want)
			}
		})
	}
}

// TestIntegrationSiteTitle tests accessing .Site.Title in a template.
func TestIntegrationSiteTitle(t *testing.T) {
	site := makeSite()
	tmpl := `{{ .Site.Title }}`
	got := renderPartialStr(t, tmpl, &TemplateData{
		Page: &content.Page{Kind: "home"},
		Site: site,
	})
	if got != "Test Site" {
		t.Errorf(".Site.Title = %q, want 'Test Site'", got)
	}
}

// TestIntegrationPageTitle tests accessing page title fields.
func TestIntegrationPageTitle(t *testing.T) {
	site := makeSite()
	page := &content.Page{Kind: "page", Title: "My Article"}
	tmpl := `{{ .Title }}`
	got := renderPartialStr(t, tmpl, &TemplateData{Page: page, Site: site})
	if got != "My Article" {
		t.Errorf(".Title = %q, want 'My Article'", got)
	}
}

// TestIntegrationRangeMenu tests ranging over .Site.Menus.main.
func TestIntegrationRangeMenu(t *testing.T) {
	site := makeSite()
	site.Menus = map[string][]SiteMenuItem{
		"main": {
			{Name: "Home", URL: "/"},
			{Name: "About", URL: "/about/"},
		},
	}
	tmpl := `{{ range .Site.Menus.main }}{{ .Name }}:{{ .URL }},{{ end }}`
	got := renderPartialStr(t, tmpl, &TemplateData{
		Page: &content.Page{Kind: "home"},
		Site: site,
	})
	if !strings.Contains(got, "Home:/") || !strings.Contains(got, "About:/about/") {
		t.Errorf("range .Site.Menus.main = %q, want Home and About entries", got)
	}
}

// TestIntegrationSiteTaxonomies tests accessing .Site.Taxonomies in a template.
func TestIntegrationSiteTaxonomies(t *testing.T) {
	site := makeSite()
	site.AllRegularPages = []*content.Page{
		{Title: "A", Tags: []string{"go", "web"}},
		{Title: "B", Tags: []string{"go"}},
	}
	tmpl := `{{ range $name, $entry := .Site.Taxonomies.tags }}{{ $name }}:{{ $entry.Count }},{{ end }}`
	got := renderPartialStr(t, tmpl, &TemplateData{
		Page: &content.Page{Kind: "taxonomy"},
		Site: site,
	})
	if !strings.Contains(got, "go:2") {
		t.Errorf("Site.Taxonomies.tags = %q, should contain go:2", got)
	}
	if !strings.Contains(got, "web:1") {
		t.Errorf("Site.Taxonomies.tags = %q, should contain web:1", got)
	}
}

// TestIntegrationMarkdownify tests markdownify function in templates.
func TestIntegrationMarkdownify(t *testing.T) {
	site := makeSite()
	page := &content.Page{Kind: "page"}
	tmpl := `{{ markdownify "**bold** text" }}`
	got := renderPartialStr(t, tmpl, &TemplateData{Page: page, Site: site})
	if !strings.Contains(got, "strong") && !strings.Contains(got, "bold") {
		t.Errorf("markdownify = %q, should contain strong or bold markup", got)
	}
}

// TestIntegrationSafeHTML tests safeHTML function in templates.
func TestIntegrationSafeHTML(t *testing.T) {
	site := makeSite()
	page := &content.Page{Kind: "page"}
	tmpl := `{{ "<strong>test</strong>" | safeHTML }}`
	got := renderPartialStr(t, tmpl, &TemplateData{Page: page, Site: site})
	if !strings.Contains(got, "<strong>test</strong>") {
		t.Errorf("safeHTML = %q, should pass through raw HTML", got)
	}
}

// TestIntegrationDefault tests the default function.
func TestIntegrationDefault(t *testing.T) {
	site := makeSite()
	page := &content.Page{Kind: "page"}
	ctx := &TemplateData{Page: page, Site: site}

	tmpl := `{{ default "fallback" .Params.missing }}`
	got := renderPartialStr(t, tmpl, ctx)
	if got != "fallback" {
		t.Errorf("default with missing param = %q, want fallback", got)
	}

	page.Params = map[string]any{"key": "value"}
	tmpl2 := `{{ default "fallback" .Params.key }}`
	got2 := renderPartialStr(t, tmpl2, ctx)
	if got2 != "value" {
		t.Errorf("default with set param = %q, want value", got2)
	}
}

// TestIntegrationAbsURL tests absURL and relURL functions.
func TestIntegrationAbsURL(t *testing.T) {
	site := makeSite()
	page := &content.Page{Kind: "page"}
	ctx := &TemplateData{Page: page, Site: site}

	tmpl := `{{ absURL "/posts/hello/" }}`
	got := renderPartialStr(t, tmpl, ctx)
	if !strings.HasPrefix(got, "http://localhost:1313/") {
		t.Errorf("absURL = %q, should start with baseURL", got)
	}

	tmpl2 := `{{ relURL "/posts/hello/" }}`
	got2 := renderPartialStr(t, tmpl2, ctx)
	if !strings.HasPrefix(got2, "/") {
		t.Errorf("relURL = %q, should start with /", got2)
	}
}

// TestIntegrationPreprocessReturnRewrite tests that {{ return funcname args }} is rewritten.
func TestIntegrationPreprocessReturnRewrite(t *testing.T) {
	// "{{ return upper .Title }}" should be rewritten to "{{ return (upper .Title) }}"
	src := `{{ return upper .Title }}`
	result := preprocessTemplate(src)
	if !strings.Contains(result, "(upper .Title)") {
		t.Errorf("preprocessTemplate did not rewrite return: %q", result)
	}

	// Already wrapped should not be double-wrapped
	src2 := `{{ return (dict "key" "val") }}`
	result2 := preprocessTemplate(src2)
	if strings.Contains(result2, "((") {
		t.Errorf("preprocessTemplate double-wrapped: %q", result2)
	}
}

// TestIntegrationPreprocessTemplatePartials tests that template "partials/X" is rewritten.
func TestIntegrationPreprocessTemplatePartials(t *testing.T) {
	src := `{{ template "partials/pagination.html" . }}`
	result := preprocessTemplate(src)
	if !strings.Contains(result, `partial "pagination.html"`) {
		t.Errorf("preprocessTemplate did not rewrite template partial: %q", result)
	}

	src2 := `{{- template "partials/nav.html" .Site -}}`
	result2 := preprocessTemplate(src2)
	if !strings.Contains(result2, `partial "nav.html"`) {
		t.Errorf("preprocessTemplate did not rewrite template partial with context: %q", result2)
	}
}

// TestIntegrationSiteHomeStub tests Site.Home and Site.GetPage stubs.
func TestIntegrationSiteHomeStub(t *testing.T) {
	site := makeSite()
	page := &content.Page{Kind: "page"}
	ctx := &TemplateData{Page: page, Site: site}

	tmpl := `{{ .Site.Home.IsHome }}`
	got := renderPartialStr(t, tmpl, ctx)
	if got != "true" {
		t.Errorf(".Site.Home.IsHome = %q, want true", got)
	}

	tmpl2 := `{{ with .Site.Home.Resources }}has{{ else }}empty{{ end }}`
	got2 := renderPartialStr(t, tmpl2, ctx)
	if got2 != "empty" {
		t.Errorf(".Site.Home.Resources with = %q, want empty (nil)", got2)
	}
}

// TestIntegrationHugoObject tests hugo.IsServer and hugo.Generator.
func TestIntegrationHugoObject(t *testing.T) {
	site := makeSite()
	ctx := &TemplateData{Page: &content.Page{Kind: "page"}, Site: site}

	tmpl := `{{ hugo.IsServer }}`
	got := renderPartialStr(t, tmpl, ctx)
	// IsServer is false in test (not serving)
	if got != "false" && got != "true" {
		t.Errorf("hugo.IsServer = %q, want bool", got)
	}

	tmpl2 := `{{ hugo.Generator }}`
	got2 := renderPartialStr(t, tmpl2, ctx)
	if !strings.Contains(got2, "generator") && !strings.Contains(got2, "tago") && !strings.Contains(got2, "Hugo") {
		t.Errorf("hugo.Generator = %q, should contain generator tag", got2)
	}
}

// TestIntegrationPathFuncs tests path namespace functions.
func TestIntegrationPathFuncs(t *testing.T) {
	site := makeSite()
	ctx := &TemplateData{Page: &content.Page{Kind: "page"}, Site: site}

	tests := []struct {
		tmpl string
		want string
	}{
		{`{{ path.Join "images" "cover.jpg" }}`, "images/cover.jpg"},
		{`{{ path.Base "images/cover.jpg" }}`, "cover.jpg"},
		{`{{ path.Ext "cover.jpg" }}`, ".jpg"},
		{`{{ path.Dir "images/cover.jpg" }}`, "images"},
	}

	for _, tt := range tests {
		t.Run(tt.tmpl, func(t *testing.T) {
			got := renderPartialStr(t, tt.tmpl, ctx)
			if got != tt.want {
				t.Errorf("template %q = %q, want %q", tt.tmpl, got, tt.want)
			}
		})
	}
}

// TestIntegrationNowFunction tests the now function.
func TestIntegrationNowFunction(t *testing.T) {
	site := makeSite()
	ctx := &TemplateData{Page: &content.Page{Kind: "page"}, Site: site}

	tmpl := `{{ now.Format "2006" }}`
	got := renderPartialStr(t, tmpl, ctx)
	// Should be a valid 4-digit year
	if len(got) != 4 {
		t.Errorf("now.Format 2006 = %q, want 4-digit year", got)
	}
}

// TestIntegrationFindREFunction tests findRE for regex matching.
func TestIntegrationFindREFunction(t *testing.T) {
	site := makeSite()
	ctx := &TemplateData{Page: &content.Page{Kind: "page"}, Site: site}

	tmpl := `{{ $m := findRE "[0-9]+" "abc123def456" }}{{ len $m }}`
	got := renderPartialStr(t, tmpl, ctx)
	if got != "2" {
		t.Errorf("findRE count = %q, want 2", got)
	}
}

// TestIntegrationSeqFunction tests the seq function.
func TestIntegrationSeqFunction(t *testing.T) {
	site := makeSite()
	ctx := &TemplateData{Page: &content.Page{Kind: "page"}, Site: site}

	tmpl := `{{ range seq 1 3 }}{{ . }}{{ end }}`
	got := renderPartialStr(t, tmpl, ctx)
	if got != "123" {
		t.Errorf("seq 1 3 = %q, want 123", got)
	}
}

// TestIntegrationURLsParse tests urls.Parse returning parsedURL.
func TestIntegrationURLsParse(t *testing.T) {
	site := makeSite()
	ctx := &TemplateData{Page: &content.Page{Kind: "page"}, Site: site}

	tmpl := `{{ $u := urls.Parse "https://example.com/path?q=1" }}{{ $u.Host }}/{{ $u.Path }}`
	got := renderPartialStr(t, tmpl, ctx)
	if got != "example.com//path" {
		t.Errorf("urls.Parse = %q, want example.com//path", got)
	}
}

// TestIntegrationReflectIsSlice tests reflect.IsSlice in templates.
func TestIntegrationReflectIsSlice(t *testing.T) {
	site := makeSite()
	ctx := &TemplateData{Page: &content.Page{Kind: "page"}, Site: site}

	tmpl := `{{ reflect.IsSlice (slice 1 2 3) }}`
	got := renderPartialStr(t, tmpl, ctx)
	if got != "true" {
		t.Errorf("reflect.IsSlice(slice) = %q, want true", got)
	}

	tmpl2 := `{{ reflect.IsSlice "notaslice" }}`
	got2 := renderPartialStr(t, tmpl2, ctx)
	if got2 != "false" {
		t.Errorf("reflect.IsSlice(string) = %q, want false", got2)
	}
}

// TestIntegrationPaginatorStub tests the paginator stub.
func TestIntegrationPaginatorStub(t *testing.T) {
	site := makeSite()
	page := &content.Page{Kind: "home"}
	ctx := &TemplateData{Page: page, Site: site}

	tmpl := `{{ $p := .Paginator }}{{ $p.TotalPages }}`
	got := renderPartialStr(t, tmpl, ctx)
	// TotalPages is 0 or 1 in stub
	if got != "0" && got != "1" {
		t.Errorf("Paginator.TotalPages = %q, want 0 or 1", got)
	}
}

// TestIntegrationSiteGlobal tests the global {{ site }} function.
func TestIntegrationSiteGlobal(t *testing.T) {
	site := makeSite()
	ctx := &TemplateData{
		Page: &content.Page{Kind: "page"},
		Site: site,
	}

	tmpl := `{{ site.Title }}`
	got := renderPartialStr(t, tmpl, ctx)
	if got != "Test Site" {
		t.Errorf("site.Title = %q, want Test Site", got)
	}
}

// TestIntegrationIndexFunction tests the index function for map/slice access.
func TestIntegrationIndexFunction(t *testing.T) {
	site := makeSite()
	ctx := &TemplateData{Page: &content.Page{Kind: "page"}, Site: site}

	tmpl := `{{ $m := dict "a" "alpha" "b" "beta" }}{{ index $m "a" }}`
	got := renderPartialStr(t, tmpl, ctx)
	if got != "alpha" {
		t.Errorf("index dict = %q, want alpha", got)
	}

	tmpl2 := `{{ $s := slice "x" "y" "z" }}{{ index $s 1 }}`
	got2 := renderPartialStr(t, tmpl2, ctx)
	if got2 != "y" {
		t.Errorf("index slice = %q, want y", got2)
	}
}

// TestHugoPageListByDateAscending pins the Hugo contract: .ByDate sorts oldest
// first, so .ByDate.Reverse is newest first. tago previously sorted .ByDate
// descending, which made .Reverse produce oldest-first and broke dated archives.
func TestHugoPageListByDateAscending(t *testing.T) {
	site := makeSite()
	mk := func(title string, d time.Time) *HugoPage {
		return &HugoPage{Page: &content.Page{Kind: "page", Title: title, Date: d}, Site: site}
	}
	old := time.Date(2026, 7, 12, 23, 49, 0, 0, time.UTC)
	mid := time.Date(2026, 7, 13, 8, 4, 0, 0, time.UTC)
	newest := time.Date(2026, 7, 13, 14, 55, 0, 0, time.UTC)

	// Feed in a deliberately unsorted list.
	list := HugoPageList{mk("mid", mid), mk("newest", newest), mk("old", old)}

	asc := list.ByDate()
	if asc[0].Page.Title != "old" || asc[2].Page.Title != "newest" {
		t.Fatalf("ByDate should be oldest first, got %s..%s", asc[0].Page.Title, asc[2].Page.Title)
	}

	desc := list.ByDate().Reverse()
	if desc[0].Page.Title != "newest" || desc[2].Page.Title != "old" {
		t.Fatalf("ByDate.Reverse should be newest first, got %s..%s", desc[0].Page.Title, desc[2].Page.Title)
	}

	// GroupByDate keeps the input order, so newest-first in yields newest-first groups.
	groups := desc.GroupByDate("2006-01-02")
	if len(groups) != 2 || groups[0].Key != "2026-07-13" || groups[1].Key != "2026-07-12" {
		t.Fatalf("GroupByDate should keep input order (newest day first), got %#v", groups)
	}
}

// TestHugoPageRegularPagesRecursive checks that a section reaches leaf pages at
// any depth, which is what lets a nested date archive list every report from the
// year or month landing page with no hand-maintained index.
func TestHugoPageRegularPagesRecursive(t *testing.T) {
	site := makeSite()
	leaf1 := &content.Page{Kind: "page", Title: "leaf1"}
	leaf2 := &content.Page{Kind: "page", Title: "leaf2"}
	day := &content.Page{Kind: "section", Title: "day", Children: []*content.Page{leaf1, leaf2}}
	month := &content.Page{Kind: "section", Title: "month", Children: []*content.Page{day}}

	hp := &HugoPage{Page: month, Site: site}
	if got := len(hp.RegularPagesRecursive()); got != 2 {
		t.Fatalf("RegularPagesRecursive from month = %d leaves, want 2", got)
	}
	if got := len(hp.Sections()); got != 1 {
		t.Fatalf("Sections from month = %d, want 1", got)
	}
	if got := len(hp.RegularPages()); got != 0 {
		t.Fatalf("RegularPages from month = %d (month has no direct leaf pages), want 0", got)
	}
}
