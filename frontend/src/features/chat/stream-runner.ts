export type RunPhase = "preparing" | "waiting" | "streaming";
export type RunStatus = "completed" | "cancelled" | "failed";
export interface RunIdentity { requestId: string; sessionId: string; messageId: string; workspaceId?: string; projectId?: string }
export interface RunEvent {
  request_id?: string;
  token?: string;
  content?: string;
  error?: string;
  done?: boolean;
  status?: RunStatus;
}
export interface RunInput extends RunIdentity {
  agentMode?: boolean;
  reviewedActions?: boolean;
  modelId: string;
  begin: () => void;
  prepare: (signal: AbortSignal) => Promise<{ role: string; content: string }[]>;
}
interface RunnerOptions {
  start: (input: RunInput, messages: { role: string; content: string }[], receive: (event: RunEvent) => void) => Promise<void>;
  cancel: (requestId: string) => Promise<unknown>;
  active: (run: (RunIdentity & { phase: RunPhase }) | null) => void;
  append: (run: RunIdentity, text: string) => void;
  finish: (run: RunIdentity, status: RunStatus, error?: string, content?: string) => void;
  cancelError: (requestId: string, error: unknown) => void;
  batchMs?: number;
  timeoutMs?: number;
}
interface Running {
  starting: boolean;
  input: RunInput;
  controller: AbortController;
  phase: RunPhase;
  ended: boolean;
  buffer: string;
  batch?: ReturnType<typeof setTimeout>;
  deadline?: ReturnType<typeof setTimeout>;
  acknowledged?: Promise<void>;
}

/** Request-owned lifecycle, independent of which page or session is currently visible. */
export function createStreamRunner(options: RunnerOptions) {
  let current: Running | null = null;
  const cancellations = new Set<Promise<unknown>>();
  function flush(run = current) {
    if (!run) return;
    clearTimeout(run.batch);
    run.batch = undefined;
    if (run.buffer) {
      const text = run.buffer;
      run.buffer = "";
      options.append(run.input, text);
    }
  }
  function finish(run: Running, status: RunStatus, error?: string, content?: string) {
    if (run.ended) return;
    run.ended = true;
    clearTimeout(run.deadline);
    if (content === undefined) flush(run);
    else { clearTimeout(run.batch); run.buffer = ""; }
    run.controller.abort();
    if (current === run) { current = null; options.active(null); }
    options.finish(run.input, status, error, content);
  }
  function cancelTransport(run: Running) {
    if (!run.acknowledged) return Promise.resolve();
    // Register first, cancel second: a fast stop must not miss a command still in flight.
    const pending = run.acknowledged.then(() => options.cancel(run.input.requestId));
    cancellations.add(pending);
    void pending.then(
      () => cancellations.delete(pending),
      (error) => { cancellations.delete(pending); options.cancelError(run.input.requestId, error); },
    );
    return pending;
  }
  function receive(run: Running, event: RunEvent) {
    if (current !== run || run.ended || (event.request_id && event.request_id !== run.input.requestId)) return;
    if (event.token) {
      run.buffer += event.token;
      if (run.phase !== "streaming") {
        run.phase = "streaming";
        options.active({ ...run.input, phase: "streaming" });
      }
      run.batch ??= setTimeout(() => flush(run), options.batchMs ?? 32);
    }
    if (event.error || event.done || event.status) {
      const status = event.status ?? (event.error ? "failed" : "completed");
      finish(run, status, event.error, event.content);
    }
  }
  return {
    get active() { return current && !current.starting ? current.input : null; },
    flush,
    start(input: RunInput): boolean {
      if (current) return false;
      const run: Running = { input, starting: true, controller: new AbortController(), phase: "preparing", ended: false, buffer: "" };
      current = run;
      options.active({ ...input, phase: "preparing" });
      try { input.begin(); } catch (error) {
        finish(run, "failed", String(error));
        return false;
      }
      run.starting = false;
      void (async () => {
        try {
          const messages = await input.prepare(run.controller.signal);
          if (run.ended) return;
          run.phase = "waiting";
          options.active({ ...input, phase: "waiting" });
          run.deadline = setTimeout(() => {
            if (run.ended) return;
            finish(run, "failed", "模型响应超时，请重试。");
            void cancelTransport(run).catch(() => {});
          }, options.timeoutMs ?? 130_000);
          run.acknowledged = options.start(input, messages, (event) => receive(run, event));
          await run.acknowledged;
        } catch (error) {
          if (!run.ended) finish(run, "failed", error instanceof Error ? error.message : String(error));
        }
      })();
      return true;
    },
    cancel(): Promise<unknown> {
      const run = current;
      if (!run) return Promise.resolve();
      finish(run, "cancelled");
      return cancelTransport(run);
    },
    async settleCancellations() { await Promise.allSettled([...cancellations]); },
  };
}
