package tools

import (
	"log/slog"
	"strings"
	// Removed unused imports like context, fmt, net/url, os, strconv, time, grpc codes/status, protojson, proto, pb, statuspb, timestamppb

	"clarifai-mcp-server-local/clarifai"
	"clarifai-mcp-server-local/config"
	"clarifai-mcp-server-local/mcp"
	// "clarifai-mcp-server-local/utils" // utils might still be needed if error handling remains or is called from here
)

// Handler struct remains
type Handler struct {
	clarifaiClient *clarifai.Client
	pat            string
	outputPath     string
	timeoutSec     int
	logger         *slog.Logger
	config         *config.Config
}

// NewHandler remains
func NewHandler(client *clarifai.Client, cfg *config.Config) *Handler {
	return &Handler{
		clarifaiClient: client,
		pat:            cfg.Pat,
		outputPath:     cfg.OutputPath,
		timeoutSec:     cfg.TimeoutSec,
		logger:         slog.Default(),
		config:         cfg,
	}
}

// HandleRequest remains the main router
func (h *Handler) HandleRequest(request mcp.JSONRPCRequest) *mcp.JSONRPCResponse {
	if strings.HasPrefix(request.Method, "notifications/") {
		h.logger.Debug("Ignoring notification", "method", request.Method)
		return nil // Notifications are not responded to
	}
	h.logger.Debug("Handling request", "method", request.Method, "id", request.ID)

	var response mcp.JSONRPCResponse
	switch request.Method {
	case "initialize":
		response = h.handleInitialize(request) // Keep initialize handler
	case "tools/list":
		response = h.handleListTools(request) // Call moved method
	case "tools/call":
		response = h.handleCallTool(request) // Call moved method
	case "resources/templates/list":
		response = h.handleListResourceTemplates(request) // Call moved method
	case "resources/list":
		// Assuming resources/list should behave like resources/read for now
		response = h.handleReadResource(request) // Call moved method
	case "resources/read":
		response = h.handleReadResource(request) // Call moved method
	default:
		// Default error handling remains
		response = mcp.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Error:   &mcp.RPCError{Code: -32601, Message: "Method not found", Data: request.Method},
		}
	}
	// Return pointer to the response
	return &response
}

// handleInitialize remains
func (h *Handler) handleInitialize(request mcp.JSONRPCRequest) mcp.JSONRPCResponse {
	h.logger.Debug("Handling initialize request", "id", request.ID)
	// Note: resourceTemplates is now defined in resources_handler.go
	// If handleInitialize needs access, it might need to be passed or accessed differently.
	// For now, assuming it's okay to reference the global var from the other file within the same package.
	// A better approach might be to have a function in resources_handler.go return the templates.
	return mcp.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
		Result: map[string]interface{}{
			"protocolVersion": "2024-11-05", // Consider making this a constant
			"serverInfo": map[string]interface{}{
				"name":    "clarifai-mcp-bridge", // Consider making these constants
				"version": "0.1.0",
			},
			"capabilities": map[string]interface{}{
				"tools":             map[string]interface{}{}, // Tools capability is implicitly supported
				"resources":         map[string]interface{}{}, // Resources capability is implicitly supported
				"resourceTemplates": map[string]interface{}{"templates": resourceTemplates}, // Reference moved var
				"experimental":      map[string]any{},
				"prompts":           map[string]any{"listChanged": false},
			},
		},
	}
}

// All other handler methods (handleListTools, handleCallTool, handleListResourceTemplates, handleReadResource, handleGetResource, handleListResource)
// and the tool implementation methods (callClarifaiImageByPath, callClarifaiImageByURL, callGenerateImage)
// have been moved to tools_handler.go and resources_handler.go
