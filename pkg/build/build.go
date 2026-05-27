package build

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/tago/pkg/asset"
	"github.com/tamnd/tago/pkg/cache"
	"github.com/tamnd/tago/pkg/content"
	"github.com/tamnd/tago/pkg/index"
	"github.com/tamnd/tago/pkg/render"
)

// Config holds the build configuration.
type Config struct {
	ContentDir  string
	OutputDir   string
	StaticDir   string
	LayoutsDir  string
	BaseURL     string
	DefaultLang string
	SiteTitle   string
	SiteDesc    string
	EditURLBase string
	Clean       bool
	LiveReload  bool
}

// Stats holds build statistics.
type Stats struct {
	PagesRebuilt int
	PagesTotal   int
	Duration     time.Duration
}

// Build runs the full incremental build.
func Build(cfg *Config) (*Stats, error) {
	start := time.Now()

	if cfg.DefaultLang == "" {
		cfg.DefaultLang = "en"
	}

	// Clean build: delete output and cache
	if cfg.Clean {
		if err := os.RemoveAll(cfg.OutputDir); err != nil {
			return nil, fmt.Errorf("clean output dir: %w", err)
		}
		log.Println("tago: cleaned output directory")
	}

	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir output: %w", err)
	}

	// Open cache
	cacheDB, err := cache.Open(filepath.Join(cfg.OutputDir, ".tago-cache.db"))
	if err != nil {
		return nil, fmt.Errorf("open cache: %w", err)
	}
	defer cacheDB.Close()

	// Step 1: Scan all .md files → compute SHA-256 hashes
	diskHashes, err := scanMarkdownFiles(cfg.ContentDir)
	if err != nil {
		return nil, fmt.Errorf("scan content: %w", err)
	}

	// Step 2: Load cache hashes
	cachedHashes, err := cacheDB.LoadHashes()
	if err != nil {
		return nil, fmt.Errorf("load cache hashes: %w", err)
	}

	// Step 3: Diff
	changedFiles, deletedFiles := cache.Diff(cachedHashes, diskHashes)

	log.Printf("tago: %d total files, %d changed, %d deleted",
		len(diskHashes), len(changedFiles), len(deletedFiles))

	// Step 4: Parse and render changed pages
	changedPages := make(map[string]*content.Page)
	for _, filePath := range changedFiles {
		page, err := content.ParseAndRender(filePath)
		if err != nil {
			log.Printf("tago: error parsing %s: %v", filePath, err)
			continue
		}
		permalink, lang := content.PermalinkFromPath(cfg.ContentDir, filePath, cfg.DefaultLang)
		page.RelPermalink = permalink
		page.Lang = lang
		page.OutputPath = content.OutputPathFromPermalink(cfg.OutputDir, permalink)
		page.Kind = content.KindFromPath(cfg.ContentDir, filePath)
		page.Section = content.SectionFromPath(cfg.ContentDir, filePath)
		page.Depth = content.DepthFromPath(cfg.ContentDir, filePath)
		changedPages[filePath] = page
	}

	// Step 5: Load metadata for unchanged pages from cache
	cachedRecords, err := cacheDB.LoadAll()
	if err != nil {
		return nil, fmt.Errorf("load cache: %w", err)
	}

	// Build page set: start with cached unchanged pages
	allPages := make(map[string]*content.Page)
	for _, rec := range cachedRecords {
		// Skip deleted and changed (will be replaced)
		isChanged := false
		for _, cf := range changedFiles {
			if cf == rec.FilePath {
				isChanged = true
				break
			}
		}
		isDeleted := false
		for _, df := range deletedFiles {
			if df == rec.FilePath {
				isDeleted = true
				break
			}
		}
		if isChanged || isDeleted {
			continue
		}
		page := recordToPage(rec, cfg)
		allPages[rec.FilePath] = page
	}

	// Add changed pages
	for filePath, page := range changedPages {
		allPages[filePath] = page
	}

	// Convert to slice
	var pageSlice []*content.Page
	for _, p := range allPages {
		pageSlice = append(pageSlice, p)
	}

	// Step 6: Build full page tree
	content.BuildTree(pageSlice)

	// Compute which ancestors need re-rendering
	needsRender := make(map[string]bool)

	// All changed pages need rendering
	for _, p := range changedPages {
		needsRender[p.FilePath] = true
		// Also re-render ancestors (section indexes)
		if p.Parent != nil {
			markAncestors(p, needsRender, allPages)
		}
	}

	// Step 7 & 8: Render pages
	assetRefs, err := asset.ProcessAssets(cfg.StaticDir, cfg.OutputDir)
	if err != nil {
		log.Printf("tago: asset processing error: %v", err)
	}

	site := &render.SiteData{
		Title:       cfg.SiteTitle,
		BaseURL:     cfg.BaseURL,
		Description: cfg.SiteDesc,
		EditURLBase: cfg.EditURLBase,
	}

	rAssets := render.AssetRefs{
		CSS:        assetRefs.CSS,
		JSHead:     assetRefs.JSHead,
		JS:         assetRefs.JS,
		FlexSearch: assetRefs.FlexSearch,
		KaTeX:      assetRefs.KaTeX,
		Extra:      assetRefs.Extra,
	}

	renderer := render.New(site, rAssets, cfg.LayoutsDir, cfg.LiveReload)

	pagesRebuilt := 0

	// Render pages that need updating
	for _, page := range pageSlice {
		shouldRender := needsRender[page.FilePath] || (page.FilePath == "" /* special pages */)
		if !shouldRender {
			continue
		}
		if err := renderer.RenderPage(page, pageSlice); err != nil {
			log.Printf("tago: render error for %s: %v", page.RelPermalink, err)
			continue
		}
		pagesRebuilt++

		// Save to cache
		if page.FilePath != "" {
			rec := pageToRecord(page)
			if err := cacheDB.Save(rec); err != nil {
				log.Printf("tago: cache save error for %s: %v", page.FilePath, err)
			}
		}
	}

	// Re-render tag pages if any changed page has tags
	changedTags := collectChangedTags(changedPages)
	if len(changedTags) > 0 || len(deletedFiles) > 0 {
		if err := renderTagPages(renderer, pageSlice, cfg.OutputDir); err != nil {
			log.Printf("tago: tag page render error: %v", err)
		}
	}

	// Step 9: If any change, rebuild global indexes
	if len(changedFiles) > 0 || len(deletedFiles) > 0 {
		if err := index.BuildSearchJSON(pageSlice, cfg.OutputDir); err != nil {
			log.Printf("tago: search index error: %v", err)
		}
		if err := index.BuildGraphJSON(pageSlice, cfg.OutputDir); err != nil {
			log.Printf("tago: graph error: %v", err)
		}
		if err := index.BuildTreeJSON(pageSlice, cfg.OutputDir); err != nil {
			log.Printf("tago: tree error: %v", err)
		}
		if err := index.BuildCalendarJSON(pageSlice, cfg.OutputDir); err != nil {
			log.Printf("tago: calendar error: %v", err)
		}
		if err := index.BuildSitemap(pageSlice, cfg.BaseURL, cfg.OutputDir); err != nil {
			log.Printf("tago: sitemap error: %v", err)
		}
		// Render search page
		if err := renderer.RenderSearchPage(cfg.OutputDir); err != nil {
			log.Printf("tago: search page error: %v", err)
		}
		// Generate 404.html
		if err := renderer.Render404(cfg.OutputDir, pageSlice); err != nil {
			log.Printf("tago: 404 render error: %v", err)
		}
	}

	// Step 10: Delete outputs for deleted pages
	for _, filePath := range deletedFiles {
		permalink, _ := content.PermalinkFromPath(cfg.ContentDir, filePath, cfg.DefaultLang)
		outputPath := content.OutputPathFromPermalink(cfg.OutputDir, permalink)
		if err := os.Remove(outputPath); err != nil && !os.IsNotExist(err) {
			log.Printf("tago: delete error for %s: %v", outputPath, err)
		}
		if err := cacheDB.Delete(filePath); err != nil {
			log.Printf("tago: cache delete error for %s: %v", filePath, err)
		}
	}

	stats := &Stats{
		PagesRebuilt: pagesRebuilt,
		PagesTotal:   len(pageSlice),
		Duration:     time.Since(start),
	}

	return stats, nil
}

// scanMarkdownFiles walks contentDir and returns file_path → sha256 hash.
func scanMarkdownFiles(contentDir string) (map[string]string, error) {
	result := make(map[string]string)
	err := filepath.Walk(contentDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		hash, err := content.HashFile(path)
		if err != nil {
			return err
		}
		result[path] = hash
		return nil
	})
	return result, err
}

// markAncestors marks all ancestor pages (section indexes) as needing re-render.
func markAncestors(page *content.Page, needsRender map[string]bool, allPages map[string]*content.Page) {
	cur := page.Parent
	for cur != nil {
		if cur.FilePath != "" {
			needsRender[cur.FilePath] = true
		}
		cur = cur.Parent
	}
}

// collectChangedTags returns all tags that appear in changed pages.
func collectChangedTags(changed map[string]*content.Page) map[string]bool {
	tags := make(map[string]bool)
	for _, p := range changed {
		for _, t := range p.Tags {
			tags[t] = true
		}
	}
	return tags
}

// renderTagPages renders all tag term pages and the taxonomy overview.
func renderTagPages(renderer *render.Renderer, pages []*content.Page, outputDir string) error {
	tagCounts := content.TagCounts(pages)
	tagPages := make(map[string][]*content.Page)
	for _, p := range pages {
		if p.Draft {
			continue
		}
		for _, t := range p.Tags {
			tagPages[t] = append(tagPages[t], p)
		}
	}

	// Sort each tag's pages by date desc
	for _, pgs := range tagPages {
		sort.Slice(pgs, func(i, j int) bool {
			return pgs[i].Date.After(pgs[j].Date)
		})
	}

	// Render individual tag pages
	for tag, pgs := range tagPages {
		if err := renderer.RenderTagPage(tag, pgs, outputDir); err != nil {
			return fmt.Errorf("render tag %q: %w", tag, err)
		}
	}

	// Build tag counts for taxonomy page
	var tagCountSlice []render.TagCount
	for tag, count := range tagCounts {
		tagCountSlice = append(tagCountSlice, render.TagCount{Name: tag, Count: count})
	}
	sort.Slice(tagCountSlice, func(i, j int) bool {
		if tagCountSlice[i].Count != tagCountSlice[j].Count {
			return tagCountSlice[i].Count > tagCountSlice[j].Count
		}
		return tagCountSlice[i].Name < tagCountSlice[j].Name
	})

	return renderer.RenderTaxonomyPage(tagCountSlice, outputDir)
}

// recordToPage converts a cache.PageRecord to a content.Page.
func recordToPage(rec *cache.PageRecord, cfg *Config) *content.Page {
	return &content.Page{
		FilePath:      rec.FilePath,
		FileHash:      rec.FileHash,
		RelPermalink:  rec.Permalink,
		OutputPath:    content.OutputPathFromPermalink(cfg.OutputDir, rec.Permalink),
		Title:         rec.Title,
		LinkTitle:     rec.LinkTitle,
		Description:   rec.Description,
		Date:          rec.Date,
		Tags:          rec.Tags,
		Draft:         rec.Draft,
		Weight:        rec.Weight,
		Type:          rec.Type,
		NoIndex:       rec.NoIndex,
		ExcludeSearch: rec.ExcludeSearch,
		Params:        rec.Params,
		Kind:          rec.Kind,
		Lang:          rec.Lang,
		Section:       rec.Section,
		Depth:         rec.Depth,
		WordCount:     rec.WordCount,
		ReadingTime:   rec.ReadingTime,
		Summary:       rec.Summary,
		ContentPlain:  rec.ContentPlain,
	}
}

// pageToRecord converts a content.Page to a cache.PageRecord.
func pageToRecord(page *content.Page) *cache.PageRecord {
	return &cache.PageRecord{
		FilePath:      page.FilePath,
		FileHash:      page.FileHash,
		Permalink:     page.RelPermalink,
		Title:         page.Title,
		LinkTitle:     page.LinkTitle,
		Description:   page.Description,
		Date:          page.Date,
		Tags:          page.Tags,
		Draft:         page.Draft,
		Weight:        page.Weight,
		Type:          page.Type,
		NoIndex:       page.NoIndex,
		ExcludeSearch: page.ExcludeSearch,
		Params:        page.Params,
		Kind:          page.Kind,
		Lang:          page.Lang,
		Section:       page.Section,
		Depth:         page.Depth,
		WordCount:     page.WordCount,
		ReadingTime:   page.ReadingTime,
		Summary:       page.Summary,
		ContentPlain:  page.ContentPlain,
	}
}
