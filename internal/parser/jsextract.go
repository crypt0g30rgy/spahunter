// Package parser extracts JavaScript asset URLs from HTML documents and
// discovers lazily-loaded chunk references inside downloaded JavaScript.
package parser

import (
	"encoding/json"
	"io"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"github.com/crypt0g30rgy/spahunter/internal/urlutil"
)

var (
	titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
)

// ExtractResult carries everything pulled out of one HTML document.
type ExtractResult struct {
	ScriptURLs    []string
	PreloadURLs   []string
	ImportMapURLs []string
	BaseHref      string
	Title         string
}

// ExtractJS parses html (already resolved relative to docURL) and returns
// every discovered JavaScript asset URL, resolved to absolute form.
func Extract(htmlBody, docURL string) ExtractResult {
	var res ExtractResult
	seen := make(map[string]struct{})

	add := func(bucket *[]string, ref string) {
		if ref == "" || strings.HasPrefix(ref, "data:") {
			return
		}
		resolved, err := urlutil.ApplyBaseHref(docURL, res.BaseHref, ref)
		if err != nil || resolved == "" {
			return
		}
		if _, ok := seen[resolved]; ok {
			return
		}
		seen[resolved] = struct{}{}
		*bucket = append(*bucket, resolved)
	}

	z := html.NewTokenizer(strings.NewReader(htmlBody))
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			if z.Err() == io.EOF {
				return res
			}
			return res
		case html.StartTagToken, html.SelfClosingTagToken:
			token := z.Token()
			switch token.Data {
			case "base":
				for _, a := range token.Attr {
					if a.Key == "href" {
						res.BaseHref = strings.TrimSpace(a.Val)
						break
					}
				}
			case "title":
				// x/net/html tokenizer doesn't guarantee the next token is the title text,
				// so we fall back to a regex for this one simple case.
				if m := titleRe.FindStringSubmatch(htmlBody); m != nil {
					res.Title = strings.TrimSpace(stripTags(m[1]))
				}
			case "script":
				var src string
				isImportMap := false
				for _, a := range token.Attr {
					if a.Key == "src" {
						src = a.Val
					} else if a.Key == "type" {
						if a.Val == "importmap" {
							isImportMap = true
						}
					}
				}
				if isImportMap {
					if z.Next() == html.TextToken {
						parseImportMap(z.Token().Data, &res, add)
					}
				} else if src != "" {
					add(&res.ScriptURLs, src)
				}

			case "link":
				var href, as string
				isPreload := false
				for _, a := range token.Attr {
					if a.Key == "href" {
						href = a.Val
					} else if a.Key == "rel" {
						if a.Val == "preload" || a.Val == "modulepreload" {
							isPreload = true
						}
					} else if a.Key == "as" {
						as = a.Val
					}
				}
				if href != "" && isPreload && (as == "script" || as == "module" || strings.HasSuffix(href, ".js")) {
					add(&res.PreloadURLs, href)
				}
			}
		}
	}
}

func parseImportMap(content string, res *ExtractResult, add func(*[]string, string)) {
	var parsed struct {
		Imports map[string]string            `json:"imports"`
		Scopes  map[string]map[string]string `json:"scopes"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &parsed); err == nil {
		for _, v := range parsed.Imports {
			add(&res.ImportMapURLs, v)
		}
		for _, scope := range parsed.Scopes {
			for _, v := range scope {
				add(&res.ImportMapURLs, v)
			}
		}
	}
}

// AllJSURLs flattens every discovered category into one deduplicated list.
func (r ExtractResult) AllJSURLs() []string {
	all := append([]string{}, r.ScriptURLs...)
	all = append(all, r.PreloadURLs...)
	all = append(all, r.ImportMapURLs...)
	return urlutil.Dedup(all)
}

var tagRe = regexp.MustCompile(`(?s)<[^>]+>`)

func stripTags(s string) string {
	return tagRe.ReplaceAllString(s, "")
}

// ExtractScriptListForFingerprint returns the raw <script src=...> list
// used by spa.Detect to compare page shapes without following JS content.
func ExtractScriptListForFingerprint(htmlBody string) []string {
	var out []string
	z := html.NewTokenizer(strings.NewReader(htmlBody))
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			if z.Err() == io.EOF {
				return out
			}
			return out
		case html.StartTagToken, html.SelfClosingTagToken:
			token := z.Token()
			if token.Data == "script" {
				for _, a := range token.Attr {
					if a.Key == "src" {
						out = append(out, a.Val)
						break
					}
				}
			}
		}
	}
}
