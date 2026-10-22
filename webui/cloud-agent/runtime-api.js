export function readRuntimeAgents(value) {
  const state = value?.app_state?.app_state;
  const entries = state?.agent_packages?.directory?.entries;
  if (value?.app_state?.version !== "g2" || state?.schema_version !== "opl_app_state.v1" || !Array.isArray(entries)) {
    throw new Error("Runtime returned an unsupported Agent directory response.");
  }
  const choices = [];
  for (const entry of entries) {
    if (entry.package_role !== "standard_agent" || entry.readiness?.operational_ready !== true || entry.readiness?.launch_allowed !== true) continue;
    for (const shortcut of entry.home_shortcuts || []) {
      const route = shortcut.route;
      if (route?.route_kind !== "agent_package_shortcut" || route.executor !== "codex_cli") continue;
      if (!entry.package_id || !shortcut.shortcut_id || !route.codex_visible_entry || !Array.isArray(entry.capability_metadata?.required_skill_ids)) continue;
      choices.push({
        id: JSON.stringify([entry.package_id, shortcut.shortcut_id]),
        label: entry.display_name || entry.package_id,
        selection: {
          package_id: entry.package_id,
          shortcut_id: shortcut.shortcut_id,
          codex_visible_entry: route.codex_visible_entry,
          required_skill_ids: entry.capability_metadata.required_skill_ids
        }
      });
    }
  }
  return { choices, modelReady: state.core?.codex?.model_access_ready === true };
}

export function agentMessageRequest(choice, prompt, threadId) {
  return threadId ? { prompt, threadId } : { prompt, agentSelection: choice.selection };
}

export function readAgentReply(value, expectedThreadId) {
  const turn = value?.canonicalThread?.turns?.find((item) => item.id === value.turnId);
  if (value?.executor !== "codex_app_server" || !value.threadId || !value.turnId ||
      value.canonicalThread?.id !== value.threadId || (expectedThreadId && expectedThreadId !== value.threadId) || !turn) {
    throw new Error("Runtime did not confirm the Agent turn on its conversation.");
  }
  if (turn.status !== "completed") {
    throw new Error(turn.error?.message || `Agent turn ${turn.status || "is unconfirmed"}.`);
  }
  if (typeof value.finalMessage !== "string" || !value.finalMessage.trim()) {
    throw new Error("Runtime completed the turn without an Agent answer.");
  }
  return { threadId: value.threadId, answer: value.finalMessage };
}
