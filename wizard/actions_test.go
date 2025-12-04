package wizard

import (
	"errors"
	"testing"
)

func TestActionStruct(t *testing.T) {
	// Test that Action struct can be created and fields are accessible
	called := false
	action := Action{
		Key:         "test-key",
		Name:        "Test Action",
		Description: "This is a test action",
		Handler: func() error {
			called = true
			return nil
		},
	}

	if action.Key != "test-key" {
		t.Errorf("Expected Key to be 'test-key', got '%s'", action.Key)
	}
	if action.Name != "Test Action" {
		t.Errorf("Expected Name to be 'Test Action', got '%s'", action.Name)
	}
	if action.Description != "This is a test action" {
		t.Errorf("Expected Description to be 'This is a test action', got '%s'", action.Description)
	}

	// Test handler execution
	err := action.Handler()
	if err != nil {
		t.Errorf("Expected Handler to return nil, got %v", err)
	}
	if !called {
		t.Error("Expected Handler to be called")
	}
}

func TestChooseAction_NotInteractive(t *testing.T) {
	// Test that ChooseAction returns error when not interactive
	// This test assumes pretty.Interactive is false in test environment
	actions := []Action{
		{Key: "test", Name: "Test", Description: "Test action"},
	}

	_, err := ChooseAction("Choose an action", actions)
	if err != ErrNotInteractive {
		t.Errorf("Expected ErrNotInteractive in non-interactive mode, got %v", err)
	}
}

func TestChooseAction_EmptyActions(t *testing.T) {
	// Test that ChooseAction returns error with empty actions slice
	actions := []Action{}

	_, err := ChooseAction("Choose an action", actions)
	if err == nil {
		t.Error("Expected error with empty actions, got nil")
	}
}

func TestAskRecovery_NotInteractive(t *testing.T) {
	// Test that AskRecovery returns error when not interactive
	testErr := errors.New("test error")
	options := []Action{
		{Key: "retry", Name: "Retry", Description: "Try again"},
		{Key: "skip", Name: "Skip", Description: "Skip this step"},
	}

	_, err := AskRecovery(testErr, options)
	if err != ErrNotInteractive {
		t.Errorf("Expected ErrNotInteractive in non-interactive mode, got %v", err)
	}
}

func TestConfirmDangerous_Force(t *testing.T) {
	// Test that ConfirmDangerousWithText returns true immediately when force=true
	result, err := ConfirmDangerousWithText("Delete everything", "DELETE", true)
	if err != nil {
		t.Errorf("Expected no error with force=true, got %v", err)
	}
	if !result {
		t.Error("Expected true with force=true, got false")
	}
}

func TestConfirmDangerous_NotInteractiveNoForce(t *testing.T) {
	// Test that ConfirmDangerousWithText returns error when not interactive and force=false
	result, err := ConfirmDangerousWithText("Delete everything", "DELETE", false)
	if err != ErrConfirmationRequired {
		t.Errorf("Expected ErrConfirmationRequired in non-interactive mode, got %v", err)
	}
	if result {
		t.Error("Expected false result when error is returned")
	}
}
