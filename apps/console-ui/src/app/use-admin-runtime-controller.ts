import { useCallback, useEffect, useRef, useState } from "react";
import {
  admitAdminRuntime, confirmRuntimeAdmission, listAdminRuntimeVersions,
  type AdminRuntimeVersion, type RuntimeAdmission
} from "../api/admin-runtime-api.ts";
import type { ApiError } from "../api/console-api.ts";

type Attempt = { input: RuntimeAdmission; key: string; admitted?: AdminRuntimeVersion };

function errorMessage(error: unknown, beforeAdmission = false): string {
  const status = (error as ApiError | null)?.status;
  if (status === 401) return "会话已失效，请重新登录管理员账户后重试。";
  if (status === 403) return "权限或会话校验未通过，请确认平台管理员权限并重新登录。";
  const detail = error instanceof Error ? error.message : "请求未完成";
  if (status === 404 && beforeAdmission) return `准入引用不存在，请核对已批准的发布者命名空间和准入材料后重新提交：${detail}`;
  if (status === 400 || status === 422) return `请核对准入字段和完整发布契约后重新提交：${detail}`;
  if (status === 409) return `版本身份或幂等请求冲突，请刷新目录并核对已有版本：${detail}`;
  return `${detail}。请检查 Runtime Control 服务后重试。`;
}

export function useAdminRuntimeController(csrfToken: string) {
  const [versions, setVersions] = useState<AdminRuntimeVersion[] | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [readError, setReadError] = useState("");
  const [commandError, setCommandError] = useState("");
  const [confirmed, setConfirmed] = useState<AdminRuntimeVersion | null>(null);
  const [pending, setPending] = useState(false);
  const lifetime = useRef<AbortController | null>(null);
  const readGeneration = useRef(0);
  const locked = useRef(false);
  const attempt = useRef<Attempt | null>(null);

  const refresh = useCallback(async () => {
    const signal = lifetime.current?.signal;
    if (!signal || signal.aborted || locked.current) return;
    const generation = ++readGeneration.current;
    setLoading(true); setReadError(""); setVersions(null);
    try {
      const rows = await listAdminRuntimeVersions(signal);
      if (!signal.aborted && generation === readGeneration.current) setVersions(rows);
    } catch (error) {
      if (!signal.aborted && generation === readGeneration.current) setReadError(errorMessage(error));
    } finally {
      if (!signal.aborted && generation === readGeneration.current) setLoading(false);
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    lifetime.current = controller;
    void refresh();
    return () => controller.abort();
  }, [refresh]);

  const run = useCallback(async (input?: RuntimeAdmission) => {
    const signal = lifetime.current?.signal;
    if (!signal || signal.aborted || locked.current || !csrfToken) return;
    if (!attempt.current) {
      if (!input) return;
      attempt.current = { input, key: crypto.randomUUID() };
    }
    const work = attempt.current;
    locked.current = true;
    ++readGeneration.current;
    setLoading(false); setBusy(true); setPending(true); setCommandError(""); setConfirmed(null);
    try {
      // A lost command response is retried with the same body/key. Once an
      // admission response is known, retries only read the owner's directory.
      work.admitted ??= await admitAdminRuntime(work.input, csrfToken, work.key, signal);
      if (signal.aborted) return;
      const rows = await listAdminRuntimeVersions(signal);
      if (signal.aborted) return;
      const result = confirmRuntimeAdmission(rows, work.admitted);
      setVersions(rows); setReadError(""); setConfirmed(result);
      attempt.current = null; setPending(false);
    } catch (error) {
      if (signal.aborted) return;
      const status = (error as ApiError | null)?.status;
      // These are authoritative command rejections, so editing a corrected
      // request is safe. Read failures after admission retain the attempt.
      if (!work.admitted && status !== undefined && [400, 401, 403, 404, 409, 415, 422].includes(status)) {
        attempt.current = null; setPending(false);
      }
      setCommandError(errorMessage(error, !work.admitted));
    } finally {
      locked.current = false;
      if (!signal.aborted) setBusy(false);
    }
  }, [csrfToken]);

  return { versions, loading, busy, readError, commandError, confirmed, pending, refresh, admit: run, retry: () => run() };
}
