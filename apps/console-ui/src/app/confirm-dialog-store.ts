export interface ConfirmDialogOptions {
  title: string;
  description: string;
  confirmLabel?: string;
  cancelLabel?: string;
  tone?: "primary" | "danger";
  requireText?: {
    label: string;
    expected: string;
    placeholder?: string;
  };
}

export interface PromptDialogOptions {
  title: string;
  description?: string;
  label: string;
  placeholder?: string;
  confirmLabel?: string;
  cancelLabel?: string;
}

export type ActiveConfirmRequest =
  | { kind: "confirm"; options: ConfirmDialogOptions; resolve: (confirmed: boolean) => void }
  | { kind: "prompt"; options: PromptDialogOptions; resolve: (value: string | null) => void };

type Listener = (request: ActiveConfirmRequest | null) => void;

const listeners = new Set<Listener>();
let currentRequest: ActiveConfirmRequest | null = null;

function notify(next: ActiveConfirmRequest | null) {
  currentRequest = next;
  for (const listener of listeners) listener(next);
}

export function getActiveConfirmRequest(): ActiveConfirmRequest | null {
  return currentRequest;
}

export function subscribeConfirmDialog(listener: Listener): () => void {
  listeners.add(listener);
  listener(currentRequest);
  return () => {
    listeners.delete(listener);
  };
}

export function settleConfirmDialog(request: ActiveConfirmRequest, value: boolean | string | null) {
  if (currentRequest === request) notify(null);
  if (request.kind === "confirm") request.resolve(Boolean(value));
  else request.resolve(typeof value === "string" ? value : null);
}

function dismissCurrent() {
  const active = currentRequest;
  if (!active) return;
  settleConfirmDialog(active, active.kind === "confirm" ? false : null);
}

export function requestConfirm(options: ConfirmDialogOptions): Promise<boolean> {
  dismissCurrent();
  return new Promise<boolean>((resolve) => {
    notify({ kind: "confirm", options, resolve });
  });
}

export function requestPrompt(options: PromptDialogOptions): Promise<string | null> {
  dismissCurrent();
  return new Promise<string | null>((resolve) => {
    notify({ kind: "prompt", options, resolve });
  });
}
