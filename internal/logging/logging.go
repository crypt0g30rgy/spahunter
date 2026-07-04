// Package logging provides separate, thread-safe log streams for the
// different categories of event spahunter needs to record.
package logging

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// Category names a log stream.
type Category string

const (
	CatSPA          Category = "spa_detection"
	CatRedirects    Category = "redirects"
	CatSkippedHTML  Category = "skipped_html"
	CatSkippedThird Category = "skipped_third_party"
	CatDuplicates   Category = "duplicate_assets"
	CatFailed       Category = "failed_downloads"
	CatSourceMaps   Category = "source_maps"
	CatRetries      Category = "retries"
	CatGeneral      Category = "general"
)

var allCategories = []Category{
	CatSPA, CatRedirects, CatSkippedHTML, CatSkippedThird,
	CatDuplicates, CatFailed, CatSourceMaps, CatRetries, CatGeneral,
}

// Logger fans events out to per-category log files under <output>/logs.
type Logger struct {
	mu      sync.Mutex
	loggers map[Category]*log.Logger
	files   []*os.File
	verbose bool
}

// New creates a Logger writing files into dir (created if needed).
func New(dir string, verbose bool) (*Logger, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	l := &Logger{
		loggers: make(map[Category]*log.Logger),
		verbose: verbose,
	}
	for _, cat := range allCategories {
		f, err := os.OpenFile(filepath.Join(dir, string(cat)+".log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			l.Close()
			return nil, err
		}
		l.files = append(l.files, f)
		l.loggers[cat] = log.New(f, "", log.LstdFlags|log.Lmicroseconds)
	}
	return l, nil
}

// Log writes a formatted line to the given category's log file, and
// mirrors it to stdout if verbose mode is enabled.
func (l *Logger) Log(cat Category, format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	if lg, ok := l.loggers[cat]; ok {
		lg.Println(msg)
	}
	if l.verbose {
		fmt.Printf("[%s] %s\n", cat, msg)
	}
}

// Close flushes and closes all underlying log files.
func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, f := range l.files {
		_ = f.Close()
	}
}
