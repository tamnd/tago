package index

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tamnd/tago/pkg/content"
)

// GraphNode represents a node in the knowledge graph.
type GraphNode struct {
	ID       string   `json:"id"`
	URL      string   `json:"url"`
	Title    string   `json:"title"`
	Section  string   `json:"section"`
	Kind     string   `json:"kind"`
	Depth    int      `json:"depth"`
	Tags     []string `json:"tags"`
	ReadTime int      `json:"readTime"`
}

// GraphLink represents an edge in the knowledge graph.
type GraphLink struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Kind   string `json:"kind"` // "tree" | "tag"
}

// GraphData is the full graph data structure (bipartite: page nodes + tag nodes).
type GraphData struct {
	Nodes []GraphNode `json:"nodes"`
	Links []GraphLink `json:"links"`
}

// urlize converts a tag string to a URL-safe slug.
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

// BuildGraphData builds the graph data structure (does NOT write file).
// Uses a bipartite model: each tag is its own node, links go page → tag node.
func BuildGraphData(pages []*content.Page) *GraphData {
	var nodes []GraphNode
	var links []GraphLink
	tagsSeen := map[string]bool{}

	for _, p := range pages {
		if p.Draft {
			continue
		}
		tags := p.Tags
		if tags == nil {
			tags = []string{}
		}
		nodes = append(nodes, GraphNode{
			ID:       p.RelPermalink,
			URL:      p.RelPermalink,
			Title:    p.Title,
			Section:  p.Section,
			Kind:     p.Kind,
			Depth:    p.Depth,
			Tags:     tags,
			ReadTime: p.ReadingTime,
		})
		// tree link to parent
		if p.Parent != nil {
			links = append(links, GraphLink{
				Source: p.Parent.RelPermalink,
				Target: p.RelPermalink,
				Kind:   "tree",
			})
		}
		// tag links (bipartite: page → tag node)
		for _, t := range p.Tags {
			tagID := "tag:" + t
			links = append(links, GraphLink{
				Source: p.RelPermalink,
				Target: tagID,
				Kind:   "tag",
			})
			if !tagsSeen[t] {
				tagsSeen[t] = true
				tagURL := "/tags/" + urlize(t) + "/"
				nodes = append(nodes, GraphNode{
					ID:       tagID,
					URL:      tagURL,
					Title:    "#" + t,
					Section:  "tag",
					Kind:     "tag",
					Depth:    0,
					Tags:     []string{},
					ReadTime: 0,
				})
			}
		}
	}
	if nodes == nil {
		nodes = []GraphNode{}
	}
	if links == nil {
		links = []GraphLink{}
	}
	return &GraphData{Nodes: nodes, Links: links}
}

// TreeItem is a flat node in the tree data structure.
type TreeItem struct {
	ID       string `json:"id"`
	Parent   string `json:"parent"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Section  string `json:"section"`
	Depth    int    `json:"depth"`
	ReadTime int    `json:"readTime"`
	URL      string `json:"url"`
	SubCount int    `json:"subCount"`
}

// TreeData is the flat tree data structure with parent pointers.
type TreeData struct {
	Items    []TreeItem `json:"items"`
	RootID   string     `json:"rootId"`
	RootName string     `json:"rootName"`
}

// BuildTreeData builds the flat tree data structure (does NOT write file).
func BuildTreeData(pages []*content.Page) *TreeData {
	var items []TreeItem
	for _, p := range pages {
		if p.Draft {
			continue
		}
		parentID := ""
		if p.Parent != nil {
			parentID = p.Parent.RelPermalink
		}
		subCount := 0
		for _, c := range p.Children {
			if !c.Draft {
				subCount++
			}
		}
		items = append(items, TreeItem{
			ID:       p.RelPermalink,
			Parent:   parentID,
			Name:     p.Title,
			Kind:     p.Kind,
			Section:  p.Section,
			Depth:    p.Depth,
			ReadTime: p.ReadingTime,
			URL:      p.RelPermalink,
			SubCount: subCount,
		})
	}
	if items == nil {
		items = []TreeItem{}
	}
	return &TreeData{Items: items, RootID: "/", RootName: "Brain"}
}

// CalendarEntry is a single entry in the calendar data.
type CalendarEntry struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Path  string `json:"path"`
	Date  string `json:"date"`
	Hour  string `json:"hour"`
	Time  string `json:"time"`
	Words int    `json:"words"`
	RT    string `json:"rt"`
}

// BuildCalendarData builds the calendar data (does NOT write file).
// Returns a map from date string (YYYY-MM-DD) to list of entries.
func BuildCalendarData(pages []*content.Page) map[string][]CalendarEntry {
	result := make(map[string][]CalendarEntry)
	for _, p := range pages {
		if p.Draft || p.Date.IsZero() {
			continue
		}
		day := p.Date.Format("2006-01-02")
		// Build breadcrumb path
		var pathParts []string
		for _, anc := range p.Ancestors {
			if anc.Kind != "home" {
				pathParts = append(pathParts, anc.Title)
			}
		}
		path := strings.Join(pathParts, " / ")
		rtStr := fmt.Sprintf("%d min", p.ReadingTime)
		if p.ReadingTime >= 60 {
			rtStr = fmt.Sprintf("%dh %dm", p.ReadingTime/60, p.ReadingTime%60)
		}
		result[day] = append(result[day], CalendarEntry{
			URL:   p.RelPermalink,
			Title: p.Title,
			Path:  path,
			Date:  day,
			Hour:  p.Date.Format("15"),
			Time:  p.Date.Format("15:04"),
			Words: p.WordCount,
			RT:    rtStr,
		})
	}
	return result
}

// BuildGraphJSON builds the graph JSON at outputDir/graph.json (backward compat wrapper).
func BuildGraphJSON(pages []*content.Page, outputDir string) error {
	gd := BuildGraphData(pages)
	data, err := json.MarshalIndent(gd, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outputDir, "graph.json"), data, 0644)
}

// BuildTreeJSON builds the tree JSON at outputDir/tree.json (backward compat wrapper).
func BuildTreeJSON(pages []*content.Page, outputDir string) error {
	td := BuildTreeData(pages)
	data, err := json.MarshalIndent(td, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outputDir, "tree.json"), data, 0644)
}

// BuildCalendarJSON builds the calendar JSON at outputDir/calendar.json (backward compat wrapper).
func BuildCalendarJSON(pages []*content.Page, outputDir string) error {
	cd := BuildCalendarData(pages)
	data, err := json.MarshalIndent(cd, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outputDir, "calendar.json"), data, 0644)
}
