import assert from "node:assert/strict";
import test from "node:test";

import { chromium, type Page } from "playwright";

import { CONSOLE_DEMO_CREDENTIALS, startConsoleDemoServer } from "../../tools/start-console-demo.ts";

/*
 * 页面网格对齐回归：每个页面都不允许出现「内容跑出容器盒」的情况，
 * 包括面板内表格容器向右溢出、表头/单元格文字压到相邻列、窄屏下卡片被挤出面板，
 * 以及同一列内面板标题左边界不一致。
 */
const ROUTES: Array<{ path: string; admin?: boolean }> = [
  { path: "/console/overview" },
  { path: "/console/workspaces" },
  { path: "/console/workspaces/new" },
  { path: "/console/workspaces/ws-1" },
  { path: "/console/api" },
  { path: "/console/api/usage" },
  { path: "/console/api/keys" },
  { path: "/console/billing" },
  { path: "/console/announcements" },
  { path: "/admin/overview", admin: true },
  { path: "/admin/accounts", admin: true },
  { path: "/admin/billing", admin: true },
  { path: "/admin/resources", admin: true },
  { path: "/admin/system", admin: true },
  { path: "/admin/announcements", admin: true }
];

const VIEWPORTS = [
  { name: "desktop", width: 1440, height: 950 },
  { name: "laptop", width: 1280, height: 900 },
  { name: "tablet", width: 1024, height: 900 },
  { name: "mobile", width: 390, height: 844 }
];

interface Finding { where: string; detail: string }

interface MeasurementReport {
  overflow: { clientWidth: number; scrollWidth: number };
  boxes: Array<{ el: string; excessX: number; textOverflow: number; children: Array<{ el: string; overRight: number; overLeft: number }> }>;
  tables: Array<{ table: string; rows: Array<{ cell: string; header: string; overflow: number }> }>;
  panelTitleSpread?: Array<{ left: number; xs: Array<{ x: number; count: number }> }>;
}

/** 把页面测量结果转换成对齐缺陷列表。 */
function toFindings(report: MeasurementReport): Finding[] {
  const findings: Finding[] = [];
  if (report.overflow.scrollWidth > report.overflow.clientWidth + 1) {
    findings.push({ where: "document", detail: `页面横向溢出 ${report.overflow.scrollWidth} > ${report.overflow.clientWidth}` });
  }
  for (const box of report.boxes) {
    const detail = box.children.length
      ? `子元素 ${box.children.map((child) => `${child.el}+${child.overRight}/-${child.overLeft}`).join(", ")}`
      : `文本 +${box.textOverflow}`;
    findings.push({ where: box.el, detail: `内容超出盒子 scroll+${box.excessX}px ${detail}` });
  }
  for (const table of report.tables) {
    for (const row of table.rows) {
      findings.push({ where: table.table, detail: `${row.cell}「${row.header}」超出单元格 ${row.overflow}px` });
    }
  }
  for (const spread of report.panelTitleSpread || []) {
    findings.push({ where: "panel-title", detail: `panelLeft=${spread.left} 标题左边界 ${spread.xs.map((item) => `${item.x}(${item.count})`).join(" ")}` });
  }
  return findings;
}


const MEASURE = `(() => {
  // 2px：吸收不同操作系统字体回退带来的亚像素差异，真实缺陷均远大于此
  const TOLERANCE = 2;
  const round = (value) => Math.round(value * 100) / 100;
  const describe = (el) => {
    const cls = typeof el.className === "string" ? el.className.split(/\\s+/).filter(Boolean).slice(0, 3).join(".") : "";
    return el.tagName.toLowerCase() + (cls ? "." + cls : "");
  };
  const visible = (el) => {
    const style = getComputedStyle(el);
    if (style.display === "none" || style.visibility === "hidden") return false;
    const rect = el.getBoundingClientRect();
    return rect.width > 0 && rect.height > 0;
  };
  // SDK Button 的图标包装 span 比 svg 窄 4px（对称溢出），是组件库内部尺寸，不是布局缺陷
  const skip = (el) => el.closest("[aria-hidden='true']") || el.classList.contains("sr-only") || el.tagName === "SVG" || el.tagName === "TR" || el.tagName === "TBODY" || el.tagName === "THEAD" || String(el.className).includes("_ButtonInner_");
  const report = { overflow: null, boxes: [], tables: [], panels: [] };
  const doc = document.documentElement;
  report.overflow = { clientWidth: doc.clientWidth, scrollWidth: doc.scrollWidth };

  for (const el of document.querySelectorAll("body *")) {
    if (!visible(el) || skip(el)) continue;
    const style = getComputedStyle(el);
    const excess = el.scrollWidth - el.clientWidth;
    const verticalExcess = el.scrollHeight - el.clientHeight;
    const scrollable = style.overflowX === "auto" || style.overflowX === "scroll" || style.overflowY === "auto" || style.overflowY === "scroll";
    if (excess <= 1 && verticalExcess <= 1) continue;
    if (scrollable) continue;
    const rect = el.getBoundingClientRect();
    const contentRight = rect.right - parseFloat(style.borderRightWidth) - parseFloat(style.paddingRight);
    const contentLeft = rect.left + parseFloat(style.borderLeftWidth) + parseFloat(style.paddingLeft);
    const children = Array.from(el.children).filter((child) => visible(child)).map((child) => {
      const childRect = child.getBoundingClientRect();
      const childStyle = getComputedStyle(child);
      const clips = childStyle.overflowX === "hidden" || childStyle.overflowX === "clip";
      return {
        el: describe(child),
        overRight: clips ? 0 : round(childRect.right - contentRight),
        overLeft: clips ? 0 : round(contentLeft - childRect.left),
        width: round(childRect.width)
      };
    }).filter((item) => item.overRight > TOLERANCE || item.overLeft > TOLERANCE);
    const range = document.createRange();
    range.selectNodeContents(el);
    const rects = Array.from(range.getClientRects());
    const textOverflow = rects.length ? round(Math.max(...rects.map((item) => item.right)) - contentRight) : 0;
    if (children.length === 0 && textOverflow <= TOLERANCE && excess <= 2 && verticalExcess <= 2) continue;
    // 单行省略号是既有的、可见的截断设计，不属于“内容跑出盒子”的对齐缺陷
    const ownText = Array.from(el.childNodes).some((node) => node.nodeType === 3 && (node.textContent || "").trim().length > 0);
    if (ownText && style.textOverflow === "ellipsis" && (style.overflowX === "hidden" || style.overflowX === "clip")) continue;
    report.boxes.push({ el: describe(el), excessX: excess, excessY: verticalExcess, textOverflow, width: round(rect.width), overflowX: style.overflowX, overflowY: style.overflowY, text: (el.textContent || "").trim().slice(0, 30), children: children.slice(0, 4) });
  }

  for (const table of document.querySelectorAll("table")) {
    if (!visible(table)) continue;
    const cells = Array.from(table.querySelectorAll("thead tr:last-child > th, tbody > tr:first-child > td"));
    const rows = [];
    for (const cell of cells) {
      if (!visible(cell)) continue;
      const style = getComputedStyle(cell);
      const rect = cell.getBoundingClientRect();
      const contentWidth = rect.width - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight) - parseFloat(style.borderLeftWidth) - parseFloat(style.borderRightWidth);
      const range = document.createRange();
      range.selectNodeContents(cell);
      const rects = Array.from(range.getClientRects());
      const textWidth = rects.length ? Math.max(...rects.map((item) => item.right)) - Math.min(...rects.map((item) => item.left)) : 0;
      let childOverflow = 0;
      for (const child of Array.from(cell.children)) {
        if (!visible(child)) continue;
        const childStyle = getComputedStyle(child);
        if (childStyle.overflowX === "hidden" || childStyle.overflowX === "clip") continue;
        const childRect = child.getBoundingClientRect();
        childOverflow = Math.max(childOverflow, childRect.right - (rect.right - parseFloat(style.paddingRight)));
      }
      // 单元格内被 overflow:hidden 裁切的文本（省略号）不算溢出
      const clippedText = Array.from(cell.querySelectorAll("*")).some((child) => {
        const childStyle = getComputedStyle(child);
        return (childStyle.overflowX === "hidden" || childStyle.overflowX === "clip") && String(child.textContent || "").trim().length > 0;
      });
      const overflow = Math.max(clippedText ? 0 : textWidth - contentWidth, childOverflow);
      if (overflow <= TOLERANCE) continue;
      rows.push({ cell: cell.tagName + ":" + (cell.cellIndex + 1), header: (cell.textContent || "").trim().slice(0, 20), overflow: round(overflow), contentWidth: round(contentWidth), textWidth: round(textWidth), childOverflow: round(childOverflow) });
    }
    if (rows.length) report.tables.push({ table: describe(table), wrapWidth: round((table.closest(".table-wrap") || table).getBoundingClientRect().width), rows });
  }

  // 只有左边界相同的面板才要求标题对齐（并排面板本就处于不同列）
  const byLeft = new Map();
  for (const panel of document.querySelectorAll(".panel")) {
    if (!visible(panel)) continue;
    const title = panel.querySelector(".panel-title h2, .panel-title strong, .workspace-heading h2");
    if (!title || !visible(title)) continue;
    const left = round(panel.getBoundingClientRect().left);
    const x = round(title.getBoundingClientRect().left);
    const entry = byLeft.get(left) || { xs: new Map(), cls: [] };
    entry.xs.set(x, (entry.xs.get(x) || 0) + 1);
    entry.cls.push(describe(panel));
    byLeft.set(left, entry);
    report.panels.push({ cls: describe(panel), titleX: x, panelLeft: left });
  }
  const spreads = [];
  for (const [left, entry] of byLeft) {
    if (entry.xs.size <= 1) continue;
    spreads.push({ left, xs: Array.from(entry.xs.entries()).map(([x, count]) => ({ x, count })) });
  }
  if (spreads.length) report.panelTitleSpread = spreads;
  return report;
})()`;

async function login(page: Page, origin: string, admin: boolean) {
  const credentials = admin ? CONSOLE_DEMO_CREDENTIALS.admin : CONSOLE_DEMO_CREDENTIALS.customer;
  await page.goto(`${origin}/login`, { waitUntil: "domcontentloaded" });
  await page.getByLabel("邮箱").fill(credentials.email);
  await page.getByLabel("密码").fill(credentials.password);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL(/\/console\/overview$/);
}

test("Console pages keep table, card and panel content inside their boxes", { timeout: 240_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const failures: Finding[] = [];
  try {
    for (const viewport of VIEWPORTS) {
      const context = await browser.newContext({ viewport: { width: viewport.width, height: viewport.height } });
      try {
        for (const admin of [false, true]) {
          const page = await context.newPage();
          await login(page, demo.origin, admin);
          for (const route of ROUTES) {
            if (Boolean(route.admin) !== admin) continue;
            await page.goto(`${demo.origin}${route.path}`, { waitUntil: "networkidle" });
            if (route.path === "/admin/resources") {
              const detailButton = page.getByRole("button", { name: "查看资源", exact: true }).first();
              if (await detailButton.count()) {
                await detailButton.click();
                await page.waitForTimeout(400);
              }
            }
            await page.waitForTimeout(150);
            const report = await page.evaluate(MEASURE) as MeasurementReport;
            for (const finding of toFindings(report)) {
              failures.push({ where: `${viewport.name}(${viewport.width}) ${route.path} ${finding.where}`, detail: finding.detail });
            }
          }
          await page.close();
        }
      } finally {
        await context.close();
      }
    }
  } finally {
    await browser.close();
    await demo.close();
  }
  assert.deepEqual(failures, []);
});

test("Account and purchase labels keep their full text where the container has room", { timeout: 90_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1440, height: 950 } });
    await login(page, demo.origin, true);
    await page.goto(`${demo.origin}/admin/accounts`, { waitUntil: "networkidle" });
    await page.getByRole("heading", { level: 2, name: "客户与计费账户", exact: true }).waitFor({ state: "visible" });
    const labels = await page.evaluate(() => {
      const truncated = (el: Element) => (el as HTMLElement).scrollWidth > (el as HTMLElement).clientWidth + 1;
      const emails = Array.from(document.querySelectorAll(".operator-account-identity__text strong"))
        .filter((el) => el.getBoundingClientRect().width > 0);
      const badges = Array.from(document.querySelectorAll(".account-status-stack [class*='_Badge_']"))
        .filter((el) => el.getBoundingClientRect().width > 0);
      return {
        emails: emails.map((el) => ({ text: (el.textContent || "").trim(), truncated: truncated(el) })),
        badges: badges.map((el) => ({ text: (el.textContent || "").trim(), truncated: truncated(el) }))
      };
    });
    assert.ok(labels.emails.length > 0, "account identity emails should be rendered");
    for (const email of labels.emails) {
      assert.equal(email.truncated, false, `${email.text} should not be truncated at 1440`);
    }
    for (const badge of labels.badges) {
      assert.equal(badge.truncated, false, `${badge.text} badge should not be truncated at 1440`);
    }
  } finally {
    await browser.close();
    await demo.close();
  }
});
