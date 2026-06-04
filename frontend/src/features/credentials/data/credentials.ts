import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { graphqlRequest } from '@/gql/graphql';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { useErrorHandler } from '@/hooks/use-error-handler';
import {
  attachCredentialToChannelInputSchema,
  attachableChannelsConnectionSchema,
  createUpstreamCredentialInputSchema,
  credentialQuotaScopesConnectionSchema,
  credentialRefSchema,
  migrateLegacyCredentialsPayloadSchema,
  rotateUpstreamCredentialSecretInputSchema,
  updateChannelCredentialRefInputSchema,
  updateUpstreamCredentialInputSchema,
  upstreamCredentialDetailSchema,
  upstreamCredentialSchema,
  upstreamCredentialsConnectionSchema,
  type AttachCredentialToChannelInput,
  type AttachableChannelsConnection,
  type CreateUpstreamCredentialInput,
  type CredentialQuotaScopesConnection,
  type CredentialRef,
  type CredentialStatus,
  type MigrateLegacyCredentialsPayload,
  type ProviderQuotaStatus,
  type RotateUpstreamCredentialSecretInput,
  type UpdateChannelCredentialRefInput,
  type UpdateUpstreamCredentialInput,
  type UpstreamCredential,
  type UpstreamCredentialDetail,
  type UpstreamCredentialsConnection,
} from './schema';

export type {
  AttachCredentialToChannelInput,
  AttachableChannelsConnection,
  CreateUpstreamCredentialInput,
  CredentialQuotaScopesConnection,
  CredentialRef,
  CredentialStatus,
  MigrateLegacyCredentialsPayload,
  ProviderQuotaStatus,
  RotateUpstreamCredentialSecretInput,
  UpdateChannelCredentialRefInput,
  UpdateUpstreamCredentialInput,
  UpstreamCredential,
  UpstreamCredentialDetail,
  UpstreamCredentialsConnection,
};

const CREDENTIAL_FIELDS = `
  id
  name
  quotaScopeID
  quotaScope {
    id
    name
    status
    unit
    limitAmount
    usedAmount
    warningThresholdPercent
    resetPolicy
    resetAt
    windowStartedAt
    overLimitAction
    pauseUntil
    source
    lastError
    remark
  }
  providerQuotaStatuses {
    id
    providerType
    status
    nextResetAt
    ready
    nextCheckAt
    updatedAt
  }
  secretSummary {
    kind
    providerType
    baseURL
    issuerScope
  }
  quotaStatus
  lastError
  status
  remark
  createdAt
  updatedAt
  channelRefs(first: 100) {
    totalCount
    edges {
      node {
        id
        channelID
        credentialID
        enabled
        channel {
          id
          name
          type
          baseURL
          status
        }
      }
    }
  }
`;

const REF_FIELDS = `
  id
  channelID
  credentialID
  enabled
  channel {
    id
    name
    type
    baseURL
    status
  }
`;

const UPSTREAM_CREDENTIALS_QUERY = `
  query UpstreamCredentials(
    $first: Int
    $after: Cursor
    $before: Cursor
    $last: Int
    $where: UpstreamCredentialWhereInput
    $orderBy: UpstreamCredentialOrder
  ) {
    upstreamCredentials(first: $first, after: $after, before: $before, last: $last, where: $where, orderBy: $orderBy) {
      edges {
        node {
          ${CREDENTIAL_FIELDS}
        }
      }
      pageInfo {
        hasNextPage
        hasPreviousPage
        startCursor
        endCursor
      }
      totalCount
    }
  }
`;

const ATTACHABLE_CHANNELS_QUERY = `
  query CredentialAttachableChannels(
    $first: Int
    $after: Cursor
    $where: ChannelWhereInput
    $orderBy: ChannelOrder
  ) {
    channels(first: $first, after: $after, where: $where, orderBy: $orderBy) {
      edges {
        node {
          id
          name
          type
          baseURL
          status
          credentialRefs(first: 100) {
            totalCount
            edges {
              node {
                id
                channelID
                credentialID
                enabled
                channel {
                  id
                  name
                  type
                  baseURL
                  status
                }
              }
            }
          }
        }
      }
      pageInfo {
        hasNextPage
        hasPreviousPage
        startCursor
        endCursor
      }
      totalCount
    }
  }
`;

const CREDENTIAL_QUOTA_SCOPES_QUERY = `
  query CredentialQuotaScopes(
    $first: Int
    $after: Cursor
    $where: CredentialQuotaScopeWhereInput
    $orderBy: CredentialQuotaScopeOrder
  ) {
    credentialQuotaScopes(first: $first, after: $after, where: $where, orderBy: $orderBy) {
      edges {
        node {
          id
          name
          status
          unit
          limitAmount
          usedAmount
          warningThresholdPercent
          resetPolicy
          resetAt
          windowStartedAt
          overLimitAction
          pauseUntil
          source
          lastError
          remark
        }
      }
      pageInfo {
        hasNextPage
        hasPreviousPage
        startCursor
        endCursor
      }
      totalCount
    }
  }
`;

const UPSTREAM_CREDENTIAL_DETAIL_QUERY = `
  query UpstreamCredentialDetail($id: ID!) {
    node(id: $id) {
      ... on UpstreamCredential {
        ${CREDENTIAL_FIELDS}
        executions(first: 20, orderBy: { field: CREATED_AT, direction: DESC }) {
          edges {
            node {
              id
              createdAt
              status
              modelID
              responseStatusCode
              errorMessage
              channel {
                id
                name
                type
                baseURL
                status
              }
            }
            cursor
          }
          pageInfo {
            hasNextPage
            hasPreviousPage
            startCursor
            endCursor
          }
          totalCount
        }
        usageLogs(first: 20, orderBy: { field: CREATED_AT, direction: DESC }) {
          edges {
            node {
              id
              createdAt
              requestID
              modelID
              promptTokens
              completionTokens
              totalTokens
              totalCost
              source
              format
              channel {
                id
                name
                type
                baseURL
                status
              }
            }
            cursor
          }
          pageInfo {
            hasNextPage
            hasPreviousPage
            startCursor
            endCursor
          }
          totalCount
        }
      }
    }
  }
`;

const CREATE_UPSTREAM_CREDENTIAL_MUTATION = `
  mutation CreateUpstreamCredential($input: CreateUpstreamCredentialInput!) {
    createUpstreamCredential(input: $input) {
      ${CREDENTIAL_FIELDS}
    }
  }
`;

const UPDATE_UPSTREAM_CREDENTIAL_MUTATION = `
  mutation UpdateUpstreamCredential($id: ID!, $input: UpdateUpstreamCredentialInput!) {
    updateUpstreamCredential(id: $id, input: $input) {
      ${CREDENTIAL_FIELDS}
    }
  }
`;

const ROTATE_UPSTREAM_CREDENTIAL_SECRET_MUTATION = `
  mutation RotateUpstreamCredentialSecret($id: ID!, $input: RotateUpstreamCredentialSecretInput!) {
    rotateUpstreamCredentialSecret(id: $id, input: $input) {
      ${CREDENTIAL_FIELDS}
    }
  }
`;

const ARCHIVE_UPSTREAM_CREDENTIAL_MUTATION = `
  mutation ArchiveUpstreamCredential($id: ID!) {
    archiveUpstreamCredential(id: $id) {
      ${CREDENTIAL_FIELDS}
    }
  }
`;

const DELETE_UPSTREAM_CREDENTIAL_MUTATION = `
  mutation DeleteUpstreamCredential($id: ID!) {
    deleteUpstreamCredential(id: $id)
  }
`;

const UPDATE_UPSTREAM_CREDENTIAL_STATUS_MUTATION = `
  mutation UpdateUpstreamCredentialStatus($id: ID!, $status: UpstreamCredentialStatus!) {
    updateUpstreamCredentialStatus(id: $id, status: $status) {
      ${CREDENTIAL_FIELDS}
    }
  }
`;

const ATTACH_CREDENTIAL_TO_CHANNEL_MUTATION = `
  mutation AttachCredentialToChannel($input: AttachCredentialToChannelInput!) {
    attachCredentialToChannel(input: $input) {
      ${REF_FIELDS}
    }
  }
`;

const UPDATE_CHANNEL_CREDENTIAL_REF_MUTATION = `
  mutation UpdateChannelCredentialRef($id: ID!, $input: UpdateChannelCredentialRefInput!) {
    updateChannelCredentialRef(id: $id, input: $input) {
      ${REF_FIELDS}
    }
  }
`;

const DETACH_CREDENTIAL_FROM_CHANNEL_MUTATION = `
  mutation DetachCredentialFromChannel($channelID: ID!, $credentialID: ID!) {
    detachCredentialFromChannel(channelID: $channelID, credentialID: $credentialID)
  }
`;

const MIGRATE_LEGACY_CHANNEL_CREDENTIALS_MUTATION = `
  mutation MigrateLegacyChannelCredentials {
    migrateLegacyChannelCredentials {
      migratedChannels
      createdCredentials
      createdRefs
      skippedChannels
    }
  }
`;

function invalidateCredentialQueries(queryClient: ReturnType<typeof useQueryClient>) {
  queryClient.invalidateQueries({ queryKey: ['upstreamCredentials'] });
  queryClient.invalidateQueries({ queryKey: ['credentialQuotaScopes'] });
  queryClient.invalidateQueries({ queryKey: ['credentialAttachableChannels'] });
  queryClient.invalidateQueries({ queryKey: ['upstreamCredentialDetail'] });
  queryClient.invalidateQueries({ queryKey: ['channels'] });
}

export function useUpstreamCredentials(variables?: Record<string, unknown>, options?: { enabled?: boolean }) {
  const { t } = useTranslation();
  const { handleError } = useErrorHandler();

  return useQuery({
    enabled: options?.enabled ?? true,
    queryKey: ['upstreamCredentials', variables],
    queryFn: async () => {
      try {
        const data = await graphqlRequest<{ upstreamCredentials: UpstreamCredentialsConnection }>(UPSTREAM_CREDENTIALS_QUERY, variables);
        return upstreamCredentialsConnectionSchema.parse(data.upstreamCredentials);
      } catch (error) {
        handleError(error, t('common.errors.internalServerError'));
        throw error;
      }
    },
  });
}

export function useAttachableChannels(variables?: Record<string, unknown>, options?: { enabled?: boolean }) {
  const { t } = useTranslation();
  const { handleError } = useErrorHandler();

  return useQuery({
    enabled: options?.enabled ?? true,
    queryKey: ['credentialAttachableChannels', variables],
    queryFn: async () => {
      try {
        const data = await graphqlRequest<{ channels: AttachableChannelsConnection }>(ATTACHABLE_CHANNELS_QUERY, variables);
        return attachableChannelsConnectionSchema.parse(data.channels);
      } catch (error) {
        handleError(error, t('common.errors.internalServerError'));
        throw error;
      }
    },
  });
}

export function useCredentialQuotaScopes(variables?: Record<string, unknown>, options?: { enabled?: boolean }) {
  const { t } = useTranslation();
  const { handleError } = useErrorHandler();

  return useQuery({
    enabled: options?.enabled ?? true,
    queryKey: ['credentialQuotaScopes', variables],
    queryFn: async () => {
      try {
        const data = await graphqlRequest<{ credentialQuotaScopes: CredentialQuotaScopesConnection }>(
          CREDENTIAL_QUOTA_SCOPES_QUERY,
          variables
        );
        return credentialQuotaScopesConnectionSchema.parse(data.credentialQuotaScopes);
      } catch (error) {
        handleError(error, t('common.errors.internalServerError'));
        throw error;
      }
    },
  });
}

export function useUpstreamCredentialDetail(id?: string, options?: { enabled?: boolean }) {
  const { t } = useTranslation();
  const { handleError } = useErrorHandler();

  return useQuery({
    enabled: Boolean(id) && (options?.enabled ?? true),
    queryKey: ['upstreamCredentialDetail', id],
    queryFn: async () => {
      try {
        const data = await graphqlRequest<{ node: unknown }>(UPSTREAM_CREDENTIAL_DETAIL_QUERY, { id });
        return upstreamCredentialDetailSchema.nullable().parse(data.node);
      } catch (error) {
        handleError(error, t('common.errors.internalServerError'));
        throw error;
      }
    },
  });
}

export function useCreateUpstreamCredential() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { handleError } = useErrorHandler();

  return useMutation({
    mutationFn: async (input: CreateUpstreamCredentialInput) => {
      try {
        const validated = createUpstreamCredentialInputSchema.parse(input);
        const data = await graphqlRequest<{ createUpstreamCredential: UpstreamCredential }>(CREATE_UPSTREAM_CREDENTIAL_MUTATION, {
          input: validated,
        });
        return upstreamCredentialSchema.parse(data.createUpstreamCredential);
      } catch (error) {
        handleError(error, { context: t('credentials.dialogs.create.title') });
        throw error;
      }
    },
    onSuccess: () => {
      invalidateCredentialQueries(queryClient);
      toast.success(t('credentials.messages.createSuccess'));
    },
  });
}

export function useUpdateUpstreamCredential() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { handleError } = useErrorHandler();

  return useMutation({
    mutationFn: async ({ id, input }: { id: string; input: UpdateUpstreamCredentialInput }) => {
      try {
        const validated = updateUpstreamCredentialInputSchema.parse(input);
        const data = await graphqlRequest<{ updateUpstreamCredential: UpstreamCredential }>(UPDATE_UPSTREAM_CREDENTIAL_MUTATION, {
          id,
          input: validated,
        });
        return upstreamCredentialSchema.parse(data.updateUpstreamCredential);
      } catch (error) {
        handleError(error, { context: t('credentials.dialogs.edit.title') });
        throw error;
      }
    },
    onSuccess: () => {
      invalidateCredentialQueries(queryClient);
      toast.success(t('credentials.messages.updateSuccess'));
    },
  });
}

export function useRotateUpstreamCredentialSecret() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { handleError } = useErrorHandler();

  return useMutation({
    mutationFn: async ({ id, input }: { id: string; input: RotateUpstreamCredentialSecretInput }) => {
      try {
        const validated = rotateUpstreamCredentialSecretInputSchema.parse(input);
        const data = await graphqlRequest<{ rotateUpstreamCredentialSecret: UpstreamCredential }>(
          ROTATE_UPSTREAM_CREDENTIAL_SECRET_MUTATION,
          { id, input: validated }
        );
        return upstreamCredentialSchema.parse(data.rotateUpstreamCredentialSecret);
      } catch (error) {
        handleError(error, { context: t('credentials.dialogs.rotate.title') });
        throw error;
      }
    },
    onSuccess: () => {
      invalidateCredentialQueries(queryClient);
      toast.success(t('credentials.messages.rotateSuccess'));
    },
  });
}

export function useUpdateUpstreamCredentialStatus() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { handleError } = useErrorHandler();

  return useMutation({
    mutationFn: async ({ id, status }: { id: string; status: CredentialStatus }) => {
      try {
        const data = await graphqlRequest<{ updateUpstreamCredentialStatus: UpstreamCredential }>(
          UPDATE_UPSTREAM_CREDENTIAL_STATUS_MUTATION,
          { id, status }
        );
        return upstreamCredentialSchema.parse(data.updateUpstreamCredentialStatus);
      } catch (error) {
        handleError(error, { context: t('credentials.dialogs.status.title') });
        throw error;
      }
    },
    onSuccess: (_data, variables) => {
      invalidateCredentialQueries(queryClient);
      toast.success(t('credentials.messages.statusUpdateSuccess', { status: t(`credentials.status.${variables.status}`) }));
    },
  });
}

export function useArchiveUpstreamCredential() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { handleError } = useErrorHandler();

  return useMutation({
    mutationFn: async (id: string) => {
      try {
        const data = await graphqlRequest<{ archiveUpstreamCredential: UpstreamCredential }>(ARCHIVE_UPSTREAM_CREDENTIAL_MUTATION, { id });
        return upstreamCredentialSchema.parse(data.archiveUpstreamCredential);
      } catch (error) {
        handleError(error, { context: t('credentials.dialogs.archive.title') });
        throw error;
      }
    },
    onSuccess: () => {
      invalidateCredentialQueries(queryClient);
      toast.success(t('credentials.messages.archiveSuccess'));
    },
  });
}

export function useDeleteUpstreamCredential() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { handleError } = useErrorHandler();

  return useMutation({
    mutationFn: async (id: string) => {
      try {
        const data = await graphqlRequest<{ deleteUpstreamCredential: boolean }>(DELETE_UPSTREAM_CREDENTIAL_MUTATION, { id });
        return data.deleteUpstreamCredential;
      } catch (error) {
        handleError(error, { context: t('credentials.dialogs.delete.title') });
        throw error;
      }
    },
    onSuccess: () => {
      invalidateCredentialQueries(queryClient);
      toast.success(t('credentials.messages.deleteSuccess'));
    },
  });
}

export function useAttachCredentialToChannel() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { handleError } = useErrorHandler();

  return useMutation({
    mutationFn: async (input: AttachCredentialToChannelInput) => {
      try {
        const validated = attachCredentialToChannelInputSchema.parse(input);
        const data = await graphqlRequest<{ attachCredentialToChannel: CredentialRef }>(ATTACH_CREDENTIAL_TO_CHANNEL_MUTATION, {
          input: validated,
        });
        return credentialRefSchema.parse(data.attachCredentialToChannel);
      } catch (error) {
        handleError(error, { context: t('credentials.dialogs.channels.attach') });
        throw error;
      }
    },
    onSuccess: () => {
      invalidateCredentialQueries(queryClient);
      toast.success(t('credentials.messages.attachSuccess'));
    },
  });
}

export function useUpdateChannelCredentialRef() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { handleError } = useErrorHandler();

  return useMutation({
    mutationFn: async ({ id, input }: { id: string; input: UpdateChannelCredentialRefInput }) => {
      try {
        const validated = updateChannelCredentialRefInputSchema.parse(input);
        const data = await graphqlRequest<{ updateChannelCredentialRef: CredentialRef }>(UPDATE_CHANNEL_CREDENTIAL_REF_MUTATION, {
          id,
          input: validated,
        });
        return credentialRefSchema.parse(data.updateChannelCredentialRef);
      } catch (error) {
        handleError(error, { context: t('credentials.dialogs.channels.title') });
        throw error;
      }
    },
    onSuccess: () => {
      invalidateCredentialQueries(queryClient);
      toast.success(t('credentials.messages.refUpdateSuccess'));
    },
  });
}

export function useDetachCredentialFromChannel() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { handleError } = useErrorHandler();

  return useMutation({
    mutationFn: async ({ channelID, credentialID }: { channelID: string; credentialID: string }) => {
      try {
        const data = await graphqlRequest<{ detachCredentialFromChannel: boolean }>(DETACH_CREDENTIAL_FROM_CHANNEL_MUTATION, {
          channelID,
          credentialID,
        });
        return data.detachCredentialFromChannel;
      } catch (error) {
        handleError(error, { context: t('credentials.dialogs.channels.detach') });
        throw error;
      }
    },
    onSuccess: () => {
      invalidateCredentialQueries(queryClient);
      toast.success(t('credentials.messages.detachSuccess'));
    },
  });
}

export function useMigrateLegacyChannelCredentials() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { handleError } = useErrorHandler();

  return useMutation({
    mutationFn: async () => {
      try {
        const data = await graphqlRequest<{ migrateLegacyChannelCredentials: MigrateLegacyCredentialsPayload }>(
          MIGRATE_LEGACY_CHANNEL_CREDENTIALS_MUTATION
        );
        return migrateLegacyCredentialsPayloadSchema.parse(data.migrateLegacyChannelCredentials);
      } catch (error) {
        handleError(error, { context: t('credentials.buttons.migrateLegacy') });
        throw error;
      }
    },
    onSuccess: (payload) => {
      invalidateCredentialQueries(queryClient);
      toast.success(
        t('credentials.messages.migrateSuccess', {
          credentials: payload.createdCredentials,
          refs: payload.createdRefs,
        })
      );
    },
  });
}
