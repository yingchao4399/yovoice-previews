import { formatCallError } from '../../shared/i18n/format';
import { useRef, useState, type ReactNode } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { CheckboxInput } from '@astryxdesign/core/CheckboxInput';
import { FileInput } from '@astryxdesign/core/FileInput';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { SegmentedControl, SegmentedControlItem } from '@astryxdesign/core/SegmentedControl';
import { Tab, TabList } from '@astryxdesign/core/TabList';
import { StatusDot } from '@astryxdesign/core/StatusDot';
import { TextInput } from '@astryxdesign/core/TextInput';
import { useTranslator } from '@astryxdesign/core/i18n';
import { ArrowLeft, ArrowUpRight, AudioLines, Download, ListMusic, Music2, Pause, Play, Plus, Search } from 'lucide-react';
import { call, importInstrumentFile, isDesktop, logEvent, mediaUrl } from '../../shared/lib/client';
import { Selector } from '../../shared/selector';
import { SettingsGroup } from '../../shared/ui/settings-group';
import { InstrumentDownloadProgress } from '../../shared/ui/instrument-download-progress';
import type { ModelPackage, State } from '../../shared/workbench';
import { LibraryEmpty, LibraryPage, Ticket, TicketStamp } from '../library/library-layout';
import xsxbCatalog from './xsxb-catalog.json';
import { InstrumentDetail } from './instrument-detail';
import { AudioSeek } from './audio-seek';
import { localMediaPath, StorageLocation, useStorageDirectory } from './storage-location';

interface InstrumentCandidate { id: string; title: string; creator: string; provider: string; landingUrl: string; license: string; duration: number; kind: 'noteCandidate' | 'phraseCandidate' }
type LibraryTab = 'mine' | 'catalog' | 'online' | 'sites' | 'ai' | 'bands';
const sampleSites = [
  { id: 'pianobook', name: 'Pianobook', url: 'https://www.pianobook.co.uk/', license: 'sitePianobookLicense', description: 'sitePianobook' },
  { id: 'decent', name: 'Decent Samples', url: 'https://store.decentsamples.com/downloads/decent-sampler/versions', license: 'siteDecentLicense', description: 'siteDecent' },
  { id: 'labs', name: 'Splice INSTRUMENT / LABS', url: 'https://splice.com/instrument/labs-instrument', license: 'siteLabsLicense', description: 'siteLabs' },
  { id: 'dsk', name: 'DSK Asian DreamZ', url: 'https://www.dskmusic.com/dsk-asian-dreamz-updated1/', license: 'siteDSKLicense', description: 'siteDSK' },
  { id: 'kong', name: 'Kong Audio Mini DiZi / ErHu', url: 'http://www.kongaudio.com/free.htm', license: 'siteKongLicense', description: 'siteKong' },
  { id: 'opengameart', name: 'OpenGameArt', url: 'https://opengameart.org/', license: 'siteOpenGameArtLicense', description: 'siteOpenGameArt' },
  { id: 'freesound', name: 'Freesound', url: 'https://freesound.org/search/', license: 'siteFreesoundLicense', description: 'siteFreesound' },
  { id: 'sfz', name: 'SFZ Instruments', url: 'https://sfzinstruments.github.io/', license: 'siteSFZLicense', description: 'siteSFZ' },
] as const;

// 与声音库共用页眉、页签、搜索和票根式列表；导入表单作为次级页面打开。
export function InstrumentLibrary({ state, catalog, openSettings }: { state: State; catalog: ModelPackage[]; openSettings: (tab: string) => void }) {
  const t = useTranslator();
  const [tab, setTab] = useState<LibraryTab>('mine');
  const [aiCategory, setAICategory] = useState('materials');
  const [importing, setImporting] = useState(false);
  const [query, setQuery] = useState('');
  const [family, setFamily] = useState('all');
  const [file, setFile] = useState<File | null>(null);
  const [sourceId, setSourceId] = useState('');
  const [name, setName] = useState('');
  const [instrument, setInstrument] = useState('');
  const [origin, setOrigin] = useState<'real' | 'ai'>('real');
  const [rootPitch, setRootPitch] = useState('60');
  const [articulation, setArticulation] = useState('normal');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [candidates, setCandidates] = useState<InstrumentCandidate[]>([]);
  const [searchedName, setSearchedName] = useState('');
  const [searching, setSearching] = useState(false);
  const [importingID, setImportingID] = useState('');
  const [confirmedID, setConfirmedID] = useState('');
  const [detail, setDetail] = useState<{ sourceID?: string; catalogID?: string } | null>(null);
  const [materialDetailID, setMaterialDetailID] = useState('');
  const audio = useRef<HTMLAudioElement>(null);
  const playRequest = useRef(0);
  const [playingMaterial, setPlayingMaterial] = useState('');
  const [selectedMaterial, setSelectedMaterial] = useState('');
  const [audioTime, setAudioTime] = useState(0);
  const [audioDuration, setAudioDuration] = useState(0);
  const storage = useStorageDirectory(!!materialDetailID);
  const sources = state.instrumentSources ?? [];
  const downloading = state.modelActivity?.status === 'running' && state.modelActivity.kind === 'instrumentDownload';
  const models = catalog.filter(model => model.family === 'soundfont' || model.family === 'ace_step' || model.family === 'yue2');
  const installedIDs = new Set(sources.filter(source => source.health !== 'missing' && source.health !== 'corrupt').map(source => source.catalogId).filter(Boolean));
  const search = query.trim().toLocaleLowerCase();
  const filteredSources = sources.filter(source => [source.name, source.instrument, source.license ?? ''].some(value => value.toLocaleLowerCase().includes(search)));
  const filteredCatalog = xsxbCatalog.filter(item => (family === 'all' || item.family === family) && [item.name, item.id, item.description, item.family].some(value => value.toLocaleLowerCase().includes(search)));
  const filteredModels = models.filter(model => [model.name, model.family].some(value => value.toLocaleLowerCase().includes(search)));
  const filteredMaterials = (state.arrangementMaterials ?? []).filter(item => [item.instrument, item.role, item.technique ?? '', item.modelId].some(value => value.toLocaleLowerCase().includes(search)));
  const filteredBands = (state.bandPresets ?? []).filter(item => [item.name, ...item.parts.map(part => part.instrument)].some(value => value.toLocaleLowerCase().includes(search)));
  const filteredCandidates = query.trim() === searchedName ? candidates : [];
  const filteredSites = sampleSites.filter(site => [site.name, t(`@yovoice.instruments.${site.license}`), t(`@yovoice.instruments.${site.description}`)].some(value => value.toLocaleLowerCase().includes(search)));
  const families = ['all', ...new Set(xsxbCatalog.map(item => item.family))];

  function pauseMaterial() { playRequest.current++; audio.current?.pause(); setPlayingMaterial(''); }
  function chooseTab(value: LibraryTab) { if (value !== 'ai') pauseMaterial(); setTab(value); setQuery(''); setError(''); }
  function playbackFailed(cause?: unknown) {
    setPlayingMaterial('');
    setError(t('@yovoice.instruments.materialPlayFailed'));
    logEvent('instrument_play_failed', `material=${selectedMaterial}; mediaCode=${audio.current?.error?.code ?? 0}; ${cause instanceof Error ? cause.name : ''}`);
  }
  async function playMaterial(id: string, fileName: string) {
    const element = audio.current;
    if (!element) return;
    const request = ++playRequest.current;
    if (selectedMaterial === id && !element.paused) { element.pause(); setError(''); return; }
    setError('');
    try {
      // 页面状态不能证明媒体元素仍有地址；加载失败时也要允许同一素材重新加载。
      if (selectedMaterial !== id || !element.getAttribute('src') || element.error) {
        element.pause(); setPlayingMaterial('');
        const url = await mediaUrl('outputs', fileName);
        if (request !== playRequest.current || element !== audio.current) return;
        setSelectedMaterial(id); setAudioTime(0); setAudioDuration(0);
        element.src = url; element.load();
      }
      await element.play();
      if (request === playRequest.current) setPlayingMaterial(id);
    } catch (cause) { if (request === playRequest.current) playbackFailed(cause); }
  }
  // 播放器放在各子页面外，列表、空搜索和详情切换不会卸载它。
  const withPlayer = (content: ReactNode) => <><audio hidden ref={audio} preload="metadata"
    onPlay={() => setPlayingMaterial(selectedMaterial)} onEnded={() => setPlayingMaterial('')} onPause={() => setPlayingMaterial('')}
    onTimeUpdate={() => setAudioTime(audio.current?.currentTime ?? 0)} onLoadedMetadata={() => setAudioDuration(audio.current?.duration || 0)}
    onError={() => playbackFailed()} />{content}</>;
  async function searchOnline() {
    const term = query.trim();
    if (!term) return;
    setError(''); setSearching(true); setCandidates([]); setSearchedName(term); setConfirmedID('');
    try { setCandidates(await call<InstrumentCandidate[]>('instrument.search', { name: term })); }
    catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
    finally { setSearching(false); }
  }
  async function importCandidate(candidate: InstrumentCandidate) {
    setError(''); setImportingID(candidate.id);
    try { await call('instrument.candidate.import', { id: candidate.id, instrument: searchedName, articulation: 'normal', confirmedInstrument: confirmedID === candidate.id }); }
    catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
    finally { setImportingID(''); }
  }
  async function download(id: string) {
    setError('');
    try { await call('instrument.catalog.download', { id }); }
    catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
  }
  async function confirmSource(id: string) {
    setError('');
    try { await call('instrument.source.confirm', { id }); }
    catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
  }
  async function upload() {
    if (!file) return;
    setError(''); setBusy(true);
    try {
      const source = await importInstrumentFile(file, { sourceId: sourceId || undefined,
        name: name.trim() || file.name.replace(/\.[^.]+$/, ''), instrument: sourceId ? sources.find(item => item.id === sourceId)?.instrument ?? instrument.trim() : instrument.trim(),
        origin: sourceId ? sources.find(item => item.id === sourceId)?.origin ?? origin : origin,
        rootPitch: Number(rootPitch), articulation: articulation.trim(), velocityLow: 1, velocityHigh: 127 });
      setSourceId(source.id); setFile(null); setName(''); setImporting(false); setTab('mine');
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
    finally { setBusy(false); }
  }

  if (importing) return withPlayer(<VStack className="library-page instrument-import-page" gap={6}>
    <HStack className="library-heading" hAlign="between" vAlign="center" gap={3}><h1>{t('@yovoice.instruments.importTitle')}</h1>
      <Button size="sm" variant="secondary" icon={<ArrowLeft />} label={t('@yovoice.instruments.backLibrary')} onClick={() => { setImporting(false); setError(''); }} /></HStack>
    <VStack className="library-content" gap={5}>
      <SettingsGroup title={t('@yovoice.instruments.samples')} description={t('@yovoice.instruments.samplesHint')}>
        <VStack gap={3}>
          <Selector label={t('@yovoice.instruments.target')} value={sourceId} options={[{ value: '', label: t('@yovoice.instruments.newSource') }, ...sources.map(source => ({ value: source.id, label: source.name }))]} onChange={setSourceId} />
          {!sourceId ? <VStack gap={3}>
            <SegmentedControl size="sm" label={t('@yovoice.instruments.origin')} value={origin} onChange={value => setOrigin(value as 'real' | 'ai')}>
              <SegmentedControlItem value="real" label={t('@yovoice.instruments.real')} />
              <SegmentedControlItem value="ai" label={t('@yovoice.instruments.generated')} />
            </SegmentedControl>
            <HStack gap={3} wrap="wrap"><TextInput label={t('@yovoice.instruments.sourceName')} value={name} onChange={setName} /><TextInput label={t('@yovoice.instruments.instrumentName')} value={instrument} onChange={setInstrument} /></HStack>
          </VStack> : null}
          <HStack gap={3} wrap="wrap"><TextInput label={t('@yovoice.instruments.pitch')} value={rootPitch} onChange={setRootPitch} description={t('@yovoice.instruments.pitchHint')} /><TextInput label={t('@yovoice.instruments.articulation')} value={articulation} onChange={setArticulation} /></HStack>
          <FileInput label={t('@yovoice.instruments.sampleImport')} value={file} onChange={value => setFile(Array.isArray(value) ? value[0] ?? null : value)} accept=".wav,.flac,.mp3,.ogg,.aiff,.m4a" maxSize={20 * 1024 * 1024} description={t('@yovoice.instruments.sampleImportHint')} isDisabled={!isDesktop || busy} disabledMessage={!isDesktop ? t('@yovoice.instruments.desktopOnly') : undefined} />
          <Button size="sm" variant="primary" label={t('@yovoice.instruments.addSample')} isLoading={busy} isDisabled={!file || busy || !/^\d+$/.test(rootPitch) || Number(rootPitch) > 127 || !articulation.trim() || (!sourceId && !instrument.trim())} onClick={() => void upload()} />
          {error ? <p role="alert">{error}</p> : null}
        </VStack>
      </SettingsGroup>
    </VStack>
  </VStack>);

  if (detail) {
    const catalogItem = xsxbCatalog.find(item => item.id === (detail.catalogID ?? sources.find(source => source.id === detail.sourceID)?.catalogId));
    const source = detail.sourceID ? sources.find(item => item.id === detail.sourceID) : sources.find(item => item.catalogId === detail.catalogID);
    return withPlayer(<InstrumentDetail source={source} catalog={catalogItem} downloading={downloading}
      downloadReceived={state.modelActivity?.received ?? 0} downloadTotal={state.modelActivity?.total ?? 0}
      downloadUnit={state.modelActivity?.params?.unit}
      downloadFailed={state.modelActivity?.kind === 'instrumentDownload' && state.modelActivity.status === 'failed'} downloadError={error}
      onBack={() => setDetail(null)} onDownload={() => { if (catalogItem) void download(catalogItem.id); }} />);
  }
  const materialDetail = (state.arrangementMaterials ?? []).find(item => item.id === materialDetailID);
  if (materialDetail) return withPlayer(<VStack className="library-page instrument-detail-page" gap={6}>
    <HStack className="library-heading" hAlign="between" vAlign="center" wrap="wrap" gap={3}>
      <VStack gap={1}><h1>{materialDetail.instrument} · {materialDetail.phrase}</h1><small>{t('@yovoice.instruments.generated')} · {materialDetail.modelId}</small></VStack>
      <Button size="sm" variant="secondary" icon={<ArrowLeft />} label={t('@yovoice.instruments.backLibrary')} onClick={() => setMaterialDetailID('')} />
    </HStack>
    <VStack className="library-content" gap={5}><SettingsGroup title={t('@yovoice.instruments.aiMaterialDetails')}>
      <HStack className="settings-row" hAlign="between" wrap="wrap" gap={3}><VStack gap={1}><b>{materialDetail.technique || materialDetail.role}</b><small>{materialDetail.tempo} BPM · {materialDetail.key || '—'} · {materialDetail.duration.toFixed(1)}s</small></VStack>
        <Button size="sm" variant="secondary" icon={playingMaterial === materialDetail.id ? <Pause size={15} /> : <Play size={15} />}
          label={t(playingMaterial === materialDetail.id ? '@yovoice.instruments.pauseSample' : '@yovoice.instruments.playSample')}
          isDisabled={!isDesktop} onClick={() => void playMaterial(materialDetail.id, materialDetail.fileName)} /></HStack>
      {selectedMaterial === materialDetail.id ? <AudioSeek time={audioTime} duration={audioDuration || materialDetail.duration} onSeek={seconds => { if (audio.current) audio.current.currentTime = seconds; setAudioTime(seconds); }} /> : null}
      <VStack className="settings-row" gap={2}><b>{t('@yovoice.instruments.aiPrompt')}</b><small>{materialDetail.prompt}</small></VStack>
      <StorageLocation path={localMediaPath(storage.root, 'outputs', materialDetail.fileName)} error={storage.error} />
      <VStack className="settings-row" gap={2}>
        <small>{t(materialDetail.condition === 'score_conditioned' ? '@yovoice.arrangement.scoreConditioned' : '@yovoice.arrangement.freeAudio')}</small>
        {materialDetail.alignmentWarning ? <small>{materialDetail.alignmentWarning}</small> : null}
        <Button size="sm" variant="secondary" label={t(materialDetail.reviewStatus === 'confirmed' ? '@yovoice.arrangement.reviewConfirmed' : '@yovoice.arrangement.confirmAudition')} isDisabled={!isDesktop || materialDetail.reviewStatus === 'confirmed'} onClick={() => void call('arrangement.material.review', { id: materialDetail.id, confirmed: true }).catch(cause => setError(formatCallError(t, cause)))} />
      </VStack>
      <small>{t('@yovoice.instruments.aiGeneratedAt', { date: new Date(materialDetail.createdAt).toLocaleString() })}</small>
    </SettingsGroup>{error ? <p role="alert">{error}</p> : null}</VStack>
  </VStack>);

  const controls = <VStack gap={2}>
    <TabList value={tab} onChange={value => chooseTab(value as LibraryTab)} role="tablist" hasDivider>
      <Tab value="mine" label={t('@yovoice.instruments.tabMine')} panelId="instrument-library-content" />
      <Tab value="catalog" label={t('@yovoice.instruments.tabCatalog')} panelId="instrument-library-content" />
      <Tab value="online" label={t('@yovoice.instruments.tabOnline')} panelId="instrument-library-content" />
      <Tab value="sites" label={t('@yovoice.instruments.tabSites')} panelId="instrument-library-content" />
      <Tab value="ai" label={t('@yovoice.instruments.tabAI')} panelId="instrument-library-content" />
      <Tab value="bands" label={t('@yovoice.instruments.tabBands')} panelId="instrument-library-content" />
    </TabList>
    {tab === 'catalog' ? <Selector size="sm" label={t('@yovoice.instruments.family')} value={family} options={families.map(value => ({ value, label: t(`@yovoice.instruments.family.${value}`) }))} onChange={setFamily} /> : null}
    {tab === 'catalog' ? <small>{t('@yovoice.instruments.catalogSummary')}</small> : null}
    {tab === 'online' ? <small>{t('@yovoice.instruments.searchHint')}</small> : null}
    {tab === 'sites' ? <small>{t('@yovoice.instruments.sitesHint')}</small> : null}
    {tab === 'ai' ? <small>{t('@yovoice.instruments.aiHint')}</small> : null}
    {tab === 'ai' ? <SegmentedControl size="sm" label={t('@yovoice.instruments.tabAI')} value={aiCategory} onChange={value => { pauseMaterial(); setAICategory(value); setQuery(''); setError(''); }}>
      <SegmentedControlItem value="materials" label={t('@yovoice.instruments.generatedMaterials')} />
      <SegmentedControlItem value="models" label={t('@yovoice.instruments.aiModels')} />
    </SegmentedControl> : null}
    {downloading ? <InstrumentDownloadProgress received={state.modelActivity?.received ?? 0} total={state.modelActivity?.total ?? 0} unit={state.modelActivity?.params?.unit} /> : null}
    {tab === 'ai' && aiCategory === 'materials' && selectedMaterial ? <AudioSeek time={audioTime} duration={audioDuration || (state.arrangementMaterials ?? []).find(item => item.id === selectedMaterial)?.duration || 0}
      onSeek={seconds => { if (audio.current) audio.current.currentTime = seconds; setAudioTime(seconds); }} /> : null}
  </VStack>;
  const hasResults = tab === 'mine' ? filteredSources.length > 0 : tab === 'catalog' ? filteredCatalog.length > 0 : tab === 'online' ? filteredCandidates.length > 0 : tab === 'sites' ? filteredSites.length > 0 : tab === 'bands' ? filteredBands.length > 0 : aiCategory === 'models' ? filteredModels.length > 0 : filteredMaterials.length > 0;
  return withPlayer(<LibraryPage title={t('@yovoice.instruments.title')} actions={<HStack gap={2} vAlign="center">
      {tab === 'online' ? <Button size="sm" variant="secondary" icon={<Search />} label={t('@yovoice.instruments.searchAction')} isLoading={searching} isDisabled={!isDesktop || !query.trim()} onClick={() => void searchOnline()} /> : null}
      <Button size="sm" variant="secondary" icon={<Plus />} label={t('@yovoice.instruments.importTitle')} onClick={() => { pauseMaterial(); setImporting(true); setError(''); }} />
    </HStack>} controls={controls} query={query} onQueryChange={setQuery}
    searchLabel={t(tab === 'online' ? '@yovoice.instruments.searchInstrument' : '@yovoice.instruments.searchLibrary')}
    hasItems hasResults={hasResults} noResults={error || t(tab === 'online' && !searchedName ? '@yovoice.instruments.searchPrompt' : '@yovoice.instruments.noResults')}
    empty={<LibraryEmpty icon={<Music2 />} title={t('@yovoice.instruments.noResults')} action={null} />} layout="tickets">
    {error ? <p role="alert">{error}</p> : null}
    {tab === 'mine' ? filteredSources.map((source, index) => <Ticket key={source.id} index={index} className="instrument-entry" cover={<Music2 />}
      eyebrow={`${source.provenance === 'synthesized' ? t('@yovoice.plugins.synthesized') : source.origin === 'ai' ? t('@yovoice.instruments.generated') : t('@yovoice.instruments.real')} · ${source.verification === 'metadata' ? t('@yovoice.instruments.metadataReview') : source.license || t('@yovoice.instruments.licenseUnknown')}`}
      title={<h3>{source.name}</h3>} excerpt={source.instrument}
      actions={<HStack gap={2} vAlign="center" wrap="wrap"><Button size="sm" variant="ghost" icon={<ListMusic size={15} />} label={t('@yovoice.instruments.viewSamples')} onClick={() => { pauseMaterial(); setDetail({ sourceID: source.id }); }} />{source.sourceUrl?.startsWith('https://') ? <Button size="sm" variant="ghost" icon={<ArrowUpRight size={15} />} label={t('@yovoice.instruments.sourcePage')} onClick={() => window.open(source.sourceUrl, '_blank', 'noopener')} /> : null}{source.verification === 'metadata' ? <Button size="sm" variant="secondary" label={t('@yovoice.instruments.confirmFoundSource')} onClick={() => void confirmSource(source.id)} /> : <StatusDot variant={source.health === 'missing' || source.health === 'corrupt' ? 'warning' : 'success'} label={t(source.health === 'missing' || source.health === 'corrupt' ? '@yovoice.arrangement.sourceNeedsRepair' : '@yovoice.instruments.installed')} />}</HStack>}
      stub={<TicketStamp value={source.samples.length} unit={t('@yovoice.instruments.sampleCount')} />} />) : null}
    {tab === 'catalog' ? filteredCatalog.map((item, index) => <Ticket key={item.id} index={index} className="instrument-entry" cover={<Music2 />}
      eyebrow={`CC0 · ${t(`@yovoice.instruments.family.${item.family}`)}`}
      title={<h3>{item.name}</h3>} excerpt={item.description}
      actions={<HStack gap={2} vAlign="center" wrap="wrap"><Button size="sm" variant="ghost" icon={<ListMusic size={15} />} label={t('@yovoice.instruments.viewSamples')} onClick={() => { pauseMaterial(); setDetail({ catalogID: item.id }); }} />{installedIDs.has(item.id) ? <StatusDot variant="success" label={t('@yovoice.instruments.installed')} /> : item.downloadable ?
        <Button size="sm" variant="secondary" icon={<Download size={15} />} label={t('@yovoice.instruments.download')} isDisabled={!isDesktop || downloading} onClick={() => void download(item.id)} /> :
        <StatusDot variant="neutral" label={t('@yovoice.instruments.phraseOnly')} />}</HStack>}
      stub={<TicketStamp value={item.sampleCount} unit={t('@yovoice.instruments.sampleCount')} />} />) : null}
    {tab === 'online' ? filteredCandidates.map((candidate, index) => <Ticket key={candidate.id} index={index} className="instrument-entry" cover={<AudioLines />}
      eyebrow={`${candidate.license} · ${candidate.creator || candidate.provider} · ${candidate.duration.toFixed(1)}s`}
      title={<h3>{candidate.title}</h3>} excerpt={t(candidate.kind === 'noteCandidate' ? '@yovoice.instruments.noteCandidate' : '@yovoice.instruments.phraseCandidate')}
      actions={<HStack gap={2} vAlign="center" wrap="wrap">
        <Button size="sm" variant="ghost" icon={<ArrowUpRight size={15} />} label={t('@yovoice.instruments.sourcePage')} onClick={() => window.open(candidate.landingUrl, '_blank', 'noopener')} />
        {candidate.kind === 'noteCandidate' ? <CheckboxInput size="sm" label={t('@yovoice.instruments.confirmInstrument', { instrument: searchedName })} value={confirmedID === candidate.id} onChange={checked => setConfirmedID(checked ? candidate.id : '')} /> : null}
        <Button size="sm" variant="secondary" label={t('@yovoice.instruments.verifyImport')} isLoading={importingID === candidate.id} isDisabled={!!importingID || candidate.kind !== 'noteCandidate' || confirmedID !== candidate.id} onClick={() => void importCandidate(candidate)} />
      </HStack>} stub={<TicketStamp value={candidate.duration.toFixed(0)} unit="s" />} />) : null}
    {tab === 'sites' ? filteredSites.map((site, index) => <Ticket key={site.id} index={index} className="instrument-entry" cover={<Music2 />}
      eyebrow={t(`@yovoice.instruments.${site.license}`)} title={<h3>{site.name}</h3>}
      excerpt={t(`@yovoice.instruments.${site.description}`)}
      actions={<HStack gap={2} wrap="wrap"><Button size="sm" variant="ghost" icon={<ArrowUpRight size={15} />} label={t('@yovoice.instruments.sourcePage')}
        onClick={() => window.open(site.url, '_blank', 'noopener')} />
        {['decent', 'labs', 'dsk', 'kong'].includes(site.id) ? <Button size="sm" variant="secondary" label={t('@yovoice.instruments.managePlugins')}
          onClick={() => openSettings('plugins')} /> : <Button size="sm" variant="secondary" icon={<Plus size={15} />} label={t('@yovoice.instruments.importAfterDownload')}
          onClick={() => { pauseMaterial(); setImporting(true); setError(''); }} />}</HStack>}
      stub={<TicketStamp value="WEB" unit={t('@yovoice.instruments.site')} />} />) : null}
    {tab === 'ai' && aiCategory === 'materials' ? filteredMaterials.map((material, index) => <Ticket key={material.id} index={index} className="instrument-entry" cover={<AudioLines />}
      eyebrow={`${t('@yovoice.instruments.generated')} · ${material.modelId}`}
      title={<h3>{material.instrument} · {material.technique || material.role}</h3>}
      excerpt={`${state.drafts.find(draft => draft.id === material.projectId)?.title ?? t('@yovoice.arrangement.project')} · ${material.phrase ?? t('@yovoice.arrangement.project')} · ${material.tempo ?? 0} BPM · ${material.duration.toFixed(1)}s`}
      actions={<HStack gap={2} wrap="wrap"><Button size="sm" variant="ghost" icon={<ListMusic size={15} />} label={t('@yovoice.instruments.viewDetails')} onClick={() => setMaterialDetailID(material.id)} />
        <Button size="sm" variant="secondary" icon={playingMaterial === material.id ? <Pause size={15} /> : <Play size={15} />}
          label={t(playingMaterial === material.id ? '@yovoice.instruments.pauseSample' : '@yovoice.instruments.playSample')}
          isDisabled={!isDesktop} onClick={() => void playMaterial(material.id, material.fileName)} /></HStack>}
      stub={<TicketStamp value={material.duration.toFixed(0)} unit="s" />} />) : null}
    {tab === 'ai' && aiCategory === 'models' ? filteredModels.map((model, index) => <Ticket key={model.id} index={index + filteredMaterials.length} className="instrument-entry" cover={<Music2 />}
      eyebrow={model.family === 'soundfont' ? t('@yovoice.instruments.soundfonts') : t('@yovoice.instruments.aiModels')}
      title={<h3>{model.name}</h3>} excerpt={`${model.precision} · ${model.version}`}
      actions={<HStack gap={2} vAlign="center"><StatusDot variant={state.models.some(item => item.id === model.id) ? 'success' : 'neutral'} label={state.models.some(item => item.id === model.id) ? t('@yovoice.instruments.installed') : t('@yovoice.instruments.available')} />
        <Button size="sm" variant="secondary" icon={<ArrowUpRight size={15} />} label={t('@yovoice.instruments.manage')} onClick={() => openSettings(model.family === 'soundfont' ? 'soundfonts' : 'models')} /></HStack>}
      stub={<TicketStamp value="AI" unit={t('@yovoice.instruments.model')} />} />) : null}
    {tab === 'bands' ? filteredBands.map((band, index) => <Ticket key={band.id} index={index} className="instrument-entry" cover={<Music2 />}
      eyebrow={t('@yovoice.instruments.bandPreset')}
      title={<h3>{band.name}</h3>}
      excerpt={band.parts.map(part => `${part.instrument} · ${part.source === 'ai' ? 'AI' : t('@yovoice.instruments.real')}`).join(' / ')}
      actions={<StatusDot variant="success" label={t('@yovoice.instruments.saved')} />}
      stub={<TicketStamp value={band.parts.length} unit={t('@yovoice.instruments.instruments')} />} />) : null}
  </LibraryPage>);
}
