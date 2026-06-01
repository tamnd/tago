package cache

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
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
    content_plain  TEXT,
    file_size      INT DEFAULT 0,
    file_mtime     INT DEFAULT 0,
    slug           TEXT,
    page_url       TEXT,
    aliases        TEXT,
    publish_date   TEXT,
    expiry_date    TEXT,
    lastmod        TEXT,
    categories     TEXT,
    keywords       TEXT,
    layout         TEXT
);

CREATE TABLE IF NOT EXISTS build_state (
    key   TEXT PRIMARY KEY,
    value TEXT
);
`

// StatEntry holds the stat-based pre-filter data for a cached file.
type StatEntry struct {
	Hash  string
	Size  int64
	Mtime int64
}

// PageRecord is a row from page_cache.
type PageRecord struct {
	FilePath      string
	FileHash      string
	FileSize      int64
	FileMtime     int64
	Permalink     string
	Title         string
	LinkTitle     string
	Description   string
	Tags          []string
	Categories    []string
	Keywords      []string
	Date          time.Time
	PublishDate   time.Time
	ExpiryDate    time.Time
	Lastmod       time.Time
	Section       string
	Lang          string
	Depth         int
	WordCount     int
	ReadingTime   int
	Summary       string
	ContentPlain  string
	ContentHTML   string
	Params        map[string]any
	Weight        int
	NoIndex       bool
	ExcludeSearch bool
	Type          string
	Layout        string
	Slug          string
	URL           string
	Aliases       []string
	Draft         bool
	Kind          string
	BuiltAt       int64
}

// Cache manages the SQLite build cache.
type Cache struct {
	db *sql.DB
}

// migrations runs once after schema creation to add columns introduced after
// the initial schema. Each statement is allowed to fail (column already exists).
var migrations = []string{
	`ALTER TABLE page_cache ADD COLUMN file_size    INT DEFAULT 0`,
	`ALTER TABLE page_cache ADD COLUMN file_mtime   INT DEFAULT 0`,
	`CREATE INDEX IF NOT EXISTS idx_page_cache_stat ON page_cache (file_path, file_size, file_mtime)`,
	`ALTER TABLE page_cache DROP COLUMN content_html`,
	// Hugo-compat fields added in v0.2.0
	`ALTER TABLE page_cache ADD COLUMN slug          TEXT`,
	`ALTER TABLE page_cache ADD COLUMN page_url      TEXT`,
	`ALTER TABLE page_cache ADD COLUMN aliases       TEXT`,
	`ALTER TABLE page_cache ADD COLUMN publish_date  TEXT`,
	`ALTER TABLE page_cache ADD COLUMN expiry_date   TEXT`,
	`ALTER TABLE page_cache ADD COLUMN lastmod       TEXT`,
	`ALTER TABLE page_cache ADD COLUMN categories    TEXT`,
	`ALTER TABLE page_cache ADD COLUMN keywords      TEXT`,
	`ALTER TABLE page_cache ADD COLUMN layout        TEXT`,
}

// Open opens or creates the cache database at the given path.
func Open(path string) (*Cache, error) {
	db, err := sql.Open("sqlite", path+"?_journal=WAL&_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open cache db: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init cache schema: %w", err)
	}
	for _, m := range migrations {
		db.Exec(m) // ignore errors — column already exists is expected
	}
	c := &Cache{db: db}
	// Run VACUUM once after dropping content_html to reclaim the freed space.
	// Without VACUUM, SQLite keeps the 400 MB of freed pages in the file,
	// causing slow reads on every subsequent build.
	if v, _ := c.GetState("vacuumed_v7"); v == "" {
		db.Exec(`VACUUM`)
		c.SetState("vacuumed_v7", "1")
	}
	return c, nil
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

// LoadStatMap returns a map of file_path → StatEntry (hash + size + mtime).
// Used as a pre-filter to skip SHA-256 when stat fields match the cached values.
func (c *Cache) LoadStatMap() (map[string]StatEntry, error) {
	rows, err := c.db.Query(`SELECT file_path, file_hash, file_size, file_mtime FROM page_cache`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]StatEntry)
	for rows.Next() {
		var path string
		var e StatEntry
		if err := rows.Scan(&path, &e.Hash, &e.Size, &e.Mtime); err != nil {
			return nil, err
		}
		result[path] = e
	}
	return result, rows.Err()
}

// LoadAll returns all cached page records without content_plain.
// Call LoadPlainTexts separately when the search index needs to be rebuilt.
func (c *Cache) LoadAll() ([]*PageRecord, error) {
	rows, err := c.db.Query(`
		SELECT file_path, file_hash, permalink, title, link_title, description,
		       tags, date, section, lang, depth, word_count, summary, params,
		       weight, no_index, exclude_search, type, draft, kind, built_at,
		       COALESCE(reading_time, 0),
		       COALESCE(slug,''), COALESCE(page_url,''), COALESCE(aliases,'[]'),
		       COALESCE(publish_date,''), COALESCE(expiry_date,''), COALESCE(lastmod,''),
		       COALESCE(categories,'[]'), COALESCE(keywords,'[]'), COALESCE(layout,''),
		       COALESCE(file_size,0), COALESCE(file_mtime,0)
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
		var aliasesJSON, categoriesJSON, keywordsJSON string
		var publishDateStr, expiryDateStr, lastmodStr string
		var noIndex, excludeSearch, draft int
		err := rows.Scan(
			&r.FilePath, &r.FileHash, &r.Permalink, &r.Title, &r.LinkTitle,
			&r.Description, &tagsJSON, &dateStr, &r.Section, &r.Lang,
			&r.Depth, &r.WordCount, &r.Summary, &paramsJSON,
			&r.Weight, &noIndex, &excludeSearch, &r.Type, &draft, &r.Kind, &r.BuiltAt,
			&r.ReadingTime,
			&r.Slug, &r.URL, &aliasesJSON,
			&publishDateStr, &expiryDateStr, &lastmodStr,
			&categoriesJSON, &keywordsJSON, &r.Layout,
			&r.FileSize, &r.FileMtime,
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
		if aliasesJSON != "" {
			_ = json.Unmarshal([]byte(aliasesJSON), &r.Aliases)
		}
		if categoriesJSON != "" {
			_ = json.Unmarshal([]byte(categoriesJSON), &r.Categories)
		}
		if keywordsJSON != "" {
			_ = json.Unmarshal([]byte(keywordsJSON), &r.Keywords)
		}
		if paramsJSON != "" {
			r.Params = make(map[string]any)
			_ = json.Unmarshal([]byte(paramsJSON), &r.Params)
		}
		r.Date = parseDate(dateStr)
		r.PublishDate = parseDate(publishDateStr)
		r.ExpiryDate = parseDate(expiryDateStr)
		r.Lastmod = parseDate(lastmodStr)
		if r.PublishDate.IsZero() {
			r.PublishDate = r.Date
		}
		if r.Lastmod.IsZero() {
			r.Lastmod = r.Date
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

func parseDate(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	formats := []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// LoadPlainTexts returns file_path → content_plain for all cached pages.
// Called only when the search index needs rebuilding (contentChange = true).
func (c *Cache) LoadPlainTexts() (map[string]string, error) {
	rows, err := c.db.Query(`SELECT file_path, COALESCE(content_plain, '') FROM page_cache`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]string)
	for rows.Next() {
		var path, plain string
		if err := rows.Scan(&path, &plain); err != nil {
			return nil, err
		}
		result[path] = plain
	}
	return result, rows.Err()
}

const upsertSQL = `
	INSERT INTO page_cache
	    (file_path, file_hash, permalink, title, link_title, description,
	     tags, date, section, lang, depth, word_count, reading_time, summary, params,
	     weight, no_index, exclude_search, type, draft, kind, built_at, content_plain,
	     file_size, file_mtime,
	     slug, page_url, aliases, publish_date, expiry_date, lastmod,
	     categories, keywords, layout)
	VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
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
	    content_plain=excluded.content_plain,
	    file_size=excluded.file_size,
	    file_mtime=excluded.file_mtime,
	    slug=excluded.slug,
	    page_url=excluded.page_url,
	    aliases=excluded.aliases,
	    publish_date=excluded.publish_date,
	    expiry_date=excluded.expiry_date,
	    lastmod=excluded.lastmod,
	    categories=excluded.categories,
	    keywords=excluded.keywords,
	    layout=excluded.layout
`

func marshalStrings(ss []string) string {
	if len(ss) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(ss)
	return string(b)
}

func recordArgs(r *PageRecord, builtAt int64) []any {
	tagsJSON := marshalStrings(r.Tags)
	aliasesJSON := marshalStrings(r.Aliases)
	categoriesJSON := marshalStrings(r.Categories)
	keywordsJSON := marshalStrings(r.Keywords)
	paramsJSON := "{}"
	if len(r.Params) > 0 {
		b, _ := json.Marshal(r.Params)
		paramsJSON = string(b)
	}
	fmtDate := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.Format(time.RFC3339)
	}
	return []any{
		r.FilePath, r.FileHash, r.Permalink, r.Title, r.LinkTitle, r.Description,
		tagsJSON, fmtDate(r.Date), r.Section, r.Lang,
		r.Depth, r.WordCount, r.ReadingTime, r.Summary, paramsJSON,
		r.Weight, boolToInt(r.NoIndex), boolToInt(r.ExcludeSearch), r.Type,
		boolToInt(r.Draft), r.Kind, builtAt, r.ContentPlain,
		r.FileSize, r.FileMtime,
		r.Slug, r.URL, aliasesJSON, fmtDate(r.PublishDate), fmtDate(r.ExpiryDate), fmtDate(r.Lastmod),
		categoriesJSON, keywordsJSON, r.Layout,
	}
}

// Save upserts a single page record into the cache.
func (c *Cache) Save(r *PageRecord) error {
	_, err := c.db.Exec(upsertSQL, recordArgs(r, time.Now().Unix())...)
	return err
}

// SaveBatch upserts all records in a single transaction — far faster than
// calling Save() per page on large corpora.
func (c *Cache) SaveBatch(records []*PageRecord) error {
	if len(records) == 0 {
		return nil
	}
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(upsertSQL)
	if err != nil {
		tx.Rollback()
		return err
	}
	builtAt := time.Now().Unix()
	for _, r := range records {
		if _, err := stmt.Exec(recordArgs(r, builtAt)...); err != nil {
			stmt.Close()
			tx.Rollback()
			return err
		}
	}
	stmt.Close()
	return tx.Commit()
}

// ClearContentHTML is a no-op — content_html is no longer stored in the cache.
// Pages that need re-rendering are detected via file hash changes and
// the layoutChanged / renderVersion flags in build state.
func (c *Cache) ClearContentHTML() error { return nil }

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
