package config

import (
	"net/url"
	"strings"
)

func extractYZJToken(sendURL string) string {
	u, err := url.Parse(strings.TrimSpace(sendURL))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(u.Query().Get("yzjtoken"))
}

func placeholderToken(token string) bool {
	switch strings.TrimSpace(token) {
	case "", "REPLACE_ME", "REPLACE_ME_YZJTOKEN":
		return true
	default:
		return false
	}
}

func normalizeSendURL(sendURL string) string {
	raw := strings.TrimSpace(sendURL)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return strings.ToLower(raw)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String()
}

func normalizeWebhookPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return strings.TrimRight(p, "/")
}
