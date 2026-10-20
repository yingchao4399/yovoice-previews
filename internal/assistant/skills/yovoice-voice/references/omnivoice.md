# OmniVoice (omnivoice-q8 / -bf16 / -f16)

Apply with `update_work` {"id","settings":{...}}; only given fields change.

## Voice source: `voiceMode`
- `design` (default): the voice comes from `voiceDescription`, a comma-separated list of attributes, at most one per group (a repeated or unknown attribute is rejected):
  - gender: male, female
  - age: child, teenager, young adult, middle-aged, elderly
  - pitch: very low pitch, low pitch, moderate pitch, high pitch, very high pitch
  - delivery: whisper
  - accent (English): american accent, british accent, australian accent, chinese accent, canadian accent, indian accent, korean accent, portuguese accent, russian accent, japanese accent
  - dialect (Chinese): 河南话, 陕西话, 四川话, 贵州话, 云南话, 桂林话, 济南话, 石家庄话, 甘肃话, 宁夏话, 青岛话, 东北话
  Chinese names of the English attributes are accepted too (男, 女, 青年, 低音调 …). Example: "female, young adult, high pitch".
- `clone`: needs `voiceId` and `referenceText` (exact transcript of the reference audio); the description is ignored.

## Plain parameters
- `synthesisLanguage`: `auto` (default) or a language code/name up to 32 chars.
- `omniSpeed` 0.5-2 (0 = default). The generic `speed`, `temperature`, `emotion*` fields are NOT used by this model.
- `seed` 0-2147483647 (-1 = random).

## Advanced (`modelOptions` {"omnivoice":{...}})
- `text_chunk_size` 0-12000 (0 auto), `text_chunk_mode` default/tag_aware/japanese/endline.
- `num_inference_steps` 1-100 (32): more steps = finer but slower. `guidance_scale` 0.5-5 (2).
- `audio_chunk_duration` 1-60 s (15), `audio_chunk_threshold` 1-120 s (30): long reference audio is cut into chunks.
- `duration` 0-300 s (0 = automatic): force the total length.
- `denoise`, `preprocess_prompt`, `postprocess_output` (all true by default).
- `t_shift` 0.01-2 (.1), `layer_penalty_factor` 0-20 (5), `position_temperature` 0-20 (5), `class_temperature` 0-5 (0): sampler shaping; leave alone unless the user asks.
