import { createFileRoute } from '@tanstack/react-router';
import CredentialsManagement from '@/features/credentials';

export const Route = createFileRoute('/_authenticated/credentials/')({
  component: CredentialsManagement,
});
