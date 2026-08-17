package backends

import (
	"context"
	"testing"
	"time"

	"yzj-bridge/internal/bot"
)

func TestResultFromCtx(t *testing.T) {
	if _, ok := resultFromCtx(context.Background(), "timeout"); ok {
		t.Fatal("live context should not produce a result")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, ok := resultFromCtx(ctx, "timeout")
	if !ok || res.Status != "interrupted" || res.Reply != "任务已中断" {
		t.Fatalf("%+v ok=%v", res, ok)
	}
	dead, cancel2 := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel2()
	time.Sleep(time.Millisecond)
	res, ok = resultFromCtx(dead, "cursor-cli 超时")
	if !ok || res.Status != "timeout" || res.Reply != "cursor-cli 超时" {
		t.Fatalf("%+v ok=%v", res, ok)
	}
}

func TestWithRunTimeoutInheritsParentCancel(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	ctx, stop := withRunTimeout(bot.RunOpts{Context: parent}, time.Hour)
	defer stop()
	cancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("child context did not inherit cancel")
	}
}
