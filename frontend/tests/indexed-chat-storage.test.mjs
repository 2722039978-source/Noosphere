import test from 'node:test';
import assert from 'node:assert/strict';
import { IDBFactory, IDBDatabase } from 'fake-indexeddb';
import { loadSource } from './load-source.mjs';
const { createIndexedChatStorage } = await loadSource('../src/features/chat/indexed-chat-storage.ts');
const { parseChatBackup, validateChatSnapshot } = await loadSource('../src/features/chat/chat-backup.ts');
const sample = {sessions:[{id:'one',title:'历史',modelId:'',createdAt:1,updatedAt:1,messages:[{id:'m',role:'user',content:'hello',createdAt:1}]}],activeId:'one'};
const validator = (state) => validateChatSnapshot(state);
function fixture(legacyValue) {
 const factory = new IDBFactory(); const legacy = new Map([['noosphere-chat-v1',legacyValue]]); const errors=[];
 const options={factory:()=>factory,legacy:()=>({getItem:key=>legacy.get(key)??null}),onError:error=>errors.push(error),validate:validator};
 return { options,legacy,errors,storage:createIndexedChatStorage(options) };
}
test('legacy migration preserves original backup and survives a new storage instance', async()=>{
 const raw=JSON.stringify({state:sample,version:0});const f=fixture(raw);
 assert.deepEqual((await f.storage.getItem('noosphere-chat-v1')).state,sample);
 assert.equal(f.legacy.get('noosphere-chat-v1'),raw);
 const storage=createIndexedChatStorage(f.options);
 assert.deepEqual((await storage.getItem('noosphere-chat-v1')).state,sample);
});
test('updates and deletions commit atomically and reload in session order',async()=>{
 const f=fixture(null);await f.storage.getItem('noosphere-chat-v1');
 f.storage.setItem('noosphere-chat-v1',{state:sample,version:0});assert.equal(await f.storage.flush(),true);
 const empty={sessions:[],activeId:null};f.storage.setItem('noosphere-chat-v1',{state:empty,version:0});await f.storage.flush();
 assert.deepEqual((await createIndexedChatStorage(f.options).getItem('noosphere-chat-v1')).state,empty);
});
test('corrupt legacy data blocks overwriting history',async()=>{
 const f=fixture('{broken');await assert.rejects(f.storage.getItem('noosphere-chat-v1'));
 f.storage.setItem('noosphere-chat-v1',{state:sample,version:0});assert.equal(await f.storage.flush(),false);
 assert.equal(f.legacy.get('noosphere-chat-v1'),'{broken');
});
test('backup validation rejects malformed sessions before mutation',()=>{
 assert.throws(()=>parseChatBackup('{"state":{"sessions":[{"id":"bad"}],"activeId":null}}'));
 const imported=parseChatBackup(JSON.stringify({format:'noosphere-chat-backup',version:1,state:sample}));
 assert.deepEqual(imported,sample);
 assert.throws(()=>parseChatBackup(JSON.stringify({format:'noosphere-chat-backup',version:2,state:sample})));
});

test('ordinary archive growth past import limit still hydrates',async()=>{
 const large=structuredClone(sample);large.sessions[0].messages[0].content='x'.repeat(21*1024*1024);
 const f=fixture(null);await f.storage.getItem('noosphere-chat-v1');
 f.storage.setItem('noosphere-chat-v1',{state:large,version:0});assert.equal(await f.storage.flush(),true);
 const loaded=await createIndexedChatStorage(f.options).getItem('noosphere-chat-v1');
 assert.equal(loaded.state.sessions[0].messages[0].content.length,21*1024*1024);
 assert.equal(parseChatBackup(JSON.stringify({format:'noosphere-chat-backup',version:1,state:large})).sessions[0].messages[0].content.length,21*1024*1024);
});

test('failed IndexedDB commit retains the latest state for retry',async()=>{
 const f=fixture(null);await f.storage.getItem('noosphere-chat-v1');
 const transaction=IDBDatabase.prototype.transaction;
 IDBDatabase.prototype.transaction=function(...args){if(args[1]==='readwrite')throw new DOMException('quota','QuotaExceededError');return transaction.apply(this,args)};
 try {f.storage.setItem('noosphere-chat-v1',{state:sample,version:0});assert.equal(await f.storage.flush(),false);assert.equal(f.storage.isDirty(),true);}
 finally {IDBDatabase.prototype.transaction=transaction}
 assert.equal(await f.storage.flush(),true);assert.equal(f.storage.isDirty(),false);
 assert.deepEqual((await createIndexedChatStorage(f.options).getItem('noosphere-chat-v1')).state,sample);
});

test('a newer snapshot during commit is not lost by the earlier completion',async()=>{
 const f=fixture(null);await f.storage.getItem('noosphere-chat-v1');
 f.storage.setItem('noosphere-chat-v1',{state:sample,version:0});const first=f.storage.flush();
 await Promise.resolve();
 const newer=structuredClone(sample);newer.sessions[0].title='newer';
 f.storage.setItem('noosphere-chat-v1',{state:newer,version:0});assert.equal(await first,true);
 assert.deepEqual((await createIndexedChatStorage(f.options).getItem('noosphere-chat-v1')).state,newer);
});
