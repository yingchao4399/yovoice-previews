import { DropdownMenu } from '@astryxdesign/core/DropdownMenu';
import { useLocale, useTranslator } from '@astryxdesign/core/i18n';
import { Check, ChevronDown } from 'lucide-react';
import { formatTime, projectKind, type Draft, type Generation, type State } from '../../shared/workbench';

// 两个历史入口使用同一集合：快速语音的历史跨作品，其余版本只属于当前作品。
export function historyForProject(project: Draft, state: Pick<State, 'drafts' | 'history'>): Generation[] {
  const kind = projectKind(project);
  return state.history.filter(item => !item.segment && (kind === 'text'
    ? projectKind(state.drafts.find(d => d.id === item.settings.id) ?? item.settings) === 'text'
    : item.settings.id === project.id)).sort((a, b) => b.createdAt.localeCompare(a.createdAt));
}

export function HistoryVersionMenu({ items, selected, select }: { items: Generation[]; selected?: string; select: (item: Generation) => void }) {
  const t = useTranslator();
  const locale = useLocale();
  return <DropdownMenu presentation="popover" placement="above" alignment="end" hasChevron={false}
    button={{ size: 'sm', variant: 'ghost', isIconOnly: true, icon: <ChevronDown />, label: t('@yovoice.history.quickSwitch'), tooltip: t('@yovoice.history.quickSwitch'), isDisabled: items.length === 0 }}
    items={items.map((item, index) => ({ id: item.id, label: `${t('@yovoice.app.historyVersion', { n: items.length - index })} · ${item.title}`,
      description: `${formatTime(item.duration)} · ${new Date(item.createdAt).toLocaleString(locale)}${item.source === 'score' ? ` · ${t('@yovoice.music.historyScore')}` : ''}`,
      icon: selected === item.id ? <Check /> : undefined, onClick: () => select(item) }))} />;
}

const key = 'yovoice-history-selection';
export function rememberedVersion(projectId: string): string | undefined {
  try { return JSON.parse(localStorage.getItem(key) ?? '{}')[projectId]; } catch { return undefined; }
}
export function rememberVersion(projectId: string, versionId: string) {
  try {
    const entries = JSON.parse(localStorage.getItem(key) ?? '{}');
    delete entries[projectId];
    localStorage.setItem(key, JSON.stringify(Object.fromEntries([...Object.entries(entries), [projectId, versionId]].slice(-100))));
  } catch { /* 本地偏好不可写时仍允许切换，作品数据不受影响。 */ }
}
