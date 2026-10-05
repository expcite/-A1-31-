package scenarios

import "testing"

func TestFixedCatalog(t *testing.T) {
	if _, e := Resolve("go test; malicious", "bin", "out", "run", "hash"); e == nil {
		t.Fatal("任意命令接受")
	}
	for _, kind := range []string{"m6_rules", "bench_T", "bench_H", "pbench"} {
		c, e := Resolve(kind, "bin", "out", "run", "hash")
		if e != nil || len(c.Args) == 0 {
			t.Fatal(kind, e)
		}
	}
}
