// Package spa determines whether a target behaves as a traditional
// multi-page site, a hybrid, or a full client-side-routed SPA, by
// comparing the root document against a random, almost-certainly-unrouted
// path.
package spa

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"net/url"
	"path"
	"strings"

	"github.com/crypt0g30rgy/spahunter/internal/fetcher"
	"github.com/crypt0g30rgy/spahunter/internal/framework"
	"github.com/crypt0g30rgy/spahunter/internal/model"
	"github.com/crypt0g30rgy/spahunter/internal/parser"
)

// Detect performs the SPA-vs-traditional-site detection logic.
// It returns the fetch result of the root document AND the SPA result.
func Detect(f *fetcher.Fetcher, entryURL string) (model.FetchResult, model.SPAResult) {
	root := f.Get(entryURL, "text/html")
	result := model.SPAResult{
		EntryPoint:    entryURL,
		FinalURL:      root.FinalURL,
		Kind:          model.KindUnknown,
		RedirectChain: root.RedirectChain,
	}

	if root.Err != nil {
		result.Signals = append(result.Signals, fmt.Sprintf("root fetch failed: %v", root.Err))
		return root, result
	}

	rootHTML := string(root.Body)
	result.Frameworks = framework.DetectAll(rootHTML)

	// Compare root against a random, almost-certainly-unrouted path.
	probePath, _ := url.Parse(root.FinalURL)
	probePath.Path = path.Join(probePath.Path, randomPath())
	probe := f.Get(probePath.String(), "text/html")
	if probe.Err != nil {
		result.Signals = append(result.Signals, fmt.Sprintf("probe fetch failed: %v", probe.Err))
		return root, result
	}

	// --- Classification Logic ---
	rootScripts := parser.ExtractScriptListForFingerprint(rootHTML)
	probeScripts := parser.ExtractScriptListForFingerprint(string(probe.Body))
	rootHash := hashBody(root.Body)
	probeHash := hashBody(probe.Body)

	sameBody := rootHash == probeHash
	sameScripts := sameStringSet(rootScripts, probeScripts)
	probeIs2xx := probe.StatusCode >= 200 && probe.StatusCode < 300
	redirectedToRoot := len(probe.RedirectChain) > 0 && stripQuery(probe.FinalURL) == stripQuery(root.FinalURL)

	if redirectedToRoot {
		result.RedirectRoot = true
		result.Signals = append(result.Signals, "unrouted path redirected back to root")
	}
	if sameBody && probeIs2xx {
		result.IndexFallback = true
		result.Signals = append(result.Signals, "identical body served for unrouted path (index.html fallback)")
	}
	if sameScripts && probeIs2xx && !sameBody {
		result.WildcardRoute = true
		result.Signals = append(result.Signals, "same script bundle set served for unrouted path with differing body")
	}

	switch {
	case result.IndexFallback, result.RedirectRoot, result.WildcardRoute:
		result.Kind = model.KindSPA
	case len(result.Frameworks) > 0:
		result.Kind = model.KindHybrid // has framework but no strong SPA routing signals
	case probe.StatusCode == 404:
		result.Kind = model.KindTraditional
		result.Signals = append(result.Signals, "unrouted path correctly returned 404")
	default:
		result.Kind = model.KindTraditional
	}

	return root, result
}

func randomPath() string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 12)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return "/__spahunter_probe_" + string(b)
}

func hashBody(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func toSet(items []string) map[string]struct{} {
	s := make(map[string]struct{}, len(items))
	for _, it := range items {
		s[it] = struct{}{}
	}
	return s
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true // Both empty is considered the same
	}
	setA := toSet(a)
	setB := toSet(b)
	for k := range setA {
		if _, ok := setB[k]; !ok {
			return false
		}
	}
	return true
}

func stripQuery(u string) string {
	if i := strings.IndexByte(u, '?'); i >= 0 {
		return u[:i]
	}
	return u
}
