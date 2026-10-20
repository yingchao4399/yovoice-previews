---
name: yovoice-workflows
description: Step-by-step tool sequences for common yovoice tasks — narration, voice settings and saved characters, multi-role story dubbing, writing and generating a whole song, scoring an underscore. Use whenever a task spans creating or changing works and generating audio.
---

# yovoice workflows

Work types: `text` 语音 (narration / voice-over), `story` 故事配音 (multi-speaker subtitles), `music` 音乐 (AI song generation), `score` 编曲 (arrangement rendering). Every workflow starts from the <ui_context> of the user message and uses the same pattern: read, change, then act.

## Narration (配一段旁白)

1. `get_work` on the open text work, or `create_work` {"kind":"text","title":...} for a new one.
2. `update_work` {"id","text"} with the narration; optionally `update_work` {"id","modelId"} to pick an installed voice model from `list_models`.
3. `generate_work` {"id"}; reply that generation started, then check `get_generation_status` {"id"} when the user asks or before claiming success.

## Voice settings and characters (声音设置与角色)

Everything in the right-hand inspector is editable with `update_work` {"id","settings":{...}}. Fields and ranges differ per model: load the yovoice-voice skill only when the user asks for such a change.
- Save a voice as a reusable character: `save_character` {"name","fromWork":<work id>} copies the work's current voice settings; add "settings" to tweak. No need to create a work for this, and it does not switch the page.
- Use one: `list_characters`, then `update_work` {"id","characterId"}; for a story speaker `update_work` {"id","speakerSettings":[{"speaker","characterId"}]}.
- Changing one speaker's parameters only: `speakerSettings` [{"speaker","settings":{...}}].
- Deleting a character goes through `propose_action`.

## Multi-role story dubbing (多角色故事配音)

1. `create_work` {"kind":"story","title":...}, or `get_work` an existing story.
2. `list_voices` for uploaded reference voices. Assign one voice per character when the user wants specific voices.
3. `update_work` {"id","subtitles":{"speakers":[{"name":..., "voiceId":...}],"cues":[{"speaker":...,"text":...}]}} — speaker matches by name or 1-based index; every cue needs a speaker.
4. `generate_work` when every speaker's model is installed. Generation runs cue by cue; `get_generation_status` reports progress.

## Write a song and generate it (写一首歌并生成)

1. `create_work` {"kind":"music","title":...}.
2. Follow the yovoice-lyrics skill, then apply with `update_work` {"id","text":<style>,"lyrics":...,"instrumental":false,"language":"zh"}.
3. `list_models`: an installed `ace-step-*` or `yue2-*` model is required. If missing, propose installing it with `propose_action` {"action":"install_model","target":<model id>}.
4. `update_work` {"id","modelId":...} then `generate_work` (for YuE2 in score mode, after step 5). Music takes longer than voice; say so and let the user check later.
5. With a `yue2-*` model in score mode, write the score first and let the user review and listen to it before generating; melody scores, covers, instrumentals and musical edits follow the yovoice-music skill.

## Underscore / arrangement (给作品配乐)

1. `create_work` {"kind":"score","title":...}.
2. Follow the yovoice-score skill, then apply with `update_work` {"id","score":{...}}. Validation errors come back verbatim; fix exactly the reported note or track and submit again.
3. Scores render with a sound font (`*-sf2`): confirm one is installed via `list_models`, then `update_work` {"id","modelId":"musescore-general-sf2"} and `generate_work`. Rendering needs no inference engine.

## Rules that always apply

- One intent per turn; after finishing, answer in one or two sentences in the user's language.
- `generate_work` returns immediately. Never claim audio exists until `get_generation_status` shows a completed `latest`.
- Modifying a work the user has open? Just update it — the editor refreshes by itself (`open_work` is only for switching to another work).
- Unknown id or voice: `list_works` / `list_voices` first instead of guessing.
