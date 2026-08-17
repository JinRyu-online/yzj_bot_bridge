package backends

import (
	"context"
	"errors"
	"time"

	"yzj-bridge/internal/bot"
)

func parentCtx(opts bot.RunOpts) context.Context {
	if opts.Context != nil {
		return opts.Context
	}
	return context.Background()
}

func withRunTimeout(opts bot.RunOpts, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parentCtx(opts), timeout)
}

func resultFromCtx(ctx context.Context, timeoutReply string) (bot.RunResult, bool) {
	err := ctx.Err()
	if err == nil {
		return bot.RunResult{}, false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return bot.RunResult{Reply: timeoutReply, Status: "timeout"}, true
	}
	return bot.RunResult{Reply: "任务已中断", Status: "interrupted"}, true
}
