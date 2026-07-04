// Package cache implements the global SHA-256 asset cache: identical
// JavaScript seen across different subdomains/environments/hosts is
// downloaded and written to disk only once, with every additional sighting
// recorded purely as a reference.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/crypt0g30rgy/spahunter/internal/model"
)

// Cache is a thread-safe, disk-persisted content-addressed store.
type Cache struct {
	mu      sync.Mutex
	dir     string // <output>/cache/sha256
	index   map[string]*model.CacheEntry
	indexFP string // <output>/cache/asset-index.json
}

// Open loads (or creates) the cache rooted at outputDir/cache.
func Open(outputDir string) (*Cache, error) {
	root := filepath.Join(outputDir, "cache")
	shaDir := filepath.Join(root, "sha256")
	if err := os.MkdirAll(shaDir, 0o755); err != nil {
		return nil, err
	}
	c := &Cache{
		dir:     shaDir,
		index:   make(map[string]*model.CacheEntry),
		indexFP: filepath.Join(root, "asset-index.json"),
	}
	if data, err := os.ReadFile(c.indexFP); err == nil {
		var entries []*model.CacheEntry
		if json.Unmarshal(data, &entries) == nil {
			for _, e := range entries {
				c.index[e.Hash] = e
			}
		}
	}
	return c, nil
}

// Hash computes the SHA-256 hex digest of body.
func Hash(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// LookupResult reports whether content was already cached, and if so
// where the canonical copy lives on disk.
type LookupResult struct {
	Hit           bool
	CanonicalPath string
}

// PutOrRef stores a new asset if its hash has not been seen before, or
// records an additional reference if it has. filename is used only for
// the canonical on-disk copy the first time a hash is seen. Returns
// whether this call created the canonical file (Hit=false) or merely
// referenced an existing one (Hit=true).
func (c *Cache) PutOrRef(body []byte, host, url, relPath string) (LookupResult, error) {
	hash := Hash(body)

	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, ok := c.index[hash]; ok {
		entry.ReferencedBy = append(entry.ReferencedBy, model.AssetRef{Host: host, URL: url, Path: relPath})
		return LookupResult{Hit: true, CanonicalPath: entry.CanonicalPath}, c.saveLocked()
	}

	canonicalPath := filepath.Join(c.dir, hash[:2], hash+".js")
	if err := os.MkdirAll(filepath.Dir(canonicalPath), 0o755); err != nil {
		return LookupResult{}, err
	}
	if err := os.WriteFile(canonicalPath, body, 0o644); err != nil {
		return LookupResult{}, err
	}

	entry := &model.CacheEntry{
		Hash:          hash,
		CanonicalURL:  url,
		CanonicalPath: canonicalPath,
		Size:          int64(len(body)),
		FirstSeen:     time.Now(),
		ReferencedBy:  []model.AssetRef{{Host: host, URL: url, Path: relPath}},
	}
	c.index[hash] = entry

	return LookupResult{Hit: false, CanonicalPath: canonicalPath}, c.saveLocked()
}

// Has reports whether a hash is already cached (read-only check, no ref
// recorded — callers that intend to record a reference should use
// PutOrRef instead).
func (c *Cache) Has(hash string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.index[hash]
	return ok
}

// saveLocked persists the index to disk. Caller must hold c.mu.
func (c *Cache) saveLocked() error {
	entries := make([]*model.CacheEntry, 0, len(c.index))
	for _, e := range c.index {
		entries = append(entries, e)
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.indexFP + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.indexFP)
}

// Save flushes the index explicitly (safe to call periodically or at
// shutdown; PutOrRef already persists after every write).
func (c *Cache) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.saveLocked()
}
