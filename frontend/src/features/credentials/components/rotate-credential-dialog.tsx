'use client';

import { useEffect } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { useCredentialsContext } from '../context/credentials-context';
import { useRotateUpstreamCredentialSecret } from '../data/credentials';
import type { CredentialFormValues, OAuthCredentialProvider, UpstreamCredential } from '../data/schema';
import { CredentialSecretFields } from './credential-secret-fields';
import { buildRotateCredentialInput, defaultCredentialFormValues } from './form-utils';

function oauthProviderFromCredential(credential: Pick<UpstreamCredential, 'secretSummary'> | null): OAuthCredentialProvider {
  switch (credential?.secretSummary?.providerType) {
    case 'claudecode':
      return 'claudecode';
    case 'github_copilot':
      return 'github_copilot';
    case 'antigravity':
      return 'antigravity';
    case 'codex':
    default:
      return 'codex';
  }
}

export function RotateCredentialDialog() {
  const { t } = useTranslation();
  const { open, setOpen, currentCredential, setCurrentCredential } = useCredentialsContext();
  const rotateMutation = useRotateUpstreamCredentialSecret();
  const isOpen = open === 'rotate' && !!currentCredential;

  const {
    register,
    handleSubmit,
    reset,
    setValue,
    watch,
    formState: { errors },
  } = useForm<CredentialFormValues>({
    defaultValues: defaultCredentialFormValues,
  });

  useEffect(() => {
    if (isOpen && currentCredential) {
      reset({
        ...defaultCredentialFormValues,
        secretMode: currentCredential.secretSummary?.kind === 'oauth' ? 'oauth' : 'api_key',
        oauthProvider: oauthProviderFromCredential(currentCredential),
      });
    }
  }, [currentCredential, isOpen, reset]);

  const close = () => {
    setOpen(null);
    setCurrentCredential(null);
  };

  const onSubmit = async (values: CredentialFormValues) => {
    if (!currentCredential) {
      return;
    }

    await rotateMutation.mutateAsync({
      id: currentCredential.id,
      input: buildRotateCredentialInput(values),
    });
    close();
  };

  const handleImportedCredential = () => {
    close();
  };

  return (
    <Dialog open={isOpen} onOpenChange={(nextOpen) => (nextOpen ? setOpen('rotate') : close())}>
      <DialogContent className='sm:max-w-[680px]'>
        <DialogHeader>
          <DialogTitle>{t('credentials.dialogs.rotate.title')}</DialogTitle>
          <DialogDescription>{t('credentials.dialogs.rotate.description')}</DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit(onSubmit, () => {})} noValidate>
          <div className='grid max-h-[72vh] gap-4 overflow-y-auto py-4 pr-1'>
            {currentCredential && (
              <CredentialSecretFields
                register={register}
                setValue={setValue}
                watch={watch}
                errors={errors}
                allowModeSwitch={false}
                currentCredential={currentCredential}
                onImportedCredential={handleImportedCredential}
              />
            )}
          </div>
          <DialogFooter>
            <Button type='button' variant='outline' onClick={close}>
              {t('common.buttons.cancel')}
            </Button>
            <Button type='submit' disabled={rotateMutation.isPending}>
              {rotateMutation.isPending ? t('common.buttons.saving') : t('credentials.buttons.rotateSecret')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
