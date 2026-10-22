import { useRef, useState } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { useTranslator } from '@astryxdesign/core/i18n';
import { ArrowLeft, ArrowUpRight, Download, Pause, Play } from 'lucide-react';
import { call, isDesktop, mediaUrl } from '../../shared/lib/client';
import { formatCallError } from '../../shared/i18n/format';
import { SettingsGroup } from '../../shared/ui/settings-group';
import { InstrumentDownloadProgress } from '../../shared/ui/instrument-download-progress';
import type { InstrumentSample, InstrumentSource } from '../../shared/workbench';
import { AudioSeek } from './audio-seek';
import { localMediaPath, StorageLocation, useStorageDirectory } from './storage-location';

interface CatalogDetail { id: string; name: string; description: string; sampleCount: number; downloadable: boolean; sourceUrl: string }

const pitchNames = ['C', 'C♯', 'D', 'D♯', 'E', 'F', 'F♯', 'G', 'G♯', 'A', 'A♯', 'B'];
const pitchName = (pitch: number) => `${pitchNames[pitch % 12]}${Math.floor(pitch / 12) - 1}`;

// 只播放已入库的采样，和乐谱渲染器读取同一个 instruments 文件。
export function InstrumentDetail({ source, catalog, downloading, downloadReceived, downloadTotal, downloadUnit, downloadFailed, downloadError, onBack, onDownload }: {
  source?: InstrumentSource; catalog?: CatalogDetail; downloading: boolean;
  downloadReceived: number; downloadTotal: number; downloadUnit?: unknown;
  downloadFailed: boolean; downloadError: string;
  onBack: () => void; onDownload: () => void;
}) {
  const t = useTranslator();
  const audio = useRef<HTMLAudioElement>(null);
  const [playingID, setPlayingID] = useState('');
  const [selectedID, setSelectedID] = useState('');
  const [time, setTime] = useState(0);
  const [duration, setDuration] = useState(0);
  const [loadingID, setLoadingID] = useState('');
  const [error, setError] = useState('');
  const name = source?.name ?? catalog?.name ?? '';
  const sourceURL = source?.sourceUrl ?? catalog?.sourceUrl;
  const storage = useStorageDirectory(!!source);

  async function toggle(sample: InstrumentSample) {
    const element = audio.current;
    if (!element) return;
    if (selectedID === sample.id) {
      if (!element.paused) { element.pause(); return; }
      try { await element.play(); } catch { setError(t('@yovoice.instruments.samplePlayFailed')); }
      return;
    }
    element.pause(); setPlayingID(''); setError(''); setLoadingID(sample.id);
    try {
      setSelectedID(sample.id); setTime(0); setDuration(sample.duration);
      element.src = await mediaUrl('instruments', sample.fileName);
      element.currentTime = 0;
      await element.play();
      setPlayingID(sample.id);
    } catch {
      setError(t('@yovoice.instruments.samplePlayFailed'));
    } finally { setLoadingID(''); }
  }

  async function auditionPreset() {
    if (!source?.plugin || !audio.current) return;
    setLoadingID(source.id); setError('');
    try {
      const result = await call<{ fileName: string }>('plugin.auditionInstrument', { id: source.id });
      setSelectedID(source.id); setTime(0);
      audio.current.src = await mediaUrl('outputs', result.fileName);
      await audio.current.play();
    } catch (cause) { setError(formatCallError(t, cause)); }
    finally { setLoadingID(''); }
  }
  return <VStack className="library-page instrument-detail-page" gap={6}>
    <HStack className="library-heading" hAlign="between" vAlign="center" wrap="wrap" gap={3}>
      <VStack gap={1}><h1>{name}</h1><small>{source?.instrument ?? catalog?.description}</small></VStack>
      <Button size="sm" variant="secondary" icon={<ArrowLeft />} label={t('@yovoice.instruments.backLibrary')} onClick={onBack} />
    </HStack>
    <audio hidden ref={audio} preload="metadata" onEnded={() => setPlayingID('')} onPause={() => setPlayingID('')}
      onPlay={() => setPlayingID(selectedID)} onTimeUpdate={() => setTime(audio.current?.currentTime ?? 0)}
      onLoadedMetadata={() => setDuration(audio.current?.duration || 0)}
      onError={() => { setPlayingID(''); setError(t('@yovoice.instruments.samplePlayFailed')); }} />
    <VStack className="library-content" gap={5}>
      <SettingsGroup title={t('@yovoice.instruments.sourceDetails')}>
        <HStack className="settings-row" hAlign="between" vAlign="center" wrap="wrap" gap={3}>
          <small>{source ? `${source.samples.length} ${t('@yovoice.instruments.sampleCount')}` : `${catalog?.sampleCount ?? 0} ${t('@yovoice.instruments.sampleCount')}`}</small>
          <HStack gap={2} wrap="wrap">
            {sourceURL?.startsWith('https://') ? <Button size="sm" variant="ghost" icon={<ArrowUpRight size={15} />} label={t('@yovoice.instruments.sourcePage')} onClick={() => window.open(sourceURL, '_blank', 'noopener')} /> : null}
            {(!source || source.health === 'missing' || source.health === 'corrupt') && catalog?.downloadable ? <Button size="sm" variant="primary" icon={<Download size={15} />} label={t(source ? '@yovoice.arrangement.sourceNeedsRepair' : '@yovoice.instruments.download')} isDisabled={!isDesktop || downloading} isLoading={downloading} onClick={onDownload} /> : null}
          </HStack>
        </HStack>
        {source ? <StorageLocation path={source.plugin?.path || localMediaPath(storage.root, 'instruments')} error={storage.error} /> : null}
      </SettingsGroup>
      {downloading && (!source || source.health === 'missing' || source.health === 'corrupt') ? <InstrumentDownloadProgress received={downloadReceived} total={downloadTotal} unit={downloadUnit} /> : null}
      {source?.plugin ? <SettingsGroup title={t('@yovoice.plugins.currentPreset')} description={t('@yovoice.plugins.currentPresetHint')}>
        <HStack className="settings-row" gap={2} wrap="wrap"><small>{source.plugin.name} · {t(source.provenance === 'recorded' ? '@yovoice.plugins.recorded' : '@yovoice.plugins.synthesized')}</small>
          <Button size="sm" variant="secondary" label={t('@yovoice.plugins.openEditor')} onClick={() => void call('plugin.instrumentEditor', { id: source.id }).catch(cause => setError(formatCallError(t, cause)))} />
          <Button size="sm" variant="primary" label={t('@yovoice.plugins.auditionPreset')} isLoading={loadingID === source.id} onClick={() => void auditionPreset()} />
        </HStack>
        {selectedID === source.id ? <AudioSeek time={time} duration={duration} onSeek={seconds => { if (audio.current) audio.current.currentTime = seconds; setTime(seconds); }} /> : null}
      </SettingsGroup> : null}
      <SettingsGroup title={t('@yovoice.instruments.sampleAudition')} description={source ? t('@yovoice.instruments.sampleAuditionHint') : t('@yovoice.instruments.downloadToAudition')}>
        {source?.samples.map((sample, index) => <VStack key={sample.id} className="settings-row" gap={2}><HStack hAlign="between" vAlign="center" wrap="wrap" gap={3}>
          <VStack className="grow" gap={1}><b>{pitchName(sample.rootPitch)} · MIDI {sample.rootPitch}</b>
            <small>{sample.articulation} · {sample.duration.toFixed(1)}s · {t('@yovoice.instruments.sampleNumber', { number: index + 1 })}</small>
            {sample.generation ? <small>AI · {sample.generation.modelId} · {sample.generation.engineVersion} · {sample.generation.version}</small> : null}
            <small className="instrument-file-path">{localMediaPath(storage.root, 'instruments', sample.fileName) || sample.fileName}</small></VStack>
          <Button size="sm" variant="secondary" icon={playingID === sample.id ? <Pause size={15} /> : <Play size={15} />}
            label={t(playingID === sample.id ? '@yovoice.instruments.pauseSample' : '@yovoice.instruments.playSample')}
            isDisabled={!isDesktop} isLoading={loadingID === sample.id} onClick={() => void toggle(sample)} />
        </HStack>{selectedID === sample.id ? <AudioSeek time={time} duration={duration || sample.duration} onSeek={seconds => { if (audio.current) audio.current.currentTime = seconds; setTime(seconds); }} /> : null}</VStack>)}
      </SettingsGroup>
      {downloadFailed && !source ? <p role="alert">{t('@yovoice.arrangement.downloadFailed')}</p> : null}
      {downloadError ? <p role="alert">{downloadError}</p> : null}
      {error ? <p role="alert">{error}</p> : null}
    </VStack>
  </VStack>;
}
