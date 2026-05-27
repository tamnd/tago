package cache

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const schema = `
CREATE TABLE IF NOT EXISTS page_cache (
    file_path      TEXT PRIMARY KEY,
    file_hash      TEXT NOT NULL,
    permalink      TEXT NOT NULL,
    title          TEXT,
    description    TEXT,
    tags           TEXT,
    date           TEXT,
    section        TEXT,
    lang           TEXT,
    depth          INT DEFAULT 0,
    word_count     INT DEFAULT 0,
    reading_time   INT DEFAULT 0,
    summary        TEXT,
    params         TEXT,
    weight         INT DEFAULT 0,
    no_index       INT DEFAULT 0,
    exclude_search INT DEFAULT 0,
    type           TEXT,
    draft          INT DEFAULT 0,
    built_at       INT,
    link_title     TEXT,
    kind           TEXT,
    content_plain  TEXT
);

CREATE TABLE IF NOT EXISTS build_state (
    key   TEXT PRIMARY KEY,
    value TEXT
);
`

// PageRecord is a row from page_cache.
type PageRecord struct {
	FilePath      string
	FileHash      string
	Permalink     string
	Title         string
	LinkTitle     string
	Description   string
	Tags          []string
	Date          time.Time
	Section       string
	Lang          string
	Depth         int
	WordCount     int
	ReadingTime   int
	Summary       string
	ContentPlain  string // plain text for search (stripped HTML)
	Params        map[string]any
	Weight        int
	NoIndex       bool
	ExcludeSearch bool
	Type          string
	Draft         bool
	Kind          string
	BuiltAt       int64
}

// Cache manages the SQLite build cache.
type Cache struct {
	db *sql.DB
}

// Open opens or creates the cache database at the given path.
func Open(path string) (*Cache, error) {
	db, err := sql.Open("sqlite3", path+"?_journal=WAL&_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open cache db: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init cache schema: %w", err)
	}
	return &Cache{db: db}, nil
}

// Close closes the database.
func (c *Cache) Close() error {
	return c.db.Close()
}

// LoadHashes returns a map of file_path → file_hash for all cached pages.
func (c *Cache) LoadHashes() (map[string]string, error) {
	rows, err := c.db.Query(`SELECT file_path, file_hash FROM page_cache`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]string)
	for rows.Next() {
		var path, hash string
		if err := rows.Scan(&path, &hash); err != nil {
			return nil, err
		}
		result[path] = hash
	}
	return result, rows.Err()
}

// LoadAll returns all cached page records.
func (c *Cache) LoadAll() ([]*PageRecord, error) {
	rows, err := c.db.Query(`
		SELECT file_path, file_hash, permalink, title, link_title, description,
		       tags, date, section, lang, depth, word_count, summary, params,
		       weight, no_index, exclude_search, type, draft, kind, built_at,
		       COALESCE(content_plain, ''), COALESCE(reading_time, 0)
		FROM page_cache
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*PageRecord
	for rows.Next() {
		r := &PageRecord{}
		var tagsJSON, paramsJSON, dateStr string
		var noIndex, excludeSearch, draft int
		err := rows.Scan(
			&r.FilePath, &r.FileHash, &r.Permalink, &r.Title, &r.LinkTitle,
			&r.Description, &tagsJSON, &dateStr, &r.Section, &r.Lang,
			&r.Depth, &r.WordCount, &r.Summary, &paramsJSON,
			&r.Weight, &noIndex, &excludeSearch, &r.Type, &draft, &r.Kind, &r.BuiltAt,
			&r.ContentPlain, &r.ReadingTime,
		)
		if err != nil {
			return nil, err
		}
		r.NoIndex = noIndex != 0
		r.ExcludeSearch = excludeSearch != 0
		r.Draft = draft != 0
		if tagsJSON != "" {
			_ = json.Unmarshal([]byte(tagsJSON), &r.Tags)
		}
		if paramsJSON != "" {
			r.Params = make(map[string]any)
			_ = json.Unmarshal([]byte(paramsJSON), &r.Params)
		}
		if dateStr != "" {
			formats := []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"}
			for _, f := range formats {
				if t, err := time.Parse(f, dateStr); err == nil {
					r.Date = t
					break
				}
			}
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// Save upserts a page record into the cache.
func (c *Cache) Save(r *PageRecord) error {
	tagsJSON := "[]"
	if len(r.Tags) > 0 {
		b, _ := json.Marshal(r.Tags)
		tagsJSON = string(b)
	}
	paramsJSON := "{}"
	if len(r.Params) > 0 {
		b, _ := json.Marshal(r.Params)
		paramsJSON = string(b)
	}
	dateStr := ""
	if !r.Date.IsZero() {
		dateStr = r.Date.Format(time.RFC3339)
	}
	noIndex := boolToInt(r.NoIndex)
	excludeSearch := boolToInt(r.ExcludeSearch)
	draft := boolToInt(r.Draft)
	builtAt := time.Now().Unix()

	_, err := c.db.Exec(`
		INSERT INTO page_cache
		    (file_path, file_hash, permalink, title, link_title, description,
		     tags, date, section, lang, depth, word_count, reading_time, summary, params,
		     weight, no_index, exclude_search, type, draft, kind, built_at, content_plain)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(file_path) DO UPDATE SET
		    file_hash=excluded.file_hash,
		    permalink=excluded.permalink,
		    title=excluded.title,
		    link_title=excluded.link_title,
		    description=excluded.description,
		    tags=excluded.tags,
		    date=excluded.date,
		    section=excluded.section,
		    lang=excluded.lang,
		    depth=excluded.depth,
		    word_count=excluded.word_count,
		    reading_time=excluded.reading_time,
		    summary=excluded.summary,
		    params=excluded.params,
		    weight=excluded.weight,
		    no_index=excluded.no_index,
		    exclude_search=excluded.exclude_search,
		    type=excluded.type,
		    draft=excluded.draft,
		    kind=excluded.kind,
		    built_at=excluded.built_at,
		    content_plain=excluded.content_plain
	`,
		r.FilePath, r.FileHash, r.Permalink, r.Title, r.LinkTitle, r.Description,
		tagsJSON, dateStr, r.Section, r.Lang,
		r.Depth, r.WordCount, r.ReadingTime, r.Summary, paramsJSON,
		r.Weight, noIndex, excludeSearch, r.Type, draft, r.Kind, builtAt, r.ContentPlain,
	)
	return err
}

// Delete removes a page record from the cache.
func (c *Cache) Delete(filePath string) error {
	_, err := c.db.Exec(`DELETE FROM page_cache WHERE file_path = ?`, filePath)
	return err
}

// DeleteMany removes multiple page records.
func (c *Cache) DeleteMany(filePaths []string) error {
	if len(filePaths) == 0 {
		return nil
	}
	placeholders := strings.Repeat("?,", len(filePaths))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(filePaths))
	for i, p := range filePaths {
		args[i] = p
	}
	_, err := c.db.Exec(`DELETE FROM page_cache WHERE file_path IN (`+placeholders+`)`, args...)
	return err
}

// GetState retrieves a build state value by key.
func (c *Cache) GetState(key string) (string, error) {
	var value string
	err := c.db.QueryRow(`SELECT value FROM build_state WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// SetState stores a build state value.
func (c *Cache) SetState(key, value string) error {
	_, err := c.db.Exec(`
		INSERT INTO build_state (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value
	`, key, value)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Diff computes which files changed or were deleted.
// diskHashes: file_path → hash for all files currently on disk
// Returns (changed, deleted) file paths.
func Diff(cachedHashes, diskHashes map[string]string) (changed, deleted []string) {
	for path, diskHash := range diskHashes {
		cachedHash, inCache := cachedHashes[path]
		if !inCache || cachedHash != diskHash {
			changed = append(changed, path)
		}
	}
	for path := range cachedHashes {
		if _, onDisk := diskHashes[path]; !onDisk {
			deleted = append(deleted, path)
		}
	}
	return
}
