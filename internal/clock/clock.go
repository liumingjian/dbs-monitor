package clock

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Clock is the seam for every time-dependent decision on the platform: the
// timestamps that get persisted, the durations that get recorded as samples,
// and the tickers that drive the collection and evaluation loops.
//
// Two adapters satisfy it. Real is the production adapter. Manual is the test
// adapter: its clock only moves when a test moves it, and its tickers fire.
type Clock interface {
	Now() time.Time
	// Since returns the duration elapsed since started, which must have come
	// from this same Clock's Now.
	Since(started time.Time) time.Duration
	// Ticker returns a channel that receives at interval, plus a stop
	// function. The channel holds at most one pending tick: a consumer that
	// falls behind sees one tick on resume, not a backlog.
	Ticker(interval time.Duration) (<-chan time.Time, func())
}

type Real struct{}

func (Real) Now() time.Time { return time.Now() }

// Since goes through time.Since so it reads the monotonic clock carried by
// started. A wall-clock step (NTP) cannot distort the result.
func (Real) Since(started time.Time) time.Duration { return time.Since(started) }

func (Real) Ticker(interval time.Duration) (<-chan time.Time, func()) {
	ticker := time.NewTicker(interval)
	return ticker.C, ticker.Stop
}

// Manual is the test adapter. It never advances on its own; Advance is the
// only thing that moves it, and advancing past a ticker's interval fires that
// ticker.
type Manual struct {
	mutex   sync.Mutex
	now     time.Time
	tickers []*manualTicker
}

type manualTicker struct {
	interval time.Duration
	next     time.Time
	ticks    chan time.Time
	stopped  bool
}

func NewManual(now time.Time) *Manual {
	return &Manual{now: now}
}

func (manual *Manual) Now() time.Time {
	manual.mutex.Lock()
	defer manual.mutex.Unlock()
	return manual.now
}

// Set moves the clock to an instant chosen outright. It fires no ticker: use
// Advance when the elapsed interval is what matters.
func (manual *Manual) Set(now time.Time) {
	manual.mutex.Lock()
	defer manual.mutex.Unlock()
	manual.now = now
}

func (manual *Manual) Since(started time.Time) time.Duration {
	return manual.Now().Sub(started)
}

func (manual *Manual) Ticker(interval time.Duration) (<-chan time.Time, func()) {
	if interval <= 0 {
		panic("clock: Manual.Ticker requires a positive interval")
	}
	manual.mutex.Lock()
	defer manual.mutex.Unlock()
	ticker := &manualTicker{
		interval: interval,
		next:     manual.now.Add(interval),
		// Capacity one, matching time.Ticker: a tick that nobody is waiting
		// for is dropped rather than queued.
		ticks: make(chan time.Time, 1),
	}
	manual.tickers = append(manual.tickers, ticker)
	stop := func() {
		manual.mutex.Lock()
		defer manual.mutex.Unlock()
		ticker.stopped = true
	}
	return ticker.ticks, stop
}

// AwaitTicker blocks until at least count tickers exist, or ctx is done. A
// test that drives a loop from the outside uses it as a startup barrier: the
// loop has to be waiting on its ticker before the clock moves, or the tick it
// was supposed to see is one that nobody has subscribed to yet. Nothing this
// waits on is an assertion, so the polling interval carries no meaning.
func (manual *Manual) AwaitTicker(ctx context.Context, count int) error {
	for {
		manual.mutex.Lock()
		created := len(manual.tickers)
		manual.mutex.Unlock()
		if created >= count {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("clock: waited for %d tickers, %d were created: %w", count, created, ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
}

// Advance moves the clock forward and fires every ticker whose interval the
// move crossed. A ticker fires at most once per Advance no matter how many
// intervals were crossed, which is what a consumer of a real time.Ticker sees
// after a stall.
func (manual *Manual) Advance(duration time.Duration) {
	if duration < 0 {
		panic("clock: Manual.Advance requires a non-negative duration")
	}
	manual.mutex.Lock()
	defer manual.mutex.Unlock()
	manual.now = manual.now.Add(duration)
	for _, ticker := range manual.tickers {
		if ticker.stopped || ticker.next.After(manual.now) {
			continue
		}
		for !ticker.next.After(manual.now) {
			ticker.next = ticker.next.Add(ticker.interval)
		}
		select {
		case ticker.ticks <- manual.now:
		default:
		}
	}
}

var (
	_ Clock = Real{}
	_ Clock = (*Manual)(nil)
)
