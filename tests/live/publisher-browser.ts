// Invoked by the livebuild Go harness with real disposable owner endpoints.
// V2 login and publisher authority are real; unrelated retained-shell reads use a demo projection.
import assert from "node:assert/strict";
import { createServer, request } from "node:http";
import { mkdir, readFile } from "node:fs/promises";
import { resolve, extname } from "node:path";
import { chromium } from "playwright";
import { startConsoleDemoServer } from "../../tools/start-console-demo.ts";

const bff = process.env.OPL_PUBLISHER_BFF_URL!;
const zip = process.env.OPL_PUBLISHER_TEST_ZIP!;
assert.match(bff, /^http:\/\/127\.0\.0\.1:\d+$/);
const demo = await startConsoleDemoServer({ port: 0, log: false });
const proxy = createServer(async (incoming, outgoing) => {
  if (!incoming.url?.startsWith("/api/")) {
    const pathname = new URL(incoming.url || "/", "http://local.test").pathname;
    const filename = resolve("dist", extname(pathname) ? "." + pathname : "index.html");
    if (!filename.startsWith(resolve("dist") + "/")) { outgoing.writeHead(404); outgoing.end(); return; }
    try {
      const data = await readFile(filename);
      const types: Record<string,string> = {".html":"text/html", ".js":"application/javascript", ".css":"text/css", ".svg":"image/svg+xml", ".woff2":"font/woff2"};
      outgoing.writeHead(200, {"content-type": types[extname(filename)] || "application/octet-stream"}); outgoing.end(data);
    } catch { outgoing.writeHead(404); outgoing.end(); }
    return;
  }
  const target = new URL(incoming.url || "/", incoming.url?.startsWith("/api/v2/") ? bff : demo.origin);
  const upstream = request(target, { method: incoming.method, headers: incoming.headers }, (response) => {
    outgoing.writeHead(response.statusCode || 502, response.headers); response.pipe(outgoing);
  });
  upstream.on("error", () => { outgoing.writeHead(502); outgoing.end(); });
  incoming.pipe(upstream);
});
await new Promise<void>((resolve) => proxy.listen(0, "127.0.0.1", resolve));
const address = proxy.address();
assert.ok(address && typeof address !== "string");
const origin = `http://127.0.0.1:${address.port}`;
const browser = await chromium.launch({ headless: true });
try {
  const context = await browser.newContext({ viewport: { width: 1280, height: 960 } });
  const page = await context.newPage();
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto(`${origin}/login`, { waitUntil: "networkidle" });
  await page.getByLabel("邮箱").fill("publisher@example.test");
  await page.getByLabel("密码").fill("isolated-password");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL(/\/console\/overview$/);
  await page.goto(`${origin}/console/workspaces`, { waitUntil: "networkidle" });
  await page.getByRole("link", { name: "发布 Package", exact: true }).click();
  await page.waitForURL(/\/console\/publisher$/);
  await page.getByLabel("Package", { exact: true }).selectOption({ label: "live-package" });
  await page.getByLabel("版本名称").fill("browser-v1");
  await page.getByLabel("ZIP 文件").setInputFiles(zip);
  await page.getByRole("button", { name: "上传并构建 / 继续上传", exact: true }).click();
  await page.getByRole("heading", { name: "版本已就绪", exact: true }).waitFor({ timeout: 90000 });
  const result = page.getByRole("region", { name: "构建结果" });
  const artifact = await result.locator("code").textContent();
  assert.match(artifact || "", /^sha256:[a-f0-9]{64}$/);
  const version = await page.getByRole("region", { name: "已就绪版本" }).locator("p").first().textContent();
  assert.ok(version?.startsWith("capv_"));
  const output = "output/cloud625-publisher-browser";
  await mkdir(output, { recursive: true });
  await page.screenshot({ path: `${output}/desktop.png`, fullPage: true });
  await page.reload({ waitUntil: "networkidle" });
  await page.getByRole("button", { name: "刷新构建状态", exact: true }).click();
  await page.getByRole("heading", { name: "版本已就绪", exact: true }).waitFor();
  assert.equal(await page.getByRole("region", { name: "已就绪版本" }).locator("p").first().textContent(), version);
  await page.setViewportSize({ width: 390, height: 844 });
  if (await page.locator(".sidebar.open").count()) await page.locator(".sidebar-close").click();
  await page.waitForFunction(() => (document.querySelector(".sidebar")?.getBoundingClientRect().right || 0) <= 1);
  await page.screenshot({ path: `${output}/mobile.png`, fullPage: true });
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth));
  assert.deepEqual(errors, []);
  console.log(`BROWSER_PUBLISHER_OK artifact=${artifact} version=${version}; reload readback preserved`);
} catch (error) {
  const pages = browser.contexts().flatMap((context) => context.pages());
  if (pages[0]) console.error((await pages[0].locator("body").innerText()).slice(-3000));
  throw error;
} finally {
  await browser.close(); proxy.closeAllConnections(); await new Promise<void>((resolve) => proxy.close(() => resolve())); await demo.close();
}
