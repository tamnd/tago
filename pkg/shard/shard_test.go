package shard

import (
	"testing"
)

func TestParseConfig(t *testing.T) {
	data := []byte(`
[main]
project = "brain"
domain = "brain.tamnd.com"
output = "public-main"
file_budget = 18000

[[shards]]
name = "leetcode"
project = "brain-leetcode"
domain = "leetcode.tamnd.com"
source_prefix = "/practice/leetcode/"
output = "public-shards/leetcode"
file_budget = 18000

[[shards]]
name = "kvant"
project = "brain-kvant"
domain = "kvant.tamnd.com"
source_prefix = "/practice/kvant/"
output = "public-shards/kvant"
file_budget = 22000
`)
	cfg, err := ParseConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Main.Domain != "brain.tamnd.com" {
		t.Errorf("main domain: got %q", cfg.Main.Domain)
	}
	if len(cfg.Shards) != 2 {
		t.Errorf("shard count: got %d", len(cfg.Shards))
	}
	if cfg.Shards[1].SourcePrefix != "/practice/kvant/" {
		t.Errorf("kvant prefix: got %q", cfg.Shards[1].SourcePrefix)
	}
	if cfg.Shards[1].FileBudget != 22000 {
		t.Errorf("kvant budget: got %d", cfg.Shards[1].FileBudget)
	}
}

func makeTestCfg() (*Site, *Site, []*Site) {
	main := &Site{Name: "main", Domain: "brain.tamnd.com"}
	leetcode := &Site{Name: "leetcode", Domain: "leetcode.tamnd.com", SourcePrefix: "/practice/leetcode/"}
	kvant := &Site{Name: "kvant", Domain: "kvant.tamnd.com", SourcePrefix: "/practice/kvant/"}
	return main, kvant, []*Site{leetcode, kvant}
}

func TestMapURL(t *testing.T) {
	main, kvant, shards := makeTestCfg()

	tests := []struct {
		url     string
		current *Site
		want    string
	}{
		// From main: links to shards become absolute shard URLs
		{"/practice/kvant/math/1/", main, "https://kvant.tamnd.com/math/1/"},
		{"/practice/leetcode/1/", main, "https://leetcode.tamnd.com/1/"},
		{"/about/", main, "/about/"},
		// Protocol-relative: never rewritten
		{"//cdn.example.com/file.js", main, "//cdn.example.com/file.js"},
		{"https://example.com/", main, "https://example.com/"},
		// From kvant shard: self-links stripped of prefix
		{"/practice/kvant/math/1/", kvant, "/math/1/"},
		{"/practice/kvant/", kvant, "/"},
		// From kvant: links to main become absolute main URLs
		{"/about/", kvant, "https://brain.tamnd.com/about/"},
		// From kvant: links to other shards become absolute
		{"/practice/leetcode/1/", kvant, "https://leetcode.tamnd.com/1/"},
		// Shared assets stay local even in shard context
		{"/css/main.css", kvant, "/css/main.css"},
		{"/favicon.ico", kvant, "/favicon.ico"},
		{"/js/app.js", kvant, "/js/app.js"},
	}

	for _, tc := range tests {
		got := mapURL(tc.url, tc.current, main, shards)
		if got != tc.want {
			t.Errorf("mapURL(%q, %s): got %q, want %q", tc.url, tc.current.Name, got, tc.want)
		}
	}
}

func TestRewriteText(t *testing.T) {
	main, kvant, shards := makeTestCfg()

	tests := []struct {
		name    string
		input   string
		current *Site
		want    string
	}{
		{
			"main: shard link",
			`<a href="/practice/kvant/math/1/">Kvant</a>`,
			main,
			`<a href="https://kvant.tamnd.com/math/1/">Kvant</a>`,
		},
		{
			"main: non-shard link untouched",
			`<a href="/about/">About</a>`,
			main,
			`<a href="/about/">About</a>`,
		},
		{
			"shard: self link stripped",
			`<a href="/practice/kvant/math/1/">Problem</a>`,
			kvant,
			`<a href="/math/1/">Problem</a>`,
		},
		{
			"shard: canonical rewritten",
			`<link rel="canonical" href="https://brain.tamnd.com/practice/kvant/math/1/">`,
			kvant,
			`<link rel="canonical" href="https://kvant.tamnd.com/math/1/">`,
		},
		{
			"shard: css url() rewritten",
			`background: url("/practice/kvant/images/logo.png");`,
			kvant,
			`background: url("/images/logo.png");`,
		},
		{
			"shard: css url() bare",
			`background: url(/practice/kvant/images/logo.png);`,
			kvant,
			`background: url(/images/logo.png);`,
		},
		{
			"shard: shared css left alone",
			`<link href="/css/style.css">`,
			kvant,
			`<link href="/css/style.css">`,
		},
		{
			"shard: src attribute",
			`<img src="/practice/kvant/images/logo.png">`,
			kvant,
			`<img src="/images/logo.png">`,
		},
	}

	for _, tc := range tests {
		got := rewriteText(tc.input, tc.current, main, shards)
		if got != tc.want {
			t.Errorf("%s:\n  input: %s\n  got:   %s\n  want:  %s", tc.name, tc.input, got, tc.want)
		}
	}
}

func TestParseGitLog(t *testing.T) {
	input := `@@1700000001
content/en/practice/kvant/math/1.md
content/en/practice/kvant/math/2.md

@@1700000002
content/en/practice/kvant/math/1.md
content/en/about.md
`
	m := parseGitLog(input)
	if m["content/en/practice/kvant/math/1.md"] != 1700000001 {
		t.Errorf("file1 ts: got %d", m["content/en/practice/kvant/math/1.md"])
	}
	if m["content/en/practice/kvant/math/2.md"] != 1700000001 {
		t.Errorf("file2 ts: got %d", m["content/en/practice/kvant/math/2.md"])
	}
	// Second commit entry for file1 is ignored (first = most recent)
	if len(m) != 3 {
		t.Errorf("expected 3 entries, got %d", len(m))
	}
}
