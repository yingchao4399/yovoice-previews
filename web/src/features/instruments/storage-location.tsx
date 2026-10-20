import { useEffect, useState } from 'react';
import { VStack } from '@astryxdesign/core/Layout';
import { useTranslator } from '@astryxdesign/core/i18n';
import { call, isDesktop } from '../../shared/lib/client';
import { formatCallError } from '../../shared/i18n/format';

// 读取当前数据目录，避免把默认目录写死；详情内的所有采样共用一次查询。
export function useStorageDirectory(enabled: boolean) {
  const [root, setRoot] = useState('');
  const [error, setError] = useState<unknown>(null);
  useEffect(() => {
    if (!enabled || !isDesktop) return;
    let active = true;
    void call<{ root: string }>('storage.info').then(info => {
      if (active) { setRoot(info.root); setError(null); }
    }).catch(cause => { if (active) setError(cause); });
    return () => { active = false; };
  }, [enabled]);
  return { root, error };
}

export function localMediaPath(root: string, kind: 'instruments' | 'outputs', fileName?: string) {
  if (!root) return '';
  const separator = root.includes('\\') ? '\\' : '/';
  return [root.replace(/[\\/]+$/, ''), kind, ...(fileName ? [fileName] : [])].join(separator);
}

export function StorageLocation({ path, error }: { path: string; error?: unknown }) {
  const t = useTranslator();
  if (!path && !error) return null;
  return <VStack className="settings-row" gap={1}>
    <b>{t('@yovoice.instruments.storageLocation')}</b>
    {path ? <small className="instrument-file-path">{path}</small> : <small role="alert">{formatCallError(t, error)}</small>}
  </VStack>;
}
