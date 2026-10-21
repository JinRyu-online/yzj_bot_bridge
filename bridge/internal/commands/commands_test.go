package commands

import (
	"strings"
	"testing"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/registry"
)

func TestParseStopAlwaysAvailable(t *testing.T) {
	b := &bot.Bot{Config: bot.Config{ID: "logbot", CommandsEnabled: false}}
	for _, in := range []string{"--stop", "--abort", "--interrupt", "/停止", "/中断"} {
		got := Parse(in, b, nil)
		if got.Overrides["stop"] != "1" || !got.Handled || got.RestText != "" {
			t.Fatalf("%q => %+v", in, got)
		}
	}
}

func TestParseStopIgnoresTrailingText(t *testing.T) {
	b := &bot.Bot{Config: bot.Config{ID: "logbot", CommandsEnabled: true}}
	got := Parse("--stop 换个问题", b, nil)
	if got.Overrides["stop"] != "1" || !got.Handled || got.RestText != "" {
		t.Fatalf("%+v", got)
	}
}

func TestParseHelpListsStop(t *testing.T) {
	b := &bot.Bot{Config: bot.Config{ID: "logbot", CommandsEnabled: true}}
	got := Parse("--help", b, nil)
	if !got.Handled || !strings.Contains(got.Reply, "--stop") || !strings.Contains(got.Reply, "--jobs") {
		t.Fatalf("%+v", got)
	}
	if !strings.Contains(got.Reply, "--memory") || !strings.Contains(got.Reply, "--forget") {
		t.Fatalf("help missing memory/forget: %+v", got)
	}
}

func TestParseMemoryAndForget(t *testing.T) {
	b := &bot.Bot{Config: bot.Config{ID: "logbot", CommandsEnabled: true}}
	got := Parse("/memory off", b, nil)
	if got.Overrides["memory"] != "off" || !got.Handled || got.RestText != "" {
		t.Fatalf("%+v", got)
	}
	got = Parse("--memory on", b, nil)
	if got.Overrides["memory"] != "on" || !got.Handled {
		t.Fatalf("%+v", got)
	}
	got = Parse("/memory", b, nil)
	if got.Overrides["memory"] != "show" {
		t.Fatalf("%+v", got)
	}
	got = Parse("/forget", b, nil)
	if got.Overrides["forget"] != "1" || !got.Handled || got.RestText != "" {
		t.Fatalf("%+v", got)
	}
}

func TestParseJobsAlwaysAvailable(t *testing.T) {
	b := &bot.Bot{Config: bot.Config{ID: "logbot", CommandsEnabled: false}}
	for _, in := range []string{"--jobs", "--queue", "--list", "--tasks", "/任务", "/队列", "/列表"} {
		got := Parse(in, b, nil)
		if got.Overrides["jobs"] != "1" || !got.Handled || got.RestText != "" {
			t.Fatalf("%q => %+v", in, got)
		}
	}
}
func TestMessageTargetsBotOtherGroupMention(t *testing.T) {
	reg := registry.New()
	reg.Replace([]*bot.Bot{
		{Config: bot.Config{ID: "border_bot", RoleID: "border_bot", Name: "寄件日志查询", Group: "寄件助手群"}},
		{Config: bot.Config{ID: "same_city", RoleID: "bsame_city_log", Name: "同城日志查询", Group: "同城告警群"}},
	})
	content := "@寄件日志查询 查下此单下单原始日志"
	if MessageTargetsBot(content, reg.Get("same_city"), reg) {
		t.Fatal("@寄件日志查询 不应命中 同城日志查询")
	}
	if !MessageTargetsBot(content, reg.Get("border_bot"), reg) {
		t.Fatal("@寄件日志查询 应命中 寄件日志查询")
	}
	// 无 @ 的消息仍按原有私聊/兜底语义处理。
	if !MessageTargetsBot("你好", reg.Get("same_city"), reg) {
		t.Fatal("无 @ 消息不应被过滤")
	}
}
func TestMessageTargetsBotPrivateKeepsEmail(t *testing.T) {
	reg := registry.New()
	reg.Replace([]*bot.Bot{{Config: bot.Config{ID: "fairy", RoleID: "fairy", Name: "Fairy", Group: "g"}}})
	b := reg.Get("fairy")
	if !MessageTargetsBot("我的邮箱 test@example.com", b, reg, 3) {
		t.Fatal("私聊里的邮箱不能被当成 @ 别人")
	}
	if MessageTargetsBot("我的邮箱 test@example.com", b, reg) {
		t.Fatal("群聊里未解析到机器人时仍应拦截")
	}
}
