// Package storage manages spahunter's on-disk output layout:
//
//	output/<host>/{html,js,maps,metadata,logs}/
//	output/cache/{sha256,asset-index.json}
package storage

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/crypt0g30rgy/spahunter/internal/model"
)

// Storage handles all filesystem writes for a run.
type Storage struct {
	root string
	mu   sync.Mutex
}

// New ensures the output root exists.
func New(root string) (*Storage, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &Storage{root: root}, nil
}

// Root returns the output root directory.
func (s *Storage) Root() string { return s.root }

func (s *Storage) hostDir(host string) string {
	if host == "" {
		host = "_unknown_host"
	}
	return filepath.Join(s.root, sanitize(host))
}

// EnsureHostDirs creates the standard subdirectory layout for a host.
func (s *Storage) EnsureHostDirs(host string) error {
	base := s.hostDir(host)
	for _, sub := range []string{"js", "maps", "metadata"} {
		if err := os.MkdirAll(filepath.Join(base, sub), 0o755); err != nil {
			return err
		}
	}
	return nil
}

// pathForURL derives a stable, collision-resistant relative filename for
// a URL within a host's subdirectory, preserving the original basename
// for readability and appending a short hash of the full URL to avoid
// collisions between different paths that share a basename.
func pathForURL(rawURL, subdir, defaultExt string) string {
	u, err := url.Parse(rawURL)
	base := "asset"
	if err == nil {
		base = filepath.Base(u.Path)
	}
	if base == "" || base == "." || base == "/" {
		base = "index"
	}
	if !strings.Contains(base, ".") && defaultExt != "" {
		base += defaultExt
	}
	h := sha1.Sum([]byte(rawURL))
	short := hex.EncodeToString(h[:])[:10]
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	return filepath.Join(subdir, name+"."+short+ext)
}

// SaveHTML writes an HTML document under <host>/html/.
func (s *Storage) SaveHTML(host, rawURL string, body []byte) (string, error) {
	rel := pathForURL(rawURL, "html", ".html")
	return s.write(host, rel, body)
}

// SaveJS writes a JS asset under <host>/js/.
func (s *Storage) SaveJS(host, rawURL string, body []byte) (string, error) {
	rel := pathForURL(rawURL, "js", ".js")
	return s.write(host, rel, body)
}

// SaveMap writes a source map under <host>/maps/.
func (s *Storage) SaveMap(host, rawURL string, body []byte) (string, error) {
	rel := pathForURL(rawURL, "maps", ".map")
	return s.write(host, rel, body)
}

func (s *Storage) write(host, rel string, body []byte) (string, error) {
	full := filepath.Join(s.hostDir(host), rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(full, body, 0o644); err != nil {
		return "", err
	}
	return full, nil
}

// SaveMetadata appends one asset's metadata record to
// <host>/metadata/assets.jsonl (JSON Lines, safe for concurrent append
// under the storage mutex and easy to resume/stream-process).
func (s *Storage) SaveMetadata(host string, meta model.AssetMetadata) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Join(s.hostDir(host), "metadata")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "assets.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

// SaveSPAResult writes the SPA-detection result for a host.
func (s *Storage) SaveSPAResult(host string, result model.SPAResult) error {
	if err := s.EnsureHostDirs(host); err != nil {
		return err
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.hostDir(host), "metadata", "spa_result.json"), data, 0o644)
}

// LoadCompletedURLs scans an existing metadata/assets.jsonl for a host and
// returns the set of original URLs already recorded, for --resume.
func (s *Storage) LoadCompletedURLs(host string) map[string]struct{} {
	done := make(map[string]struct{})
	for _, m := range s.LoadCompletedAssets(host) {
		done[m.OriginalURL] = struct{}{}
	}
	return done
}

// LoadCompletedAssets scans an existing metadata/assets.jsonl for a host
// and returns the full metadata records, for --resume and --maps-only.
func (s *Storage) LoadCompletedAssets(host string) []model.AssetMetadata {
	var out []model.AssetMetadata
	fp := filepath.Join(s.hostDir(host), "metadata", "assets.jsonl")
	data, err := os.ReadFile(fp)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m model.AssetMetadata
		if json.Unmarshal([]byte(line), &m) == nil {
			out = append(out, m)
		}
	}
	return out
}

func sanitize(host string) string {
	// Replace characters that are invalid in Windows directory names.
	// This is a simplified approach; a more robust solution for
	// cross-platform compatibility might involve a library or a more
	// extensive mapping of invalid characters for different filesystems.
	replacer := strings.NewReplacer(
		":", "_",
		"/", "_",
		"\\", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
		"-", "_", // Also replace hyphens to be safe with subdomains
	)
	return replacer.Replace(host)
}
