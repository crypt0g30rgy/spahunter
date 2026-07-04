// Package sourcemap locates source map references inside downloaded
// JavaScript (both `//# sourceMappingURL=` and the block-comment form),
// and can optionally probe the conventional "<bundle>.map" path when no
// inline reference exists.
package sourcemap

import (
	"regexp"
	"strings"

	"github.com/crypt0g30rgy/spahunter/internal/fetcher"
	"github.com/crypt0g30rgy/spahunter/internal/model"
	"github.com/crypt0g30rgy/spahunter/internal/urlutil"
)

var (
	lineCommentRe  = regexp.MustCompile(`//[#@]\s*sourceMappingURL=([^\s'"]+)`)
	blockCommentRe = regexp.MustCompile(`/\*[#@]\s*sourceMappingURL=([^\s*]+)\s*\*/`)
)

// FindInline looks for a sourceMappingURL comment inside JS source and
// resolves it against the script's own URL.
func FindInline(js, scriptURL string) (string, bool) {
	if m := lineCommentRe.FindStringSubmatch(js); m != nil {
		if abs, err := urlutil.Resolve(scriptURL, strings.TrimSpace(m[1])); err == nil && abs != "" {
			return abs, true
		}
	}
	if m := blockCommentRe.FindStringSubmatch(js); m != nil {
		if abs, err := urlutil.Resolve(scriptURL, strings.TrimSpace(m[1])); err == nil && abs != "" {
			return abs, true
		}
	}
	return "", false
}

// ConventionalURL returns the "<bundle>.js.map" guess for a script URL.
func ConventionalURL(scriptURL string) string {
	return scriptURL + ".map"
}

// Resolve determines the final source-map status for a script, trying an
// inline reference first and then, if enabled, probing the conventional
// path. It never downloads a map twice for the same resolved URL — the
// global asset cache (internal/cache) is responsible for that dedup, this
// function only decides *whether a map exists and where*.
func Resolve(f *fetcher.Fetcher, js, scriptURL string, probeConventional bool) (status model.SourceMapStatus, mapURL string) {
	if abs, ok := FindInline(js, scriptURL); ok {
		return model.SourceMapFound, abs
	}
	if !probeConventional {
		return model.SourceMapUnavailable, ""
	}

	guess := ConventionalURL(scriptURL)
	res := f.Head(guess)
	if res.Err != nil {
		return model.SourceMapUnavailable, ""
	}
	switch {
	case res.StatusCode == 200:
		return model.SourceMapFound, guess
	case res.StatusCode == 403:
		return model.SourceMapForbidden, guess
	case res.StatusCode >= 300 && res.StatusCode < 400:
		return model.SourceMapRedirected, res.FinalURL
	default:
		return model.SourceMapUnavailable, ""
	}
}
