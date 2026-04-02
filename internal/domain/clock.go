package domain

import "time"

// Clock abstracts time so that tests can inject a controllable clock.
type Clock interface {
	Now() time.Time
}

// RealClock returns the actual wall-clock time.
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }

// MockClock is a manually-controlled clock for tests.
type MockClock struct {
	T time.Time
}

func (c *MockClock) Now() time.Time { return c.T }

// Advance moves the mock clock forward by d.
func (c *MockClock) Advance(d time.Duration) { c.T = c.T.Add(d) }
