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
import type { CredentialFormValues } from '../data/schema';
import { CredentialSecretFields } from './credential-secret-fields';
import { buildRotateCredentialInput, defaultCredentialFormValues } from './form-utils';

export function RotateCredentialDialog() {
  const { t } = useTranslation();
  const { open, setOpen, currentCredential, setCurrentCredential } = useCredentialsContext();
  const rotateMutation = useRotateUpstreamCredentialSecret();
  const isOpen = open === 'rotate' && !!currentCredential;

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<CredentialFormValues>({
    defaultValues: defaultCredentialFormValues,
  });

  useEffect(() => {
    if (isOpen && currentCredential) {
      reset(defaultCredentialFormValues);
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

  return (
    <Dialog open={isOpen} onOpenChange={(nextOpen) => (nextOpen ? setOpen('rotate') : close())}>
      <DialogContent className='sm:max-w-[680px]'>
        <DialogHeader>
          <DialogTitle>{t('credentials.dialogs.rotate.title')}</DialogTitle>
          <DialogDescription>{t('credentials.dialogs.rotate.description')}</DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit(onSubmit, () => {})} noValidate>
          <div className='grid max-h-[72vh] gap-4 overflow-y-auto py-4 pr-1'>
            {currentCredential && <CredentialSecretFields register={register} errors={errors} />}
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
