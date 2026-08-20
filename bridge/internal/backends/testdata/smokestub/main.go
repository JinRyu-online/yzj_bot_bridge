// 冒烟测试用假 CLI：模拟 Cursor/Claude 的最小 stdout 协议。
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "create-chat" {
		fmt.Println("smoke-session")
		return
	}
	if os.Getenv("SMOKE_PLAIN_ERROR") == "1" {
		fmt.Println("error: unknown option '- bad prompt'")
		os.Exit(1)
	}
	prompt := extractPrompt(os.Args)
	result := "pong"
	if prompt != "" && prompt != "ping" {
		result = "echo:" + prompt
	}
	fmt.Println(`{"type":"system","subtype":"init","session_id":"smoke-session"}`)
	fmt.Print(`{"type":"assistant","message":{"content":[{"type":"text","text":`)
	enc, _ := json.Marshal(result)
	fmt.Println(string(enc) + `}]}}`)
	fmt.Print(`{"type":"result","result":`)
	fmt.Println(string(jsonMarshal(result)) + "}")
}

func jsonMarshal(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func extractPrompt(args []string) string {
	for i, a := range args {
		if a == "--" && i+1 < len(args) {
			return strings.Join(args[i+1:], " ")
		}
	}
	return ""
}
