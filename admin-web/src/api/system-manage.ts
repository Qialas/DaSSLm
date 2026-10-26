import { asyncRoutes } from '@/router/routes/asyncRoutes'

// DaSSLm uses its local business menu; it has no template mock/menu endpoint.
export async function fetchGetMenuList() {
  return asyncRoutes
}
