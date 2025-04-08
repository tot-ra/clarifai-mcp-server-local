package config

import (
	"errors"
	"log/slog"
	"os"
	"testing"
)

// Helper function to set command line args for a test
func setTestArgs(args []string) func() {
	originalArgs := os.Args
	os.Args = append([]string{"test"}, args...) // os.Args[0] is program name
	return func() {
		os.Args = originalArgs // Restore original args after test
	}
}

func TestLoadConfig(t *testing.T) {
	// Store original os.Args and restore after all tests in this function
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	defaultTempDir := os.TempDir()

	testCases := []struct {
		name          string
		args          []string
		expectedCfg   *Config
		expectedError error
	}{
		{
			name: "All flags provided",
			args: []string{
				"-pat", "test-pat-123",
				"-output-path", "/custom/output",
				"-grpc-addr", "localhost:443",
				"-log-level", "DEBUG",
				"-timeout", "60",
			},
			expectedCfg: &Config{
				Pat:         "test-pat-123",
				OutputPath:  "/custom/output",
				GrpcAddr:    "localhost:443",
				LogLevel:    slog.LevelDebug,
				TimeoutSec:  60,
				logLevelStr: "DEBUG", // Internal field also set
			},
			expectedError: nil,
		},
		{
			name: "Missing optional flags (use defaults)",
			args: []string{
				"-pat", "test-pat-456",
			},
			expectedCfg: &Config{
				Pat:         "test-pat-456",
				OutputPath:  defaultTempDir,         // Default
				GrpcAddr:    "api.clarifai.com:443", // Default
				LogLevel:    slog.LevelInfo,         // Default
				TimeoutSec:  120,                    // Default
				logLevelStr: "INFO",                 // Default internal field
			},
			expectedError: nil,
		},
		{
			name: "Missing required pat flag",
			args: []string{
				"-output-path", "/another/path",
			},
			expectedCfg:   nil,
			expectedError: ErrPatMissing,
		},
		{
			name: "Invalid log level (defaults to INFO)",
			args: []string{
				"-pat", "test-pat-789",
				"-log-level", "TRACE", // Invalid level
			},
			expectedCfg: &Config{
				Pat:         "test-pat-789",
				OutputPath:  defaultTempDir,
				GrpcAddr:    "api.clarifai.com:443",
				LogLevel:    slog.LevelInfo, // Should default to INFO
				TimeoutSec:  120,
				logLevelStr: "TRACE",
			},
			expectedError: nil,
		},
		{
			name: "Warn log level",
			args: []string{
				"-pat", "test-pat-warn",
				"-log-level", "WARN",
			},
			expectedCfg: &Config{
				Pat:         "test-pat-warn",
				OutputPath:  defaultTempDir,
				GrpcAddr:    "api.clarifai.com:443",
				LogLevel:    slog.LevelWarn, // Check WARN level
				TimeoutSec:  120,
				logLevelStr: "WARN",
			},
			expectedError: nil,
		},
		// Note: Testing flag parsing errors (like "-pat") is tricky because
		// flag.ContinueOnError prints to os.Stderr and doesn't return a distinct error type easily.
		// We rely on the required -pat check for the main error path.
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set args for this test case and schedule restoration
			restoreArgs := setTestArgs(tc.args)
			defer restoreArgs()

			// Call LoadConfig
			cfg, err := LoadConfig()

			// Check error
			if !errors.Is(err, tc.expectedError) {
				t.Errorf("Expected error '%v', but got '%v'", tc.expectedError, err)
			}

			// Check config values only if no error was expected
			if tc.expectedError == nil && err == nil {
				if cfg == nil {
					t.Fatal("Expected non-nil config, but got nil")
				}
				// Compare relevant fields (ignore logLevelStr if needed, though checking it is fine)
				if cfg.Pat != tc.expectedCfg.Pat {
					t.Errorf("Expected Pat '%s', got '%s'", tc.expectedCfg.Pat, cfg.Pat)
				}
				if cfg.OutputPath != tc.expectedCfg.OutputPath {
					t.Errorf("Expected OutputPath '%s', got '%s'", tc.expectedCfg.OutputPath, cfg.OutputPath)
				}
				if cfg.GrpcAddr != tc.expectedCfg.GrpcAddr {
					t.Errorf("Expected GrpcAddr '%s', got '%s'", tc.expectedCfg.GrpcAddr, cfg.GrpcAddr)
				}
				if cfg.LogLevel != tc.expectedCfg.LogLevel {
					t.Errorf("Expected LogLevel '%v', got '%v'", tc.expectedCfg.LogLevel, cfg.LogLevel)
				}
				if cfg.TimeoutSec != tc.expectedCfg.TimeoutSec {
					t.Errorf("Expected TimeoutSec '%d', got '%d'", tc.expectedCfg.TimeoutSec, cfg.TimeoutSec)
				}
			} else if tc.expectedError != nil && err == nil {
				t.Errorf("Expected error '%v', but got nil config", tc.expectedError)
			} else if tc.expectedError == nil && err != nil {
				t.Errorf("Expected no error, but got: %v", err)
			}
			// If errors match, and config is expected nil, we don't need to compare cfg fields.
		})
	}
}
