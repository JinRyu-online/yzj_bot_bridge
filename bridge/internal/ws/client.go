package ws

import (
	"context"
	"encoding/json"
	"log"
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
	hb := c.Heartbeat
	if hb <= 0 {
		hb = 30 * time.Second
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
