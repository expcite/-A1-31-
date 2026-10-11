package probe
import "testing"
func TestRaceProbe(t *testing.T) { ch:=make(chan int); go func(){ch<-1}(); if <-ch!=1 { t.Fatal("bad channel") } }
