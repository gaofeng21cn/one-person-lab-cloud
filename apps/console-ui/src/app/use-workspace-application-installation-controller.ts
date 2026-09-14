import { useCallback, useEffect, useRef, useState } from "react";

import type { AuthSession, WorkspaceDTO } from "../api/dtos.ts";
import { resumeWorkspaceApplicationInstallation } from "../api/workspaces-api.ts";

interface WorkspaceApplicationInstallationDependencies {
  session: AuthSession | null;
  workspace: WorkspaceDTO | null;
  workspaceId: string;
  currentMutationRequest: () => () => boolean;
  refreshWorkspace: () => Promise<void>;
  flash: (message: string, tone?: "good" | "danger") => void;
  mutationError: (error: unknown) => string;
}

export function useWorkspaceApplicationInstallationController({ session, workspace, workspaceId, currentMutationRequest, refreshWorkspace, flash, mutationError }: WorkspaceApplicationInstallationDependencies) {
  const [busy, setBusy] = useState(false);
  const generation = useRef(0);
  const scope = useRef({ workspaceId, userId: session?.user.id, csrfToken: session?.csrfToken });
  scope.current = { workspaceId, userId: session?.user.id, csrfToken: session?.csrfToken };
  const reset = useCallback(() => { generation.current += 1; setBusy(false); }, []);
  useEffect(() => { reset(); return reset; }, [reset, workspaceId, session?.user.id, session?.csrfToken]);

  const installation = workspace?.id === workspaceId ? workspace.applicationInstallation : null;
  useEffect(() => {
    if (!session || busy || !installation || !["pending", "running"].includes(installation.status)) return;
    const timer = setTimeout(() => void refreshWorkspace(), 2000);
    return () => clearTimeout(timer);
  }, [session, busy, installation, refreshWorkspace]);

  const resume = useCallback(async (): Promise<boolean> => {
    if (!session || busy || !installation?.canResume || !workspaceId) return false;
    const request = ++generation.current;
    const requestStillCurrent = currentMutationRequest();
    const ownsRequest = () => request === generation.current && requestStillCurrent() && scope.current.workspaceId === workspaceId
      && scope.current.userId === session.user.id && scope.current.csrfToken === session.csrfToken;
    setBusy(true);
    try {
      const result = await resumeWorkspaceApplicationInstallation(workspaceId, installation.operationId, session.csrfToken);
      if (!ownsRequest()) return false;
      if (result.workspaceId !== workspaceId || result.applicationInstallation && (result.applicationInstallation.applicationId !== installation.applicationId || result.applicationInstallation.revision !== installation.revision)) {
        throw new Error("workspace_application_installation_identity_mismatch");
      }
      await refreshWorkspace();
      if (ownsRequest()) flash("已继续原应用安装");
      return ownsRequest();
    } catch (error) {
      if (ownsRequest()) flash(mutationError(error), "danger");
      return false;
    } finally {
      if (ownsRequest()) setBusy(false);
    }
  }, [busy, currentMutationRequest, flash, installation, mutationError, refreshWorkspace, session, workspaceId]);
  return { busy, resume, reset };
}
