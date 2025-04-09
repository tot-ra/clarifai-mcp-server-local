package config

import (
	"errors" // Import errors package
	"flag"
	"fmt"      // Import fmt for error formatting
	"log/slog" // Import slog
	"os"
	"strings" // For log level parsing
)

// Config holds the application configuration.
type Config struct {
	Pat         string     // Clarifai Personal Access Token
	OutputPath  string     // Directory to save large generated images
	GrpcAddr    string     // Clarifai gRPC API address
	LogLevel      slog.Level // Use slog.Level type
	TimeoutSec    int        // gRPC call timeout in seconds
	DefaultUserID string     // Optional: Default User ID for listing resources
	DefaultAppID  string     // Optional: Default App ID for listing resources
	logLevelStr   string     // Temporary storage for the flag string
}

// ErrPatMissing indicates the required PAT flag was not provided.
var ErrPatMissing = errors.New("required flag -pat (Clarifai Personal Access Token) is missing")

// LoadConfig loads configuration from command-line flags.
// It returns an error if the required -pat flag is missing.
func LoadConfig() (*Config, error) {
	cfg := &Config{}

	// Use a new FlagSet for parsing to avoid interfering with other packages' flags
	// or tests running in parallel.
	fs := flag.NewFlagSet("clarifai-mcp-server", flag.ContinueOnError) // ContinueOnError prevents os.Exit

	// Define flags on the new FlagSet
	fs.StringVar(&cfg.Pat, "pat", "", "Clarifai Personal Access Token (required)")
	fs.StringVar(&cfg.OutputPath, "output-path", "", "Directory to save large generated images (optional, defaults to OS temp dir)")
	fs.StringVar(&cfg.GrpcAddr, "grpc-addr", "api.clarifai.com:443", "Clarifai gRPC API address")
	fs.StringVar(&cfg.logLevelStr, "log-level", "INFO", "Logging level (DEBUG, INFO, WARN, ERROR)") // Store flag in temp string
	fs.IntVar(&cfg.TimeoutSec, "timeout", 120, "gRPC call timeout in seconds")
	fs.StringVar(&cfg.DefaultUserID, "default-user-id", "", "Default User ID for listing resources without a specific URI (optional)")
	fs.StringVar(&cfg.DefaultAppID, "default-app-id", "", "Default App ID for listing resources without a specific URI (optional)")

	// Parse the flags from os.Args[1:]
	err := fs.Parse(os.Args[1:])
	if err != nil {
		// flag.ContinueOnError usually handles printing usage, but return error anyway
		return nil, fmt.Errorf("error parsing flags: %w", err)
	}

	// Parse log level string
	switch strings.ToUpper(cfg.logLevelStr) {
	case "DEBUG":
		cfg.LogLevel = slog.LevelDebug
	case "INFO":
		cfg.LogLevel = slog.LevelInfo
	case "WARN":
		cfg.LogLevel = slog.LevelWarn
	case "ERROR":
		cfg.LogLevel = slog.LevelError
	default:
		cfg.LogLevel = slog.LevelInfo // Default to INFO for invalid values
	}

	// Set default output path if not provided
	if cfg.OutputPath == "" {
		cfg.OutputPath = os.TempDir()
	}

	// Basic validation (PAT is required)
	if cfg.Pat == "" {
		// fs.Usage() // Optionally print usage for the specific flag set
		return nil, ErrPatMissing // Return specific error
	}

	return cfg, nil
}
