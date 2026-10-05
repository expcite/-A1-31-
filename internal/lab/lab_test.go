package lab

import (
	"context"
	"testing"
)

func TestLoopbackTwentyDevices(t *testing.T) {
	s, e := Run(context.Background(), 20, 3)
	if e != nil {
		t.Fatal(e)
	}
	if s.Delivered != 60 {
		t.Fatalf("delivered %d", s.Delivered)
	}
}
func TestInvalidInput(t *testing.T) {
	if _, e := Run(context.Background(), 0, 3); e == nil {
		t.Fatal("accepted invalid input")
	}
}
