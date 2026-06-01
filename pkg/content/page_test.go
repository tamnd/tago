package content

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeTemp creates a temp .md file with the given content and returns its path.
func writeTemp(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseSlug(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "post.md", `---
title: My Post
slug: custom-slug
---
Content here.
`)
	p, err := ParseAndRender(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.Slug != "custom-slug" {
		t.Errorf("Slug = %q, want %q", p.Slug, "custom-slug")
	}
	if p.Title != "My Post" {
		t.Errorf("Title = %q, want %q", p.Title, "My Post")
	}
}

func TestParseURL(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "post.md", `---
title: Relocated
url: /new/location/
---
`)
	p, err := ParseAndRender(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.URL != "/new/location/" {
		t.Errorf("URL = %q, want /new/location/", p.URL)
	}
}

func TestParseAliases(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "post.md", `---
title: Has Aliases
aliases:
  - /old/path/
  - /another/old/
---
`)
	p, err := ParseAndRender(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Aliases) != 2 {
		t.Fatalf("len(Aliases) = %d, want 2", len(p.Aliases))
	}
	if p.Aliases[0] != "/old/path/" {
		t.Errorf("Aliases[0] = %q, want /old/path/", p.Aliases[0])
	}
}

func TestParsePublishDate(t *testing.T) {
	dir := t.TempDir()
	// Test all Hugo aliases
	cases := []struct {
		key  string
		body string
	}{
		{"publishDate", "publishDate: 2025-03-15"},
		{"pubdate", "pubdate: 2025-03-15"},
		{"published", "published: 2025-03-15"},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			path := writeTemp(t, dir, tc.key+".md", "---\ntitle: Test\n"+tc.body+"\n---\n")
			p, err := ParseAndRender(path)
			if err != nil {
				t.Fatal(err)
			}
			if p.PublishDate.Year() != 2025 || p.PublishDate.Month() != 3 || p.PublishDate.Day() != 15 {
				t.Errorf("PublishDate = %v, want 2025-03-15", p.PublishDate)
			}
		})
	}
}

func TestParseExpiryDate(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		key string
	}{
		{"expiryDate"},
		{"unpublishdate"},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			path := writeTemp(t, dir, tc.key+".md", "---\ntitle: Test\n"+tc.key+": 2020-01-01\n---\n")
			p, err := ParseAndRender(path)
			if err != nil {
				t.Fatal(err)
			}
			if p.ExpiryDate.Year() != 2020 {
				t.Errorf("ExpiryDate = %v, want year 2020", p.ExpiryDate)
			}
		})
	}
}

func TestParseLastmod(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "post.md", `---
title: Test
date: 2024-01-01
lastmod: 2025-06-01
---
`)
	p, err := ParseAndRender(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.Lastmod.Year() != 2025 {
		t.Errorf("Lastmod = %v, want year 2025", p.Lastmod)
	}
}

func TestParseLastmodAliasModified(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "post.md", `---
title: Test
modified: 2025-05-01
---
`)
	p, err := ParseAndRender(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.Lastmod.Year() != 2025 || p.Lastmod.Month() != 5 {
		t.Errorf("Lastmod (via modified) = %v, want 2025-05", p.Lastmod)
	}
}

func TestParseLayout(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "post.md", `---
title: Test
layout: special
---
`)
	p, err := ParseAndRender(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.Layout != "special" {
		t.Errorf("Layout = %q, want special", p.Layout)
	}
}

func TestParseCategories(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "post.md", `---
title: Test
categories:
  - Go
  - Web
---
`)
	p, err := ParseAndRender(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Categories) != 2 {
		t.Fatalf("len(Categories) = %d, want 2", len(p.Categories))
	}
}

func TestParseKeywords(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "post.md", `---
title: Test
keywords: [golang, ssg, tago]
---
`)
	p, err := ParseAndRender(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Keywords) != 3 {
		t.Fatalf("len(Keywords) = %d, want 3", len(p.Keywords))
	}
}

func TestParseCascadeMap(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "_index.md", `---
title: Section
cascade:
  draft: true
  type: docs
---
`)
	p, err := ParseAndRender(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.Cascade == nil {
		t.Fatal("Cascade is nil")
	}
	if !toBool(p.Cascade["draft"]) {
		t.Error("Cascade draft should be true")
	}
	if toString(p.Cascade["type"]) != "docs" {
		t.Errorf("Cascade type = %q, want docs", p.Cascade["type"])
	}
}

func TestParseExtraParams(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "post.md", `---
title: Test
myCustomField: hello
anotherField: 42
---
`)
	p, err := ParseAndRender(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.Params["myCustomField"] != "hello" {
		t.Errorf("Params[myCustomField] = %v, want hello", p.Params["myCustomField"])
	}
}

func TestApplySlugURL(t *testing.T) {
	cases := []struct {
		name     string
		base     string
		slug     string
		urlField string
		kind     string
		want     string
	}{
		{"no override", "/posts/my-post/", "", "", "page", "/posts/my-post/"},
		{"slug override", "/posts/my-post/", "custom", "", "page", "/posts/custom/"},
		{"url override", "/posts/my-post/", "", "/new/path/", "page", "/new/path/"},
		{"url wins over slug", "/posts/my-post/", "slug", "/url/path/", "page", "/url/path/"},
		{"slug ignored for section", "/posts/", "custom", "", "section", "/posts/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &Page{Slug: tc.slug, URL: tc.urlField, Kind: tc.kind}
			got := ApplySlugURL(tc.base, p)
			if got != tc.want {
				t.Errorf("ApplySlugURL(%q) = %q, want %q", tc.base, got, tc.want)
			}
		})
	}
}

func TestApplySlugURLRootIndex(t *testing.T) {
	// slug on root index should not change /
	p := &Page{Slug: "home"}
	got := ApplySlugURL("/", p)
	if got != "/" {
		t.Errorf("ApplySlugURL on root = %q, want /", got)
	}
}

func TestPermalinkFromPathLeafBundle(t *testing.T) {
	dir := t.TempDir()
	contentDir := filepath.Join(dir, "content")
	// Create leaf bundle: content/posts/my-post/index.md
	postDir := filepath.Join(contentDir, "posts", "my-post")
	if err := os.MkdirAll(postDir, 0755); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(postDir, "index.md")
	if err := os.WriteFile(indexPath, []byte("---\ntitle: Leaf\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	permalink, _ := PermalinkFromPath(contentDir, indexPath, "en")
	if permalink != "/posts/my-post/" {
		t.Errorf("leaf bundle permalink = %q, want /posts/my-post/", permalink)
	}
}

func TestKindFromPathLeafBundle(t *testing.T) {
	dir := t.TempDir()
	contentDir := filepath.Join(dir, "content")
	postDir := filepath.Join(contentDir, "posts", "my-post")
	if err := os.MkdirAll(postDir, 0755); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(postDir, "index.md")
	if err := os.WriteFile(indexPath, []byte("---\ntitle: Leaf\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	kind := KindFromPath(contentDir, indexPath)
	if kind != "page" {
		t.Errorf("leaf bundle kind = %q, want page", kind)
	}
}

func TestKindFromPathBranchBundle(t *testing.T) {
	dir := t.TempDir()
	contentDir := filepath.Join(dir, "content")
	sectionDir := filepath.Join(contentDir, "posts")
	if err := os.MkdirAll(sectionDir, 0755); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(sectionDir, "_index.md")
	if err := os.WriteFile(indexPath, []byte("---\ntitle: Posts\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	kind := KindFromPath(contentDir, indexPath)
	if kind != "section" {
		t.Errorf("branch bundle kind = %q, want section", kind)
	}
}

func TestPublishDateDefaultsToDate(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "post.md", `---
title: Test
date: 2024-06-01
---
`)
	p, err := ParseAndRender(path)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	if !p.PublishDate.Equal(want) {
		t.Errorf("PublishDate = %v, want %v (should default to date)", p.PublishDate, want)
	}
}

func TestLastmodDefaultsToDate(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "post.md", `---
title: Test
date: 2024-06-01
---
`)
	p, err := ParseAndRender(path)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	if !p.Lastmod.Equal(want) {
		t.Errorf("Lastmod = %v, want %v (should default to date)", p.Lastmod, want)
	}
}

func TestDraftDefault(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "post.md", `---
title: Test
---
`)
	p, err := ParseAndRender(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.Draft {
		t.Error("Draft should default to false")
	}
}

func TestIsHomIsPageIsSection(t *testing.T) {
	home := &Page{Kind: "home"}
	if !home.IsHome() {
		t.Error("IsHome() should be true for kind=home")
	}
	if home.IsPage() {
		t.Error("IsPage() should be false for kind=home")
	}

	page := &Page{Kind: "page"}
	if !page.IsPage() {
		t.Error("IsPage() should be true for kind=page")
	}
	if page.IsHome() {
		t.Error("IsHome() should be false for kind=page")
	}

	section := &Page{Kind: "section"}
	if !section.IsSection() {
		t.Error("IsSection() should be true for kind=section")
	}
	if !section.IsNode() {
		t.Error("IsNode() should be true for kind=section")
	}
}

func TestFuzzyWordCount(t *testing.T) {
	p := &Page{WordCount: 42}
	if p.FuzzyWordCount() != 42 {
		t.Errorf("FuzzyWordCount(42) = %d, want 42", p.FuzzyWordCount())
	}
	p2 := &Page{WordCount: 1234}
	if p2.FuzzyWordCount() != 1200 {
		t.Errorf("FuzzyWordCount(1234) = %d, want 1200", p2.FuzzyWordCount())
	}
}

func TestRegularPagesMethod(t *testing.T) {
	section := &Page{Kind: "section"}
	child1 := &Page{Kind: "page", Title: "A"}
	child2 := &Page{Kind: "section", Title: "Sub"}
	child3 := &Page{Kind: "page", Title: "B"}
	section.Children = []*Page{child1, child2, child3}

	rp := section.RegularPages()
	if len(rp) != 2 {
		t.Errorf("RegularPages() = %d pages, want 2", len(rp))
	}
}

func TestSectionsMethod(t *testing.T) {
	home := &Page{Kind: "home"}
	s1 := &Page{Kind: "section", Title: "Blog"}
	s2 := &Page{Kind: "section", Title: "Docs"}
	p1 := &Page{Kind: "page", Title: "Post"}
	home.Children = []*Page{s1, s2, p1}

	secs := home.Sections()
	if len(secs) != 2 {
		t.Errorf("Sections() = %d, want 2", len(secs))
	}
}
