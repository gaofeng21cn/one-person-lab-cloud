/**
 * feature_id: v1.workbench.chat
 * route_id: workbench.chat
 * component_id: workbench.chat.message_list
 * state_owner: workbench.chat.session_state
 */
import ReactMarkdown from "react-markdown";
import {
  ActionBarPrimitive,
  AttachmentPrimitive,
  ErrorPrimitive,
  MessagePartPrimitive,
  MessagePrimitive,
  ThreadPrimitive,
  useAuiState,
} from "@assistant-ui/react";
import { createContext, useContext } from "react";
import { ArrowDown, Bot, Check, Copy, FileImage, BookOpen } from "lucide-react";
import { InputPanel } from "./InputPanel";
import type { ApiTurnResult, ApiRun, UiFailure } from "../../../state/workbench/remoteModelAdapter";
import { RunProgress } from "./RunProgress";
import { remarkEvidenceCitations } from "../../../state/workbench/citationMarkdown";

type OpenEvidence = (result: ApiTurnResult, evidenceId: string) => void;
const EvidenceContext = createContext<OpenEvidence>(() => {});
type Feedback = { run: ApiRun | null; restoring: boolean; connectionError: UiFailure | null; canCancel?: boolean; onCancel?: () => void; onReconnect: () => void };
const FeedbackContext = createContext<Feedback | null>(null);

function AnswerContent({ result }: { result: ApiTurnResult }) {
  const openEvidence = useContext(EvidenceContext);
  const evidenceById = new Map(result.evidence.map((item) => [item.id, item]));
  return <>
    {result.claims && result.claims.length > 0 ? <>
      {result.claims.map((claim, index) => <div className="answer-claim" key={index}>
        <ReactMarkdown>{claim.text}</ReactMarkdown>
        {claim.evidence_ids.map((id) => {
          const evidence = evidenceById.get(id);
          return evidence && <button type="button" className="citation-link" key={id}
            title={evidence.title} aria-label={`查看引用 ${evidence.citationId ?? result.evidence.indexOf(evidence) + 1}：${evidence.title}`}
            onClick={() => openEvidence(result, id)}>{evidence.citationId ?? result.evidence.indexOf(evidence) + 1}</button>;
        })}
      </div>)}
    </> : <ReactMarkdown remarkPlugins={[remarkEvidenceCitations(result.evidence)]} components={{
      a: ({ href, children }) => {
        const evidence = result.evidence.find((item) => href === `#evidence-${encodeURIComponent(item.id)}`);
        return evidence ? <button type="button" className="citation-link" title={evidence.title}
          aria-label={`查看引用 ${evidence.citationId}：${evidence.title}`}
          onClick={() => openEvidence(result, evidence.id)}>{children}</button>
          : <a href={href} target="_blank" rel="noreferrer">{children}</a>;
      },
    }}>{result.reply}</ReactMarkdown>}
    {result.uncertainties?.map((text, index) => <ReactMarkdown key={index}>{text}</ReactMarkdown>)}
    {result.evidence.length > 0 && <button className="answer-sources-button" type="button" onClick={() => openEvidence(result, result.evidence[0].id)}>
      <BookOpen aria-hidden="true" /><span>{result.evidence.length} 条引用</span>
    </button>}
  </>;
}

function MarkdownPart() {
  const part = useAuiState((state) => state.part);
  if (part.type !== "text") return null;
  return <ReactMarkdown>{part.text}</ReactMarkdown>;
}

function MessageAttachment() {
  return (
    <AttachmentPrimitive.Root className="message-attachment">
      <FileImage aria-hidden="true" />
      <span><AttachmentPrimitive.Name /></span>
    </AttachmentPrimitive.Root>
  );
}

function MessageActions() {
  const isCopied = useAuiState((state) => state.message.isCopied);

  return (
    <ActionBarPrimitive.Root className="message-actions" hideWhenRunning autohide="not-last">
      <ActionBarPrimitive.Copy className="message-action" title="复制消息">
        {isCopied ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
      </ActionBarPrimitive.Copy>
    </ActionBarPrimitive.Root>
  );
}

function UserMessage() {
  const messageIndex = useAuiState((state) => state.message.index);

  return (
    <MessagePrimitive.Root
      className="message-row user-row"
      data-message-index={messageIndex}
    >
      <div className="message-bubble user-bubble">
        <MessagePrimitive.Attachments>{() => <MessageAttachment />}</MessagePrimitive.Attachments>
        <MessagePrimitive.Parts>
          {({ part }) => {
            if (part.type === "text") return <MessagePartPrimitive.Text component="p" />;
            if (part.type === "image") return <MessagePartPrimitive.Image className="message-image" />;
            return null;
          }}
        </MessagePrimitive.Parts>
      </div>
    </MessagePrimitive.Root>
  );
}

function AssistantMessage() {
  const messageIndex = useAuiState((state) => state.message.index);
  const result = useAuiState((state) => state.message.metadata.custom.result) as ApiTurnResult | undefined;
  const run = useAuiState((state) => state.message.metadata.custom.run) as ApiRun | undefined;
  const feedback = useContext(FeedbackContext);
  const current = feedback?.run?.runId === run?.runId;

  return (
    <MessagePrimitive.Root
      className="message-row assistant-row"
      data-message-index={messageIndex}
      data-run-id={run?.runId || result?.runId}
    >
      <div className="assistant-message-layout">
        <span className="message-avatar"><Bot aria-hidden="true" /></span>
        <div className="assistant-message-body">
          <RunProgress run={run} failure={current ? feedback?.connectionError : null}
            canCancel={feedback?.canCancel} onCancel={current ? feedback?.onCancel : undefined} onReconnect={feedback?.onReconnect} />
          {result ? <AnswerContent result={result} /> : <MessagePrimitive.Parts>
            {({ part }) => part.type === "text" ? <MarkdownPart /> : null}
          </MessagePrimitive.Parts>}
          <MessagePrimitive.Error>
            <ErrorPrimitive.Root className="setup-error" role="alert">
              <ErrorPrimitive.Message />
            </ErrorPrimitive.Root>
          </MessagePrimitive.Error>
          <MessageActions />
        </div>
      </div>
    </MessagePrimitive.Root>
  );
}

function Welcome({ title }: { title: string }) {
  return (
    <ThreadPrimitive.Empty>
      <section className="thread-welcome">
        <span className="welcome-mark"><Bot aria-hidden="true" /></span>
        <h1>{title}</h1>
      </section>
    </ThreadPrimitive.Empty>
  );
}

export function MessageList({ inputDisabled, onOpenEvidence, feedback, assistantTitle }: { assistantTitle: string; inputDisabled: boolean; onOpenEvidence: OpenEvidence; feedback: Feedback }) {
  return (
    <EvidenceContext.Provider value={onOpenEvidence}><FeedbackContext.Provider value={feedback}><ThreadPrimitive.Viewport
      className="message-viewport"
      data-feature-id="v1.workbench.chat"
      data-route-id="workbench.chat"
      data-component-id="workbench.chat.message_list"
    >
      <div className="message-column">
        <Welcome title={assistantTitle} />
        <ThreadPrimitive.Messages>
          {({ message }) => message.role === "user" ? <UserMessage /> : <AssistantMessage />}
        </ThreadPrimitive.Messages>
        {(!feedback.run || feedback.connectionError?.kind === "input_rejected" || feedback.connectionError?.kind === "submission_unknown")
          && (feedback.restoring || feedback.connectionError) && <div className="message-row assistant-row"><div className="assistant-message-body">
            <RunProgress loading={feedback.restoring} failure={feedback.connectionError} onReconnect={feedback.onReconnect} />
          </div></div>}
      </div>

      <ThreadPrimitive.ScrollToBottom className="scroll-to-bottom" title="滚动到最新消息">
        <ArrowDown aria-hidden="true" />
      </ThreadPrimitive.ScrollToBottom>

      <ThreadPrimitive.ViewportFooter className="viewport-footer">
        <InputPanel disabled={inputDisabled} canCancel={feedback.canCancel} />
      </ThreadPrimitive.ViewportFooter>
    </ThreadPrimitive.Viewport></FeedbackContext.Provider></EvidenceContext.Provider>
  );
}
