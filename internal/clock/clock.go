package clock

import "time"

type Clock interface {
	Now() time.Time
	Ticker(time.Duration) (<-chan time.Time, func())
}

type Real struct{}

func (Real) Now() time.Time { return time.Now() }

func (Real) Ticker(interval time.Duration) (<-chan time.Time, func()) {
	ticker := time.NewTicker(interval)
	return ticker.C, ticker.Stop
}
