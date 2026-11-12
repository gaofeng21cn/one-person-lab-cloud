import { useState, type FormEvent } from "react";
import { parseRuntimeAdmission, type RuntimeAdmissionDraft } from "../api/admin-runtime-api.ts";
import { useAdminRuntimeController } from "../app/use-admin-runtime-controller.ts";
import { Badge, Button, Field } from "../components/ui/index.ts";

export function AdminRuntimeManager({ csrfToken }: { csrfToken: string }) {
  const controller = useAdminRuntimeController(csrfToken);
  const [draft, setDraft] = useState<RuntimeAdmissionDraft>({ name: "", versionLabel: "", publisherNamespaceId: "", admissionReceiptId: "", publisherContractJson: "" });
  const [formError, setFormError] = useState("");
  const update = (key: keyof RuntimeAdmissionDraft, value: string) => setDraft(current => ({ ...current, [key]: value }));
  const submit = (event: FormEvent) => {
    event.preventDefault();
    try {
      const input = parseRuntimeAdmission(draft);
      setFormError("");
      void controller.admit(input);
    } catch (error) { setFormError(error instanceof Error ? error.message : "请核对准入字段。"); }
  };

  return <section className="panel" aria-label="Runtime 版本管理">
    <div className="panel-title"><div><h2>Runtime 版本管理</h2></div><Button busy={controller.loading} disabled={controller.busy} onClick={() => void controller.refresh()} size="sm" variant="outline">刷新版本目录</Button></div>
    <p>准入新的不可变 Runtime 版本，供后续应用交付和构建选择。已有版本的镜像 digest 保持固定，客户 Workspace 不会自动更新。</p>
    {controller.readError ? <p role="alert">目录读取失败：{controller.readError}</p> : null}
    {controller.loading ? <p role="status">正在读取 Runtime 版本…</p> : null}
    {controller.versions ? controller.versions.length ? <div className="table-wrap"><table className="ops-table">
      <thead><tr><th>Runtime</th><th>准入状态</th><th>默认构建</th><th>不可变版本</th></tr></thead>
      <tbody>{controller.versions.map(version => <tr key={version.id}>
        <td><strong>{version.name}</strong><small>{version.versionLabel}</small></td>
        <td><Badge color={version.status === "approved" ? "success" : version.status === "revoked" ? "danger" : "warning"}>{({ approved: "已批准", deprecated: "已弃用", revoked: "已撤销" })[version.status]}</Badge></td>
        <td>{version.defaultForNewBuilds ? "当前默认" : "—"}</td>
        <td><details><summary>查看版本事实</summary><dl className="data-list">
          <div><dt>版本 ID</dt><dd>{version.id}</dd></div>
          <div><dt>镜像 digest</dt><dd style={{ overflowWrap: "anywhere" }}>{version.artifactDigest}</dd></div>
          <div><dt>Runtime ABI</dt><dd>{version.runtimeAbiVersion}</dd></div>
          <div><dt>发布者</dt><dd>{version.publisherNamespaceId}</dd></div>
          <div><dt>准入回执</dt><dd>{version.admissionReceiptId}</dd></div>
          <div><dt>契约 digest</dt><dd style={{ overflowWrap: "anywhere" }}>{version.publisherContractDigest}</dd></div>
          <div><dt>创建时间</dt><dd>{version.createdAt}</dd></div>
        </dl></details></td>
      </tr>)}</tbody>
    </table></div> : <p>Runtime Control 目录暂无版本，可在下方准入新版本。</p> : null}
    <details>
      <summary>准入新 Runtime 版本</summary>
      <p>填写新版本身份及已有准入材料；完整契约由 Runtime Control 校验。</p>
      <form onSubmit={submit}>
        <fieldset disabled={controller.busy || controller.pending}>
          <Field label="Runtime 名称" value={draft.name} onChange={event => update("name", event.target.value)} required />
          <Field label="版本标签" value={draft.versionLabel} onChange={event => update("versionLabel", event.target.value)} required />
          <details><summary>工程准入参数</summary>
            <Field label="发布者命名空间 ID" value={draft.publisherNamespaceId} onChange={event => update("publisherNamespaceId", event.target.value)} />
            <Field label="准入回执 ID" value={draft.admissionReceiptId} onChange={event => update("admissionReceiptId", event.target.value)} />
            <Field label="Runtime 发布契约 JSON" description="粘贴完整 RuntimePublisherContract，保留发布者提供的 image、ABI、访问和数据契约。" multiline rows={12} value={draft.publisherContractJson} onChange={event => update("publisherContractJson", event.target.value)} />
          </details>
          <Button type="submit" color="primary" busy={controller.busy}>提交新版本准入</Button>
        </fieldset>
      </form>
      {formError ? <p role="alert">{formError}</p> : null}
      {controller.commandError ? <p role="alert">{controller.commandError}</p> : null}
      {controller.pending && !controller.busy ? <><p>准入结果尚未确认。保留本次材料重试，避免重复准入。</p><Button onClick={() => void controller.retry()} variant="outline">重试本次准入与读回</Button></> : null}
      {controller.confirmed ? <p role="status">已从 Runtime Control 目录确认新版本：{controller.confirmed.name} / {controller.confirmed.versionLabel}（{controller.confirmed.id}）。</p> : null}
    </details>
  </section>;
}
