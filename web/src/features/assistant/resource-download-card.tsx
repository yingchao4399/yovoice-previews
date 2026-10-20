import { useState } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { StatusDot } from '@astryxdesign/core/StatusDot';
import { useTranslator } from '@astryxdesign/core/i18n';
import { Download } from 'lucide-react';
import { call } from '../../shared/lib/client';
import { formatCallError } from '../../shared/i18n/format';
import { formatBytes, type Activity, type AssistantProposal } from '../../shared/workbench';

export function ResourceDownloadCard({ proposal: p, activity, onResolve }: { proposal: AssistantProposal; activity?: Activity | null; onResolve: (id: string, confirm: boolean) => Promise<void> }) {
  const t = useTranslator();
  const batch = p.downloads!;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const total = batch.items.reduce((sum, item) => sum + item.bytes, 0);
  const complete = batch.items.filter(item => item.state === 'completed').reduce((sum, item) => sum + item.bytes, 0);
  const current = batch.items.find(item => item.state === 'running');
  const bytes = activity?.requestId === p.id && activity.params?.name === current?.name ? activity.received ?? 0 : 0;
  const pending = p.state === 'pending';
  const active = batch.state === 'queued' || batch.state === 'running';
  const perform = async (action: 'confirm' | 'dismiss' | 'resume' | 'cancel') => {
    setBusy(true); setError('');
    try { if (action === 'confirm' || action === 'dismiss') await onResolve(p.id, action === 'confirm'); else await call('assistant.download.control', { id: p.id, action }); }
    catch (e) { setError(formatCallError(t, e)); }
    finally { setBusy(false); }
  };
  return <VStack className="assistant-proposal resource-download-card" gap={3} role="group" aria-label={t('@yovoice.resources.downloadTitle')} data-testid="resource-download-card">
    <HStack gap={2} vAlign="center"><Download size={16} /><strong className="grow">{t('@yovoice.resources.downloadTitle')}</strong><small>{formatBytes(total)}</small></HStack>
    {p.reason ? <small>{p.reason}</small> : null}
    <VStack as="ul" gap={2}>{batch.items.map(item => {
      const state = (batch.state === 'paused' || batch.state === 'cancelled') && item.state === 'running' ? batch.state : item.state;
      return <HStack as="li" key={item.catalogId} gap={2} vAlign="center"><VStack className="grow" gap={0}><b>{item.name}</b><small>{item.license} · {formatBytes(item.bytes)}</small></VStack>
        <HStack gap={1} vAlign="center"><StatusDot variant={state === 'completed' ? 'success' : state === 'failed' ? 'error' : state === 'running' ? 'accent' : 'neutral'} isPulsing={state === 'running'} label={t(`@yovoice.resources.downloadState.${state}`)} /><small>{t(`@yovoice.resources.downloadState.${state}`)}</small></HStack></HStack>;
    })}</VStack>
    <small>{t('@yovoice.resources.downloadScope')}</small>
    {pending ? <HStack gap={2} hAlign="end"><Button size="sm" variant="ghost" label={t('@yovoice.assistant.proposal.dismiss')} isDisabled={busy} onClick={() => void perform('dismiss')} /><Button size="sm" variant="primary" label={t('@yovoice.resources.confirmDownload')} isLoading={busy} onClick={() => void perform('confirm')} /></HStack>
      : <>
        <HStack gap={2} vAlign="center"><small className="grow">{t(`@yovoice.resources.downloadState.${batch.state}`)}</small><small>{formatBytes(Math.min(total, complete + bytes))} / {formatBytes(total)}</small></HStack>
        <progress className="ai-composer-progress" max={Math.max(1, total)} value={Math.min(total, complete + bytes)} aria-label={t('@yovoice.resources.downloadProgress')} />
        {active ? <Button size="sm" variant="ghost" label={t('@yovoice.action.cancel')} isDisabled={busy} onClick={() => void perform('cancel')} /> : null}
        {batch.state === 'failed' || batch.state === 'paused' ? <Button size="sm" variant="secondary" label={t('@yovoice.resources.retryRemaining')} isLoading={busy} onClick={() => void perform('resume')} /> : null}
      </>}
    {error || p.errorCode ? <small role="alert" data-tone="danger">{error || formatCallError(t, p)}</small> : null}
  </VStack>;
}
