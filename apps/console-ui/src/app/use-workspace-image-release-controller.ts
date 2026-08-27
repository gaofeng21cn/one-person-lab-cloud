import { useCallback, useEffect, useRef, useState } from "react";

import { activateOperatorWorkspaceImageRelease } from "../api/console-read-api.ts";
import type { AuthSession, OperatorWorkspaceRuntimeImagePolicyDTO, SourceEnvelope } from "../api/dtos.ts";
import type { WorkspaceImageReleaseController } from "./console-controller-types.ts";
import {
  resolveWorkspaceImageReleaseActivationIntent,
  workspaceImageReleaseActivationIdempotencyKey,
  workspaceImageReleaseActivationMatches,
  type WorkspaceImageReleaseActivationIntent
} from "./workspace-image-release-controller-model.ts";

interface WorkspaceImageReleaseDependencies {
  session: AuthSession | null;
  workspaceId: string;
  policy: SourceEnvelope<OperatorWorkspaceRuntimeImagePolicyDTO> | null;
  currentMutationRequest: () => () => boolean;
  refreshPolicy: () => Promise<void>;
  refreshPreview: (workspaceId: string) => Promise<void>;
  flash: (text: string, tone?: "good" | "danger") => void;
  mutationError: (error: unknown) => string;
}

export interface WorkspaceImageReleaseCapability extends WorkspaceImageReleaseController {
  reset: () => void;
}

export function useWorkspaceImageReleaseController({
  session,
  workspaceId,
  policy,
  currentMutationRequest,
  refreshPolicy,
  refreshPreview,
  flash,
  mutationError
}: WorkspaceImageReleaseDependencies): WorkspaceImageReleaseCapability {
  const currentPolicy = policy?.available ? policy.data : null;
  const [selectedVersion, setSelectedVersion] = useState("");
  const [busy, setBusy] = useState(false);
  const requestGeneration = useRef(0);
  const intent = useRef<WorkspaceImageReleaseActivationIntent | null>(null);
  const scope = useRef({ userId: session?.user.id || "", csrfToken: session?.csrfToken || "" });
  scope.current = { userId: session?.user.id || "", csrfToken: session?.csrfToken || "" };

  const reset = useCallback(() => {
    requestGeneration.current += 1;
    intent.current = null;
    setSelectedVersion("");
    setBusy(false);
  }, []);

  useEffect(() => {
    reset();
    return reset;
  }, [reset, session?.csrfToken, session?.user.id]);

  useEffect(() => {
    if (!currentPolicy) return;
    setSelectedVersion((current) => currentPolicy.releases.some((release) => release.version === current)
      ? current
      : currentPolicy.active.version);
  }, [currentPolicy]);

  const selectVersion = useCallback((version: string) => {
    if (!currentPolicy?.releases.some((release) => release.version === version)) return;
    intent.current = null;
    setSelectedVersion(version);
  }, [currentPolicy]);

  const activateSelectedRelease = useCallback(async (): Promise<boolean> => {
    if (!session || !currentPolicy || busy || !selectedVersion || selectedVersion === currentPolicy.active.version
      || !currentPolicy.releases.some((release) => release.version === selectedVersion)) return false;
    if (!window.confirm(`确认将 ${selectedVersion} 设为新开通 Workspace 的默认版本？`)) return false;
    const reason = "activate approved Workspace image release for new launches";
    const resolved = resolveWorkspaceImageReleaseActivationIntent(
      intent.current,
      selectedVersion,
      currentPolicy.revision,
      reason,
      () => workspaceImageReleaseActivationIdempotencyKey(() => crypto.randomUUID())
    );
    intent.current = resolved;
    const requestStillCurrent = currentMutationRequest();
    const generation = ++requestGeneration.current;
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    setBusy(true);
    try {
      const result = await activateOperatorWorkspaceImageRelease(
        resolved.releaseVersion,
        resolved.expectedRevision,
        resolved.reason,
        csrfToken,
        resolved.idempotencyKey
      );
      if (generation !== requestGeneration.current || !requestStillCurrent()
        || scope.current.userId !== userId || scope.current.csrfToken !== csrfToken) return false;
      if (!workspaceImageReleaseActivationMatches(result, resolved)) {
        flash("Workspace 默认镜像切换未通过权威读回", "danger");
        return false;
      }
      intent.current = null;
      await refreshPolicy();
      if (workspaceId) await refreshPreview(workspaceId);
      if (generation !== requestGeneration.current || !requestStillCurrent()) return false;
      flash(`新开通 Workspace 默认版本已切换为 ${resolved.releaseVersion}`);
      return true;
    } catch (error) {
      if (generation === requestGeneration.current && requestStillCurrent()) flash(mutationError(error), "danger");
      return false;
    } finally {
      if (generation === requestGeneration.current && requestStillCurrent()) setBusy(false);
    }
  }, [busy, currentMutationRequest, currentPolicy, flash, mutationError, refreshPolicy, refreshPreview, selectedVersion, session, workspaceId]);

  return { selectedVersion, busy, selectVersion, activateSelectedRelease, reset };
}
