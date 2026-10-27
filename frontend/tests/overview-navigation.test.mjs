import test from 'node:test';
import assert from 'node:assert/strict';
import { loadSource } from './load-source.mjs';

test('startup migrates historic archive preferences without resetting graphics or boot', async () => {
  const { normalizeExperiencePreferences } = await loadSource('../src/stores/experience-preferences.ts');
  assert.deepEqual(normalizeExperiencePreferences({ startPage: 'archive', graphics: 'low', boot: false }), { startPage: 'overview', graphics: 'low', boot: false });
  assert.equal(normalizeExperiencePreferences({}).startPage, 'overview');
  assert.equal(normalizeExperiencePreferences(null).startPage, 'overview');
  assert.equal(normalizeExperiencePreferences({ startPage: 'chat' }).startPage, 'chat');
  assert.equal(normalizeExperiencePreferences({ startPage: 'overview', graphics: 'off', boot: true }).graphics, 'off');
});

const configured = [
  { id: 'gateway / explicit', name: 'Configured model', enabled: true },
  { id: 'disabled', name: 'Disabled model', enabled: false },
];
const ready = { models: configured, modelsReady: true, modelsError: null, persistenceReady: true, workspaceReady: true, workspaceId: 'alpha' };

test('model handoff selects the exact decoded Gateway configuration and waits for metadata', async () => {
  const { readModelChatIntent, resolveModelChatIntent } = await loadSource('../src/features/chat/model-chat-intent.ts');
  const intent = readModelChatIntent('?model=gateway%20%2F%20explicit', 'entry', 'alpha');
  assert.equal(intent.modelId, configured[0].id);
  assert.equal(resolveModelChatIntent(intent, { ...ready, models: [], modelsReady: false }).status, 'loading');
  assert.equal(resolveModelChatIntent(intent, { ...ready, models: [], modelsReady: true }).status, 'invalid');
  assert.equal(resolveModelChatIntent(intent, { ...ready, persistenceReady: false }).status, 'loading');
  assert.equal(resolveModelChatIntent(intent, ready).status, 'ready');
  assert.equal(resolveModelChatIntent(intent, ready).model.id, configured[0].id);
  assert.equal(readModelChatIntent('?tab=history', 'entry', 'alpha'), null);
});

test('invalid, disabled, unavailable and changed-workspace intents cannot select a fallback', async () => {
  const { readModelChatIntent, resolveModelChatIntent } = await loadSource('../src/features/chat/model-chat-intent.ts');
  for (const [query, status] of [['?model=missing', 'invalid'], ['?model=disabled', 'disabled'], ['?model=', 'invalid']]) {
    const result = resolveModelChatIntent(readModelChatIntent(query, 'entry', 'alpha'), ready);
    assert.equal(result.status, status);
    assert.ok(result.message.length);
    assert.equal(result.model, undefined);
  }
  const intent = readModelChatIntent('?model=gateway%20%2F%20explicit', 'entry', 'alpha');
  assert.equal(resolveModelChatIntent(intent, { ...ready, models: [], modelsReady: false, modelsError: 'offline' }).status, 'unavailable');
  assert.equal(resolveModelChatIntent(intent, { ...ready, workspaceId: 'beta' }).status, 'workspace-changed');
});

test('a handoff preserves an existing conversation and reuses only an idle empty draft', async () => {
  const { modelChatSessionPlan } = await loadSource('../src/features/chat/model-chat-intent.ts');
  const session = { id: 'history', workspaceId: 'alpha', messages: [{ role: 'user', content: 'keep me' }] };
  assert.deepEqual(modelChatSessionPlan(session, 'alpha', null), { create: true });
  const empty = { ...session, id: 'draft', messages: [] };
  assert.deepEqual(modelChatSessionPlan(empty, 'alpha', null), { create: false, sessionId: 'draft' });
  assert.deepEqual(modelChatSessionPlan(empty, 'alpha', 'draft'), { create: true });
  assert.deepEqual(modelChatSessionPlan(empty, 'beta', null), { create: true });
  assert.equal(session.messages[0].content, 'keep me');
});

test('consumed browser-history intents never overwrite a subsequent manual model choice', async () => {
  const { readModelChatIntent, resolveModelChatIntent, consumedModelChatState } = await loadSource('../src/features/chat/model-chat-intent.ts');
  const intent = readModelChatIntent('?model=gateway%20%2F%20explicit', 'entry', 'alpha');
  const history = consumedModelChatState({ archiveHandoff: false, other: 'preserved' }, intent, 'alpha', 'new-chat');
  assert.equal(history.other, 'preserved');
  assert.equal(resolveModelChatIntent(intent, { ...ready, historyState: history }).status, 'consumed');
  // A new mount receives a new location key but still recognizes the consumed history entry.
  const remounted = readModelChatIntent('?model=gateway%20%2F%20explicit', 'remount', 'alpha');
  assert.equal(resolveModelChatIntent(remounted, { ...ready, historyState: history }).status, 'consumed');
  assert.equal(resolveModelChatIntent(intent, { ...ready, workspaceId: 'beta', historyState: history }).status, 'consumed');
  const next = readModelChatIntent('?model=disabled', 'other-entry', 'alpha');
  assert.equal(resolveModelChatIntent(next, { ...ready, historyState: history }).status, 'disabled');
});
