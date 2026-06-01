package content

import (
	"os"
	"path/filepath"
	"testing"
)

func setupContentDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	contentDir := filepath.Join(dir, "content")
	if err := os.MkdirAll(contentDir, 0755); err != nil {
		t.Fatal(err)
	}
	return contentDir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildTreeParentChild(t *testing.T) {
	contentDir := setupContentDir(t)
	writeFile(t, filepath.Join(contentDir, "_index.md"), "---\ntitle: Home\n---\n")
	writeFile(t, filepath.Join(contentDir, "posts", "_index.md"), "---\ntitle: Posts\n---\n")
	writeFile(t, filepath.Join(contentDir, "posts", "hello.md"), "---\ntitle: Hello\ndate: 2024-01-01\n---\nContent.\n")

	opts := LoadOptions{ContentDir: contentDir, DefaultLang: "en", OutputDir: t.TempDir()}
	pages, err := LoadAll(opts)
	if err != nil {
		t.Fatal(err)
	}

	var home, postsSection, hello *Page
	for _, p := range pages {
		switch p.Kind {
		case "home":
			home = p
		case "section":
			postsSection = p
		case "page":
			hello = p
		}
	}

	if home == nil {
		t.Fatal("home page not found")
	}
	if postsSection == nil {
		t.Fatal("posts section not found")
	}
	if hello == nil {
		t.Fatal("hello page not found")
	}

	// Home should have posts section as child
	found := false
	for _, c := range home.Children {
		if c == postsSection {
			found = true
		}
	}
	if !found {
		t.Error("home.Children should contain postsSection")
	}

	// Hello's parent should be postsSection
	if hello.Parent != postsSection {
		t.Error("hello.Parent should be postsSection")
	}
}

func TestBuildTreeAncestors(t *testing.T) {
	contentDir := setupContentDir(t)
	writeFile(t, filepath.Join(contentDir, "_index.md"), "---\ntitle: Home\n---\n")
	writeFile(t, filepath.Join(contentDir, "docs", "_index.md"), "---\ntitle: Docs\n---\n")
	writeFile(t, filepath.Join(contentDir, "docs", "api.md"), "---\ntitle: API\n---\n")

	opts := LoadOptions{ContentDir: contentDir, DefaultLang: "en", OutputDir: t.TempDir()}
	pages, err := LoadAll(opts)
	if err != nil {
		t.Fatal(err)
	}

	var api *Page
	for _, p := range pages {
		if p.Title == "API" {
			api = p
		}
	}
	if api == nil {
		t.Fatal("API page not found")
	}
	// Should have 2 ancestors: home + docs section
	if len(api.Ancestors) != 2 {
		t.Errorf("api.Ancestors = %d, want 2", len(api.Ancestors))
	}
}

func TestCascadeDraftPropagation(t *testing.T) {
	contentDir := setupContentDir(t)
	// Section with cascade: draft=true
	writeFile(t, filepath.Join(contentDir, "drafts", "_index.md"),
		"---\ntitle: Drafts\ncascade:\n  draft: true\n---\n")
	// Page 1 has no explicit draft field — cascade should apply draft=true
	writeFile(t, filepath.Join(contentDir, "drafts", "page1.md"),
		"---\ntitle: Page 1\n---\nContent.\n")

	opts := LoadOptions{ContentDir: contentDir, DefaultLang: "en", OutputDir: t.TempDir()}
	pages, err := LoadAll(opts)
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range pages {
		if p.Title == "Page 1" {
			if !p.Draft {
				t.Error("Page 1 should have draft=true from cascade")
			}
		}
	}
}

func TestCascadeTypePropagation(t *testing.T) {
	contentDir := setupContentDir(t)
	writeFile(t, filepath.Join(contentDir, "docs", "_index.md"),
		"---\ntitle: Docs\ncascade:\n  type: documentation\n---\n")
	writeFile(t, filepath.Join(contentDir, "docs", "guide.md"),
		"---\ntitle: Guide\n---\nContent.\n")

	opts := LoadOptions{ContentDir: contentDir, DefaultLang: "en", OutputDir: t.TempDir()}
	pages, err := LoadAll(opts)
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range pages {
		if p.Title == "Guide" {
			if p.Type != "documentation" {
				t.Errorf("Guide.Type = %q, want documentation (from cascade)", p.Type)
			}
		}
	}
}

func TestCascadeParamsInheritance(t *testing.T) {
	contentDir := setupContentDir(t)
	writeFile(t, filepath.Join(contentDir, "special", "_index.md"),
		"---\ntitle: Special\ncascade:\n  showToc: true\n---\n")
	writeFile(t, filepath.Join(contentDir, "special", "article.md"),
		"---\ntitle: Article\n---\nContent.\n")

	opts := LoadOptions{ContentDir: contentDir, DefaultLang: "en", OutputDir: t.TempDir()}
	pages, err := LoadAll(opts)
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range pages {
		if p.Title == "Article" {
			if p.Params == nil {
				t.Fatal("Article.Params is nil")
			}
			if _, ok := p.Params["showToc"]; !ok {
				t.Error("Article should inherit showToc from cascade")
			}
		}
	}
}

func TestChildrenSortedByWeightThenDate(t *testing.T) {
	contentDir := setupContentDir(t)
	writeFile(t, filepath.Join(contentDir, "_index.md"), "---\ntitle: Home\n---\n")
	writeFile(t, filepath.Join(contentDir, "c.md"), "---\ntitle: C\nweight: 1\ndate: 2024-01-01\n---\n")
	writeFile(t, filepath.Join(contentDir, "a.md"), "---\ntitle: A\nweight: 2\ndate: 2024-01-03\n---\n")
	writeFile(t, filepath.Join(contentDir, "b.md"), "---\ntitle: B\nweight: 2\ndate: 2024-01-02\n---\n")

	opts := LoadOptions{ContentDir: contentDir, DefaultLang: "en", OutputDir: t.TempDir()}
	pages, err := LoadAll(opts)
	if err != nil {
		t.Fatal(err)
	}

	var home *Page
	for _, p := range pages {
		if p.Kind == "home" {
			home = p
		}
	}
	if home == nil {
		t.Fatal("home not found")
	}
	if len(home.Children) < 3 {
		t.Fatalf("home.Children = %d, want at least 3", len(home.Children))
	}
	// C (weight=1) should come first
	if home.Children[0].Title != "C" {
		t.Errorf("first child = %q, want C (weight 1)", home.Children[0].Title)
	}
	// A and B both weight=2; A has later date so should come first
	if home.Children[1].Title != "A" {
		t.Errorf("second child = %q, want A (date 2024-01-03)", home.Children[1].Title)
	}
}

func TestTagCounts(t *testing.T) {
	pages := []*Page{
		{Tags: []string{"go", "web"}},
		{Tags: []string{"go", "rust"}},
		{Tags: []string{"web"}},
	}
	counts := TagCounts(pages)
	if counts["go"] != 2 {
		t.Errorf("go count = %d, want 2", counts["go"])
	}
	if counts["web"] != 2 {
		t.Errorf("web count = %d, want 2", counts["web"])
	}
	if counts["rust"] != 1 {
		t.Errorf("rust count = %d, want 1", counts["rust"])
	}
}

func TestFilterByTag(t *testing.T) {
	pages := []*Page{
		{Title: "A", Tags: []string{"go"}},
		{Title: "B", Tags: []string{"rust"}},
		{Title: "C", Tags: []string{"go", "web"}},
	}
	result := FilterByTag(pages, "go")
	if len(result) != 2 {
		t.Errorf("FilterByTag(go) = %d, want 2", len(result))
	}
}

func TestLeafBundleInTree(t *testing.T) {
	contentDir := setupContentDir(t)
	// Leaf bundle: posts/my-post/index.md (no underscore)
	writeFile(t, filepath.Join(contentDir, "_index.md"), "---\ntitle: Home\n---\n")
	writeFile(t, filepath.Join(contentDir, "posts", "_index.md"), "---\ntitle: Posts\n---\n")
	writeFile(t, filepath.Join(contentDir, "posts", "my-post", "index.md"),
		"---\ntitle: My Post\n---\nContent.\n")

	opts := LoadOptions{ContentDir: contentDir, DefaultLang: "en", OutputDir: t.TempDir()}
	pages, err := LoadAll(opts)
	if err != nil {
		t.Fatal(err)
	}

	var myPost *Page
	for _, p := range pages {
		if p.Title == "My Post" {
			myPost = p
		}
	}
	if myPost == nil {
		t.Fatal("My Post not found")
	}
	if myPost.Kind != "page" {
		t.Errorf("leaf bundle kind = %q, want page", myPost.Kind)
	}
	if myPost.RelPermalink != "/posts/my-post/" {
		t.Errorf("leaf bundle permalink = %q, want /posts/my-post/", myPost.RelPermalink)
	}
}
