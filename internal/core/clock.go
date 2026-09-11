package core

import "time"

// Clock supplies the current time in Unix milliseconds. All timestamps in
// trellis are integer milliseconds; see the spec's global constraints.
type Clock interface {
	NowMS() int64
}

// RealClock reads the system clock.
type RealClock struct{}

func (RealClock) NowMS() int64 { return time.Now().UnixMilli() }

// FixedClock returns a constant time, for tests that assert on timestamps.
type FixedClock struct{ MS int64 }

func (c FixedClock) NowMS() int64 { return c.MS }
