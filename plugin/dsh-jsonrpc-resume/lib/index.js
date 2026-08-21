/**
 * @bridge/dsh-jsonrpc-resume — thin extension of the official SDK server.
 *
 * Adds to the official protocol:
 *   1. `session/resume { sessionId, contentBlocks? } -> { messageId? }`
 *      — cross-process session restore (see below).
 *   2. optional `cwd` on `session/prompt { sessionId, cwd?, contentBlocks }`
 *      — per-session working directory at creation time.
 *
 * Everything else (initialize / shutdown, all four notifications, exit
 * lifecycle) is inherited unchanged from `@deepseek-ai/dsh-sdk-jsonrpc-server`:
 * this plugin only subclasses the official server and reuses the official
 * line transport + apply wiring.
 *
 * Why resume: the official design keeps process lifetime == session lifetime
 * and does not expose `ctx.agents.resume()`, so an out-of-process client
 * cannot continue a session after the process exits. When the official
 * protocol grows a resume method, delete this plugin and switch the profile
 * row back to the official package — zero other changes.
 *
 * Why cwd: the bridge runs one shared pool process for all bots and wants
 * each bot session to work in its own directory. The DSH toolchain treats
 * `session.header.cwd` as authoritative (dsh-sandbox-policy / dsh-tool-bash
 * resolve it first, falling back to the process cwd), so the pool process
 * itself runs in a neutral directory and the bridge passes each session's
 * workspace as `cwd` on the creating `session/prompt`. `meta.cwd` is baked
 * into the persisted session header, so a later cross-process `session/resume`
 * restores the same working directory from disk without any extra parameter.
 * Clients that never send `cwd` get the official behavior (process cwd).
 */
import Schema from '@deepseek-ai/schemastery'
import { JsonRpcLineTransport } from '@deepseek-ai/dsh-sdk-protocol'
import { createUserMessage } from '@deepseek-ai/dsh-llm'
import { SessionId } from '@deepseek-ai/dsh-session'
import { HarnessSdkJsonRpcServer } from '@deepseek-ai/dsh-sdk-jsonrpc-server'

export const name = 'sdk-jsonrpc-server'
export const inject = ['agents']
export const Config = Schema.object({ maxTokensAsSuccess: Schema.boolean().default(false) })

class ResumableServer extends HarnessSdkJsonRpcServer {
  /** Dispatch one request; session/resume is ours, everything else official. */
  async handleRequest(method, params) {
    if (method === 'session/resume') return this.resume(params)
    return super.handleRequest(method, params)
  }

  /** Official prompt(), extended to thread an optional per-session cwd. */
  async prompt(params) {
    const rec = await this.getOrCreateSession(params.sessionId, params.cwd)
    if (this.ctx.agents.get(rec.handle.agent.id) !== rec.handle.agent) {
      throw new Error(`session agent was disposed outside the server: ${params.sessionId}`)
    }
    const message = createUserMessage({
      content: params.contentBlocks,
      source: { kind: 'user' },
    })
    rec.handle.agent.followup(message)
    return { messageId: message.id }
  }

  /** Official getOrCreateSession(), extended to thread the cwd. */
  async getOrCreateSession(sessionId, cwd) {
    if (this.shuttingDown) throw new Error('SDK server is shutting down')
    const existing = this.sessions.get(sessionId)
    if (existing) return existing
    const pending = this.sessionCreations.get(sessionId)
    if (pending) return pending
    const creation = this.createSession(sessionId, cwd)
    this.sessionCreations.set(sessionId, creation)
    creation.then(
      () => {
        this.sessionCreations.delete(sessionId)
      },
      () => {
        this.sessionCreations.delete(sessionId)
      },
    )
    return creation
  }

  /**
   * Official createSession(), extended with an optional per-session cwd.
   * `meta.cwd` is the authoritative working directory for the DSH toolchain
   * and is persisted into the session header (restored on session/resume).
   */
  async createSession(sessionId, cwd) {
    const rec = {
      handle: await this.ctx.agents.create({
        sessionId: SessionId(sessionId),
        meta: { cwd: cwd ?? this.cwd },
        agentOptions: {
          provider: this.provider,
          model: this.model,
          ...(this.maxTokens === undefined ? {} : { maxTokens: this.maxTokens }),
        },
      }),
    }
    this.sessions.set(sessionId, rec)
    return rec
  }

  /** Restore a persisted session (and optionally enqueue a user prompt). */
  async resume(params) {
    if (this.shuttingDown) throw new Error('SDK server is shutting down')
    const { sessionId, contentBlocks } = params
    let rec = this.sessions.get(sessionId)
    if (!rec) {
      const handle = await this.ctx.agents.resume({
        resumeSessionId: sessionId,
        agentOptions: {
          provider: this.provider,
          model: this.model,
          ...(this.maxTokens === undefined ? {} : { maxTokens: this.maxTokens }),
        },
      })
      rec = { handle }
      this.sessions.set(sessionId, rec)
    }
    if (contentBlocks !== undefined && contentBlocks.length > 0) {
      const message = createUserMessage({ content: contentBlocks, source: { kind: 'user' } })
      rec.handle.agent.followup(message)
      return { messageId: message.id }
    }
    return { messageId: undefined }
  }
}

/** Faithful copy of the official apply(); only the server class differs. */
export function apply(ctx, config) {
  const resolvedConfig = config
  const rootFiber = ctx.root.fiber
  const input = config.input ?? process.stdin
  const output = config.output ?? process.stdout
  const exit = config.exit ?? ((code) => process.exit(code))
  const transport = new JsonRpcLineTransport(input, output)
  const server = new ResumableServer(ctx, transport, { maxTokensAsSuccess: resolvedConfig.maxTokensAsSuccess })
  let exitTask
  const disposeAndExit = () => {
    exitTask ??= (async () => {
      await Promise.allSettled([Promise.resolve().then(() => transport.flush())])
      await Promise.allSettled([Promise.resolve().then(() => rootFiber.dispose())])
      exit(0)
    })()
    return exitTask
  }
  transport.onRequest(async (method, params) => {
    if (method === 'initialize' || method === 'session/resume') await ctx.get('loader')?.await()
    const result = await server.handleRequest(method, params)
    if (method === 'shutdown') setImmediate(() => disposeAndExit())
    return result
  })
  ctx.effect(() => {
    transport.start()
    return async () => {
      await server.shutdown()
      transport.close()
    }
  }, 'jsonrpc.serve')
}
