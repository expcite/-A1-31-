package live

import "time"

// Schedule is a clock-independent timer decision. Only the device actor owns it.
// Its returned deadline drives an actual time.Timer in device.go.
type Schedule struct {
	Interval time.Duration
	Enabled  bool
	Next     time.Time
}

func (s *Schedule) Reset(now time.Time, ms uint32, enabled bool) {
	s.Interval = time.Duration(ms) * time.Millisecond
	s.Enabled = enabled
	s.Next = now.Add(s.Interval)
}
func (s *Schedule) Due(now time.Time) bool { return s.Enabled && !now.Before(s.Next) }
func (s *Schedule) Advance(now time.Time)  { s.Next = now.Add(s.Interval) }
func resetTimer(t *time.Timer, s Schedule) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	if s.Enabled {
		t.Reset(time.Until(s.Next))
	}
}
