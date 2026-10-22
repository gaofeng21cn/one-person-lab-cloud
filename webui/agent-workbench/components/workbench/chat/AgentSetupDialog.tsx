import { useEffect, useState } from "react";
import { Settings2, X } from "lucide-react";
import type { AgentConfig, PackageDescriptor } from "../../../state/workbench/remoteModelAdapter";

type Props = {
  open: boolean; currentConfig: AgentConfig | null; packages: PackageDescriptor[];
  onApplied: (config: AgentConfig) => void; onClose: () => void;
};

export function AgentSetupDialog({ open, currentConfig, packages, onApplied, onClose }: Props) {
  const [selectedId, setSelectedId] = useState("");
  const [sidebarTitle, setSidebarTitle] = useState("");
  const [assistantTitle, setAssistantTitle] = useState("");
  useEffect(() => {
    if (!open) return;
    setSelectedId(currentConfig?.choiceId ?? "");
    setSidebarTitle(currentConfig?.sidebarTitle ?? "");
    setAssistantTitle(currentConfig?.assistantTitle ?? "");
  }, [open, currentConfig]);
  if (!open) return null;
  const selected = packages.find((item) => item.choiceId === selectedId);
  const apply = (event: React.FormEvent) => {
    event.preventDefault();
    if (!selected || !sidebarTitle.trim() || !assistantTitle.trim()) return;
    onApplied({
      choiceId: selected.choiceId, packageId: selected.packageId, packageName: selected.name,
      sidebarTitle: sidebarTitle.trim(), assistantTitle: assistantTitle.trim(),
      ...(currentConfig?.notice ? { notice: currentConfig.notice } : {}),
    });
  };
  return <div className="setup-dialog-backdrop" role="presentation">
    <form className="setup-dialog" role="dialog" aria-modal="true" aria-labelledby="agent-setup-title" onSubmit={apply}>
      <header className="setup-dialog-header">
        <div className="setup-dialog-title"><span className="setup-dialog-icon"><Settings2 aria-hidden="true" /></span><h2 id="agent-setup-title">配置 Agent</h2></div>
        {currentConfig && <button className="setup-close-button" type="button" onClick={onClose} title="关闭配置"><X aria-hidden="true" /></button>}
      </header>
      <div className="setup-dialog-body">
        <label className="setup-field"><span>侧栏名称</span><input name="sidebarTitle" value={sidebarTitle} required maxLength={80}
          onChange={(event) => setSidebarTitle(event.target.value)} /></label>
        <label className="setup-field"><span>助手名称</span><input name="assistantTitle" value={assistantTitle} required maxLength={80}
          onChange={(event) => setAssistantTitle(event.target.value)} /></label>
        <label className="setup-field"><span>选择 Agent</span><select aria-label="选择 Agent" name="agentChoiceId" value={selectedId} required onChange={(event) => setSelectedId(event.target.value)}>
          <option value="" disabled>{packages.length ? "请选择 Agent" : "暂无可用的 Agent"}</option>
          {packages.map((item) => <option key={item.choiceId} value={item.choiceId}>{item.name}</option>)}
        </select><small>所选 Agent 用于新对话；历史会话沿用原来的 Agent。</small></label>
      </div>
      <footer className="setup-dialog-footer"><span className="setup-footer-status" role="status" />
        <button className="setup-apply-button" type="submit" disabled={!selected || !sidebarTitle.trim() || !assistantTitle.trim()}>应用配置</button>
      </footer>
    </form>
  </div>;
}
