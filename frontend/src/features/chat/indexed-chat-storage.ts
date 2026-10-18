import type { PersistStorage, StorageValue } from 'zustand/middleware';

interface Snapshot { sessions: { id: string }[]; activeId: string | null }
interface Options<T> {
  factory: () => IDBFactory;
  legacy: () => Pick<Storage, 'getItem'>;
  validate: (state: unknown) => T;
  onError: (error: unknown | null) => void;
  databaseName?: string;
}

/** Atomic per-session writes, with legacy JSON retained as a recovery backup. */
export function createIndexedChatStorage<T extends Snapshot>(options: Options<T>): PersistStorage<T> & {
  flush: () => Promise<boolean>; isDirty: () => boolean;
} {
  let database: Promise<IDBDatabase> | undefined;
  let pending: StorageValue<T> | undefined;
  let loaded = false;
  let blocked = false;
  let writing = false;
  let chain: Promise<boolean> = Promise.resolve(true);
  let saved = new Map<string, T['sessions'][number]>();
  let timer: ReturnType<typeof setTimeout> | undefined;
  const open = () => database ??= new Promise<IDBDatabase>((resolve, reject) => {
    const request = options.factory().open(options.databaseName ?? 'noosphere-conversations', 1);
    request.onupgradeneeded = () => {
      request.result.createObjectStore('sessions', { keyPath: 'id' });
      request.result.createObjectStore('metadata');
    };
    request.onerror = () => reject(request.error);
    request.onblocked = () => reject(new Error('请关闭其他 Noosphere 窗口后重试。'));
    request.onsuccess = () => { request.result.onversionchange = () => request.result.close(); resolve(request.result); };
  });
  const write = async (value: StorageValue<T>) => {
    const db = await open();
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction(['sessions','metadata'], 'readwrite');
      const store = tx.objectStore('sessions');
      const ids = new Set(value.state.sessions.map((session) => session.id));
      for (const id of saved.keys()) if (!ids.has(id)) store.delete(id);
      for (const session of value.state.sessions) if (saved.get(session.id) !== session) store.put(session);
      tx.objectStore('metadata').put({ ids: [...ids], activeId: value.state.activeId, version: value.version ?? 0 }, 'snapshot');
      tx.oncomplete = () => resolve();
      tx.onabort = tx.onerror = () => reject(tx.error ?? new Error('会话事务未提交。'));
    });
    saved = new Map(value.state.sessions.map((session) => [session.id, session]));
  };
  const flush = (): Promise<boolean> => {
    clearTimeout(timer); timer = undefined;
    chain = chain.then(async () => {
      if (!loaded || blocked) return false;
      writing = true;
      try {
        while (pending) {
          const value = pending;
          await write(value);
          if (pending === value) pending = undefined;
        }
        options.onError(null); return true;
      } catch (error) { options.onError(error); return false; }
      finally { writing = false; }
    });
    return chain;
  };
  return {
    async getItem(name) {
      try {
        const db = await open();
        const snapshot = await new Promise<{meta: {ids:string[];activeId:string|null;version:number} | undefined; sessions:T['sessions']} >((resolve,reject) => {
          const tx = db.transaction(['sessions','metadata'], 'readonly');
          const meta = tx.objectStore('metadata').get('snapshot');
          const sessions = tx.objectStore('sessions').getAll();
          tx.oncomplete = () => resolve({meta:meta.result,sessions:sessions.result});
          tx.onabort = tx.onerror = () => reject(tx.error);
        });
        let value: StorageValue<T> | null = null;
        if (snapshot.meta) {
          const byId = new Map(snapshot.sessions.map((session) => [session.id, session]));
          const ordered = snapshot.meta.ids.map((id) => byId.get(id));
          value = {version:snapshot.meta.version,state:options.validate({sessions:ordered,activeId:snapshot.meta.activeId})};
          saved = new Map(value.state.sessions.map((session) => [session.id,session]));
        } else {
          const raw = options.legacy().getItem(name);
          if (raw) {
            const old = JSON.parse(raw);
            if (old.version != null && old.version !== 0) throw new Error('不支持旧会话版本，原数据已保留。');
            value = {version:0,state:options.validate(old.state)};
            await write(value);
          }
        }
        loaded = true; options.onError(null); return value;
      } catch (error) { blocked = true; options.onError(error); throw error; }
    },
    setItem(_name,value) {
      if (!loaded || blocked) return;
      pending = value;
      timer ??= setTimeout(() => { void flush(); }, 250);
    },
    async removeItem() {
      if (!loaded || blocked) return;
      await flush();
      const db = await open();
      await new Promise<void>((resolve,reject) => {
        const tx=db.transaction(['sessions','metadata'],'readwrite');
        tx.objectStore('sessions').clear();
        // Keep the migration marker so a deleted archive cannot resurrect legacy data.
        tx.objectStore('metadata').put({ids:[],activeId:null,version:0},'snapshot');
        tx.oncomplete=()=>resolve();tx.onabort=tx.onerror=()=>reject(tx.error);
      });
      saved.clear();
    },
    flush,
    isDirty: () => !!pending || writing || blocked,
  };
}
