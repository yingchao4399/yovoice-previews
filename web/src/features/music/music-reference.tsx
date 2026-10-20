import { useEffect, useRef, useState } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { TextArea } from '@astryxdesign/core/TextArea';
import { useTranslator } from '@astryxdesign/core/i18n';
import { call, importMusicReferenceFile } from '../../shared/lib/client';
import { formatCallError } from '../../shared/i18n/format';
import { ResourceAudioPreview, ResourcePicker } from '../../shared/ui/resource-picker';
import type { ResourceItem } from '../../shared/resource';
import type { AudioAsset, AudioTimeline, Draft } from '../../shared/workbench';

// 参考仅产生描述条件；加入素材是另一项显式操作，不能偷偷混入成曲。
export function MusicReference({ draft, kind, change }: { draft: Draft; kind: 'voice' | 'accompaniment'; change: (patch: Partial<Draft>) => void }) {
  const t = useTranslator();
  const picker = useRef<HTMLInputElement>(null), latest = useRef(draft), mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  latest.current = draft;
  const [library, setLibrary] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState('');
  const [expanded, setExpanded] = useState(false);
  const voice = kind === 'voice';
  const id = voice ? draft.music?.voiceReferenceAssetId : draft.music?.accompanimentAssetId;
  const description = (voice ? draft.music?.voiceReferenceDescription : draft.music?.accompanimentDescription) ?? '';
  const asset = draft.timeline?.assets?.find(item => item.id === id);
  const music = draft.music ?? { duration: { mode: 'auto' as const } };
  const descriptionPatch = (value: string) => voice ? { voiceReferenceDescription: value } : { accompanimentDescription: value };
  const attach = (value: AudioAsset) => {
    if (!mounted.current || latest.current.id !== draft.id) throw new Error(t('@yovoice.resources.workChanged'));
    const current = latest.current;
    const currentMusic = current.music ?? { duration: { mode: 'auto' as const } };
    const timeline: AudioTimeline = { tracks: [], ...current.timeline, assets: [...(current.timeline?.assets ?? []), value] };
    change({ timeline, music: { ...currentMusic, ...(voice ? { voiceReferenceAssetId: value.id, voiceReferenceDescription: '' } : { accompanimentAssetId: value.id, accompanimentDescription: '' }) } });
  };
  const upload = async (file: File) => {
    setBusy(true); setError('');
    try { attach(await importMusicReferenceFile(file)); }
    catch (cause) { if (mounted.current) setError(formatCallError(t, cause)); }
    finally { if (mounted.current) setBusy(false); }
  };
  const choose = async (item: ResourceItem) => { attach(await call<AudioAsset>('resources.import', { id: item.id })); setLibrary(false); };
  const addMaterial = () => {
    if (!asset) return;
    const timeline = draft.timeline!;
    if (timeline.tracks.some(track => track.clips.some(clip => clip.assetId === asset.id))) return;
    change({ timeline: { ...timeline, tracks: [...timeline.tracks, { id: crypto.randomUUID().replaceAll('-', ''), name: asset.name, muted: false, clips: [{ id: crypto.randomUUID().replaceAll('-', ''), assetId: asset.id, start: 0, offset: 0, duration: asset.duration }] }] } });
  };
  return <VStack className="music-reference" gap={2}>
    {voice ? <h3>{t('@yovoice.music.referenceVoice')}</h3> : <Button size="sm" variant="ghost" label={t('@yovoice.music.referenceAccompaniment')} aria-expanded={expanded} onClick={() => setExpanded(!expanded)} />}
    {voice || expanded ? <>
    <small>{t('@yovoice.music.referenceSemanticHint')}</small>
    <input ref={picker} type="file" accept="audio/*" hidden onChange={event => { const file = event.target.files?.[0]; if (file) void upload(file); event.target.value = ''; }} />
    <HStack gap={2} wrap="wrap">
      <Button size="sm" variant="secondary" label={t('@yovoice.music.referenceLibrary')} isDisabled={busy} onClick={() => setLibrary(true)} />
      <Button size="sm" variant="secondary" label={t('@yovoice.music.referenceUpload')} isLoading={busy} isDisabled={busy} onClick={() => picker.current?.click()} />
    </HStack>
    {asset ? <VStack gap={2}><strong>{asset.name}</strong><ResourceAudioPreview mediaKind="outputs" fileName={asset.fileName} />
      <HStack gap={2} wrap="wrap">
        {!voice ? <Button size="sm" label={t('@yovoice.music.referenceAddMaterial')} isDisabled={draft.timeline?.tracks.some(track => track.clips.some(clip => clip.assetId === asset.id))} onClick={addMaterial} /> : null}
        <Button size="sm" variant="ghost" label={t('@yovoice.music.referenceClear')} onClick={() => change({ music: { ...music, ...(voice ? { voiceReferenceAssetId: undefined, voiceReferenceDescription: '' } : { accompanimentAssetId: undefined, accompanimentDescription: '' }) } })} />
      </HStack></VStack> : null}
    <TextArea label={t(voice ? '@yovoice.music.referenceVoiceDescription' : '@yovoice.music.referenceMusicDescription')} value={description} rows={3} maxLength={1500} onChange={value => change({ music: { ...music, ...descriptionPatch(value) } })} />
    {error ? <small role="alert">{error}</small> : null}
    {library ? <ResourcePicker choose={choose} close={() => setLibrary(false)} /> : null}
    </> : null}
  </VStack>;
}
