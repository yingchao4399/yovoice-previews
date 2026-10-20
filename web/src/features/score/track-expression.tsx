import { useState } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { Collapsible } from '@astryxdesign/core/Collapsible';
import { VStack, HStack } from '@astryxdesign/core/Layout';
import { Slider } from '@astryxdesign/core/Slider';
import { useTranslator } from '@astryxdesign/core/i18n';
import { Selector } from '../../shared/selector';
import type { InstrumentSource, Score, ScoreTrack } from '../../shared/workbench';

// 渲染器已有的声部参数在这里编辑；每次改动只替换当前声部，不触碰歌曲模型或其他声部。
export function TrackExpression({ score, sources = [], change }: { score: Score; sources?: InstrumentSource[]; change: (score: Score) => void }) {
  const t = useTranslator();
  const [selectedId, setSelectedId] = useState('');
  const track = score.tracks.find(item => item.id === selectedId) ?? score.tracks[0];
  if (!track) return null;
  const update = (patch: Partial<ScoreTrack>) => change({ ...score, tracks: score.tracks.map(item => item.id === track.id ? { ...item, ...patch } : item) });
  const humanize = track.humanize ?? { velocity: 0, timingMs: 0 };
  const formatPercent = (value: number) => `${Math.round(value * 100)}%`;
  return <Collapsible trigger={t('@yovoice.score.expression.title')} defaultIsOpen={false}>
    <VStack gap={3}>
      <small>{t('@yovoice.score.expression.description')}</small>
      <Selector label={t('@yovoice.score.expression.track')} value={track.id} options={score.tracks.map(item => ({ value: item.id, label: item.name }))} onChange={setSelectedId} />
      <Selector label={t('@yovoice.score.expression.source')} value={track.sourceId ?? ''} options={[{ value: '', label: t('@yovoice.score.expression.soundfontSource') }, ...sources.map(source => ({ value: source.id, label: `${source.name} · ${source.instrument}` }))]} onChange={sourceId => update({ sourceId: sourceId || undefined })} />
      {track.sourceId ? <small>{t('@yovoice.score.expression.sampleCoverage')}</small> : null}
      <Slider label={t('@yovoice.score.expression.velocity')} value={humanize.velocity ?? 0} min={0} max={40} step={1} valueDisplay="text" formatValue={value => `±${value}`} onChange={(velocity: number) => update({ humanize: { ...humanize, velocity } })} />
      <Slider label={t('@yovoice.score.expression.timing')} value={humanize.timingMs ?? 0} min={0} max={50} step={1} valueDisplay="text" formatValue={value => `±${value} ms`} onChange={(timingMs: number) => update({ humanize: { ...humanize, timingMs } })} />
      <Slider label={t('@yovoice.score.expression.pan')} value={track.pan ?? 0} min={-1} max={1} step={0.05} valueDisplay="text" formatValue={value => value === 0 ? t('@yovoice.score.expression.center') : `${Math.round(Math.abs(value) * 100)}% ${t(value < 0 ? '@yovoice.score.expression.left' : '@yovoice.score.expression.right')}`} onChange={(pan: number) => update({ pan })} />
      <Slider label={t('@yovoice.score.expression.reverb')} value={track.reverb ?? 0} min={0} max={1} step={0.05} valueDisplay="text" formatValue={formatPercent} onChange={(reverb: number) => update({ reverb })} />
      <HStack gap={2} wrap="wrap">
        <Button size="sm" variant="secondary" label={t('@yovoice.score.expression.natural')} onClick={() => update({ humanize: { velocity: 8, timingMs: 10 } })} />
        <Button size="sm" variant="ghost" label={t('@yovoice.score.expression.reset')} onClick={() => update({ humanize: undefined, pan: undefined, reverb: undefined })} />
      </HStack>
    </VStack>
  </Collapsible>;
}
