package memory

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	rePhone     = regexp.MustCompile(`(?i)(?:\+?86[-\s]?)?1[3-9]\d{9}`)
	reWaybill   = regexp.MustCompile(`(?i)\b(?:YT|SF|JD|ZT|YD|STO|YTO|ZTO|EMS|TT|JT|DBL|HTKY)\d{8,}\b|\b\d{10,15}\b`)
	reToken     = regexp.MustCompile(`(?i)\b(?:Bearer\s+)?[A-Za-z0-9_\-]{24,}\.[A-Za-z0-9_\-]{10,}|\bsk-[A-Za-z0-9]{16,}\b|\byzjtoken=[^\s&]+`)
	reLogWall   = regexp.MustCompile(`(?s)(?:\n.*){30,}`)
)

// StripPII removes common PII patterns from text for prompts and storage.
func StripPII(s string) string {
	s = reToken.ReplaceAllString(s, "[token]")
	s = rePhone.ReplaceAllString(s, "[phone]")
	s = reWaybill.ReplaceAllString(s, "[id]")
	if utf8.RuneCountInString(s) > 4000 || reLogWall.MatchString(s) {
		s = clipRunes(s, 800)
	}
	return strings.TrimSpace(s)
}

// ContainsPII reports whether s still looks like it has PII after a light check
// (used to reject inferred field values before write).
func ContainsPII(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if rePhone.MatchString(s) || reToken.MatchString(s) {
		return true
	}
	// Long digit runs that look like tracking / phone leftovers.
	if reWaybill.MatchString(s) && utf8.RuneCountInString(s) < 80 {
		return true
	}
	return false
}

// SanitizeFieldValue strips PII and rejects remaining PII (returns empty).
func SanitizeFieldValue(s string, maxRunes int) string {
	s = StripPII(s)
	if ContainsPII(s) {
		return ""
	}
	if maxRunes > 0 {
		s = clipRunes(s, maxRunes)
	}
	return strings.TrimSpace(s)
}

// SanitizeDonts cleans and caps donts list.
func SanitizeDonts(in []string, max int) []string {
	if max <= 0 {
		max = 8
	}
	var out []string
	seen := map[string]struct{}{}
	for _, d := range in {
		d = SanitizeFieldValue(d, 80)
		if d == "" {
			continue
		}
		key := strings.ToLower(d)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, d)
		if len(out) >= max {
			break
		}
	}
	return out
}
