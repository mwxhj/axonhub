'use client';

import { CreateCredentialDialog } from './create-credential-dialog';
import { CredentialArchiveDialog } from './credential-archive-dialog';
import { CredentialChannelsDialog } from './credential-channels-dialog';
import { CredentialDetailDialog } from './credential-detail-dialog';
import { CredentialStatusDialog } from './credential-status-dialog';
import { EditCredentialDialog } from './edit-credential-dialog';
import { RotateCredentialDialog } from './rotate-credential-dialog';

export function CredentialDialogs() {
  return (
    <>
      <CreateCredentialDialog />
      <CredentialDetailDialog />
      <EditCredentialDialog />
      <RotateCredentialDialog />
      <CredentialStatusDialog />
      <CredentialArchiveDialog />
      <CredentialChannelsDialog />
    </>
  );
}
