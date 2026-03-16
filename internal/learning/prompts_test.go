package learning

import (
	"strings"
	"testing"
)

func TestCorrectionExtractionPrompt(t *testing.T) {
	prompt := CorrectionExtractionPrompt("no, don't use pip, use uv instead")

	if !strings.Contains(prompt, "no, don't use pip") {
		t.Error("prompt should contain the user text")
	}
	if !strings.Contains(prompt, "is_correction") {
		t.Error("prompt should mention is_correction in expected format")
	}
}

func TestParseCorrectionExtractionResponse(t *testing.T) {
	t.Run("valid correction", func(t *testing.T) {
		response := `{
			"is_correction": true,
			"wrong": "used pip install",
			"right": "use uv instead of pip",
			"confidence": 0.9,
			"reasoning": "User explicitly corrected package manager usage"
		}`

		result, err := ParseCorrectionExtractionResponse(response)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.IsCorrection {
			t.Error("expected IsCorrection to be true")
		}
		if result.Wrong != "used pip install" {
			t.Errorf("Wrong = %q, want 'used pip install'", result.Wrong)
		}
		if result.Right != "use uv instead of pip" {
			t.Errorf("Right = %q, want 'use uv instead of pip'", result.Right)
		}
		if result.Confidence != 0.9 {
			t.Errorf("Confidence = %v, want 0.9", result.Confidence)
		}
	})

	t.Run("not a correction", func(t *testing.T) {
		response := `{"is_correction": false, "confidence": 0.1, "reasoning": "general conversation"}`

		result, err := ParseCorrectionExtractionResponse(response)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.IsCorrection {
			t.Error("expected IsCorrection to be false")
		}
	})

	t.Run("invalid confidence clamped", func(t *testing.T) {
		response := `{"is_correction": true, "confidence": 5.0}`
		result, err := ParseCorrectionExtractionResponse(response)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Confidence != 0.5 {
			t.Errorf("expected confidence clamped to 0.5, got %v", result.Confidence)
		}
	})

	t.Run("no JSON in response", func(t *testing.T) {
		_, err := ParseCorrectionExtractionResponse("No JSON here")
		if err == nil {
			t.Error("expected error for response without JSON")
		}
	})
}
