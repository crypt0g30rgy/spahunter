// Package filters decides which discovered JavaScript URLs are
// "application-owned" versus common third-party/analytics/CDN noise.
package filters

import (
	"regexp"
	"strings"

	"github.com/crypt0g30rgy/spahunter/internal/urlutil"
)

// defaultThirdPartyHosts is a curated list of common analytics, RUM,
// consent, CDN and vendor SDK hosts that are not part of the target
// application's own bundle set.
var defaultThirdPartyHosts = []string{
	// Cloudflare
	"challenges.cloudflare.com", "static.cloudflareinsights.com", "cdnjs.cloudflare.com",
	// Dynatrace
	"dynatrace.com", "dtCDN",
	// Google Analytics / Tag Manager / reCAPTCHA / fonts / APIs
	"www.google-analytics.com", "www.googletagmanager.com", "googletagmanager.com",
	"www.recaptcha.net", "recaptcha.net", "www.gstatic.com", "fonts.googleapis.com",
	"fonts.gstatic.com", "googleapis.com", "google.com/recaptcha",
	// Hotjar / FullStory / New Relic / Datadog / Sentry / Rollbar
	"static.hotjar.com", "script.hotjar.com", "fullstory.com", "js-agent.newrelic.com",
	"newrelic.com", "datadoghq-browser-agent.com", "datadoghq.com", "browser.sentry-cdn.com",
	"sentry.io", "cdn.ravenjs.com", "cdn.rollbar.com", "rollbar.com",
	// Intercom / Segment / OneTrust / Cookiebot / Optimizely / Adobe
	"widget.intercom.io", "js.intercomcdn.com", "cdn.segment.com", "cdn.cookielaw.org",
	"consent.cookiebot.com", "cdn.optimizely.com", "assets.adobedtm.com",
	// Facebook / LinkedIn / Microsoft Clarity / Bing
	"connect.facebook.net", "snap.licdn.com", "px.ads.linkedin.com",
	"www.clarity.ms", "clarity.ms", "bat.bing.com",
	// hCaptcha
	"hcaptcha.com", "js.hcaptcha.com",
	// Stripe / PayPal
	"js.stripe.com", "www.paypal.com/sdk", "www.paypalobjects.com",
	// Common CDNs for third-party libraries
	"cdn.jsdelivr.net", "unpkg.com", "ajax.googleapis.com", "maxcdn.bootstrapcdn.com",
	"stackpath.bootstrapcdn.com", "code.jquery.com",
}

// defaultThirdPartyPathHints catches vendor scripts served from a
// first-party-looking host (e.g. /cdn-cgi/, /gtag/js).
var defaultThirdPartyPathHints = []string{
	"/cdn-cgi/", "gtag/js", "gtm.js", "analytics.js", "fbevents.js",
	"hotjar-", "clarity.js", "intercom.js",
}

// Filter applies the include/exclude decision for a discovered JS URL.
type Filter struct {
	includeThirdParty bool
	skipCommonLibs    bool
	blacklistHosts    map[string]struct{}
	whitelistHosts    map[string]struct{} // if non-empty, only these hosts are allowed
	excludeRegex      *regexp.Regexp
	includeRegex      *regexp.Regexp
}

// Config configures a new Filter.
type Config struct {
	IncludeThirdParty bool
	SkipCommonLibs    bool
	BlacklistHosts    []string
	WhitelistHosts    []string
	ExcludeRegex      string
	IncludeRegex      string
}

// New builds a Filter from Config, compiling any provided regexes.
func New(cfg Config) (*Filter, error) {
	f := &Filter{
		includeThirdParty: cfg.IncludeThirdParty,
		skipCommonLibs:    cfg.SkipCommonLibs,
		blacklistHosts:    toSet(cfg.BlacklistHosts),
		whitelistHosts:    toSet(cfg.WhitelistHosts),
	}
	if cfg.ExcludeRegex != "" {
		re, err := regexp.Compile(cfg.ExcludeRegex)
		if err != nil {
			return nil, err
		}
		f.excludeRegex = re
	}
	if cfg.IncludeRegex != "" {
		re, err := regexp.Compile(cfg.IncludeRegex)
		if err != nil {
			return nil, err
		}
		f.includeRegex = re
	}
	return f, nil
}

func toSet(items []string) map[string]struct{} {
	s := make(map[string]struct{}, len(items))
	for _, it := range items {
		s[strings.ToLower(it)] = struct{}{}
	}
	return s
}

// Decision explains why a URL was allowed or rejected.
type Decision struct {
	Allow  bool
	Reason string
}

// Allow decides whether a discovered JS URL should be downloaded.
func (f *Filter) Allow(rawURL string) Decision {
	host := strings.ToLower(urlutil.Host(rawURL))
	lower := strings.ToLower(rawURL)

	if len(f.whitelistHosts) > 0 {
		if _, ok := f.whitelistHosts[host]; !ok {
			return Decision{false, "host not in whitelist"}
		}
	}
	if _, ok := f.blacklistHosts[host]; ok {
		return Decision{false, "host in blacklist"}
	}

	if f.includeRegex != nil && !f.includeRegex.MatchString(rawURL) {
		return Decision{false, "did not match include-regex"}
	}
	if f.excludeRegex != nil && f.excludeRegex.MatchString(rawURL) {
		return Decision{false, "matched exclude-regex"}
	}

	if !f.includeThirdParty {
		for _, h := range defaultThirdPartyHosts {
			if strings.Contains(host, strings.ToLower(h)) || strings.Contains(lower, strings.ToLower(h)) {
				return Decision{false, "known third-party host: " + h}
			}
		}
	}

	if f.skipCommonLibs {
		for _, hint := range defaultThirdPartyPathHints {
			if strings.Contains(lower, hint) {
				return Decision{false, "common library path hint: " + hint}
			}
		}
	}

	return Decision{true, "application asset"}
}
