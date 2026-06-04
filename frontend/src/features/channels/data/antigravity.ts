import { apiRequest } from '@/lib/api-client'
import { ProxyConfig } from '../hooks/use-oauth-flow'
import type { UpstreamCredential } from '@/features/credentials/data/credentials'

export async function antigravityOAuthStart(headers?: Record<string, string>, projectId?: string): Promise<{ session_id: string; auth_url: string }> {
  return apiRequest('/admin/antigravity/oauth/start', {
    method: 'POST',
    body: {
        project_id: projectId
    },
    headers,
    requireAuth: true,
  })
}

export async function antigravityOAuthExchange(
  input: {
    session_id: string
    callback_url: string
    proxy?: ProxyConfig
  },
  headers?: Record<string, string>
): Promise<{ credential: UpstreamCredential }> {
  return apiRequest('/admin/antigravity/oauth/exchange', {
    method: 'POST',
    body: input,
    headers,
    requireAuth: true,
  })
}
