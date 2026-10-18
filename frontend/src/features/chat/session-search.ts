import type { ChatMessage, ChatSession } from "./chat-store";

export interface SessionSearchMatch {
  messageId?: string;
  source: "title" | "user" | "assistant";
  snippet: string;
  /** UTF-16 offsets into snippet, suitable for String.slice. */
  matchStart: number;
  matchEnd: number;
}

export interface SessionSearchResult {
  session: ChatSession;
  match?: SessionSearchMatch;
}

interface OffsetChange {
  foldedStart: number;
  foldedEnd: number;
  originalStart: number;
  originalEnd: number;
}

interface SearchText {
  original: string;
  folded: string;
  offsetChanges: OffsetChange[];
}

interface TextMatch {
  start: number;
  end: number;
}

interface MessageCache {
  text: SearchText;
  query?: string;
  match: TextMatch | null;
}

interface SessionCache {
  title?: SearchText;
  query?: string;
  result: SessionSearchResult | null;
  emptyResult: SessionSearchResult;
}

// Store updates replace changed sessions/messages. Weak keys release deleted or
// superseded objects, and each entry retains only its most recent query result.
const sessionCache = new WeakMap<ChatSession, SessionCache>();
const messageCache = new WeakMap<ChatMessage, MessageCache>();
const SNIPPET_LENGTH = 90;

function indexText(original: string): SearchText {
  const folded = original.toLowerCase();
  const offsetChanges: OffsetChange[] = [];

  // Lowercasing can expand a character (e.g. İ -> i + combining dot). Keep
  // sparse offset changes so matches still highlight the original character.
  if (folded.length !== original.length) {
    let originalOffset = 0;
    let foldedOffset = 0;
    for (const character of original) {
      const foldedLength = character.toLowerCase().length;
      if (foldedLength !== character.length) {
        offsetChanges.push({
          foldedStart: foldedOffset,
          foldedEnd: foldedOffset + foldedLength,
          originalStart: originalOffset,
          originalEnd: originalOffset + character.length,
        });
      }
      originalOffset += character.length;
      foldedOffset += foldedLength;
    }
  }

  return { original, folded, offsetChanges };
}

function originalOffset(text: SearchText, index: number, end: boolean): number {
  let delta = 0;
  for (const change of text.offsetChanges) {
    if (index <= change.foldedStart) break;
    if (index < change.foldedEnd) {
      return end ? change.originalEnd : change.originalStart;
    }
    delta = change.foldedEnd - change.originalEnd;
  }
  return index - delta;
}

function isInsideSurrogatePair(text: string, index: number): boolean {
  if (index <= 0 || index >= text.length) return false;
  const before = text.charCodeAt(index - 1);
  const after = text.charCodeAt(index);
  return before >= 0xd800 && before <= 0xdbff && after >= 0xdc00 && after <= 0xdfff;
}

function findMatch(text: SearchText, query: string): TextMatch | null {
  const index = text.folded.indexOf(query);
  if (index < 0) return null;
  let start = originalOffset(text, index, false);
  let end = originalOffset(text, index + query.length, true);
  if (isInsideSurrogatePair(text.original, start)) start--;
  if (isInsideSurrogatePair(text.original, end)) end++;
  return { start, end };
}

function moveByCodePoints(text: string, index: number, count: number): number {
  const direction = count < 0 ? -1 : 1;
  for (let remaining = Math.abs(count); remaining > 0; remaining--) {
    if ((direction < 0 && index === 0) || (direction > 0 && index === text.length)) break;
    index += direction;
    if (isInsideSurrogatePair(text, index)) index += direction;
  }
  return index;
}

function makeSnippet(
  text: string,
  match: TextMatch,
  source: SessionSearchMatch["source"],
): Pick<SessionSearchMatch, "snippet" | "matchStart" | "matchEnd"> {
  const matchLength = Array.from(text.slice(match.start, match.end)).length;
  const context = Math.max(0, SNIPPET_LENGTH - matchLength);
  // Keep the match visible in narrow, clamped sidebar rows. Most context goes
  // after the match, with a shorter lead-in for the single-line session title.
  const before = Math.min(context, source === "title" ? 3 : 8);
  const start = moveByCodePoints(text, match.start, -before);
  const actualBefore = Array.from(text.slice(start, match.start)).length;
  const end = moveByCodePoints(text, match.end, context - actualBefore);
  const prefix = start > 0 ? "…" : "";
  const suffix = end < text.length ? "…" : "";

  return {
    snippet: prefix + text.slice(start, end) + suffix,
    matchStart: prefix.length + match.start - start,
    matchEnd: prefix.length + match.end - start,
  };
}

function searchSession(session: ChatSession, query: string): SessionSearchResult | null {
  let cached = sessionCache.get(session);
  if (!cached) {
    cached = { result: null, emptyResult: { session } };
    sessionCache.set(session, cached);
  }

  // Browsing all sessions must not index or scan message history.
  if (!query) return cached.emptyResult;
  if (cached.query === query) return cached.result;

  cached.query = query;
  cached.title ??= indexText(session.title);
  const titleMatch = findMatch(cached.title, query);
  if (titleMatch) {
    cached.result = {
      session,
      match: { source: "title", ...makeSnippet(session.title, titleMatch, "title") },
    };
    return cached.result;
  }

  for (const message of session.messages) {
    let cachedMessage = messageCache.get(message);
    if (!cachedMessage) {
      cachedMessage = { text: indexText(message.content), match: null };
      messageCache.set(message, cachedMessage);
    }
    if (cachedMessage.query !== query) {
      cachedMessage.query = query;
      cachedMessage.match = findMatch(cachedMessage.text, query);
    }
    if (cachedMessage.match) {
      cached.result = {
        session,
        match: {
          messageId: message.id,
          source: message.role,
          ...makeSnippet(cachedMessage.text.original, cachedMessage.match, message.role),
        },
      };
      return cached.result;
    }
  }

  cached.result = null;
  return null;
}

/** Literal, locale-independent lowercase search over immutable chat objects. */
export function searchSessions(sessions: readonly ChatSession[], query: string): SessionSearchResult[] {
  const foldedQuery = query.trim().toLowerCase();
  const results: SessionSearchResult[] = [];
  for (const session of sessions) {
    const result = searchSession(session, foldedQuery);
    if (result) results.push(result);
  }
  // Array.sort is stable; equal timestamps retain the caller's session order.
  return results.sort((a, b) => b.session.updatedAt - a.session.updatedAt);
}
