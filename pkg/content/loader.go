package content

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
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

	BuildTree(pages)
	return pages, nil
}

// BuildTree sets Parent, Children, and Ancestors for all pages.
// It builds the hierarchy based on permalinks.
func BuildTree(pages []*Page) {
	// Build a map from permalink → page
	byPermalink := make(map[string]*Page, len(pages))
	for _, p := range pages {
		byPermalink[p.RelPermalink] = p
	}

	for _, p := range pages {
		parentPermalink := parentOf(p.RelPermalink)
		if parentPermalink != "" {
			if parent, ok := byPermalink[parentPermalink]; ok {
				p.Parent = parent
			}
		}
	}

	// Set Children
	for _, p := range pages {
		if p.Parent != nil {
			p.Parent.Children = append(p.Parent.Children, p)
		}
	}

	// Sort children by weight then date then title
	for _, p := range pages {
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

	// Set Ancestors
	for _, p := range pages {
		p.Ancestors = buildAncestors(p)
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
