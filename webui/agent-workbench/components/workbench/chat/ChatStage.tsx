/**
 * feature_id: v1.workbench.chat
 * route_id: workbench.chat
 * component_id: workbench.chat.stage
 * state_owner: workbench.chat.stream_state
 */
import { ThreadPrimitive } from "@assistant-ui/react";
import { Bot, Settings2, PanelLeft, PanelRight } from "lucide-react";
import { MessageList } from "./MessageList";
import { needsStatusReadback, type UiFailure, type AgentConfig, type ApiRun, type ApiTurnResult } from "../../../state/workbench/remoteModelAdapter";

type ChatStageProps = {
  agentConfig: AgentConfig | null;
  onOpenEvidence: (result: ApiTurnResult, evidenceId: string) => void;
  onOpenSetup: () => void;
  onOpenHistory: () => void;
  onOpenInsights: () => void;
  run: ApiRun | null;
  restoring: boolean;
  connectionError: UiFailure | null;
  canCancel?: boolean;
  onCancel?: () => void;
  onReconnect: () => void;
};

export function ChatStage({
  agentConfig,
  onOpenEvidence,
  onOpenSetup,
  onOpenHistory,
  onOpenInsights,
  run,
  restoring,
  connectionError,
  canCancel = false,
  onCancel,
  onReconnect,
}: ChatStageProps) {
  return (
    <ThreadPrimitive.Root className="chat-stage" data-component-id="workbench.chat.stage">
      <header className="chat-header">
        <button className="mobile-panel-button history-panel-button" title="打开会话列表" type="button" onClick={onOpenHistory}><PanelLeft /></button>
        <div className="agent-identity">
          <span className="agent-mark"><Bot aria-hidden="true" /></span>
          <span>
            <strong>{agentConfig?.assistantTitle ?? ""}</strong>
          </span>
        </div>

        <div className="chat-header-actions">
          <button className="mobile-panel-button evidence-panel-button" type="button" title="打开提示与证据" onClick={onOpenInsights}><PanelRight /></button>
          <button className="setup-header-button" type="button" disabled={run?.status === "running" || run?.status === "cancelling"} onClick={onOpenSetup} title="配置 Agent">
            <Settings2 aria-hidden="true" />
          </button>
        </div>
      </header>

      <MessageList assistantTitle={agentConfig?.assistantTitle ?? ""} inputDisabled={!agentConfig || restoring || needsStatusReadback(connectionError)} onOpenEvidence={onOpenEvidence}
        feedback={{ run, restoring, connectionError, canCancel, onCancel, onReconnect }} />
    </ThreadPrimitive.Root>
  );
}
