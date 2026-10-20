import { useState } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { CheckboxInput } from '@astryxdesign/core/CheckboxInput';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { useTranslator } from '@astryxdesign/core/i18n';
import { call, isDesktop, mediaUrl, saveAudio } from '../../shared/lib/client';
import { formatCallError } from '../../shared/i18n/format';
import { Selector } from '../../shared/selector';
import { formatTime, type AudioAsset, type Draft, type State } from '../../shared/workbench';

// 生成目标与导出副本分开：不能通过截断/补零冒充模型生成了指定长度。
export function MusicAudioExport({ draft, state }: { draft: Draft; state: State }) {
  const t = useTranslator();
  const versions = state.history.filter(g => !g.segment && g.settings.id === draft.id).sort((a,b) => b.createdAt.localeCompare(a.createdAt));
  const [selected, setSelected] = useState(''), [fixed, setFixed] = useState(false), [pad, setPad] = useState(false);
  const [seconds, setSeconds] = useState(draft.music?.duration.mode === 'fixed' ? draft.music.duration.seconds ?? 90 : 90);
  const [busy, setBusy] = useState(false), [error, setError] = useState('');
  const source = versions.find(g => g.id === selected) ?? versions[0];
  const run = async () => {
    if (!source || busy) return;
    setBusy(true); setError('');
    let temporary: AudioAsset | undefined, url = '';
    try {
      if (fixed) temporary = await call<AudioAsset>('resources.region', { id: `generation:${source.id}`, region: { seconds, pad, fadeOut: .08 } });
      url = await mediaUrl('outputs', temporary?.fileName ?? source.fileName);
      const response = await fetch(url);
      if (!response.ok) throw new Error(t('@yovoice.error.audioMissing'));
      await saveAudio(await response.blob(), fixed ? `${source.title}-${seconds}s` : source.title);
    } catch (cause) { setError(formatCallError(t,cause)); }
    finally {
      if (url.startsWith('blob:')) URL.revokeObjectURL(url);
      if (temporary) { try { await call('resources.discard',{ id:temporary.id }); } catch { /* 已保存的音频不受清理临时副本失败影响，可由存储清理回收。 */ } }
      setBusy(false);
    }
  };
  if (!source) return null;
  return <VStack gap={3}>
    <Selector label={t('@yovoice.music.exportVersion')} value={source.id} isDisabled={busy} options={versions.map(g => ({ value:g.id,label:`${g.title} · ${formatTime(g.duration)}` }))} onChange={setSelected} />
    <CheckboxInput label={t('@yovoice.music.exportFixed')} value={fixed} onChange={setFixed} isDisabled={busy || !isDesktop} />
    {fixed ? <VStack gap={2}>
      <label className="number-field">{t('@yovoice.music.exportSeconds')}<input type="number" min={1} max={600} step={1} value={seconds} disabled={busy} onChange={event => setSeconds(Number(event.target.value))} /></label>
      <CheckboxInput label={t('@yovoice.music.exportPad')} value={pad} onChange={setPad} isDisabled={busy} />
      <small>{t('@yovoice.music.exportFixedHint')}</small>
    </VStack> : null}
    <HStack gap={2}><Button size="sm" variant="primary" label={t('@yovoice.music.exportAudioCopy')} isLoading={busy} isDisabled={busy || fixed && (!Number.isInteger(seconds) || seconds < 1 || seconds > 600 || seconds > source.duration && !pad)} onClick={() => void run()} /></HStack>
    {!isDesktop ? <small>{t('@yovoice.music.exportDesktopHint')}</small> : null}
    {error ? <p role="alert">{error}</p> : null}
  </VStack>;
}
