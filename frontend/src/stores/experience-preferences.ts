export type GraphicsMode = 'auto' | 'high' | 'low' | 'off';
export type StartupPage = 'overview' | 'chat';
export type ExperiencePreferences = { graphics: GraphicsMode; boot: boolean; startPage: StartupPage };

/** Historic archive defaults become the usable overview while other preferences survive. */
export function normalizeExperiencePreferences(value: unknown): ExperiencePreferences {
  const saved = value && typeof value === 'object' ? value as Record<string, unknown> : {};
  return {
    graphics: ['auto', 'high', 'low', 'off'].includes(saved.graphics as string) ? saved.graphics as GraphicsMode : 'auto',
    boot: typeof saved.boot === 'boolean' ? saved.boot : true,
    startPage: saved.startPage === 'chat' ? 'chat' : 'overview',
  };
}
