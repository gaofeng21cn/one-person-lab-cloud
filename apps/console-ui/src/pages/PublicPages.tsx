import {
  AlertCircle,
  ArrowLeft,
  ArrowRight,
  Cloud,
  Eye,
  EyeOff,
  KeyRound,
  LockKeyhole,
  ReceiptText,
  RefreshCw
} from "lucide-react";
import { useState, type FormEvent } from "react";

import type { ConsoleController } from "../app/use-console-controller.ts";
import { Button } from "../components/ui/index.ts";
import "./public-pages.css";

function PublicBrand({ controller, target = "/" }: { controller?: ConsoleController; target?: string }) {
  return (
    <a
      className="brand"
      href={target}
      onClick={(event) => {
        if (!controller) return;
        event.preventDefault();
        controller.navigate(target);
      }}
    >
      <img alt="OPL Cloud" src="/opl-app-icon.png" />
      <strong>OPL Cloud</strong>
    </a>
  );
}

export function PublicHome({ controller }: { controller: ConsoleController }) {
  return (
    <div className="access-page">
      <header className="public-nav-wrap">
        <nav aria-label="公共导航" className="public-nav">
          <PublicBrand controller={controller} />
          <div className="public-nav-actions">
            <Button onClick={() => controller.navigate("/login")} size="sm" variant="outline">登录</Button>
          </div>
        </nav>
      </header>

      <main className="access-main">
        <div className="access-hero-grid">
          <section aria-labelledby="home-heading" className="access-hero">
            <div className="access-identity">
              <img alt="" src="/opl-app-icon.png" />
              <h1 id="home-heading">OPL Cloud</h1>
            </div>
            <p className="access-tagline">让 <span className="nowrap-brand">One Person Lab</span> 在云端持续运行</p>
            <p className="access-lede">管理在线工作空间、统一治理 AI API 密钥与用量，随时掌握账户余额与账单明细。</p>
            <div className="access-actions">
              <Button color="primary" onClick={() => controller.navigate("/login")} size="lg">登录 OPL Cloud<ArrowRight aria-hidden size={16} /></Button>
              <span className="access-note">面向已开通账户开放使用</span>
            </div>
          </section>

          <figure className="access-showcase">
            <img alt="OPL Cloud 把本地项目、在线工作空间、AI 接入与账单连接为一条工作链" decoding="async" height={853} src="/opl-cloud-overview.jpg" width={1280} />
            <figcaption className="access-showcase__caption">工作空间 · OPL Gateway · 账户余额与账单</figcaption>
          </figure>
        </div>

        <section aria-labelledby="home-capabilities-heading" className="access-features-section">
          <h2 id="home-capabilities-heading">核心能力</h2>
          <ul aria-label="产品能力" className="access-features">
            <li>
              <div className="access-features__head">
                <span className="access-features__icon access-features__icon--sky" aria-hidden="true"><KeyRound size={18} /></span>
                <span className="access-features__tag">AI Gateway</span>
              </div>
              <div className="access-features__body">
                <strong>OPL Gateway 与 AI API</strong>
                <span>统一签发和管理 API 密钥，实时掌握本月实际消费与请求明细。</span>
              </div>
            </li>
            <li>
              <div className="access-features__head">
                <span className="access-features__icon access-features__icon--emerald" aria-hidden="true"><Cloud size={18} /></span>
                <span className="access-features__tag">Workspace</span>
              </div>
              <div className="access-features__body">
                <strong>在线工作空间</strong>
                <span>创建、启停并访问云端工作空间，内置默认应用或自定义智能体运行环境。</span>
              </div>
            </li>
            <li>
              <div className="access-features__head">
                <span className="access-features__icon" aria-hidden="true"><ReceiptText size={18} /></span>
                <span className="access-features__tag">Billing</span>
              </div>
              <div className="access-features__body">
                <strong>余额与账单</strong>
                <span>同一账户余额覆盖工作空间月费与 API 消费，消费明细清晰可查。</span>
              </div>
            </li>
          </ul>
        </section>
      </main>

      <footer className="public-footer">
        <div className="public-footer__inner">
          <span><strong>OPL Cloud</strong> · 云端工作空间与 AI API 统一控制台</span>
          <span>如需开通账户，请联系平台管理员</span>
        </div>
      </footer>
    </div>
  );
}

export function LoginPage({ controller }: { controller: ConsoleController }) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (email.trim() && password) void controller.submitLogin(email.trim(), password);
  };

  return (
    <main className="login-page">
      <aside aria-label="OPL Cloud 平台概览" className="login-showcase">
        <div className="login-showcase__top">
          <PublicBrand controller={controller} />
          <button className="login-back-link" onClick={() => controller.navigate("/")} type="button">
            <ArrowLeft aria-hidden size={15} />返回
          </button>
        </div>

        <div className="login-showcase__main">
          <div className="login-showcase__copy">
            <h2>让 <span className="nowrap-brand">One Person Lab</span> 在云端持续运行</h2>
            <p>一个统一控制台，连接 OPL Gateway API 接入、在线工作空间管理与账单结算。</p>
          </div>

          <div className="login-metrics-pair" aria-hidden="true">
            <div className="login-metric-card">
              <div className="login-metric-card__head">
                <span>OPL GATEWAY</span>
                <span className="login-led login-led--ok" />
              </div>
              <strong>API 密钥与用量</strong>
              <small>OpenAI 兼容接入 · 实时统计</small>
            </div>
            <div className="login-metric-card">
              <div className="login-metric-card__head">
                <span>WORKSPACES</span>
                <span className="login-led login-led--sky" />
              </div>
              <strong>云端运行环境</strong>
              <small>按账户权限交付与管理</small>
            </div>
          </div>

          <div className="login-ledger">
            <div className="login-ledger__head">
              <span>平台模块</span>
              <span className="login-ledger__pill"><span aria-hidden="true" className="access-dot" />OPL Cloud</span>
            </div>
            <div className="login-ledger__row">
              <span className="login-ledger__icon login-ledger__icon--sky" aria-hidden="true"><KeyRound size={15} /></span>
              <span className="login-ledger__text">
                <strong>OPL Gateway 与 AI API</strong>
                <small>查看可用余额、管理 API 密钥与调用用量</small>
              </span>
              <span className="login-ledger__badge">API</span>
            </div>
            <div className="login-ledger__row">
              <span className="login-ledger__icon login-ledger__icon--emerald" aria-hidden="true"><Cloud size={15} /></span>
              <span className="login-ledger__text">
                <strong>在线工作空间</strong>
                <small>云端交付、模型配置与运行入口管理</small>
              </span>
              <span className="login-ledger__badge">Workspace</span>
            </div>
            <div className="login-ledger__row">
              <span className="login-ledger__icon" aria-hidden="true"><ReceiptText size={15} /></span>
              <span className="login-ledger__text">
                <strong>余额与账单记录</strong>
                <small>工作空间订阅、扣款趋势与账单明细</small>
              </span>
              <span className="login-ledger__badge">Billing</span>
            </div>
          </div>
        </div>

        <footer className="login-showcase__foot">OPL Cloud · 云端工作空间与 AI API 控制台</footer>
      </aside>

      <section aria-labelledby="login-heading" className="login-stage">
        <div className="login-mobile-bar">
          <button className="login-back-link" onClick={() => controller.navigate("/")} type="button">
            <ArrowLeft aria-hidden size={15} />返回
          </button>
          <PublicBrand controller={controller} />
        </div>

        <div className="login-card">
          <div className="login-card__head">
            <h1 id="login-heading">登录 OPL Cloud</h1>
            <p>使用你的 OPL Cloud 账户邮箱与密码登录。</p>
          </div>

          <form className="login-form" onSubmit={submit}>
            <div className="login-field">
              <label htmlFor="login-email">邮箱</label>
              <input
                autoCapitalize="none"
                autoComplete="username"
                autoCorrect="off"
                className="login-input"
                enterKeyHint="next"
                id="login-email"
                inputMode="email"
                name="email"
                onChange={(event) => setEmail(event.currentTarget.value)}
                placeholder="name@example.com"
                required
                spellCheck={false}
                type="email"
                value={email}
              />
            </div>

            <div className="login-field">
              <label htmlFor="login-password">密码</label>
              <div className="login-password-wrap">
                <input
                  autoComplete="current-password"
                  className="login-input login-input--password"
                  enterKeyHint="done"
                  id="login-password"
                  name="password"
                  onChange={(event) => setPassword(event.currentTarget.value)}
                  placeholder="请输入密码"
                  required
                  type={showPassword ? "text" : "password"}
                  value={password}
                />
                <button
                  aria-label={showPassword ? "隐藏明文" : "显示明文"}
                  aria-pressed={showPassword}
                  className="login-password-toggle"
                  onClick={() => setShowPassword((visible) => !visible)}
                  title={showPassword ? "隐藏明文" : "显示明文"}
                  type="button"
                >
                  {showPassword ? <EyeOff aria-hidden size={16} /> : <Eye aria-hidden size={16} />}
                </button>
              </div>
            </div>

            {controller.authError ? (
              <div className="login-error" role="alert">
                <AlertCircle aria-hidden size={16} />
                <span>{controller.authError}</span>
              </div>
            ) : null}

            <Button block busy={controller.authStatus === "checking"} color="primary" size="lg" type="submit">登录</Button>
          </form>

          <p className="login-footnote">如需开通新账户、申请工作空间或重置密码，请联系平台管理员。</p>
        </div>
      </section>
    </main>
  );
}

export function SessionRecovery({ controller }: { controller: ConsoleController }) {
  const failed = controller.authStatus === "error";
  return (
    <main className="message-page">
      <section className="status-card">
        <span className={`status-card__icon ${failed ? "status-card__icon--danger" : ""}`} aria-hidden="true">
          {failed ? <AlertCircle size={22} /> : <span className="spinner" />}
        </span>
        <span className="status-card__code">{failed ? "AUTH · ERROR" : "AUTH · VERIFYING"}</span>
        <h1>{failed ? "无法恢复登录" : "正在恢复登录"}</h1>
        <p>{failed ? controller.authError || "身份服务暂不可用。" : "正在确认当前登录状态…"}</p>
        {failed ? <Button color="primary" onClick={() => controller.navigate("/login")}>重新登录</Button> : null}
      </section>
    </main>
  );
}

export function LogoutRecovery({ controller }: { controller: ConsoleController }) {
  const unconfirmed = controller.authStatus === "logout_unconfirmed";
  return (
    <main className="message-page" data-auth-state={controller.authStatus}>
      <section className="status-card">
        <span className={`status-card__icon ${unconfirmed ? "status-card__icon--warning" : ""}`} aria-hidden="true">
          {unconfirmed ? <AlertCircle size={22} /> : <span className="spinner" />}
        </span>
        <span className="status-card__code">{unconfirmed ? "SESSION · PENDING" : "SESSION · CLOSING"}</span>
        <h1>{unconfirmed ? "退出未确认" : "正在安全退出"}</h1>
        <p>{unconfirmed ? controller.authError : "正在确认退出登录…"}</p>
        {unconfirmed ? <Button color="primary" onClick={() => void controller.signOut()}><RefreshCw aria-hidden size={16} />重试退出</Button> : null}
      </section>
    </main>
  );
}

export function ForbiddenPage({ controller }: { controller: ConsoleController }) {
  return (
    <main className="message-page">
      <section className="status-card">
        <span className="status-card__icon" aria-hidden="true"><LockKeyhole size={22} /></span>
        <span className="status-card__code">403 · FORBIDDEN</span>
        <h1>无权访问</h1>
        <p>当前账户没有管理权限。</p>
        <Button color="primary" onClick={() => controller.navigate("/console/overview")}>返回概览</Button>
      </section>
    </main>
  );
}

export function NotFoundPage({ controller }: { controller: ConsoleController }) {
  return (
    <main className="message-page">
      <section className="status-card">
        <span className="status-card__icon" aria-hidden="true"><Cloud size={22} /></span>
        <span className="status-card__code">404 · NOT FOUND</span>
        <h1>页面不存在</h1>
        <p>你访问的页面不存在或暂未开放。</p>
        <Button color="primary" onClick={() => controller.navigate(controller.session ? "/console/overview" : "/")}>返回</Button>
      </section>
    </main>
  );
}
