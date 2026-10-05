package console

// Set from the public measurement-source manifest during reproducible build.
// Development builds carry an explicit zero identifier, never an old exe hash.
var SourceHash = "0000000000000000000000000000000000000000000000000000000000000000"
var Version = "interactive-v1.2-dev"
var BenchSHA, PBenchSHA, ScenarioSHA string
