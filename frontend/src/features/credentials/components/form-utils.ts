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
  remark: '',
};

function clean(value: string | undefined) {
  const trimmed = value?.trim() ?? '';
  return trimmed.length > 0 ? trimmed : undefined;
}

export function buildCredentialSecret(values: CredentialFormValues): CredentialSecretInput {
  return { apiKey: clean(values.apiKey) };
}

export function buildCreateCredentialInput(values: CredentialFormValues): CreateUpstreamCredentialInput {
  return {
    name: clean(values.name),
    secret: buildCredentialSecret(values),
    status: values.status,
    remark: clean(values.remark),
  };
}

export function buildUpdateCredentialInput(values: CredentialFormValues): UpdateUpstreamCredentialInput {
  return {
    name: clean(values.name),
    status: values.status,
    remark: clean(values.remark),
  };
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
