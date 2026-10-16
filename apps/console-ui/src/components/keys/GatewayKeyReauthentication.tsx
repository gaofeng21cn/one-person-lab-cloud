import { Alert } from "../ui/index.ts";

export function GatewayKeyReauthentication() {
  const redirect = encodeURIComponent(window.location.pathname + window.location.search);
  return <Alert color="warning" title="API 密钥访问需要重新登录"
    description="请重新登录以恢复 API 密钥与用量读取；当前 Cloud 会话和钱包仍可使用。"
    actions={<a href={`/login?redirect=${redirect}`}>重新登录</a>} />;
}
