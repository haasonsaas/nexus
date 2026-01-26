# Local TTS

## Summary
Local text-to-speech (TTS) to complete the “voice loop” (STT → LLM → TTS) for channels that support audio.

## Goals
- Provide a configurable TTS layer (`tts` config).
- Generate audio responses when the inbound message is a voice/audio message (or has transcribed `media_text`).
- Deliver audio attachments reliably even when text responses are streamed.

## Non-goals
- Wake word / always-on microphone loop.
- Neural voice training.
- Long-term storage/hosting of generated audio (current implementation uses temp files).

## Implementation (2026-01-26)

### Config
Added top-level `tts` configuration, backed by `internal/tts.Config` and surfaced in `nexus.example.yaml`.

Providers currently supported:
- `macos` (local `say`, outputs AIFF)
- `edge` (local `edge-tts` CLI, outputs MP3)
- `openai` (OpenAI `/audio/speech`)
- `elevenlabs` (ElevenLabs API)

Example:
```yaml
tts:
  enabled: true
  provider: macos
  fallback_chain: [edge]
```

### Voice loop behavior
When `tts.enabled` is true, the gateway generates a TTS audio attachment for assistant responses **only** when the inbound message looks like voice/audio:
- inbound attachments include type `voice` or `audio`, OR
- inbound metadata includes non-empty `media_text` (transcribed audio), OR
- inbound metadata includes `has_voice: true` (Telegram voice notes).

### Streaming + attachments
Streaming adapters typically only update message text. The gateway now sends any attachments (including TTS audio) as a follow-up outbound message after the final streaming update.

### Channel delivery improvements
Some channel adapters/download helpers now accept attachment URLs in these forms:
- local file paths (including `file://...`)
- `data:<mime>;base64,...`

## Open Questions
- Add additional local engines (e.g., Piper, Coqui TTS) and/or add an HTTP artifact serving option for generated audio.
- Decide whether to persist generated audio as artifacts for replay/transcripts.
