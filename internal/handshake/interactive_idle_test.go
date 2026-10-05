package handshake

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// Exercise the real record implementation. The existing fixture produces real
// session material; no bypass is available to the gateway/device/console.
func TestInteractiveIdleProfilePartialFrame(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	var sm sessionMaterial
	sm.sid[0] = 1
	ch, e := newChannel(a, 16384, 60*time.Millisecond, sm, 2, "peer", PeerSnapshot{}, false, 100, 1024)
	if e != nil {
		t.Fatal(e)
	}
	ch.(*secureChannel).idleTimeout = time.Second
	done := make(chan error, 1)
	go func() { _, _, e := ch.Receive(context.Background()); done <- e }()
	time.Sleep(120 * time.Millisecond)
	if IsClosed(ch) {
		t.Fatal("首字节空闲误作部分帧超时")
	}
	if _, e = b.Write([]byte{0}); e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-done:
		if !errors.Is(e, ErrTimeout) {
			t.Fatal("部分帧没有按短预算超时", e)
		}
		if !IsClosed(ch) {
			t.Fatal("部分帧出错后未关闭")
		}
	case <-time.After(time.Second):
		t.Fatal("部分帧预算没有约束")
	}
}
