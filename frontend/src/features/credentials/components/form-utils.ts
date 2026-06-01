import type {
  CredentialFormValues,
  CredentialSecretInput,
  CredentialStatus,
  CreateUpstreamCredentialInput,
  RotateUpstreamCredentialSecretInput,
  UpdateUpstreamCredentialInput,
} from '../data/schema';

export const defaultCredentialFormValues: CredentialFormValues = {
  name: '',
  secretMode: 'api_key',
  apiKey: '',
  oauthProvider: 'codex',
  oauthCredentials: '',
  status: 'enabled',
  quotaScopeMode: 'none',
  quotaScopeID: '',
  quotaScopeName: '',
  quotaUnit: 'usd',
  quotaLimitAmount: '',
  quotaUsedAmount: '',
  quotaResetPolicy: 'none',
  quotaResetAt: '',
  quotaWindowStartedAt: '',
  quotaWarningThresholdPercent: 80,
  quotaOverLimitAction: 'warn',
  quotaRemark: '',
  remark: '',
};

function clean(value: string | undefined) {
  const trimmed = value?.trim() ?? '';
  return trimmed.length > 0 ? trimmed : undefined;
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

function parseOAuthCredentials(raw: string | undefined): CredentialSecretInput['oauth'] | undefined {
  const trimmed = clean(raw);
  if (!trimmed) {
    return undefined;
  }

  try {
    const parsed = JSON.parse(trimmed) as Record<string, unknown>;
    const scopes = Array.isArray(parsed.scopes)
      ? parsed.scopes.filter((scope): scope is string => typeof scope === 'string')
      : typeof parsed.scope === 'string'
        ? parsed.scope.split(/\s+/).filter(Boolean)
        : undefined;

    return {
      accessToken: typeof parsed.access_token === 'string' ? parsed.access_token : undefined,
      refreshToken: typeof parsed.refresh_token === 'string' ? parsed.refresh_token : undefined,
      clientID: typeof parsed.client_id === 'string' ? parsed.client_id : undefined,
      expiresAt: typeof parsed.expires_at === 'string' ? parsed.expires_at : undefined,
      tokenType: typeof parsed.token_type === 'string' ? parsed.token_type : undefined,
      scopes,
    };
  } catch {
    return {
      accessToken: trimmed,
    };
  }
}

function parseAntigravityOAuthCredentials(raw: string | undefined): CredentialSecretInput['oauth'] | undefined {
  const trimmed = clean(raw);
  if (!trimmed) {
    return undefined;
  }

  const [refreshToken] = trimmed.split('|');
  return {
    refreshToken: clean(refreshToken),
  };
}

function oauthProviderInputScope(provider: CredentialFormValues['oauthProvider']) {
  switch (provider) {
    case 'codex':
      return { providerType: 'codex', issuerScope: 'openai' };
    case 'claudecode':
      return { providerType: 'claudecode', issuerScope: 'anthropic_gcp' };
    case 'github_copilot':
      return { providerType: 'github_copilot', issuerScope: 'github_copilot' };
    case 'antigravity':
      return { providerType: 'antigravity', issuerScope: 'antigravity' };
    default:
      return {};
  }
}

export function buildCredentialSecret(values: CredentialFormValues): CredentialSecretInput {
  if (values.secretMode === 'oauth') {
    const rawCredentials = clean(values.oauthCredentials);
    if (values.oauthProvider === 'antigravity') {
      return {
        apiKey: rawCredentials,
        oauth: parseAntigravityOAuthCredentials(rawCredentials),
      };
    }

    return {
      apiKey: rawCredentials,
      oauth: parseOAuthCredentials(rawCredentials),
    };
  }

  return { apiKey: clean(values.apiKey) };
}

function buildQuotaInput(values: CredentialFormValues, options?: { forUpdate?: boolean }) {
  const limitAmount = clean(values.quotaLimitAmount);
  const usedAmount = clean(values.quotaUsedAmount);
  const quotaName = clean(values.quotaScopeName);
  const quotaRemark = clean(values.quotaRemark);
  const resetAt = toTime(values.quotaResetAt);
  const windowStartedAt = toTime(values.quotaWindowStartedAt);
  const warningThresholdPercent = Number(values.quotaWarningThresholdPercent);

  return {
    name: quotaName,
    unit: values.quotaUnit,
    limitAmount,
    usedAmount,
    resetPolicy: values.quotaResetPolicy,
    resetAt,
    windowStartedAt,
    clearResetAt:
      options?.forUpdate && !resetAt && (values.quotaResetPolicy === 'none' || values.quotaResetPolicy === 'manual') ? true : undefined,
    clearWindowStartedAt:
      options?.forUpdate && !windowStartedAt && (values.quotaResetPolicy === 'none' || values.quotaResetPolicy === 'manual')
        ? true
        : undefined,
    warningThresholdPercent: Number.isFinite(warningThresholdPercent) ? warningThresholdPercent : undefined,
    overLimitAction: values.quotaOverLimitAction,
    remark: quotaRemark,
  };
}

export function buildCreateCredentialInput(values: CredentialFormValues): CreateUpstreamCredentialInput {
  const input: CreateUpstreamCredentialInput = {
    name: clean(values.name),
    secret: buildCredentialSecret(values),
    status: values.status,
    remark: clean(values.remark),
  };

  if (values.secretMode === 'oauth') {
    Object.assign(input, oauthProviderInputScope(values.oauthProvider));
  }

  if (values.quotaScopeMode === 'shared') {
    input.quotaScopeID = clean(values.quotaScopeID);
  } else if (values.quotaScopeMode === 'new') {
    input.quota = buildQuotaInput(values);
  }

  return input;
}

export function buildUpdateCredentialInput(values: CredentialFormValues): UpdateUpstreamCredentialInput {
  const input: UpdateUpstreamCredentialInput = {
    name: clean(values.name),
    status: values.status,
    remark: clean(values.remark),
  };

  if (values.quotaScopeMode === 'shared') {
    input.quotaScopeID = clean(values.quotaScopeID);
  } else if (values.quotaScopeMode === 'new') {
    input.quota = buildQuotaInput(values, { forUpdate: true });
  } else {
    input.clearQuotaScope = true;
  }

  return input;
}

export function buildRotateCredentialInput(values: CredentialFormValues): RotateUpstreamCredentialSecretInput {
  return {
    secret: buildCredentialSecret(values),
  };
}

export function validateSecret(values: CredentialFormValues, status: CredentialStatus) {
  if (!status) {
    return false;
  }

  if (values.secretMode === 'oauth') {
    return Boolean(clean(values.oauthCredentials));
  }

  return Boolean(clean(values.apiKey));
}
