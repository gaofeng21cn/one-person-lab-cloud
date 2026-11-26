export function readRuntimeAgents(value) {
  const state = value?.app_state?.app_state;
  const entries = state?.agent_packages?.directory?.entries;
  if (value?.app_state?.version !== "g2" || state?.schema_version !== "opl_app_state.v1" || !Array.isArray(entries)) {
    throw new Error("运行时返回了不受支持的智能体目录响应。");
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
    throw new Error("运行时没有在对话中确认这一轮智能体回应。");
  }
  if (turn.status !== "completed") {
    // The Runtime owns the failure reason: its message stays the customer-facing
    // fact, and only the fallback wording belongs to this page.
    throw new Error(turn.error?.message || `这一轮智能体回应${turn.status ? `的状态是 ${turn.status}` : "尚未确认"}。`);
  }
  if (typeof value.finalMessage !== "string" || !value.finalMessage.trim()) {
    throw new Error("运行时完成了这一轮，但没有返回智能体回答。");
  }
  return { threadId: value.threadId, answer: value.finalMessage };
}
