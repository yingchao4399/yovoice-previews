import { ProgressBar } from '@astryxdesign/core/ProgressBar';
import { useTranslator } from '@astryxdesign/core/i18n';

export function InstrumentDownloadProgress({ received, total, unit }: { received: number; total: number; unit?: unknown }) {
  const t = useTranslator();
  const label = unit === 'bytes'
    ? t('@yovoice.instruments.downloadingBytes', { done: (received / 1048576).toFixed(1), total: (total / 1048576).toFixed(1) })
    : t('@yovoice.instruments.downloading', { done: received, total });
  return <ProgressBar label={label} value={received} max={total || 1} isIndeterminate={!total} />;
}
