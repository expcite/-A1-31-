package demo

import (
	"os"
	"path/filepath"

	"example.com/edgehybrid/internal/evidence"
)

type ActiveInfo struct {
	RunID        string `json:"run_id"`
	PID          int    `json:"pid"`
	URL          string `json:"url"`
	LogDirectory string `json:"log_directory"`
	Status       string `json:"status"`
}

// Only an instance that holds the named lock calls this. A RUNNING marker
// with the lock now available denotes a previous abnormal/unrecorded exit.
// It is diagnostic metadata, never a data source for this run's counters.
func PreviousExitUnrecorded(base string) bool {
	var info ActiveInfo
	return evidence.Read(filepath.Join(base, "active.json"), &info) == nil && info.Status == "RUNNING"
}
func WriteActive(base string, c *Controller, stopped bool) error {
	status := "RUNNING"
	if stopped {
		status = "STOPPED"
	}
	return evidence.WriteJSON(filepath.Join(base, "active.json"), ActiveInfo{RunID: c.RunID, PID: os.Getpid(), URL: c.Server.URL, LogDirectory: c.LogDir, Status: status})
}
