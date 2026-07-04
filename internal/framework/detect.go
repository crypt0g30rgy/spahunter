// Package framework detects which frontend framework and/or build tool
// produced a given page, based on static HTML markers, script naming
// conventions and well-known metadata tags. It never executes JavaScript.
package framework

import (
	"regexp"
	"strings"

	"github.com/crypt0g30rgy/spahunter/internal/model"
)

type signal struct {
	name    string
	pattern *regexp.Regexp
	weight  float64
}

// signals is deliberately ordered from most to least specific so that
// meta-frameworks (Next.js, Nuxt, Gatsby...) are detected ahead of their
// underlying base framework (React, Vue) when both markers are present.
var signals = []signal{
	{"Next.js", regexp.MustCompile(`(?i)__NEXT_DATA__|/_next/static/|next/dist`), 0.9},
	{"Nuxt", regexp.MustCompile(`(?i)__NUXT__|/_nuxt/`), 0.9},
	{"Gatsby", regexp.MustCompile(`(?i)___gatsby|/page-data/|gatsby-`), 0.9},
	{"Remix", regexp.MustCompile(`(?i)__remixContext|/build/_shared/`), 0.85},
	{"Astro", regexp.MustCompile(`(?i)astro-island|data-astro-cid`), 0.85},
	{"Angular", regexp.MustCompile(`(?i)ng-version=|<app-root|ng-app`), 0.85},
	{"Vue", regexp.MustCompile(`(?i)data-v-app|__VUE__|v-cloak|__vue_app__`), 0.75},
	{"React", regexp.MustCompile(`(?i)data-reactroot|data-reactid|__REACT_DEVTOOLS_GLOBAL_HOOK__|id=["']root["']`), 0.6},
	{"Svelte", regexp.MustCompile(`(?i)svelte-[a-z0-9]{6,}`), 0.7},
	{"Vite", regexp.MustCompile(`(?i)/@vite/client|type=["']module["'][^>]*src=["'][^"']*\.jsx?["']|vite\.svg`), 0.6},
	{"Webpack", regexp.MustCompile(`(?i)webpackJsonp|__webpack_require__|/webpack/runtime`), 0.5},
}

// Detect inspects HTML body content for framework fingerprints.
func Detect(html string) model.FrameworkInfo {
	best := model.FrameworkInfo{Name: "unknown", Confidence: 0}
	var matched []string

	// Track a raw hit list; a page can legitimately show more than one
	// signal (e.g. Next.js app using React under the hood), so we report
	// the highest-confidence match as primary and record the rest.
	type hit struct {
		name   string
		weight float64
	}
	var hits []hit

	for _, s := range signals {
		if s.pattern.MatchString(html) {
			hits = append(hits, hit{s.name, s.weight})
			matched = append(matched, s.name)
		}
	}

	if len(hits) == 0 {
		return best
	}

	for _, h := range hits {
		if h.weight > best.Confidence {
			best.Confidence = h.weight
			best.Name = h.name
		}
	}
	best.Signals = matched
	return best
}

// DetectAll returns every matching framework (not just the top one),
// useful when a page is a meta-framework built atop a base library.
func DetectAll(html string) []model.FrameworkInfo {
	var out []model.FrameworkInfo
	for _, s := range signals {
		if s.pattern.MatchString(html) {
			out = append(out, model.FrameworkInfo{Name: s.name, Confidence: s.weight, Signals: []string{s.name}})
		}
	}
	return out
}

// ScriptNameHints inspects a list of script URLs for filename conventions
// that hint at a bundler even when no HTML markers were found (e.g. an
// API-served SPA shell with minimal markup).
func ScriptNameHints(scriptURLs []string) []string {
	var hints []string
	for _, u := range scriptURLs {
		lower := strings.ToLower(u)
		switch {
		case strings.Contains(lower, "_next/static"):
			hints = append(hints, "Next.js (script path)")
		case strings.Contains(lower, "_nuxt/"):
			hints = append(hints, "Nuxt (script path)")
		case strings.Contains(lower, "chunk-vendors") || strings.Contains(lower, "app.") && strings.Contains(lower, ".chunk.js"):
			hints = append(hints, "Webpack (chunk naming)")
		case strings.Contains(lower, "/assets/") && strings.Contains(lower, "-legacy"):
			hints = append(hints, "Vite (legacy chunk)")
		}
	}
	return hints
}
