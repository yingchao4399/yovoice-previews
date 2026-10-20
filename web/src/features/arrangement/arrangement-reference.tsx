import { useEffect, useRef, useState } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { useTranslator } from '@astryxdesign/core/i18n';
import { call, importTimelineFile, isDesktop, mediaUrl } from '../../shared/lib/client';
import { formatCallError } from '../../shared/i18n/format';
import { Selector } from '../../shared/selector';
import { SettingsGroup } from '../../shared/ui/settings-group';
import type { ArrangementInput, AudioTimeline, Draft, State } from '../../shared/workbench';

// 参考音频只进入分析；用户演奏素材另建手工轨道，重新拼合时会保留。
export function ArrangementReference({ draft, state, change, openSettings, openScore }: {
  draft: Draft; state: State; change: (patch: Partial<Draft>) => void; openSettings: () => void; openScore: () => void;
}) {
  const t = useTranslator();
  const picker = useRef<HTMLInputElement>(null);
  const purpose = useRef<'reference' | 'performance'>('reference');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [url, setURL] = useState('');
  const [analysis, setAnalysis] = useState<{ id: string; started: number }>();
  const input: ArrangementInput = draft.arrangementInput ?? { brief: '', seconds: 90, source: 'auto' };
  const reference = draft.timeline?.assets?.find(item => item.id === input.referenceAssetId);
  const referenceFile = reference?.fileName;
  const modelInstalled = state.models.some(model => model.id.startsWith('sheetsage2'));
  const running = state.activity?.projectId === draft.id && state.activity.status === 'running';
  const requested = input.referenceAnalysisAt ? { id: reference?.id, started: input.referenceAnalysisAt } : analysis;
  const result = requested && requested.id === reference?.id && state.melody?.projectId === draft.id && state.melody.source === 'cover' && Date.parse(state.melody.createdAt) >= requested.started ? state.melody : undefined;
  useEffect(() => {
    setURL('');
    if (!referenceFile) return;
    let cancelled = false;
    void mediaUrl('outputs', referenceFile).then(value => { if (!cancelled) setURL(value); });
    return () => { cancelled = true; };
  }, [referenceFile]);
  async function upload(file: File) {
    setBusy(true); setError('');
    try {
      const asset = await importTimelineFile(file);
      const timeline: AudioTimeline = { tracks: [], ...draft.timeline, assets: [...(draft.timeline?.assets ?? []), asset] };
      if (purpose.current === 'reference') change({ timeline, arrangementInput: { ...input, referenceAssetId: asset.id, referenceAbc: undefined, referenceAnalysisAt: undefined } });
      else {
        timeline.tracks = [...timeline.tracks, { id: crypto.randomUUID().replaceAll('-', ''), name: asset.name, muted: false, clips: [{ id: crypto.randomUUID().replaceAll('-', ''), assetId: asset.id, start: 0, offset: 0, duration: asset.duration }] }];
        change({ timeline });
      }
    } catch (cause) { setError(formatCallError(t, cause)); }
    finally { setBusy(false); }
  }
  async function analyze() {
    if (!reference) return;
    setError(''); const started = Date.now(); setAnalysis({ id: reference.id, started }); change({ arrangementInput: { ...input, referenceAnalysisAt: started } });
    try { await call('arrangement.reference.analyze', { draft, assetId: reference.id }); }
    catch (cause) { setError(formatCallError(t, cause)); }
  }
  return <SettingsGroup title={t('@yovoice.arrangement.referenceTitle')} description={t('@yovoice.arrangement.referenceHint')}>
    <VStack className="settings-row" gap={3}>
      <input ref={picker} type="file" accept="audio/*" hidden onChange={event => { const file = event.target.files?.[0]; if (file) void upload(file); event.target.value = ''; }} />
      <HStack gap={2} wrap="wrap">
        <Button size="sm" variant="secondary" label={t('@yovoice.arrangement.importReference')} isDisabled={busy} onClick={() => { purpose.current = 'reference'; picker.current?.click(); }} />
        <Button size="sm" variant="secondary" label={t('@yovoice.arrangement.importPerformance')} isDisabled={busy} onClick={() => { purpose.current = 'performance'; picker.current?.click(); }} />
        <Button size="sm" variant="ghost" label={t('@yovoice.arrangement.openScore')} onClick={openScore} />
      </HStack>
      {draft.timeline?.assets?.length ? <Selector label={t('@yovoice.arrangement.referenceAudio')} value={reference?.id ?? ''} options={[{ value: '', label: t('@yovoice.arrangement.noReference') }, ...draft.timeline.assets.map(asset => ({ value: asset.id, label: asset.name }))]} onChange={referenceAssetId => change({ arrangementInput: { ...input, referenceAssetId, referenceAbc: undefined, referenceAnalysisAt: undefined } })} /> : null}
      {reference ? <VStack gap={2}>{url ? <audio controls src={url} /> : null}<small>{input.referenceAbc ? t('@yovoice.arrangement.referenceAnalyzed') : t('@yovoice.arrangement.referenceNotAnalyzed')}</small>
        <Button size="sm" variant="secondary" label={t('@yovoice.arrangement.analyzeReference')} isDisabled={!isDesktop || running} onClick={() => void (modelInstalled ? analyze() : openSettings())} />
        {!modelInstalled ? <small>{t('@yovoice.arrangement.referenceModelRequired')}</small> : null}
        {result ? <Button size="sm" variant="primary" label={t('@yovoice.arrangement.useReferenceAnalysis')} onClick={() => { change({ arrangementInput: { ...input, referenceAbc: result.abc } }); void call('melody.dismiss', { id: result.id }); }} /> : null}
      </VStack> : null}
      {error ? <small role="alert">{error}</small> : null}
    </VStack>
  </SettingsGroup>;
}
