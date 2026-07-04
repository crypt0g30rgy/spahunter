# spahunter

A high-performance Go tool for **authorized security testing and bug bounty
reconnaissance**, focused exclusively on **asset acquisition** for Single
Page Applications: identifying SPA entry points, enumerating
application-owned JavaScript bundles, recursively discovering lazy-loaded
chunks, and optionally retrieving source maps — all for later **manual**
review.

**Non-goals**: spahunter does not fuzz endpoints, brute-force directories,
enumerate APIs, detect vulnerabilities, or perform any active scanning.
Only use it against targets you are authorized to test.

## Build

```
go build -o spahunter ./cmd/spahunter
```

Requires Go 1.21+. No external dependencies — standard library only (see
"Known limitations" below for why).

## Usage

```
# Full pipeline
./spahunter -i urls.txt

# SPA detection only
./spahunter -i urls.txt -detect-spa

# List JS URLs only (no download)
./spahunter -i urls.txt -list-js

# Download-only: input is already a list of JS URLs
./spahunter -i js-urls.txt -download-js

# Source maps only, for previously-downloaded JS
./spahunter -i urls.txt -maps-only

# Resume an interrupted full run
./spahunter -i urls.txt -resume
```

Single URL instead of a file: `-u https://example.com`

### Key flags

| Flag | Default | Description |
|---|---|---|
| `-workers` | 20 | concurrent workers |
| `-timeout` | 15s | per-request timeout |
| `-retries` | 2 | retries per request |
| `-user-agent-profile` | (rotate) | pin one of: chrome-windows, chrome-linux, firefox, safari, edge, chrome-android, safari-ios |
| `-download-maps` | true | fetch source maps when found |
| `-output` | ./output | output directory |
| `-include-third-party` | false | disable the built-in third-party/CDN filter |
| `-skip-common-libraries` | true | skip common vendor path hints (gtag.js, analytics.js, ...) |
| `-exclude-regex` / `-include-regex` | | additional URL filters |
| `-verify-js` | true | reject HTML masquerading as JS (SPA fallback pages) |
| `-cache-assets` | false | global SHA-256 dedup cache across hosts |
| `-no-redirects` / `-max-redirects` | | redirect policy |
| `-verbose` | false | mirror logs to stdout |

## Output layout

```
output/
  <host>/
    html/        # (reserved for saved entry-point HTML, if enabled)
    js/           # per-host JS (only used when the global cache is off,
                   # or as a fallback if a cache write fails)
    maps/         # per-host source maps (same fallback rule as above)
    metadata/
      assets.jsonl      # one JSON object per asset (see model.AssetMetadata)
      spa_result.json   # SPA detection result for this host
  cache/
    sha256/<xx>/<hash>.js   # canonical, content-addressed copies
    asset-index.json       # hash -> canonical path + every host/URL that referenced it
  _logs/
    spa_detection.log, redirects.log, skipped_html.log,
    skipped_third_party.log, duplicate_assets.log,
    failed_downloads.log, source_maps.log, retries.log
```

When the global asset cache is enabled, identical JavaScript
served from multiple hosts/subdomains/environments is written to disk
**once**; every other sighting is recorded as a reference in
`cache/asset-index.json` and flagged `is_duplicate` in that host's
`assets.jsonl`.

## Project layout

```
cmd/spahunter/          CLI entry point
internal/
  model/                shared structs (no cross-package cycles)
  urlutil/              normalization, resolution, dedup
  browser/              rotating browser header profiles
  fetcher/              HTTP layer: pooling, retries, redirects, gzip
  framework/            React/Next/Angular/Vue/... detection
  spa/                  SPA-vs-traditional classification
  parser/               HTML JS extraction + lazy-chunk discovery
  filters/               third-party/CDN allow/deny logic
  validator/             JS-vs-HTML response validation
  sourcemap/             inline + conventional-path source map resolution
  cache/                 global SHA-256 content-addressed cache
  queue/                 worker pool + self-feeding dedup frontier
  storage/               on-disk output layout
  logging/               per-category log files
  config/                CLI flags
  pipeline/              orchestrates all of the above per CLI mode
```

## Known limitations

- **Webpack numeric chunk IDs.** `__webpack_require__.e(id)` calls that
  reference a chunk purely by numeric ID (resolved at runtime via an
  internal chunk-id-to-filename map) can't be resolved without executing
  the bundle. spahunter instead pattern-matches literal, content-hashed
  filenames that appear as string constants near the runtime — which is
  how most production webpack builds actually emit chunk URLs — but a
  build that only ever references chunks by ID plus a separately-fetched
  manifest may need that manifest parsed as a follow-up enhancement.
