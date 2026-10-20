import { Button } from '@astryxdesign/core/Button';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { Tab, TabList } from '@astryxdesign/core/TabList';
import { useTranslator } from '@astryxdesign/core/i18n';
import { FileMusic, FolderDown, ListMusic } from 'lucide-react';
import { call, isDesktop } from '../../shared/lib/client';
import { Selector } from '../../shared/selector';
import type { Draft, State, UiLocale } from '../../shared/workbench';
import { ScoreEditor } from '../score/score-editor';
import { folkProfiles } from './folk-profiles';
import { MusicEditor } from './music-editor';

export type MusicView = 'compose' | 'score' | 'audio' | 'export';

// 新页签只包裹音乐作品，原歌曲编辑器与独立曲谱作品保持原样。
export function MusicWorkspace({ draft, state, change, locale, onError, view, setView, save }: {
  draft: Draft; state: State; change: (patch: Partial<Draft>) => void; locale: UiLocale;
  onError: (error: string) => void; view: MusicView; setView: (view: MusicView) => void; save: () => Promise<unknown>;
}) {
  const t = useTranslator();
  const profile = folkProfiles.find(item => item.id === draft.folkProfileId);
  const label = (id: MusicView) => t(`@yovoice.music.workspace.${id}`);
  const localized = (text: Record<'zh-CN' | 'en', string>) => text[locale === 'zh-CN' ? 'zh-CN' : 'en'];
  return <VStack className="music-workspace" gap={5}>
    <TabList value={view} onChange={value => setView(value as MusicView)} role="tablist" hasDivider>
      {(['compose', 'score', 'audio', 'export'] as const).map(id => <Tab key={id} value={id} label={label(id)} panelId={`music-${id}-panel`} />)}
    </TabList>
    {view === 'compose' ? <VStack id="music-compose-panel" role="tabpanel" aria-label={label('compose')} gap={5}>
      <VStack className="music-block" gap={3}>
        <h2>{t('@yovoice.music.folkProfile')}</h2>
        <HStack gap={2} vAlign="center" wrap="wrap">
          <Selector label={t('@yovoice.music.folkProfile')} isLabelHidden value={draft.folkProfileId ?? ''} options={[{ value: '', label: t('@yovoice.music.chooseProfile') }, ...folkProfiles.map(item => ({ value: item.id, label: localized(item.name) }))]} onChange={folkProfileId => change({ folkProfileId: folkProfileId || undefined })} />
          <Button size="sm" variant="secondary" label={t('@yovoice.music.applyProfile')} isDisabled={!profile} onClick={() => { if (profile) change({ text: profile.prompt.slice(0, 512) }); }} />
        </HStack>
        {profile ? <small>{localized(profile.ensemble)}</small> : null}
      </VStack>
      <MusicEditor draft={draft} state={state} change={change} locale={locale} onError={onError} />
    </VStack> : null}
    {view === 'score' ? <VStack id="music-score-panel" role="tabpanel" aria-label={label('score')} gap={4}>
      <small>{t('@yovoice.music.scoreTimbreHint')}</small>
      <ScoreEditor draft={draft} change={change} locale={locale} onError={onError} />
    </VStack> : null}
    {view === 'audio' ? <VStack id="music-audio-panel" role="tabpanel" aria-label={label('audio')} gap={3}>
      <h2>{label('audio')}</h2>
      <p>{t('@yovoice.music.audioHint')}</p>
      {!draft.timeline ? <Button size="sm" variant="secondary" icon={<ListMusic />} label={t('@yovoice.music.createTimeline')} onClick={() => change({ timeline: { tracks: [] } })} /> : <small>{t('@yovoice.music.timelineBelow')}</small>}
    </VStack> : null}
    {view === 'export' ? <VStack id="music-export-panel" role="tabpanel" aria-label={label('export')} gap={3}>
      <h2>{label('export')}</h2>
      <p>{t('@yovoice.music.exportHint')}</p>
      <HStack gap={2} wrap="wrap">
        <Button size="sm" variant="secondary" icon={<FileMusic />} label={t('@yovoice.music.openScoreExport')} onClick={() => setView('score')} />
        {isDesktop ? <Button size="sm" variant="secondary" icon={<FolderDown />} label={t('@yovoice.music.exportProject')} onClick={() => void (async () => { await save(); await call('project.export', { id: draft.id, name: draft.title }); })().catch(error => onError((error as Error).message))} /> : null}
      </HStack>
    </VStack> : null}
  </VStack>;
}
