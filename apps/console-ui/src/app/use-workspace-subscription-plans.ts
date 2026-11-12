import { useEffect, useState } from "react";

import type { AuthSession, ComputePlanDTO, StoragePlanDTO } from "../api/dtos.ts";
import { listAvailableComputePlans, listAvailableStoragePlans } from "../api/workspaces-api.ts";

type PlanRead<T> = { status: "loading" | "unavailable" } | { status: "available"; items: T[] };
type ScopedRead<T> = { session: AuthSession; revision: number; result: PlanRead<T> };
export type WorkspaceSubscriptionPlans = {
  compute: PlanRead<ComputePlanDTO>;
  storage: PlanRead<StoragePlanDTO>;
  refresh: () => void;
};

// The Billing page shares these two directory reads across its rows. Each source
// settles independently; neither supplies accepted subscription prices.
export function useWorkspaceSubscriptionPlans(session: AuthSession | null, enabled = true): WorkspaceSubscriptionPlans {
  const [compute, setCompute] = useState<ScopedRead<ComputePlanDTO>>();
  const [storage, setStorage] = useState<ScopedRead<StoragePlanDTO>>();
  const [revision, setRevision] = useState(0);
  const allowed = enabled && !!session?.user.accountId && session.user.status === "active" && (session.user.role === "owner" || session.user.role === "admin");
  useEffect(() => {
    setCompute(undefined);
    setStorage(undefined);
    if (!session || !allowed) return;
    let current = true;
    // Existing list APIs read every page, without availability or pairing filters.
    // They expose no AbortSignal; cleanup and scoped rendering reject late replies.
    void listAvailableComputePlans().then((items) => {
      if (current) setCompute({ session, revision, result: { status: "available", items } });
    }).catch(() => {
      if (current) setCompute({ session, revision, result: { status: "unavailable" } });
    });
    void listAvailableStoragePlans().then((items) => {
      if (current) setStorage({ session, revision, result: { status: "available", items } });
    }).catch(() => {
      if (current) setStorage({ session, revision, result: { status: "unavailable" } });
    });
    return () => { current = false; };
  }, [session, allowed, revision]);

  const scoped = <T,>(read: ScopedRead<T> | undefined): PlanRead<T> => !allowed ? { status: "unavailable" }
    : read?.session === session && read.revision === revision ? read.result : { status: "loading" };
  return { compute: scoped(compute), storage: scoped(storage), refresh: () => setRevision((value) => value + 1) };
}
