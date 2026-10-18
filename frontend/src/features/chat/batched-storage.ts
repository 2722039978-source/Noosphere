import type { PersistStorage, StorageValue } from "zustand/middleware";

export class StorageReadError extends Error {
  constructor() { super("Saved conversations could not be read; writes are paused to protect the original data."); }
}

/** Delay serialization as well as the synchronous write; keep only the latest snapshot. */
export function createBatchedStorage<T>(
  getStorage: () => Pick<Storage, "getItem" | "setItem" | "removeItem">,
  onError: (error: unknown | null) => void,
  delay = 250,
): PersistStorage<T> & { flush: () => boolean } {
  const pending = new Map<string, StorageValue<T>>();
  const unreadable = new Map<string, StorageReadError>();
  let timer: ReturnType<typeof setTimeout> | undefined;
  const flush = () => {
    clearTimeout(timer);
    timer = undefined;
    if (unreadable.size) { onError(unreadable.values().next().value); return false; }
    try {
      for (const [name, value] of pending) {
        getStorage().setItem(name, JSON.stringify(value));
        pending.delete(name);
      }
      onError(null);
      return true;
    } catch (error) {
      // Retain the latest snapshot so a user-triggered retry can recover it.
      onError(error);
      return false;
    }
  };
  return {
    getItem(name) {
      try {
        const raw = getStorage().getItem(name);
        const value = raw ? JSON.parse(raw) : null;
        unreadable.delete(name);
        return value;
      } catch (error) {
        const failure = new StorageReadError();
        unreadable.set(name, failure);
        onError(failure);
        return null;
      }
    },
    setItem(name, value) {
      pending.set(name, value);
      timer ??= setTimeout(flush, delay);
    },
    removeItem(name) {
      pending.delete(name);
      try { getStorage().removeItem(name); unreadable.delete(name); } catch (error) { onError(error); }
    },
    flush,
  };
}
