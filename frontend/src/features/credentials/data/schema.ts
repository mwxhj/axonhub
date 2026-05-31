import { z } from 'zod';
import { pageInfoSchema } from '@/gql/pagination';
import { channelStatusSchema, channelTypeSchema } from '@/features/channels/data/schema';

export const credentialAuthKindSchema = z.enum(['api_key', 'oauth', 'azure', 'gcp', 'other']);
export type CredentialAuthKind = z.infer<typeof credentialAuthKindSchema>;
export const credentialSecretKindSchema = credentialAuthKindSchema;
export type CredentialSecretKind = z.infer<typeof credentialSecretKindSchema>;

export const credentialStatusSchema = z.enum(['enabled', 'disabled', 'archived']);
export type CredentialStatus = z.infer<typeof credentialStatusSchema>;

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
  weightOverride: z.number().int().nullable().optional(),
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
  providerType: z.string().optional().nullable(),
  baseURL: z.string().optional().nullable(),
  authKind: credentialAuthKindSchema.optional().nullable(),
  secretKind: credentialSecretKindSchema,
  issuerScope: z.string().optional().nullable(),
  keyHint: z.string().optional().nullable(),
  quotaScopeID: z.number().int().optional().nullable(),
  quotaStatus: z.string().optional().nullable(),
  lastError: z.string().optional().nullable(),
  fingerprint: z.string(),
  status: credentialStatusSchema,
  weight: z.number().int(),
  remark: z.string().optional().nullable(),
  createdAt: z.string(),
  updatedAt: z.string(),
  channelRefs: credentialRefConnectionSchema.optional().nullable(),
});
export type UpstreamCredential = z.infer<typeof upstreamCredentialSchema>;

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
  authKind: credentialAuthKindSchema.optional(),
  secretKind: credentialSecretKindSchema.optional(),
  issuerScope: z.string().optional(),
  secret: credentialSecretInputSchema,
  status: credentialStatusSchema.optional(),
  weight: z.number().int().positive().optional(),
  remark: z.string().optional(),
});
export type CreateUpstreamCredentialInput = z.infer<typeof createUpstreamCredentialInputSchema>;

export const updateUpstreamCredentialInputSchema = z.object({
  name: z.string().optional(),
  status: credentialStatusSchema.optional(),
  weight: z.number().int().positive().optional(),
  remark: z.string().optional(),
});
export type UpdateUpstreamCredentialInput = z.infer<typeof updateUpstreamCredentialInputSchema>;

export const rotateUpstreamCredentialSecretInputSchema = z.object({
  secret: credentialSecretInputSchema,
});
export type RotateUpstreamCredentialSecretInput = z.infer<typeof rotateUpstreamCredentialSecretInputSchema>;

export const attachCredentialToChannelInputSchema = z.object({
  channelID: z.string().min(1),
  credentialID: z.string().min(1),
  enabled: z.boolean().optional(),
  weightOverride: z.number().int().positive().optional(),
});
export type AttachCredentialToChannelInput = z.infer<typeof attachCredentialToChannelInputSchema>;

export const updateChannelCredentialRefInputSchema = z.object({
  enabled: z.boolean().optional(),
  weightOverride: z.number().int().positive().optional(),
  clearWeightOverride: z.boolean().optional(),
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
  providerType: string;
  baseURL: string;
  authKind: CredentialAuthKind;
  issuerScope: string;
  apiKey: string;
  oauthAccessToken: string;
  oauthRefreshToken: string;
  oauthClientID: string;
  oauthExpiresAt: string;
  oauthTokenType: string;
  oauthScopes: string;
  azureAPIVersion: string;
  gcpRegion: string;
  gcpProjectID: string;
  gcpJSONData: string;
  status: CredentialStatus;
  weight: number;
  remark: string;
};
