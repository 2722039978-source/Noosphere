/** Sample delivered frame cadence, which includes GPU backpressure. */
export class FrameBudget {
  private last = 0;
  private intervals: number[] = [];
  reset() { this.last = 0; this.intervals = []; }
  sample(now: number) {
    const previous = this.last;
    this.last = now;
    if (!previous || now - previous > 250) { this.intervals = []; return false; }
    this.intervals.push(now - previous);
    if (this.intervals.length < 45) return false;
    const slow = this.intervals.filter(value => value > 28).length >= 30;
    this.intervals = [];
    return slow;
  }
}
export function renderPixelRatio(width: number, height: number, dpr: number, high: boolean) {
  const pixelBudget = high ? 5_000_000 : 3_000_000;
  return Math.min(dpr || 1, high ? 2 : 1.5, Math.max(1, Math.sqrt(pixelBudget / Math.max(1, width * height))));
}
