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
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Textarea } from '@/components/ui/textarea';
import { useCredentialsContext } from '../context/credentials-context';
import { useUpdateUpstreamCredential } from '../data/credentials';
import type { CredentialFormValues, CredentialStatus } from '../data/schema';
import { buildUpdateCredentialInput, defaultCredentialFormValues } from './form-utils';

const statuses: CredentialStatus[] = ['enabled', 'disabled', 'archived'];

export function EditCredentialDialog() {
  const { t } = useTranslation();
  const { open, setOpen, currentCredential, setCurrentCredential } = useCredentialsContext();
  const updateMutation = useUpdateUpstreamCredential();
  const isOpen = open === 'edit' && !!currentCredential;

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

  const status = watch('status');

  useEffect(() => {
    if (isOpen && currentCredential) {
      reset({
        ...defaultCredentialFormValues,
        name: currentCredential.name ?? '',
        providerType: currentCredential.providerType,
        baseURL: currentCredential.baseURL ?? '',
        authKind: currentCredential.authKind,
        status: currentCredential.status,
        weight: currentCredential.weight,
        remark: currentCredential.remark ?? '',
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

    await updateMutation.mutateAsync({
      id: currentCredential.id,
      input: buildUpdateCredentialInput(values),
    });
    close();
  };

  return (
    <Dialog open={isOpen} onOpenChange={(nextOpen) => (nextOpen ? setOpen('edit') : close())}>
      <DialogContent className='sm:max-w-[620px]'>
        <DialogHeader>
          <DialogTitle>{t('credentials.dialogs.edit.title')}</DialogTitle>
          <DialogDescription>{t('credentials.dialogs.edit.description')}</DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit(onSubmit, () => {})} noValidate>
          <div className='grid max-h-[72vh] gap-4 overflow-y-auto py-4 pr-1'>
            <div className='grid gap-2'>
              <Label htmlFor='edit-credential-name'>{t('credentials.fields.name')}</Label>
              <Input id='edit-credential-name' {...register('name')} />
            </div>

            <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
              <div className='grid gap-2'>
                <Label>{t('credentials.fields.providerType')}</Label>
                <Input value={currentCredential?.providerType ?? ''} readOnly className='bg-muted' />
              </div>
              <div className='grid gap-2'>
                <Label>{t('credentials.fields.authKind')}</Label>
                <Input value={currentCredential ? t(`credentials.authKinds.${currentCredential.authKind}`) : ''} readOnly className='bg-muted' />
              </div>
            </div>

            <div className='grid gap-2'>
              <Label>{t('credentials.fields.baseURL')}</Label>
              <Input value={currentCredential?.baseURL ?? ''} readOnly className='bg-muted' />
            </div>

            <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
              <div className='grid gap-2'>
                <Label htmlFor='edit-credential-status'>{t('credentials.fields.status')}</Label>
                <Select value={status} onValueChange={(value) => setValue('status', value as CredentialStatus)}>
                  <SelectTrigger id='edit-credential-status'>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {statuses.map((item) => (
                      <SelectItem key={item} value={item}>
                        {t(`credentials.status.${item}`)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className='grid gap-2'>
                <Label htmlFor='edit-credential-weight'>{t('credentials.fields.weight')}</Label>
                <Input
                  id='edit-credential-weight'
                  type='number'
                  min={1}
                  {...register('weight', {
                    valueAsNumber: true,
                    min: { value: 1, message: t('credentials.validation.weightPositive') },
                  })}
                />
                {errors.weight && <span className='text-sm text-red-500'>{errors.weight.message}</span>}
              </div>
            </div>

            <div className='grid gap-2'>
              <Label htmlFor='edit-credential-remark'>{t('credentials.fields.remark')}</Label>
              <Textarea id='edit-credential-remark' rows={3} {...register('remark')} />
            </div>
          </div>
          <DialogFooter>
            <Button type='button' variant='outline' onClick={close}>
              {t('common.buttons.cancel')}
            </Button>
            <Button type='submit' disabled={updateMutation.isPending}>
              {updateMutation.isPending ? t('common.buttons.saving') : t('common.buttons.save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
