import { request } from '@/services/client.mjs'

export interface DaSSLmUser {
  id: number
  username: string
  nickname?: string
  email?: string
  role: string
  status: string
}

export function toArtUser(user: DaSSLmUser): Api.Auth.UserInfo {
  return {
    userId: user.id,
    userName: user.nickname || user.username,
    email: user.email || '',
    roles: [user.role],
    buttons: [],
    avatar: '/favicon.svg'
  }
}

export async function fetchLogin(params: Api.Auth.LoginParams) {
  const response = await request('/auth/login', {
    method: 'POST',
    body: JSON.stringify({ username: params.userName, password: params.password })
  })
  return { token: response.data.token as string, user: toArtUser(response.data.user) }
}

export async function fetchGetUserInfo(): Promise<Api.Auth.UserInfo> {
  const response = await request('/auth/me')
  return toArtUser(response.data)
}
