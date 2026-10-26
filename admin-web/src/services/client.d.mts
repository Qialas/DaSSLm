export const TOKEN_KEY: string
export const UNAUTHORIZED_EVENT: string
export function getToken(): string
export function setToken(token: string): void
export class ApiError extends Error {
  status: number
  code: number
  body: unknown
  constructor(message: string, status: number, body?: unknown)
}
export interface ApiResponse<T = any> { code: number; message: string; data: T }
export function createApiClient(options?: {
  baseURL?: string
  fetcher?: typeof fetch
  token?: () => string
  onUnauthorized?: (error: ApiError) => void
}): (path: string, options?: RequestInit & { silentError?: boolean }) => Promise<ApiResponse>
export const request: ReturnType<typeof createApiClient>
