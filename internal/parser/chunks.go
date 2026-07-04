package parser

import (
	"regexp"
	"strings"

	"github.com/crypt0g30rgy/spahunter/internal/urlutil"
)

// Chunk-reference patterns. These intentionally favour recall over
// precision — false positives are cheap (a failed/filtered fetch),
// missed chunks are not.
var (
	// Dynamic ESM imports: import("./foo.js") or import('foo')
	dynamicImportRe = regexp.MustCompile(`\bimport\s*\(\s*["'\x60]([^"'\x60]+)["'\x60]\s*\)`)

	// Webpack runtime chunk URL construction:
	//   __webpack_require__.e("123")   -> chunk id, not a URL directly
	//   webpackJsonp.push / webpackChunk<name>.push([[id], ...])
	// We can't resolve numeric chunk IDs to filenames without executing
	// the runtime, but webpack typically also emits a public-path +
	// filename map we can regex for, and many builds inline literal
	// chunk URLs as string constants near ".chunk.js" / content hashes.
	webpackChunkURLRe   = regexp.MustCompile(`["'\x60]([\w\-./]+?\.[0-9a-f]{6,20}\.chunk\.js)["'\x60]`)
	webpackChunkURLRe2  = regexp.MustCompile(`["'\x60]((?:static/|assets/|js/)?[\w\-./]+?\.[0-9a-f]{6,20}\.js)["'\x60]`)
	webpackPublicPathRe = regexp.MustCompile(`__webpack_require__\.p\s*=\s*["'\x60]([^"'\x60]*)["'\x60]`)
	webpackChunkPushRe  = regexp.MustCompile(`\(?(?:window\[[^\]]+\]|self\.webpackChunk[\w$]*|webpackJsonp[\w$]*)\s*=\s*(?:window\[[^\]]+\]|self\.webpackChunk[\w$]*|webpackJsonp[\w$]*)\s*\|\|\s*\[\]`)

	// Vite/Rollup preload helper: __vitePreload(() => import("./chunk-xyz.js"), ["./chunk-xyz.js", ...])
	viteChunkListRe = regexp.MustCompile(`["'\x60]([\w\-./]+?\.[0-9a-zA-Z_-]{6,12}\.js)["'\x60]`)

	// Generic asset-manifest style: {"chunk-a": "/assets/chunk-a.abc123.js"}
	genericJSFileRe = regexp.MustCompile(`["'\x60]([\w\-./]+?\.js)["'\x60]`)
)

// ExtractChunkRefs scans JS source for lazily-loaded chunk references and
// returns them resolved to absolute URLs against sourceURL. publicPath,
// if discovered inline via __webpack_require__.p, is applied to bare
// filename references.
func ExtractChunkRefs(js string, sourceURL string) []string {
	var refs []string
	seen := make(map[string]struct{})

	publicPath := ""
	if m := webpackPublicPathRe.FindStringSubmatch(js); m != nil {
		publicPath = m[1]
	}

	addRaw := func(ref string) {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return
		}
		// skip obvious non-path noise
		if strings.Contains(ref, " ") || strings.HasPrefix(ref, "http") == false && strings.HasPrefix(ref, "/") == false && strings.HasPrefix(ref, "./") == false && strings.HasPrefix(ref, "../") == false && !looksLikeBareChunkFilename(ref) {
			return
		}

		base := sourceURL
		if publicPath != "" && !strings.HasPrefix(ref, "http") && !strings.HasPrefix(ref, "/") {
			resolved, err := urlutil.Resolve(sourceURL, publicPath)
			if err == nil {
				base = resolved
			}
		}

		abs, err := urlutil.Resolve(base, ref)
		if err != nil || abs == "" {
			return
		}
		if _, ok := seen[abs]; ok {
			return
		}
		seen[abs] = struct{}{}
		refs = append(refs, abs)
	}

	for _, m := range dynamicImportRe.FindAllStringSubmatch(js, -1) {
		addRaw(m[1])
	}
	for _, m := range webpackChunkURLRe.FindAllStringSubmatch(js, -1) {
		addRaw(m[1])
	}
	for _, m := range webpackChunkURLRe2.FindAllStringSubmatch(js, -1) {
		addRaw(m[1])
	}
	for _, m := range viteChunkListRe.FindAllStringSubmatch(js, -1) {
		addRaw(m[1])
	}

	return urlutil.Dedup(refs)
}

// looksLikeBareChunkFilename allows things like "chunk-abc123.js" through
// even without a path separator, since bundlers frequently reference
// sibling chunks by bare filename (resolved against the current script's
// directory or the discovered public path).
func looksLikeBareChunkFilename(s string) bool {
	return strings.HasSuffix(s, ".js") && !strings.ContainsAny(s, "{}()<>\"'")
}

// IsWebpackRuntime is a light heuristic to flag whether a downloaded
// script is itself a webpack runtime/bootstrap chunk (useful for logging
// / prioritizing chunk-map scraping).
func IsWebpackRuntime(js string) bool {
	return webpackChunkPushRe.MatchString(js) || strings.Contains(js, "__webpack_require__")
}
