import { HStack } from '@astryxdesign/core/Layout';
import { useTranslator } from '@astryxdesign/core/i18n';
import { formatTime } from '../../shared/workbench';

export function AudioSeek({ time, duration, onSeek }: { time: number; duration: number; onSeek: (seconds: number) => void }) {
  const t = useTranslator();
  const end = Number.isFinite(duration) && duration > 0 ? duration : 0;
  return <HStack gap={2} vAlign="center" className="instrument-seek">
    <small>{formatTime(time)}</small>
    <input className="preview-progress" type="range" min={0} max={end || 1} step={0.01} value={Math.min(time, end || 1)} disabled={!end}
      aria-label={t('@yovoice.player.progress')} aria-valuetext={t('@yovoice.player.progressValue', { current: formatTime(time), total: formatTime(end) })}
      onChange={event => onSeek(Number(event.target.value))} />
    <small>{formatTime(end)}</small>
  </HStack>;
}
