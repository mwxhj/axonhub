import type {
  CredentialAuthKind,
  CredentialFormValues,
  CredentialSecretInput,
  CredentialStatus,
  CreateUpstreamCredentialInput,
  RotateUpstreamCredentialSecretInput,
  UpdateUpstreamCredentialInput,
} from '../data/schema';

export const defaultCredentialFormValues: CredentialFormValues = {
  name: '',
  providerType: 'openai',
  baseURL: '',
  authKind: 'api_key',
  apiKey: '',
  oauthAccessToken: '',
  oauthRefreshToken: '',
  oauthClientID: '',
  oauthExpiresAt: '',
  oauthTokenType: '',
  oauthScopes: '',
  azureAPIVersion: '',
  gcpRegion: '',
  gcpProjectID: '',
  gcpJSONData: '',
  status: 'enabled',
  weight: 100,
  remark: '',
};

function clean(value: string | undefined) {
  const trimmed = value?.trim() ?? '';
  return trimmed.length > 0 ? trimmed : undefined;
}

function parseScopes(value: string) {
  return value
    .split(',')
    .map((scope) => scope.trim())
    .filter(Boolean);
}

function toTime(value: string | undefined) {
  const trimmed = clean(value);
  if (!trimmed) {
    return undefined;
  }

  const date = new Date(trimmed);
  if (Number.isNaN(date.getTime())) {
    return trimmed;
  }

  return date.toISOString();
}

export function buildCredentialSecret(values: CredentialFormValues): CredentialSecretInput {
  switch (values.authKind) {
    case 'api_key':
      return { apiKey: clean(values.apiKey) };
    case 'oauth':
      return {
        oauth: {
          accessToken: clean(values.oauthAccessToken),
          refreshToken: clean(values.oauthRefreshToken),
          clientID: clean(values.oauthClientID),
          expiresAt: toTime(values.oauthExpiresAt),
          tokenType: clean(values.oauthTokenType),
          scopes: parseScopes(values.oauthScopes),
        },
      };
    case 'azure':
      return {
        azure: {
          apiVersion: clean(values.azureAPIVersion) ?? '',
        },
      };
    case 'gcp':
      return {
        gcp: {
          region: clean(values.gcpRegion) ?? '',
          projectID: clean(values.gcpProjectID) ?? '',
          jsonData: values.gcpJSONData,
        },
      };
    default:
      return { apiKey: clean(values.apiKey) };
  }
}

export function buildCreateCredentialInput(values: CredentialFormValues): CreateUpstreamCredentialInput {
  return {
    name: clean(values.name),
    providerType: clean(values.providerType) ?? 'openai',
    baseURL: clean(values.baseURL),
    authKind: values.authKind,
    secret: buildCredentialSecret(values),
    status: values.status,
    weight: Number(values.weight) || 100,
    remark: clean(values.remark),
  };
}

export function buildUpdateCredentialInput(values: Pick<CredentialFormValues, 'name' | 'status' | 'weight' | 'remark'>): UpdateUpstreamCredentialInput {
  return {
    name: clean(values.name),
    status: values.status,
    weight: Number(values.weight) || 100,
    remark: clean(values.remark),
  };
}

export function buildRotateCredentialInput(authKind: CredentialAuthKind, values: CredentialFormValues): RotateUpstreamCredentialSecretInput {
  return {
    secret: buildCredentialSecret({
      ...values,
      authKind,
    }),
  };
}

export function validateSecret(values: CredentialFormValues, status: CredentialStatus) {
  if (!status) {
    return false;
  }

  switch (values.authKind) {
    case 'api_key':
      return Boolean(clean(values.apiKey));
    case 'oauth':
      return Boolean(clean(values.oauthAccessToken));
    case 'azure':
      return Boolean(clean(values.azureAPIVersion));
    case 'gcp':
      return Boolean(clean(values.gcpRegion) && clean(values.gcpProjectID) && clean(values.gcpJSONData));
    default:
      return Boolean(clean(values.apiKey));
  }
}
