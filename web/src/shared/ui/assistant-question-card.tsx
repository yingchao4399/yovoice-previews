import { useState } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { TextArea } from '@astryxdesign/core/TextArea';
import { useTranslator } from '@astryxdesign/core/i18n';
import { Check, MessageCircleQuestion } from 'lucide-react';
import { call } from '../lib/client';
import { formatCallError } from '../i18n/format';
import type { AssistantQuestion } from '../workbench';

export function AssistantQuestionCard({ question: q, activateSession = false, onDeferred }: { question: AssistantQuestion; activateSession?: boolean; onDeferred?: () => void }) {
  const t = useTranslator();
  const [selected, setSelected] = useState<number[]>(q.selected ?? []);
  const [custom, setCustom] = useState(q.custom ?? '');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [open, setOpen] = useState(q.state !== 'deferred');
  const editable = q.state === 'pending' || q.state === 'deferred';
  const answer = async (action: 'submit' | 'cancel' | 'later') => {
    setBusy(true); setError('');
    try { if (activateSession) await call('assistant.open', { id: q.sessionId }); await call('assistant.answer', { id: q.id, sessionId: q.sessionId, action, selected, custom }); if (action === 'later') { setOpen(false); onDeferred?.(); } }
    catch (e) { setError(formatCallError(t, e)); }
    finally { setBusy(false); }
  };
  return <VStack className="assistant-proposal assistant-question" gap={3} role="group" aria-label={q.prompt} data-testid="assistant-question">
    <HStack gap={2} vAlign="center"><MessageCircleQuestion size={16} /><strong className="grow">{q.prompt}</strong></HStack>
    {!editable ? <small>{q.state === 'cancelled' ? t('@yovoice.assistant.question.cancelled') : [...(q.selected ?? []).map(i => q.options[i]), q.custom].filter(Boolean).join('；')}</small>
      : !open ? <Button size="sm" variant="secondary" label={t('@yovoice.assistant.question.resume')} onClick={() => setOpen(true)} /> : <>
        <small>{t(q.multiple ? '@yovoice.assistant.question.multiple' : '@yovoice.assistant.question.single')}</small>
        <VStack gap={2}>{q.options.map((option, index) => <Button key={index} size="sm" variant="secondary" className="assistant-question-option" label={option} icon={selected.includes(index) ? <Check /> : undefined} aria-pressed={selected.includes(index)} isDisabled={busy}
          onClick={() => setSelected(previous => q.multiple ? previous.includes(index) ? previous.filter(i => i !== index) : [...previous, index].sort((a, b) => a - b) : [index])} />)}</VStack>
        <TextArea label={t('@yovoice.assistant.question.custom')} value={custom} onChange={setCustom} maxLength={2000} isDisabled={busy} />
        <HStack gap={2} hAlign="end" wrap="wrap">
          <Button size="sm" variant="ghost" label={t('@yovoice.assistant.question.cancel')} isDisabled={busy} onClick={() => void answer('cancel')} />
          <Button size="sm" variant="ghost" label={t('@yovoice.assistant.question.later')} isDisabled={busy} onClick={() => void answer('later')} />
          <Button size="sm" variant="primary" label={t('@yovoice.assistant.question.submit')} isDisabled={busy || !selected.length && !custom.trim()} isLoading={busy} onClick={() => void answer('submit')} />
        </HStack>
      </>}
    {error ? <small role="alert" data-tone="danger">{error}</small> : null}
  </VStack>;
}
