package backends

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"yzj-bridge/internal/bot"
)

func TestSafePath(t *testing.T) {
	dir := t.TempDir()
	got, err := SafePath(dir, "a/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "a", "b.txt")
	if got != want {
		t.Fatalf("%s != %s", got, want)
	}
	_, err = SafePath(dir, "../outside")
	if err == nil {
		t.Fatal("expected escape error")
	}
	_ = os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hi"), 0o644)
}

func TestOpenAIRunInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	o := NewOpenAI(bot.Config{Model: "m", OpenAITimeout: 5, OpenAIAPIKey: "k"}, nil)
	res := o.Run("hi", bot.RunOpts{Context: ctx, Mode: "ask"})
	if res.Status != "interrupted" || res.Reply != "任务已中断" {
		t.Fatalf("%+v", res)
	}
}
