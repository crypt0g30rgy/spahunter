// Package pipeline wires together fetcher, framework/spa detection,
// parser, filters, validator, sourcemap, cache and storage into the
// end-to-end asset-acquisition workflow, and implements each CLI mode.
package pipeline

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/crypt0g30rgy/spahunter/internal/cache"
	"github.com/crypt0g30rgy/spahunter/internal/config"
	"github.com/crypt0g30rgy/spahunter/internal/fetcher"
	"github.com/crypt0g30rgy/spahunter/internal/filters"
	"github.com/crypt0g30rgy/spahunter/internal/framework"
	"github.com/crypt0g30rgy/spahunter/internal/logging"
	"github.com/crypt0g30rgy/spahunter/internal/model"
	"github.com/crypt0g30rgy/spahunter/internal/parser"
	"github.com/crypt0g30rgy/spahunter/internal/queue"
	"github.com/crypt0g30rgy/spahunter/internal/sourcemap"
	"github.com/crypt0g30rgy/spahunter/internal/spa"
	"github.com/crypt0g30rgy/spahunter/internal/storage"
	"github.com/crypt0g30rgy/spahunter/internal/urlutil"
	"github.com/crypt0g30rgy/spahunter/internal/validator"
)

// Pipeline holds every long-lived component for a run.
type Pipeline struct {
	cfg    *config.Config
	fetch  *fetcher.Fetcher
	store  *storage.Storage
	logger *logging.Logger
	filter *filters.Filter
	cache  *cache.Cache
}

// New builds a Pipeline from a parsed Config.
func New(cfg *config.Config) (*Pipeline, error) {
	f, err := fetcher.New(fetcher.Options{
		Timeout:       cfg.Timeout,
		Retries:       cfg.Retries,
		MaxRedirects:  cfg.MaxRedirects,
		NoRedirects:   cfg.NoRedirects,
		PinnedProfile: cfg.UserAgentProfile,
	})
	if err != nil {
		return nil, fmt.Errorf("fetcher: %w", err)
	}

	st, err := storage.New(cfg.Output)
	if err != nil {
		return nil, fmt.Errorf("storage: %w", err)
	}

	lg, err := logging.New(cfg.Output+"/_logs", cfg.Verbose)
	if err != nil {
		return nil, fmt.Errorf("logging: %w", err)
	}

	filt, err := filters.New(filters.Config{
		IncludeThirdParty: cfg.IncludeThirdParty,
		SkipCommonLibs:    cfg.SkipCommonLibs,
		ExcludeRegex:      cfg.ExcludeRegex,
		IncludeRegex:      cfg.IncludeRegex,
	})
	if err != nil {
		return nil, fmt.Errorf("filters: %w", err)
	}

	var c *cache.Cache
	if cfg.CacheAssets {
		c, err = cache.Open(cfg.Output)
		if err != nil {
			return nil, fmt.Errorf("cache: %w", err)
		}
	}

	return &Pipeline{cfg: cfg, fetch: f, store: st, logger: lg, filter: filt, cache: c}, nil
}

// Close releases pipeline resources.
func (p *Pipeline) Close() {
	if p.cache != nil {
		_ = p.cache.Save()
	}
	p.logger.Close()
}

// LoadURLs reads the input file (one URL per line, '#' comments allowed)
// or wraps a single -u URL, normalizing and deduplicating.
func LoadURLs(cfg *config.Config) ([]string, error) {
	var raw []string
	if cfg.InputURL != "" {
		raw = append(raw, cfg.InputURL)
	}
	if cfg.InputFile != "" {
		f, err := os.Open(cfg.InputFile)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		buf := make([]byte, 0, 64*1024)
		sc.Buffer(buf, 1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			raw = append(raw, line)
		}
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}

	out := make([]string, 0, len(raw))
	for _, r := range raw {
		n, err := urlutil.Normalize(r)
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	return urlutil.Dedup(out), nil
}

// Run dispatches to the handler for the configured mode.
func (p *Pipeline) Run(urls []string) error {
	switch p.cfg.Mode {
	case config.ModeDetectSPA:
		return p.runDetectSPA(urls)
	case config.ModeListJS:
		return p.runListJS(urls)
	case config.ModeDownload:
		return p.runDownloadOnly(urls)
	case config.ModeMapsOnly:
		return p.runMapsOnly(urls)
	case config.ModeResume, config.ModeFull:
		return p.runFull(urls, p.cfg.Mode == config.ModeResume)
	default:
		return fmt.Errorf("unknown mode: %s", p.cfg.Mode)
	}
}

// ---- SPA Detection Only ----------------------------------------------

func (p *Pipeline) runDetectSPA(urls []string) error {
	pool := queue.NewPool(p.cfg.Workers)
	for _, u := range urls {
		u := u
		pool.Submit(func() {
			host := urlutil.Host(u)
			_, result := spa.Detect(p.fetch, u)
			if err := p.store.SaveSPAResult(host, result); err != nil {
				p.logger.Log(logging.CatFailed, "save spa result %s: %v", u, err)
			}
			p.logger.Log(logging.CatSPA, "%s kind=%s frameworks=%v entry=%s final=%s",
				u, result.Kind, frameworkNames(result.Frameworks), result.EntryPoint, result.FinalURL)
			fmt.Printf("%-45s kind=%-12s frameworks=%v\n", u, result.Kind, frameworkNames(result.Frameworks))
		})
	}
	pool.Wait()
	pool.Close()
	return nil
}

func frameworkNames(list []model.FrameworkInfo) []string {
	out := make([]string, 0, len(list))
	for _, f := range list {
		out = append(out, f.Name)
	}
	return out
}

// ---- List JavaScript Only ---------------------------------------------

func (p *Pipeline) runListJS(urls []string) error {
	pool := queue.NewPool(p.cfg.Workers)
	for _, u := range urls {
		u := u
		pool.Submit(func() {
			p.listJSForEntry(u)
		})
	}
	pool.Wait()
	pool.Close()
	return nil
}

func (p *Pipeline) listJSForEntry(entryURL string) {
	res := p.fetch.Get(entryURL, "text/html")
	if res.Err != nil {
		p.logger.Log(logging.CatFailed, "fetch %s: %v", entryURL, res.Err)
		return
	}
	if len(res.RedirectChain) > 0 {
		p.logger.Log(logging.CatRedirects, "%s -> %s (%d hops)", entryURL, res.FinalURL, len(res.RedirectChain))
	}
	extract := parser.Extract(string(res.Body), res.FinalURL)
	for _, jsURL := range extract.AllJSURLs() {
		decision := p.filter.Allow(jsURL)
		if !decision.Allow {
			p.logger.Log(logging.CatSkippedThird, "%s (%s)", jsURL, decision.Reason)
			continue
		}
		fmt.Println(jsURL)
	}
}

// ---- Download Only ------------------------------------------------------

// runDownloadOnly treats each input line as a JS URL to download directly
// (e.g. a list previously produced by list-js mode).
func (p *Pipeline) runDownloadOnly(urls []string) error {
	pool := queue.NewPool(p.cfg.Workers)
	for _, u := range urls {
		u := u
		pool.Submit(func() {
			host := urlutil.Host(u)
			_ = p.store.EnsureHostDirs(host)
			p.downloadJS(u, host, "download-only-mode")
		})
	}
	pool.Wait()
	pool.Close()
	return nil
}

// ---- Source Maps Only ---------------------------------------------------

// runMapsOnly walks previously-downloaded JS files recorded in each host's
// metadata/assets.jsonl and retrieves any source maps for them, without
// re-crawling HTML. It reads each asset's own local copy to look for an
// inline sourceMappingURL comment before falling back to probing the
// conventional "<bundle>.js.map" path — it does not treat already-map
// files, or the JS files themselves, as targets needing a map of their
// own.
func (p *Pipeline) runMapsOnly(urls []string) error {
	// In maps-only mode, `urls` is interpreted as a list of hosts or entry
	// URLs whose already-downloaded JS we should probe for maps.
	pool := queue.NewPool(p.cfg.Workers)
	seenHosts := make(map[string]struct{})
	for _, u := range urls {
		host := urlutil.Host(u)
		if host == "" {
			continue
		}
		if _, ok := seenHosts[host]; ok {
			continue
		}
		seenHosts[host] = struct{}{}

		// Deduplicate completed assets by OriginalURL, keeping the most recent state
		assetsMap := make(map[string]model.AssetMetadata)
		for _, asset := range p.store.LoadCompletedAssets(host) {
			if isMapURL(asset.OriginalURL) {
				continue // this record IS a map, not something needing one
			}
			assetsMap[asset.OriginalURL] = asset
		}

		for _, asset := range assetsMap {
			asset := asset
			host := host
			if asset.SourceMapStatus == model.SourceMapFound && asset.SourceMapURL != "" {
				if asset.SourceMapPath != "" {
					if _, err := os.Stat(asset.SourceMapPath); err == nil {
						continue // Already downloaded and exists on disk
					}
				}
			}
			if asset.LocalPath == "" {
				continue // nothing on disk to inspect (e.g. rejected HTML)
			}
			pool.Submit(func() {
				body, err := os.ReadFile(asset.LocalPath)
				if err != nil {
					p.logger.Log(logging.CatFailed, "read local %s: %v", asset.LocalPath, err)
					return
				}
				status, mapURL := sourcemap.Resolve(p.fetch, string(body), asset.OriginalURL, true)
				p.logger.Log(logging.CatSourceMaps, "%s status=%s url=%s", asset.OriginalURL, status, mapURL)
				if status == model.SourceMapFound && mapURL != "" {
					if mapPath, ok := p.fetchAndStoreSourceMap(mapURL, asset.OriginalURL, host); ok {
						asset.SourceMapStatus = status
						asset.SourceMapURL = mapURL
						asset.SourceMapPath = mapPath
						_ = p.store.SaveMetadata(host, asset)
					}
				}
			})
		}
	}
	pool.Wait()
	pool.Close()
	return nil
}

func isMapURL(u string) bool {
	return strings.HasSuffix(strings.ToLower(u), ".map")
}

// ---- Full pipeline -------------------------------------------------------

func (p *Pipeline) runFull(urls []string, resume bool) error {
	pool := queue.NewPool(p.cfg.Workers)
	for _, u := range urls {
		u := u
		pool.Submit(func() {
			p.processEntry(u, resume)
		})
	}
	pool.Wait()
	pool.Close()
	return nil
}

func (p *Pipeline) processEntry(entryURL string, resume bool) {
	host := urlutil.Host(entryURL)
	if err := p.store.EnsureHostDirs(host); err != nil {
		p.logger.Log(logging.CatFailed, "ensure dirs %s: %v", host, err)
		return
	}

	// 1. Initial GET + framework/SPA detection share the same fetch.
	root, spaResult := spa.Detect(p.fetch, entryURL)
	_ = p.store.SaveSPAResult(host, spaResult)
	p.logger.Log(logging.CatSPA, "%s kind=%s frameworks=%v", entryURL, spaResult.Kind, frameworkNames(spaResult.Frameworks))
	if len(root.RedirectChain) > 0 {
		p.logger.Log(logging.CatRedirects, "%s -> %s (%d hops)", entryURL, root.FinalURL, len(root.RedirectChain))
	}

	if root.Err != nil {
		p.logger.Log(logging.CatFailed, "fetch entry %s: %v", entryURL, root.Err)
		return
	}

	frameworkPrimary := "unknown"
	if len(spaResult.Frameworks) > 0 {
		frameworkPrimary = spaResult.Frameworks[0].Name
	} else {
		frameworkPrimary = framework.Detect(string(root.Body)).Name
	}

	extract := parser.Extract(string(root.Body), root.FinalURL)
	jsURLs := extract.AllJSURLs()

	var completed map[string]struct{}
	if resume {
		completed = p.store.LoadCompletedURLs(host)
	}

	// 2. Recursive frontier: download each JS asset, parse it for lazy
	// chunks, and enqueue newly discovered chunk URLs.
	frontier := queue.NewFrontier(p.cfg.Workers, func(jsURL string, enqueue func(string)) {
		if resume {
			if _, done := completed[jsURL]; done {
				return
			}
		}
		decision := p.filter.Allow(jsURL)
		if !decision.Allow {
			p.logger.Log(logging.CatSkippedThird, "%s (%s)", jsURL, decision.Reason)
			return
		}
		body, ok := p.downloadJS(jsURL, host, entryURL, withFramework(frameworkPrimary), withEntry(entryURL))
		if !ok {
			return
		}
		for _, chunkURL := range parser.ExtractChunkRefs(string(body), jsURL) {
			enqueue(chunkURL)
		}
	})

	for _, jsURL := range jsURLs {
		frontier.Enqueue(jsURL)
	}
	frontier.Wait()
}

// downloadOpt allows optional metadata to be threaded through downloadJS
// without an explosion of positional parameters.
type downloadOpt func(*model.AssetMetadata)

func withFramework(name string) downloadOpt {
	return func(m *model.AssetMetadata) { m.Framework = name }
}
func withEntry(entry string) downloadOpt {
	return func(m *model.AssetMetadata) { m.EntryPoint = entry }
}

// downloadJS performs steps "Validate -> Download -> Store Metadata" (and
// triggers source-map retrieval) for one JS URL. It returns the body and
// true on success so callers can further parse it (e.g. for chunk
// discovery); returns ok=false for any rejected/failed asset.
//
// discoveredFrom carries either the HTML entry point (full pipeline /
// download-only mode) or "download-only-mode" as a synthetic marker.
func (p *Pipeline) downloadJS(jsURL, host, discoveredFrom string, opts ...downloadOpt) ([]byte, bool) {
	res := p.fetch.Get(jsURL, "application/javascript,text/javascript,*/*;q=0.5")
	if res.Err != nil {
		p.logger.Log(logging.CatFailed, "%s: %v", jsURL, res.Err)
		return nil, false
	}
	if len(res.RedirectChain) > 0 {
		p.logger.Log(logging.CatRedirects, "%s -> %s (%d hops)", jsURL, res.FinalURL, len(res.RedirectChain))
	}

	if p.cfg.VerifyJS {
		v := validator.Validate(res.ContentType, res.Body)
		if !v.IsJavaScript {
			p.logger.Log(logging.CatSkippedHTML, "%s (%s)", jsURL, v.Reason)
			meta := model.AssetMetadata{
				OriginalURL: jsURL, FinalURL: res.FinalURL, StatusCode: res.StatusCode,
				ContentType: res.ContentType, Size: res.Size, Host: host,
				DiscoveredFrom: discoveredFrom, RedirectChain: res.RedirectChain,
				SourceMapStatus: model.SourceMapNotChecked, FetchedAt: time.Now(),
			}
			_ = p.store.SaveMetadata(host, meta)
			return nil, false
		}
	}

	meta := model.AssetMetadata{
		OriginalURL:    jsURL,
		FinalURL:       res.FinalURL,
		StatusCode:     res.StatusCode,
		ContentType:    res.ContentType,
		Size:           res.Size,
		Host:           host,
		DiscoveredFrom: discoveredFrom,
		RedirectChain:  res.RedirectChain,
		FetchedAt:      time.Now(),
	}
	for _, opt := range opts {
		opt(&meta)
	}

	hash := cacheHash(res.Body)
	var localPath string
	if p.cache != nil {
		lookup, err := p.cache.PutOrRef(res.Body, host, jsURL, "js/"+shortName(jsURL))
		if err != nil {
			p.logger.Log(logging.CatFailed, "cache write %s: %v", jsURL, err)
		} else {
			localPath = lookup.CanonicalPath
			if lookup.Hit {
				meta.IsDuplicate = true
				meta.DuplicateOf = lookup.CanonicalPath
				p.logger.Log(logging.CatDuplicates, "%s duplicate of cached asset %s", jsURL, lookup.CanonicalPath)
			}
		}
	}
	if localPath == "" {
		// caching disabled, or failed: fall back to per-host storage
		if lp, err := p.store.SaveJS(host, jsURL, res.Body); err == nil {
			localPath = lp
		} else {
			p.logger.Log(logging.CatFailed, "write %s: %v", jsURL, err)
		}
	}
	meta.SHA256 = hash
	meta.LocalPath = localPath

	// Source maps: always check, download when available (per spec).
	if p.cfg.DownloadMaps {
		status, mapURL := sourcemap.Resolve(p.fetch, string(res.Body), jsURL, true)
		meta.SourceMapStatus = status
		meta.SourceMapURL = mapURL
		if status == model.SourceMapFound && mapURL != "" {
			if mapPath, ok := p.fetchAndStoreSourceMap(mapURL, jsURL, host); ok {
				meta.SourceMapPath = mapPath
			}
		}
		p.logger.Log(logging.CatSourceMaps, "%s status=%s url=%s", jsURL, status, mapURL)
	} else {
		meta.SourceMapStatus = model.SourceMapNotChecked
	}

	if err := p.store.SaveMetadata(host, meta); err != nil {
		p.logger.Log(logging.CatFailed, "save metadata %s: %v", jsURL, err)
	}

	return res.Body, true
}

func (p *Pipeline) fetchAndStoreSourceMap(mapURL, forJS, host string) (string, bool) {
	res := p.fetch.Get(mapURL, "application/json,*/*;q=0.5")
	if res.Err != nil {
		p.logger.Log(logging.CatFailed, "sourcemap %s: %v", mapURL, res.Err)
		return "", false
	}
	if res.StatusCode != 200 {
		p.logger.Log(logging.CatSourceMaps, "%s status=%d (not downloaded)", mapURL, res.StatusCode)
		return "", false
	}

	var localPath string
	if p.cache != nil {
		lookup, err := p.cache.PutOrRef(res.Body, host, mapURL, "maps/"+shortName(mapURL))
		if err == nil {
			localPath = lookup.CanonicalPath
			if lookup.Hit {
				p.logger.Log(logging.CatDuplicates, "%s duplicate map of %s", mapURL, lookup.CanonicalPath)
			}
		}
	}
	if localPath == "" {
		if lp, err := p.store.SaveMap(host, mapURL, res.Body); err == nil {
			localPath = lp
		} else {
			p.logger.Log(logging.CatFailed, "write map %s: %v", mapURL, err)
			return "", false
		}
	}

	meta := model.AssetMetadata{
		OriginalURL:     mapURL,
		FinalURL:        res.FinalURL,
		StatusCode:      res.StatusCode,
		ContentType:     res.ContentType,
		Size:            res.Size,
		Host:            host,
		DiscoveredFrom:  forJS,
		LocalPath:       localPath,
		SHA256:          cacheHash(res.Body),
		SourceMapStatus: model.SourceMapFound,
		FetchedAt:       time.Now(),
	}
	if err := p.store.SaveMetadata(host, meta); err != nil {
		p.logger.Log(logging.CatFailed, "save map metadata %s: %v", mapURL, err)
	}
	return localPath, true
}

func cacheHash(body []byte) string { return cache.Hash(body) }

func shortName(u string) string {
	if i := strings.LastIndexByte(u, '/'); i >= 0 && i+1 < len(u) {
		return u[i+1:]
	}
	return u
}
