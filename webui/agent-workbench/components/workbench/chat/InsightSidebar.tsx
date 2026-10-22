import { BookOpen, PanelRightClose, Shield, X } from "lucide-react";
import { useEffect, useRef } from "react";
import ReactMarkdown from "react-markdown";
import rehypeRaw from "rehype-raw";
import rehypeSanitize from "rehype-sanitize";
import type { AgentConfig, ApiTurnResult } from "../../../state/workbench/remoteModelAdapter";
import { groupEvidenceDocuments, sourceParagraphs } from "../../../state/workbench/evidencePresentation";

export function InsightSidebar({ result, evidenceId, notice, open, onClose, onSelect }: {
  result: ApiTurnResult | null; evidenceId?: string; notice?: AgentConfig["notice"]; open: boolean;
  onClose: () => void; onSelect: (id?: string) => void;
}) {
  const panel = useRef<HTMLDivElement>(null);
  const selected = useRef<HTMLElement>(null);
  const groups = groupEvidenceDocuments(result?.evidence ?? []);
  useEffect(() => { selected.current?.scrollIntoView({ block: "nearest" }); }, [result?.runId, evidenceId]);
  useEffect(() => {
    if (!evidenceId) return;
    const dismiss = (event: PointerEvent) => {
      if (event.target instanceof Element && !event.target.closest(".citation-link, .answer-sources-button")
        && !panel.current?.contains(event.target)) onSelect();
    };
    const escape = (event: KeyboardEvent) => { if (event.key === "Escape") onSelect(); };
    document.addEventListener("pointerdown", dismiss);
    document.addEventListener("keydown", escape);
    return () => { document.removeEventListener("pointerdown", dismiss); document.removeEventListener("keydown", escape); };
  }, [evidenceId, onSelect]);
  return <aside className="insight-sidebar" data-open={open || undefined} aria-label="提示与证据" data-component-id="workbench.chat.insight_sidebar">
    <header className="insight-header"><strong>提示与证据</strong>
      <button className="sidebar-close" type="button" title="关闭证据栏" onClick={onClose}><PanelRightClose /></button>
    </header>
    <div className="insight-scroll">
      {notice && <section className="safety-card" aria-label={notice.title}>
        <h2><Shield aria-hidden="true" />{notice.title}</h2>
        <p>{notice.body}</p>
      </section>}
      <h2 className="evidence-heading"><BookOpen aria-hidden="true" />引用证据{groups.length > 0 && <span>{groups.length} 个来源 · {result!.evidence.length} 处引用</span>}</h2>
      {groups.length === 0 && <p className="evidence-empty">当前回答没有附带引用资料。</p>}
      <div ref={panel} className="evidence-card-list" data-result-run-id={result?.runId}>
        {groups.map((group, index) => {
          const expanded = group.items.some((item) => item.id === evidenceId);
          const contentId = `evidence-${result!.runId}-${index}`;
          return <article className="evidence-document" key={group.key} data-document-key={group.key}>
            <button className="evidence-title" type="button" aria-expanded={expanded} aria-controls={contentId}
              onClick={() => onSelect(expanded ? undefined : group.items[0].id)}>
              <BookOpen className="evidence-file-icon" aria-hidden="true" /><strong>{group.title}</strong>
              <span className="evidence-count">{group.items.length} 处</span>
            </button>
            {expanded && <div className="evidence-popover" id={contentId} role="region" aria-label={`${group.title} 引用原文`}>
              <header className="evidence-popover-header"><span>引用原文 · {group.items.length} 处</span>
                <button type="button" className="message-action" title="收起引用原文" onClick={() => onSelect()}><X aria-hidden="true" /></button>
              </header>
              <div className="evidence-popover-scroll">
                {group.items.map((item) => {
                  const number = item.citationId ?? result!.evidence.indexOf(item) + 1;
                  const claims = result!.claims?.flatMap((claim, i) => claim.evidence_ids.includes(item.id) ? [i + 1] : []) ?? [];
                  return <section key={item.id} ref={item.id === evidenceId ? selected : null} className="evidence-passage"
                    data-evidence-id={item.id} data-selected={item.id === evidenceId || undefined}>
                    <div className="evidence-passage-label"><span className="evidence-number">{number}</span><strong>原文 {number}</strong>
                      {claims.length > 0 && <span>对应回答第 {claims.join("、")} 段</span>}
                    </div>
                    <blockquote>{sourceParagraphs(item.quote).map((paragraph, i) => <div className="source-paragraph" key={i}>
                      <ReactMarkdown rehypePlugins={[rehypeRaw, rehypeSanitize]}
                        disallowedElements={["img", "a"]} unwrapDisallowed>{paragraph}</ReactMarkdown>
                    </div>)}</blockquote>
                  </section>;
                })}
              </div>
            </div>}
          </article>;
        })}
      </div>
    </div>
  </aside>;
}
