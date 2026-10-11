package evidence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func evxTransaction() Transaction {
	return Transaction{RunID: "evx-synthetic", PeerID: "D001", SessionID: "fixture-session", SampleID: 1, OperationID: "set-interval-D001-1", PlaintextBytes: 42}
}
func evxEventLine(t *testing.T, tx Transaction, outcome string) []byte {
	t.Helper()
	b, err := json.Marshal(EventOf(tx, outcome))
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}
func TestEVX05BusinessCorrelationAndInputBounds(t *testing.T) {
	tx := evxTransaction()
	var buf bytes.Buffer
	buf.WriteString("\n \t\n")
	for _, outcome := range []string{"telemetry-accepted", "control-sent", "control-confirmed"} {
		buf.Write(evxEventLine(t, tx, outcome))
	}
	index, err := ParseEvents(buf.Bytes(), tx.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(index) != 1 || len(index[tx.Key()]) != 3 {
		t.Fatal("lost facts", index)
	}
	if got := index[tx.Key()]["telemetry-accepted"]; got.PlaintextBytes != 42 || got.Key() != tx.Key() {
		t.Fatal(got)
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")
	if err = WriteBytes(p, buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadEvents(p, tx.RunID)
	if err != nil || !reflect.DeepEqual(index, loaded) {
		t.Fatal("load", err)
	}
	_, err = LoadEvents(filepath.Join(dir, "absent"), tx.RunID)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		mut  func(map[string]any)
		part string
	}{
		{"foreign_run", func(v map[string]any) { v["run_id"] = "old" }, "event run mismatch"},
		{"missing_run", func(v map[string]any) { delete(v, "run_id") }, "event run mismatch"},
		{"missing_stage", func(v map[string]any) { delete(v, "stage") }, ""},
		{"missing_outcome", func(v map[string]any) { delete(v, "outcome") }, ""},
		{"empty_peer", func(v map[string]any) { v["peer_id"] = "" }, "invalid business correlation"},
		{"missing_sid", func(v map[string]any) { delete(v, "session_id") }, "event session_id"},
		{"zero_sample", func(v map[string]any) { v["sample_id"] = 0 }, "invalid business correlation"},
		{"negative_sample", func(v map[string]any) { v["sample_id"] = -1 }, "event sample_id"},
		{"wrong_sample_type", func(v map[string]any) { v["sample_id"] = "1" }, "event sample_id"},
		{"bad_operation", func(v map[string]any) { v["operation_id"] = "other" }, "invalid business correlation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := evxEventLine(t, tx, "control-confirmed")
			var v map[string]any
			if err := json.Unmarshal(b, &v); err != nil {
				t.Fatal(err)
			}
			tc.mut(v)
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			_, err = ParseEvents(b, tx.RunID)
			evxRequireError(t, err, tc.part)
		})
	}
	t.Run("telemetry_missing_length", func(t *testing.T) {
		bad := tx
		bad.PlaintextBytes = 0
		_, err := ParseEvents(evxEventLine(t, bad, "telemetry-accepted"), tx.RunID)
		evxRequireError(t, err, "accepted event missing plaintext length")
	})
	t.Run("telemetry_operation_derived", func(t *testing.T) {
		bad := tx
		bad.OperationID = ""
		got, err := ParseEvents(evxEventLine(t, bad, "telemetry-accepted"), tx.RunID)
		if err != nil || got[tx.Key()]["telemetry-accepted"].OperationID != tx.OperationID {
			t.Fatal(got, err)
		}
	})
	t.Run("duplicate_fact", func(t *testing.T) {
		b := append(evxEventLine(t, tx, "control-confirmed"), evxEventLine(t, tx, "control-confirmed")...)
		_, err := ParseEvents(b, tx.RunID)
		evxRequireError(t, err, "duplicate business event")
	})
	t.Run("foreign_session_cannot_satisfy_key", func(t *testing.T) {
		other := tx
		other.SessionID = "other"
		got, err := ParseEvents(evxEventLine(t, other, "control-confirmed"), tx.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := got[tx.Key()]; ok {
			t.Fatal("foreign session merged")
		}
	})
	t.Run("ignored_nonbusiness_and_unknown_outcome", func(t *testing.T) {
		b := []byte(`{"run_id":"evx-synthetic","stage":"startup","outcome":"ok"}` + "\n" + `{"run_id":"evx-synthetic","stage":"business","outcome":"future-event"}`)
		got, err := ParseEvents(b, tx.RunID)
		if err != nil || len(got) != 0 {
			t.Fatal("nonbusiness records not ignored as specified", got, err)
		}
	})
	t.Run("malformed_and_duplicate_json", func(t *testing.T) {
		for _, b := range [][]byte{[]byte(`{"run_id":`), []byte(`{"run_id":"evx-synthetic","run_id":"x"}`)} {
			_, err := ParseEvents(b, tx.RunID)
			evxRequireError(t, err, "")
		}
	})
	t.Run("line_limit", func(t *testing.T) {
		_, err := ParseEvents(bytes.Repeat([]byte{'x'}, MaxLine+1), tx.RunID)
		evxRequireError(t, err, "event exceeds length limit")
	})
	t.Run("correlation_limit", func(t *testing.T) {
		var b bytes.Buffer
		for i := 1; i <= 10001; i++ {
			v := tx
			v.SampleID = uint64(i)
			v.OperationID = fmt.Sprintf("set-interval-D001-%d", i)
			b.Write(evxEventLine(t, v, "control-confirmed"))
		}
		_, err := ParseEvents(b.Bytes(), tx.RunID)
		evxRequireError(t, err, "too many business events")
	})
}
func TestEVX06ConfirmationObserverExactKeyAndPartialLine(t *testing.T) {
	tx := evxTransaction()
	t.Run("cached_exact_confirmation", func(t *testing.T) {
		o := ConfirmationObserver{Path: "path-is-not-opened", RunID: tx.RunID, Index: EventIndex{tx.Key(): map[string]BusinessEvent{"control-confirmed": EventOf(tx, "control-confirmed")}}}
		if err := o.Wait(context.Background(), tx); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("complete_line", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "events")
		b := evxEventLine(t, tx, "control-confirmed")
		if err := WriteBytes(p, b); err != nil {
			t.Fatal(err)
		}
		o := ConfirmationObserver{Path: p, RunID: tx.RunID}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := o.Wait(ctx, tx); err != nil || o.Offset != int64(len(b)) {
			t.Fatal(o.Offset, err)
		}
	})
	t.Run("partial_line_then_append", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "events")
		b := evxEventLine(t, tx, "control-confirmed")
		cut := len(b) / 2
		if err := WriteBytes(p, b[:cut]); err != nil {
			t.Fatal(err)
		}
		o := ConfirmationObserver{Path: p, RunID: tx.RunID}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := o.Wait(ctx, tx)
		if !errors.Is(err, context.Canceled) || len(o.Index) != 0 || len(o.Pending) != cut {
			t.Fatal("partial line counted", err)
		}
		f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, we := f.Write(b[cut:])
		ce := f.Close()
		if we != nil || ce != nil {
			t.Fatal(we, ce)
		}
		ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
		defer cancel2()
		if err = o.Wait(ctx2, tx); err != nil || len(o.Pending) != 0 {
			t.Fatal("completed line", err)
		}
	})
	t.Run("foreign_confirmation_times_out", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "events")
		other := tx
		other.SessionID = "other"
		if err := WriteBytes(p, evxEventLine(t, other, "control-confirmed")); err != nil {
			t.Fatal(err)
		}
		o := ConfirmationObserver{Path: p, RunID: tx.RunID}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		if err := o.Wait(ctx, tx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
		if _, ok := o.Index[tx.Key()]["control-confirmed"]; ok {
			t.Fatal("false current confirmation")
		}
	})
	t.Run("missing_file", func(t *testing.T) {
		o := ConfirmationObserver{Path: filepath.Join(t.TempDir(), "absent"), RunID: tx.RunID}
		if err := o.Wait(context.Background(), tx); !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
	})
	t.Run("bad_run_rejected", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "events")
		other := tx
		other.RunID = "old"
		if err := WriteBytes(p, evxEventLine(t, other, "control-confirmed")); err != nil {
			t.Fatal(err)
		}
		o := ConfirmationObserver{Path: p, RunID: tx.RunID}
		evxRequireError(t, o.Wait(context.Background(), tx), "run mismatch")
	})
	t.Run("duplicate_across_reads", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "events")
		if err := WriteBytes(p, evxEventLine(t, tx, "control-sent")); err != nil {
			t.Fatal(err)
		}
		o := ConfirmationObserver{Path: p, RunID: tx.RunID, Index: EventIndex{tx.Key(): map[string]BusinessEvent{"control-sent": EventOf(tx, "control-sent")}}}
		evxRequireError(t, o.Wait(context.Background(), tx), "duplicate event")
	})
	t.Run("pending_limit", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "events")
		if err := WriteBytes(p, []byte(strings.Repeat("x", MaxLine+1))); err != nil {
			t.Fatal(err)
		}
		o := ConfirmationObserver{Path: p, RunID: tx.RunID}
		evxRequireError(t, o.Wait(context.Background(), tx), "event observer limit")
	})
}
