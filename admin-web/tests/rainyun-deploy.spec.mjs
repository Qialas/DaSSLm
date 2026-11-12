import { test, expect } from '@playwright/test';

// 雨云部署：账号表单、证书列表，以及自动部署目标“更新已有证书 / 新建证书”两种方式
const user = { id: 1, username: 'admin', nickname: '测试管理员', email: '', role: 'admin', status: 'enabled' };
const rainyunAccount = { id: 7, name: '雨云主账号', provider: 'rainyun', endpoint: '', accessKey: 'ry-key', status: 'enabled' };
const certificate = {
  id: 3, commonName: 'example.com', sans: '["example.com","www.example.com"]', sslAccountId: 0, status: 'issued',
  expiresAt: new Date(Date.now() + 60 * 86400000).toISOString()
};
const rainyunCerts = [
  { id: 205, domain: 'other.net', domains: ['other.net'], issuer: 'R11', expireAt: new Date(Date.now() + 30 * 86400000).toISOString(), uploadAt: new Date().toISOString(), bindDomains: [], expired: false },
  { id: 201, domain: 'example.com, www.example.com', domains: ['example.com', 'www.example.com'], issuer: 'R10', expireAt: new Date(Date.now() + 10 * 86400000).toISOString(), uploadAt: new Date().toISOString(), bindDomains: ['cdn.example.com'], expired: false }
];

async function mockBackend(page) {
  const calls = [];
  await page.route('**/api/**', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname.replace('/api', '');
    calls.push({ path, method: request.method(), query: Object.fromEntries(url.searchParams), body: request.postDataJSON() });
    const respond = (data) => route.fulfill({ status: 200, json: { code: 0, message: '', data } });
    if (path === '/auth/me') return respond(user);
    if (path === '/deploy-accounts/7/test') return respond({ message: '检测通过：雨云证书列表可访问，共 2 张证书' });
    if (path === '/deploy-accounts/7/certificates') {
      const keyword = (url.searchParams.get('keyword') || '').toLowerCase();
      const items = rainyunCerts.filter((item) => !keyword || item.domain.includes(keyword) || String(item.id) === keyword);
      return respond({ items, total: items.length, page: 1, pageSize: 20 });
    }
    if (request.method() === 'POST' && path === '/deploy-accounts') return respond({ id: 8, ...request.postDataJSON() });
    if (request.method() === 'POST' && path === '/tasks') return respond({ id: 1, ...request.postDataJSON() });
    const resources = { 'deploy-accounts': [rainyunAccount], certificates: [certificate], 'ssl-accounts': [], tasks: [] };
    const rows = resources[path.slice(1)] || [];
    return respond({ items: rows, total: rows.length, page: 1, pageSize: 20 });
  });
  await page.addInitScript(() => localStorage.setItem('dasslm_token', 'test-token'));
  return calls;
}

test('rainyun deploy account form, connection test and certificate list', async ({ page }) => {
  const calls = await mockBackend(page);
  await page.goto('/accounts/deploy');
  const row = page.locator('.el-table__row', { hasText: '雨云主账号' });
  await expect(row).toContainText('雨云');
  await expect(row).toContainText('默认');

  await row.getByText('检测').click();
  await expect(page.getByText('检测通过：雨云证书列表可访问，共 2 张证书')).toBeVisible();

  await row.getByText('证书', { exact: true }).click();
  const drawer = page.locator('.el-drawer', { hasText: '雨云主账号 证书列表' });
  await expect(drawer.locator('.el-table__row')).toHaveCount(2);
  await expect(drawer).toContainText('example.com, www.example.com');
  await expect(drawer).toContainText('cdn.example.com');
  await drawer.getByPlaceholder('按域名或证书ID搜索').fill('other');
  await drawer.getByPlaceholder('按域名或证书ID搜索').press('Enter');
  await expect(drawer.locator('.el-table__row')).toHaveCount(1);
  expect(calls.some((call) => call.path === '/deploy-accounts/7/certificates' && call.query.keyword === 'other')).toBe(true);
  await drawer.locator('.el-drawer__close-btn').click();

  await page.getByRole('button', { name: '新建' }).click();
  const form = page.locator('.el-drawer', { hasText: '新建部署账号' });
  await form.getByLabel('账号名称').fill('雨云备用');
  await form.getByRole('radio', { name: /雨云/ }).click();
  await expect(form.getByText('面板地址')).toHaveCount(0);
  await expect(form.getByText('sslcenter:write')).toBeVisible();
  await form.getByLabel('API 密钥').fill('new-ry-key');
  await form.getByRole('button', { name: '保存' }).click();
  await expect(page.getByText('保存成功')).toBeVisible();
  const created = calls.find((call) => call.method === 'POST' && call.path === '/deploy-accounts');
  expect(created.body).toMatchObject({ name: '雨云备用', provider: 'rainyun', endpoint: '', accessKey: 'new-ry-key' });
});

test('auto deploy task can update an existing rainyun certificate or create a new one', async ({ page }) => {
  const calls = await mockBackend(page);
  await page.goto('/tasks');
  await page.getByRole('button', { name: '新建自动部署' }).click();
  const drawer = page.locator('.el-drawer', { hasText: '新建自动部署任务' });
  await drawer.locator('.cert-picker__card', { hasText: 'example.com' }).click();

  // 方式一：更新雨云中已有的证书
  await drawer.getByRole('button', { name: '添加部署目标' }).click();
  let picker = page.locator('.el-drawer', { hasText: '添加部署目标' });
  await picker.locator('.deploy-picker__card', { hasText: '雨云主账号' }).click();
  await expect(picker.getByText('雨云中已有 1 张 example.com 的证书')).toBeVisible();
  await picker.getByText('更新已有证书', { exact: true }).click();
  await picker.locator('.el-select', { hasText: '选择雨云「我的证书」' }).click();
  const options = page.locator('.el-select-dropdown__item:visible');
  await expect(options.first()).toContainText('#201 example.com, www.example.com');
  await expect(options.first()).toContainText('域名匹配');
  // 先误选不匹配的证书会有提示，再改选匹配的证书
  await options.filter({ hasText: '#205' }).click();
  await expect(picker.getByText('所选证书的域名中没有 example.com')).toBeVisible();
  await picker.locator('.el-select', { hasText: '#205' }).click();
  await page.locator('.el-select-dropdown__item:visible', { hasText: '#201' }).click();
  await expect(picker.getByText('所选证书的域名中没有 example.com')).toHaveCount(0);
  await picker.getByRole('button', { name: '添加', exact: true }).click();
  await expect(drawer.locator('.target-row', { hasText: '更新证书 ID 201' })).toBeVisible();

  // 方式二：首次部署时新建证书
  await drawer.getByRole('button', { name: '添加部署目标' }).click();
  picker = page.locator('.el-drawer', { hasText: '添加部署目标' });
  await picker.locator('.deploy-picker__card', { hasText: '雨云主账号' }).click();
  await expect(picker.getByText('之后证书续期都会更新这张证书')).toBeVisible();
  await picker.getByRole('button', { name: '添加', exact: true }).click();
  await expect(drawer.locator('.target-row', { hasText: '新建证书' })).toBeVisible();

  // 编辑已选证书的目标时回显原来的证书
  await drawer.locator('.target-row', { hasText: '更新证书 ID 201' }).getByRole('button', { name: '编辑部署目标' }).click();
  picker = page.locator('.el-drawer', { hasText: '编辑部署目标' });
  await expect(picker.locator('.el-select', { hasText: '#201 example.com, www.example.com' })).toBeVisible();
  await picker.getByRole('button', { name: '返回' }).click();

  await drawer.getByRole('button', { name: '保存' }).click();
  await expect(page.getByText('已保存，系统将自动执行部署')).toBeVisible();
  const saved = calls.find((call) => call.method === 'POST' && call.path === '/tasks');
  expect(saved.body.certificateId).toBe(3);
  expect(saved.body.targets).toHaveLength(2);
  expect(saved.body.targets[0]).toMatchObject({ deployAccountId: 7, siteName: 'example.com', productType: '', resourceId: '201', resourceName: 'example.com, www.example.com' });
  expect(saved.body.targets[1]).toMatchObject({ deployAccountId: 7, siteName: 'example.com', productType: '', resourceId: '', resourceName: '' });
});
