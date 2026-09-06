import { test, expect, type Page } from "@playwright/test";

async function login(page: Page) {
  await page.goto("/");
  await page.getByLabel("用户名", { exact: true }).fill("admin");
  await page
    .getByLabel("密码", { exact: true })
    .fill("correct horse battery staple");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "概览", exact: true }),
  ).toBeVisible();
}

test("administrator manages services, hosts and successive settings saves", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("console", (msg) => {
    if (/Content Security Policy|Refused to/.test(msg.text()))
      errors.push(msg.text());
  });
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("dialog", (d) => d.accept());
  await page.goto("/");
  await expect(page.locator("html")).toHaveAttribute("lang", "zh-CN");
  await expect(page).toHaveTitle("Avahi 管理面板");
  await expect(page.getByText(/没有默认密码/)).toBeVisible();
  await page.getByLabel("用户名", { exact: true }).fill("admin");
  await page
    .getByLabel("密码", { exact: true })
    .fill("correct horse battery staple");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "概览", exact: true }),
  ).toBeVisible();
  await page.getByRole("menuitem", { name: "服务发布", exact: true }).click();
  await page.getByRole("button", { name: "+ 添加服务", exact: true }).click();
  await page.getByLabel("显示名称", { exact: true }).fill("Browser service");
  await page.getByLabel("端口", { exact: true }).fill("8088");
  await page.getByRole("button", { name: "+ TXT 记录", exact: true }).click();
  await page.getByLabel("TXT 键").fill("path");
  await page.getByLabel("TXT 值").fill("/browser");
  await page.getByRole("button", { name: "保存并应用", exact: true }).click();
  await expect(
    page.getByRole("cell", { name: "Browser service", exact: true }),
  ).toBeVisible();
  await page.getByRole("menuitem", { name: "静态主机", exact: true }).click();
  await page.getByRole("button", { name: "+ 添加主机", exact: true }).click();
  await page.getByLabel("主机名（完整域名）").fill("browser.local");
  await page.getByLabel("IP 地址", { exact: true }).fill("192.168.1.42");
  await page.getByRole("button", { name: "保存并应用", exact: true }).click();
  await expect(
    page.getByRole("cell", { name: "browser.local", exact: true }),
  ).toBeVisible();
  await page.getByRole("menuitem", { name: "配置设置", exact: true }).click();
  await page.getByLabel(/^主机名/).fill("browser-first");
  await expect(page.getByText(/可能暴露原本隔离网络/)).toBeVisible();
  await page.getByLabel(/^启用服务发布/).click();
  await page.getByRole("option", { name: "是", exact: true }).click();
  await page.getByRole("button", { name: "保存并应用", exact: true }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "确认继续" })
    .click();
  await expect(page.getByRole("status")).toContainText("更改已应用");
  await page.getByLabel(/^主机名/).fill("browser-second");
  await page.getByRole("button", { name: "保存并应用", exact: true }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "确认继续" })
    .click();
  await expect(page.getByRole("status")).toHaveText("更改已应用");
  await expect(page.locator(".ant-alert-error")).toHaveCount(0);
  await expect(page.locator("pre").last()).toContainText(
    "disable-publishing=no",
  );
  await page.getByRole("menuitem", { name: "网络接口", exact: true }).click();
  await expect(page.getByText(/本页禁止空选保存/)).toBeVisible();
  await page.getByRole("menuitem", { name: "日志", exact: true }).click();
  await page.route("**/api/v1/logs?*", (route) => route.fulfill({ json: [] }));
  await page.getByLabel("日志级别").click();
  await page.getByRole("option", { name: "警告", exact: true }).click();
  const logRequest = page.waitForRequest(
    (request) =>
      request.url().includes("/logs?") &&
      request.url().includes("priority=warning"),
  );
  await page.getByRole("button", { name: "搜索", exact: true }).click();
  await logRequest;
  await expect(page.getByText(/日志原文不会翻译/)).toBeVisible();
  await page.getByRole("menuitem", { name: "服务发现", exact: true }).click();
  await expect(
    page.getByText(
      "没有匹配的服务，发现结果会自动更新。请确认设备位于可通信的局域网，且目标正在广播服务。",
    ),
  ).toBeVisible();
  await page.getByRole("menuitem", { name: "维护", exact: true }).click();
  await expect(page.getByRole("heading", { name: "配置快照" })).toBeVisible();
  await expect(page.getByText("当前管理基线", { exact: true })).toBeVisible();
  await expect(page.getByText(/不包含整个/)).toBeVisible();
  await page.screenshot({
    path: "test-results/desktop-maintenance.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(
    page.getByRole("heading", { name: "维护", exact: true }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy();
  await page.screenshot({
    path: "test-results/mobile-maintenance.png",
    fullPage: true,
  });
  await page.getByRole("button", { name: "退出登录", exact: true }).click();
  await expect(page.getByRole("heading", { name: "欢迎登录" })).toBeVisible();
  expect(errors).toEqual([]);
});

test("unsaved changes, destructive cancellation, snapshots and mobile navigation", async ({
  page,
}) => {
  let stopped = 0;
  page.on("request", (request) => {
    if (request.url().endsWith("/avahi/stop") && request.method() === "POST")
      stopped++;
  });
  await login(page);
  await page.getByRole("menuitem", { name: "配置设置", exact: true }).click();
  await page.getByLabel("主机名", { exact: true }).fill("unsaved-draft");
  await page.getByRole("menuitem", { name: "维护", exact: true }).click();
  await expect(page.getByRole("dialog")).toContainText("存在未保存的修改");
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "取消", exact: true })
    .click();
  await expect(page.getByLabel("主机名", { exact: true })).toHaveValue(
    "unsaved-draft",
  );
  await page.getByRole("button", { name: "刷新", exact: true }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "取消", exact: true })
    .click();
  await expect(page.getByLabel("主机名", { exact: true })).toHaveValue(
    "unsaved-draft",
  );
  await page.screenshot({
    path: "test-results/desktop-settings.png",
    fullPage: true,
    animations: "disabled",
  });
  await page.getByRole("menuitem", { name: "维护", exact: true }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "确认继续" })
    .click();
  await page.getByRole("button", { name: "停止", exact: true }).click();
  await expect(page.getByRole("dialog")).toContainText("不会停止此管理面板");
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "取消", exact: true })
    .click();
  expect(stopped).toBe(0);
  await page.getByRole("button", { name: "查看", exact: true }).first().click();
  await expect(page.getByRole("dialog")).toContainText("快照详情");
  await expect(page.getByRole("dialog").locator("pre")).toContainText(
    "avahi-daemon.conf",
  );
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "关闭", exact: true })
    .click();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole("button", { name: "打开导航" }).click();
  await page.getByRole("menuitem", { name: "服务发布", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "服务发布", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "+ 添加服务", exact: true }).click();
  await page.getByLabel("显示名称", { exact: true }).fill("移动端草稿");
  await expect
    .poll(async () => {
      const box = await page.getByRole("dialog").boundingBox();
      return box ? Math.round(box.x + box.width) : 10000;
    })
    .toBeLessThanOrEqual(390);
  await page.screenshot({
    path: "test-results/mobile-service-editor.png",
    fullPage: true,
    animations: "disabled",
  });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBeTruthy();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "取消", exact: true })
    .click();
  await page
    .getByRole("dialog")
    .filter({ hasText: "放弃修改" })
    .getByRole("button", { name: "确认继续" })
    .click();
  await page.getByRole("button", { name: "退出登录" }).click();
});

test("audit pagination and filtering operate on loaded records", async ({
  page,
}) => {
  await page.route("**/api/v1/audit", (route) =>
    route.fulfill({
      json: Array.from({ length: 25 }, (_, i) => ({
        id: i + 1,
        createdAt: new Date(2026, 0, 1, 0, i).toISOString(),
        actor: `审计用户-${i + 1}`,
        action: "PUT",
        resource: "/api/v1/config",
        result: i % 2 ? "success" : "failed",
      })),
    }),
  );
  await login(page);
  await page.getByRole("menuitem", { name: "维护", exact: true }).click();
  const card = page.locator(".ant-card").filter({
    has: page.getByRole("heading", { name: "操作审计", exact: true }),
  });
  await expect(card.getByText("共 25 条")).toBeVisible();
  await expect(
    card.getByRole("cell", { name: "审计用户-25", exact: true }),
  ).toBeVisible();
  await card.getByTitle("下一页", { exact: true }).click();
  await expect(
    card.getByRole("cell", { name: "审计用户-15", exact: true }),
  ).toBeVisible();
  await page.getByLabel("审计结果").click();
  await page.getByRole("option", { name: "失败", exact: true }).click();
  await expect(card.getByText("共 13 条")).toBeVisible();
  await page.getByRole("button", { name: "退出登录" }).click();
});

test("missing helper shows Chinese recovery guidance and original diagnostic", async ({
  page,
}) => {
  const diagnostic =
    'Post "http://helper/rpc/ListSnapshots": dial unix /run/avahi-manager/helper.sock: connect: no such file or directory';
  // A missing helper cannot emit a successful configuration snapshot either.
  await page.route("**/api/v1/config/events", (route) =>
    route.fulfill({
      contentType: "text/event-stream",
      body: ": helper unavailable\n\n",
    }),
  );
  await page.route("**/api/v1/config", (route) =>
    route.fulfill({ status: 503, json: { message: diagnostic } }),
  );
  await page.goto("/");
  await page.getByLabel("用户名", { exact: true }).fill("admin");
  await page
    .getByLabel("密码", { exact: true })
    .fill("correct horse battery staple");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(
    page.getByRole("alert").filter({ hasText: "特权 Helper 套接字不存在" }),
  ).toBeVisible();
  await expect(
    page.getByRole("alert").filter({ hasText: diagnostic }),
  ).toBeVisible();
  await page.getByRole("menuitem", { name: "服务发布", exact: true }).click();
  await expect(page.getByText(/配置不可用。请确认 Avahi 已安装/)).toBeVisible();
  await page.getByRole("button", { name: "退出登录", exact: true }).click();
});
