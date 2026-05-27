package index

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/tamnd/tago/pkg/content"
)

// SearchEntry matches Hugo's search JSON output format.
// { "/path/": { "title": "Title", "data": { "": "plain text..." } } }
type SearchEntry struct {
	Title string            `json:"title"`
	Data  map[string]string `json:"data"`
}

// BuildSearchJSON builds the search index JSON file at outputDir/en.search-data.json.
func BuildSearchJSON(pages []*content.Page, outputDir string) error {
	index := make(map[string]*SearchEntry)

	for _, p := range pages {
		if p.Draft || p.ExcludeSearch || p.NoIndex {
			continue
		}
		if p.Kind == "home" {
			continue
		}

		// Use pre-computed ContentPlain if available, else strip from HTML
		plainText := p.ContentPlain
		if plainText == "" && p.ContentHTML != "" {
			plainText = normalizeWhitespace(stripHTML(p.ContentHTML))
		}

		entry := &SearchEntry{
			Title: p.Title,
			Data: map[string]string{
				"": plainText,
			},
		}
		index[p.RelPermalink] = entry
	}

	data, err := json.Marshal(index)
	if err != nil {
		return err
	}

	outPath := filepath.Join(outputDir, "en.search-data.json")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}
	return os.WriteFile(outPath, data, 0644)
}

func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
		} else if r == '>' {
			inTag = false
			b.WriteRune(' ')
		} else if !inTag {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalizeWhitespace(s string) string {
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}
