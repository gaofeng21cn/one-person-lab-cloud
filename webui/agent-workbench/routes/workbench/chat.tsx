/** feature_id: v1.workbench.chat; route_id: workbench.chat; component_id: workbench.chat.route */
import { useEffect, useState } from "react";
import { AgentSetupDialog } from "../../components/workbench/chat/AgentSetupDialog";
import { ChatStage } from "../../components/workbench/chat/ChatStage";
import { HistorySidebar } from "../../components/workbench/chat/HistorySidebar";
import { InsightSidebar } from "../../components/workbench/chat/InsightSidebar";
import type { UiFailure, AgentConfig, ApiRun, ApiTurnResult, PackageDescriptor } from "../../state/workbench/remoteModelAdapter";

type Props = {
  packages: PackageDescriptor[]; canCancel?: boolean;
  agentConfig: AgentConfig | null; onAgentConfigChange: (config: AgentConfig) => void;
  run: ApiRun | null; restoring: boolean; connectionError: UiFailure | null;
  onCancel?: () => void; onReconnect: () => void;
};

export function WorkbenchChatRoute(props: Props) {
  const [selected, setSelected] = useState<{ result: ApiTurnResult; evidenceId?: string } | null>(null);
  const [setupOpen, setSetupOpen] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [insightOpen, setInsightOpen] = useState(false);
  const result = selected?.result ?? props.run?.result ?? null;
  useEffect(() => { setSelected(null); }, [props.run?.runId]);
  useEffect(() => {
    const close = (event: KeyboardEvent) => {
      if (event.key === "Escape") { setHistoryOpen(false); setInsightOpen(false); setSetupOpen(false); }
    };
    window.addEventListener("keydown", close);
    return () => window.removeEventListener("keydown", close);
  }, []);
  return <div className="workbench-shell assistant-workbench" data-feature-id="v1.workbench.chat" data-route-id="workbench.chat">
    <HistorySidebar title={props.agentConfig?.sidebarTitle ?? ""} open={historyOpen} onClose={() => setHistoryOpen(false)} />
    <ChatStage {...props} onOpenSetup={() => setSetupOpen(true)} onOpenHistory={() => setHistoryOpen(true)}
      onOpenInsights={() => setInsightOpen(true)} onOpenEvidence={(answer, evidenceId) => {
        setSelected((prior) => ({ result: answer, evidenceId: prior?.result.runId === answer.runId && prior.evidenceId === evidenceId ? undefined : evidenceId })); setInsightOpen(true);
      }} />
    <InsightSidebar result={result} evidenceId={selected?.evidenceId} notice={props.agentConfig?.notice} open={insightOpen} onClose={() => { setInsightOpen(false); if (result) setSelected({ result }); }}
      onSelect={(evidenceId) => { if (result) setSelected({ result, evidenceId }); }} />
    <button type="button" className="drawer-overlay" data-visible={historyOpen || insightOpen || undefined} aria-label="关闭侧栏"
      onClick={() => { setHistoryOpen(false); setInsightOpen(false); }} />
    <AgentSetupDialog open={setupOpen} currentConfig={props.agentConfig} packages={props.packages} onClose={() => setSetupOpen(false)}
      onApplied={(config) => { props.onAgentConfigChange(config); setSetupOpen(false); }} />
  </div>;
}
