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
import { useCreateUpstreamCredential } from '../data/credentials';
import type { CredentialFormValues, CredentialStatus } from '../data/schema';
import { CredentialQuotaFields } from './credential-quota-fields';
import { CredentialSecretFields } from './credential-secret-fields';
import { buildCreateCredentialInput, defaultCredentialFormValues } from './form-utils';

const statuses: CredentialStatus[] = ['enabled', 'disabled', 'archived'];

export function CreateCredentialDialog() {
  const { t } = useTranslation();
  const { open, setOpen } = useCredentialsContext();
  const createMutation = useCreateUpstreamCredential();
  const isOpen = open === 'create';

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
    if (isOpen) {
      reset(defaultCredentialFormValues);
    }
  }, [isOpen, reset]);

  const onSubmit = async (values: CredentialFormValues) => {
    await createMutation.mutateAsync(buildCreateCredentialInput(values));
    setOpen(null);
    reset(defaultCredentialFormValues);
  };

  return (
    <Dialog open={isOpen} onOpenChange={(nextOpen) => setOpen(nextOpen ? 'create' : null)}>
      <DialogContent className='sm:max-w-[720px]'>
        <DialogHeader>
          <DialogTitle>{t('credentials.dialogs.create.title')}</DialogTitle>
          <DialogDescription>{t('credentials.dialogs.create.description')}</DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit(onSubmit, () => {})} noValidate>
          <div className='grid max-h-[72vh] gap-4 overflow-y-auto py-4 pr-1'>
            <div className='grid gap-2'>
              <Label htmlFor='credential-name'>{t('credentials.fields.name')}</Label>
              <Input id='credential-name' {...register('name')} />
            </div>

            <CredentialSecretFields register={register} errors={errors} />

            <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
              <div className='grid gap-2'>
                <Label htmlFor='credential-status'>{t('credentials.fields.status')}</Label>
                <Select value={status} onValueChange={(value) => setValue('status', value as CredentialStatus)}>
                  <SelectTrigger id='credential-status'>
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
            </div>

            <CredentialQuotaFields register={register} setValue={setValue} watch={watch} errors={errors} />

            <div className='grid gap-2'>
              <Label htmlFor='credential-remark'>{t('credentials.fields.remark')}</Label>
              <Textarea id='credential-remark' rows={3} {...register('remark')} />
            </div>
          </div>
          <DialogFooter>
            <Button type='button' variant='outline' onClick={() => setOpen(null)}>
              {t('common.buttons.cancel')}
            </Button>
            <Button type='submit' disabled={createMutation.isPending}>
              {createMutation.isPending ? t('common.buttons.creating') : t('common.buttons.create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
