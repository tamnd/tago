package index

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tamnd/tago/pkg/content"
)

type urlset struct {
	XMLName xml.Name  `xml:"urlset"`
	XMLNS   string    `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

// BuildSitemap generates sitemap.xml in outputDir.
func BuildSitemap(pages []*content.Page, baseURL, outputDir string) error {
	baseURL = strings.TrimRight(baseURL, "/")

	var urls []sitemapURL
	for _, p := range pages {
		if p.Draft || p.NoIndex {
			continue
		}
		loc := baseURL + p.RelPermalink
		lastmod := ""
		if !p.Date.IsZero() {
			lastmod = p.Date.UTC().Format(time.RFC3339)
		} else {
			lastmod = time.Now().UTC().Format("2006-01-02")
		}
		urls = append(urls, sitemapURL{
			Loc:     loc,
			LastMod: lastmod,
		})
	}

	us := urlset{
		XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  urls,
	}

	out, err := xml.MarshalIndent(us, "", "  ")
	if err != nil {
		return err
	}

	content := []byte(xml.Header + string(out) + "\n")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outputDir, "sitemap.xml"), content, 0644)
}
