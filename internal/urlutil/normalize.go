// Package urlutil provides URL normalization, resolution and
// deduplication helpers used throughout spahunter.
package urlutil

import (
	"net/url"
	"sort"
	"strings"
)

// Normalize cleans a URL string: it removes the fragment, lower-cases the
// scheme/host, collapses duplicate slashes in the path, and leaves query
// strings intact (since queries can affect which chunk/bundle is served).
func Normalize(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if u.Scheme == "" {
		u.Scheme = "https"
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""

	// collapse duplicate slashes in the path (but not in the scheme part)
	if u.Path != "" {
		parts := strings.Split(u.Path, "/")
		cleaned := parts[:0]
		for i, p := range parts {
			if p == "" && i != 0 && i != len(parts)-1 {
				continue
			}
			cleaned = append(cleaned, p)
		}
		u.Path = strings.Join(cleaned, "/")
		if u.Path == "" {
			u.Path = "/"
		}
	} else {
		u.Path = "/"
	}

	return u.String(), nil
}

// Resolve resolves a possibly-relative reference (including protocol-
// relative "//host/path" forms) against a base URL.
func Resolve(base, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", nil
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	// protocol-relative
	if strings.HasPrefix(ref, "//") {
		ref = baseURL.Scheme + ":" + ref
	}
	refURL, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	resolved := baseURL.ResolveReference(refURL)
	resolved.Fragment = ""
	return resolved.String(), nil
}

// ApplyBaseHref re-resolves a reference against an explicit <base href>
// value, falling back to the document URL if baseHref is empty.
func ApplyBaseHref(docURL, baseHref, ref string) (string, error) {
	if baseHref == "" {
		return Resolve(docURL, ref)
	}
	resolvedBase, err := Resolve(docURL, baseHref)
	if err != nil {
		return Resolve(docURL, ref)
	}
	return Resolve(resolvedBase, ref)
}

// Host returns the lower-cased host (without port) for a URL string.
func Host(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	h := u.Hostname()
	return strings.ToLower(h)
}

// Dedup removes duplicate strings while preserving first-seen order.
func Dedup(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// DedupHosts returns the sorted, deduplicated set of hosts among the given
// URLs.
func DedupHosts(urls []string) []string {
	seen := make(map[string]struct{})
	for _, u := range urls {
		h := Host(u)
		if h == "" {
			continue
		}
		seen[h] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for h := range seen {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}
