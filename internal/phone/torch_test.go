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
		}, time.Millisecond)
		cancel()
		if len(calls) != 2 || calls[0] != "on" || calls[1] != "off" {
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
	err := pulseTorch(context.Background(), func(context.Context) error { return problem }, func(context.Context) error { called = true; return nil }, time.Millisecond)
	if !called || !errors.Is(err, problem) {
		t.Fatal("打开失败也要尝试关闭，不得报告成功")
	}
}

func TestTorchOffFailureStaysFailure(t *testing.T) {
	problem := errors.New("off rejected")
	err := pulseTorch(context.Background(), func(context.Context) error { return nil }, func(context.Context) error { return problem }, time.Millisecond)
	if !errors.Is(err, problem) {
		t.Fatal("关闭失败不能报告手电筒调用完成")
	}
}
