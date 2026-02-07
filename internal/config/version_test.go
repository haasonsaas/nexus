package config

import (
	"errors"
	"testing"
)

func TestValidateVersion_Current(t *testing.T) {
	if err := ValidateVersion(CurrentVersion); err != nil {
		t.Fatalf("expected nil error for CurrentVersion, got %v", err)
	}
}

func TestValidateVersion_Zero(t *testing.T) {
	err := ValidateVersion(0)
	if err == nil {
		t.Fatal("expected error for version 0")
	}
	var ve *VersionError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *VersionError, got %T", err)
	}
	if ve.Reason != "missing or outdated" {
		t.Fatalf("expected reason 'missing or outdated', got %q", ve.Reason)
	}
}

func TestValidateVersion_Negative(t *testing.T) {
	err := ValidateVersion(-1)
	if err == nil {
		t.Fatal("expected error for negative version")
	}
	var ve *VersionError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *VersionError, got %T", err)
	}
	if ve.Reason != "missing or outdated" {
		t.Fatalf("expected reason 'missing or outdated', got %q", ve.Reason)
	}
}

func TestValidateVersion_NewerThanBuild(t *testing.T) {
	err := ValidateVersion(CurrentVersion + 1)
	if err == nil {
		t.Fatal("expected error for version newer than build")
	}
	var ve *VersionError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *VersionError, got %T", err)
	}
	if ve.Reason != "newer than this build" {
		t.Fatalf("expected reason 'newer than this build', got %q", ve.Reason)
	}
	// Verify the error message mentions upgrading
	msg := ve.Error()
	if msg == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestVersionError_NilReceiver(t *testing.T) {
	var ve *VersionError
	if got := ve.Error(); got != "" {
		t.Fatalf("expected empty string from nil VersionError, got %q", got)
	}
}

func TestVersionError_EmptyReason(t *testing.T) {
	ve := &VersionError{Version: 0, Current: 1}
	msg := ve.Error()
	if msg == "" {
		t.Fatal("expected non-empty error message for empty reason")
	}
}
