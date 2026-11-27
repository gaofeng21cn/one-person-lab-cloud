import { useEffect, useState, type FormEvent } from "react";

import {
  getActiveConfirmRequest,
  requestConfirm,
  requestPrompt,
  settleConfirmDialog,
  subscribeConfirmDialog,
  type ActiveConfirmRequest,
  type ConfirmDialogOptions,
  type PromptDialogOptions
} from "../../app/confirm-dialog-store.ts";
import { Alert } from "./index.ts";
import { Button } from "./Button.tsx";
import { Field } from "./Field.tsx";
import { Modal } from "./Modal.tsx";

export { requestConfirm, requestPrompt, type ConfirmDialogOptions, type PromptDialogOptions };

export function ConfirmDialogHost() {
  const [request, setRequest] = useState<ActiveConfirmRequest | null>(getActiveConfirmRequest);
  const [inputValue, setInputValue] = useState("");

  useEffect(() => subscribeConfirmDialog(setRequest), []);

  useEffect(() => {
    setInputValue("");
  }, [request]);

  if (!request) return null;

  const cancel = () => {
    settleConfirmDialog(request, request.kind === "confirm" ? false : null);
  };

  if (request.kind === "confirm") {
    const { title, description, confirmLabel = "确认", cancelLabel = "取消", tone = "primary", requireText } = request.options;
    const expected = requireText?.expected.trim() ?? "";
    const canConfirm = !requireText || inputValue.trim() === expected;
    const submit = (event?: FormEvent) => {
      event?.preventDefault();
      if (!canConfirm) return;
      settleConfirmDialog(request, true);
    };

    return (
      <Modal
        className={`console-confirm-modal${tone === "danger" ? " console-confirm-modal--danger" : ""}`}
        footer={
          <>
            <Button autoFocus={tone === "danger" && !requireText} onClick={cancel} variant="outline">{cancelLabel}</Button>
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
    settleConfirmDialog(request, trimmed);
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
