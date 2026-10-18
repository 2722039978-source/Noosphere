// Hide rapidly changing titles behind one stable strip, then reveal the final title.
export class ScrubTitle {
  private text: HTMLElement;
  private value: string;
  private lastChange = -Infinity;
  private timer?: ReturnType<typeof setTimeout>;
  private animation?: Animation;
  private obscured = false;

  constructor(private host: HTMLElement) {
    this.value = host.textContent ?? "";
    this.text = document.createElement("span");
    this.text.textContent = this.value;
    host.replaceChildren(this.text);
    host.classList.add("scrub-title");
  }

  update(value: string, animated: boolean) {
    if (!animated) {
      this.reset();
      this.value = this.text.textContent = value;
      this.lastChange = -Infinity;
      return;
    }
    if (value === this.value) return;
    const now = performance.now();
    const rapid = now - this.lastChange < 240;
    this.lastChange = now;
    this.value = value;
    clearTimeout(this.timer);
    if (!this.obscured && !rapid) {
      this.text.textContent = value;
      return;
    }
    if (!this.obscured) {
      // Keep one width throughout a burst so the strip does not chase each title.
      this.host.style.width = `${Math.max(88, this.host.offsetWidth)}px`;
      this.obscured = true;
      this.host.dataset.scrubbing = "true";
      this.text.style.opacity = "0";
      this.animation?.cancel();
      this.animation = this.text.animate(
        [
          { opacity: 1 },
          { opacity: 0.28, offset: 0.35 },
          { opacity: 1, offset: 0.7 },
          { opacity: 0 },
        ],
        { duration: 140, easing: "steps(1, end)" },
      );
    }
    this.text.textContent = value;
    this.timer = setTimeout(() => this.reveal(), 260);
  }

  private reveal() {
    this.animation?.cancel();
    this.obscured = false;
    delete this.host.dataset.scrubbing;
    this.host.style.width = "";
    this.text.style.opacity = "";
    this.animation = this.host.animate(
      [
        { opacity: 0.2 },
        { opacity: 1, offset: 0.35 },
        { opacity: 0.5, offset: 0.6 },
        { opacity: 1 },
      ],
      { duration: 180, easing: "steps(1, end)" },
    );
  }

  reset() {
    clearTimeout(this.timer);
    this.animation?.cancel();
    this.animation = undefined;
    this.obscured = false;
    delete this.host.dataset.scrubbing;
    this.host.style.width = "";
    this.text.style.opacity = "";
    this.lastChange = -Infinity;
  }
}
