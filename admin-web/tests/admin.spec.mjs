import { test, expect } from '@playwright/test';

const user = { id: 1, username: 'admin', nickname: '测试管理员', email: '', role: 'admin', status: 'enabled' };
const domain = { id: 1, name: 'example.com', domainAccountId: 1, dnsProvider: 'cloudflare', recordCount: 1, status: 'enabled' };
const domainAccount = { id: 1, name: '测试 DNS', provider: 'cloudflare', secretKey: '', status: 'enabled' };
const sslAccount = { id: 1, name: '测试 SSL', provider: 'letsencrypt', status: 'enabled' };

async function mockBackend(page) {
  const calls = [];
  let expired = false;
  const records = [{ id: 1, domainId: 1, rr: 'www', type: 'A', value: '192.0.2.1', ttl: 600, status: 'enabled', proxied: false }];
  await page.route('**/api/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname.replace('/api', '');
    calls.push({ path, method: request.method(), authorization: request.headers().authorization, body: request.postDataJSON() });
    const respond = (data, status = 200, message = '') => route.fulfill({ status, json: { code: status === 200 ? 0 : status, message, data } });
    if (path === '/auth/login') {
      if (request.postDataJSON()?.password !== 'test-password') return respond(null, 401, '用户名或密码错误');
      expired = false;
      return respond({ token: 'test-token', user });
    }
    if (expired) return respond(null, 401, '登录状态无效');
    if (path === '/auth/me') return respond(user);
    if (path === '/dashboard/overview') return respond({ domains: 1, certificates: 0, tasks: 0, expiringCerts7d: 0, server: { cpu: { total: 4 }, memory: {}, disk: {} } });
    if (path === '/domains/1') return respond(domain);
    if (path === '/domains/1/records') {
      if (request.method() === 'POST') {
        const record = { id: records.length + 1, domainId: 1, ...request.postDataJSON() };
        records.push(record);
        return respond(record);
      }
      return respond({ items: records, total: records.length });
    }
    if (path === '/notification-rules' || path === '/system-tasks') return respond([]);
    const resources = { domains: [domain], 'domain-accounts': [domainAccount], 'ssl-accounts': [sslAccount], users: [user] };
    const rows = resources[path.slice(1)] || [];
    return respond({ items: rows, total: rows.length, page: 1, pageSize: 20 });
  });
  return { calls, expire: () => { expired = true; } };
}

async function authenticate(page) {
  await page.addInitScript(() => localStorage.setItem('dasslm_token', 'test-token'));
}

const pageTitle = (title) => new RegExp(`^${title.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')} - `);

test('native login retains the target route and uses the real backend request fields', async ({ page }) => {
  const backend = await mockBackend(page);
  await page.goto('/certificates');
  await expect(page).toHaveURL(/\/auth\/login\?redirect=/);
  await page.getByRole('textbox', { name: '用户名', exact: true }).fill('admin');
  await page.getByLabel('密码', { exact: true }).fill('wrong');
  await page.getByRole('button', { name: '登录控制台' }).click();
  await expect(page.getByText('用户名或密码错误', { exact: true })).toBeVisible();
  await page.getByLabel('密码', { exact: true }).fill('test-password');
  await page.getByRole('button', { name: '登录控制台' }).click();
  await expect(page).toHaveURL(/\/certificates$/);
  await expect(page).toHaveTitle(pageTitle('SSL 证书'));
  await expect(page.locator('.art-table-card .el-table')).toBeVisible();
  const login = backend.calls.find((call) => call.path === '/auth/login');
  expect(login.body).toEqual({ username: 'admin', password: 'wrong' });
  expect(await page.evaluate(() => localStorage.getItem('dasslm_token'))).toBe('test-token');
});

test('every business route renders as a native Vue page inside the official layout', async ({ page }) => {
  const errors = [];
  page.on('pageerror', (error) => errors.push(error.message));
  const backend = await mockBackend(page);
  await authenticate(page);
  const routes = [
    ['/dashboard', '仪表盘', '运行工作台'], ['/domains', '域名管理', 'example.com'], ['/domains/1', 'DNS 记录', '192.0.2.1'],
    ['/certificates', 'SSL 证书', '新建'], ['/tasks', '自动任务', '新建自动部署'], ['/accounts/domain', '域名账号', '测试 DNS'],
    ['/accounts/ssl', 'SSL 账号', '测试 SSL'], ['/accounts/deploy', '部署账号', '新建'], ['/logs', '日志管理', '登录日志'],
    ['/settings/base', '基础设置', '站点设置'], ['/settings/profile', '后台用户', '测试管理员'], ['/settings/notice', '通知渠道', '新建渠道'],
    ['/manage', '管理', '域名账号']
  ];
  for (const [path, title, text] of routes) {
    await page.goto(path);
    await expect(page).toHaveTitle(pageTitle(title));
    await expect(page.locator('.layout-sidebar')).toBeVisible();
    await expect(page.locator('#app-content').getByText(text, { exact: true }).first()).toBeVisible();
    await expect(page.locator('[class^="ant-"], [class*=" ant-"]')).toHaveCount(0);
  }
  await page.goto('/domains/1');
  await page.reload();
  await expect(page.getByText('192.0.2.1', { exact: true })).toBeVisible();
  expect(backend.calls.filter((call) => call.path !== '/auth/login').every((call) => call.authorization === 'Bearer test-token')).toBe(true);
  expect(errors).toEqual([]);
});

test('DNS record form saves through the Go API and keeps its state across theme switches', async ({ page }) => {
  const backend = await mockBackend(page);
  await authenticate(page);
  await page.goto('/domains');
  await page.getByRole('button', { name: '记录', exact: true }).click();
  await expect(page).toHaveURL(/\/domains\/1$/);
  await page.getByRole('button', { name: '新建记录' }).click();
  const dialog = page.getByRole('dialog', { name: '新建DNS记录' });
  await dialog.getByRole('textbox', { name: '主机记录', exact: true }).fill('api');
  await dialog.getByRole('textbox', { name: '记录值', exact: true }).fill('192.0.2.2');
  await page.locator('[title="切换主题"]').dispatchEvent('click', { clientX: 600, clientY: 30 });
  await expect(page.locator('html')).toHaveClass(/dark/);
  await expect(dialog.getByRole('textbox', { name: '主机记录', exact: true })).toHaveValue('api');
  await dialog.getByRole('button', { name: '保存' }).click();
  await expect(page.getByText('192.0.2.2', { exact: true })).toBeVisible();
  await expect(dialog).not.toBeVisible();
  const create = backend.calls.find((call) => call.path === '/domains/1/records' && call.method === 'POST');
  expect(create.body).toMatchObject({ rr: 'api', type: 'A', value: '192.0.2.2' });
  await page.screenshot({ path: 'test-results/dns-art-design-pro.png', fullPage: true });
});

test('theme switches and protected 401 responses return to login', async ({ page }) => {
  const backend = await mockBackend(page);
  await authenticate(page);
  await page.goto('/domains');
  await expect(page.getByText('example.com', { exact: true })).toBeVisible();
  await page.locator('[title="切换主题"]').click();
  await expect(page.locator('html')).toHaveClass(/dark/);
  await page.evaluate(async () => { if (document.getAnimations) await Promise.all(document.getAnimations().filter(a => a.effect?.pseudoElement?.includes('view-transition')).map(a => a.finished.catch(() => {}))); });
  const tableBackground = await page.locator('.art-table-card').evaluate((el) => getComputedStyle(el).backgroundColor);
  expect(tableBackground).not.toBe('rgb(255, 255, 255)');
  await page.screenshot({ path: 'test-results/dark-art-design-pro.png', fullPage: true });
  backend.expire();
  await page.goto('/certificates');
  await expect(page).toHaveURL(/\/auth\/login/);
  await expect(page.getByRole('button', { name: '登录控制台' })).toBeVisible();
  expect(await page.evaluate(() => localStorage.getItem('dasslm_token'))).toBeNull();
});

test('mobile uses the framework responsive menu without overflowing the viewport', async ({ page }) => {
  await mockBackend(page);
  await authenticate(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/domains');
  await expect(page).toHaveTitle(pageTitle('域名管理'));
  await expect(page.getByText('example.com', { exact: true })).toBeVisible();
  const size = await page.evaluate(() => ({ width: window.innerWidth, scroll: document.documentElement.scrollWidth }));
  expect(size.scroll).toBeLessThanOrEqual(size.width);
  await page.screenshot({ path: 'test-results/mobile-art-design-pro.png', fullPage: true });
  await page.getByText('example.com', { exact: true }).click();
  await expect(page).toHaveURL(/\/domains\/1$/);
  await expect(page.getByText('192.0.2.1', { exact: true })).toBeVisible();
});

test('official menus, tabs and logout can be used again after signing back in', async ({ page }) => {
  await mockBackend(page);
  await authenticate(page);
  await page.goto('/dashboard');
  await page.getByRole('menuitem', { name: '域名管理', exact: true }).click();
  await expect(page.getByText('example.com', { exact: true })).toBeVisible();
  await page.getByRole('menuitem', { name: 'SSL 证书', exact: true }).click();
  await expect(page).toHaveTitle(pageTitle('SSL 证书'));
  await page.locator('#app-header li[id^="scroll-li-"]').filter({ hasText: '域名管理' }).click();
  await expect(page).toHaveURL(/\/domains$/);
  await page.getByRole('img', { name: '用户菜单' }).hover();
  await page.getByText('退出登录', { exact: true }).click();
  await page.getByRole('button', { name: /确.*定/ }).click();
  await expect(page).toHaveURL(/\/auth\/login/);
  expect(await page.evaluate(() => localStorage.getItem('dasslm_token'))).toBeNull();
  await page.getByRole('textbox', { name: '用户名', exact: true }).fill('admin');
  await page.getByLabel('密码', { exact: true }).fill('test-password');
  await page.getByRole('button', { name: '登录控制台' }).click();
  await expect(page.getByText('example.com', { exact: true })).toBeVisible();
  await page.getByRole('menuitem', { name: 'SSL 证书', exact: true }).click();
  await expect(page).toHaveTitle(pageTitle('SSL 证书'));
});
