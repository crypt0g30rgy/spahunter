// Package fetcher implements the HTTP layer: connection pooling,
// keep-alive, gzip handling, retries, timeouts, redirect-chain capture
// and rotating browser profiles.
package fetcher

import (
	"compress/gzip"
	"crypto/tls"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/crypt0g30rgy/spahunter/internal/browser"
	"github.com/crypt0g30rgy/spahunter/internal/model"
)

// Fetcher performs HTTP requests with retry/backoff, honours a
// configurable redirect policy while still recording the chain, and
// rotates full browser profiles per request.
type Fetcher struct {
	client        *http.Client
	timeout       time.Duration
	retries       int
	maxRedirects  int
	noRedirects   bool
	pinnedProfile *browser.Profile
	rrIndex       uint64 // round-robin counter for profile rotation
}

// Options configures a new Fetcher.
type Options struct {
	Timeout       time.Duration
	Retries       int
	MaxRedirects  int
	NoRedirects   bool
	PinnedProfile string // name of a browser.Profile to pin, or "" to rotate
}

// New builds a Fetcher with connection pooling and HTTP/2 support (HTTP/2
// is negotiated automatically by net/http's Transport over TLS/ALPN).
func New(opts Options) (*Fetcher, error) {
	transport := &http.Transport{
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 20,
		MaxConnsPerHost:     40,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		ForceAttemptHTTP2:   true,
		TLSClientConfig:     &tls.Config{}, // default verification; ok to override for internal testing
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   opts.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if opts.NoRedirects {
				return http.ErrUseLastResponse
			}
			max := opts.MaxRedirects
			if max <= 0 {
				max = 10
			}
			if len(via) >= max {
				return fmt.Errorf("stopped after %d redirects", max)
			}
			return nil
		},
	}

	f := &Fetcher{
		client:       client,
		timeout:      opts.Timeout,
		retries:      opts.Retries,
		maxRedirects: opts.MaxRedirects,
		noRedirects:  opts.NoRedirects,
	}
	if opts.PinnedProfile != "" {
		if p, ok := browser.ByName(opts.PinnedProfile); ok {
			f.pinnedProfile = &p
		} else {
			return nil, fmt.Errorf("unknown browser profile: %s", opts.PinnedProfile)
		}
	}
	return f, nil
}

func (f *Fetcher) nextProfile() browser.Profile {
	if f.pinnedProfile != nil {
		return *f.pinnedProfile
	}
	// simple round robin keeps header sets internally consistent per
	// request while still exercising every profile across a run
	i := atomic.AddUint64(&f.rrIndex, 1)
	return browser.Profiles[int(i)%len(browser.Profiles)]
}

// randJitter returns a small jittered backoff duration for retry N.
func randJitter(attempt int) time.Duration {
	base := time.Duration(attempt*attempt) * 200 * time.Millisecond
	jitter := time.Duration(rand.Intn(150)) * time.Millisecond
	return base + jitter
}

// Get performs a GET request, following redirects per policy, recording
// the redirect chain, retrying on transient failures, and transparently
// decompressing gzip bodies.
func (f *Fetcher) Get(rawURL string, acceptOverride string) model.FetchResult {
	var lastErr error
	var chain []model.RedirectHop
	profile := f.nextProfile()

	start := time.Now()
	for attempt := 0; attempt <= f.retries; attempt++ {
		if attempt > 0 {
			time.Sleep(randJitter(attempt))
		}

		req, err := http.NewRequest(http.MethodGet, rawURL, nil)
		if err != nil {
			return model.FetchResult{OriginalURL: rawURL, Err: err}
		}
		profile.Apply(req, acceptOverride != "")
		if acceptOverride != "" {
			req.Header.Set("Accept", acceptOverride)
		}

		chain = nil
		prevURL := rawURL
		client := *f.client
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			chain = append(chain, model.RedirectHop{
				From:       prevURL,
				To:         req.URL.String(),
				StatusCode: 0, // filled from response when available
			})
			prevURL = req.URL.String()
			if f.noRedirects {
				return http.ErrUseLastResponse
			}
			max := f.maxRedirects
			if max <= 0 {
				max = 10
			}
			if len(via) >= max {
				return fmt.Errorf("stopped after %d redirects", max)
			}
			return nil
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		body, readErr := readBody(resp)
		result := model.FetchResult{
			OriginalURL:   rawURL,
			FinalURL:      resp.Request.URL.String(),
			StatusCode:    resp.StatusCode,
			ContentType:   resp.Header.Get("Content-Type"),
			Body:          body,
			Size:          int64(len(body)),
			RedirectChain: chain,
			Profile:       profile.Name,
			Duration:      time.Since(start),
		}
		resp.Body.Close()

		if readErr != nil {
			lastErr = readErr
			continue
		}

		// retry on 5xx, but not on 4xx (those are meaningful responses)
		if resp.StatusCode >= 500 && attempt < f.retries {
			lastErr = fmt.Errorf("server error: %d", resp.StatusCode)
			continue
		}

		return result
	}

	return model.FetchResult{OriginalURL: rawURL, Err: lastErr, Profile: profile.Name}
}

// Head performs a HEAD request, useful for cheaply probing source-map
// existence without downloading a full body.
func (f *Fetcher) Head(rawURL string) model.FetchResult {
	profile := f.nextProfile()
	req, err := http.NewRequest(http.MethodHead, rawURL, nil)
	if err != nil {
		return model.FetchResult{OriginalURL: rawURL, Err: err}
	}
	profile.Apply(req, false)

	resp, err := f.client.Do(req)
	if err != nil {
		return model.FetchResult{OriginalURL: rawURL, Err: err, Profile: profile.Name}
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))

	return model.FetchResult{
		OriginalURL: rawURL,
		FinalURL:    resp.Request.URL.String(),
		StatusCode:  resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		Profile:     profile.Name,
	}
}

func readBody(resp *http.Response) ([]byte, error) {
	var reader io.Reader = resp.Body
	enc := strings.ToLower(resp.Header.Get("Content-Encoding"))
	switch {
	case strings.Contains(enc, "gzip"):
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			// Some servers mislabel; fall back to raw body.
			break
		}
		defer gz.Close()
		reader = gz
	case strings.Contains(enc, "br"):
		reader = brotli.NewReader(resp.Body)
	}
	return io.ReadAll(io.LimitReader(reader, 200<<20)) // 200MB safety cap
}

// ParseHost is a tiny helper re-exported for convenience in callers that
// only have the fetcher imported.
func ParseHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
