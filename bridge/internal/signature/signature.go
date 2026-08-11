package signature

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
)

func strField(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func timeField(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

// BuildSignatureString builds the HMAC plaintext in fixed field order.
func BuildSignatureString(msg map[string]any) string {
	parts := []string{
		strField(msg["robotId"]),
		strField(msg["robotName"]),
		strField(msg["operatorOpenid"]),
		strField(msg["operatorName"]),
		timeField(msg["time"]),
		strField(msg["msgId"]),
		strField(msg["content"]),
	}
	return strings.Join(parts, ",")
}

func ComputeHMACSHA1(data, secret string) string {
	mac := hmac.New(sha1.New, []byte(secret))
	_, _ = mac.Write([]byte(data))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func VerifySignature(msg map[string]any, signature, secret string) (bool, string) {
	if secret == "" {
		return true, ""
	}
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return false, "missing sign header"
	}
	expected := ComputeHMACSHA1(BuildSignatureString(msg), secret)
	if subtle.ConstantTimeCompare([]byte(signature), []byte(expected)) == 1 {
		return true, ""
	}
	return false, "invalid signature"
}

func ExtractSignHeader(h http.Header) string {
	for _, key := range []string{"sign", "Sign", "SIGN"} {
		if v := strings.TrimSpace(h.Get(key)); v != "" {
			return v
		}
	}
	return ""
}
