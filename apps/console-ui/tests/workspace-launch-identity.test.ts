// Seam: isAgentLaunchSurface in workspace-launch-controller-model.ts, the single
// decision that picks which owner opens a Workspace for this build. The Workspace
// Launch page reads it; a Legacy Workspace goes to the Control Plane and a Cloud
// Workspace goes to the Workspace owner through the /api/v2 BFF.
import assert from "node:assert/strict";
import test from "node:test";

import { isAgentLaunchSurface, type AgentLaunchSurfaceProbe } from "../src/app/workspace-launch-controller-model.ts";

const reviewAgentLaunch = () => undefined;
const readyCapabilityVersion = { id: "capv-alpha", status: "ready" };

function probe(overrides: Partial<AgentLaunchSurfaceProbe>): AgentLaunchSurfaceProbe {
  return { cloudIdentity: false, agentCapabilityVersions: [], reviewAgentLaunch, ...overrides };
}

test("a Legacy Workspace identity is not routed into the Agent/BFF launch flow", () => {
  assert.equal(
    isAgentLaunchSurface(probe({ cloudIdentity: false, agentCapabilityVersions: [] })),
    false,
    "a Control Plane identity must never be handed the Workspace owner launch page"
  );
});

test("a Cloud Workspace identity with ready CapabilityVersions reaches the Agent/BFF launch flow", () => {
  assert.equal(
    isAgentLaunchSurface(probe({ cloudIdentity: true, agentCapabilityVersions: [readyCapabilityVersion] })),
    true
  );
});

test("the loaded CapabilityVersion list does not decide the launch surface", () => {
  // Same signed-in identity, same mounted controller, before any owner read has
  // answered. An empty list is a successful read of "nothing ready", not a
  // statement about who is signed in. This also keeps a Cloud identity that
  // launches the default OPL App from an approved Runtime Release alone, which
  // legitimately carries no CapabilityVersion.
  assert.equal(isAgentLaunchSurface(probe({ cloudIdentity: true, agentCapabilityVersions: [] })), true);
  assert.equal(isAgentLaunchSurface(probe({ cloudIdentity: false, agentCapabilityVersions: [] })), false);
});

test("a controller without the Agent launch action is never an Agent launch surface", () => {
  assert.equal(
    isAgentLaunchSurface(probe({ cloudIdentity: true, agentCapabilityVersions: [readyCapabilityVersion], reviewAgentLaunch: undefined })),
    false
  );
});
