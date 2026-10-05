package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// writeValidationReport persists the validator's judgement. A successful
// measurement is not a successful delivery when this evidence cannot be saved.
// This is production error handling, not a mutable test hook.
func writeValidationReport(outDir string, problems []string) error {
	if problems == nil {
		problems = []string{}
	}
	report := map[string]any{"problems": problems}
	if len(problems) > 0 {
		report["dir"] = outDir
	}
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding validation report: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(outDir, "validation.json"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("opening validation report: %w", err)
	}
	n, writeErr := f.Write(b)
	if writeErr == nil && n != len(b) {
		writeErr = io.ErrShortWrite
	}
	// Always close the file. Preserve both the first failure and cleanup errors.
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return fmt.Errorf("persisting validation report: %w", err)
	}
	return nil
}

func finishValidation(outDir string, problems []string, runCode int) int {
	if len(problems) > 0 {
		fmt.Fprintln(os.Stderr, "bench: the written results FAILED formal validation:")
		for _, problem := range problems {
			fmt.Fprintf(os.Stderr, "  - %s\n", problem)
		}
	}
	if err := writeValidationReport(outDir, problems); err != nil {
		fmt.Fprintf(os.Stderr, "bench: %v\n", err)
		return 5
	}
	if len(problems) > 0 {
		fmt.Printf("bench: results in %s (validation FAILED)\n", outDir)
		if runCode == 0 {
			return 5
		}
		return runCode
	}
	fmt.Printf("bench: results in %s (formally validated)\n", outDir)
	return runCode
}
