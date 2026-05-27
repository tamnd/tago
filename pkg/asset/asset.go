package asset

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Refs holds the final URLs for fingerprinted assets.
type Refs struct {
	CSS        string
	JSHead     string
	JS         string
	FlexSearch string
	KaTeX      string
	Extra      map[string]string // original basename → fingerprinted URL
}

// ProcessAssets fingerprints CSS/JS files and copies static assets.
// staticDir: source directory (e.g. site/static/)
// outputDir: destination (e.g. public/)
// Returns asset refs with fingerprinted URLs.
func ProcessAssets(staticDir, outputDir string) (Refs, error) {
	var refs Refs

	if staticDir == "" {
		return refs, nil
	}

	if _, err := os.Stat(staticDir); os.IsNotExist(err) {
		return refs, nil
	}

	err := filepath.Walk(staticDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		rel, _ := filepath.Rel(staticDir, path)
		ext := strings.ToLower(filepath.Ext(path))

		// Fingerprint CSS and JS
		if ext == ".css" || ext == ".js" {
			fingerprinted, err := fingerprintFile(path, rel, outputDir)
			if err != nil {
				return err
			}
			// Update refs based on filename
			base := strings.ToLower(filepath.Base(rel))
			url := "/" + filepath.ToSlash(fingerprinted)
			switch {
			case strings.Contains(base, "flexsearch"):
				refs.FlexSearch = url
			case strings.Contains(base, "katex"):
				if ext == ".css" {
					refs.KaTeX = url
				}
			case strings.Contains(base, "head") && ext == ".js":
				refs.JSHead = url
			case ext == ".css":
				refs.CSS = url
			case ext == ".js":
				refs.JS = url
			}
			// Always record in Extra for templates to use by original name
			if refs.Extra == nil {
				refs.Extra = make(map[string]string)
			}
			refs.Extra[strings.ToLower(filepath.Base(rel))] = url
		} else {
			// Copy as-is
			dest := filepath.Join(outputDir, rel)
			if err := copyFile(path, dest); err != nil {
				return err
			}
		}
		return nil
	})

	return refs, err
}

// fingerprintFile copies src to outputDir with a hash suffix in the filename.
// Returns the relative path of the output file.
func fingerprintFile(src, relPath, outputDir string) (string, error) {
	hash, err := hashFile(src)
	if err != nil {
		return "", err
	}

	ext := filepath.Ext(relPath)
	base := strings.TrimSuffix(relPath, ext)
	fingerprinted := base + "." + hash[:8] + ext

	dest := filepath.Join(outputDir, fingerprinted)
	if err := copyFile(src, dest); err != nil {
		return "", err
	}
	return fingerprinted, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func copyFile(src, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}

	// Check if already exists with same content
	if _, err := os.Stat(dest); err == nil {
		srcHash, _ := hashFile(src)
		dstHash, _ := hashFile(dest)
		if srcHash == dstHash {
			return nil
		}
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// CopyDir copies all files from src to dst recursively (without fingerprinting).
func CopyDir(src, dst string) error {
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		dest := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(dest, 0755)
		}
		return copyFile(path, dest)
	})
}
