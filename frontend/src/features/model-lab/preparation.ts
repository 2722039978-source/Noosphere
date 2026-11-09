export const PREPARATION_LIFETIME = 120_000;
export type PreparedSelection = {key: string; ready: boolean; receivedAt: number};
export function preparationCurrent(result: PreparedSelection | null, key: string, now = Date.now()) {
 return !!result && result.ready && result.key === key && now >= result.receivedAt && now-result.receivedAt < PREPARATION_LIFETIME;
}
