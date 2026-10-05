// Package scenarios contains only fixed, bounded local commands, not a general
// executor. M6 faults remain in the isolated test binary, never production.
package scenarios

import (
	"errors"
	"path/filepath"
)

type Command struct {
	Binary      string
	Args        []string
	Description string
}

func Resolve(kind, dir, out, run, source string) (Command, error) {
	switch kind {
	case "m6_rules":
		return Command{filepath.Join(dir, "scenario.exe"), []string{"-test.run=^TestM6Structured$", "-test.v", "-test.timeout=30s"}, "正常M6有界真实规则测试"}, nil
	case "bench_T", "bench_H":
		mode := "traditional"
		if kind == "bench_H" {
			mode = "hybrid"
		}
		return Command{filepath.Join(dir, "bench.exe"), []string{"-mode", mode, "-run-kind", "smoke", "-trials", "3", "-timeout", "5s", "-out", out, "-run-id", run, "-source-manifest-sha256", source}, "3次独立真实握手smoke，完整CLI结果验收器"}, nil
	case "pbench":
		return Command{filepath.Join(dir, "pbench.exe"), []string{"-run-kind", "smoke", "-trials", "3", "-timeout", "5s", "-out", filepath.Join(out, "results"), "-certs-dir", filepath.Join(out, "private_certs"), "-run-id", run, "-source-manifest-sha256", source}, "3次公开TLS混合对照smoke；非Suite3"}, nil
	default:
		return Command{}, errors.New("只支持固定m6_rules/bench_T/bench_H/pbench，无任意目标或命令")
	}
}
