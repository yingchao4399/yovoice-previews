import type { UiLocale } from '../../shared/workbench';

const major = [
  ['C', '0'], ['G', '1♯'], ['D', '2♯'], ['A', '3♯'], ['E', '4♯'], ['B', '5♯'],
  ['F♯', '6♯'], ['F', '1♭'], ['B♭', '2♭'], ['E♭', '3♭'], ['A♭', '4♭'], ['D♭', '5♭'],
] as const;

const minor = [
  ['A', '0'], ['E', '1♯'], ['B', '2♯'], ['F♯', '3♯'], ['C♯', '4♯'], ['G♯', '5♯'],
  ['D', '1♭'], ['G', '2♭'], ['C', '3♭'], ['F', '4♭'], ['B♭', '5♭'], ['E♭', '6♭'],
] as const;

export function scoreKeyOptions(locale: UiLocale) {
  const zh = locale === 'zh-CN';
  const options = (keys: typeof major | typeof minor, mode: 'major' | 'minor') => keys.map(([root, signature]) => ({
    value: zh ? `${root} ${mode === 'major' ? '大调' : '小调'}` : `${root} ${mode}`,
    label: zh ? `${root} ${mode === 'major' ? '大调' : '小调'} · ${signature === '0' ? '无升降号' : `${signature.slice(0, -1)} 个${signature.endsWith('♯') ? '升号' : '降号'}`}` : `${root} ${mode} · ${signature === '0' ? 'no sharps or flats' : `${signature.slice(0, -1)} ${signature.endsWith('♯') ? 'sharps' : 'flats'}`}`,
  }));
  return [
    { type: 'section' as const, title: zh ? '大调' : 'Major', options: options(major, 'major') },
    { type: 'section' as const, title: zh ? '小调' : 'Minor', options: options(minor, 'minor') },
    { type: 'section' as const, title: zh ? '民乐五声调式' : 'Chinese pentatonic modes', options: [
      ['宫', 'Gong'], ['商', 'Shang'], ['角', 'Jue'], ['徵', 'Zhi'], ['羽', 'Yu'],
    ].map(([name, romanized]) => ({ value: zh ? `${name}调式` : `${romanized} mode`, label: zh ? `${name}调式` : `${romanized} mode` })) },
  ];
}
