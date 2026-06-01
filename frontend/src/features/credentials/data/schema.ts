import { z } from 'zod';
import { pageInfoSchema } from '@/gql/pagination';
import { channelStatusSchema, channelTypeSchema } from '@/features/channels/data/schema';

export const credentialStatusSchema = z.enum(['enabled', 'disabled', 'archived']);
export type CredentialStatus = z.infer<typeof credentialStatusSchema>;

export const credentialQuotaStatusSchema = z.enum(['available', 'warning', 'exhausted', 'paused', 'disabled', 'unknown']);
export type CredentialQuotaStatus = z.infer<typeof credentialQuotaStatusSchema>;

export const credentialQuotaUnitSchema = z.enum(['usd', 'token', 'request', 'credit', 'custom', 'unknown']);
export type CredentialQuotaUnit = z.infer<typeof credentialQuotaUnitSchema>;

export const credentialQuotaResetPolicySchema = z.enum(['none', 'manual', 'daily', 'monthly', 'custom']);
export type CredentialQuotaResetPolicy = z.infer<typeof credentialQuotaResetPolicySchema>;

export const credentialQuotaOverLimitActionSchema = z.enum(['warn', 'pause', 'disable']);
export type CredentialQuotaOverLimitAction = z.infer<typeof credentialQuotaOverLimitActionSchema>;

export const credentialQuotaSourceSchema = z.enum(['local_budget', 'provider_api', 'response_error', 'manual', 'inferred', 'unknown']);
export type CredentialQuotaSource = z.infer<typeof credentialQuotaSourceSchema>;

export const credentialQuotaScopeSchema = z.object({
  id: z.string(),
  name: z.string(),
  status: credentialQuotaStatusSchema.optional().nullable(),
  unit: credentialQuotaUnitSchema.optional().nullable(),
  limitAmount: z.string().optional().nullable(),
  usedAmount: z.string().optional().nullable(),
  warningThresholdPercent: z.number().int().optional().nullable(),
  resetPolicy: credentialQuotaResetPolicySchema.optional().nullable(),
  resetAt: z.string().optional().nullable(),
  windowStartedAt: z.string().optional().nullable(),
  overLimitAction: credentialQuotaOverLimitActionSchema.optional().nullable(),
  pauseUntil: z.string().optional().nullable(),
  source: credentialQuotaSourceSchema.optional().nullable(),
  lastError: z.string().optional().nullable(),
  remark: z.string().optional().nullable(),
});
export type CredentialQuotaScope = z.infer<typeof credentialQuotaScopeSchema>;

export const providerQuotaStatusSchema = z.object({
  id: z.string(),
  channelID: z.string().optional().nullable(),
  scopeKey: z.string(),
  credentialID: z.string().optional().nullable(),
  credentialFingerprint: z.string().optional().nullable(),
  secretFingerprint: z.string().optional().nullable(),
  resourceScopeKey: z.string().optional().nullable(),
  quotaScopeID: z.string().optional().nullable(),
  providerType: z.string(),
  status: z.enum(['available', 'warning', 'exhausted', 'unknown']),
  quotaData: z.record(z.string(), z.unknown()).optional().nullable(),
  nextResetAt: z.string().optional().nullable(),
  ready: z.boolean(),
  nextCheckAt: z.string(),
  updatedAt: z.string(),
});
export type ProviderQuotaStatus = z.infer<typeof providerQuotaStatusSchema>;

export const upstreamCredentialSecretSummarySchema = z.object({
  kind: z.enum(['api_key', 'oauth', 'azure', 'gcp', 'other']).or(z.string()),
  providerType: z.string().optional().nullable(),
  baseURL: z.string().optional().nullable(),
  issuerScope: z.string().optional().nullable(),
});
export type UpstreamCredentialSecretSummary = z.infer<typeof upstreamCredentialSecretSummarySchema>;

export const credentialQuotaScopeEdgeSchema = z.object({
  node: credentialQuotaScopeSchema.nullable(),
});

export const credentialQuotaScopesConnectionSchema = z.object({
  edges: z.array(credentialQuotaScopeEdgeSchema).nullable().optional(),
  pageInfo: pageInfoSchema.optional(),
  totalCount: z.number(),
});
export type CredentialQuotaScopesConnection = z.infer<typeof credentialQuotaScopesConnectionSchema>;

export const credentialChannelSummarySchema = z.object({
  id: z.string(),
  name: z.string(),
  type: channelTypeSchema,
  baseURL: z.string().optional().nullable(),
  status: channelStatusSchema,
});
export type CredentialChannelSummary = z.infer<typeof credentialChannelSummarySchema>;

export const credentialRefSchema = z.object({
  id: z.string(),
  channelID: z.string(),
  credentialID: z.string(),
  enabled: z.boolean(),
  channel: credentialChannelSummarySchema.optional().nullable(),
});
export type CredentialRef = z.infer<typeof credentialRefSchema>;

export const credentialRefEdgeSchema = z.object({
  node: credentialRefSchema.nullable(),
});

export const credentialRefConnectionSchema = z.object({
  edges: z.array(credentialRefEdgeSchema).nullable().optional(),
  totalCount: z.number(),
});

export const upstreamCredentialSchema = z.object({
  id: z.string(),
  name: z.string().optional().nullable(),
  keyHint: z.string().optional().nullable(),
  quotaScopeID: z.string().optional().nullable(),
  secretFingerprint: z.string().optional().nullable(),
  quotaScope: credentialQuotaScopeSchema.optional().nullable(),
  providerQuotaStatuses: z.array(providerQuotaStatusSchema).optional().nullable(),
  secretSummary: upstreamCredentialSecretSummarySchema.optional().nullable(),
  quotaStatus: z.string().optional().nullable(),
  lastError: z.string().optional().nullable(),
  fingerprint: z.string().optional().nullable(),
  status: credentialStatusSchema,
  remark: z.string().optional().nullable(),
  createdAt: z.string(),
  updatedAt: z.string(),
  channelRefs: credentialRefConnectionSchema.optional().nullable(),
});
export type UpstreamCredential = z.infer<typeof upstreamCredentialSchema>;

export const credentialExecutionSchema = z.object({
  id: z.string(),
  createdAt: z.coerce.date(),
  status: z.string(),
  modelID: z.string(),
  responseStatusCode: z.number().optional().nullable(),
  errorMessage: z.string().optional().nullable(),
  credentialNameSnapshot: z.string().optional().nullable(),
  credentialKeyHint: z.string().optional().nullable(),
  credentialSource: z.string().optional().nullable(),
  resourceScopeKey: z.string().optional().nullable(),
  quotaScopeNameSnapshot: z.string().optional().nullable(),
  quotaScopeStatusSnapshot: z.string().optional().nullable(),
  channel: credentialChannelSummarySchema.partial().optional().nullable(),
});
export type CredentialExecution = z.infer<typeof credentialExecutionSchema>;

export const credentialExecutionConnectionSchema = z.object({
  edges: z.array(
    z.object({
      node: credentialExecutionSchema.nullable(),
      cursor: z.string(),
    })
  ),
  pageInfo: pageInfoSchema,
  totalCount: z.number(),
});

export const credentialUsageLogSchema = z.object({
  id: z.string(),
  createdAt: z.coerce.date(),
  requestID: z.string(),
  modelID: z.string(),
  promptTokens: z.number(),
  completionTokens: z.number(),
  totalTokens: z.number(),
  totalCost: z.number().optional().nullable(),
  source: z.string(),
  format: z.string(),
  credentialNameSnapshot: z.string().optional().nullable(),
  credentialKeyHint: z.string().optional().nullable(),
  credentialSource: z.string().optional().nullable(),
  resourceScopeKey: z.string().optional().nullable(),
  quotaScopeNameSnapshot: z.string().optional().nullable(),
  quotaScopeStatusSnapshot: z.string().optional().nullable(),
  channel: credentialChannelSummarySchema.partial().optional().nullable(),
});
export type CredentialUsageLog = z.infer<typeof credentialUsageLogSchema>;

export const credentialUsageLogConnectionSchema = z.object({
  edges: z.array(
    z.object({
      node: credentialUsageLogSchema.nullable(),
      cursor: z.string(),
    })
  ),
  pageInfo: pageInfoSchema,
  totalCount: z.number(),
});

export const upstreamCredentialDetailSchema = upstreamCredentialSchema.extend({
  executions: credentialExecutionConnectionSchema.optional().nullable(),
  usageLogs: credentialUsageLogConnectionSchema.optional().nullable(),
});
export type UpstreamCredentialDetail = z.infer<typeof upstreamCredentialDetailSchema>;

export const upstreamCredentialEdgeSchema = z.object({
  node: upstreamCredentialSchema.nullable(),
});

export const upstreamCredentialsConnectionSchema = z.object({
  edges: z.array(upstreamCredentialEdgeSchema).nullable().optional(),
  pageInfo: pageInfoSchema,
  totalCount: z.number(),
});
export type UpstreamCredentialsConnection = z.infer<typeof upstreamCredentialsConnectionSchema>;

export const attachableChannelSchema = credentialChannelSummarySchema.extend({
  credentialRefs: credentialRefConnectionSchema.optional().nullable(),
});
export type AttachableChannel = z.infer<typeof attachableChannelSchema>;

export const attachableChannelEdgeSchema = z.object({
  node: attachableChannelSchema.nullable(),
});

export const attachableChannelsConnectionSchema = z.object({
  edges: z.array(attachableChannelEdgeSchema).nullable().optional(),
  pageInfo: pageInfoSchema,
  totalCount: z.number(),
});
export type AttachableChannelsConnection = z.infer<typeof attachableChannelsConnectionSchema>;

export const credentialSecretInputSchema = z.object({
  apiKey: z.string().optional(),
  oauth: z
    .object({
      accessToken: z.string().optional(),
      refreshToken: z.string().optional(),
      clientID: z.string().optional(),
      expiresAt: z.string().optional(),
      tokenType: z.string().optional(),
      scopes: z.array(z.string()).optional(),
    })
    .optional(),
  azure: z
    .object({
      apiVersion: z.string(),
    })
    .optional(),
  gcp: z
    .object({
      region: z.string(),
      projectID: z.string(),
      jsonData: z.string(),
    })
    .optional(),
});
export type CredentialSecretInput = z.infer<typeof credentialSecretInputSchema>;

export const createUpstreamCredentialInputSchema = z.object({
  name: z.string().optional(),
  providerType: z.string().optional(),
  baseURL: z.string().optional(),
  issuerScope: z.string().optional(),
  secret: credentialSecretInputSchema,
  status: credentialStatusSchema.optional(),
  quotaScopeID: z.string().optional(),
  quota: z
    .object({
      name: z.string().optional(),
      unit: credentialQuotaUnitSchema.optional(),
      limitAmount: z.string().optional(),
      usedAmount: z.string().optional(),
      resetPolicy: credentialQuotaResetPolicySchema.optional(),
      resetAt: z.string().optional(),
      windowStartedAt: z.string().optional(),
      warningThresholdPercent: z.number().int().min(0).max(100).optional(),
      overLimitAction: credentialQuotaOverLimitActionSchema.optional(),
      remark: z.string().optional(),
    })
    .optional(),
  remark: z.string().optional(),
});
export type CreateUpstreamCredentialInput = z.infer<typeof createUpstreamCredentialInputSchema>;

export const updateUpstreamCredentialInputSchema = z.object({
  name: z.string().optional(),
  status: credentialStatusSchema.optional(),
  quotaScopeID: z.string().optional(),
  clearQuotaScope: z.boolean().optional(),
  quota: createUpstreamCredentialInputSchema.shape.quota
    .unwrap()
    .extend({
      clearResetAt: z.boolean().optional(),
      clearWindowStartedAt: z.boolean().optional(),
    })
    .optional(),
  remark: z.string().optional(),
});
export type UpdateUpstreamCredentialInput = z.infer<typeof updateUpstreamCredentialInputSchema>;

export const rotateUpstreamCredentialSecretInputSchema = z.object({
  secret: credentialSecretInputSchema,
});
export type RotateUpstreamCredentialSecretInput = z.infer<typeof rotateUpstreamCredentialSecretInputSchema>;

export const credentialSecretModeSchema = z.enum(['api_key', 'oauth']);
export type CredentialSecretMode = z.infer<typeof credentialSecretModeSchema>;

export const oauthCredentialProviderSchema = z.enum(['codex', 'claudecode', 'github_copilot', 'antigravity']);
export type OAuthCredentialProvider = z.infer<typeof oauthCredentialProviderSchema>;

export const attachCredentialToChannelInputSchema = z.object({
  channelID: z.string().min(1),
  credentialID: z.string().min(1),
  enabled: z.boolean().optional(),
});
export type AttachCredentialToChannelInput = z.infer<typeof attachCredentialToChannelInputSchema>;

export const updateChannelCredentialRefInputSchema = z.object({
  enabled: z.boolean().optional(),
});
export type UpdateChannelCredentialRefInput = z.infer<typeof updateChannelCredentialRefInputSchema>;

export const migrateLegacyCredentialsPayloadSchema = z.object({
  migratedChannels: z.number().int(),
  createdCredentials: z.number().int(),
  createdRefs: z.number().int(),
  skippedChannels: z.number().int(),
});
export type MigrateLegacyCredentialsPayload = z.infer<typeof migrateLegacyCredentialsPayloadSchema>;

export type CredentialFormValues = {
  name: string;
  secretMode: CredentialSecretMode;
  apiKey: string;
  oauthProvider: OAuthCredentialProvider;
  oauthCredentials: string;
  status: CredentialStatus;
  quotaScopeMode: 'none' | 'new' | 'shared';
  quotaScopeID: string;
  quotaScopeName: string;
  quotaUnit: CredentialQuotaUnit;
  quotaLimitAmount: string;
  quotaUsedAmount: string;
  quotaResetPolicy: CredentialQuotaResetPolicy;
  quotaResetAt: string;
  quotaWindowStartedAt: string;
  quotaWarningThresholdPercent: number;
  quotaOverLimitAction: CredentialQuotaOverLimitAction;
  quotaRemark: string;
  remark: string;
};
