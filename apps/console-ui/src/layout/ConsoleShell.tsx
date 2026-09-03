import {
  Activity,
  ChevronDown,
  CircleDollarSign,
  Database,
  LayoutDashboard,
  LogOut,
  Megaphone,
  Menu,
  ReceiptText,
  RefreshCw,
  Server,
  Settings,
  UserRound,
  UsersRound,
  X
} from "lucide-react";
import type { ComponentType, ReactNode } from "react";

import { presentAccountStatus } from "../app/customer-experience-model.ts";
import type { ConsoleNavigationId, ConsoleRoute } from "../app/console-router.ts";
import type { ConsoleController } from "../app/use-console-controller.ts";
import { adminMenu, customerMenu } from "../console-model.ts";
import { Button, Tooltip } from "../components/ui/index.ts";

const icons: Record<string, ComponentType<{ size?: number; "aria-hidden"?: boolean }>> = {
  Activity,
  CircleDollarSign,
  Database,
  LayoutDashboard,
  Megaphone,
  ReceiptText,
  Server,
  UsersRound
};

function isNavigationActive(route: ConsoleRoute | null, navigationId: ConsoleNavigationId) {
  return route?.navigationId === navigationId;
}

function mobileNavigationLabel(item: (typeof customerMenu)[number] | (typeof adminMenu)[number]) {
  return "mobileLabel" in item && typeof item.mobileLabel === "string" ? item.mobileLabel : item.label;
}

function NavigationLink({ controller, item }: { controller: ConsoleController; item: (typeof customerMenu)[number] | (typeof adminMenu)[number] }) {
  const Icon = icons[item.icon] || LayoutDashboard;
  const active = isNavigationActive(controller.route, item.id);
  return (
    <a
      aria-current={active ? "page" : undefined}
      className={active ? "active" : ""}
      href={item.path}
      onClick={(event) => {
        event.preventDefault();
        controller.navigate(item.path);
      }}
    >
      <Icon aria-hidden size={18} />
      <span>{item.label}</span>
    </a>
  );
}

export function ConsoleShell({ children, controller }: { children: ReactNode; controller: ConsoleController }) {
  const adminSurface = controller.route?.surface === "admin";
  const accountStatus = presentAccountStatus(controller.session?.user.status);
  const mobileItems = adminSurface
    ? adminMenu.filter((item) => item.id !== "admin.announcements")
    : customerMenu;

  return (
    <div className="app-shell">
      {controller.sidebarOpen ? <button aria-label="关闭导航" className="sidebar-scrim" onClick={() => controller.setSidebarOpen(false)} /> : null}
      <aside className={`sidebar ${controller.sidebarOpen ? "open" : ""}`} aria-label="产品导航">
        <div className="sidebar-head">
          <a className="brand" href="/console/overview" onClick={(event) => { event.preventDefault(); controller.navigate("/console/overview"); }}>
            <img alt="OPL Cloud" src="/opl-app-icon.png" />
            <strong>OPL Cloud</strong>
          </a>
          <Tooltip content="关闭导航">
            <button aria-label="关闭导航" className="sidebar-close" onClick={() => controller.setSidebarOpen(false)}><X aria-hidden size={18} /></button>
          </Tooltip>
        </div>

        <nav className="side-nav">
          {customerMenu.map((item) => <NavigationLink controller={controller} item={item} key={item.path} />)}

          {controller.session?.isOperator ? (
            <div className="operator-nav">
              <div className="nav-section-label">Admin</div>
              {adminMenu.map((item) => <NavigationLink controller={controller} item={item} key={item.path} />)}
            </div>
          ) : null}
        </nav>

        <div className="sidebar-account">
          <UserRound aria-hidden size={18} />
          <span>
            <strong>{controller.session?.user.email || "-"}</strong>
            <small>{controller.session?.isOperator ? "管理员" : "客户"}</small>
          </span>
          <Tooltip content="账号信息">
            <button aria-label="账号信息" onClick={() => controller.setGlobalSlide(controller.globalSlide ? "" : "account")}>
              <ChevronDown aria-hidden size={17} />
            </button>
          </Tooltip>
        </div>
      </aside>

      <div className="main-column">
        <header className={`topbar ${controller.route?.kind === "customer.overview" ? "topbar-overview" : ""}`}>
          <div className="topbar-title">
            <button aria-label="打开导航" className="mobile-menu" onClick={() => controller.setSidebarOpen(true)}><Menu aria-hidden size={19} /></button>
            <div>
              <span className="eyebrow">{adminSurface ? "Admin" : "Console"}</span>
              <h1>{controller.pageTitle}</h1>
            </div>
          </div>
          <div className="topbar-actions">
            {!adminSurface ? (
              <Tooltip content="消息">
                <Button aria-label="消息" onClick={() => controller.navigate("/console/announcements")} size="sm" uniform variant="ghost">
                  <Megaphone aria-hidden size={17} />
                </Button>
              </Tooltip>
            ) : null}
            <Tooltip content="刷新当前页面">
              <Button aria-label="刷新" onClick={() => void controller.refreshCurrentPage()} size="sm" uniform variant="ghost">
                <RefreshCw aria-hidden size={17} />
              </Button>
            </Tooltip>
            <Tooltip content="账号信息">
              <Button aria-label="账号信息" onClick={() => controller.setGlobalSlide("account")} size="sm" uniform variant="ghost">
                <Settings aria-hidden size={17} />
              </Button>
            </Tooltip>
            <Tooltip content="退出登录">
              <Button aria-label="退出登录" onClick={() => void controller.signOut()} size="sm" uniform variant="ghost"><LogOut aria-hidden size={17} /></Button>
            </Tooltip>
          </div>
        </header>
        <main className="page-content">{children}</main>
      </div>

      <nav aria-label="移动端导航" className={`mobile-bottom-nav ${adminSurface ? "admin-mobile-nav" : ""}`}>
        {mobileItems.map((item) => {
          const Icon = icons[item.icon] || LayoutDashboard;
          const active = isNavigationActive(controller.route, item.id);
          return (
            <a aria-current={active ? "page" : undefined} className={active ? "active" : ""} href={item.path} key={item.path} onClick={(event) => { event.preventDefault(); controller.navigate(item.path); }}>
              <Icon aria-hidden size={18} />
              <span>{mobileNavigationLabel(item)}</span>
            </a>
          );
        })}
      </nav>

      {controller.globalSlide ? (
        <div className="account-band global-slide" role="complementary" aria-label="账号信息">
          <div className="account-band-copy">
            <h2>账号信息</h2>
            <dl className="data-list">
              <div><dt>邮箱</dt><dd>{controller.session?.user.email || "-"}</dd></div>
              <div><dt>身份</dt><dd>{controller.session?.isOperator ? "管理员" : "客户"}</dd></div>
              <div><dt>账户状态</dt><dd>{accountStatus.label}</dd></div>
            </dl>
            <details className="account-technical-details">
              <summary>技术详情</summary>
              <dl className="data-list">
                <div><dt>Account ID</dt><dd><code>{controller.session?.user.accountId || "暂不可用"}</code></dd></div>
                <div><dt>Console User ID</dt><dd><code>{controller.session?.user.consoleUserId || controller.session?.user.id || "暂不可用"}</code></dd></div>
                <div><dt>Sub2API User ID</dt><dd><code>{controller.session?.user.sub2apiUserId || "暂不可用"}</code></dd></div>
                <div><dt>Session ID</dt><dd><code>暂不可用</code></dd></div>
                <div><dt>Session 到期</dt><dd><code>{controller.session?.expiresAt || "暂不可用"}</code></dd></div>
              </dl>
            </details>
          </div>
          <div className="account-band-action">
            <Button onClick={() => void controller.signOut()} variant="outline"><LogOut aria-hidden size={16} />退出登录</Button>
            <Button aria-label="关闭" onClick={() => controller.setGlobalSlide("")} uniform variant="ghost"><X aria-hidden size={18} /></Button>
          </div>
        </div>
      ) : null}

      {controller.toast.text ? <div className={`toast ${controller.toast.tone}`} role="status">{controller.toast.text}</div> : null}
    </div>
  );
}
