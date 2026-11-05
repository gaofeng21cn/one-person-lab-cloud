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
  UserRound,
  UsersRound,
  X
} from "lucide-react";
import { useEffect, useRef, type ComponentType, type ReactNode } from "react";

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
  const sidebarRef = useRef<HTMLElement>(null);
  const mobileMenuRef = useRef<HTMLButtonElement>(null);
  const accountHeadingRef = useRef<HTMLHeadingElement>(null);
  const accountReturnFocusRef = useRef<HTMLElement | null>(null);
  const { sidebarOpen, globalSlide, setSidebarOpen, setGlobalSlide } = controller;
  const adminSurface = controller.route?.surface === "admin";
  const accountStatus = presentAccountStatus(controller.session?.user.status);
  const mobileItems = adminSurface
    ? adminMenu.filter((item) => item.id !== "admin.announcements")
    : customerMenu;
  const openAccount = () => {
    const focused = document.activeElement;
    accountReturnFocusRef.current = focused instanceof HTMLElement && !(sidebarOpen && sidebarRef.current?.contains(focused))
      ? focused
      : mobileMenuRef.current;
    setSidebarOpen(false);
    setGlobalSlide("account");
  };
  const openNavigation = () => {
    setGlobalSlide("");
    setSidebarOpen(true);
  };
  const toggleAccount = () => {
    if (controller.globalSlide) {
      controller.setGlobalSlide("");
      return;
    }
    openAccount();
  };

  useEffect(() => {
    if (!sidebarOpen) return;
    sidebarRef.current?.querySelector<HTMLButtonElement>(".sidebar-close")?.focus();
    return () => { mobileMenuRef.current?.focus(); };
  }, [sidebarOpen]);

  useEffect(() => {
    if (!globalSlide) return;
    accountHeadingRef.current?.focus();
    return () => {
      const target = accountReturnFocusRef.current;
      if (target?.isConnected && target.getClientRects().length) target.focus();
    };
  }, [globalSlide]);

  useEffect(() => {
    if (!sidebarOpen && !globalSlide) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        setSidebarOpen(false);
        setGlobalSlide("");
      }
      if (event.key !== "Tab" || !sidebarOpen || !window.matchMedia("(max-width: 820px)").matches) return;
      const controls = Array.from(sidebarRef.current?.querySelectorAll<HTMLElement>("a[href], button:not(:disabled)") || [])
        .filter((element) => element.getClientRects().length > 0);
      const first = controls[0];
      const last = controls[controls.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last?.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first?.focus();
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [sidebarOpen, globalSlide, setSidebarOpen, setGlobalSlide]);

  return (
    <div className="app-shell">
      {controller.sidebarOpen ? <button aria-label="关闭导航" className="sidebar-scrim" onClick={() => controller.setSidebarOpen(false)} /> : null}
      <aside className={`sidebar ${controller.sidebarOpen ? "open" : ""}`} aria-label="产品导航" id="console-navigation" ref={sidebarRef}>
        <div className="sidebar-head">
          <a className="brand" href="/console/overview" onClick={(event) => { event.preventDefault(); controller.navigate("/console/overview"); }}>
            <img alt="" src="/opl-app-icon.png" />
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
            <button aria-controls="console-account" aria-expanded={Boolean(globalSlide)} aria-label="账号信息" onClick={toggleAccount}>
              <ChevronDown aria-hidden size={17} />
            </button>
          </Tooltip>
        </div>
      </aside>

      <div className="main-column">
        <header className={`topbar ${controller.route?.kind === "customer.overview" ? "topbar-overview" : ""}`}>
          <div className="topbar-title">
            <button aria-controls="console-navigation" aria-expanded={sidebarOpen} aria-label="打开导航" className="mobile-menu" onClick={openNavigation} ref={mobileMenuRef}><Menu aria-hidden size={19} /></button>
            <div>
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
              <Button aria-controls="console-account" aria-expanded={Boolean(globalSlide)} aria-label="账号信息" onClick={openAccount} size="sm" uniform variant="ghost">
                <UserRound aria-hidden size={17} />
              </Button>
            </Tooltip>
            {adminSurface ? (
              <Tooltip content="退出登录">
                <Button aria-label="退出登录" onClick={() => void controller.signOut()} size="sm" uniform variant="ghost"><LogOut aria-hidden size={17} /></Button>
              </Tooltip>
            ) : null}
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
        <div className="account-band global-slide" role="complementary" aria-label="账号信息" id="console-account">
          <div className="account-band-copy">
            <h2 ref={accountHeadingRef} tabIndex={-1}>账号信息</h2>
            <dl className="data-list">
              <div><dt>邮箱</dt><dd>{controller.session?.user.email || "-"}</dd></div>
              <div><dt>身份</dt><dd>{controller.session?.isOperator ? "管理员" : "客户"}</dd></div>
              <div><dt>账户状态</dt><dd>{accountStatus.label}</dd></div>
            </dl>
            {adminSurface ? (
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
            ) : null}
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
