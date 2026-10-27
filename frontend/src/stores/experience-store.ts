import { create } from 'zustand';
import { normalizeExperiencePreferences, type ExperiencePreferences as Preferences } from './experience-preferences';
export type { GraphicsMode, StartupPage } from './experience-preferences';
function read(): Preferences {
  try {
    const value = JSON.parse(localStorage.getItem('noosphere-experience-v1') || '{}');
    return normalizeExperiencePreferences(value);
  } catch { return normalizeExperiencePreferences(null); }
}
export const useExperienceStore = create<Preferences & { update: (value: Partial<Preferences>) => void }>((set, get) => ({
  ...read(),
  update(value) {
    const current = get();
    const next = normalizeExperiencePreferences({ graphics: value.graphics ?? current.graphics, boot: value.boot ?? current.boot, startPage: value.startPage ?? current.startPage });
    try { localStorage.setItem('noosphere-experience-v1', JSON.stringify(next)); } catch { /* Session preferences still work. */ }
    set(next);
  },
}));
