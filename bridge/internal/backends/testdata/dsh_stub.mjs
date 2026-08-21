#!/usr/bin/env node
/**
 * dsh_stub.mjs — 离线 DSH JSON-RPC server stub（bridge backend 冒烟用）。
 *
 * 模拟 @bridge/dsh-jsonrpc-resume 的协议面，不碰真实 DSH / LLM：
 *   - initialize          -> { serverInfo }
 *   - session/prompt      -> 断言 cwd 参数传入并回显（首条创建）
 *                           回复 pong:<cwd>:<text>；未知 sessionId 惰性创建
 *   - session/resume      -> 已知 sid 恢复（回复 resumed:<cwd>:<text>），
 *                           未知 sid 回错误 `session "<id>" not found`
 *   - session.event       -> turn/start → assistant/message(text) → turn/end(completed)
 *   - shutdown            -> 应答后 exit 0
 *
 * 会话注册表通过 DSH_STUB_STATE_FILE（JSONL，一行一个 {sessionId,cwd}）跨进程持久化，
 * 模拟真实 DSH 的磁盘会话，供「resume 已知」用例在进程重建后命中。
 */
import { createInterface } from 'node:readline'
import { existsSync, readFileSync, writeFileSync } from 'node:fs'

const STATE_FILE = process.env.DSH_STUB_STATE_FILE || ''

// sid -> { sessionId, cwd }
const state = new Map()
if (STATE_FILE && existsSync(STATE_FILE)) {
  try {
    for (const line of readFileSync(STATE_FILE, 'utf8').split('\n')) {
      if (!line.trim()) continue
      const rec = JSON.parse(line)
      state.set(String(rec.sessionId), rec)
    }
  } catch {
    /* 状态文件损坏时从空开始 */
  }
}

function persist(rec) {
  if (!STATE_FILE) return
  writeFileSync(STATE_FILE, JSON.stringify(rec) + '\n', { flag: 'a' })
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
      data: { message: { content: [{ type: 'text', text }] } },
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
      respond(id, { serverInfo: { name: 'dsh-stub', version: '0.0.1' } })
      break
    case 'session/prompt': {
      const sid = String(params.sessionId)
      const cwd = typeof params.cwd === 'string' ? params.cwd : ''
      if (!state.has(sid)) {
        const rec = { sessionId: sid, cwd }
        state.set(sid, rec)
        persist(rec)
      }
      respond(id, { messageId: 'msg-' + nextMsg++ })
      emitTurn(sid, `pong:${state.get(sid).cwd}:${textOf(params.contentBlocks)}`, 'completed')
      break
    }
    case 'session/resume': {
      const sid = String(params.sessionId)
      const rec = state.get(sid)
      if (!rec) {
        respondError(id, `session "${sid}" not found`)
        break
      }
      respond(id, { messageId: 'msg-' + nextMsg++ })
      emitTurn(sid, `resumed:${rec.cwd}:${textOf(params.contentBlocks)}`, 'completed')
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
