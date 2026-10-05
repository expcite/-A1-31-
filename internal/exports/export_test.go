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
}
