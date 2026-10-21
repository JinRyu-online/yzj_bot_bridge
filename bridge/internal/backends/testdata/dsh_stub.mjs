#!/usr/bin/env node
/**
 * dsh_stub.mjs — 离线 DSH 官方 SDK JSON-RPC 0.2.x server stub（bridge backend 冒烟用）。
 *
 * 协议面（与 @deepseek-ai/dsh-sdk-jsonrpc-server 0.2.1 对齐）：
 *   - initialize                    -> { serverInfo }，记录默认 cwd
 *   - session/working-directory/set -> 创建/更新会话 cwd，回 { cwd }
 *   - session/prompt                -> 回 { messageId }，事件流：turn/start →
 *                                      assistant/message(reasoning + text) → turn/end(completed)
 *   - session/resume                -> 已知 sid：回 {}；未知：unknown SDK session: "<id>"
 *   - shutdown                      -> 应答后 exit 0
 *
 * 会话注册表通过 DSH_STUB_STATE_FILE（JSONL，一行一个 {sessionId,cwd}）跨进程持久化，
 * 模拟真实 DSH 的磁盘会话，供「resume 已知」用例在进程重建后命中。
 * 进程内 resumed 标记让紧接着的 prompt 回复带 resumed: 前缀（供测试断言走了 resume 分支）。
 */
import { createInterface } from 'node:readline'
import { existsSync, readFileSync, writeFileSync } from 'node:fs'

const STATE_FILE = process.env.DSH_STUB_STATE_FILE || ''

// sid -> cwd
const state = new Map()
if (STATE_FILE && existsSync(STATE_FILE)) {
  try {
    for (const line of readFileSync(STATE_FILE, 'utf8').split('\n')) {
      if (!line.trim()) continue
      const rec = JSON.parse(line)
      state.set(String(rec.sessionId), String(rec.cwd || ''))
    }
  } catch {
    /* 状态文件损坏时从空开始 */
  }
}
const resumed = new Set()
let defaultCwd = process.cwd()

function persist(sid, cwd) {
  if (!STATE_FILE) return
  writeFileSync(STATE_FILE, JSON.stringify({ sessionId: sid, cwd }) + '\n', { flag: 'a' })
}

const rl = createInterface({ input: process.stdin, crlfDelay: Infinity })
let nextMsg = 1

function send(obj) {
  process.stdout.write(JSON.stringify(obj) + '\n')
}
function respond(id, result) {
  send({ jsonrpc: '2.0', id, result })
}
function respondError(id, message) {
  send({ jsonrpc: '2.0', id, error: { code: -32000, message } })
}
function notify(method, params) {
  send({ jsonrpc: '2.0', method, params })
}

function emitTurn(sid, text, reason) {
  notify('session.event', { sessionId: sid, event: { type: 'turn/start' } })
  notify('session.event', {
    sessionId: sid,
    event: {
      type: 'assistant/message',
      data: {
        message: {
          content: [
            // reasoning 块模拟真实 DSH 的思考过程（仅供 bridge 日志，不应进回复正文）。
            { type: 'reasoning', text: `let me think: ${text}` },
            { type: 'text', text },
          ],
        },
      },
    },
  })
  notify('session.event', {
    sessionId: sid,
    event: { type: 'turn/end', data: { reason: { kind: reason } } },
  })
}

function textOf(contentBlocks) {
  return (contentBlocks || []).map((b) => b.text ?? b.content ?? '').join('')
}

rl.on('line', (line) => {
  let frame
  try {
    frame = JSON.parse(line)
  } catch {
    return
  }
  const { id, method, params = {} } = frame
  switch (method) {
    case 'initialize':
      if (typeof params.cwd === 'string' && params.cwd) defaultCwd = params.cwd
      respond(id, { serverInfo: { name: 'dsh-stub', version: '0.2.1-stub' } })
      break
    case 'session/working-directory/set': {
      const sid = String(params.sessionId)
      const cwd = String(params.path || '')
      state.set(sid, cwd)
      persist(sid, cwd)
      respond(id, { cwd })
      break
    }
    case 'session/prompt': {
      const sid = String(params.sessionId)
      if (!state.has(sid)) {
        state.set(sid, defaultCwd)
        persist(sid, defaultCwd)
      }
      const prefix = resumed.has(sid) ? 'resumed' : 'pong'
      resumed.delete(sid)
      respond(id, { messageId: 'msg-' + nextMsg++ })
      emitTurn(sid, `${prefix}:${state.get(sid)}:${textOf(params.contentBlocks)}`, 'completed')
      break
    }
    case 'session/resume': {
      const sid = String(params.sessionId)
      if (!state.has(sid)) {
        respondError(id, `unknown SDK session: "${sid}"`)
        break
      }
      resumed.add(sid)
      respond(id, {})
      break
    }
    case 'shutdown':
      respond(id, {})
      setTimeout(() => process.exit(0), 10)
      break
    default:
      respondError(id, `unknown method: ${method}`)
  }
})
rl.on('close', () => process.exit(0))
