export type GraphicsMode = 'auto' | 'high' | 'low' | 'off';
export type ExperiencePreferences = { graphics: GraphicsMode; boot: boolean };

export function normalizeExperiencePreferences(value: unknown): ExperiencePreferences {
  const saved = value && typeof value === 'object' ? value as Record<string, unknown> : {};
  return {
    graphics: ['auto', 'high', 'low', 'off'].includes(saved.graphics as string) ? saved.graphics as GraphicsMode : 'auto',
    boot: typeof saved.boot === 'boolean' ? saved.boot : true,
  };
}
