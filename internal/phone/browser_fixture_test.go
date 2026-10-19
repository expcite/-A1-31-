package phone_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"example.com/edgehybrid/internal/console"
)

// Explicit software-only UI fixture; never built into a shipped executable.
func TestPhoneBrowserFixture(t *testing.T) {
	path := os.Getenv("EH_PHONE_BROWSER_FIXTURE")
	if path == "" {
		t.Skip("explicit local browser fixture only")
	}
	m, _ := setup(t, func(context.Context) (float64, error) { return 33, nil }, nil)
	s, e := console.Start(m, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	b, _ := json.Marshal(map[string]string{"url": s.URL, "code": s.LocalCode(), "scope": "SOFTWARE_TEST_ONLY"})
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
	deadline := time.NewTimer(90 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("browser fixture timeout")
		case <-tick.C:
			if _, e = os.Stat(path + ".stop"); e == nil {
				return
			}
		}
	}
}
