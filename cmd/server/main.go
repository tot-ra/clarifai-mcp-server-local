package main

import (
	"context"
	"fmt"       // Re-added for Fprintf
	"log/slog"  // Import slog
	"math/rand" // Added for filename generation
	"os"        // Needed for joining paths
	"time"      // Added for filename generation

	"clarifai-mcp-server-local/internal/clarifai" // Import the new clarifai package
	"clarifai-mcp-server-local/internal/config"   // Import the new config package
	"clarifai-mcp-server-local/internal/mcp"      // Import the new mcp package
	"clarifai-mcp-server-local/internal/tools"    // Import the new tools package
	// "clarifai-mcp-server-local/internal/utils" // No longer needed directly here
	// gRPC related imports (Some might be removed if no longer directly used here)
	// "google.golang.org/grpc" // No longer needed directly here
	// "google.golang.org/grpc/codes" // No longer needed directly here
	// "google.golang.org/grpc/credentials" // No longer needed directly here
	// "google.golang.org/grpc/metadata" // No longer needed directly here
	// "google.golang.org/grpc/status" // No longer needed directly here
	// Use gogo/protobuf types for Struct
	// statuspb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api/status" // No longer needed directly here
)

// toolsDefinitionMap is now defined within the tools package.
/*
var toolsDefinitionMap = map[string]interface{}{ // Renamed to avoid confusion
	"infer_image": map[string]interface{}{
		"description": "Performs inference on an image using a specified or default Clarifai model. Requires the server to be started with a valid --pat flag.", // Updated description
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"image_bytes": map[string]interface{}{
					"type":        "string",
					"description": "Base64 encoded bytes of the image file.",
				},
				"image_url": map[string]interface{}{
					"type":        "string",
					"description": "URL of the image file. Provide either image_bytes or image_url.",
				},
				"model_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional: Specific model ID to use. Defaults to a general classification model if omitted.",
				},
				"app_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional: App ID context. Defaults to the app associated with the PAT.",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional: User ID context. Defaults to the user associated with the PAT.",
				},
			},
			"anyOf": []map[string]interface{}{
				{"required": []string{"image_bytes"}},
				{"required": []string{"image_url"}},
			},
		},
	},
	"generate_image": map[string]interface{}{
		"description": "Generates an image based on a text prompt using a specified or default Clarifai text-to-image model. Requires the server to be started with a valid --pat flag.", // Updated description
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"text_prompt": map[string]interface{}{
					"type":        "string",
					"description": "Text prompt describing the desired image.",
				},
				"model_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional: Specific text-to-image model ID. Defaults to a suitable model if omitted.",
				},
				"app_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional: App ID context. Defaults to the app associated with the PAT.",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional: User ID context. Defaults to the user associated with the PAT.",
				},
			},
			"required": []string{"text_prompt"},
		},
	},
}
*/

func main() {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		// Use standard log before slog is initialized, or consider a pre-init logger
		// log.Fatalf("Failed to load configuration: %v", err)
		fmt.Fprintf(os.Stderr, "Error loading configuration: %v\n", err) // Print error to stderr
		// If the error was due to missing -pat, flag package might have already printed usage.
		os.Exit(1)
	}

	// Initialize structured logger
	logHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel})
	logger := slog.New(logHandler)
	slog.SetDefault(logger) // Set as default logger

	// --- Setup Clarifai Client ---
	clarifaiClient, err := clarifai.NewClient(cfg.GrpcAddr)
	if err != nil {
		slog.Error("Failed to create Clarifai client", "error", err) // Use slog
		os.Exit(1)                                                   // Exit if client setup fails
	}
	// globalState.clarifaiClient = clarifaiClient // Don't store in global state anymore
	defer func() {
		if err := clarifaiClient.Close(); err != nil { // Close client directly
			slog.Error("Error closing Clarifai client connection", "error", err) // Use slog
		}
	}() // Ensure connection is closed on exit

	slog.Info("Starting Clarifai MCP Server Bridge on stdio...") // Use slog

	// Create the tools handler, passing dependencies
	toolHandler := tools.NewHandler(clarifaiClient, cfg.Pat, cfg.OutputPath, cfg.TimeoutSec)

	// No longer initializing global auth state here.
	// The patFlag is passed directly to the handler goroutine.

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use the StdioServer implementation from the mcp package
	server := mcp.NewStdioServer(os.Stdin, os.Stdout)
	server.Start(ctx) // Start reader/writer goroutines

	// Main processing loop (reading from channel)
	go func() {
		// Seed random number generator for filenames (used by utils.SaveImage)
		// TODO: Consider moving seeding to main or using crypto/rand for uniqueness if critical
		rand.Seed(time.Now().UnixNano())

		for request := range server.ReadChannel() {
			// Handle the request using the tools handler
			responsePtr := toolHandler.HandleRequest(request)

			// Send response if one was generated (HandleRequest returns nil for notifications)
			if responsePtr != nil {
				server.WriteChannel() <- *responsePtr
			}
		}
		// log.Println("Main processing loop finished.") // Keep logging commented
		server.Close() // Close server when read channel closes
	}()

	// Wait for server shutdown
	server.Wait()
	// log.Println("Server exited.") // Keep logging commented
}

// --- Removed setupGRPCConnection, createContextWithAuth, mapGRPCErrorToJSONRPC ---
// --- These functions are now part of the internal/clarifai package ---
