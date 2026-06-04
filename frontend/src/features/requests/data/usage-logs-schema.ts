import { z } from 'zod';
import { pageInfoSchema } from '@/gql/pagination';
import { channelSchema } from '@/features/channels/data';

// Usage Log Source
export const usageLogSourceSchema = z.enum(['api', 'playground', 'test']);
export type UsageLogSource = z.infer<typeof usageLogSourceSchema>;

export const costItemSchema = z.object({
  itemCode: z.string(),
  quantity: z.number(),
  subtotal: z.number(),
});

const usageLogCredentialSchema = z.object({
  id: z.string(),
  name: z.string().optional().nullable(),
});

// Usage Log schema based on backend entity structure
export const usageLogSchema = z.object({
  id: z.string(),
  createdAt: z.coerce.date(),
  updatedAt: z.coerce.date(),
  requestID: z.string(),
  channel: channelSchema.partial().nullable().optional(),
  credentialID: z.string().nullable().optional(),
  quotaScopeID: z.string().nullable().optional(),
  quotaScopeNameSnapshot: z.string().nullable().optional(),
  quotaScopeStatusSnapshot: z.string().nullable().optional(),
  credentialNameSnapshot: z.string().nullable().optional(),
  credentialQuotaStatusSnapshot: z.string().nullable().optional(),
  credential: usageLogCredentialSchema.nullable().optional(),
  modelID: z.string(),
  promptTokens: z.number(),
  completionTokens: z.number(),
  totalTokens: z.number(),
  promptAudioTokens: z.number().nullable().optional(),
  promptCachedTokens: z.number().nullable().optional(),
  promptWriteCachedTokens: z.number().nullable().optional(),
  completionAudioTokens: z.number().nullable().optional(),
  completionReasoningTokens: z.number().nullable().optional(),
  completionAcceptedPredictionTokens: z.number().nullable().optional(),
  completionRejectedPredictionTokens: z.number().nullable().optional(),
  source: usageLogSourceSchema,
  format: z.string(),
  totalCost: z.number().nullable().optional(),
  costItems: z.array(costItemSchema).nullable().optional(),
});
export type UsageLog = z.infer<typeof usageLogSchema>;

// Usage Log Connection schema for GraphQL pagination
export const usageLogEdgeSchema = z.object({
  node: usageLogSchema,
  cursor: z.string(),
});

export const usageLogConnectionSchema = z.object({
  edges: z.array(usageLogEdgeSchema),
  pageInfo: pageInfoSchema,
  totalCount: z.number(),
});
export type UsageLogConnection = z.infer<typeof usageLogConnectionSchema>;

// Usage Log Edge type
export type UsageLogEdge = z.infer<typeof usageLogEdgeSchema>;

// Usage Log List schema for table display
export const usageLogListSchema = z.array(usageLogSchema);
export type UsageLogList = z.infer<typeof usageLogListSchema>;
