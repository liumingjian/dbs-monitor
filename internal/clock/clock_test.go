package clock

import (
	"context"
	"testing"
	"time"
)

var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestManualOnlyMovesWhenAdvanced(t *testing.T) {
	manual := NewManual(epoch)
	if got := manual.Now(); !got.Equal(epoch) {
		t.Fatalf("Now() = %s, want %s", got, epoch)
	}
	manual.Advance(90 * time.Second)
	if got := manual.Now(); !got.Equal(epoch.Add(90 * time.Second)) {
		t.Fatalf("Now() after advance = %s, want %s", got, epoch.Add(90*time.Second))
	}
}

func TestManualSinceMeasuresAgainstItsOwnNow(t *testing.T) {
	manual := NewManual(epoch)
	started := manual.Now()
	manual.Advance(1500 * time.Millisecond)
	if got := manual.Since(started); got != 1500*time.Millisecond {
		t.Fatalf("Since() = %s, want 1.5s", got)
	}
}

func TestManualSetMovesToAnInstantOutright(t *testing.T) {
	manual := NewManual(epoch)
	target := epoch.Add(-2 * time.Hour)
	manual.Set(target)
	if got := manual.Now(); !got.Equal(target) {
		t.Fatalf("Now() after Set = %s, want %s", got, target)
	}
}

func TestManualTickerFires(t *testing.T) {
	manual := NewManual(epoch)
	ticks, stop := manual.Ticker(5 * time.Second)
	defer stop()

	manual.Advance(4 * time.Second)
	select {
	case at := <-ticks:
		t.Fatalf("ticker fired at %s before its interval elapsed", at)
	default:
	}

	manual.Advance(time.Second)
	select {
	case at := <-ticks:
		if !at.Equal(epoch.Add(5 * time.Second)) {
			t.Fatalf("tick at %s, want %s", at, epoch.Add(5*time.Second))
		}
	default:
		t.Fatal("ticker did not fire after its interval elapsed")
	}
}

// A consumer that stalls past many intervals sees one tick on resume, not a
// backlog. This is what time.Ticker does, and the collection scheduler's
// backpressure coalescing depends on it.
func TestManualTickerCoalescesTicksAcrossAStall(t *testing.T) {
	manual := NewManual(epoch)
	ticks, stop := manual.Ticker(time.Second)
	defer stop()

	manual.Advance(10 * time.Minute)

	select {
	case at := <-ticks:
		if !at.Equal(epoch.Add(10 * time.Minute)) {
			t.Fatalf("tick at %s, want %s", at, epoch.Add(10*time.Minute))
		}
	default:
		t.Fatal("ticker did not fire across the stall")
	}
	select {
	case at := <-ticks:
		t.Fatalf("ticker queued a second tick at %s; ticks must coalesce", at)
	default:
	}
}

func TestManualTickerStops(t *testing.T) {
	manual := NewManual(epoch)
	ticks, stop := manual.Ticker(time.Second)
	stop()
	manual.Advance(time.Minute)
	select {
	case at := <-ticks:
		t.Fatalf("stopped ticker fired at %s", at)
	default:
	}
}

func TestManualTickersAreIndependent(t *testing.T) {
	manual := NewManual(epoch)
	fast, stopFast := manual.Ticker(time.Second)
	slow, stopSlow := manual.Ticker(time.Minute)
	defer stopFast()
	defer stopSlow()

	manual.Advance(2 * time.Second)
	select {
	case <-fast:
	default:
		t.Fatal("one-second ticker did not fire after two seconds")
	}
	select {
	case at := <-slow:
		t.Fatalf("one-minute ticker fired at %s after two seconds", at)
	default:
	}
}

func TestRealSinceIsMonotonic(t *testing.T) {
	var real Real
	started := real.Now()
	if elapsed := real.Since(started); elapsed < 0 {
		t.Fatalf("Since() = %s, want a non-negative duration", elapsed)
	}
}

func TestManualAwaitTickerBarrier(t *testing.T) {
	manual := NewManual(epoch)
	ready := make(chan struct{})
	go func() {
		defer close(ready)
		_, stop := manual.Ticker(time.Second)
		defer stop()
		<-ready
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := manual.AwaitTicker(ctx, 1); err != nil {
		t.Fatalf("AwaitTicker: %v", err)
	}
}

func TestManualAwaitTickerRespectsContext(t *testing.T) {
	manual := NewManual(epoch)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := manual.AwaitTicker(ctx, 1); err == nil {
		t.Fatal("AwaitTicker returned nil with no ticker created")
	}
}
