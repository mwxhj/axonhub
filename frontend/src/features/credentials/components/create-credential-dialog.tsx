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
import { CHANNEL_CONFIGS } from '@/features/channels/data/config_channels';
import type { ChannelType } from '@/features/channels/data/schema';
import { useCredentialsContext } from '../context/credentials-context';
import { useCreateUpstreamCredential } from '../data/credentials';
import type { CredentialAuthKind, CredentialFormValues, CredentialStatus } from '../data/schema';
import { CredentialSecretFields } from './credential-secret-fields';
import { buildCreateCredentialInput, defaultCredentialFormValues } from './form-utils';

const providerTypes = Object.keys(CHANNEL_CONFIGS).sort() as ChannelType[];
const authKinds: CredentialAuthKind[] = ['api_key', 'oauth', 'azure', 'gcp', 'other'];
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

  const authKind = watch('authKind');
  const providerType = watch('providerType') as ChannelType;
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
      <DialogContent className='sm:max-w-[760px]'>
        <DialogHeader>
          <DialogTitle>{t('credentials.dialogs.create.title')}</DialogTitle>
          <DialogDescription>{t('credentials.dialogs.create.description')}</DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit(onSubmit, () => {})} noValidate>
          <div className='grid max-h-[72vh] gap-4 overflow-y-auto py-4 pr-1'>
            <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
              <div className='grid gap-2'>
                <Label htmlFor='credential-name'>{t('credentials.fields.name')}</Label>
                <Input id='credential-name' {...register('name')} />
              </div>
              <div className='grid gap-2'>
                <Label htmlFor='credential-provider-type'>{t('credentials.fields.providerType')}</Label>
                <Select value={providerType} onValueChange={(value) => setValue('providerType', value)}>
                  <SelectTrigger id='credential-provider-type'>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {providerTypes.map((type) => (
                      <SelectItem key={type} value={type}>
                        {type}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>

            <div className='grid gap-2'>
              <Label htmlFor='credential-base-url'>{t('credentials.fields.baseURL')}</Label>
              <Input id='credential-base-url' placeholder={CHANNEL_CONFIGS[providerType]?.baseURL} {...register('baseURL')} />
            </div>

            <div className='grid grid-cols-1 gap-4 md:grid-cols-3'>
              <div className='grid gap-2'>
                <Label htmlFor='credential-auth-kind'>{t('credentials.fields.authKind')}</Label>
                <Select value={authKind} onValueChange={(value) => setValue('authKind', value as CredentialAuthKind)}>
                  <SelectTrigger id='credential-auth-kind'>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {authKinds.map((kind) => (
                      <SelectItem key={kind} value={kind}>
                        {t(`credentials.authKinds.${kind}`)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
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
              <div className='grid gap-2'>
                <Label htmlFor='credential-weight'>{t('credentials.fields.weight')}</Label>
                <Input
                  id='credential-weight'
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

            <CredentialSecretFields authKind={authKind} register={register} errors={errors} />

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
