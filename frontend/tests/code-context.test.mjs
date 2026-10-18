import test from 'node:test';
import assert from 'node:assert/strict';
import { loadSource } from './load-source.mjs';
const { formatCodeContext } = await loadSource('../src/features/chat/code-context.ts');

test('code context preserves source references and excludes other projects', () => {
  const result = formatCodeContext('code-a', [
    { record: {session_id:'code-a', content:'return target()', metadata:{file:'src/a.py',start_line:'2',end_line:'9'}} },
    { record: {session_id:'code-b', content:'private other project', metadata:{file:'b.py'}} },
  ]);
  assert.equal(result.count, 1);
  assert.match(result.context, /src\/a.py:2-9/);
  assert.match(result.context, /return target/);
  assert.doesNotMatch(result.context, /private other/);
});
test('code context stays within the character budget and marks truncation', () => {
  const result = formatCodeContext('code-a', [{record:{session_id:'code-a',content:'x'.repeat(50000),metadata:{file:'large.py',start_line:'1',end_line:'200'}}}], 1000);
  assert.ok(result.context.length <= 1000);
  assert.match(result.context, /truncated/);
  assert.equal(formatCodeContext('',[]).count,0);
});
