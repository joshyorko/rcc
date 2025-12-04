package wizard

import (
	"errors"
	"testing"

	"github.com/joshyorko/rcc/pretty"
)

func TestConfirmWithForce(t *testing.T) {
	// When force is true, should return true without prompting
	result, err := Confirm("Test question", true)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if !result {
		t.Error("Expected true when force is set")
	}
}

func TestConfirmNonInteractiveWithoutForce(t *testing.T) {
	// Save original Interactive state
	originalInteractive := pretty.Interactive
	defer func() { pretty.Interactive = originalInteractive }()

	// Set non-interactive mode
	pretty.Interactive = false

	// Should return ErrConfirmationRequired in non-interactive mode without force
	result, err := Confirm("Test question", false)
	if !errors.Is(err, ErrConfirmationRequired) {
		t.Errorf("Expected ErrConfirmationRequired, got: %v", err)
	}
	if result {
		t.Error("Expected false when non-interactive without force")
	}
}

func TestConfirmDangerousWithForce(t *testing.T) {
	// When force is true, should return true without prompting
	result, err := ConfirmDangerous("Dangerous operation", true)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if !result {
		t.Error("Expected true when force is set")
	}
}

func TestConfirmDangerousNonInteractiveWithoutForce(t *testing.T) {
	// Save original Interactive state
	originalInteractive := pretty.Interactive
	defer func() { pretty.Interactive = originalInteractive }()

	// Set non-interactive mode
	pretty.Interactive = false

	// Should return ErrConfirmationRequired in non-interactive mode without force
	result, err := ConfirmDangerous("Dangerous operation", false)
	if !errors.Is(err, ErrConfirmationRequired) {
		t.Errorf("Expected ErrConfirmationRequired, got: %v", err)
	}
	if result {
		t.Error("Expected false when non-interactive without force")
	}
}

func TestErrConfirmationRequired(t *testing.T) {
	// Verify the error message
	expected := "confirmation required: use --yes flag in non-interactive mode"
	if ErrConfirmationRequired.Error() != expected {
		t.Errorf("Expected error message %q, got %q", expected, ErrConfirmationRequired.Error())
	}
}
