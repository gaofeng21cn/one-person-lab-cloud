import { chromium, type Browser, type LaunchOptions } from "playwright";

/*
 * 浏览器启动的唯一入口。
 *
 * 统一使用本机已安装的 Google Chrome（channel: "chrome"），不再依赖
 * Playwright 捆绑下载的 Chromium。捆绑副本会在每次启动时把自己注册进
 * macOS LaunchServices，导致 Finder「打开方式」菜单里堆积大量
 * “Google Chrome for Testing” 条目，并额外占用约 550 MB 磁盘。
 *
 * 需要切换浏览器时只改这里，不要在各个测试里直接调用 chromium.launch()。
 */
export function launchBrowser(options: LaunchOptions = {}): Promise<Browser> {
  return chromium.launch({ channel: "chrome", ...options });
}
