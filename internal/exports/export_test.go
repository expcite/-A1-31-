package exports

import (
	"encoding/json"
	"example.com/edgehybrid/internal/live"
	"strings"
	"testing"
	"time"
)

func TestExportRecomputeAndPrivacy(t *testing.T) {
	temp := 26.3
	s := live.Snapshot{Run: "r", Events: []live.Event{{Schema: "interactive-event-v1", Run: "r", ID: 1, At: time.Now(), Endpoint: "device", Device: "D001", SID: strings.Repeat("a", 32), Sample: 1, Stage: "telemetry_written", Operation: "=HYPERLINK(1)", Detail: "<script>bad</script>", Temperature: &temp}}}
	b, _, e := Build(s, strings.Repeat("a", 64), "json", false)
	if e != nil {
		t.Fatal(e)
	}
	var d Document
	if e = json.Unmarshal(b, &d); e != nil {
		t.Fatal(e)
	}
	if e = Validate(d); e != nil || d.Events[0].Temperature != nil {
		t.Fatal("默认导出载荷", e)
	}
	d.StageCounts["telemetry_written"] = 80
	if e = Validate(d); e == nil {
		t.Fatal("篡改分母未拒绝")
	}
	b, _, e = Build(s, strings.Repeat("a", 64), "csv", true)
	if e != nil || !strings.Contains(string(b), "'=HYPERLINK") {
		t.Fatal("CSV注入未中和", e)
	}
	b, _, e = Build(s, strings.Repeat("a", 64), "html", true)
	if e != nil || strings.Contains(string(b), "<script>") {
		t.Fatal("HTML导出注入")
	}
	if !strings.Contains(string(b), d.LedgerRoot) {
		t.Fatal("HTML导出未包含因果哈希链根存根")
	}
}

func TestLedgerRootHashChain(t *testing.T) {
	events1 := []live.Event{
		{ID: 1, Endpoint: "device", Device: "D001", Stage: "authenticated"},
		{ID: 2, Endpoint: "device", Device: "D001", Stage: "telemetry_written"},
	}
	events2 := []live.Event{
		{ID: 1, Endpoint: "device", Device: "D001", Stage: "authenticated"},
		{ID: 2, Endpoint: "device", Device: "D001", Stage: "telemetry_accepted"}, // altered stage
	}
	eventsReordered := []live.Event{
		{ID: 2, Endpoint: "device", Device: "D001", Stage: "telemetry_written"},
		{ID: 1, Endpoint: "device", Device: "D001", Stage: "authenticated"},
	}

	root1 := ComputeLedgerRoot("run-1", events1)
	root2 := ComputeLedgerRoot("run-1", events2)
	rootReordered := ComputeLedgerRoot("run-1", eventsReordered)

	if len(root1) != 64 || len(root2) != 64 {
		t.Fatalf("expected 64-char hex, got %d / %d", len(root1), len(root2))
	}
	if root1 == root2 {
		t.Fatal("altered stage must produce distinct ledger root")
	}
	if root1 == rootReordered {
		t.Fatal("reordered events must produce distinct ledger root")
	}
}
