// 冒烟测试用假 CLI：模拟 Cursor/Claude 的最小 stdout 协议。
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "create-chat" {
		fmt.Println("smoke-session")
		return
	}
	fmt.Println(`{"type":"system","subtype":"init","session_id":"smoke-session"}`)
	fmt.Println(`{"type":"assistant","message":{"content":[{"type":"text","text":"pong"}]}}`)
	fmt.Println(`{"type":"result","result":"pong"}`)
}
