package build

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setup(t *testing.T) (contentDir, outputDir string) {
	t.Helper()
	dir := t.TempDir()
	contentDir = filepath.Join(dir, "content")
	outputDir = filepath.Join(dir, "public")
	if err := os.MkdirAll(contentDir, 0755); err != nil {
		t.Fatal(err)
	}
	return
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func defaultCfg(contentDir, outputDir string) *Config {
	return &Config{
		ContentDir:  contentDir,
		OutputDir:   outputDir,
		StaticDir:   filepath.Join(contentDir, "../static"),
		LayoutsDir:  "",
		BaseURL:     "http://localhost:1313/",
		DefaultLang: "en",
		SiteTitle:   "Test Site",
	}
}

func TestBuildBasic(t *testing.T) {
	contentDir, outputDir := setup(t)
	write(t, filepath.Join(contentDir, "_index.md"), "---\ntitle: Home\n---\nWelcome.\n")
	write(t, filepath.Join(contentDir, "about.md"), "---\ntitle: About\ndate: 2024-01-01\n---\nAbout page.\n")

	cfg := defaultCfg(contentDir, outputDir)
	stats, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if stats.PagesTotal < 2 {
		t.Errorf("PagesTotal = %d, want >= 2", stats.PagesTotal)
	}
	// Check output files exist
	if _, err := os.Stat(filepath.Join(outputDir, "index.html")); err != nil {
		t.Error("index.html not generated")
	}
	if _, err := os.Stat(filepath.Join(outputDir, "about", "index.html")); err != nil {
		t.Error("about/index.html not generated")
	}
}

func TestBuildExcludesDrafts(t *testing.T) {
	contentDir, outputDir := setup(t)
	write(t, filepath.Join(contentDir, "_index.md"), "---\ntitle: Home\n---\n")
	write(t, filepath.Join(contentDir, "draft-post.md"), "---\ntitle: Draft\ndraft: true\n---\nDraft content.\n")
	write(t, filepath.Join(contentDir, "published.md"), "---\ntitle: Published\n---\nPublished content.\n")

	cfg := defaultCfg(contentDir, outputDir)
	cfg.BuildDrafts = false
	stats, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	// Draft should not be in PagesTotal
	if stats.PagesTotal > 2 { // home + published
		t.Errorf("PagesTotal = %d with drafts excluded, want <= 2", stats.PagesTotal)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "draft-post", "index.html")); err == nil {
		t.Error("draft-post/index.html should not be generated without --buildDrafts")
	}
}

func TestBuildIncludesDraftsWithFlag(t *testing.T) {
	contentDir, outputDir := setup(t)
	write(t, filepath.Join(contentDir, "_index.md"), "---\ntitle: Home\n---\n")
	write(t, filepath.Join(contentDir, "draft-post.md"), "---\ntitle: Draft\ndraft: true\n---\nDraft content.\n")

	cfg := defaultCfg(contentDir, outputDir)
	cfg.BuildDrafts = true
	_, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "draft-post", "index.html")); err != nil {
		t.Error("draft-post/index.html should be generated with BuildDrafts=true")
	}
}

func TestBuildExcludesFuturePages(t *testing.T) {
	contentDir, outputDir := setup(t)
	write(t, filepath.Join(contentDir, "_index.md"), "---\ntitle: Home\n---\n")
	futureDate := time.Now().Add(72 * time.Hour).Format("2006-01-02")
	write(t, filepath.Join(contentDir, "future.md"),
		"---\ntitle: Future\npublishDate: "+futureDate+"\n---\nFuture content.\n")

	cfg := defaultCfg(contentDir, outputDir)
	cfg.BuildFuture = false
	_, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "future", "index.html")); err == nil {
		t.Error("future/index.html should not be generated without --buildFuture")
	}
}

func TestBuildIncludesFutureWithFlag(t *testing.T) {
	contentDir, outputDir := setup(t)
	write(t, filepath.Join(contentDir, "_index.md"), "---\ntitle: Home\n---\n")
	futureDate := time.Now().Add(72 * time.Hour).Format("2006-01-02")
	write(t, filepath.Join(contentDir, "future.md"),
		"---\ntitle: Future\npublishDate: "+futureDate+"\n---\nFuture content.\n")

	cfg := defaultCfg(contentDir, outputDir)
	cfg.BuildFuture = true
	_, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "future", "index.html")); err != nil {
		t.Error("future/index.html should be generated with BuildFuture=true")
	}
}

func TestBuildExcludesExpiredPages(t *testing.T) {
	contentDir, outputDir := setup(t)
	write(t, filepath.Join(contentDir, "_index.md"), "---\ntitle: Home\n---\n")
	pastDate := time.Now().Add(-72 * time.Hour).Format("2006-01-02")
	write(t, filepath.Join(contentDir, "expired.md"),
		"---\ntitle: Expired\nexpiryDate: "+pastDate+"\n---\nExpired content.\n")

	cfg := defaultCfg(contentDir, outputDir)
	cfg.BuildExpired = false
	_, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "expired", "index.html")); err == nil {
		t.Error("expired/index.html should not be generated without --buildExpired")
	}
}

func TestBuildIncludesExpiredWithFlag(t *testing.T) {
	contentDir, outputDir := setup(t)
	write(t, filepath.Join(contentDir, "_index.md"), "---\ntitle: Home\n---\n")
	pastDate := time.Now().Add(-72 * time.Hour).Format("2006-01-02")
	write(t, filepath.Join(contentDir, "expired.md"),
		"---\ntitle: Expired\nexpiryDate: "+pastDate+"\n---\nExpired content.\n")

	cfg := defaultCfg(contentDir, outputDir)
	cfg.BuildExpired = true
	_, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "expired", "index.html")); err != nil {
		t.Error("expired/index.html should be generated with BuildExpired=true")
	}
}

func TestBuildSlugOverride(t *testing.T) {
	contentDir, outputDir := setup(t)
	write(t, filepath.Join(contentDir, "_index.md"), "---\ntitle: Home\n---\n")
	write(t, filepath.Join(contentDir, "my-long-filename.md"),
		"---\ntitle: Short\nslug: short\n---\nContent.\n")

	cfg := defaultCfg(contentDir, outputDir)
	_, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	// Page should be at /short/ not /my-long-filename/
	if _, err := os.Stat(filepath.Join(outputDir, "short", "index.html")); err != nil {
		t.Error("short/index.html should exist with slug override")
	}
}

func TestBuildURLOverride(t *testing.T) {
	contentDir, outputDir := setup(t)
	write(t, filepath.Join(contentDir, "_index.md"), "---\ntitle: Home\n---\n")
	write(t, filepath.Join(contentDir, "post.md"),
		"---\ntitle: Post\nurl: /custom/path/\n---\nContent.\n")

	cfg := defaultCfg(contentDir, outputDir)
	_, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "custom", "path", "index.html")); err != nil {
		t.Error("custom/path/index.html should exist with url override")
	}
}

func TestBuildAliasPages(t *testing.T) {
	contentDir, outputDir := setup(t)
	write(t, filepath.Join(contentDir, "_index.md"), "---\ntitle: Home\n---\n")
	write(t, filepath.Join(contentDir, "new-post.md"),
		"---\ntitle: New Post\naliases:\n  - /old-post/\n  - /archive/post/\n---\nContent.\n")

	cfg := defaultCfg(contentDir, outputDir)
	_, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	// Alias pages should be generated
	if _, err := os.Stat(filepath.Join(outputDir, "old-post", "index.html")); err != nil {
		t.Error("old-post/index.html alias redirect should exist")
	}
	if _, err := os.Stat(filepath.Join(outputDir, "archive", "post", "index.html")); err != nil {
		t.Error("archive/post/index.html alias redirect should exist")
	}
}

func TestBuildSiteParams(t *testing.T) {
	contentDir, outputDir := setup(t)
	write(t, filepath.Join(contentDir, "_index.md"), "---\ntitle: Home\n---\n")

	cfg := defaultCfg(contentDir, outputDir)
	cfg.Params = map[string]any{
		"author":    "Test Author",
		"analytics": "UA-12345",
	}
	_, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	// Just verify build succeeds with params set
}

func TestBuildLeafBundle(t *testing.T) {
	contentDir, outputDir := setup(t)
	write(t, filepath.Join(contentDir, "_index.md"), "---\ntitle: Home\n---\n")
	write(t, filepath.Join(contentDir, "posts", "_index.md"), "---\ntitle: Posts\n---\n")
	write(t, filepath.Join(contentDir, "posts", "my-article", "index.md"),
		"---\ntitle: My Article\n---\nLeaf bundle content.\n")

	cfg := defaultCfg(contentDir, outputDir)
	_, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	// Leaf bundle should output at /posts/my-article/ not /posts/my-article/index/
	if _, err := os.Stat(filepath.Join(outputDir, "posts", "my-article", "index.html")); err != nil {
		t.Error("posts/my-article/index.html should exist for leaf bundle")
	}
}

func TestBuildIncrementalCache(t *testing.T) {
	contentDir, outputDir := setup(t)
	write(t, filepath.Join(contentDir, "_index.md"), "---\ntitle: Home\n---\n")
	write(t, filepath.Join(contentDir, "page1.md"), "---\ntitle: Page 1\n---\nContent.\n")

	cfg := defaultCfg(contentDir, outputDir)
	stats1, err := Build(cfg)
	if err != nil {
		t.Fatalf("First build error: %v", err)
	}
	// Second build with no changes should rebuild 0 pages
	stats2, err := Build(cfg)
	if err != nil {
		t.Fatalf("Second build error: %v", err)
	}
	if stats2.PagesRebuilt > 0 {
		t.Errorf("Second build rebuilt %d pages, want 0 (no changes)", stats2.PagesRebuilt)
	}
	if stats2.PagesTotal != stats1.PagesTotal {
		t.Errorf("PagesTotal changed: %d -> %d", stats1.PagesTotal, stats2.PagesTotal)
	}
}

func TestBuildTagPages(t *testing.T) {
	contentDir, outputDir := setup(t)
	write(t, filepath.Join(contentDir, "_index.md"), "---\ntitle: Home\n---\n")
	write(t, filepath.Join(contentDir, "post1.md"), "---\ntitle: Post 1\ntags: [go, web]\n---\nContent.\n")
	write(t, filepath.Join(contentDir, "post2.md"), "---\ntitle: Post 2\ntags: [go]\n---\nContent.\n")

	cfg := defaultCfg(contentDir, outputDir)
	_, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	// Tag pages should be generated
	if _, err := os.Stat(filepath.Join(outputDir, "tags", "go", "index.html")); err != nil {
		t.Error("tags/go/index.html should exist")
	}
	if _, err := os.Stat(filepath.Join(outputDir, "tags", "web", "index.html")); err != nil {
		t.Error("tags/web/index.html should exist")
	}
	if _, err := os.Stat(filepath.Join(outputDir, "tags", "index.html")); err != nil {
		t.Error("tags/index.html taxonomy page should exist")
	}
}

func TestGenerateAliasPages(t *testing.T) {
	outputDir := t.TempDir()
	pages := []*struct {
		title       string
		permalink   string
		aliases     []string
	}{
		{"My Page", "/posts/new/", []string{"/posts/old/", "/archive/post/"}},
		{"No Aliases", "/about/", nil},
	}
	// Convert to content.Page-like — use build's internal function directly via build_test
	// Instead test via Build() which calls generateAliasPages
	_ = outputDir
	_ = pages
	// This is tested via TestBuildAliasPages above
}
