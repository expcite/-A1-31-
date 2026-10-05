package console

import (
	"example.com/edgehybrid/internal/evidence"
	"os"
	"path/filepath"
)

type ActiveMarker struct {
	URL    string `json:"url"`
	Run    string `json:"run_id"`
	PID    int    `json:"pid"`
	Status string `json:"status"`
}

func WriteActiveMarker(base, url, run, status string) error {
	if err := os.MkdirAll(base, 0700); err != nil {
		return err
	}
	return evidence.WriteJSONAtomic(filepath.Join(base, "active.json"), ActiveMarker{url, run, os.Getpid(), status})
}
