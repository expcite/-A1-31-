package console

import (
	"example.com/edgehybrid/internal/live"
	"sync"
	"time"
)

// Preserve the exact public event window returned to an authorized caller.
// Ring audit writes must not invalidate the response they just produced.
// No file reads, credential storage or unbounded all-history retention.
const exportSnapshotLimit = 16
const exportSnapshotTTL = 2 * time.Minute

type exportSnapshot struct {
	run               string
	from, to, dropped uint64
	events            []live.Event
	until             time.Time
}
type snapshotStore struct {
	mu      sync.Mutex
	entries []exportSnapshot
}

func (s *Server) rememberSnapshot(v live.Snapshot) live.Snapshot {
	s.snapshots.mu.Lock()
	defer s.snapshots.mu.Unlock()
	now := time.Now()
	kept := s.snapshots.entries[:0]
	for _, p := range s.snapshots.entries {
		if now.Before(p.until) && p.run == v.Run {
			kept = append(kept, p)
		}
	}
	s.snapshots.entries = kept
	from := uint64(0)
	if len(v.Events) > 0 {
		from = v.Events[0].ID
	}
	for _, p := range kept {
		if p.from == from && p.to == v.Cursor {
			return v
		}
	}
	if len(kept) >= exportSnapshotLimit {
		s.snapshots.entries = append([]exportSnapshot{}, kept[1:]...)
	}
	s.snapshots.entries = append(s.snapshots.entries, exportSnapshot{v.Run, from, v.Cursor, v.Dropped, append([]live.Event{}, v.Events...), now.Add(exportSnapshotTTL)})
	return v
}
func (s *Server) retainedSnapshot(q exportSelection) (live.Snapshot, bool) {
	s.snapshots.mu.Lock()
	defer s.snapshots.mu.Unlock()
	now := time.Now()
	for _, p := range s.snapshots.entries {
		if p.run == q.Run && p.from == q.From && p.to == q.To && now.Before(p.until) {
			return live.Snapshot{Run: p.run, Cursor: p.to, Dropped: p.dropped, Events: append([]live.Event{}, p.events...)}, true
		}
	}
	return live.Snapshot{}, false
}
