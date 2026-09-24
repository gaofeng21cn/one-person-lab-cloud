import { useEffect, useState } from "react";

import { getAgentDelivery, type AgentDeliveryDTO, type AgentDeliveryOwnerFact } from "../../api/delivery-api.ts";

interface AgentDeliveryState {
  value: AgentDeliveryDTO | null;
  loading: boolean;
  error: string;
}

function ownerLabel(owner: string): string {
  switch (owner) {
    case "capability": return "Capability";
    case "build": return "Build";
    case "workspace": return "Workspace";
    case "serve": return "Serve";
    default: return owner;
  }
}

function OwnerRow({ fact }: { fact: AgentDeliveryOwnerFact }) {
  const digest = typeof fact.details.artifactDigest === "string" ? fact.details.artifactDigest : "";
  const accessUrl = typeof fact.details.accessUrl === "string" ? fact.details.accessUrl : "";
  return <div>
    <dt>{ownerLabel(fact.owner)}</dt>
    <dd>
      <span>{fact.state}</span>
      {digest ? <small> 制品摘要：{digest}</small> : null}
      {accessUrl ? <small> 访问地址：{accessUrl}</small> : null}
    </dd>
  </div>;
}

// AgentDeliveryPanel renders the same owner facts the BFF read for the Agent
// delivery chain of one Workspace. A missing owner read is shown as an explicit
// failure rather than a blank or invented value.
export function AgentDeliveryPanel({ workspaceId }: { workspaceId: string }) {
  const [state, setState] = useState<AgentDeliveryState>({ value: null, loading: true, error: "" });

  useEffect(() => {
    const controller = new AbortController();
    setState({ value: null, loading: true, error: "" });
    getAgentDelivery(workspaceId, { signal: controller.signal })
      .then((value) => setState({ value, loading: false, error: "" }))
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        setState({ value: null, loading: false, error: error instanceof Error ? error.message : "delivery_read_failed" });
      });
    return () => controller.abort();
  }, [workspaceId]);

  if (state.loading) return <div className="source-loading" aria-live="polite"><span className="spinner" />正在读取交付链</div>;
  if (state.error || !state.value) {
    return <div>
      <dt>交付链</dt>
      <dd><code>{state.error || "delivery_unavailable"}</code></dd>
    </div>;
  }
  const value = state.value;
  return <>
    <OwnerRow fact={value.workspace} />
    <OwnerRow fact={value.capabilityVersion} />
    <OwnerRow fact={value.build} />
    <OwnerRow fact={value.serve} />
  </>;
}
