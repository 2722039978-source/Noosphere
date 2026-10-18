import assert from "node:assert/strict";
import test from "node:test";
import { loadSource } from "./load-source.mjs";
const { createStreamRunner } = await loadSource("../src/features/chat/stream-runner.ts");
const tick = () => new Promise(setImmediate);
const deferred = () => { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; };
function harness(t, overrides = {}) {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const events = [], receivers = new Map(), cancels = [], errors = [];
  const runner = createStreamRunner({
    start: async (run, _messages, receive) => { receivers.set(run.requestId, receive); },
    cancel: async (id) => { cancels.push(id); }, active: (run) => events.push(["active", run?.phase ?? null]),
    append: (run, text) => events.push(["append", run.requestId, text]),
    finish: (run, status, error, content) => events.push(["finish", run.requestId, status, error, content]),
    cancelError: (id, error) => errors.push([id, error]), ...overrides,
  });
  t.after(async () => { await runner.cancel().catch(() => {}); });
  const input = (id = "a", options = {}) => ({ requestId: id, sessionId: "s", messageId: `m-${id}`, modelId: "fake", begin() {}, prepare: async () => [{ role: "user", content: "test" }], ...options });
  return { runner, input, events, receivers, cancels, errors, terminal: () => events.filter((e) => e[0] === "finish"), text: () => events.filter((e) => e[0] === "append").map((e) => e[2]).join("") };
}
test("burst tokens are batched, remain ordered, and flush on terminal", async (t) => {
  const h = harness(t); h.runner.start(h.input()); await tick();
  h.receivers.get("a")({ token: "你" }); h.receivers.get("a")({ token: "好" });
  assert.equal(h.text(), ""); t.mock.timers.tick(32); assert.equal(h.text(), "你好");
  h.receivers.get("a")({ token: "！" }); h.receivers.get("a")({ done: true });
  assert.equal(h.text(), "你好！"); assert.equal(h.terminal().length, 1); assert.equal(h.runner.active, null);
});
test("final snapshot replaces streamed text without duplication, including an empty final", async (t) => {
  const h = harness(t); h.runner.start(h.input()); await tick();
  h.receivers.get("a")({ token: "old" }); h.receivers.get("a")({ done: true, content: "authoritative" });
  t.mock.timers.tick(100); assert.equal(h.text(), ""); assert.equal(h.terminal()[0][4], "authoritative");
  h.runner.start(h.input("b")); await tick(); h.receivers.get("b")({ done: true, content: "" }); assert.equal(h.terminal()[1][4], "");
});
test("a final-only Gemini answer is kept", async (t) => {
  const h = harness(t); h.runner.start(h.input()); await tick();
  h.receivers.get("a")({ done: true, status: "completed", content: "完整回答 🌐" });
  assert.equal(h.terminal()[0][4], "完整回答 🌐");
});
test("stop keeps partial output and rejects every late callback after a new run", async (t) => {
  const h = harness(t); h.runner.start(h.input()); await tick(); const old = h.receivers.get("a");
  old({ token: "partial" }); await h.runner.cancel();
  assert.equal(h.text(), "partial"); assert.equal(h.terminal()[0][2], "cancelled"); assert.deepEqual(h.cancels, ["a"]);
  h.runner.start(h.input("b")); await tick(); old({ token: "LEAK" }); old({ done: true, error: "late" });
  assert.equal(h.runner.active.requestId, "b"); assert.equal(h.text(), "partial"); assert.equal(h.terminal().length, 1);
});
test("stop during context retrieval aborts preparation and never starts HTTP", async (t) => {
  const task = deferred(); let signal;
  const h = harness(t); h.runner.start(h.input("a", { prepare: (value) => { signal = value; return task.promise; } }));
  assert.equal(h.runner.start(h.input("b")), false); await h.runner.cancel();
  assert.ok(signal.aborted); task.resolve([]); await tick(); assert.equal(h.receivers.size, 0); assert.deepEqual(h.cancels, []);
});
test("fast cancellation waits for registration ACK before cancel RPC", async (t) => {
  const ack = deferred(); const order = [];
  const h = harness(t, { start: () => { order.push("start"); return ack.promise; }, cancel: async () => { order.push("cancel"); } });
  h.runner.start(h.input()); await tick(); const stopped = h.runner.cancel(); await tick(); assert.deepEqual(order, ["start"]);
  ack.resolve(); await stopped; assert.deepEqual(order, ["start", "cancel"]);
});
test("request IDs isolate events and a terminal is accepted exactly once", async (t) => {
  const h = harness(t); h.runner.start(h.input()); await tick(); const receive = h.receivers.get("a");
  receive({ request_id: "b", token: "wrong", done: true }); assert.ok(h.runner.active);
  receive({ request_id: "a", done: true }); receive({ error: "duplicate" }); assert.equal(h.terminal().length, 1); assert.equal(h.text(), "");
});
test("an interrupted stream keeps partial text and reports failure", async (t) => {
  const h = harness(t); h.runner.start(h.input()); await tick();
  h.receivers.get("a")({ token: "keep" }); h.receivers.get("a")({ status: "failed", error: "unexpected EOF", done: true });
  assert.equal(h.text(), "keep"); assert.equal(h.terminal()[0][2], "failed"); assert.equal(h.terminal()[0][3], "unexpected EOF");
});
test("deadline cancels the transport and cannot overwrite a later run", async (t) => {
  const h = harness(t, { timeoutMs: 100 }); h.runner.start(h.input()); await tick(); t.mock.timers.tick(100); await tick();
  assert.deepEqual(h.cancels, ["a"]); assert.equal(h.terminal()[0][2], "failed");
  h.runner.start(h.input("b")); await tick(); h.receivers.get("a")({ done: true }); assert.equal(h.runner.active.requestId, "b");
});
test("failed invocation releases the active run for retry", async (t) => {
  const h = harness(t, { start: async () => { throw new Error("bridge unavailable"); } }); h.runner.start(h.input()); await tick();
  assert.equal(h.runner.active, null); assert.equal(h.terminal()[0][3], "bridge unavailable"); assert.equal(h.runner.start(h.input("b")), true); await tick();
});
test("begin may notify store subscribers before the placeholder exists", async (t) => {
  const h = harness(t); let observed;
  h.runner.start(h.input("a", { begin: () => { observed = h.runner.active; assert.equal(h.runner.start(h.input("b")), false); } }));
  assert.equal(observed, null); assert.equal(h.runner.active.requestId, "a"); await tick();
});
test("cancel errors are reported and shutdown awaits in-flight cancellation", async (t) => {
  const remote = deferred(); const h = harness(t, { cancel: () => remote.promise });
  h.runner.start(h.input()); await tick(); const stopped = h.runner.cancel().catch(() => {});
  let settled = false; const shutdown = h.runner.settleCancellations().then(() => { settled = true; });
  await tick(); assert.equal(settled, false); remote.reject(new Error("offline")); await stopped; await shutdown;
  assert.equal(h.errors.length, 1); assert.equal(h.runner.active, null);
});
