// Package queue implements a simple, efficient worker pool plus a
// dedup-aware frontier for recursive discovery (e.g. lazy chunk crawling)
// where the total item count is not known up front.
package queue

import "sync"

// Pool runs a fixed number of worker goroutines over tasks submitted via
// Submit. Call Wait after all Submit calls are done (or use Frontier for
// self-feeding recursive workloads instead).
type Pool struct {
	tasks   chan func()
	wg      sync.WaitGroup
	workers int
}

// NewPool starts n worker goroutines.
func NewPool(n int) *Pool {
	if n <= 0 {
		n = 1
	}
	p := &Pool{tasks: make(chan func(), n*4), workers: n}
	for i := 0; i < n; i++ {
		go p.worker()
	}
	return p
}

func (p *Pool) worker() {
	for t := range p.tasks {
		t()
		p.wg.Done()
	}
}

// Submit enqueues a task. Blocks if the internal buffer is full,
// providing natural backpressure.
func (p *Pool) Submit(task func()) {
	p.wg.Add(1)
	p.tasks <- task
}

// Wait blocks until all submitted tasks have completed.
func (p *Pool) Wait() {
	p.wg.Wait()
}

// Close shuts down worker goroutines. Call after Wait.
func (p *Pool) Close() {
	close(p.tasks)
}

// Frontier is a dedup-aware, self-feeding work queue for recursive
// discovery problems like lazy-chunk crawling, where processing one item
// may enqueue more items and the total is unknown ahead of time.
//
// Concurrency is bounded with a semaphore rather than a fixed-size
// channel-backed pool: a self-feeding workload where in-flight items
// enqueue further items can deadlock a bounded task channel once workers
// fill the buffer and then block trying to submit their own children.
// A semaphore plus one goroutine per item (gated by the semaphore) avoids
// that: a goroutine that can't acquire a slot yet simply waits, it never
// blocks a slot that's already running.
type Frontier struct {
	mu      sync.Mutex
	seen    map[string]struct{}
	pending sync.WaitGroup
	sem     chan struct{}
	handler func(item string, enqueue func(string))
}

// NewFrontier creates a self-feeding frontier with at most n items
// processed concurrently.
func NewFrontier(n int, handler func(item string, enqueue func(string))) *Frontier {
	if n <= 0 {
		n = 1
	}
	return &Frontier{
		seen:    make(map[string]struct{}),
		sem:     make(chan struct{}, n),
		handler: handler,
	}
}

// Enqueue adds an item if it hasn't been seen before. Safe for concurrent
// use, including from within the handler itself (recursive discovery).
func (fr *Frontier) Enqueue(item string) {
	fr.mu.Lock()
	if _, ok := fr.seen[item]; ok {
		fr.mu.Unlock()
		return
	}
	fr.seen[item] = struct{}{}
	fr.mu.Unlock()

	fr.pending.Add(1)
	go func() {
		defer fr.pending.Done()
		fr.sem <- struct{}{}
		defer func() { <-fr.sem }()
		fr.handler(item, fr.Enqueue)
	}()
}

// Wait blocks until the frontier has fully drained (no in-flight or
// pending items remain).
func (fr *Frontier) Wait() {
	fr.pending.Wait()
}
