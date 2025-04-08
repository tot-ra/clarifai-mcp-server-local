package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanBase64Data(t *testing.T) {
	testCases := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "No prefix, no whitespace",
			input:    []byte("purebase64"),
			expected: "purebase64",
		},
		{
			name:     "Data URI prefix",
			input:    []byte("data:image/png;base64,withprefix"),
			expected: "withprefix",
		},
		{
			name:     "Whitespace padding",
			input:    []byte("  paddedbase64  "),
			expected: "paddedbase64",
		},
		{
			name:     "Prefix and whitespace",
			input:    []byte("  data:image/jpeg;base64,  prefixandpadding \n"),
			expected: "prefixandpadding",
		},
		{
			name:     "Empty input",
			input:    []byte(""),
			expected: "",
		},
		{
			name:     "Whitespace only",
			input:    []byte("   \t\n "),
			expected: "",
		},
		{
			name:     "Prefix only",
			input:    []byte("data:image/gif;base64,"),
			expected: "",
		},
		{
			name:     "Prefix with whitespace",
			input:    []byte(" data:text/plain;base64, "),
			expected: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := CleanBase64Data(tc.input)
			if result != tc.expected {
				t.Errorf("Expected '%s', but got '%s'", tc.expected, result)
			}
		})
	}
}

func TestSaveImage(t *testing.T) {
	tempDir := t.TempDir()
	validImageData := []byte("data:image/png;base64,validimagedata")
	cleanedImageData := "validimagedata" // What should actually be written

	t.Run("Successful save", func(t *testing.T) {
		savedPath, err := SaveImage(tempDir, validImageData)
		if err != nil {
			t.Fatalf("Expected no error, but got: %v", err)
		}

		// Check if path is within the temp dir and has expected prefix/suffix
		if !strings.HasPrefix(savedPath, tempDir) {
			t.Errorf("Expected saved path '%s' to be within temp dir '%s'", savedPath, tempDir)
		}
		if !strings.HasPrefix(filepath.Base(savedPath), "generated_image_") || !strings.HasSuffix(savedPath, ".png") {
			t.Errorf("Saved path '%s' does not match expected format 'generated_image_...png'", savedPath)
		}

		// Check file content
		contentBytes, readErr := os.ReadFile(savedPath)
		if readErr != nil {
			t.Fatalf("Failed to read saved file '%s': %v", savedPath, readErr)
		}
		if string(contentBytes) != cleanedImageData {
			t.Errorf("Expected file content '%s', but got '%s'", cleanedImageData, string(contentBytes))
		}
		// No need to explicitly remove, t.TempDir() handles cleanup
	})

	t.Run("Save with pure base64", func(t *testing.T) {
		pureImageData := []byte("purebase64data")
		savedPath, err := SaveImage(tempDir, pureImageData)
		if err != nil {
			t.Fatalf("Expected no error for pure base64, but got: %v", err)
		}
		contentBytes, readErr := os.ReadFile(savedPath)
		if readErr != nil {
			t.Fatalf("Failed to read saved file '%s': %v", savedPath, readErr)
		}
		if string(contentBytes) != "purebase64data" {
			t.Errorf("Expected file content 'purebase64data', but got '%s'", string(contentBytes))
		}
	})

	// Note: Testing directory creation failure is hard without manipulating permissions.
	// MkdirAll usually succeeds or the WriteFile fails later.
	// We rely on the successful save case to implicitly test directory creation.

	// Testing WriteFile failure is also tricky without specific OS conditions.
}
