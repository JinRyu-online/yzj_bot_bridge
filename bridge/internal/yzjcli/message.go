// Package yzjcli talks to the official Yunzhijia CLI (`yzj-cli`) and polls
// configured group chats. It never stores credentials itself: login state is
// owned by yzj-cli (its own config dir / OS keychain).
package yzjcli

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// timeLayout is the sendTime format returned by yunzhijia msgList, e.g.
// "2026-10-21 11:59:13.446".
const timeLayout = "2006-01-02 15:04:05.000"

// Segment is one rich-text segment inside param.desc: an @ mention
// (type=at, data=openID or "all") or an inline image (type=image, data=fileID).
type Segment struct {
	Type   string `json:"type"`
	Data   string `json:"data"`
	Start  int    `json:"start"`
	Length int    `json:"length"`
	W      int    `json:"w"`
	H      int    `json:"h"`
}

// FlexBool tolerates the platform's inconsistency (bool vs "true"/"false").
type FlexBool bool

// UnmarshalJSON accepts bool, numeric and quoted boolean forms.
func (b *FlexBool) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	s = strings.Trim(s, `"`)
	switch strings.ToLower(s) {
	case "true", "1", "yes":
		*b = true
	default:
		*b = false
	}
	return nil
}

// Value returns the parsed boolean.
func (b FlexBool) Value() bool { return bool(b) }

// FlexInt tolerates number vs quoted number fields.
type FlexInt int

// UnmarshalJSON accepts number and quoted-number forms.
func (i *FlexInt) UnmarshalJSON(data []byte) error {
	s := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if s == "" || s == "null" {
		*i = 0
		return nil
	}
	if v, err := strconv.Atoi(s); err == nil {
		*i = FlexInt(v)
		return nil
	}
	var f float64
	if err := json.Unmarshal([]byte(s), &f); err == nil {
		*i = FlexInt(int(f))
		return nil
	}
	*i = 0
	return nil
}

// MessageParam carries the message metadata observed via
// `yzj-cli im message list`. Fields not present for a message type stay zero.
type MessageParam struct {
	Desc            []Segment       `json:"desc"`
	ReplyMsgID      string          `json:"replyMsgId"`
	ReplyOpenID     string          `json:"replyOpenId"`
	ReplyPersonName string          `json:"replyPersonName"`
	ReplyRootMsgID  string          `json:"replyRootMsgId"`
	ReplySummary    string          `json:"replySummary"`
	IsReference     FlexBool        `json:"isReference"`
	FormatType      string          `json:"formatType"`
	FileID          string          `json:"file_id"`
	Ext             string          `json:"ext"`
	Name            string          `json:"name"`
	Size            json.RawMessage `json:"size"`
	FType           *FlexInt        `json:"ftype"`
	PicWidth        int             `json:"picWidth"`
	PicHeight       int             `json:"picHeight"`
	IsEncrypted     FlexBool        `json:"isEncrypted"`
	SysType         string          `json:"sysType"`
	NotifyToAll     FlexBool        `json:"notifyToAll"`
}

// Message is one chat message returned by `yzj-cli im message list`.
type Message struct {
	Content    string          `json:"content"`
	ExtFields  json.RawMessage `json:"extFields"`
	FromOpenID string          `json:"fromOpenId"`
	MsgID      string          `json:"msgId"`
	MsgType    string          `json:"msgType"` // text | richText | file | other
	Param      MessageParam    `json:"param"`
	SendTime   string          `json:"sendTime"`
}

// Attachment is one downloadable media item found in a message.
type Attachment struct {
	Kind   string // image | file
	FileID string
	Name   string
	Ext    string
	Size   int64
	Width  int
	Height int
}

var imageExts = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".bmp": "image/bmp",
}

// SendTimeValue parses sendTime; zero time when unparseable.
func (m Message) SendTimeValue() time.Time {
	s := strings.TrimSpace(m.SendTime)
	if s == "" {
		return time.Time{}
	}
	if t, err := time.ParseInLocation(timeLayout, s, time.Local); err == nil {
		return t
	}
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t
		}
	}
	return time.Time{}
}

// IsFromBot reports whether the sender is a robot (BOT-<id>).
func (m Message) IsFromBot() bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(m.FromOpenID)), "BOT-")
}

// IsRecall reports recall/system notices, which must not be dispatched.
func (m Message) IsRecall() bool {
	return strings.EqualFold(strings.TrimSpace(m.MsgType), "other") ||
		strings.EqualFold(strings.TrimSpace(m.Param.SysType), "withdrawMsg")
}

// ImageAttachments returns inline images carried by the message.
func (m Message) ImageAttachments() []Attachment {
	var out []Attachment
	switch strings.ToLower(strings.TrimSpace(m.MsgType)) {
	case "file":
		ext := normalizeExt(m.Param.Ext)
		if (m.Param.FType != nil && int(*m.Param.FType) == 1) || isImageExt(ext) {
			out = append(out, Attachment{
				Kind: "image", FileID: strings.TrimSpace(m.Param.FileID),
				Name: m.Param.Name, Ext: ext, Size: rawSize(m.Param.Size),
				Width: m.Param.PicWidth, Height: m.Param.PicHeight,
			})
		}
	case "richtext":
		for _, seg := range m.Param.Desc {
			if !strings.EqualFold(seg.Type, "image") || strings.TrimSpace(seg.Data) == "" {
				continue
			}
			out = append(out, Attachment{
				Kind: "image", FileID: strings.TrimSpace(seg.Data),
				Width: seg.W, Height: seg.H,
			})
		}
	}
	return dropEmptyAttachments(out)
}

// Attachments returns every downloadable attachment (images for now; files are
// a Phase 2 extension and use the same file_id mechanism).
func (m Message) Attachments() []Attachment {
	return m.ImageAttachments()
}

// HasImages reports whether the message carries at least one image.
func (m Message) HasImages() bool {
	for _, a := range m.Attachments() {
		if a.Kind == "image" && a.FileID != "" {
			return true
		}
	}
	return false
}

// MentionsBots reports whether the message @-mentions one of the bots.
// Robotic ids start with "BOT-"; when botOpenIDs is provided the at segment
// must match one of them, otherwise any robot mention matches. When no robot
// at-segment is present we fall back to matching "@<alias>" in content.
func (m Message) MentionsBots(aliases, botOpenIDs []string) bool {
	for _, seg := range m.Param.Desc {
		if !strings.EqualFold(strings.TrimSpace(seg.Type), "at") {
			continue
		}
		data := strings.TrimSpace(seg.Data)
		if data == "" || strings.EqualFold(data, "all") {
			continue
		}
		if !strings.HasPrefix(strings.ToUpper(data), "BOT-") {
			continue
		}
		if len(botOpenIDs) > 0 && !containsFold(botOpenIDs, data) {
			continue
		}
		return true
	}
	if len(botOpenIDs) > 0 {
		return false
	}
	low := strings.ToLower(m.Content)
	for _, a := range aliases {
		a = strings.TrimSpace(a)
		if a != "" && strings.Contains(low, "@"+strings.ToLower(a)) {
			return true
		}
	}
	return false
}

// QuotesBot reports whether the message quotes/replies to a robot message.
// The reference is a robot when replyOpenId starts with "BOT-"; when
// botOpenIDs are known they must match, and when aliases are known the robot
// display name (replyPersonName) must match one of them.
func (m Message) QuotesBot(aliases, botOpenIDs []string) bool {
	reply := strings.TrimSpace(m.Param.ReplyOpenID)
	if !strings.HasPrefix(strings.ToUpper(reply), "BOT-") {
		return false
	}
	if len(botOpenIDs) > 0 && !containsFold(botOpenIDs, reply) {
		return false
	}
	if len(aliases) > 0 {
		name := strings.TrimSpace(m.Param.ReplyPersonName)
		if name != "" {
			for _, a := range aliases {
				if strings.EqualFold(strings.TrimSpace(a), name) {
					return true
				}
			}
			// Name known but different from ours: not our bot.
			if len(botOpenIDs) == 0 {
				return false
			}
		}
	}
	return true
}

// DisplayText is the text handed to the agent. Image-only messages carry the
// "[图片]" placeholder from the platform.
func (m Message) DisplayText() string {
	return strings.TrimSpace(m.Content)
}

// BotMsgIDs returns msg ids that look like robot-sent messages.
func (m Message) IsBotMsgID() bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(m.MsgID)), "BOT-")
}

var fileIDInTextRe = regexp.MustCompile(`(?i)(?:fileId=|filesvr/|downloadfile/)([0-9a-zA-Z_-]{16,})`)

// FileIDsFromText is a best-effort fallback for text messages that embed a
// download link instead of a structured attachment.
func FileIDsFromText(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range fileIDInTextRe.FindAllStringSubmatch(s, -1) {
		id := m[1]
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func normalizeExt(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		return ""
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return ext
}

func isImageExt(ext string) bool {
	_, ok := imageExts[normalizeExt(ext)]
	return ok
}

// MimeForExt maps a normalised extension to a mime type ("" when unknown).
func MimeForExt(ext string) string { return imageExts[normalizeExt(ext)] }

func rawSize(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
			return v
		}
	}
	return 0
}

func dropEmptyAttachments(in []Attachment) []Attachment {
	out := in[:0]
	for _, a := range in {
		if strings.TrimSpace(a.FileID) != "" {
			out = append(out, a)
		}
	}
	return out
}

func containsFold(list []string, v string) bool {
	for _, x := range list {
		if strings.EqualFold(strings.TrimSpace(x), strings.TrimSpace(v)) {
			return true
		}
	}
	return false
}
