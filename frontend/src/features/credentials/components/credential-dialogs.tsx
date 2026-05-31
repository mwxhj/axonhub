'use client';

import { CreateCredentialDialog } from './create-credential-dialog';
import { EditCredentialDialog } from './edit-credential-dialog';
import { RotateCredentialDialog } from './rotate-credential-dialog';
import { CredentialStatusDialog } from './credential-status-dialog';
import { CredentialChannelsDialog } from './credential-channels-dialog';

export function CredentialDialogs() {
  return (
    <>
      <CreateCredentialDialog />
      <EditCredentialDialog />
      <RotateCredentialDialog />
      <CredentialStatusDialog />
      <CredentialChannelsDialog />
    </>
  );
}
