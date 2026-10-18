import test from 'node:test';
import assert from 'node:assert/strict';
import { loadSource } from './load-source.mjs';
const backup = await loadSource('../src/features/chat/chat-backup.ts');
const session = (id, workspaceId) => ({id, workspaceId, title:id, modelId:'', messages:[],createdAt:1,updatedAt:1});

test('workspace filtering keeps legacy history local and never falls back to another workspace', () => {
  const sessions = [session('old'), session('a','alpha'), session('b','beta')];
  assert.deepEqual(backup.sessionsInWorkspace(sessions,'local').map(s=>s.id), ['old']);
  assert.deepEqual(backup.sessionsInWorkspace(sessions,'alpha').map(s=>s.id), ['a']);
  assert.equal(backup.activeWorkspaceSession(sessions,'b','alpha')?.id, 'a');
  assert.equal(backup.activeWorkspaceSession(sessions,'b','missing'), null);
  assert.equal(sessions.length,3);
});

test('import and export retain workspace ownership while unscoped archives remain local', () => {
  const current={sessions:[session('a','alpha')],activeId:'a'};
  const imported=backup.parseChatBackup(JSON.stringify({format:'noosphere-chat-backup',version:1,state:{sessions:[session('old'),session('b','beta')],activeId:'b'}}));
  const merged=backup.mergeChatBackup(current, imported);
  const roundtrip=backup.parseChatBackup(JSON.stringify({format:'noosphere-chat-backup',version:1,state:merged}));
  assert.deepEqual(backup.sessionsInWorkspace(roundtrip.sessions,'local').map(s=>s.id),['old']);
  assert.deepEqual(backup.sessionsInWorkspace(roundtrip.sessions,'beta').map(s=>s.id),['b']);
  assert.equal(roundtrip.activeId,'a');
});

test('malformed workspace ownership is rejected before import', () => {
  for (const workspaceId of [12, {}, '', '../alpha', 'alpha?x=1']) {
    assert.throws(()=>backup.validateChatSnapshot({sessions:[session('bad',workspaceId)],activeId:'bad'}));
  }
});

test('citation snapshots roundtrip and malformed evidence cannot break rendering', () => {
  const message={id:'m',role:'assistant',content:'Answer [R1]',createdAt:1,citations:[{id:'R1',kind:'code',title:'a.py',excerpt:'def a(): pass',workspace_id:'alpha',file:'a.py',source_hash:'a'.repeat(64)}],contextWarnings:['reindex'],retrievalMode:'lexical'};
  const state={sessions:[{...session('a','alpha'),messages:[message]}],activeId:'a'};
  assert.deepEqual(backup.parseChatBackup(JSON.stringify({state})).sessions[0].messages[0].citations,message.citations);
  for (const patch of [{citations:{}},{citations:[null]},{citations:[{...message.citations[0],title:{}}]},{contextWarnings:[{}]},{retrievalMode:'pretend'}]) {
    assert.throws(()=>backup.validateChatSnapshot({...state,sessions:[{...state.sessions[0],messages:[{...message,...patch}]}]}));
  }
});
