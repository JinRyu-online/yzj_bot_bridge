package backends

import (
	"encoding/json"
	"log"
	"strings"
)

// appendCLIPrompt appends the POSIX end-of-options marker so user content
// starting with "-" is not parsed as CLI flags by cursor-agent / claude.
func appendCLIPrompt(args []string, prompt string) []string {
	return append(args, "--", prompt)
}

// extractCLIPrompt returns positional args after "--", or empty if absent.
func extractCLIPrompt(args []string) string {
	for i, a := range args {
		if a == "--" && i+1 < len(args) {
			return strings.Join(args[i+1:], " ")
		}
	}
	return ""
}

type cliStreamCollect struct {
	nonJSON []string
}

func (c *cliStreamCollect) readLine(line string) (map[string]any, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, false
	}
	var ev map[string]any
	if json.Unmarshal([]byte(line), &ev) != nil {
		c.nonJSON = append(c.nonJSON, line)
		return nil, false
	}
	return ev, true
}

func cliReplyOrError(resultText, assistantText string, nonJSON []string, waitErr error) (reply, status string) {
	reply = strings.TrimSpace(resultText)
	if reply == "" {
		reply = strings.TrimSpace(assistantText)
	}
	if reply != "" {
		return reply, "ok"
	}
	if msg := formatCLINonJSONError(nonJSON, waitErr); msg != "" {
		return msg, "cli_error"
	}
	return "(空回复)", "empty"
}

func formatCLINonJSONError(nonJSON []string, waitErr error) string {
	var parts []string
	for _, line := range nonJSON {
		line = strings.TrimSpace(line)
		if line != "" {
			parts = append(parts, line)
		}
	}
	if waitErr != nil {
		if msg := strings.TrimSpace(waitErr.Error()); msg != "" {
			parts = append(parts, msg)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	msg := strings.Join(parts, "\n")
	return "(空回复: " + clipRunes(msg, 500) + ")"
}

func logCLINonJSON(botID, engine string, lines []string) {
	if len(lines) == 0 {
		return
	}
	joined := strings.Join(lines, " | ")
	log.Printf("bot=%s %s: non-json output: %s", botID, engine, clipRunes(joined, 400))
}
