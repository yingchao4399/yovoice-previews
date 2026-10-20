import { useState } from 'react';
import { VStack } from '@astryxdesign/core/Layout';
import { useTranslator } from '@astryxdesign/core/i18n';
import { Selector } from '../selector';
import { formatTime } from '../workbench';

const presets = [0, 30, 60, 90, 120, 180, 300, 480, 600];

// 共用创作目标；超出模型能力时说明原因，不静默截断。
export function DurationControl({ value, change, disabled, max = 600 }: { value: number; change: (seconds: number) => void; disabled?: boolean; max?: number }) {
  const t = useTranslator();
  const [custom, setCustom] = useState(false);
  const numeric = custom || !presets.includes(value);
  return <VStack gap={2} className="duration-control">
    <Selector size="sm" label={t('@yovoice.arrangement.duration')} isLabelHidden value={numeric ? 'custom' : String(value)} isDisabled={disabled}
      options={[...presets.map(seconds => ({ value: String(seconds), label: seconds ? `${formatTime(seconds)}${seconds > max ? ` · ${t('@yovoice.music.durationUnsupported')}` : ''}` : t('@yovoice.music.durationAuto'), disabled: seconds > max })), { value: 'custom', label: t('@yovoice.music.durationCustom') }]}
      onChange={next => { setCustom(next === 'custom'); if (next !== 'custom') change(Number(next)); else if (!value) change(90); }} />
    {numeric ? <label className="number-field">{t('@yovoice.music.durationSeconds')}<input type="number" min={15} max={max} step={1} value={value || ''} disabled={disabled} onChange={event => { if (event.target.value) change(Number(event.target.value)); }} /></label> : null}
  </VStack>;
}
