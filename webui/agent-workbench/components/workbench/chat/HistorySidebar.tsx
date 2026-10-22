/** Official assistant-ui ThreadList composition; state is owned by the server adapter. */
import { ThreadListPrimitive, ThreadListItemPrimitive, useAui, useAuiState } from "@assistant-ui/react";
import { BookOpen, Check, MessageSquare, PanelLeftClose, Pencil, Plus, Search, Trash2, X } from "lucide-react";
import { useState } from "react";

function ThreadItem() {
  const aui = useAui();
  const title = useAuiState((state) => state.threadListItem.title) ?? "新对话";
  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState(title);
  return <ThreadListItemPrimitive.Root className="aui-thread-list-item">
    {editing ? <form className="thread-rename" onSubmit={(event) => {
      event.preventDefault();
      aui.threadListItem().rename(value.trim());
      setEditing(false);
    }}>
      <input aria-label="会话名称" autoFocus value={value} onChange={(event) => setValue(event.target.value)} maxLength={80} />
      <button className="message-action" title="保存名称" disabled={!value.trim()}><Check /></button>
      <button className="message-action" type="button" title="取消重命名" onClick={() => setEditing(false)}><X /></button>
    </form> : <>
      <ThreadListItemPrimitive.Trigger className="aui-thread-trigger">
        <MessageSquare aria-hidden="true" /><span><ThreadListItemPrimitive.Title fallback="新对话" /></span>
      </ThreadListItemPrimitive.Trigger>
      <div className="thread-item-actions">
        <button type="button" className="message-action" title="重命名会话" onClick={() => { setValue(title); setEditing(true); }}><Pencil /></button>
        <ThreadListItemPrimitive.Delete className="message-action" title="删除会话"><Trash2 /></ThreadListItemPrimitive.Delete>
      </div>
    </>}
  </ThreadListItemPrimitive.Root>;
}

export function HistorySidebar({ open, onClose, title }: { open: boolean; onClose: () => void; title: string }) {
  const [search, setSearch] = useState("");
  const ids = useAuiState((state) => state.threads.threadIds);
  const items = useAuiState((state) => state.threads.threadItems);
  return <aside className="history-sidebar" data-open={open || undefined} aria-label="会话列表" data-component-id="workbench.chat.history_sidebar">
    <header className="sidebar-brand"><BookOpen aria-hidden="true" /><strong>{title}</strong>
      <button className="sidebar-close" type="button" title="关闭会话列表" onClick={onClose}><PanelLeftClose /></button>
    </header>
    <ThreadListPrimitive.Root className="aui-thread-list-root">
      <ThreadListPrimitive.New className="new-thread-button" onClick={onClose}><Plus /><span>新对话</span></ThreadListPrimitive.New>
      <label className="history-search"><Search aria-hidden="true" /><input type="search" aria-label="搜索会话" placeholder="搜索会话" value={search} onChange={(event) => setSearch(event.target.value)} /></label>
      <div className="history-items">
        {ids.map((id, index) => (items.find((item) => item.id === id)?.title ?? "新对话").toLowerCase().includes(search.toLowerCase())
          ? <ThreadListPrimitive.ItemByIndex key={id} index={index} components={{ ThreadListItem: ThreadItem }} /> : null)}
      </div>
    </ThreadListPrimitive.Root>
  </aside>;
}
