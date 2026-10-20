import { useEffect, useRef, useState } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { VStack } from '@astryxdesign/core/Layout';
import { List, ListItem } from '@astryxdesign/core/List';
import { TextInput } from '@astryxdesign/core/TextInput';
import { useTranslator } from '@astryxdesign/core/i18n';
import { call, mediaUrl } from '../lib/client';
import { formatCallError } from '../i18n/format';
import { formatTime } from '../workbench';
import { Selector } from '../selector';
import type { ResourceItem, ResourcePage, ResourceQuery } from '../resource';
import { AppDialog } from './app-dialog';

// 只解析当前选中的文件；切换或关闭时停止播放并释放预览 Blob。
export function ResourceAudioPreview({ mediaKind, fileName }: { mediaKind: 'voices' | 'outputs' | 'instruments'; fileName: string }) {
  const t = useTranslator();
  const [url, setURL] = useState(''), [error, setError] = useState('');
  useEffect(() => {
    let active = true, resolved = '';
    setURL(''); setError('');
    void mediaUrl(mediaKind, fileName).then(value => {
      resolved = value;
      if (active) setURL(value);
      else if (value.startsWith('blob:')) URL.revokeObjectURL(value);
    }).catch(cause => { if (active) setError(formatCallError(t, cause)); });
    return () => { active = false; if (resolved.startsWith('blob:')) URL.revokeObjectURL(resolved); };
  }, [mediaKind, fileName, t]);
  return <VStack gap={1} className="resource-audio-preview">
    {url ? <audio key={url} controls preload="metadata" src={url} aria-label={t('@yovoice.resources.preview')} onError={() => setError(t('@yovoice.error.audioBlobMissing'))} /> : null}
    {error ? <small role="alert">{error}</small> : null}
  </VStack>;
}

export function ResourcePicker({ choose, close }: { choose: (item: ResourceItem) => Promise<void>; close: () => void }) {
  const t = useTranslator();
  const [query, setQuery] = useState(''), [kind, setKind] = useState('playable');
  const [parent, setParent] = useState<ResourceItem>();
  const [page, setPage] = useState<ResourcePage>({ items: [], total: 0, offset: 0 });
  const [selected, setSelected] = useState<ResourceItem>();
  const [loading, setLoading] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState('');
  const request = useRef(0), mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const load = async (input: ResourceQuery, append: boolean, seq: number) => {
    setLoading(true); setError('');
    try {
      const result = await call<ResourcePage>('resources.search', input);
      if (mounted.current && seq === request.current) setPage(previous => append ? { ...result, items: [...previous.items, ...result.items] } : result);
    } catch (cause) { if (mounted.current && seq === request.current) setError(formatCallError(t, cause)); }
    finally { if (mounted.current && seq === request.current) setLoading(false); }
  };
  useEffect(() => {
    const seq = ++request.current;
    setSelected(undefined); setPage({ items: [], total: 0, offset: 0 });
    const timer = setTimeout(() => { void load({ query, kind: parent ? 'sample' : kind, parentId: parent?.id, limit: 30 }, false, seq); }, 150);
    return () => clearTimeout(timer);
    // 查询条件变化使旧请求失效；翻页沿用当前序号。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query, kind, parent?.id]);
  const playable = selected?.status === 'ready' && !!selected.mediaKind && !!selected.fileName;
  const attach = async () => {
    if (!playable || !selected || busy) return;
    setBusy(true); setError('');
    try { await choose(selected); }
    catch (cause) { if (mounted.current) setError(formatCallError(t, cause)); }
    finally { if (mounted.current) setBusy(false); }
  };
  return <AppDialog title={t('@yovoice.resources.pick')} width={680} onClose={close} closeLabel={t('@yovoice.action.close')} busy={busy} error={error}
    actions={<><Button label={t('@yovoice.action.cancel')} isDisabled={busy} onClick={close} /><Button label={t('@yovoice.resources.use')} variant="primary" isDisabled={!playable || busy} isLoading={busy} onClick={() => void attach()} /></>}>
    <TextInput label={t('@yovoice.resources.search')} value={query} onChange={setQuery} />
    {parent ? <Button size="sm" variant="ghost" label={t('@yovoice.resources.back', { name: parent.name })} onClick={() => { setParent(undefined); setQuery(''); }} />
      : <Selector label={t('@yovoice.resources.category')} value={kind} options={['playable', 'audio', 'voice', 'instrument', 'sample'].map(value => ({ value, label: t(`@yovoice.resources.kind.${value === 'playable' ? 'all' : value}`) }))} onChange={setKind} />}
    <VStack gap={2} className="resource-picker-results" aria-busy={loading}>
      <List header={t('@yovoice.resources.results', { count: page.total })} density="compact" hasDividers>
        {page.items.filter(item => ['audio', 'voice', 'instrument', 'sample'].includes(item.kind)).map(item => <ListItem key={item.id} label={item.name}
          description={`${item.origin ? t(`@yovoice.resources.origin.${item.origin}`) : t('@yovoice.resources.kind.voice')} · ${item.duration ? formatTime(item.duration) : item.license || t('@yovoice.resources.kind.instrument')} · ${t(`@yovoice.resources.status.${item.status}`)}`}
          endContent={item.kind === 'instrument' ? <Button size="sm" label={t('@yovoice.resources.samples')} onClick={() => { setParent(item); setQuery(''); }} />
            : <Button size="sm" label={t('@yovoice.resources.audition', { name: item.name })} isDisabled={item.status !== 'ready'} aria-pressed={item.id === selected?.id} onClick={() => setSelected(item)} />} />)}
      </List>
      {!loading && !page.items.length ? <p className="muted">{t('@yovoice.resources.empty')}</p> : null}
      {page.nextOffset !== undefined ? <Button size="sm" label={t('@yovoice.resources.more')} isDisabled={loading} onClick={() => void load({ query, kind: parent ? 'sample' : kind, parentId: parent?.id, offset: page.nextOffset, limit: 30 }, true, request.current)} /> : null}
    </VStack>
    {selected?.mediaKind && selected.fileName ? <VStack gap={2}><strong>{selected.name}</strong><small>{selected.license || t('@yovoice.resources.licenseUnknown')}</small>
      <ResourceAudioPreview mediaKind={selected.mediaKind} fileName={selected.fileName} /></VStack> : null}
  </AppDialog>;
}
