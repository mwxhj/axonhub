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
  apiKey: '',
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

export function buildCredentialSecret(values: CredentialFormValues): CredentialSecretInput {
  return { apiKey: clean(values.apiKey) };
}

function buildQuotaInput(values: CredentialFormValues, options?: { forUpdate?: boolean }) {
  const limitAmount = clean(values.quotaLimitAmount);
  const usedAmount = clean(values.quotaUsedAmount);
  const quotaName = clean(values.quotaScopeName);
  const quotaRemark = clean(values.quotaRemark);
  const resetAt = toTime(values.quotaResetAt);
  const windowStartedAt = toTime(values.quotaWindowStartedAt);

  if (!limitAmount && !usedAmount && !quotaName && !quotaRemark && values.quotaResetPolicy === 'none' && !resetAt && !windowStartedAt) {
    return undefined;
  }

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
    warningThresholdPercent: Number(values.quotaWarningThresholdPercent) || undefined,
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

  return Boolean(clean(values.apiKey));
}
