package evidence

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Limited parser robustness only: <=64KiB per candidate. It cannot prove absence
// of excessive allocations on arbitrary file sizes, nor semantic truth of data.
func FuzzEVXEvidenceDecode(f *testing.F) {
	for _, s := range []string{`{"id":"设备","count":0,"nested":[]}`, `{"id":"x","count":2,"nested":[{"count":1}]}`, `{"id":"x","id":"y"}`, `{} {}`, `{"id":`, "\xff"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 64<<10 {
			return
		}
		var x evxObject
		err := Decode(b, &x)
		if err != nil {
			return
		}
		encoded, e := json.Marshal(x)
		if e != nil {
			t.Fatal("accepted value cannot encode", e)
		}
		var y evxObject
		if e = Decode(encoded, &y); e != nil || !reflect.DeepEqual(x, y) {
			t.Fatal("accepted typed value fails roundtrip", e)
		}
		again, e := json.Marshal(y)
		if e != nil || string(encoded) != string(again) {
			t.Fatal("unstable typed encoding", e)
		}
	})
}
