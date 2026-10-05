package bench

import (
	"time"
)

const SchemaResource = "m5b-resource/2"
const SchemaThroughput = "m5b-throughput/2"

// ResourceRow is one role's process observation inside a resource smoke.

type WorkerOptions struct {
	RunKind              string
	BaseDir              string
	OutDir               string
	Addr                 string
	Mode                 Mode
	Trials               int
	Timeout              time.Duration
	RunID                string
	ParentRunID          string
	SourceManifestSHA256 string
	TrialsPath           string
	MetricsPath          string
	GatewayEventsPath    string // observed by the CONTROLLER, not the worker
}
