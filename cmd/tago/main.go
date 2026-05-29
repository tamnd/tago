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
)

const version = "0.1.0"

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
`)
}

type flags struct {
	content     string
	output      string
	static      string
	layouts     string
	theme       string // optional theme name → themes/<name>/layouts + themes/<name>/static
	themeStatic string // derived from theme
	baseURL     string
	title       string
	desc        string
	lang        string // default language code, e.g. "en", "vi", "ja"
	clean       bool
	port        int
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

// loadTOML reads basic settings from tago.toml if present.
func loadTOML(f *flags) {
	data, err := os.ReadFile("tago.toml")
	if err != nil {
		return
	}

	lines := splitLines(string(data))
	for _, line := range lines {
		key, val, ok := parseTOMLLine(line)
		if !ok {
			continue
		}
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
		case "outputDir":
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
		case "lang", "defaultLang", "default_lang":
			if f.lang == "" {
				f.lang = val
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
		ContentDir:     f.content,
		OutputDir:      f.output,
		StaticDir:      f.static,
		ThemeStaticDir: f.themeStatic,
		LayoutsDir:     f.layouts,
		BaseURL:        f.baseURL,
		DefaultLang:    defaultLang,
		SiteTitle:      f.title,
		SiteDesc:       f.desc,
		Clean:          f.clean,
		LiveReload:     false,
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
		ContentDir:     f.content,
		OutputDir:      f.output,
		StaticDir:      f.static,
		ThemeStaticDir: f.themeStatic,
		LayoutsDir:     f.layouts,
		BaseURL:        fmt.Sprintf("http://localhost:%d/", f.port),
		DefaultLang:    serveLang,
		SiteTitle:      f.title,
		SiteDesc:       f.desc,
		Clean:          f.clean,
		LiveReload:     true,
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
