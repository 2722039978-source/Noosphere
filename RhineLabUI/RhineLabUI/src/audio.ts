export class TerminalAudio {
  enabled = false;
  private context?: AudioContext;
  play(type: "tick" | "open" | "confirm" | "back" = "tick") {
    if (!this.enabled) return;
    try {
      this.context ??= new AudioContext();
      const c = this.context;
      if (c.state === "suspended") void c.resume();
      const osc = c.createOscillator(),
        gain = c.createGain();
      osc.type = "sine";
      const frequency = { tick: 760, open: 420, confirm: 960, back: 320 }[type];
      osc.frequency.setValueAtTime(frequency, c.currentTime);
      osc.frequency.exponentialRampToValueAtTime(
        frequency * (type === "open" ? 1.8 : 0.7),
        c.currentTime + 0.13,
      );
      gain.gain.setValueAtTime(0.0001, c.currentTime);
      gain.gain.exponentialRampToValueAtTime(0.035, c.currentTime + 0.008);
      gain.gain.exponentialRampToValueAtTime(0.0001, c.currentTime + 0.18);
      osc.connect(gain);
      gain.connect(c.destination);
      osc.start();
      osc.stop(c.currentTime + 0.2);
    } catch {
      /* Audio is optional; the terminal remains fully interactive. */
    }
  }
}
