// Package model holds shared data structures used across the spahunter
// packages so that internal packages do not need to import each other
// directly (avoids import cycles).
package model

import "time"

// RedirectHop records a single hop in a redirect chain.
type RedirectHop struct {
	From       string `json:"from"`
	To         string `json:"to"`
	StatusCode int    `json:"status_code"`
}

// FetchResult is the outcome of fetching a single URL.
type FetchResult struct {
	OriginalURL   string        `json:"original_url"`
	FinalURL      string        `json:"final_url"`
	StatusCode    int           `json:"status_code"`
	ContentType   string        `json:"content_type"`
	Body          []byte        `json:"-"`
	Size          int64         `json:"size"`
	RedirectChain []RedirectHop `json:"redirect_chain,omitempty"`
	Err           error         `json:"-"`
	Profile       string        `json:"browser_profile,omitempty"`
	Duration      time.Duration `json:"-"`
}

// FrameworkInfo describes a detected frontend framework/build tool.
type FrameworkInfo struct {
	Name       string   `json:"name"`
	Confidence float64  `json:"confidence"`
	Signals    []string `json:"signals,omitempty"`
}

// SPAKind classifies how a site behaves with respect to client-side routing.
type SPAKind string

const (
	KindTraditional SPAKind = "traditional"
	KindHybrid      SPAKind = "hybrid"
	KindSPA         SPAKind = "spa"
	KindUnknown     SPAKind = "unknown"
)

// SPAResult is the outcome of SPA detection for a single host/entry point.
type SPAResult struct {
	EntryPoint    string          `json:"entry_point"`
	FinalURL      string          `json:"final_url"`
	Kind          SPAKind         `json:"kind"`
	Frameworks    []FrameworkInfo `json:"frameworks,omitempty"`
	IndexFallback bool            `json:"index_fallback"`
	WildcardRoute bool            `json:"wildcard_route"`
	RedirectRoot  bool            `json:"redirect_to_root"`
	RedirectChain []RedirectHop   `json:"redirect_chain,omitempty"`
	Signals       []string        `json:"signals,omitempty"`
}

// SourceMapStatus enumerates the state of a source map lookup.
type SourceMapStatus string

const (
	SourceMapFound       SourceMapStatus = "found"
	SourceMapUnavailable SourceMapStatus = "unavailable"
	SourceMapForbidden   SourceMapStatus = "forbidden"
	SourceMapRedirected  SourceMapStatus = "redirected"
	SourceMapNotChecked  SourceMapStatus = "not_checked"
)

// AssetMetadata is the per-asset record persisted to disk.
type AssetMetadata struct {
	OriginalURL     string          `json:"original_url"`
	FinalURL        string          `json:"final_url"`
	StatusCode      int             `json:"response_code"`
	ContentType     string          `json:"content_type"`
	Size            int64           `json:"size"`
	SHA256          string          `json:"sha256"`
	Framework       string          `json:"framework,omitempty"`
	EntryPoint      string          `json:"entry_point,omitempty"`
	DiscoveredFrom  string          `json:"discovered_from,omitempty"`
	Host            string          `json:"host"`
	LocalPath       string          `json:"local_path,omitempty"`
	SourceMapStatus SourceMapStatus `json:"source_map_status"`
	SourceMapURL    string          `json:"source_map_url,omitempty"`
	SourceMapPath   string          `json:"source_map_path,omitempty"`
	RedirectChain   []RedirectHop   `json:"redirect_chain,omitempty"`
	IsDuplicate     bool            `json:"is_duplicate"`
	DuplicateOf     string          `json:"duplicate_of,omitempty"`
	FetchedAt       time.Time       `json:"fetched_at"`
}

// CacheEntry is a record in the global asset cache (keyed by SHA-256).
type CacheEntry struct {
	Hash          string     `json:"hash"`
	CanonicalURL  string     `json:"canonical_url"`
	CanonicalPath string     `json:"canonical_path"`
	Size          int64      `json:"size"`
	FirstSeen     time.Time  `json:"first_seen"`
	ReferencedBy  []AssetRef `json:"referenced_by"`
}

// AssetRef records one location an already-cached asset was seen at.
type AssetRef struct {
	Host string `json:"host"`
	URL  string `json:"url"`
	Path string `json:"path"`
}

// JSAsset represents a JavaScript URL discovered during crawling, prior to
// download.
type JSAsset struct {
	URL            string `json:"url"`
	DiscoveredFrom string `json:"discovered_from"`
	Host           string `json:"host"`
	IsChunk        bool   `json:"is_chunk"`
	EntryPoint     string `json:"entry_point,omitempty"`
}
