import { useEffect, useState } from "react";

import type { AuthSession } from "../../api/dtos.ts";
import type { ApiError } from "../../api/console-api.ts";
import { getWorkspaceTransactions, getWorkspaceTransactionTrend, getWorkspaceFinancialReceipt, type WorkspaceFinancialReceipt, type WorkspaceTransaction, type WorkspaceTransactionTrend, type WorkspaceTransactionPage } from "../../api/workspace-transactions-api.ts";
import { formatWorkspaceTransactionAmount } from "../../app/workspace-experience-model.ts";
import { formatDate } from "../../console-model.ts";
import { Alert, Button } from "../ui/index.ts";

const statusLabels = { requested: "等待确认", confirmed: "已确认", rejected: "已拒绝", unknown: "结果未确认" };
const kindLabels = { charge: "扣款", refund: "退款", recharge: "充值" };
const purposeLabels: Record<string, string> = {
  base_period: "订阅周期", upgrade_supplement: "升级补差", base_period_delete: "删除退款",
  upgrade_failure_full: "升级失败退款", supplement_delete_unused: "未用补差退款",
  next_period_plan_failure_full: "下周期变更失败退款", recharge: "充值"
};

type ReadState = { session: AuthSession | null; workspaceId: string; cursor: string; revision: number; page?: WorkspaceTransactionPage; error?: string };

export function WorkspaceTransactionsPanel({ session, workspaceId }: { session: AuthSession | null; workspaceId: string }) {
  const allowed = Boolean(session?.user.accountId && (session.user.role === "admin" || session.user.role === "owner"));
  const [query, setQuery] = useState({ session, workspaceId, cursor: "" });
  const [revision, setRevision] = useState(0);
  const [state, setState] = useState<ReadState>();
  // Changing User, Tenant, session or Workspace immediately hides the previous
  // projection, even before React runs the abort cleanup for the old request.
  const cursor = query.session === session && query.workspaceId === workspaceId ? query.cursor : "";
  const current = state?.session === session && state.workspaceId === workspaceId && state.cursor === cursor && state.revision === revision ? state : undefined;
  useEffect(() => {
    if (!allowed) return;
    const controller = new AbortController();
    const scope = { session, workspaceId, cursor, revision };
    getWorkspaceTransactions(workspaceId, cursor, controller.signal).then((page) => {
      if (!controller.signal.aborted) setState({ ...scope, page });
    }).catch((error: ApiError) => {
      if (!controller.signal.aborted) setState({ ...scope, error: error.status === 403 ? "当前账号无权查看团队交易。" : "暂时无法读取交易记录，请重试。" });
    });
    return () => controller.abort();
  }, [allowed, session, workspaceId, cursor, revision]);

  return <section className="panel" aria-label="工作空间交易记录">
    <div className="panel-title"><h2>团队交易记录</h2>{allowed ? <Button size="sm" variant="outline" onClick={() => setRevision((value) => value + 1)}>刷新交易</Button> : null}</div>
    {allowed ? <WorkspaceTransactionTrendView session={session} workspaceId={workspaceId} revision={revision} /> : null}
    {!allowed ? <p>团队交易仅向本团队管理员和所有者开放。</p> : current?.error ? <Alert color="warning" title="交易记录暂不可用" description={current.error} /> : !current?.page ? <p role="status">正在读取交易记录…</p> : <>
      <p>按记录创建时间排列；未确认交易仍需核实，不能视为已扣款或已退款。有收据的交易可进一步读取原始财务详情；尚无收据时会明确提示。</p>
      {current.page.items.length === 0 ? <p>暂无交易记录。</p> : <div className="table-wrap"><table><thead><tr><th>记录时间</th><th>类型</th><th>金额（美元）</th><th>状态</th><th>详情</th></tr></thead><tbody>
        {current.page.items.map((item) => <tr key={item.id}><td>{formatDate(item.createdAt, true)}</td><td>{kindLabels[item.kind]}</td><td>{formatWorkspaceTransactionAmount(item.amountUSDMicros)}</td><td>{statusLabels[item.status]}</td><td><details><summary>查看交易详情</summary><dl className="data-list">
          <div><dt>交易编号</dt><dd>{item.id}</dd></div>
          <div><dt>工作空间编号</dt><dd>{item.workspaceId}</dd></div>
          <div><dt>用途</dt><dd>{item.purpose ? purposeLabels[item.purpose] || item.purpose : "未提供"}</dd></div>
          <div><dt>最近记录时间</dt><dd>{formatDate(item.updatedAt, true)}</dd></div>
          {item.originalChargeOperationId ? <div><dt>原扣款编号</dt><dd>{item.originalChargeOperationId}</dd></div> : null}
          {item.externalReference ? <div><dt>支付凭据编号</dt><dd>{item.externalReference}</dd></div> : null}
          {item.receiptId ? <div><dt>收据编号</dt><dd>{item.receiptId}</dd></div> : null}
          {item.coverageStart && item.coverageEnd ? <div><dt>对应费用期间</dt><dd>{formatDate(item.coverageStart, true)} 至 {formatDate(item.coverageEnd, true)}</dd></div> : null}
          {item.planChangeId ? <div><dt>方案变更编号</dt><dd>{item.planChangeId}</dd></div> : null}
        </dl>{item.receiptId ? <WorkspaceFinancialReceiptView session={session} workspaceId={workspaceId} transaction={item} /> : null}</details></td></tr>)}
      </tbody></table></div>}
      <div className="workspace-actions">
        {cursor ? <Button onClick={() => setQuery({ session, workspaceId, cursor: "" })} variant="outline">返回第一页</Button> : null}
        {current.page.nextCursor ? <Button onClick={() => setQuery({ session, workspaceId, cursor: current.page!.nextCursor! })} variant="outline">下一页交易</Button> : null}
      </div>
    </>}
  </section>;
}

function WorkspaceTransactionTrendView({ session, workspaceId, revision }: { session: AuthSession | null; workspaceId: string; revision: number }) {
  const [state, setState] = useState<{ session: AuthSession | null; workspaceId: string; revision: number; value?: WorkspaceTransactionTrend; error?: string }>();
  const current = state?.session === session && state.workspaceId === workspaceId && state.revision === revision ? state : undefined;
  useEffect(() => {
    const controller = new AbortController();
    const scope = { session, workspaceId, revision };
    getWorkspaceTransactionTrend(workspaceId, controller.signal).then((value) => {
      if (!controller.signal.aborted) setState({ ...scope, value });
    }).catch((error: ApiError) => {
      if (!controller.signal.aborted) setState({ ...scope, error: error.status === 403 ? "当前账号无权查看团队结算趋势。" : "暂时无法读取结算趋势，请刷新重试。" });
    });
    return () => controller.abort();
  }, [session, workspaceId, revision]);
  return <section aria-label="最近14天结算趋势">
    <h3>最近 14 天结算趋势</h3>
    {current?.error ? <p role="alert">{current.error}</p> : !current?.value ? <p role="status">正在读取结算趋势…</p> : <>
      <p>按 UTC 日期和支付确认时间统计，今日截至 {formatDate(current.value.periodEnd, true)}；金额单位为美元。</p>
      <div className="table-wrap"><table><thead><tr><th>日期（UTC）</th><th>已确认扣款</th><th>已确认退款</th></tr></thead><tbody>{current.value.days.map((day) => <tr key={day.date}><td>{day.date}</td><td>{formatWorkspaceTransactionAmount(day.confirmedChargeUSDMicros)}</td><td>{formatWorkspaceTransactionAmount(day.confirmedRefundUSDMicros)}</td></tr>)}</tbody></table></div>
      <p>本窗口创建、结果未知的交易：扣款 {formatWorkspaceTransactionAmount(current.value.unknownChargeUSDMicros)}；退款 {formatWorkspaceTransactionAmount(current.value.unknownRefundUSDMicros)}。</p>
      <p>本窗口创建、等待确认的交易：扣款 {formatWorkspaceTransactionAmount(current.value.requestedChargeUSDMicros)}；退款 {formatWorkspaceTransactionAmount(current.value.requestedRefundUSDMicros)}。以上金额均未计入已确认统计。</p>
    </>}
  </section>;
}

function WorkspaceFinancialReceiptView({ session, workspaceId, transaction }: { session: AuthSession | null; workspaceId: string; transaction: WorkspaceTransaction }) {
  const [requested, setRequested] = useState(false);
  const [state, setState] = useState<{ session: AuthSession | null; workspaceId: string; transaction: WorkspaceTransaction; value?: WorkspaceFinancialReceipt; error?: string }>();
  const current = state?.session === session && state.workspaceId === workspaceId && state.transaction === transaction ? state : undefined;
  useEffect(() => {
    if (!requested) return;
    const controller = new AbortController();
    const scope = { session, workspaceId, transaction };
    getWorkspaceFinancialReceipt(workspaceId, transaction, controller.signal).then((value) => {
      if (!controller.signal.aborted) setState({ ...scope, value });
    }).catch((error: ApiError) => {
      if (!controller.signal.aborted) setState({ ...scope, error: error.status === 404 ? "此交易尚无可读取的原始财务收据。" : error.status === 403 ? "当前账号无权读取此收据。" : "原始财务收据暂不可用，请稍后重试。" });
    });
    return () => controller.abort();
  }, [requested, session, workspaceId, transaction]);
  if (!requested) return <Button size="sm" variant="outline" onClick={() => setRequested(true)}>读取原始财务收据</Button>;
  if (current?.error) return <p role="alert">{current.error}</p>;
  if (!current?.value) return <p role="status">正在读取原始财务收据…</p>;
  const value = current.value;
  return <section aria-label="原始财务收据"><dl className="data-list">
    <div><dt>收据时间</dt><dd>{formatDate(value.receipt.createdAt, true)}</dd></div>
    <div><dt>原报价编号</dt><dd>{value.quote.id}</dd></div>
    <div><dt>原报价金额（美元）</dt><dd>{formatWorkspaceTransactionAmount(value.quote.totalUSDMicros)}</dd></div>
    <div><dt>原始说明</dt><dd>{value.receipt.evidenceSummary}</dd></div>
  </dl><ul>{value.quote.lineItems.map((line, index) => <li key={index}>{({ compute: "计算资源", storage: "存储资源" } as Record<string, string>)[line.kind] || line.kind}：{formatWorkspaceTransactionAmount(line.amountUSDMicros)} 美元</li>)}</ul></section>;
}
