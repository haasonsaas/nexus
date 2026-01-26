# Local TTS

## Summary
Local text-to-speech for full voice loop.

## Goals
- Enable local TTS engine selection
- Cache generated audio for repeated phrases
- Integrate with voice pipeline and agents

## Non-goals
- Neural voice training in this phase

## Proposed Design
- Abstract TTS provider interface with local backends
- Add config for engine selection and voice
- Stream audio output to voice adapter

## Open Questions
- Which local engines should be supported first (e.g. Piper, eSpeak)?
