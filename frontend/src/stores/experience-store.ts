import { create } from 'zustand';
export type GraphicsMode = 'auto' | 'high' | 'low' | 'off';
type Preferences = { graphics: GraphicsMode; boot: boolean; startPage: 'archive' | 'chat' };
const defaults: Preferences = { graphics: 'auto', boot: true, startPage: 'archive' };
function read(): Preferences {
  try {
    const value = JSON.parse(localStorage.getItem('noosphere-experience-v1') || '{}');
    return { graphics: ['auto', 'high', 'low', 'off'].includes(value?.graphics) ? value.graphics : 'auto', boot: typeof value?.boot === 'boolean' ? value.boot : true, startPage: value?.startPage === 'chat' ? 'chat' : 'archive' };
  } catch { return defaults; }
}
export const useExperienceStore = create<Preferences & { update: (value: Partial<Preferences>) => void }>((set, get) => ({
  ...read(),
  update(value) {
    const current = get();
    const next = { graphics: value.graphics ?? current.graphics, boot: value.boot ?? current.boot, startPage: value.startPage ?? current.startPage };
    try { localStorage.setItem('noosphere-experience-v1', JSON.stringify(next)); } catch { /* Session preferences still work. */ }
    set(next);
  },
}));
