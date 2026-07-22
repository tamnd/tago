package content

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

// LoadOptions configures the content loader.
type LoadOptions struct {
	ContentDir  string
	DefaultLang string
	OutputDir   string
}

// LoadAll walks contentDir, parses all markdown files, and builds the page tree.
// It returns all pages including the home page and section indexes.
func LoadAll(opts LoadOptions) ([]*Page, error) {
	if opts.DefaultLang == "" {
		opts.DefaultLang = "en"
	}

	var pages []*Page

	err := filepath.Walk(opts.ContentDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		page, err := ParseAndRender(path)
		if err != nil {
			return err
		}

		permalink, lang := PermalinkFromPath(opts.ContentDir, path, opts.DefaultLang)
		page.RelPermalink = permalink
		page.Lang = lang
		page.OutputPath = OutputPathFromPermalink(opts.OutputDir, permalink)
		page.Kind = KindFromPath(opts.ContentDir, path)
		page.Section = SectionFromPath(opts.ContentDir, path)
		page.Depth = DepthFromPath(opts.ContentDir, path)

		pages = append(pages, page)
		return nil
	})
	if err != nil {
		return nil, err
	}

	synth := BuildTree(pages)
	for _, s := range synth {
		s.Lang = opts.DefaultLang
		s.OutputPath = OutputPathFromPermalink(opts.OutputDir, s.RelPermalink)
	}
	pages = append(pages, synth...)
	return pages, nil
}

// BuildTree sets Parent, Children, and Ancestors for all pages.
// It builds the hierarchy based on permalinks.
//
// It also synthesizes in-memory section pages for any directory that holds
// content but has no _index.md, mirroring Hugo, which creates a section for
// every such directory. Without this, a page nested under a dir with no
// _index.md would have no parent and drop out of .RegularPagesRecursive even
// though it still renders as a standalone page. The synthesized sections are
// returned so the caller can finish them (output path, permalink) and add them
// to the render set; they carry an empty FilePath, which the build pipeline
// treats as always-render, never-cache.
func BuildTree(pages []*Page) []*Page {
	// Build a map from permalink → page
	byPermalink := make(map[string]*Page, len(pages))
	for _, p := range pages {
		byPermalink[p.RelPermalink] = p
	}

	// Fill in section pages for directories that have content but no _index.md,
	// so every page is reachable from its root section.
	synth := synthesizeMissingSections(pages, byPermalink)

	// From here on, wire the tree over the real pages plus the synthesized ones.
	all := make([]*Page, 0, len(pages)+len(synth))
	all = append(all, pages...)
	all = append(all, synth...)

	for _, p := range all {
		parentPermalink := parentOf(p.RelPermalink)
		if parentPermalink != "" {
			if parent, ok := byPermalink[parentPermalink]; ok {
				p.Parent = parent
			}
		}
	}

	// Set Children
	for _, p := range all {
		if p.Parent != nil {
			p.Parent.Children = append(p.Parent.Children, p)
		}
	}

	// Sort children by weight then date then title
	for _, p := range all {
		if len(p.Children) > 0 {
			sort.Slice(p.Children, func(i, j int) bool {
				ci, cj := p.Children[i], p.Children[j]
				if ci.Weight != cj.Weight {
					return ci.Weight < cj.Weight
				}
				if !ci.Date.Equal(cj.Date) {
					return ci.Date.After(cj.Date)
				}
				return ci.Title < cj.Title
			})
		}
	}

	// A synthesized section carries no date of its own, so give it the newest
	// date among its descendants, the way Hugo dates an auto-created section.
	// Process deepest-first so a parent section sees its child sections' dates.
	sort.SliceStable(synth, func(i, j int) bool { return synth[i].Depth > synth[j].Depth })
	for _, s := range synth {
		s.Date = maxDescendantDate(s)
	}

	// Set Ancestors
	for _, p := range all {
		p.Ancestors = buildAncestors(p)
	}

	// Apply cascade: collect cascade maps from all ancestors (nearest wins),
	// then apply missing fields to each descendant page.
	for _, p := range all {
		// Build merged cascade from root → immediate parent (root first, parent last = nearest wins)
		var cascades []map[string]any
		for _, anc := range p.Ancestors {
			if len(anc.Cascade) > 0 {
				cascades = append(cascades, anc.Cascade)
			}
		}
		if len(cascades) == 0 {
			continue
		}
		// Merge: later (closer ancestor) overrides earlier
		merged := make(map[string]any)
		for _, c := range cascades {
			for k, v := range c {
				merged[k] = v
			}
		}
		applyCascade(p, merged)
	}

	return synth
}

// synthesizeMissingSections walks up from every page and creates a stub section
// page for each ancestor directory that has no page of its own. Newly created
// stubs are added to byPermalink so a shared ancestor is synthesized once, and
// returned in creation order. It stops at the site root ("/"): a missing home
// page is left alone.
func synthesizeMissingSections(pages []*Page, byPermalink map[string]*Page) []*Page {
	var synth []*Page
	for _, p := range pages {
		cur := parentOf(p.RelPermalink)
		for cur != "" && cur != "/" {
			if _, ok := byPermalink[cur]; ok {
				break
			}
			s := newSyntheticSection(cur)
			byPermalink[cur] = s
			synth = append(synth, s)
			cur = parentOf(cur)
		}
	}
	return synth
}

// newSyntheticSection builds an in-memory section page for a permalink that has
// no backing _index.md. FilePath is deliberately left empty: the build pipeline
// renders empty-FilePath pages every time and never writes them to the cache.
func newSyntheticSection(permalink string) *Page {
	segs := splitPermalink(permalink)
	name := ""
	section := ""
	if len(segs) > 0 {
		name = segs[len(segs)-1]
		section = segs[0]
	}
	title := humanizeSegment(name)
	return &Page{
		RelPermalink: permalink,
		Kind:         "section",
		Section:      section,
		Depth:        len(segs),
		Title:        title,
		LinkTitle:    title,
		Params:       map[string]any{},
	}
}

// splitPermalink returns the non-empty path segments of a permalink.
// "/experiments/2026/07/" → ["experiments", "2026", "07"].
func splitPermalink(permalink string) []string {
	trimmed := strings.Trim(permalink, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

// humanizeSegment turns a path segment into a display title: dashes and
// underscores become spaces and the first letter is upper-cased, matching
// Hugo's default section titling. Numeric segments (date folders) pass through.
func humanizeSegment(s string) string {
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// maxDescendantDate returns the newest Date among a page's descendants, used to
// date a synthesized section from its content the way Hugo dates auto sections.
func maxDescendantDate(p *Page) time.Time {
	newest := p.Date
	for _, c := range p.Children {
		if d := maxDescendantDate(c); d.After(newest) {
			newest = d
		}
	}
	return newest
}

// applyCascade applies cascaded front matter values to a page, only for fields
// that the page has not explicitly set.
func applyCascade(p *Page, cascade map[string]any) {
	if v, ok := cascade["draft"]; ok && !p.Draft {
		p.Draft = toBool(v)
	}
	if v, ok := cascade["type"]; ok && p.Type == "" {
		p.Type = toString(v)
	}
	if v, ok := cascade["layout"]; ok && p.Layout == "" {
		p.Layout = toString(v)
	}
	if v, ok := cascade["tags"]; ok && len(p.Tags) == 0 {
		p.Tags = toStringSlice(v)
	}
	if v, ok := cascade["categories"]; ok && len(p.Categories) == 0 {
		p.Categories = toStringSlice(v)
	}
	if v, ok := cascade["noindex"]; ok && !p.NoIndex {
		p.NoIndex = toBool(v)
	}
	// Any unrecognised cascade keys go to Params if not already set
	if p.Params == nil {
		p.Params = make(map[string]any)
	}
	for k, v := range cascade {
		if _, exists := p.Params[k]; !exists {
			p.Params[k] = v
		}
	}
}

// parentOf returns the permalink of the parent section.
// /foo/bar/baz/ → /foo/bar/
// /foo/bar/    → /foo/
// /foo/        → /
// /            → "" (no parent)
func parentOf(permalink string) string {
	if permalink == "/" {
		return ""
	}
	// Remove trailing slash
	trimmed := strings.TrimRight(permalink, "/")
	// Find last /
	idx := strings.LastIndex(trimmed, "/")
	if idx < 0 {
		return "/"
	}
	parent := trimmed[:idx+1]
	if parent == "" {
		return "/"
	}
	return parent
}

func buildAncestors(p *Page) []*Page {
	var ancestors []*Page
	cur := p.Parent
	for cur != nil {
		ancestors = append([]*Page{cur}, ancestors...)
		cur = cur.Parent
	}
	return ancestors
}

// FilterBySection returns pages belonging to a specific section (not including section index itself).
func FilterBySection(pages []*Page, section string) []*Page {
	var result []*Page
	for _, p := range pages {
		if p.Section == section && p.Kind == "page" {
			result = append(result, p)
		}
	}
	return result
}

// FilterByTag returns all pages that have the given tag.
func FilterByTag(pages []*Page, tag string) []*Page {
	var result []*Page
	for _, p := range pages {
		for _, t := range p.Tags {
			if t == tag {
				result = append(result, p)
				break
			}
		}
	}
	return result
}

// AllTags returns a deduplicated list of all tags across all pages.
func AllTags(pages []*Page) []string {
	seen := make(map[string]bool)
	var tags []string
	for _, p := range pages {
		for _, t := range p.Tags {
			if !seen[t] {
				seen[t] = true
				tags = append(tags, t)
			}
		}
	}
	sort.Strings(tags)
	return tags
}

// TagCounts returns map[tag]count.
func TagCounts(pages []*Page) map[string]int {
	counts := make(map[string]int)
	for _, p := range pages {
		for _, t := range p.Tags {
			counts[t]++
		}
	}
	return counts
}

// FindHome returns the home page (Kind=="home") or nil.
func FindHome(pages []*Page) *Page {
	for _, p := range pages {
		if p.Kind == "home" {
			return p
		}
	}
	return nil
}

// FindByPermalink returns a page by its permalink.
func FindByPermalink(pages []*Page, permalink string) *Page {
	for _, p := range pages {
		if p.RelPermalink == permalink {
			return p
		}
	}
	return nil
}

// SectionPages returns section index pages (Kind=="section").
func SectionPages(pages []*Page) []*Page {
	var result []*Page
	for _, p := range pages {
		if p.Kind == "section" {
			result = append(result, p)
		}
	}
	return result
}

// RegularPages returns pages with Kind=="page".
func RegularPages(pages []*Page) []*Page {
	var result []*Page
	for _, p := range pages {
		if p.Kind == "page" {
			result = append(result, p)
		}
	}
	return result
}
