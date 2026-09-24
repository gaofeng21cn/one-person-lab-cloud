import { getJson } from "./console-api.ts";

// AgentDeliveryOwnerFact names the Cloud owner that reported one layer of the
// Agent delivery chain. The Console renders the owner's facts; it never invents a
// layer the BFF did not read.
export interface AgentDeliveryOwnerFact {
  owner: string;
  state: string;
  details: Record<string, unknown>;
}

// AgentDeliveryDTO is the BFF-composed view of one Workspace's Agent delivery
// chain: the pinned capability version, the build, the workspace plan, the Serve
// deployment and access, each attributed to its owning service.
export interface AgentDeliveryDTO {
  workspaceId: string;
  workspace: AgentDeliveryOwnerFact;
  serve: AgentDeliveryOwnerFact;
  capabilityVersion: AgentDeliveryOwnerFact;
  build: AgentDeliveryOwnerFact;
}

// getAgentDelivery reads the composed Agent delivery view for one Workspace.
export function getAgentDelivery(workspaceId: string, { signal }: { signal?: AbortSignal } = {}): Promise<AgentDeliveryDTO> {
  return getJson<AgentDeliveryDTO>(`/api/v2/delivery/${encodeURIComponent(workspaceId)}`, { signal });
}
