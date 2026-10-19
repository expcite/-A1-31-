package phone

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTorchCleanupAfterSuccessAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		calls := []string{}
		err := pulseTorch(ctx, func(context.Context) error {
			calls = append(calls, "on")
			if cancelled {
				cancel()
			}
			return nil
		}, func(c context.Context) error {
			if c.Err() != nil {
				t.Fatal("关闭不能使用已取消的采集上下文")
			}
			calls = append(calls, "off")
			return nil
		}, time.Millisecond, time.Millisecond, time.Millisecond)
		cancel()
		if len(calls) != 3 || calls[0] != "off" || calls[1] != "on" || calls[2] != "off" {
			t.Fatal(calls)
		}
		if cancelled && !errors.Is(err, context.Canceled) {
			t.Fatal("取消不能冒充提示成功")
		}
		if !cancelled && err != nil {
			t.Fatal(err)
		}
	}
}

func TestTorchAttemptsOffAfterAmbiguousOnFailure(t *testing.T) {
	called := false
	problem := errors.New("on timed out")
	err := pulseTorch(context.Background(), func(context.Context) error { return problem }, func(context.Context) error { called = true; return nil }, time.Millisecond, time.Millisecond, time.Millisecond)
	if !called || !errors.Is(err, problem) {
		t.Fatal("打开失败也要尝试关闭，不得报告成功")
	}
}

func TestTorchOffFailureStaysFailure(t *testing.T) {
	problem := errors.New("off rejected")
	err := pulseTorch(context.Background(), func(context.Context) error { return nil }, func(context.Context) error { return problem }, time.Millisecond, time.Millisecond, time.Millisecond)
	if !errors.Is(err, problem) {
		t.Fatal("关闭失败不能报告手电筒调用完成")
	}
}

func TestRepeatedTorchResetsBeforeEveryPulse(t *testing.T) {
	var calls []string
	on := func(context.Context) error { calls = append(calls, "on"); return nil }
	off := func(context.Context) error { calls = append(calls, "off"); return nil }
	for i := 0; i < 5; i++ {
		if err := pulseTorch(context.Background(), on, off, time.Millisecond, time.Millisecond, time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	if len(calls) != 15 {
		t.Fatal(calls)
	}
	for i, call := range calls {
		expected := "off"
		if i%3 == 1 {
			expected = "on"
		}
		if call != expected {
			t.Fatal(calls)
		}
	}
}

func TestTorchCancellationWhileResettingDoesNotTurnOn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var offCalls int
	err := pulseTorch(ctx, func(context.Context) error { t.Fatal("reset cancellation must prevent on"); return nil }, func(c context.Context) error {
		offCalls++
		if offCalls == 1 {
			cancel()
		} else if c.Err() != nil {
			t.Fatal("cleanup must remain usable")
		}
		return nil
	}, time.Second, time.Second, time.Second)
	if offCalls != 2 || !errors.Is(err, context.Canceled) {
		t.Fatal(offCalls, err)
	}
}
