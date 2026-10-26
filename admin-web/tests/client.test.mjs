import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ApiError, createApiClient, getToken, LEGACY_TOKEN_KEY, setToken, TOKEN_KEY } from '../src/services/client.mjs';

test('the shared client consumes the Go API envelope and send the existing bearer token', async () => {
  let observed;
  const api = createApiClient({
    baseURL: '/api/', token: () => 'existing-token',
    fetcher: async (url, options) => {
      observed = { url, options };
      return new Response(JSON.stringify({ code: 0, data: { id: 42 } }));
    }
  });
  const result = await api('/domains/42', { headers: { 'X-Test': 'bridge' }, silentError: true });
  assert.equal(result.data.id, 42);
  assert.equal(observed.url, '/api/domains/42');
  assert.equal(observed.options.headers.get('Authorization'), 'Bearer existing-token');
  assert.equal(observed.options.headers.get('X-Test'), 'bridge');
  assert.equal('silentError' in observed.options, false);
});

test('protected 401 responses invalidate the session, incorrect login credentials do not', async () => {
  let invalidations = 0;
  const api = createApiClient({
    token: () => '', onUnauthorized: () => invalidations++,
    fetcher: async () => new Response(JSON.stringify({ code: 401, message: '登录状态无效' }), { status: 401 })
  });
  await assert.rejects(api('/auth/login'), (error) => error instanceof ApiError && error.message === '登录状态无效');
  assert.equal(invalidations, 0);
  await assert.rejects(api('/domains'), (error) => error.status === 401);
  assert.equal(invalidations, 1);
});

test('business errors and invalid JSON are rejected instead of appearing as successful data', async () => {
  const business = createApiClient({ token: () => '', fetcher: async () => new Response(JSON.stringify({ code: 500, message: '保存失败' })) });
  await assert.rejects(business('/domains'), /保存失败/);
  const invalid = createApiClient({ token: () => '', fetcher: async () => new Response('<html>proxy error</html>', { status: 502 }) });
  await assert.rejects(invalid('/domains'), (error) => error.status === 502);
});

test('a token saved under the legacy qdl_token key is migrated to dasslm_token', () => {
  const store = new Map([[LEGACY_TOKEN_KEY, 'legacy-token']]);
  const previous = globalThis.localStorage;
  globalThis.localStorage = {
    getItem: (key) => (store.has(key) ? store.get(key) : null),
    setItem: (key, value) => store.set(key, String(value)),
    removeItem: (key) => store.delete(key)
  };
  try {
    assert.equal(getToken(), 'legacy-token');
    assert.equal(store.get(TOKEN_KEY), 'legacy-token');
    assert.equal(store.has(LEGACY_TOKEN_KEY), false);
    setToken('');
    assert.equal(getToken(), '');
  } finally {
    globalThis.localStorage = previous;
  }
});
