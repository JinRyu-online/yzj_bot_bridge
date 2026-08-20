package memory

import "time"

// Field holds manual/inferred values with an optional lock.
type Field struct {
	Manual   string `json:"manual,omitempty"`
	Inferred string `json:"inferred,omitempty"`
	Locked   bool   `json:"locked,omitempty"`
}

// StringListField is like Field but for string slices (donts).
type StringListField struct {
	Manual   []string `json:"manual,omitempty"`
	Inferred []string `json:"inferred,omitempty"`
	Locked   bool     `json:"locked,omitempty"`
}

// FactCard is an optional short fact with expiry.
type FactCard struct {
	Text      string `json:"text"`
	Source    string `json:"source,omitempty"` // manual | inferred
	ExpiresAt string `json:"expires_at,omitempty"`
	Locked    bool   `json:"locked,omitempty"`
}

// Profile is the durable user memory document keyed by OperatorOpenID.
type Profile struct {
	OpenID             string          `json:"open_id"`
	DisplayName        string          `json:"display_name,omitempty"`
	HowToAddress       Field           `json:"how_to_address"`
	Role               Field           `json:"role"`
	AskStyle           Field           `json:"ask_style"`
	ReplyStyle         Field           `json:"reply_style"`
	Donts              StringListField `json:"donts"`
	Notes              Field           `json:"notes"`
	FactCards          []FactCard      `json:"fact_cards,omitempty"`
	LastSeen           string          `json:"last_seen,omitempty"`
	BotsSeen           []string        `json:"bots_seen,omitempty"`
	OptedOut           bool            `json:"opted_out,omitempty"`
	ForgetPendingUntil string          `json:"forget_pending_until,omitempty"`
	ProfiledCount      int             `json:"profiled_count"` // turns consumed by successful profiler
	LastProfileAt      string          `json:"last_profile_at,omitempty"`
	LastError          string          `json:"last_error,omitempty"`
	UpdatedAt          string          `json:"updated_at,omitempty"`
}

// Turn is one completed Q/A pair stored in memory turns jsonl (not session jsonl).
type Turn struct {
	TS        string `json:"ts"`
	BotID     string `json:"bot_id"`
	Group     string `json:"group,omitempty"`
	User      string `json:"user"`
	Assistant string `json:"assistant"`
}

// Effective returns manual if set, else inferred.
func (f Field) Effective() string {
	if s := trimSpace(f.Manual); s != "" {
		return s
	}
	return trimSpace(f.Inferred)
}

// Effective returns manual if non-empty, else inferred.
func (f StringListField) Effective() []string {
	if len(f.Manual) > 0 {
		return cloneStrings(f.Manual)
	}
	return cloneStrings(f.Inferred)
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 {
		c := s[len(s)-1]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
