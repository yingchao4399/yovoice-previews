# YuE2 native ABC dialect

YuE2 sings lyrics over a melody score written in a strict native ABC dialect, stored in the music work's `abc` field. Call `get_work` first: `settings.abc` is the current score, `text` the style, `settings.lyrics` the lyrics. Only music works whose modelId starts with `yue2-` use it.

## Native dialect, all required

- Header lines exactly in this order: `X:1` / `T:` (blank) / `M:<n>/<d>` / `L:1/<power of two>` (keep the source value, usually 1/32) / `Q:1/4=<integer BPM>` / `V: Vocal clef=treble name="Vocal Melody" snm="Vocal"` / `V: Ins clef=treble name="Ins Melody" snm="Inst."` / `K:<major or minor key such as G, Bb, F#m>`.
- Music is written in groups of 1 to 4 measures: a `V: Vocal` line and its music line, then a `V: Ins` line and its music line with the same number of measures. Each music line ends with `|`.
- Sections are comment lines with one `%` and a space before a group: `% intro`, `% verse`, `% pre-chorus`, `% chorus`, `% bridge`, `% interlude`, `% outro`, `% instrumental`. Do not repeat the same section comment twice in a row.
- Both voices are monophonic melodies. Ins carries instrumental themes or solos and rests while the vocal sings; it is not a chord staff.
- Chord symbols are quoted and only in Vocal, also while Vocal rests: `"C"`, `"Am7"`, `"F#m7/C#"`. Allowed qualities: major (no suffix), m, dim, aug, 7, maj7, m7, dim7, m7b5, sus4, sus2, 6, m6, 7sus4, m(maj7); optional /bass.
- With L:1/32 a 4/4 measure holds exactly 32 units (3/4 and 6/8 hold 24). Duration multipliers are only 1,2,3,4,6,8,12,16,24,32,48; write other lengths as tied notes like `C8-C2`. `z` is a rest; `Z`, `Z2`, `Z3`, `Z4` are whole resting measures.
- `c` is an octave above `C`, `c'` above `c`, `C,` below `C`. `^` sharp, `_` flat, `=` natural; accidentals last to the barline and apply to the same letter in every octave. The key signature applies to unmarked notes.
- A tie (`-`) joins equal pitches; a chord change inside a held note splits it with a tie: `"C"E16-"Am7"E16`. A tie never enters a rest and the score never ends on a tie.
- A meter or key change starts a new group and appears as matching `M:` / `K:` lines right after both `V:` lines of that group; a key change inside a group is an inline `[K:G]` at the same moment in both voices.
- An unmarked note tied across a barline keeps the previous bar's pitch; the notes after it use the new bar's accidentals. Adjacent whole-bar rests inside a group are compressed: `Z2`, not `Z|Z`.
- Count notes as sounding events after joining ties: `C8-C8` is one half note, `C8C8` two attacks.
- Not allowed: lyric `w:` lines, tuplets, grace notes, bracketed note stacks, slurs, repeat signs, decorations, `%%%` directives, extra voices.

## How to write or edit

- With no score yet: one section comment per lyric section, a singable vocal line with chords, short instrumental themes in Ins between vocal phrases.
- When editing, change only what the request asks for and keep everything else note for note: other sections, the other voice, chords, tempo, key and meter. Keep section comments and bar grouping.
- Keep vocal phrases singable for the lyrics: roughly one note per syllable, held notes on long vowels, rests where lines breathe.
- Check that every measure adds up to the meter before applying.
- When the request changes the arrangement feel (genre, instruments, tempo, vocal character), also update the style (`text`) in English tags.

Apply with one `update_work` {"id", "abc": <the complete score>} (plus "text" when the style changes). If it is rejected, the error names the measure and the problem: fix exactly that and update again. Tell the user in one sentence what changed.
