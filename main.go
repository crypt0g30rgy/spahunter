// Command spahunter is a high-performance SPA JavaScript asset
// acquisition tool for authorized security testing and bug bounty
// reconnaissance. It identifies SPA entry points, enumerates
// application-owned JS bundles, recursively discovers lazy-loaded
// chunks, and optionally retrieves source maps, storing everything for
// later manual analysis.
//
// It does not fuzz endpoints, brute-force directories, enumerate APIs,
// detect vulnerabilities, or perform any active scanning.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/crypt0g30rgy/spahunter/internal/config"
	"github.com/crypt0g30rgy/spahunter/internal/pipeline"
)

func main() {
	if err := run(); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Parse(os.Args[1:])
	if err != nil {
		return err
	}

	urls, err := pipeline.LoadURLs(cfg)
	if err != nil {
		return fmt.Errorf("loading input: %w", err)
	}
	if len(urls) == 0 {
		return fmt.Errorf("no valid URL(s) to process")
	}

	fmt.Fprintf(os.Stderr, "spahunter: mode=%s targets=%d workers=%d output=%s\n",
		cfg.Mode, len(urls), cfg.Workers, cfg.Output)

	p, err := pipeline.New(cfg)
	if err != nil {
		return fmt.Errorf("initializing pipeline: %w", err)
	}
	defer p.Close()

	start := time.Now()
	if err := p.Run(urls); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "spahunter: done in %s\n", time.Since(start).Round(time.Millisecond))
	return nil
}
