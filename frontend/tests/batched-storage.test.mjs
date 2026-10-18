import assert from "node:assert/strict";
import test from "node:test";
import { loadSource } from "./load-source.mjs";
const { createBatchedStorage } = await loadSource("../src/features/chat/batched-storage.ts");
function harness(t) {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const data = new Map(), errors = [], writes = [];
  let fails = false;
  const storage = createBatchedStorage(() => ({ getItem: (key) => data.get(key) ?? null, removeItem: (key) => data.delete(key), setItem: (key, value) => { if (fails) throw new Error("quota"); writes.push([key, value]); data.set(key, value); } }), (error) => errors.push(error), 250);
  return { storage, data, errors, writes, fail: (value) => { fails = value; } };
}
test("batches serialization and persists only the newest snapshot", (t) => {
  const h = harness(t); let serializations = 0;
  for (let count = 0; count < 1000; count++) h.storage.setItem("chat", { state: { toJSON() { serializations++; return { count }; } }, version: 1 });
  assert.equal(serializations, 0); assert.equal(h.writes.length, 0);
  t.mock.timers.tick(250); assert.equal(serializations, 1); assert.equal(h.writes.length, 1);
  assert.deepEqual(h.storage.getItem("chat"), { state: { count: 999 }, version: 1 });
});
test("explicit flush saves terminal data immediately and clears its timer", (t) => {
  const h = harness(t); h.storage.setItem("chat", { state: { status: "cancelled", content: "partial" } });
  assert.equal(h.storage.flush(), true); assert.equal(h.writes.length, 1);
  t.mock.timers.tick(500); assert.equal(h.writes.length, 1);
});
test("quota failure retains the latest snapshot for a successful retry", (t) => {
  const h = harness(t); h.fail(true); h.storage.setItem("chat", { state: "old" });
  assert.equal(h.storage.flush(), false); assert.ok(h.errors.at(-1));
  h.storage.setItem("chat", { state: "latest" }); h.fail(false);
  assert.equal(h.storage.flush(), true); assert.deepEqual(h.storage.getItem("chat"), { state: "latest" }); assert.equal(h.errors.at(-1), null);
});
test("removing a pending value prevents a delayed write from resurrecting it", (t) => {
  const h = harness(t); h.storage.setItem("chat", { state: "deleted" }); h.storage.removeItem("chat"); t.mock.timers.tick(500);
  assert.equal(h.storage.getItem("chat"), null); assert.equal(h.writes.length, 0);
});
test("reads legacy JSON and reports invalid storage without throwing", (t) => {
  const h = harness(t); h.data.set("chat", JSON.stringify({ state: { sessions: [], activeId: null }, version: 0 }));
  assert.equal(h.storage.getItem("chat").version, 0); h.data.set("chat", "invalid"); assert.equal(h.storage.getItem("chat"), null); assert.ok(h.errors.at(-1));
});
test("a read failure cannot silently overwrite the original data on hydration", (t) => {
  const h = harness(t); h.data.set("chat", "unreadable original");
  assert.equal(h.storage.getItem("chat"), null);
  h.storage.setItem("chat", { state: { sessions: [] } }); t.mock.timers.tick(250);
  assert.equal(h.storage.flush(), false); assert.equal(h.data.get("chat"), "unreadable original"); assert.equal(h.writes.length, 0);
});
