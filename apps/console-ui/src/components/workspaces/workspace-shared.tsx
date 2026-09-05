import { ChevronLeft, ChevronRight } from "lucide-react";
import type { ReactNode } from "react";

import type { SourceEnvelope } from "../../api/dtos.ts";
import { Button } from "../ui/index.ts";

export interface WorkspaceNavigation {
  navigate: (path: string) => void;
}

export function sourceData<T>(source: SourceEnvelope<T> | null | undefined): T | null {
  return source?.available ? source.data : null;
}

export function billingUnitLabel(billingUnit?: string) {
  if (billingUnit === "calendar_month") return "按自然月计费";
  return "暂不可用";
}

export function PageLink({ children, controller, path, className = "" }: { children: ReactNode; controller: WorkspaceNavigation; path: string; className?: string }) {
  return <a className={className} href={path} onClick={(event) => { event.preventDefault(); controller.navigate(path); }}>{children}</a>;
}

export function Pagination({ current, pages, onChange, label }: { current: number; pages: number; onChange: (page: number) => void; label: string }) {
  if (pages <= 1) return null;
  return <nav aria-label={label} className="pagination"><Button disabled={current <= 1} onClick={() => onChange(current - 1)} size="sm" variant="outline"><ChevronLeft aria-hidden size={16} />上一页</Button><span>第 {current} / {pages} 页</span><Button disabled={current >= pages} onClick={() => onChange(current + 1)} size="sm" variant="outline">下一页<ChevronRight aria-hidden size={16} /></Button></nav>;
}
