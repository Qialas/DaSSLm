export const TOKEN_KEY = 'dasslm_token';
// 旧版本使用的登录凭证键名，读取时自动迁移，避免升级后被迫重新登录
export const LEGACY_TOKEN_KEY = 'qdl_token';
export const UNAUTHORIZED_EVENT = 'dasslm:unauthorized';

export function getToken() {
  const token = localStorage.getItem(TOKEN_KEY);
  if (token) return token;
  const legacy = localStorage.getItem(LEGACY_TOKEN_KEY);
  if (!legacy) return '';
  localStorage.setItem(TOKEN_KEY, legacy);
  localStorage.removeItem(LEGACY_TOKEN_KEY);
  return legacy;
}

export function setToken(token) {
  localStorage.removeItem(LEGACY_TOKEN_KEY);
  if (token) localStorage.setItem(TOKEN_KEY, token);
  else localStorage.removeItem(TOKEN_KEY);
}

export class ApiError extends Error {
  constructor(message, status, body) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = status;
    this.body = body;
  }
}

export function createApiClient({ baseURL = '/api', fetcher = (...args) => fetch(...args), token = getToken, onUnauthorized } = {}) {
  return async (path, options = {}) => {
    const { silentError: _silentError, ...fetchOptions } = options;
    const headers = new Headers(fetchOptions.headers);
    if (!headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
    const accessToken = token();
    if (accessToken) headers.set('Authorization', `Bearer ${accessToken}`);
    const response = await fetcher(`${baseURL.replace(/\/$/, '')}${path}`, { ...fetchOptions, headers });
    const body = await response.json().catch(() => ({}));
    if (!response.ok || body.code !== 0) {
      const error = new ApiError(body.message || `请求失败（${response.status}）`, response.status, body);
      if (response.status === 401 && path !== '/auth/login') onUnauthorized?.(error);
      throw error;
    }
    return body;
  };
}

export const request = createApiClient({
  baseURL: import.meta.env?.VITE_API_URL || import.meta.env?.UMI_APP_API_BASE || '/api',
  onUnauthorized: () => window.dispatchEvent(new Event(UNAUTHORIZED_EVENT))
});
