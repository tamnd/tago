package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/tamnd/tago/pkg/build"
	"github.com/tamnd/tago/pkg/server"
	"github.com/tamnd/tago/pkg/shard"
)

const version = "0.3.0"

func main() {
	log.SetFlags(0)
	log.SetPrefix("tago: ")

	if len(os.Args) < 2 {
		printHelp()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "build":
		runBuild(os.Args[2:])
	case "serve":
		runServe(os.Args[2:])
	case "clean":
		runClean(os.Args[2:])
	case "split":
		runSplit(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println("tago", version)
	case "help", "--help", "-h":
		printHelp()
	default:
		fmt.Fprintf(os.Stderr, "tago: unknown command %q\n", os.Args[1])
		printHelp()
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Print(`tago - incremental static site generator

Usage:
  tago build   [flags]  Build the site
  tago serve   [flags]  Start dev server with live reload
  tago clean   [flags]  Remove output directory
  tago split   [flags]  Split public/ into per-subdomain shard outputs
  tago version          Print version

Build flags:
  --content <dir>    Content directory (default: content)
  --output <dir>     Output directory (default: public)
  --static <dir>     Static assets directory (default: static)
  --layouts <dir>    Custom layouts directory (default: layouts)
  --base-url <url>   Base URL (default: http://localhost:1313/)
  --title <title>    Site title (default: My Site)
  --clean            Delete output before building

Serve flags:
  --port <port>      HTTP port (default: 1313)
  (all build flags also apply)

Split flags:
  --config <path>    deploy-shards.toml path    (default: deploy-shards.toml)
  --public <dir>     Built site directory        (default: public)
  --lastmods <path>  Incremental lastmods cache  (default: content-lastmods.json)
  --summary <path>   JSON summary output path    (default: deploy-shards-summary.json)
`)
}

type flags struct {
	content         string
	output          string
	static          string
	layouts         string
	theme           string // optional theme name → themes/<name>/layouts + themes/<name>/static
	themeStatic     string // derived from theme
	baseURL         string
	title           string
	desc            string
	lang            string // default language code, e.g. "en", "vi", "ja"
	editURLBase     string
	clean           bool
	syntaxHighlight bool // enable Chroma server-side syntax highlighting
	buildDrafts     bool
	buildFuture     bool
	buildExpired    bool
	params          map[string]any    // from [params] in tago.toml
	taxonomies      map[string]string // from [taxonomies] in tago.toml
	port            int
}

func parseFlags(args []string) *flags {
	f := &flags{
		content: "content",
		output:  "public",
		static:  "static",
		layouts: "layouts",
		baseURL: "http://localhost:1313/",
		title:   "My Site",
		port:    1313,
	}

	i := 0
	for i < len(args) {
		switch args[i] {
		case "--content":
			i++
			if i < len(args) {
				f.content = args[i]
			}
		case "--output":
			i++
			if i < len(args) {
				f.output = args[i]
			}
		case "--static":
			i++
			if i < len(args) {
				f.static = args[i]
			}
		case "--layouts":
			i++
			if i < len(args) {
				f.layouts = args[i]
			}
		case "--base-url":
			i++
			if i < len(args) {
				f.baseURL = args[i]
			}
		case "--title":
			i++
			if i < len(args) {
				f.title = args[i]
			}
		case "--description":
			i++
			if i < len(args) {
				f.desc = args[i]
			}
		case "--theme":
			i++
			if i < len(args) {
				f.theme = args[i]
			}
		case "--clean":
			f.clean = true
		case "--buildDrafts", "--build-drafts", "-D":
			f.buildDrafts = true
		case "--buildFuture", "--build-future", "-F":
			f.buildFuture = true
		case "--buildExpired", "--build-expired", "-E":
			f.buildExpired = true
		case "--syntax-highlight":
			f.syntaxHighlight = true
		case "--port":
			i++
			if i < len(args) {
				fmt.Sscanf(args[i], "%d", &f.port)
			}
		}
		i++
	}

	// Try to load tago.toml from cwd
	loadTOML(f)

	// Derive theme paths after TOML load
	if f.theme != "" && f.layouts == "layouts" {
		f.layouts = filepath.Join("themes", f.theme, "layouts")
	}
	if f.theme != "" && f.themeStatic == "" {
		f.themeStatic = filepath.Join("themes", f.theme, "static")
	}

	// Make paths absolute
	if !filepath.IsAbs(f.content) {
		if abs, err := filepath.Abs(f.content); err == nil {
			f.content = abs
		}
	}
	if !filepath.IsAbs(f.output) {
		if abs, err := filepath.Abs(f.output); err == nil {
			f.output = abs
		}
	}
	if !filepath.IsAbs(f.static) {
		if abs, err := filepath.Abs(f.static); err == nil {
			f.static = abs
		}
	}
	if !filepath.IsAbs(f.layouts) {
		if abs, err := filepath.Abs(f.layouts); err == nil {
			f.layouts = abs
		}
	}
	if f.themeStatic != "" && !filepath.IsAbs(f.themeStatic) {
		if abs, err := filepath.Abs(f.themeStatic); err == nil {
			f.themeStatic = abs
		}
	}

	return f
}

// loadTOML reads settings from tago.toml (preferred), hugo.toml, or config.toml.
// Supports [params] and [taxonomies] sections plus Hugo key aliases.
func loadTOML(f *flags) {
	var data []byte
	var err error
	for _, name := range []string{"tago.toml", "hugo.toml", "config.toml"} {
		data, err = os.ReadFile(name)
		if err == nil {
			break
		}
	}
	if err != nil {
		return
	}

	section := "" // current [section] header
	lines := splitLines(string(data))
	for _, line := range lines {
		// Detect section headers like [params] or [taxonomies]
		trimmed := trim(line)
		if len(trimmed) > 2 && trimmed[0] == '[' && trimmed[len(trimmed)-1] == ']' {
			section = trim(trimmed[1 : len(trimmed)-1])
			continue
		}

		key, val, ok := parseTOMLLine(line)
		if !ok {
			continue
		}

		switch section {
		case "params":
			if f.params == nil {
				f.params = make(map[string]any)
			}
			f.params[key] = val
		case "taxonomies":
			if f.taxonomies == nil {
				f.taxonomies = make(map[string]string)
			}
			f.taxonomies[key] = val
		default:
			switch key {
			case "title":
				if f.title == "My Site" {
					f.title = val
				}
			case "baseURL", "base_url":
				if f.baseURL == "http://localhost:1313/" {
					f.baseURL = val
				}
			case "description":
				if f.desc == "" {
					f.desc = val
				}
			case "contentDir":
				if f.content == "content" {
					f.content = val
				}
			case "outputDir", "publishDir":
				if f.output == "public" {
					f.output = val
				}
			case "staticDir":
				if f.static == "static" {
					f.static = val
				}
			case "theme":
				if f.theme == "" {
					f.theme = val
				}
			case "lang", "defaultLang", "defaultContentLanguage", "default_lang":
				if f.lang == "" {
					f.lang = val
				}
			case "syntaxHighlight", "syntax_highlight":
				f.syntaxHighlight = val == "true" || val == "1" || val == "yes"
			case "buildDrafts", "build_drafts":
				f.buildDrafts = val == "true" || val == "1" || val == "yes"
			case "buildFuture", "build_future":
				f.buildFuture = val == "true" || val == "1" || val == "yes"
			case "buildExpired", "build_expired":
				f.buildExpired = val == "true" || val == "1" || val == "yes"
			case "editURL", "edit_url", "editURLBase":
				if f.editURLBase == "" {
					f.editURLBase = val
				}
			}
		}
	}
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func parseTOMLLine(line string) (key, val string, ok bool) {
	// Strip comments
	if idx := indexOf(line, '#'); idx >= 0 {
		line = line[:idx]
	}
	// Find =
	idx := indexOf(line, '=')
	if idx < 0 {
		return "", "", false
	}
	key = trim(line[:idx])
	val = trim(line[idx+1:])
	// Strip quotes
	if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
		val = val[1 : len(val)-1]
	}
	if key == "" || val == "" {
		return "", "", false
	}
	return key, val, true
}

func indexOf(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func trim(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

func runBuild(args []string) {
	f := parseFlags(args)

	defaultLang := "en"
	if f.lang != "" {
		defaultLang = f.lang
	}

	cfg := &build.Config{
		ContentDir:      f.content,
		OutputDir:       f.output,
		StaticDir:       f.static,
		ThemeStaticDir:  f.themeStatic,
		LayoutsDir:      f.layouts,
		BaseURL:         f.baseURL,
		DefaultLang:     defaultLang,
		SiteTitle:       f.title,
		SiteDesc:        f.desc,
		EditURLBase:     f.editURLBase,
		Clean:           f.clean,
		LiveReload:      false,
		SyntaxHighlight: f.syntaxHighlight,
		BuildDrafts:     f.buildDrafts,
		BuildFuture:     f.buildFuture,
		BuildExpired:    f.buildExpired,
		Params:          f.params,
		Taxonomies:      f.taxonomies,
	}

	stats, err := build.Build(cfg)
	if err != nil {
		log.Fatalf("build failed: %v", err)
	}

	fmt.Printf("tago: done — %d pages rebuilt (of %d total) in %s\n",
		stats.PagesRebuilt, stats.PagesTotal, stats.Duration.Round(1000000))
}

func runServe(args []string) {
	f := parseFlags(args)

	serveLang := "en"
	if f.lang != "" {
		serveLang = f.lang
	}

	cfg := &build.Config{
		ContentDir:      f.content,
		OutputDir:       f.output,
		StaticDir:       f.static,
		ThemeStaticDir:  f.themeStatic,
		LayoutsDir:      f.layouts,
		BaseURL:         fmt.Sprintf("http://localhost:%d/", f.port),
		DefaultLang:     serveLang,
		SiteTitle:       f.title,
		SiteDesc:        f.desc,
		EditURLBase:     f.editURLBase,
		Clean:           f.clean,
		LiveReload:      true,
		SyntaxHighlight: f.syntaxHighlight,
		BuildDrafts:     f.buildDrafts,
		BuildFuture:     f.buildFuture,
		BuildExpired:    f.buildExpired,
		Params:          f.params,
		Taxonomies:      f.taxonomies,
	}

	// Initial build
	stats, err := build.Build(cfg)
	if err != nil {
		log.Fatalf("initial build failed: %v", err)
	}
	fmt.Printf("tago: initial build — %d pages built in %s\n",
		stats.PagesTotal, stats.Duration.Round(1000000))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	srv := server.New(f.output, f.content, f.port, func() error {
		cfg2 := *cfg
		cfg2.LiveReload = true
		_, err := build.Build(&cfg2)
		return err
	})

	if err := srv.Start(ctx); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func runClean(args []string) {
	f := parseFlags(args)
	if err := os.RemoveAll(f.output); err != nil {
		log.Fatalf("clean failed: %v", err)
	}
	fmt.Printf("tago: removed %s\n", f.output)
}

func runSplit(args []string) {
	opts := shard.Options{
		ConfigFile:   "deploy-shards.toml",
		PublicDir:    "public",
		LastmodsFile: "content-lastmods.json",
		SummaryFile:  "deploy-shards-summary.json",
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config":
			i++
			if i < len(args) {
				opts.ConfigFile = args[i]
			}
		case "--public":
			i++
			if i < len(args) {
				opts.PublicDir = args[i]
			}
		case "--lastmods":
			i++
			if i < len(args) {
				opts.LastmodsFile = args[i]
			}
		case "--summary":
			i++
			if i < len(args) {
				opts.SummaryFile = args[i]
			}
		}
	}

	summaries, err := shard.Split(opts)
	if err != nil {
		log.Fatalf("split failed: %v", err)
	}
	for _, s := range summaries {
		fmt.Printf("tago split: %s — %d files, %d bytes → %s\n",
			s.Site, s.Files, s.Bytes, s.Domain)
	}
}
