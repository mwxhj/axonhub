'use client';

import React, { createContext, useContext, useState } from 'react';
import type { UpstreamCredential } from '../data/credentials';

type CredentialsDialogType = 'create' | 'edit' | 'rotate' | 'status' | 'channels' | null;

interface CredentialsContextType {
  open: CredentialsDialogType;
  setOpen: (open: CredentialsDialogType) => void;
  currentCredential: UpstreamCredential | null;
  setCurrentCredential: React.Dispatch<React.SetStateAction<UpstreamCredential | null>>;
}

const CredentialsContext = createContext<CredentialsContextType | undefined>(undefined);

export function useCredentialsContext() {
  const context = useContext(CredentialsContext);
  if (!context) {
    throw new Error('useCredentialsContext must be used within CredentialsProvider');
  }
  return context;
}

interface CredentialsProviderProps {
  children: React.ReactNode;
}

export default function CredentialsProvider({ children }: CredentialsProviderProps) {
  const [open, setOpen] = useState<CredentialsDialogType>(null);
  const [currentCredential, setCurrentCredential] = useState<UpstreamCredential | null>(null);

  return (
    <CredentialsContext.Provider value={{ open, setOpen, currentCredential, setCurrentCredential }}>
      {children}
    </CredentialsContext.Provider>
  );
}
