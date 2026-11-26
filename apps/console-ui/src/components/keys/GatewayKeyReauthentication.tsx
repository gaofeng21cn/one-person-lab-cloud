import type { HTMLAttributes, MouseEvent } from "react";

import { Alert } from "../ui/index.ts";

export function GatewayKeyReauthentication() {
  const redirect = encodeURIComponent(window.location.pathname + window.location.search);
  const target = `/login?redirect=${redirect}`;
  const alertSemantics = { role: "alert" } satisfies HTMLAttributes<HTMLDivElement>;
  const handleNavigate = (event: MouseEvent<HTMLAnchorElement>) => {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0) return;
    event.preventDefault();
    window.history.pushState(null, "", target);
    window.dispatchEvent(new PopStateEvent("popstate"));
  };
  return <Alert {...alertSemantics} color="warning" title="API 密钥访问需要重新登录"
    description="请重新登录以恢复 API 密钥与用量读取；当前控制台会话和余额仍可使用。"
    actions={<a href={target} onClick={handleNavigate}>重新登录</a>} />;
}
