import { useState, type ReactNode } from 'react';
import { ResizeHandle, type ResizableProps } from '@astryxdesign/core/Resizable';
import { Button } from '@astryxdesign/core/Button';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { useTranslator } from '@astryxdesign/core/i18n';
import { AudioLines, ChevronRight, FolderOpen, ListMusic, Mic, Music, Pencil, Settings2, Trash2 } from 'lucide-react';
import { projectKind, type Draft, type State } from '../shared/workbench';
import { overflowTitle } from '../shared/ui/overflow-title';
import { UpdateSidebarCard } from '../features/update/update-notice';

// 侧栏：保留官方音乐生成入口，智能编曲和音源库作为独立工作流入口。
export function SidebarNav(props: {
  page: string;
  setPage: (id: string) => void;
  newDraft: ReactNode;
  state: State;
  draft: Draft;
  selectDraft: (item: Draft) => void;
  removeDraft: (item: Draft) => void;
  resizable: ResizableProps;
}) {
  const t = useTranslator();
  const { page, setPage, newDraft, state, draft, selectDraft, removeDraft, resizable } = props;
  const [recentOpen, setRecentOpen] = useState(() => localStorage.getItem('yovoice-recent-open') !== 'false');
  const projectActive = page === 'create' || page === 'history';
  return <VStack as="nav" className="sidebar" aria-label={t('@yovoice.nav.main')} gap={6} data-testid="sidebar">
    {newDraft}
    <VStack className="sidebar-destinations" gap={2}>
      {(['text', 'story', 'music'] as const).map(kind => {
        // 编曲生成并入音乐生成：两类作品都高亮「音乐生成」。
        const group = (k: string) => k === 'score' ? 'music' : k;
        const selected = group(page) === kind || (projectActive && group(projectKind(draft)) === kind);
        return <Button key={kind} data-testid={`nav-${kind}`} label={t(`@yovoice.project.${kind}`)} variant="ghost" icon={kind === 'text' ? <Mic /> : kind === 'story' ? <Pencil /> : <Music />} className={`nav-item ${selected ? 'selected' : ''}`} aria-current={selected ? 'page' : undefined} onClick={() => setPage(kind)} />;
      })}
      <Button data-testid="nav-arrangement" label={t('@yovoice.arrangement.title')} variant="ghost" icon={<ListMusic />} className={`nav-item ${page === 'arrangement' ? 'selected' : ''}`} aria-current={page === 'arrangement' ? 'page' : undefined} onClick={() => setPage('arrangement')} />
      <Button data-testid="nav-characters" label={t('@yovoice.nav.characters')} variant="ghost" icon={<AudioLines />} className={`nav-item ${page === 'characters' || page === 'voices' ? 'selected' : ''}`} aria-current={page === 'characters' || page === 'voices' ? 'page' : undefined} onClick={() => setPage('characters')} />
      <Button data-testid="nav-instruments" label={t('@yovoice.instruments.title')} variant="ghost" icon={<FolderOpen />} className={`nav-item ${page === 'instruments' ? 'selected' : ''}`} aria-current={page === 'instruments' ? 'page' : undefined} onClick={() => setPage('instruments')} />
    </VStack>
    <details className="recent-projects" open={recentOpen}>
      <summary onClick={event => { event.preventDefault(); const open = !recentOpen; setRecentOpen(open); localStorage.setItem('yovoice-recent-open', String(open)); }}><ChevronRight aria-hidden />{t('@yovoice.nav.recent')}</summary>
      <VStack className="project-list" gap={1}>{[...state.drafts].sort((a, b) => (b.updatedAt ?? '').localeCompare(a.updatedAt ?? '')).slice(0, 5).map(item => <HStack key={item.id} className={`project-row ${item.id === draft.id && page === 'create' ? 'current' : ''}`} gap={0}>
        <Button label={item.id === draft.id ? draft.title : item.title} variant="ghost" size="sm" className="project-link grow" onPointerEnter={overflowTitle} aria-current={item.id === draft.id && page === 'create' ? 'page' : undefined} onClick={() => selectDraft(item)} />
        <Button className="project-quick-delete" label={t('@yovoice.nav.deleteProject', { title: item.id === draft.id ? draft.title : item.title })} icon={<Trash2 />} isIconOnly size="sm" variant="ghost" onClick={() => removeDraft(item.id === draft.id ? draft : item)} />
      </HStack>)}</VStack>
    </details>
    <VStack className="sidebar-bottom" gap={3}><UpdateSidebarCard />
      <Button data-testid="nav-settings" label={t('@yovoice.nav.settings')} icon={<Settings2 size={18} />} variant="ghost" className={`nav-item ${page === 'settings' ? 'selected' : ''}`} onClick={() => setPage('settings')} /></VStack>
    <ResizeHandle label={t('@yovoice.nav.resize')} direction="horizontal" position="overlay" pillPlacement="center" resizable={resizable} isAlwaysVisible={false} />
  </VStack>;
}
