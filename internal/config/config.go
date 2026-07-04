// Package config defines spahunter's runtime configuration and CLI flags.
package config

import (
	"flag"
	"fmt"
	"os"
	"time"
)

// Mode selects which stage(s) of the pipeline to run.
type Mode string

const (
	ModeFull      Mode = "full"
	ModeDetectSPA Mode = "detect-spa"
	ModeListJS    Mode = "list-js"
	ModeDownload  Mode = "download-js"
	ModeMapsOnly  Mode = "maps-only"
	ModeResume    Mode = "resume"
)

// Config holds every tunable for a spahunter run.
type Config struct {
	InputFile string
	InputURL  string

	Mode Mode

	Workers      int
	Timeout      time.Duration
	Retries      int
	MaxRedirects int
	NoRedirects  bool

	UserAgentProfile string

	DownloadMaps bool
	MapsOnly     bool
	ListJS       bool
	DetectSPA    bool
	DownloadJS   bool
	Resume       bool

	Output string

	IncludeThirdParty bool
	SkipCommonLibs    bool
	ExcludeRegex      string
	IncludeRegex      string
	VerifyJS          bool
	CacheAssets       bool

	Verbose bool
}

// Parse builds a Config from command-line arguments.
func Parse(args []string) (*Config, error) {
	fs := flag.NewFlagSet("spahunter", flag.ContinueOnError)

	cfg := &Config{}

	fs.StringVar(&cfg.InputFile, "i", "", "input file containing URLs (one per line)")
	fs.StringVar(&cfg.InputURL, "u", "", "single input URL")

	fs.IntVar(&cfg.Workers, "workers", 20, "number of concurrent workers")
	fs.DurationVar(&cfg.Timeout, "timeout", 15*time.Second, "per-request timeout")
	fs.IntVar(&cfg.Retries, "retries", 2, "number of retries per request")
	fs.StringVar(&cfg.UserAgentProfile, "user-agent-profile", "", "pin a specific browser profile (default: rotate all)")

	fs.BoolVar(&cfg.DownloadMaps, "download-maps", true, "download source maps when available")
	fs.BoolVar(&cfg.MapsOnly, "maps-only", false, "only retrieve source maps for already-downloaded JS")
	fs.BoolVar(&cfg.ListJS, "list-js", false, "only extract and list JS URLs, do not download")
	fs.BoolVar(&cfg.DetectSPA, "detect-spa", false, "only run SPA detection")
	fs.BoolVar(&cfg.DownloadJS, "download-js", false, "download-only mode: input file is a list of JS URLs")
	fs.BoolVar(&cfg.Resume, "resume", false, "resume an interrupted run, skipping completed work")

	fs.StringVar(&cfg.Output, "output", "output", "output directory")

	fs.BoolVar(&cfg.IncludeThirdParty, "include-third-party", false, "do not filter out known third-party/analytics assets")
	fs.BoolVar(&cfg.SkipCommonLibs, "skip-common-libraries", true, "skip common CDN-hosted libraries (jquery, bootstrap, etc)")
	fs.StringVar(&cfg.ExcludeRegex, "exclude-regex", "", "additional regex of URLs to exclude")
	fs.StringVar(&cfg.IncludeRegex, "include-regex", "", "if set, only URLs matching this regex are kept")
	fs.BoolVar(&cfg.VerifyJS, "verify-js", true, "validate that downloaded assets are actually JavaScript")
	fs.BoolVar(&cfg.CacheAssets, "cache-assets", false, "enable the global SHA-256 asset cache")

	fs.BoolVar(&cfg.NoRedirects, "no-redirects", false, "disable following redirects")
	fs.IntVar(&cfg.MaxRedirects, "max-redirects", 10, "maximum redirects to follow")

	fs.BoolVar(&cfg.Verbose, "verbose", false, "verbose logging to stdout")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	switch {
	case cfg.DetectSPA:
		cfg.Mode = ModeDetectSPA
	case cfg.ListJS:
		cfg.Mode = ModeListJS
	case cfg.MapsOnly:
		cfg.Mode = ModeMapsOnly
	case cfg.DownloadJS:
		cfg.Mode = ModeDownload
	case cfg.Resume:
		cfg.Mode = ModeResume
	default:
		cfg.Mode = ModeFull
	}

	if cfg.InputFile == "" && cfg.InputURL == "" {
		return nil, fmt.Errorf("either -i <file> or -u <url> is required")
	}
	if cfg.InputFile != "" {
		if _, err := os.Stat(cfg.InputFile); err != nil {
			return nil, fmt.Errorf("input file: %w", err)
		}
	}

	return cfg, nil
}
