// Package shard splits a tago/hugo build output into per-subdomain deployments.
// It copies files, rewrites URLs, splits search data, generates sitemaps, and
// writes Cloudflare Pages _redirects files.
package shard

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// textSuffixes are the file types we rewrite URLs in.
var textSuffixes = map[string]bool{
	".html": true, ".css": true, ".js": true, ".json": true,
	".xml": true, ".webmanifest": true, ".txt": true,
}

// sharedRootDirs are copied verbatim to every shard output.
var sharedRootDirs = []string{"css", "js"}

// sharedRootFiles are copied verbatim to every shard output.
var sharedRootFiles = []string{
	"404.html", "apple-touch-icon.png",
	"favicon-16x16.png", "favicon-32x32.png",
	"favicon.ico", "favicon.svg",
	"site.webmanifest",
}

// localSharedPrefixes are URL paths that must stay local even in a shard context.
var localSharedPrefixes = []string{
	"/css/", "/js/", "/_worker",
	"/apple-touch-icon.png", "/favicon",
	"/site.webmanifest", "/en.search-data.json", "/sitemap.xml",
}

// -------------------------------------------------------------------------
// Data types
// -------------------------------------------------------------------------

// Site describes one Cloudflare Pages deployment target.
type Site struct {
	Name         string // "main", "leetcode", …
	Project      string // Cloudflare Pages project name
	Domain       string // e.g. "brain.tamnd.com"
	Output       string // destination directory
	FileBudget   int    // max files allowed (0 = unlimited)
	SourcePrefix string // e.g. "/practice/kvant/" — empty for main
	RangeMin     int    // lower bound (inclusive) on the numeric first path
	RangeMax     int    // segment under SourcePrefix; 0/0 = whole subtree.
	// Several shards may share one SourcePrefix and partition it by a numeric
	// first segment (e.g. a codeforces contest id) using RangeMin/RangeMax.
	// This lets one prefix span multiple Cloudflare projects when it outgrows
	// the 20,000-files-per-deployment limit.
}

// hasRange reports whether the shard partitions its SourcePrefix by a numeric
// first-segment range instead of owning the whole subtree.
func (s *Site) hasRange() bool { return s.RangeMin > 0 || s.RangeMax > 0 }

// isPrimaryRange reports whether this shard owns the non-numeric pages under a
// partitioned prefix (the section index and shared assets). The primary is the
// shard whose lower bound is zero.
func (s *Site) isPrimaryRange() bool { return s.RangeMin == 0 }

// idInRange reports whether a numeric first segment belongs to this shard.
func (s *Site) idInRange(id int) bool {
	if id < s.RangeMin {
		return false
	}
	if s.RangeMax > 0 && id > s.RangeMax {
		return false
	}
	return true
}

// BaseURL returns "https://<domain>".
func (s *Site) BaseURL() string { return "https://" + s.Domain }

// Config is the parsed deploy-shards.toml.
type Config struct {
	Main   *Site
	Shards []*Site
}

// Options controls the Split operation.
type Options struct {
	ConfigFile   string // path to deploy-shards.toml  (default: deploy-shards.toml)
	PublicDir    string // path to built public/ dir    (default: public)
	ContentRoot  string // for lastmods path mapping    (default: content/en)
	LastmodsFile string // incremental cache            (default: content-lastmods.json)
	SummaryFile  string // JSON summary output          (default: deploy-shards-summary.json)
	Incremental  bool   // use manifest-based change detection; falls back to full split on miss
	ManifestFile string // path to public/ size manifest (default: public-manifest.json)
}

// SiteSummary is one entry in the JSON summary written after a split.
type SiteSummary struct {
	Site    string `json:"site"`
	Domain  string `json:"domain"`
	Project string `json:"project"`
	Files   int    `json:"files"`
	Bytes   int64  `json:"bytes"`
}

// -------------------------------------------------------------------------
// TOML parsing (deploy-shards.toml format only)
// -------------------------------------------------------------------------

func slashWrap(p string) string {
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p
}

func trimComment(s string) string {
	for i, c := range s {
		if c == '#' {
			return strings.TrimSpace(s[:i])
		}
	}
	return strings.TrimSpace(s)
}

func parseKV(line string) (key, val string, ok bool) {
	line = trimComment(line)
	idx := strings.IndexByte(line, '=')
	if idx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:idx])
	val = strings.TrimSpace(line[idx+1:])
	if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
		val = val[1 : len(val)-1]
	}
	if key == "" || val == "" {
		return "", "", false
	}
	return key, val, true
}

func applyField(s *Site, key, val string) {
	switch key {
	case "name":
		s.Name = val
	case "project":
		s.Project = val
	case "domain":
		s.Domain = val
	case "output":
		s.Output = val
	case "file_budget":
		if n, err := strconv.Atoi(val); err == nil {
			s.FileBudget = n
		}
	case "source_prefix":
		s.SourcePrefix = slashWrap(val)
	case "range_min":
		if n, err := strconv.Atoi(val); err == nil {
			s.RangeMin = n
		}
	case "range_max":
		if n, err := strconv.Atoi(val); err == nil {
			s.RangeMax = n
		}
	}
}

// firstSegmentID parses the first path segment of rel (which begins with "/")
// as an integer, e.g. "/1234/A/" → 1234. ok is false when the segment is empty
// or non-numeric.
func firstSegmentID(rel string) (id int, ok bool) {
	s := strings.TrimPrefix(rel, "/")
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

// ParseConfig parses a deploy-shards.toml byte slice.
func ParseConfig(data []byte) (*Config, error) {
	cfg := &Config{}
	var current *Site

	for _, rawLine := range strings.Split(string(data), "\n") {
		line := trimComment(rawLine)
		if line == "" {
			continue
		}
		if line == "[[shards]]" {
			current = &Site{}
			cfg.Shards = append(cfg.Shards, current)
			continue
		}
		if line == "[main]" {
			if cfg.Main == nil {
				cfg.Main = &Site{Name: "main"}
			}
			current = cfg.Main
			continue
		}
		// Skip other section headers
		if len(line) > 2 && line[0] == '[' && line[len(line)-1] == ']' {
			current = nil
			continue
		}
		if current == nil {
			continue
		}
		k, v, ok := parseKV(rawLine)
		if ok {
			applyField(current, k, v)
		}
	}

	if cfg.Main == nil {
		return nil, fmt.Errorf("shard config: missing [main] section")
	}
	return cfg, nil
}

// -------------------------------------------------------------------------
// URL mapping
// -------------------------------------------------------------------------

// combinedRe matches all URL patterns in a single scan.
// Groups: [1,2,3]=canonical  [4,5]=attr  [6]=css-double  [7]=css-single  [8]=css-bare
var combinedRe = regexp.MustCompile(
	`(<link rel="canonical" href=")([^"]+)(")` +
		`|((?:href|src|action|content|poster)=["'])(/[^"'<>{}\s]*)` +
		`|url\("(/[^"]+)"\)` +
		`|url\('(/[^']+)'\)` +
		`|url\((/[^)'"\s]+)\)`,
)

// relLinkRe matches href/src values that are relative paths (not absolute, not
// an anchor). combinedRe only rewrites absolute "/…" URLs, so relative links
// like href="1234/" pass through untouched. Those break when the target moves
// to a sibling shard (e.g. a codeforces contest above the split cutoff), so we
// resolve them against the page's own path and rewrite the cross-shard ones.
var relLinkRe = regexp.MustCompile(`((?:href|src)=["'])([^"'/#][^"'<>{}\s]*)(["'])`)

// hasURLScheme reports whether s begins with a scheme like "https:", "mailto:",
// "tel:", or "data:" (a ':' before any '/', '.', '?' or '#').
func hasURLScheme(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ':' {
			return true
		}
		if c == '/' || c == '.' || c == '?' || c == '#' {
			return false
		}
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '+' || c == '-') {
			return false
		}
	}
	return false
}

// resolveRelative joins a relative URL onto baseDir (which ends with "/"),
// cleaning any "../" segments while preserving a trailing slash and any
// query/anchor suffix.
func resolveRelative(baseDir, rel string) string {
	core, suffix := rel, ""
	if i := strings.IndexAny(rel, "?#"); i >= 0 {
		core, suffix = rel[:i], rel[i:]
	}
	joined := path.Join(baseDir, core)
	if strings.HasSuffix(core, "/") && !strings.HasSuffix(joined, "/") {
		joined += "/"
	}
	return joined + suffix
}

func isLocalShared(url string) bool {
	for _, p := range localSharedPrefixes {
		if strings.HasPrefix(url, p) {
			return true
		}
	}
	return false
}

// pageBaseDir returns the URL directory that relative links inside the file at
// output-relative path rel (on site current) resolve against. For a shard the
// output root maps to SourcePrefix; for main it maps to "/".
func pageBaseDir(current *Site, rel string) string {
	rel = filepath.ToSlash(rel)
	var urlPath string
	if current.SourcePrefix != "" {
		urlPath = current.SourcePrefix + rel // SourcePrefix ends with "/"
	} else {
		urlPath = "/" + rel
	}
	dir := path.Dir(urlPath)
	if dir == "/" {
		return "/"
	}
	return dir + "/"
}

func splitSuffix(url string) (path, suffix string) {
	for i, c := range url {
		if c == '?' || c == '#' {
			return url[:i], url[i:]
		}
	}
	return url, ""
}

// shardPathFor returns the shard-relative path for url if it belongs to shard.
// It preserves the trailing-slash behaviour of the input: directory-style paths
// (/path/) stay as directories; file paths (/path/file.png) stay as files.
func shardPathFor(urlPath string, shard *Site) (string, bool) {
	if shard.SourcePrefix == "" {
		return "", false
	}
	if !strings.HasPrefix(urlPath, "/") {
		urlPath = "/" + urlPath
	}
	// Use a trailing-slash normalised copy ONLY for prefix comparison, so that
	// "/practice/kvant" still matches prefix "/practice/kvant/" without adding
	// a spurious slash to the result.
	compare := urlPath
	if !strings.HasSuffix(compare, "/") {
		compare += "/"
	}
	var remaining string
	switch {
	case compare == shard.SourcePrefix:
		remaining = "/"
	case strings.HasPrefix(compare, shard.SourcePrefix):
		// Strip prefix from the original urlPath (preserves file vs. directory).
		// SourcePrefix ends with "/"; trim the last char so we keep the leading "/".
		remaining = urlPath[len(shard.SourcePrefix)-1:]
		if !strings.HasPrefix(remaining, "/") {
			remaining = "/" + remaining
		}
	default:
		return "", false
	}
	// When several shards partition one prefix by contest id, gate on the range:
	// numeric first segments must fall in [RangeMin, RangeMax]; non-numeric pages
	// (section index, assets) belong only to the primary shard.
	if shard.hasRange() {
		if id, ok := firstSegmentID(remaining); ok {
			if !shard.idInRange(id) {
				return "", false
			}
		} else if !shard.isPrimaryRange() {
			return "", false
		}
	}
	return remaining, true
}

// mapURL rewrites a single absolute path URL for the context of current site.
func mapURL(url string, current, main *Site, shards []*Site) string {
	if !strings.HasPrefix(url, "/") || strings.HasPrefix(url, "//") {
		return url
	}
	// Shared assets stay local on all sites.
	if current.Name != "main" && isLocalShared(url) {
		return url
	}
	rawPath, suffix := splitSuffix(url)

	for _, shard := range shards {
		mapped, ok := shardPathFor(rawPath, shard)
		if !ok {
			continue
		}
		if current.Name == shard.Name {
			return mapped + suffix
		}
		return shard.BaseURL() + mapped + suffix
	}
	if current.Name == "main" {
		return url
	}
	return main.BaseURL() + url
}

// rewriteText applies all URL rewrites to an HTML/CSS/JS/… file body in a
// single regex scan (combinedRe) instead of five separate passes.
func rewriteText(text string, baseDir string, current, main *Site, shards []*Site) string {
	mapFn := func(u string) string { return mapURL(u, current, main, shards) }

	text = combinedRe.ReplaceAllStringFunc(text, func(m string) string {
		subs := combinedRe.FindStringSubmatch(m)
		if len(subs) < 9 {
			return m
		}
		switch {
		case subs[1] != "": // canonical: prefix=subs[1] url=subs[2] quote=subs[3]
			prefix, oldURL, quote := subs[1], subs[2], subs[3]
			var urlPath string
			for _, base := range []string{main.BaseURL(), "https://brain.tamnd.com"} {
				if strings.HasPrefix(oldURL, base) {
					urlPath = oldURL[len(base):]
					if urlPath == "" {
						urlPath = "/"
					}
					break
				}
			}
			if urlPath == "" {
				return m
			}
			mapped := mapFn(urlPath)
			if strings.HasPrefix(mapped, "/") {
				mapped = current.BaseURL() + mapped
			}
			return prefix + mapped + quote

		case subs[4] != "": // attr: attrPrefix=subs[4] url=subs[5]
			attrPrefix, url := subs[4], subs[5]
			mapped := mapFn(url)
			if mapped == url {
				return m
			}
			return attrPrefix + mapped

		case subs[6] != "": // css url("...")
			mapped := mapFn(subs[6])
			if mapped == subs[6] {
				return m
			}
			return `url("` + mapped + `")`

		case subs[7] != "": // css url('...')
			mapped := mapFn(subs[7])
			if mapped == subs[7] {
				return m
			}
			return "url('" + mapped + "')"

		case subs[8] != "": // css url(...)
			mapped := mapFn(subs[8])
			if mapped == subs[8] {
				return m
			}
			return "url(" + mapped + ")"
		}
		return m
	})

	// Second pass: rewrite relative href/src links that now cross a shard
	// boundary. combinedRe only touches absolute "/…" URLs, so a relative link
	// like href="104500/" in a listing page would still point at the current
	// shard after a range split. Resolve each relative link against the page's
	// own directory and, when the resolved target lives on a sibling shard,
	// rewrite it to that shard's absolute URL. Same-shard relatives are left
	// alone to keep the diff minimal. Only relevant when a ranged shard exists.
	if baseDir != "" && anyRanged(shards) {
		text = relLinkRe.ReplaceAllStringFunc(text, func(m string) string {
			subs := relLinkRe.FindStringSubmatch(m)
			if len(subs) < 4 {
				return m
			}
			prefix, rel, quote := subs[1], subs[2], subs[3]
			if hasURLScheme(rel) {
				return m
			}
			mapped := mapFn(resolveRelative(baseDir, rel))
			if strings.HasPrefix(mapped, "http") {
				return prefix + mapped + quote
			}
			return m
		})
	}

	return text
}

// anyRanged reports whether any shard partitions its prefix by contest-id range.
func anyRanged(shards []*Site) bool {
	for _, s := range shards {
		if s.hasRange() {
			return true
		}
	}
	return false
}

// rewriteNeeded is a fast byte-level pre-check before running any regex.
// It returns false when the file obviously has nothing to rewrite.
func rewriteNeeded(data []byte, current, main *Site, shards []*Site) bool {
	// Any file containing a shard SourcePrefix needs rewriting.
	for _, s := range shards {
		if bytes.Contains(data, []byte(s.SourcePrefix)) {
			return true
		}
	}
	// On shard sites, files containing the main site's canonical domain need
	// rewriting (canonical tag fix). On main, same check for shard domains.
	if bytes.Contains(data, []byte(main.BaseURL())) {
		return true
	}
	for _, s := range shards {
		if bytes.Contains(data, []byte(s.BaseURL())) {
			return true
		}
	}
	return false
}

func rewriteFile(path string, baseDir string, current, main *Site, shards []*Site) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	// Fast pre-check: skip regex entirely if no known shard/domain markers present.
	if !rewriteNeeded(data, current, main, shards) {
		return false, nil
	}
	original := string(data)
	rewritten := rewriteText(original, baseDir, current, main, shards)
	if rewritten == original {
		return false, nil
	}
	// Remove before write to break any hard link created by copyFile.
	_ = os.Remove(path)
	return true, os.WriteFile(path, []byte(rewritten), 0644)
}

// rewriteTree rewrites all text files under root in parallel goroutines.
func rewriteTree(root string, current, main *Site, shards []*Site) (int, error) {
	var files []string
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if textSuffixes[strings.ToLower(filepath.Ext(path))] {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		return 0, err
	}

	workers := runtime.NumCPU() * 2
	if workers < 4 {
		workers = 4
	}
	if workers > 32 {
		workers = 32
	}

	var (
		mu       sync.Mutex
		count    int
		firstErr error
		wg       sync.WaitGroup
		sem      = make(chan struct{}, workers)
	)

	for _, f := range files {
		wg.Add(1)
		sem <- struct{}{}
		go func(path string) {
			defer wg.Done()
			defer func() { <-sem }()
			rel := strings.TrimPrefix(strings.TrimPrefix(path, root), string(filepath.Separator))
			changed, err := rewriteFile(path, pageBaseDir(current, rel), current, main, shards)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
			} else if changed {
				count++
			}
		}(f)
	}
	wg.Wait()
	return count, firstErr
}

// -------------------------------------------------------------------------
// File copying
// -------------------------------------------------------------------------

// copyFile hard-links src to dst; falls back to data copy on cross-device or
// other link failures. Callers are responsible for pre-creating the directory.
func copyFile(src, dst string) error {
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// copyDir copies src to dst recursively using parallel goroutines for files.
func copyDir(src, dst string) error {
	// First pass: create all directories.
	if err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		return os.MkdirAll(filepath.Join(dst, rel), 0755)
	}); err != nil {
		return err
	}
	// Second pass: collect all file pairs.
	type pair struct{ src, dst string }
	var files []pair
	if err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		files = append(files, pair{path, filepath.Join(dst, rel)})
		return nil
	}); err != nil {
		return err
	}
	// Third pass: parallel file copy.
	workers := runtime.NumCPU() * 4
	if workers < 8 {
		workers = 8
	}
	var (
		mu       sync.Mutex
		firstErr error
		wg       sync.WaitGroup
		sem      = make(chan struct{}, workers)
	)
	for _, p := range files {
		wg.Add(1)
		sem <- struct{}{}
		go func(p pair) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := copyFile(p.src, p.dst); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}(p)
	}
	wg.Wait()
	return firstErr
}

// copyShardRanged copies only the immediate children of src whose numeric name
// falls in the shard's range. Non-numeric children (the section index.html and
// any shared files) go to the primary shard only. Used when several shards
// partition one source_prefix by contest id.
func copyShardRanged(src, dst string, shard *Site) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if id, perr := strconv.Atoi(name); perr == nil {
			if !shard.idInRange(id) {
				continue
			}
		} else if !shard.isPrimaryRange() {
			continue
		}
		s := filepath.Join(src, name)
		d := filepath.Join(dst, name)
		if e.IsDir() {
			if err := copyDir(s, d); err != nil {
				return err
			}
		} else if err := copyFile(s, d); err != nil {
			return err
		}
	}
	return nil
}

// copyTreeExcluding copies src to dst, skipping top-level dirs in excluded, using parallel goroutines.
func copyTreeExcluding(src, dst string, excluded map[string]bool) error {
	type pair struct{ src, dst string }
	var files []pair
	if err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if d.IsDir() && excluded[rel] {
			return filepath.SkipDir
		}
		dstPath := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(dstPath, 0755)
		}
		files = append(files, pair{path, dstPath})
		return nil
	}); err != nil {
		return err
	}
	workers := runtime.NumCPU() * 4
	if workers < 8 {
		workers = 8
	}
	var (
		mu       sync.Mutex
		firstErr error
		wg       sync.WaitGroup
		sem      = make(chan struct{}, workers)
	)
	for _, p := range files {
		wg.Add(1)
		sem <- struct{}{}
		go func(p pair) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := copyFile(p.src, p.dst); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}(p)
	}
	wg.Wait()
	return firstErr
}

func copyShared(publicDir, outDir string) error {
	for _, dir := range sharedRootDirs {
		src := filepath.Join(publicDir, dir)
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		}
		if err := copyDir(src, filepath.Join(outDir, dir)); err != nil {
			return err
		}
	}
	for _, file := range sharedRootFiles {
		src := filepath.Join(publicDir, file)
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		}
		if err := copyFile(src, filepath.Join(outDir, file)); err != nil {
			return err
		}
	}
	// _worker*.js fingerprinted files
	entries, _ := os.ReadDir(publicDir)
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "_worker") && strings.HasSuffix(e.Name(), ".js") {
			src := filepath.Join(publicDir, e.Name())
			if err := copyFile(src, filepath.Join(outDir, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// cleanDir removes .tago-cache.db and files larger than 24 MiB.
func cleanDir(root string) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if d.Name() == ".tago-cache.db" {
			_ = os.Remove(path)
			return nil
		}
		if info, err := d.Info(); err == nil && info.Size() > 24*1024*1024 {
			_ = os.Remove(path)
		}
		return nil
	})
}

// -------------------------------------------------------------------------
// Search data splitting
// -------------------------------------------------------------------------

func splitSearchData(publicDir, mainOutput string, shards []*Site) error {
	src := filepath.Join(publicDir, "en.search-data.json")
	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	mainData := make(map[string]json.RawMessage)
	shardData := make([]map[string]json.RawMessage, len(shards))
	for i := range shards {
		shardData[i] = make(map[string]json.RawMessage)
	}

	for key, val := range raw {
		assigned := false
		for i, shard := range shards {
			if mapped, ok := shardPathFor(key, shard); ok {
				shardData[i][mapped] = val
				assigned = true
				break
			}
		}
		if !assigned {
			mainData[key] = val
		}
	}

	writeJSON := func(path string, m map[string]json.RawMessage) error {
		b, err := json.Marshal(m)
		if err != nil {
			return err
		}
		return os.WriteFile(path, b, 0644)
	}
	if err := writeJSON(filepath.Join(mainOutput, "en.search-data.json"), mainData); err != nil {
		return err
	}
	for i, shard := range shards {
		if err := writeJSON(filepath.Join(shard.Output, "en.search-data.json"), shardData[i]); err != nil {
			return err
		}
	}
	return nil
}

// -------------------------------------------------------------------------
// Git lastmods (incremental cache)
// -------------------------------------------------------------------------

type lastmodsMap map[string]int64

// loadGitLastmods returns lastmod timestamps for content files, using an
// incremental JSON cache so only new commits are scanned on each run.
func loadGitLastmods(cachePath, contentRoot string) (lastmodsMap, error) {
	// Load existing cache.
	var cachedRaw map[string]json.RawMessage
	var sinceTS int64
	if raw, err := os.ReadFile(cachePath); err == nil {
		_ = json.Unmarshal(raw, &cachedRaw)
		if cachedRaw != nil {
			if tsRaw, ok := cachedRaw["__max_ts__"]; ok {
				_ = json.Unmarshal(tsRaw, &sinceTS)
				delete(cachedRaw, "__max_ts__")
			}
		}
	}

	// Build git log command (incremental if we have a prior timestamp).
	args := []string{"log", "--format=@@%ct", "--name-only", "--", contentRoot}
	if sinceTS > 0 {
		after := time.Unix(sinceTS, 0).UTC().Format("2006-01-02T15:04:05Z")
		args = append(args, "--after="+after)
	}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		// git not available or not a git repo — skip lastmods
		return nil, nil
	}

	newDates := parseGitLog(string(out))

	// Merge: convert cached to lastmodsMap then overlay new dates.
	merged := make(lastmodsMap, len(cachedRaw)+len(newDates))
	for k, v := range cachedRaw {
		var ts int64
		if json.Unmarshal(v, &ts) == nil {
			merged[k] = ts
		}
	}
	for k, v := range newDates {
		merged[k] = v
	}

	// Save updated cache.
	var maxTS int64
	for _, ts := range merged {
		if ts > maxTS {
			maxTS = ts
		}
	}
	cacheOut := make(map[string]interface{}, len(merged)+1)
	for k, v := range merged {
		cacheOut[k] = v
	}
	cacheOut["__max_ts__"] = maxTS
	if b, err := json.Marshal(cacheOut); err == nil {
		_ = os.WriteFile(cachePath, b, 0644)
	}

	return propagateParentDates(merged, contentRoot), nil
}

func parseGitLog(text string) lastmodsMap {
	result := make(lastmodsMap)
	var cur int64
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "@@") {
			if ts, err := strconv.ParseInt(strings.TrimSpace(line[2:]), 10, 64); err == nil {
				cur = ts
			}
		} else if line != "" && cur != 0 {
			if _, exists := result[line]; !exists {
				result[line] = cur
			}
		}
	}
	return result
}

func propagateParentDates(dates lastmodsMap, contentRoot string) lastmodsMap {
	result := make(lastmodsMap, len(dates))
	for k, v := range dates {
		result[k] = v
	}
	root := filepath.Clean(contentRoot)
	for filename, date := range dates {
		parent := filepath.Dir(filepath.Clean(filename))
		for {
			rel, err := filepath.Rel(root, parent)
			if err != nil || strings.HasPrefix(rel, "..") {
				break
			}
			key := filepath.ToSlash(parent)
			if existing, ok := result[key]; !ok || date > existing {
				result[key] = date
			}
			if rel == "." {
				break
			}
			parent = filepath.Dir(parent)
		}
	}
	return result
}

// -------------------------------------------------------------------------
// Sitemaps
// -------------------------------------------------------------------------

type xmlURLSet struct {
	XMLName xml.Name `xml:"urlset"`
	Xmlns   string   `xml:"xmlns,attr"`
	URLs    []xmlURL `xml:"url"`
}

type xmlURL struct {
	Loc     string `xml:"loc"`
	Lastmod string `xml:"lastmod,omitempty"`
}

type xmlSitemapIndex struct {
	XMLName  xml.Name     `xml:"sitemapindex"`
	Xmlns    string       `xml:"xmlns,attr"`
	Sitemaps []xmlSitemap `xml:"sitemap"`
}

type xmlSitemap struct {
	Loc     string `xml:"loc"`
	Lastmod string `xml:"lastmod,omitempty"`
}

func pathToURLKey(path, root string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) == 0 || parts[len(parts)-1] != "index.html" {
		return "", false
	}
	if strings.HasPrefix(parts[0], "..") {
		return "", false
	}
	if len(parts) == 1 {
		return "/", true
	}
	return "/" + strings.Join(parts[:len(parts)-1], "/") + "/", true
}

func contentPathForURL(site *Site, urlPath string) string {
	var orig string
	if site.SourcePrefix != "" {
		orig = strings.Trim(site.SourcePrefix, "/")
		if urlPath != "/" {
			orig += "/" + strings.Trim(urlPath, "/")
		}
	} else {
		orig = strings.Trim(urlPath, "/")
	}
	return filepath.Join("content", "en", filepath.FromSlash(orig))
}

func lastmodForURL(site *Site, urlPath string, dates lastmodsMap) string {
	if dates == nil {
		return ""
	}
	base := contentPathForURL(site, urlPath)
	fwd := filepath.ToSlash(base)
	var ts int64
	for _, candidate := range []string{fwd + ".md", fwd + "/_index.md", fwd} {
		if t, ok := dates[candidate]; ok && t > ts {
			ts = t
		}
	}
	if ts == 0 {
		return ""
	}
	return time.Unix(ts, 0).UTC().Format("2006-01-02T15:04:05Z")
}

func writeURLSet(path string, urls []xmlURL) (string, error) {
	urlset := xmlURLSet{
		Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  urls,
	}
	b, err := xml.MarshalIndent(urlset, "", "  ")
	if err != nil {
		return "", err
	}
	content := `<?xml version="1.0" encoding="UTF-8"?>` + "\n" + string(b) + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", err
	}
	var maxLastmod string
	for _, u := range urls {
		if u.Lastmod > maxLastmod {
			maxLastmod = u.Lastmod
		}
	}
	return maxLastmod, nil
}

func writeSitemapIndex(path string, sitemaps []xmlSitemap) error {
	idx := xmlSitemapIndex{
		Xmlns:    "http://www.sitemaps.org/schemas/sitemap/0.9",
		Sitemaps: sitemaps,
	}
	b, err := xml.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	content := `<?xml version="1.0" encoding="UTF-8"?>` + "\n" + string(b) + "\n"
	return os.WriteFile(path, []byte(content), 0644)
}

func writeRobots(site *Site) error {
	body := fmt.Sprintf("User-agent: *\nAllow: /\n\nSitemap: %s/sitemap.xml\n", site.BaseURL())
	return os.WriteFile(filepath.Join(site.Output, "robots.txt"), []byte(body), 0644)
}

func generateSitemaps(main *Site, shards []*Site, dates lastmodsMap) error {
	var shardEntries []xmlSitemap

	for _, shard := range shards {
		var urls []xmlURL
		_ = filepath.WalkDir(shard.Output, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || d.Name() != "index.html" {
				return err
			}
			urlPath, ok := pathToURLKey(path, shard.Output)
			if !ok {
				return nil
			}
			urls = append(urls, xmlURL{
				Loc:     shard.BaseURL() + urlPath,
				Lastmod: lastmodForURL(shard, urlPath, dates),
			})
			return nil
		})
		lastmod, err := writeURLSet(filepath.Join(shard.Output, "sitemap.xml"), urls)
		if err != nil {
			return err
		}
		if err := writeRobots(shard); err != nil {
			return err
		}
		shardEntries = append(shardEntries, xmlSitemap{
			Loc:     shard.BaseURL() + "/sitemap.xml",
			Lastmod: lastmod,
		})
	}

	var mainURLs []xmlURL
	_ = filepath.WalkDir(main.Output, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "index.html" {
			return err
		}
		urlPath, ok := pathToURLKey(path, main.Output)
		if !ok {
			return nil
		}
		mainURLs = append(mainURLs, xmlURL{
			Loc:     main.BaseURL() + urlPath,
			Lastmod: lastmodForURL(main, urlPath, dates),
		})
		return nil
	})
	mainLastmod, err := writeURLSet(filepath.Join(main.Output, "sitemap-main.xml"), mainURLs)
	if err != nil {
		return err
	}

	allSitemaps := []xmlSitemap{{Loc: main.BaseURL() + "/sitemap-main.xml", Lastmod: mainLastmod}}
	allSitemaps = append(allSitemaps, shardEntries...)
	if err := writeSitemapIndex(filepath.Join(main.Output, "sitemap.xml"), allSitemaps); err != nil {
		return err
	}
	return writeRobots(main)
}

// -------------------------------------------------------------------------
// Redirects
// -------------------------------------------------------------------------

func writeRedirects(main *Site, shards []*Site) error {
	var mainLines []string
	for _, shard := range shards {
		p := shard.SourcePrefix
		mainLines = append(mainLines,
			p+" "+shard.BaseURL()+"/ 301",
			p+"* "+shard.BaseURL()+"/:splat 301",
		)
	}
	if err := os.WriteFile(
		filepath.Join(main.Output, "_redirects"),
		[]byte(strings.Join(mainLines, "\n")+"\n"), 0644,
	); err != nil {
		return err
	}
	for _, shard := range shards {
		p := shard.SourcePrefix
		lines := []string{
			p + " / 301",
			p + "* /:splat 301",
			"/" + shard.Name + "/ / 301",
			"/" + shard.Name + "/* /:splat 301",
		}
		if err := os.WriteFile(
			filepath.Join(shard.Output, "_redirects"),
			[]byte(strings.Join(lines, "\n")+"\n"), 0644,
		); err != nil {
			return err
		}
	}
	return nil
}

// -------------------------------------------------------------------------
// Validation
// -------------------------------------------------------------------------

func validateSite(site *Site) (*SiteSummary, error) {
	var count int
	var total int64
	if err := filepath.WalkDir(site.Output, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		count++
		total += info.Size()
		return nil
	}); err != nil {
		return nil, err
	}
	if site.FileBudget > 0 && count > site.FileBudget {
		return nil, fmt.Errorf("%s: %d files exceeds budget %d", site.Name, count, site.FileBudget)
	}
	return &SiteSummary{
		Site:    site.Name,
		Domain:  site.Domain,
		Project: site.Project,
		Files:   count,
		Bytes:   total,
	}, nil
}

// -------------------------------------------------------------------------
// Incremental split helpers
// -------------------------------------------------------------------------

// publicManifest maps public/-relative file paths to their byte sizes.
type publicManifest map[string]int64

func loadManifest(path string) publicManifest {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var m publicManifest
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	return m
}

func saveManifest(publicDir, path string) {
	m := make(publicManifest)
	_ = filepath.WalkDir(publicDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, _ := d.Info()
		if info != nil {
			rel, _ := filepath.Rel(publicDir, p)
			m[filepath.ToSlash(rel)] = info.Size()
		}
		return nil
	})
	if b, err := json.Marshal(m); err == nil {
		_ = os.WriteFile(path, b, 0644)
	}
}

// findChangedByManifest returns public/-relative paths whose size differs from
// the manifest, plus new files not present in the manifest, plus deleted paths
// (size == -1 sentinel) that existed in the manifest but are gone from public/.
func findChangedByManifest(publicDir string, manifest publicManifest) []string {
	current := make(map[string]int64)
	_ = filepath.WalkDir(publicDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, _ := d.Info()
		if info != nil {
			rel, _ := filepath.Rel(publicDir, p)
			current[filepath.ToSlash(rel)] = info.Size()
		}
		return nil
	})

	var changed []string
	for rel, size := range current {
		if prev, ok := manifest[rel]; !ok || prev != size {
			changed = append(changed, rel)
		}
	}
	return changed
}

// isSharedRelPath reports whether relPath is a file shared to every shard output.
func isSharedRelPath(relPath string) bool {
	top := relPath
	if idx := strings.IndexByte(relPath, filepath.Separator); idx >= 0 {
		top = relPath[:idx]
	}
	// Forward-slash form (manifest keys use /)
	if idx := strings.IndexByte(relPath, '/'); idx >= 0 {
		top = relPath[:idx]
	}
	for _, d := range sharedRootDirs {
		if top == d {
			return true
		}
	}
	for _, f := range sharedRootFiles {
		if relPath == f {
			return true
		}
	}
	return strings.HasPrefix(top, "_worker")
}

// copyOrRewriteTo copies srcPath to dstPath for the given site, rewriting URLs
// in text files. Creates parent directories as needed.
func copyOrRewriteTo(srcPath, dstPath string, site, main *Site, shards []*Site) error {
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return err
	}
	if !textSuffixes[strings.ToLower(filepath.Ext(srcPath))] {
		_ = os.Remove(dstPath)
		if err := os.Link(srcPath, dstPath); err == nil {
			return nil
		}
		return copyFile(srcPath, dstPath)
	}
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(dstPath, site.Output), string(filepath.Separator))
	rewritten := rewriteText(string(data), pageBaseDir(site, rel), site, main, shards)
	_ = os.Remove(dstPath)
	return os.WriteFile(dstPath, []byte(rewritten), 0644)
}

// splitIncremental updates only the files that changed in public/ (detected via
// size manifest) inside the cached split output directories.
func splitIncremental(opts Options, cfg *Config, allSites []*Site, dates lastmodsMap, changed []string) ([]SiteSummary, error) {
	t := time.Now()

	fmt.Fprintf(os.Stderr, "tago split [incremental] %d changed files\n", len(changed))

	for _, relPath := range changed {
		srcPath := filepath.Join(opts.PublicDir, filepath.FromSlash(relPath))
		urlPath := "/" + relPath // relPath already uses /

		if isSharedRelPath(relPath) {
			for _, site := range allSites {
				dst := filepath.Join(site.Output, filepath.FromSlash(relPath))
				if err := copyOrRewriteTo(srcPath, dst, site, cfg.Main, cfg.Shards); err != nil {
					return nil, fmt.Errorf("update shared %s in %s: %w", relPath, site.Name, err)
				}
			}
			continue
		}

		var targetSite *Site
		var dstRel string
		for _, shard := range cfg.Shards {
			if mapped, ok := shardPathFor(urlPath, shard); ok {
				targetSite = shard
				dstRel = filepath.FromSlash(strings.TrimPrefix(mapped, "/"))
				break
			}
		}
		if targetSite == nil {
			targetSite = cfg.Main
			dstRel = filepath.FromSlash(relPath)
		}

		dst := filepath.Join(targetSite.Output, dstRel)
		if err := copyOrRewriteTo(srcPath, dst, targetSite, cfg.Main, cfg.Shards); err != nil {
			return nil, fmt.Errorf("update %s: %w", relPath, err)
		}
	}
	t = logPhase("incremental-update", t)

	// Strip any leaked DB files or oversized outputs, same as the full split.
	// Cloudflare Pages rejects the whole deployment on a single file over
	// 25 MiB, and the incremental path used to skip this, so a stale oversized
	// page could survive across runs.
	for _, s := range allSites {
		cleanDir(s.Output)
	}

	if err := splitSearchData(opts.PublicDir, cfg.Main.Output, cfg.Shards); err != nil {
		return nil, fmt.Errorf("split search: %w", err)
	}
	if err := writeRedirects(cfg.Main, cfg.Shards); err != nil {
		return nil, fmt.Errorf("write redirects: %w", err)
	}
	if err := generateSitemaps(cfg.Main, cfg.Shards, dates); err != nil {
		return nil, fmt.Errorf("generate sitemaps: %w", err)
	}
	_ = t

	var summaries []SiteSummary
	for _, s := range allSites {
		sum, err := validateSite(s)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, *sum)
	}
	if b, err := json.MarshalIndent(summaries, "", "  "); err == nil {
		_ = os.WriteFile(opts.SummaryFile, b, 0644)
	}
	return summaries, nil
}

// -------------------------------------------------------------------------
// Main entry point
// -------------------------------------------------------------------------

func logPhase(label string, t time.Time) time.Time {
	fmt.Fprintf(os.Stderr, "tago split [%s] %.1fs\n", label, time.Since(t).Seconds())
	return time.Now()
}

// Split is the full pipeline: copy → clean → rewrite → search → redirects →
// sitemaps → validate. It mirrors split_cloudflare_pages_shards.py but runs
// the URL-rewrite step in parallel Go goroutines (typically 6–10× faster).
func Split(opts Options) ([]SiteSummary, error) {
	if opts.ConfigFile == "" {
		opts.ConfigFile = "deploy-shards.toml"
	}
	if opts.PublicDir == "" {
		opts.PublicDir = "public"
	}
	if opts.ContentRoot == "" {
		opts.ContentRoot = "content/en"
	}
	if opts.LastmodsFile == "" {
		opts.LastmodsFile = "content-lastmods.json"
	}
	if opts.SummaryFile == "" {
		opts.SummaryFile = "deploy-shards-summary.json"
	}

	// 1. Load config.
	cfgData, err := os.ReadFile(opts.ConfigFile)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg, err := ParseConfig(cfgData)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(opts.PublicDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("missing build output: %s", opts.PublicDir)
	}

	allSites := append([]*Site{cfg.Main}, cfg.Shards...)

	if opts.ManifestFile == "" {
		opts.ManifestFile = "public-manifest.json"
	}

	// Load git lastmods (needed for both paths).
	dates, _ := loadGitLastmods(opts.LastmodsFile, opts.ContentRoot)

	// Incremental mode: if manifest + all output dirs exist (cached from previous run),
	// only re-split files whose size changed vs the manifest.
	if opts.Incremental {
		manifest := loadManifest(opts.ManifestFile)
		if manifest != nil {
			allExist := true
			for _, s := range allSites {
				if _, err := os.Stat(s.Output); os.IsNotExist(err) {
					allExist = false
					break
				}
			}
			if allExist {
				changed := findChangedByManifest(opts.PublicDir, manifest)
				return splitIncremental(opts, cfg, allSites, dates, changed)
			}
		}
		fmt.Fprintf(os.Stderr, "tago split [incremental] cache miss — doing full split\n")
	}

	t := time.Now()

	// 2. Remove stale outputs.
	for _, s := range allSites {
		if err := os.RemoveAll(s.Output); err != nil {
			return nil, err
		}
	}
	t = logPhase("remove", t)

	// 3. Copy: main (excluding shard dirs) + each shard + shared assets, all in parallel.
	excluded := make(map[string]bool)
	for _, shard := range cfg.Shards {
		if shard.SourcePrefix != "" {
			excluded[strings.Trim(shard.SourcePrefix, "/")] = true
		}
	}
	// Validate shard sources before launching goroutines.
	for _, shard := range cfg.Shards {
		src := filepath.Join(opts.PublicDir, strings.Trim(shard.SourcePrefix, "/"))
		if _, err := os.Stat(src); os.IsNotExist(err) {
			return nil, fmt.Errorf("missing shard source: %s", src)
		}
	}
	var (
		copyWG  sync.WaitGroup
		copyMu  sync.Mutex
		copyErr error
	)
	copyWG.Add(1)
	go func() {
		defer copyWG.Done()
		if err := copyTreeExcluding(opts.PublicDir, cfg.Main.Output, excluded); err != nil {
			copyMu.Lock()
			if copyErr == nil {
				copyErr = fmt.Errorf("copy main: %w", err)
			}
			copyMu.Unlock()
		}
	}()
	for _, shard := range cfg.Shards {
		shard := shard
		copyWG.Add(1)
		go func() {
			defer copyWG.Done()
			src := filepath.Join(opts.PublicDir, strings.Trim(shard.SourcePrefix, "/"))
			copyShard := copyDir
			if shard.hasRange() {
				copyShard = func(src, dst string) error { return copyShardRanged(src, dst, shard) }
			}
			if err := copyShard(src, shard.Output); err != nil {
				copyMu.Lock()
				if copyErr == nil {
					copyErr = fmt.Errorf("copy shard %s: %w", shard.Name, err)
				}
				copyMu.Unlock()
				return
			}
			if err := copyShared(opts.PublicDir, shard.Output); err != nil {
				copyMu.Lock()
				if copyErr == nil {
					copyErr = fmt.Errorf("copy shared to %s: %w", shard.Name, err)
				}
				copyMu.Unlock()
			}
		}()
	}
	copyWG.Wait()
	if copyErr != nil {
		return nil, copyErr
	}
	t = logPhase("copy", t)

	// 4. Clean oversized / leaked DB files.
	for _, s := range allSites {
		cleanDir(s.Output)
	}

	// 5. Rewrite URLs — run all sites in parallel for maximum throughput.
	var (
		rewriteWG  sync.WaitGroup
		rewriteMu  sync.Mutex
		rewriteErr error
	)
	for _, s := range allSites {
		rewriteWG.Add(1)
		go func(site *Site) {
			defer rewriteWG.Done()
			st := time.Now()
			n, err := rewriteTree(site.Output, site, cfg.Main, cfg.Shards)
			fmt.Fprintf(os.Stderr, "tago split [rewrite:%s] %d changed, %.1fs\n", site.Name, n, time.Since(st).Seconds())
			if err != nil {
				rewriteMu.Lock()
				if rewriteErr == nil {
					rewriteErr = err
				}
				rewriteMu.Unlock()
			}
		}(s)
	}
	rewriteWG.Wait()
	if rewriteErr != nil {
		return nil, fmt.Errorf("rewrite: %w", rewriteErr)
	}
	t = logPhase("rewrite", t)

	// 6. Split search data.
	if err := splitSearchData(opts.PublicDir, cfg.Main.Output, cfg.Shards); err != nil {
		return nil, fmt.Errorf("split search: %w", err)
	}

	// 7. Write _redirects.
	if err := writeRedirects(cfg.Main, cfg.Shards); err != nil {
		return nil, fmt.Errorf("write redirects: %w", err)
	}

	// 8. Sitemaps.
	if err := generateSitemaps(cfg.Main, cfg.Shards, dates); err != nil {
		return nil, fmt.Errorf("generate sitemaps: %w", err)
	}
	t = logPhase("sitemaps+search", t)

	// 9. Validate and collect summary.
	var summaries []SiteSummary
	for _, s := range allSites {
		sum, err := validateSite(s)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, *sum)
	}

	// Write summary JSON.
	if b, err := json.MarshalIndent(summaries, "", "  "); err == nil {
		_ = os.WriteFile(opts.SummaryFile, b, 0644)
	}

	// Save public/ size manifest for next incremental run.
	saveManifest(opts.PublicDir, opts.ManifestFile)

	return summaries, nil
}
