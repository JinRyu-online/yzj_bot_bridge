package ws

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/config"
	"yzj-bridge/internal/inbound"
)

type Client struct {
	Bot        *bot.Bot
	Dispatcher *inbound.Dispatcher
	StaleMS    int
	InvalidLim int
	Reconnect  time.Duration
	MaxDelay   time.Duration
	Heartbeat  time.Duration
	// AuthFailLimit 连续鉴权失败（如 openToken 过期）达到该次数后自动停用通道，
	// 避免无限重连刷屏。<=0 时使用默认值 3。
	AuthFailLimit int
	// OnAuthGiveUp 在通道因连续鉴权失败被停用时回调（botID），
	// 由 Runtime 挂接以持久化停用状态；可为 nil。
	OnAuthGiveUp func(botID string)

	mu      sync.Mutex
	enabled bool
	cancel  context.CancelFunc
}

func (c *Client) SetEnabled(v bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enabled = v
	c.Bot.SetWSEnabled(v)
	if v {
		if c.cancel == nil {
			ctx, cancel := context.WithCancel(context.Background())
			c.cancel = cancel
			go c.loop(ctx)
		}
	} else if c.cancel != nil {
		c.cancel()
		c.cancel = nil
		c.Bot.SetConnected(false)
	}
}

func (c *Client) Enabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enabled
}

func (c *Client) Dispose() {
	c.SetEnabled(false)
}

func (c *Client) loop(ctx context.Context) {
	delay := c.Reconnect
	if delay <= 0 {
		delay = 5 * time.Second
	}
	maxDelay := c.MaxDelay
	if maxDelay <= 0 {
		maxDelay = 60 * time.Second
	}
	stale := time.Duration(c.StaleMS) * time.Millisecond
	if stale <= 0 {
		stale = 45 * time.Second
	}
	invalidLim := c.InvalidLim
	if invalidLim <= 0 {
		invalidLim = 3
	}
	authFailLim := c.AuthFailLimit
	if authFailLim <= 0 {
		authFailLim = 3
	}
	hb := c.Heartbeat
	if hb <= 0 {
		hb = 30 * time.Second
	}

	// authState 跨重连累计连续鉴权失败次数；达到上限后停用该通道。
	var authState struct {
		mu      sync.Mutex
		n       int
		message string
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if !c.Enabled() {
			return
		}
		wsURL, err := config.DeriveWSURL(c.Bot.Config.SendMsgURL)
		if err != nil {
			c.Bot.SetLastError(err.Error())
			log.Printf("ws url bot=%s: %v", c.Bot.Config.ID, err)
			time.Sleep(delay)
			continue
		}
		conn, _, err := websocket.Dial(ctx, wsURL, nil)
		if err != nil {
			c.Bot.SetLastError(err.Error())
			c.Bot.SetConnected(false)
			log.Printf("ws dial bot=%s: %v", c.Bot.Config.ID, err)
			time.Sleep(delay)
			if delay < maxDelay {
				delay *= 2
				if delay > maxDelay {
					delay = maxDelay
				}
			}
			continue
		}
		delay = c.Reconnect
		if delay <= 0 {
			delay = 5 * time.Second
		}
		c.Bot.SetConnected(true)
		c.Bot.SetLastError("")
		log.Printf("ws connected bot=%s", c.Bot.Config.ID)

		last := time.Now()
		invalid := 0
		readDone := make(chan struct{})
		go func() {
			defer close(readDone)
			for {
				_, data, err := conn.Read(ctx)
				if err != nil {
					return
				}
				last = time.Now()
				var raw map[string]any
				if json.Unmarshal(data, &raw) != nil {
					invalid++
					if invalid >= invalidLim {
						return
					}
					continue
				}
				invalid = 0
				if msg, isFail := authFailMessage(raw); isFail {
					authState.mu.Lock()
					authState.n++
					authState.message = msg
					n := authState.n
					authState.mu.Unlock()
					log.Printf("ws auth bot=%s: %s (%d/%d)", c.Bot.Config.ID, msg, n, authFailLim)
					// 鉴权失败后连接已无意义：主动断开，让主循环计数并决定是否重连/停用。
					_ = conn.Close(websocket.StatusPolicyViolation, "auth failed")
					return
				}
				if authOK(raw) {
					authState.mu.Lock()
					authState.n = 0
					authState.message = ""
					authState.mu.Unlock()
				}
				cr := inbound.ClassifyWS(raw)
				switch cr.Kind {
				case "dispatch":
					c.Dispatcher.Handle(c.Bot.Config.ID, cr.Norm)
				case "ack":
					b, _ := json.Marshal(cr.Ack)
					_ = conn.Write(ctx, websocket.MessageText, b)
				}
			}
		}()

		ticker := time.NewTicker(hb)
	waitLoop:
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				_ = conn.Close(websocket.StatusNormalClosure, "")
				c.Bot.SetConnected(false)
				return
			case <-readDone:
				ticker.Stop()
				_ = conn.Close(websocket.StatusNormalClosure, "")
				c.Bot.SetConnected(false)
				authState.mu.Lock()
				n, msg := authState.n, authState.message
				authState.mu.Unlock()
				if n >= authFailLim {
					lastErr := "ws auth failed: " + msg
					c.Bot.SetLastError(lastErr)
					log.Printf("ws auth bot=%s: %s — giving up after %d consecutive failures, disabling channel", c.Bot.Config.ID, msg, n)
					c.SetEnabled(false)
					if c.OnAuthGiveUp != nil {
						c.OnAuthGiveUp(c.Bot.Config.ID)
					}
					return
				}
				break waitLoop
			case <-ticker.C:
				if time.Since(last) > stale {
					ticker.Stop()
					_ = conn.Close(websocket.StatusGoingAway, "stale")
					c.Bot.SetConnected(false)
					break waitLoop
				}
				_ = conn.Write(ctx, websocket.MessageText, []byte(`{"cmd":"ping"}`))
			}
		}
		time.Sleep(delay)
	}
}

// authFailMessage 提取 cmd=auth 且未成功的鉴权失败信息（如 error.user.openToken.expire）。
func authFailMessage(raw map[string]any) (string, bool) {
	cmd, _ := raw["cmd"].(string)
	if !strings.EqualFold(cmd, "auth") {
		return "", false
	}
	if ok, _ := raw["success"].(bool); ok {
		return "", false
	}
	msg, _ := raw["error"].(string)
	return msg, true
}

// authOK 判断是否收到明确的鉴权成功帧；用于重置失败计数。
func authOK(raw map[string]any) bool {
	cmd, _ := raw["cmd"].(string)
	if !strings.EqualFold(cmd, "auth") {
		return false
	}
	ok, _ := raw["success"].(bool)
	return ok
}
