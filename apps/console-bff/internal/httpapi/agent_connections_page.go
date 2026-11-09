package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"html/template"
	"net/http"
)

func (s *Server) agentConnectionsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	caller, err := RequireSession(r.Context(), s.identity, r)
	if err != nil {
		writePublisherIdentityError(w, r, err)
		return
	}
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		writePublisherError(w, r, 503, "page_unavailable", "connection page unavailable")
		return
	}
	value := hex.EncodeToString(nonce[:])
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+value+"'; style-src 'nonce-"+value+"'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = agentConnectionsTemplate.Execute(w, struct{ Nonce, CSRF, Name string }{value, caller.Session.GetCsrfToken(), caller.Session.GetDisplayName()})
}

var agentConnectionsTemplate = template.Must(template.New("agent-connections").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Agent 连接 · OPL Cloud</title>
<style nonce="{{.Nonce}}">
body{margin:0;background:#f5f7fa;color:#17212e;font:16px/1.6 system-ui,sans-serif}main{max-width:760px;margin:48px auto;padding:0 24px}h1{font-size:30px;line-height:1.2}section{padding:24px;background:#fff;border:1px solid #dce3ed;border-radius:14px;margin:20px 0}label{display:block;font-weight:600}input{box-sizing:border-box;width:100%;max-width:420px;padding:10px;margin:8px 0 16px;border:1px solid #9aa8bb;border-radius:6px;font:inherit}button{background:#163f75;color:#fff;border:0;padding:10px 16px;border-radius:6px;font:inherit;cursor:pointer}button:disabled{opacity:.55;cursor:default}li{padding:14px 0;border-bottom:1px solid #e1e7ef;overflow-wrap:anywhere}li button{margin-top:8px}ul{padding:0;list-style:none}small{display:block;color:#536276}a{color:#163f75}code{overflow-wrap:anywhere}#status{min-height:2em} @media(max-width:480px){main{margin-top:28px;padding:0 16px}section{padding:18px}h1{font-size:26px}}
</style></head><body><main><a href="/console/overview">返回控制台</a><h1>连接你的 Agent</h1><p>当前账号：{{.Name}}</p>
<p>让 Agent 读取当前账号的资源方案、工作区、报价及操作进度。授权最长一小时，退出当前 Cloud 会话后失效。</p>
<section><h2>下载连接凭证</h2><form id="issue"><label for="label">连接名称</label><input id="label" name="label" required maxlength="256" placeholder="例如：我的 Codex"><br><button id="download" type="submit">授权只读访问并下载</button></form>
<p>将下载的 JSON 文件交给 Cloud 连接工具导入。凭证只下载一次，请勿粘贴到聊天中。</p><p id="status" role="status" aria-live="polite"></p></section>
<section><h2>已授权连接</h2><p>撤销后，下一次调用即失效。</p><ul id="connections"></ul></section>
</main><script nonce="{{.Nonce}}">
const csrf = {{.CSRF}};
const status = document.getElementById('status');
const button = document.getElementById('download');
const label = document.getElementById('label');
label.addEventListener('input', () => label.setCustomValidity(Array.from(label.value.trim()).length > 128 ? '连接名称最多 128 个字符。' : ''));
async function metadata() {
  const response = await fetch('/api/v2/auth/agent-credentials', {credentials:'same-origin',cache:'no-store'});
  if (!response.ok) throw new Error('无法读取连接，请重新登录后再试。');
  const data = await response.json();
  const list = document.getElementById('connections'); list.replaceChildren();
  if (data.items.length === 0) { const item = document.createElement('li'); item.textContent = '暂无连接。'; list.append(item); }
  for (const entry of data.items) {
    const item = document.createElement('li'); const name = document.createElement('strong'); name.textContent = entry.label; item.append(name);
    const detail = document.createElement('small'); detail.textContent = entry.revokedAt ? '已撤销' : '到期时间：' + new Date(entry.expiresAt).toLocaleString(); item.append(detail);
    if (!entry.revokedAt && Date.parse(entry.expiresAt) > Date.now()) {
      const revoke = document.createElement('button'); revoke.type = 'button'; revoke.textContent = '撤销连接';
      revoke.addEventListener('click', async () => {
        revoke.disabled = true;
        try {
          const result = await fetch('/api/v2/auth/agent-credentials/' + encodeURIComponent(entry.id), {method:'DELETE',credentials:'same-origin',headers:{'X-CSRF-Token':csrf}});
          if (!result.ok) throw new Error('撤销未完成，请刷新连接状态后重试。');
          status.textContent = '连接已撤销。'; await metadata();
        } catch (error) { status.textContent = error.message; revoke.disabled = false; }
      }); item.append(revoke);
    } list.append(item);
  }
}
document.getElementById('issue').addEventListener('submit', async event => {
  event.preventDefault(); button.disabled = true; status.textContent = '';
  try {
    const response = await fetch('/api/v2/auth/agent-credentials', {method:'POST',credentials:'same-origin',cache:'no-store',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:JSON.stringify({label:document.getElementById('label').value})});
    if (!response.ok) throw new Error('授权未完成，请刷新连接状态并检查登录状态后再试。');
    const blob = await response.blob(); const url = URL.createObjectURL(blob);
    const link = document.createElement('a'); link.href = url; link.download = 'opl-cloud-credential.json'; link.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
    status.textContent = '凭证已下载。请通过 Cloud 连接工具导入；若未保存，可撤销后重新签发。'; await metadata();
  } catch (error) { status.textContent = error.message; }
  finally { button.disabled = false; }
});
metadata().catch(error => { status.textContent = error.message; });
</script></body></html>`))
