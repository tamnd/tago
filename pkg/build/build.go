package build

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tamnd/tago/pkg/asset"
	"github.com/tamnd/tago/pkg/cache"
	"github.com/tamnd/tago/pkg/content"
	"github.com/tamnd/tago/pkg/index"
	"github.com/tamnd/tago/pkg/render"
)

// Config holds the build configuration.
type Config struct {
	ContentDir     string
	OutputDir      string
	StaticDir      string
	ThemeStaticDir string // optional: themes/<name>/static (overrides StaticDir for CSS/JS refs)
	LayoutsDir     string
	BaseURL        string
	DefaultLang    string
	SiteTitle      string
	SiteDesc       string
	EditURLBase    string
	Clean          bool
	LiveReload     bool
}

// Stats holds build statistics.
type Stats struct {
	PagesRebuilt int
	PagesTotal   int
	Duration     time.Duration
}

// renderVersion is bumped whenever the markdown renderer changes in a way that
// alters HTML output (new extensions, link rewriting, etc.). On mismatch,
// tago forces a full re-render by setting layoutChanged=true, which causes
// every page's output file to be re-rendered from its source file.
const renderVersion = "7"

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

	// Detect renderer version change OR layout file changes — both force a full re-render
	// of every page (including section/home pages that have no body text).
	layoutChanged := false

	if storedVer, _ := cacheDB.GetState("render_version"); storedVer != renderVersion {
		log.Printf("tago: render_version changed (%q → %q), forcing full re-render",
			storedVer, renderVersion)
		if err := cacheDB.ClearContentHTML(); err != nil {
			log.Printf("tago: warn: clear content_html: %v", err)
		}
		_ = cacheDB.SetState("render_version", renderVersion)
		layoutChanged = true
	}

	// Hash layout + theme-static dirs so template edits auto-trigger a full re-render
	// without needing a manual renderVersion bump.
	layoutsHash := hashDirMeta(cfg.LayoutsDir) + "|" + hashDirMeta(cfg.ThemeStaticDir)
	if storedHash, _ := cacheDB.GetState("layouts_hash"); storedHash != layoutsHash {
		if storedHash != "" {
			log.Printf("tago: layouts changed, forcing full re-render")
		}
		_ = cacheDB.SetState("layouts_hash", layoutsHash)
		layoutChanged = true
	}

	// Step 1: Load stat map for mtime pre-filter, then scan .md files.
	statMap, err := cacheDB.LoadStatMap()
	if err != nil {
		statMap = map[string]cache.StatEntry{} // non-fatal: fall back to full hash
	}
	diskHashes, err := scanMarkdownFiles(cfg.ContentDir, statMap)
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
		// Capture stat fields so the next build can skip hashing this file.
		if info, serr := os.Stat(filePath); serr == nil {
			page.FileSize = info.Size()
			page.FileMtime = info.ModTime().UnixNano()
		}
		changedPages[filePath] = page
	}

	// Step 5: Load metadata for unchanged pages from cache
	cachedRecords, err := cacheDB.LoadAll()
	if err != nil {
		return nil, fmt.Errorf("load cache: %w", err)
	}

	// Build page set: start with cached unchanged pages
	changedSet := make(map[string]bool, len(changedFiles))
	for _, f := range changedFiles {
		changedSet[f] = true
	}
	deletedSet := make(map[string]bool, len(deletedFiles))
	for _, f := range deletedFiles {
		deletedSet[f] = true
	}

	// cachedPageMap: filePath → cached page metadata (for structural diff).
	cachedPageMap := make(map[string]*content.Page, len(cachedRecords))
	for _, rec := range cachedRecords {
		cachedPageMap[rec.FilePath] = recordToPage(rec, cfg)
	}

	allPages := make(map[string]*content.Page)
	for _, rec := range cachedRecords {
		if changedSet[rec.FilePath] || deletedSet[rec.FilePath] {
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

	// If output file is missing (e.g. empty public/ after cache miss), force re-render.
	// Also force re-render of ALL pages when the layout/render version changed — this
	// catches section and home pages that have no body text and therefore skip the
	// content_html-based check below.
	for _, p := range allPages {
		if needsRender[p.FilePath] {
			continue
		}
		if p.OutputPath == "" {
			continue
		}
		if layoutChanged {
			needsRender[p.FilePath] = true
		} else if _, err := os.Stat(p.OutputPath); os.IsNotExist(err) {
			needsRender[p.FilePath] = true
		}
	}

	// Step 7 & 8: Render pages
	assetRefs, err := asset.ProcessAssets(cfg.StaticDir, cfg.OutputDir)
	if err != nil {
		log.Printf("tago: asset processing error: %v", err)
	}
	// Theme static assets override site static refs (CSS, JS).  Both directories
	// are copied to public/; theme files land on top.
	if cfg.ThemeStaticDir != "" {
		themeRefs, terr := asset.ProcessAssets(cfg.ThemeStaticDir, cfg.OutputDir)
		if terr != nil {
			log.Printf("tago: theme asset processing error: %v", terr)
		} else {
			if themeRefs.CSS != "" {
				assetRefs.CSS = themeRefs.CSS
			}
			if themeRefs.JS != "" {
				assetRefs.JS = themeRefs.JS
			}
			if themeRefs.JSHead != "" {
				assetRefs.JSHead = themeRefs.JSHead
			}
			if themeRefs.FlexSearch != "" {
				assetRefs.FlexSearch = themeRefs.FlexSearch
			}
			for k, v := range themeRefs.Extra {
				if assetRefs.Extra == nil {
					assetRefs.Extra = make(map[string]string)
				}
				assetRefs.Extra[k] = v
			}
		}
	}

	// chroma.css is regenerated every build — use a fixed URL so all pages always
	// get the current version without needing to re-render for a fingerprint change.
	if _, err := content.WriteChromaCSS(filepath.Join(cfg.OutputDir, "css", "chroma.css")); err != nil {
		log.Printf("tago: chroma css: %v", err)
	}
	chromaCSSURL := "/css/chroma.css"

	// Detect pages whose output file has an empty content div — this catches
	// any stale output written before the content_html column was removed.
	for _, p := range allPages {
		if needsRender[p.FilePath] || p.OutputPath == "" {
			continue
		}
		if outputHasEmptyContent(p.OutputPath) {
			needsRender[p.FilePath] = true
		}
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
		ChromaCSS:  chromaCSSURL,
		Extra:      assetRefs.Extra,
	}

	renderer := render.New(site, rAssets, cfg.LayoutsDir, cfg.LiveReload)

	pagesRebuilt := 0
	var toSave []*cache.PageRecord

	// Render pages that need updating
	for _, page := range pageSlice {
		shouldRender := needsRender[page.FilePath] || (page.FilePath == "" /* special pages */)
		if !shouldRender {
			continue
		}
		// ContentHTML is no longer cached in the DB; re-parse from disk for any
		// page that needs rendering but wasn't in changedPages (e.g. ancestors
		// re-rendered due to a child change, or layout-change forced re-renders).
		if page.ContentHTML == "" && page.FilePath != "" {
			if reparsed, rerr := content.ParseAndRender(page.FilePath); rerr == nil {
				page.ContentHTML = reparsed.ContentHTML
				page.ContentPlain = reparsed.ContentPlain
			}
		}
		if err := renderer.RenderPage(page, pageSlice); err != nil {
			log.Printf("tago: render error for %s: %v", page.RelPermalink, err)
			continue
		}
		pagesRebuilt++

		if page.FilePath != "" {
			toSave = append(toSave, pageToRecord(page))
		}
	}

	// Flush all cache updates in a single transaction.
	if err := cacheDB.SaveBatch(toSave); err != nil {
		log.Printf("tago: cache batch save error: %v", err)
	}

	// Re-render tag pages if any changed page has tags
	changedTags := collectChangedTags(changedPages)
	if len(changedTags) > 0 || len(deletedFiles) > 0 {
		if err := renderTagPages(renderer, pageSlice, cfg.OutputDir); err != nil {
			log.Printf("tago: tag page render error: %v", err)
		}
	}

	// Step 9: Rebuild global indexes, scoped by what actually changed.
	//
	// structuralChange: pages added/deleted or metadata (title/tags/section/kind)
	//   changed → graph, tree, calendar, 404 all depend on page structure.
	// contentChange: any file changed (content or metadata) → search + sitemap.
	contentChange := len(changedFiles) > 0 || len(deletedFiles) > 0 || layoutChanged
	structuralChange := len(deletedFiles) > 0 || structurallyChanged(changedPages, cachedPageMap) || layoutChanged

	// Populate ContentPlain for unchanged cached pages only when the search index
	// needs rebuilding. LoadAll() omits content_plain (large) to keep the common
	// incremental path fast; this lazy-loads it only on contentChange builds.
	if contentChange {
		if plainTexts, err := cacheDB.LoadPlainTexts(); err == nil {
			for _, p := range pageSlice {
				if p.ContentPlain == "" && p.FilePath != "" {
					p.ContentPlain = plainTexts[p.FilePath]
				}
			}
		}
	}

	type indexTask struct {
		name string
		run  func() error
	}
	var tasks []indexTask
	if contentChange {
		tasks = append(tasks,
			indexTask{"search", func() error { return index.BuildSearchJSON(pageSlice, cfg.OutputDir) }},
			indexTask{"sitemap", func() error { return index.BuildSitemap(pageSlice, cfg.BaseURL, cfg.OutputDir) }},
			indexTask{"search-page", func() error { return renderer.RenderSearchPage(cfg.OutputDir) }},
		)
	}
	if structuralChange {
		tasks = append(tasks,
			indexTask{"graph", func() error { return index.BuildGraphJSON(pageSlice, cfg.OutputDir) }},
			indexTask{"tree", func() error { return index.BuildTreeJSON(pageSlice, cfg.OutputDir) }},
			indexTask{"calendar", func() error { return index.BuildCalendarJSON(pageSlice, cfg.OutputDir) }},
			indexTask{"404", func() error { return renderer.Render404(cfg.OutputDir, pageSlice) }},
		)
	}
	if len(tasks) > 0 {
		var twg sync.WaitGroup
		for _, t := range tasks {
			twg.Add(1)
			go func(t indexTask) {
				defer twg.Done()
				if err := t.run(); err != nil {
					log.Printf("tago: %s error: %v", t.name, err)
				}
			}(t)
		}
		twg.Wait()
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

type fileInfo struct {
	path  string
	size  int64
	mtime int64
}

// scanMarkdownFiles walks contentDir and returns file_path → sha256 hash.
// statCache provides (size, mtime, hash) from the previous build; files whose
// stat fields match are reused without reading the file content.
// Remaining files are hashed in parallel using a worker pool sized to NumCPU.
func scanMarkdownFiles(contentDir string, statCache map[string]cache.StatEntry) (map[string]string, error) {
	var toHash []fileInfo
	out := make(map[string]string)

	if err := filepath.Walk(contentDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		size := info.Size()
		mtime := info.ModTime().UnixNano()
		if e, ok := statCache[path]; ok && e.Size == size && e.Mtime == mtime {
			out[path] = e.Hash // stat hit — skip reading the file
		} else {
			toHash = append(toHash, fileInfo{path, size, mtime})
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if len(toHash) == 0 {
		return out, nil
	}

	type result struct {
		fi   fileInfo
		hash string
		err  error
	}

	jobs := make(chan fileInfo, len(toHash))
	results := make(chan result, len(toHash))

	workers := runtime.NumCPU()
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for fi := range jobs {
				h, err := content.HashFile(fi.path)
				results <- result{fi, h, err}
			}
		}()
	}
	for _, fi := range toHash {
		jobs <- fi
	}
	close(jobs)
	go func() { wg.Wait(); close(results) }()

	for r := range results {
		if r.err != nil {
			return nil, r.err
		}
		out[r.fi.path] = r.hash
	}
	return out, nil
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
		FileSize:      rec.FileSize,
		FileMtime:     rec.FileMtime,
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
		ContentHTML:   rec.ContentHTML,
	}
}

// structurallyChanged reports whether any changed page has different structural
// metadata (title, tags, section, kind, date) compared to its cached version,
// or is a brand-new page not previously in the cache.
// Graph, tree, and calendar only need rebuilding on structural changes.
func structurallyChanged(changed map[string]*content.Page, cached map[string]*content.Page) bool {
	for path, p := range changed {
		prev, existed := cached[path]
		if !existed {
			return true // new page
		}
		if p.Title != prev.Title || p.Section != prev.Section || p.Kind != prev.Kind {
			return true
		}
		if !p.Date.Equal(prev.Date) {
			return true
		}
		if len(p.Tags) != len(prev.Tags) {
			return true
		}
		for i, t := range p.Tags {
			if t != prev.Tags[i] {
				return true
			}
		}
	}
	return false
}

// pageToRecord converts a content.Page to a cache.PageRecord.
func pageToRecord(page *content.Page) *cache.PageRecord {
	return &cache.PageRecord{
		FilePath:      page.FilePath,
		FileHash:      page.FileHash,
		FileSize:      page.FileSize,
		FileMtime:     page.FileMtime,
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
		ContentHTML:   page.ContentHTML,
	}
}

// outputHasEmptyContent reports whether an output HTML file was rendered with
// blank ContentHTML — detectable by the empty content div pattern.
func outputHasEmptyContent(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return bytes.Contains(data, []byte(`class="content"></div>`))
}

// hashDirMeta computes a short hash of the filenames, sizes, and mtimes of all
// files under dir (non-recursive skipped: walk is recursive). Returns "" for
// empty or missing dirs. Used to detect layout/static file changes without
// reading file contents.
func hashDirMeta(dir string) string {
	if dir == "" {
		return ""
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return ""
	}
	h := sha256.New()
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		fmt.Fprintf(h, "%s:%d:%d\n", path, info.Size(), info.ModTime().UnixNano())
		return nil
	})
	sum := fmt.Sprintf("%x", h.Sum(nil))
	return sum[:16]
}
