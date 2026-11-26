import { useEffect, useState, type FormEvent } from "react";

import { Alert } from "./index.ts";
import { Button } from "./Button.tsx";
import { Field } from "./Field.tsx";
import { Modal } from "./Modal.tsx";

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

type ActiveRequest =
  | { kind: "confirm"; options: ConfirmDialogOptions; resolve: (confirmed: boolean) => void }
  | { kind: "prompt"; options: PromptDialogOptions; resolve: (value: string | null) => void };

type Listener = (request: ActiveRequest | null) => void;

const listeners = new Set<Listener>();
let currentRequest: ActiveRequest | null = null;

function notify(next: ActiveRequest | null) {
  currentRequest = next;
  for (const listener of listeners) listener(next);
}

function dismissCurrent() {
  const active = currentRequest;
  if (!active) return;
  notify(null);
  if (active.kind === "confirm") active.resolve(false);
  else active.resolve(null);
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

export function ConfirmDialogHost() {
  const [request, setRequest] = useState<ActiveRequest | null>(currentRequest);
  const [inputValue, setInputValue] = useState("");

  useEffect(() => {
    listeners.add(setRequest);
    setRequest(currentRequest);
    return () => {
      listeners.delete(setRequest);
    };
  }, []);

  useEffect(() => {
    setInputValue("");
  }, [request]);

  if (!request) return null;

  const cancel = () => {
    const active = request;
    notify(null);
    if (active.kind === "confirm") active.resolve(false);
    else active.resolve(null);
  };

  if (request.kind === "confirm") {
    const { title, description, confirmLabel = "确认", cancelLabel = "取消", tone = "primary", requireText } = request.options;
    const expected = requireText?.expected.trim() ?? "";
    const canConfirm = !requireText || inputValue.trim() === expected;
    const submit = (event?: FormEvent) => {
      event?.preventDefault();
      if (!canConfirm) return;
      notify(null);
      request.resolve(true);
    };

    return (
      <Modal
        className={`console-confirm-modal${tone === "danger" ? " console-confirm-modal--danger" : ""}`}
        footer={
          <>
            <Button onClick={cancel} variant="outline">{cancelLabel}</Button>
            <Button color={tone === "danger" ? "danger" : "primary"} disabled={!canConfirm} onClick={() => submit()}>{confirmLabel}</Button>
          </>
        }
        onClose={cancel}
        open
        title={title}
      >
        <form className="console-confirm-form" onSubmit={submit}>
          {tone === "danger"
            ? <Alert color="danger" description={description} title="请确认高风险操作" />
            : <p className="console-confirm-description">{description}</p>}
          {requireText ? (
            <Field
              autoFocus
              label={requireText.label}
              onChange={(event) => setInputValue(event.currentTarget.value)}
              placeholder={requireText.placeholder || requireText.expected}
              required
              value={inputValue}
            />
          ) : null}
        </form>
      </Modal>
    );
  }

  const { title, description, label, placeholder, confirmLabel = "确认", cancelLabel = "取消" } = request.options;
  const trimmed = inputValue.trim();
  const submitPrompt = (event?: FormEvent) => {
    event?.preventDefault();
    if (!trimmed) return;
    notify(null);
    request.resolve(trimmed);
  };

  return (
    <Modal
      className="console-confirm-modal"
      footer={
        <>
          <Button onClick={cancel} variant="outline">{cancelLabel}</Button>
          <Button color="primary" disabled={!trimmed} onClick={() => submitPrompt()}>{confirmLabel}</Button>
        </>
      }
      onClose={cancel}
      open
      title={title}
    >
      <form className="console-confirm-form" onSubmit={submitPrompt}>
        {description ? <p className="console-confirm-description">{description}</p> : null}
        <Field
          autoFocus
          label={label}
          onChange={(event) => setInputValue(event.currentTarget.value)}
          placeholder={placeholder}
          required
          value={inputValue}
        />
      </form>
    </Modal>
  );
}
