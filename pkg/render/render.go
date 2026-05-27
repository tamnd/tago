package render

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tamnd/tago/pkg/content"
	"github.com/tamnd/tago/pkg/index"
)

// SiteData holds site-wide metadata available to all templates.
type SiteData struct {
	Title       string
	BaseURL     string
	Description string
	EditURLBase string
}

// AssetRefs holds fingerprinted asset URLs.
type AssetRefs struct {
	CSS        string
	JSHead     string
	JS         string
	FlexSearch string
	KaTeX      string
	Extra      map[string]string // original basename → fingerprinted URL
}

// TagCount holds a tag and its page count.
type TagCount struct {
	Name  string
	Count int
}

// SidebarItem is a pre-built sidebar navigation node.
type SidebarItem struct {
	Title     string
	URL       string
	Active    bool
	HasActive bool
	IsParent  bool
	Children  []*SidebarItem
}

// TemplateData is the data passed to all templates.
type TemplateData struct {
	Page             *content.Page
	Site             *SiteData
	Assets           AssetRefs
	Pages            []*content.Page // for list pages: children or tag members
	Tags             []TagCount      // for taxonomy page
	GraphDataJSON    string          // JSON string, embedded inline for type=graph
	TreeDataJSON     string          // JSON string, embedded inline for type=tree
	CalendarDataJSON string          // JSON string, embedded inline for type=calendar
	SidebarItems     []*SidebarItem  // pre-built sidebar nav tree
	SidebarRoot      *content.Page   // page whose subtree is shown in sidebar
	SidebarBack      *content.Page   // back-link target (nil = no back link)
	PrevPage         *content.Page   // previous sibling (for pager)
	NextPage         *content.Page   // next sibling (for pager)
}

// Renderer handles template rendering.
type Renderer struct {
	site        *SiteData
	assets      AssetRefs
	layoutsDir  string
	liveReload  bool
	tmplCache   map[string]*template.Template
}

// New creates a new Renderer.
func New(site *SiteData, assets AssetRefs, layoutsDir string, liveReload bool) *Renderer {
	return &Renderer{
		site:       site,
		assets:     assets,
		layoutsDir: layoutsDir,
		liveReload: liveReload,
		tmplCache:  make(map[string]*template.Template),
	}
}

// determineSidebarRoot returns the page whose children tree should fill the sidebar.
func determineSidebarRoot(page *content.Page) *content.Page {
	if page.Depth <= 2 {
		return page
	}
	if len(page.Ancestors) >= 3 {
		return page.Ancestors[2]
	}
	return page
}

// determineSidebarBack returns the page the "back" link should point to.
func determineSidebarBack(page *content.Page) *content.Page {
	switch page.Depth {
	case 0:
		return nil
	case 1:
		return page.Parent
	default:
		if len(page.Ancestors) >= 2 {
			return page.Ancestors[1]
		}
		return page.Parent
	}
}

// buildSidebarItems recursively converts a page's children into SidebarItems.
// Only the active path (the branch containing currentURL) is expanded;
// sibling branches are collapsed to their section link only.
func buildSidebarItems(root *content.Page, currentURL string) []*SidebarItem {
	var items []*SidebarItem
	for _, child := range root.Children {
		if child.Draft {
			continue
		}
		title := child.LinkTitle
		if title == "" {
			title = child.Title
		}
		item := &SidebarItem{
			Title:    title,
			URL:      child.RelPermalink,
			Active:   child.RelPermalink == currentURL,
			IsParent: len(child.Children) > 0,
		}
		if item.IsParent {
			item.Children = buildSidebarItems(child, currentURL)
			for _, c := range item.Children {
				if c.Active || c.HasActive {
					item.HasActive = true
					break
				}
			}
			// Collapse sections not on the active path.
			if !item.HasActive {
				item.Children = nil
			}
		}
		items = append(items, item)
	}
	return items
}

var funcMap = template.FuncMap{
	"rawHTML": func(s string) template.HTML {
		return template.HTML(s)
	},
	"safeJS": func(s string) template.JS {
		return template.JS(s)
	},
	"formatDate": func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.Format("Jan 2, 2006")
	},
	"urlize": func(s string) string {
		s = strings.ToLower(s)
		s = strings.ReplaceAll(s, " ", "-")
		var b strings.Builder
		for _, r := range s {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
				b.WriteRune(r)
			}
		}
		return b.String()
	},
	"trimSlash": func(s string) string {
		return strings.TrimRight(s, "/")
	},
	"tagFontSize": func(count int) float64 {
		base := 0.8
		scale := math.Log1p(float64(count)) * 0.3
		size := base + scale
		if size > 2.0 {
			size = 2.0
		}
		return math.Round(size*10) / 10
	},
	"safeURL": func(s string) template.URL {
		return template.URL(s)
	},
	"add": func(a, b int) int { return a + b },
	"sub": func(a, b int) int { return a - b },
	"mul": func(a, b int) int { return a * b },
	"dict": func(pairs ...any) map[string]any {
		m := make(map[string]any)
		for i := 0; i+1 < len(pairs); i += 2 {
			if key, ok := pairs[i].(string); ok {
				m[key] = pairs[i+1]
			}
		}
		return m
	},
	"hasPrefix": strings.HasPrefix,
	"hasSuffix": strings.HasSuffix,
}

// getTemplate returns a compiled template for the given kind.
// It first checks layoutsDir, then falls back to embedded defaults.
func (r *Renderer) getTemplate(kind string) (*template.Template, error) {
	if t, ok := r.tmplCache[kind]; ok {
		return t, nil
	}

	// page-live-reload is always defined: JS block when enabled, no-op otherwise.
	liveReloadBlock := `{{define "page-live-reload"}}{{end}}`
	if r.liveReload {
		liveReloadBlock = defaultTemplates["live-reload-js"]
	}

	// Build the template from baseof + specific kind
	baseof := defaultTemplates["baseof"]
	_ = defaultTemplates["sidebar"] // no longer used directly
	breadcrumb := defaultTemplates["breadcrumb"]
	kindTmpl, ok := defaultTemplates[kind]
	if !ok {
		return nil, fmt.Errorf("no template for kind %q", kind)
	}

	// Try loading from layoutsDir
	if r.layoutsDir != "" {
		if data, err := os.ReadFile(filepath.Join(r.layoutsDir, kind+".html")); err == nil {
			kindTmpl = string(data)
		}
		if data, err := os.ReadFile(filepath.Join(r.layoutsDir, "baseof.html")); err == nil {
			baseof = string(data)
		}
	}

	// Compose: baseof calls {{template "page-main" .}}, {{template "page-sidebar" .}},
	// and {{template "page-live-reload" .}} which are defined in kindTmpl.
	combined := baseof + "\n" +
		breadcrumb + "\n" +
		kindTmpl + "\n" +
		liveReloadBlock

	t, err := template.New("baseof").Funcs(funcMap).Parse(combined)
	if err != nil {
		return nil, fmt.Errorf("parse template %q: %w", kind, err)
	}
	r.tmplCache[kind] = t
	return t, nil
}

// RenderPage renders a single page to its OutputPath.
func (r *Renderer) RenderPage(page *content.Page, allPages []*content.Page) error {
	kind := page.Kind
	if kind == "" {
		kind = "page"
	}

	// Type overrides kind for special visualization pages
	templateName := kind
	switch page.Type {
	case "graph":
		templateName = "graph"
	case "tree":
		templateName = "tree"
	case "calendar":
		templateName = "calendar"
	}

	data := &TemplateData{Page: page, Site: r.site, Assets: r.assets}

	// Build sidebar navigation data
	sidebarRoot := determineSidebarRoot(page)
	data.SidebarRoot = sidebarRoot
	data.SidebarBack = determineSidebarBack(page)
	if sidebarRoot != nil {
		data.SidebarItems = buildSidebarItems(sidebarRoot, page.RelPermalink)
	}

	// Compute prev/next siblings for pager
	if page.Parent != nil && page.Kind == "page" {
		siblings := page.Parent.Children
		for i, sib := range siblings {
			if sib.RelPermalink == page.RelPermalink {
				if i > 0 {
					data.PrevPage = siblings[i-1]
				}
				if i < len(siblings)-1 {
					data.NextPage = siblings[i+1]
				}
				break
			}
		}
	}

	// Build visualization data when needed
	switch page.Type {
	case "graph":
		gd := index.BuildGraphData(allPages)
		b, _ := json.Marshal(gd)
		data.GraphDataJSON = string(b)
	case "tree":
		td := index.BuildTreeData(allPages)
		b, _ := json.Marshal(td)
		data.TreeDataJSON = string(b)
	case "calendar":
		cd := index.BuildCalendarData(allPages)
		b, _ := json.Marshal(cd)
		data.CalendarDataJSON = string(b)
	default:
		// Normal pages: build list for section/home
		switch kind {
		case "section":
			for _, c := range page.Children {
				if !c.Draft {
					data.Pages = append(data.Pages, c)
				}
			}
		case "home":
			for _, p := range allPages {
				if p.Kind == "section" && p.Depth == 1 {
					data.Pages = append(data.Pages, p)
				}
			}
		}
	}

	return r.renderToFile(templateName, page.OutputPath, data)
}

// Render404 renders a 404.html page to outputDir/404.html.
func (r *Renderer) Render404(outputDir string, allPages []*content.Page) error {
	outputPath := filepath.Join(outputDir, "404.html")
	fakePage := &content.Page{
		Title:        "404 - Page Not Found",
		Kind:         "404",
		RelPermalink: "/404.html",
		OutputPath:   outputPath,
	}
	data := &TemplateData{
		Page:   fakePage,
		Site:   r.site,
		Assets: r.assets,
	}
	return r.renderToFile("404", outputPath, data)
}

// RenderTagPage renders a tag term page.
func (r *Renderer) RenderTagPage(tag string, pages []*content.Page, outputDir string) error {
	urlized := urlize(tag)
	permalink := "/tags/" + urlized + "/"
	outputPath := filepath.Join(outputDir, "tags", urlized, "index.html")

	fakePage := &content.Page{
		Title:        tag,
		LinkTitle:    tag,
		RelPermalink: permalink,
		Kind:         "term",
	}

	data := &TemplateData{
		Page:   fakePage,
		Site:   r.site,
		Assets: r.assets,
		Pages:  pages,
	}

	return r.renderToFile("term", outputPath, data)
}

// RenderTaxonomyPage renders the /tags/ overview page.
func (r *Renderer) RenderTaxonomyPage(tags []TagCount, outputDir string) error {
	fakePage := &content.Page{
		Title:        "Tags",
		LinkTitle:    "Tags",
		RelPermalink: "/tags/",
		Kind:         "taxonomy",
	}

	data := &TemplateData{
		Page:   fakePage,
		Site:   r.site,
		Assets: r.assets,
		Tags:   tags,
	}

	outputPath := filepath.Join(outputDir, "tags", "index.html")
	return r.renderToFile("taxonomy", outputPath, data)
}

// RenderSearchPage renders the /search/ page.
func (r *Renderer) RenderSearchPage(outputDir string) error {
	fakePage := &content.Page{
		Title:        "Search",
		LinkTitle:    "Search",
		RelPermalink: "/search/",
		Kind:         "special",
	}

	data := &TemplateData{
		Page:   fakePage,
		Site:   r.site,
		Assets: r.assets,
	}

	outputPath := filepath.Join(outputDir, "search", "index.html")
	return r.renderToFile("search", outputPath, data)
}

func (r *Renderer) renderToFile(kind, outputPath string, data *TemplateData) error {
	t, err := r.getTemplate(kind)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(outputPath), err)
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", outputPath, err)
	}
	defer f.Close()

	if err := t.Execute(f, data); err != nil {
		return fmt.Errorf("execute template %q for %s: %w", kind, outputPath, err)
	}
	return nil
}

// InvalidateCache clears the template cache (useful after layout file changes).
func (r *Renderer) InvalidateCache() {
	r.tmplCache = make(map[string]*template.Template)
}

func urlize(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
