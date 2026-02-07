package voice

import "testing"

func TestCallState_IsTerminal(t *testing.T) {
	terminal := []CallState{
		StateCompleted, StateHangupUser, StateHangupBot,
		StateTimeout, StateError, StateFailed,
		StateNoAnswer, StateBusy, StateVoicemail,
	}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("expected %q to be terminal", s)
		}
	}

	nonTerminal := []CallState{
		StateInitiated, StateRinging, StateAnswered,
		StateActive, StateSpeaking, StateListening,
	}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Errorf("expected %q to NOT be terminal", s)
		}
	}
}

func TestCallState_IsTerminal_Unknown(t *testing.T) {
	unknown := CallState("unknown-state")
	if unknown.IsTerminal() {
		t.Error("expected unknown state to NOT be terminal")
	}
}

func TestCallState_IsTerminal_Empty(t *testing.T) {
	empty := CallState("")
	if empty.IsTerminal() {
		t.Error("expected empty state to NOT be terminal")
	}
}
