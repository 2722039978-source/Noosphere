import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import ts from "typescript";

const source = await readFile(new URL("../src/features/chat/session-search.ts", import.meta.url), "utf8");
const { outputText } = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2020 },
});
const { searchSessions } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`);

const message = (id, content, role = "user") => ({ id, content, role, createdAt: 1 });
const session = (id, title, messages = [], updatedAt = 1) => ({
  id, title, messages, updatedAt, createdAt: 1, modelId: "test",
});
const highlighted = (result) => result.match.snippet.slice(result.match.matchStart, result.match.matchEnd);
const ids = (results) => results.map((result) => result.session.id);

test("blank queries list all sessions in stable descending order without reading messages", () => {
  const older = session("old", "Older", [], 1);
  const first = session("first", "First", [], 5);
  const second = session("second", "Second", [], 5);
  Object.defineProperty(first, "messages", { get() { throw new Error("History must remain unread"); } });
  const input = Object.freeze([older, first, second]);
  const results = searchSessions(input, " \t\n　");
  assert.deepEqual(ids(results), ["first", "second", "old"]);
  assert.deepEqual(ids(input.map((value) => ({ session: value }))), ["old", "first", "second"]);
  assert.ok(results.every((result) => result.match === undefined));
});

test("title wins over messages and preserves the original casing", () => {
  const item = session("a", "Explore Noosphere", [message("m1", "noosphere")]);
  const [result] = searchSessions([item], " NOOSPHERE ");
  assert.equal(result.session, item);
  assert.equal(result.match.source, "title");
  assert.equal(result.match.messageId, undefined);
  assert.equal(highlighted(result), "Noosphere");
});

test("uses the first matching message and reports user/assistant sources", () => {
  const userItem = session("user", "Alpha", [message("u1", "开始检索知识"), message("a1", "也有知识", "assistant")]);
  const assistantItem = session("assistant", "Beta", [message("u2", "问题"), message("a2", "使用知识检索", "assistant")]);
  const results = searchSessions([userItem, assistantItem], "知识");
  assert.deepEqual(results.map(({ match }) => [match.source, match.messageId]), [["user", "u1"], ["assistant", "a2"]]);
  assert.ok(results.every((result) => highlighted(result) === "知识"));
});

test("treats regular-expression characters literally and retains internal whitespace", () => {
  const exact = session("exact", "Special", [message("m", "prefix [a.*]+? / middle  space suffix")]);
  const loose = session("loose", "aZZZZ", [message("m", "middle space")]);
  assert.deepEqual(ids(searchSessions([exact, loose], "[a.*]+?")), ["exact"]);
  assert.deepEqual(ids(searchSessions([exact, loose], "middle  space")), ["exact"]);
  assert.deepEqual(searchSessions([exact, loose], "missing"), []);
});

test("sorts nonempty results by update time and preserves ties", () => {
  const items = [session("a", "hit", [], 2), session("b", "hit", [], 4), session("c", "hit", [], 4)];
  assert.deepEqual(ids(searchSessions(items, "HIT")), ["b", "c", "a"]);
  assert.deepEqual(items.map((item) => item.id), ["a", "b", "c"]);
});

test("returns no matches for an empty session list or sessions with empty history", () => {
  assert.deepEqual(searchSessions([], "anything"), []);
  assert.deepEqual(searchSessions([session("a", "", [])], "anything"), []);
});

test("does not change source data and reuses unchanged session results", () => {
  const entry = Object.freeze(message("m", "A cached NEEDLE"));
  const item = Object.freeze(session("a", "Session", Object.freeze([entry])));
  const input = Object.freeze([item]);
  const before = JSON.stringify(input);
  const [first] = searchSessions(input, "needle");
  const [next] = searchSessions(input, " NEEDLE ");
  assert.equal(first, next);
  assert.equal(JSON.stringify(input), before);
});

test("deletions and immutable streaming updates cannot retain stale matches", () => {
  const retained = session("retained", "Remember needle", [], 1);
  const stream = message("stream", "Incomplete ", "assistant");
  const active = session("active", "Current", [stream], 2);
  const [retainedResult] = searchSessions([active, retained], "needle");
  const streamed = { ...active, messages: [{ ...stream, content: "Incomplete needle" }], updatedAt: 3 };
  const updatedResults = searchSessions([streamed, retained], "needle");
  assert.deepEqual(ids(updatedResults), ["active", "retained"]);
  assert.equal(updatedResults[1], retainedResult);
  assert.equal(updatedResults[0].match.messageId, "stream");
  assert.deepEqual(ids(searchSessions([retained], "needle")), ["retained"]);
  const deletedHistory = { ...streamed, messages: [] };
  assert.deepEqual(searchSessions([deletedHistory], "needle"), []);
  assert.equal(stream.content, "Incomplete ");
  assert.deepEqual(searchSessions([active], "needle"), []);
});

test("reuses lowercase text for unchanged messages across streaming session copies", () => {
  let contentReads = 0;
  const oldMessage = { ...message("old", ""), get content() { contentReads++; return "Earlier text"; } };
  const active = session("a", "Current", [oldMessage, message("stream", "still", "assistant")]);
  searchSessions([active], "needle");
  const readsAfterFirst = contentReads;
  const streamed = { ...active, messages: [oldMessage, message("stream", "still needle", "assistant")] };
  assert.equal(searchSessions([streamed], "needle").length, 1);
  assert.equal(contentReads, readsAfterFirst);
  searchSessions([streamed], "earlier");
  assert.equal(contentReads, readsAfterFirst);
});

test("maps lowercase expansions back to original offsets", () => {
  const item = session("a", "İİ 😀 aBc 结束", []);
  assert.equal(highlighted(searchSessions([item], "abc")[0]), "aBc");
  assert.equal(highlighted(searchSessions([item], "İİ")[0]), "İİ");
  assert.equal(highlighted(searchSessions([item], "i")[0]), "İ");
  assert.equal(highlighted(searchSessions([item], "\u0307")[0]), "İ");
  assert.equal(highlighted(searchSessions([item], "😀")[0]), "😀");
});

test("clips long Unicode snippets safely and keeps correct highlight offsets", () => {
  const text = "😀中文".repeat(40) + "İ目标🚀ABC" + "🌏上下文".repeat(40);
  const item = session("a", "Long history", [message("m", text, "assistant")]);
  const [result] = searchSessions([item], "目标🚀abc");
  assert.ok(result.match.snippet.startsWith("…"));
  assert.ok(result.match.snippet.endsWith("…"));
  assert.ok(Array.from(result.match.snippet).length <= 92);
  assert.equal(highlighted(result), "目标🚀ABC");
  assert.equal(result.match.snippet, Buffer.from(result.match.snippet).toString("utf8"));
});

test("keeps a middle-of-title Chinese match within three leading code points", () => {
  const title = "很长的历史会话标题".repeat(20) + "目标词" + "后续说明内容".repeat(20);
  const [result] = searchSessions([session("a", title)], "目标词");
  const leadIn = result.match.snippet.slice(0, result.match.matchStart);
  assert.equal(leadIn, "…" + Array.from(title.split("目标词")[0]).slice(-3).join(""));
  assert.equal(Array.from(leadIn).length, 4);
  assert.equal(highlighted(result), "目标词");
  assert.equal(Array.from(result.match.snippet).length, 92);
});

for (const role of ["user", "assistant"]) {
  test(`keeps a middle-of-message Chinese match within eight leading code points (${role})`, () => {
    const content = "很长的中文消息内容".repeat(20) + "目标词" + "后续说明内容".repeat(20);
    const [result] = searchSessions([session("a", "历史会话", [message("m", content, role)])], "目标词");
    const leadIn = result.match.snippet.slice(0, result.match.matchStart);
    assert.equal(leadIn, "…" + Array.from(content.split("目标词")[0]).slice(-8).join(""));
    assert.equal(Array.from(leadIn).length, 9);
    assert.equal(highlighted(result), "目标词");
    assert.equal(Array.from(result.match.snippet).length, 92);
  });
}

test("includes an entire query longer than the normal snippet budget", () => {
  const query = "搜索😀".repeat(40);
  const item = session("a", "History", [message("m", `前文${query}后文`)]);
  const [result] = searchSessions([item], query);
  assert.equal(highlighted(result), query);
  assert.equal(result.match.snippet, `…${query}…`);
});
