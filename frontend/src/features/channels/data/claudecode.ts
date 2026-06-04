import { apiRequest } from '@/lib/api-client'
import { ProxyConfig } from '../hooks/use-oauth-flow'
import type { UpstreamCredential } from '@/features/credentials/data/credentials'

export async function claudecodeOAuthStart(headers?: Record<string, string>): Promise<{ session_id: string; auth_url: string }> {
  return apiRequest('/admin/claudecode/oauth/start', {
    method: 'POST',
    body: {},
    headers,
    requireAuth: true,
  })
}

export async function claudecodeOAuthExchange(
  input: {
    session_id: string
    callback_url: string
    proxy?: ProxyConfig
  },
  headers?: Record<string, string>
): Promise<{ credential: UpstreamCredential }> {
  return apiRequest('/admin/claudecode/oauth/exchange', {
    method: 'POST',
    body: input,
    headers,
    requireAuth: true,
  })
}
