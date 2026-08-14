export type WebhookSkip = {
  botId: string;
  /** Skip this channel index. Omit (and set root) to skip a bot's top-level send_msg_url. */
  channelIndex?: number;
  root?: boolean;
};

function tokenFromSendUrl(url: string): string {
  const raw = url.trim();
  try {
    return new URL(raw).searchParams.get("yzjtoken")?.trim() || "";
  } catch {
    const m = raw.match(/[?&]yzjtoken=([^&]*)/i);
    return m ? decodeURIComponent(m[1]).trim() : "";
  }
}

function placeholderToken(token: string): boolean {
  const t = token.trim();
  return t === "" || t === "REPLACE_ME" || t === "REPLACE_ME_YZJTOKEN";
}

function normalizeSendUrl(url: string): string {
  const raw = url.trim();
  try {
    const u = new URL(raw);
    u.hash = "";
    u.hostname = u.hostname.toLowerCase();
    u.protocol = u.protocol.toLowerCase();
    if (u.pathname.endsWith("/") && u.pathname.length > 1) {
      u.pathname = u.pathname.slice(0, -1);
    }
    return u.toString();
  } catch {
    return raw.toLowerCase();
  }
}

function ownerLabel(botId: string, group: string): string {
  return group ? `${botId} / ${group}` : botId;
}

/**
 * Returns a Chinese error if candidateUrl's send URL or yzjtoken is already
 * used by another bot/channel. skip identifies the row currently being edited.
 */
export function findWebhookConflict(
  bots: Record<string, unknown>[],
  candidateUrl: string,
  skip?: WebhookSkip,
): string | null {
  const cand = candidateUrl.trim();
  if (!cand) return null;
  const candUrl = normalizeSendUrl(cand);
  const candTok = tokenFromSendUrl(cand);

  for (const bot of bots) {
    const botId = String(bot.id || "");
    const channels = Array.isArray(bot.channels) ? (bot.channels as Record<string, unknown>[]) : null;
    if (channels && channels.length) {
      for (let i = 0; i < channels.length; i++) {
        if (skip && skip.botId === botId && skip.channelIndex === i) continue;
        const ch = channels[i] || {};
        const url = String(ch.send_msg_url || "");
        const group = String(ch.group || "");
        const hit = conflictAgainst(candUrl, candTok, url);
        if (hit) return `${hit}已被通道 ${ownerLabel(botId, group)} 使用`;
      }
      continue;
    }
    if (skip && skip.botId === botId && skip.root) continue;
    const url = String(bot.send_msg_url || "");
    const group = String(bot.group || "");
    const hit = conflictAgainst(candUrl, candTok, url);
    if (hit) return `${hit}已被通道 ${ownerLabel(botId, group)} 使用`;
  }
  return null;
}

function conflictAgainst(candUrl: string, candTok: string, otherUrl: string): string | null {
  const other = otherUrl.trim();
  if (!other) return null;
  if (normalizeSendUrl(other) === candUrl) return "该 send_msg_url ";
  const tok = tokenFromSendUrl(other);
  if (!placeholderToken(candTok) && !placeholderToken(tok) && candTok === tok) {
    return "该 yzjtoken ";
  }
  return null;
}
