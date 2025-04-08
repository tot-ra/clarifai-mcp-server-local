package tools

import (
	"context"
	"fmt"      // Added for Sprintf
	"log/slog" // Use slog
	"strings"
	"time"

	"clarifai-mcp-server-local/internal/clarifai"
	"clarifai-mcp-server-local/internal/mcp"
	"clarifai-mcp-server-local/internal/utils"

	pb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api"
	statuspb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api/status"
)

// Handler processes MCP requests and calls appropriate tool implementations.
type Handler struct {
	clarifaiClient *clarifai.Client
	pat            string
	outputPath     string
	timeoutSec     int
	logger         *slog.Logger // Add logger dependency
}

// NewHandler creates a new tool handler.
// TODO: Accept logger as a parameter instead of relying on default.
func NewHandler(client *clarifai.Client, pat, outputPath string, timeout int) *Handler {
	return &Handler{
		clarifaiClient: client,
		pat:            pat,
		outputPath:     outputPath,
		timeoutSec:     timeout,
		logger:         slog.Default(), // Use default slog logger for now
	}
}

// Define the tools map with full schemas
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

// HandleRequest routes incoming MCP requests to the appropriate handler method.
func (h *Handler) HandleRequest(request mcp.JSONRPCRequest) *mcp.JSONRPCResponse {
	// Check for notifications first and ignore them
	if strings.HasPrefix(request.Method, "notifications/") {
		h.logger.Debug("Ignoring notification", "method", request.Method) // Use slog
		return nil                                                        // Return nil to indicate no response should be sent
	}
	h.logger.Debug("Handling request", "method", request.Method, "id", request.ID) // Use slog

	var response mcp.JSONRPCResponse
	switch request.Method {
	case "initialize":
		response = h.handleInitialize(request)
	case "tools/list":
		response = h.handleListTools(request)
	case "tools/call":
		response = h.handleCallTool(request)
	default:
		// h.logger.Warn("Unknown method received", "method", request.Method, "id", request.ID) // Use slog // Commented out to avoid interfering with client parsing
		response = mcp.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Error:   &mcp.RPCError{Code: -32601, Message: "Method not found", Data: request.Method},
		}
	}
	return &response
}

func (h *Handler) handleInitialize(request mcp.JSONRPCRequest) mcp.JSONRPCResponse {
	h.logger.Info("Handling initialize request", "id", request.ID) // Use slog
	return mcp.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
		Result: map[string]interface{}{
			"protocolVersion": "2024-11-05", // TODO: Make this dynamic or constant
			"serverInfo": map[string]interface{}{
				"name":    "clarifai-mcp-bridge", // TODO: Make configurable?
				"version": "0.1.0",               // TODO: Get from build info?
			},
			"capabilities": map[string]interface{}{
				"tools":             map[string]interface{}{}, // Empty tools map for now, list provides details
				"resources":         map[string]interface{}{},
				"resourceTemplates": map[string]interface{}{},
				"experimental":      map[string]any{},
				"prompts":           map[string]any{"listChanged": false},
			},
		},
	}
}

func (h *Handler) handleListTools(request mcp.JSONRPCRequest) mcp.JSONRPCResponse {
	// h.logger.Info("Handling tools/list request", "id", request.ID) // Use slog // Commented out to avoid interfering with client parsing
	// Convert the map to a slice/array as required by the schema
	toolsSlice := make([]map[string]interface{}, 0, len(toolsDefinitionMap))
	for name, definition := range toolsDefinitionMap {
		toolDef := definition.(map[string]interface{}) // Assert type
		toolDef["name"] = name                         // Add the name field
		toolsSlice = append(toolsSlice, toolDef)
	}
	return mcp.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
		Result:  map[string]interface{}{"tools": toolsSlice},
	}
}

func (h *Handler) handleCallTool(request mcp.JSONRPCRequest) mcp.JSONRPCResponse {
	h.logger.Info("Handling tools/call request", "tool_name", request.Params.Name, "id", request.ID) // Use slog
	var toolResult interface{}
	var toolError *mcp.RPCError

	// Check if Clarifai client is available (should be guaranteed by NewHandler, but check defensively)
	if h.clarifaiClient == nil || h.clarifaiClient.API == nil {
		toolError = &mcp.RPCError{Code: -32001, Message: "Clarifai client not initialized"}
		return mcp.JSONRPCResponse{JSONRPC: "2.0", ID: request.ID, Error: toolError}
	}
	// Check if PAT is configured
	if h.pat == "" {
		toolError = &mcp.RPCError{Code: -32001, Message: "Authentication failed: PAT not configured"}
		return mcp.JSONRPCResponse{JSONRPC: "2.0", ID: request.ID, Error: toolError}
	}

	switch request.Params.Name {
	case "infer_image":
		toolResult, toolError = h.callInferImage(request.Params.Arguments)
	case "generate_image":
		toolResult, toolError = h.callGenerateImage(request.Params.Arguments)
	default:
		toolError = &mcp.RPCError{Code: -32601, Message: "Tool not found: " + request.Params.Name}
	}

	return mcp.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
		Result:  toolResult,
		Error:   toolError,
	}
}

// --- Tool Implementations ---

func (h *Handler) callInferImage(args map[string]interface{}) (interface{}, *mcp.RPCError) {
	h.logger.Debug("Executing callInferImage tool")
	grpcClient := h.clarifaiClient.API

	// Extract arguments
	imageBytesB64, bytesOk := args["image_bytes"].(string)
	imageURL, urlOk := args["image_url"].(string)

	// Validate input: require either bytes or URL
	if (!bytesOk || imageBytesB64 == "") && (!urlOk || imageURL == "") {
		return nil, &mcp.RPCError{Code: -32602, Message: "Invalid params: missing or invalid 'image_bytes' or 'image_url'"}
	}
	if bytesOk && imageBytesB64 != "" && urlOk && imageURL != "" {
		return nil, &mcp.RPCError{Code: -32602, Message: "Invalid params: provide either 'image_bytes' or 'image_url', not both"}
	}

	// Optional arguments
	modelID, _ := args["model_id"].(string)
	userID, _ := args["user_id"].(string)
	appID, _ := args["app_id"].(string)

	// Default model and context if not provided
	// TODO: Make defaults configurable
	if modelID == "" {
		modelID = "general-image-recognition" // Default inference model
		h.logger.Debug("No model_id provided, defaulting", "model_id", modelID)
		// Let backend infer user/app from PAT if not provided for default model
	}

	h.logger.Info("Calling PostModelOutputs for infer_image", "user_id", userID, "app_id", appID, "model_id", modelID)

	// Prepare input data
	inputData := &pb.Data{}
	if bytesOk && imageBytesB64 != "" {
		// TODO: Consider decoding base64 here if the API expects raw bytes.
		// The current API seems to handle base64 strings directly in the Base64 field.
		// Need to verify if data URI prefix needs cleaning like in generate_image.
		// Assuming direct base64 string is okay for now.
		inputData.Image = &pb.Image{Base64: []byte(utils.CleanBase64Data([]byte(imageBytesB64)))} // Use cleaned base64
		h.logger.Debug("Using image_bytes for inference")
	} else {
		inputData.Image = &pb.Image{Url: imageURL}
		h.logger.Debug("Using image_url for inference", "url", imageURL)
	}

	// Prepare gRPC request
	grpcRequest := &pb.PostModelOutputsRequest{
		UserAppId: &pb.UserAppIDSet{UserId: userID, AppId: appID}, // Will use PAT context if empty
		ModelId:   modelID,
		Inputs:    []*pb.Input{{Data: inputData}},
		// Add Model.OutputInfo if needed for specific output config, e.g., concepts threshold
		// Model: &pb.Model{
		// 	OutputInfo: &pb.OutputInfo{
		// 		OutputConfig: &pb.OutputConfig{
		// 			MinValue: 0.95, // Example: Set concept threshold
		// 		},
		// 	},
		// },
	}

	// Create context with authentication and timeout
	baseCtx := context.Background()
	authCtx := clarifai.CreateContextWithAuth(baseCtx, h.pat)
	callTimeout := time.Duration(h.timeoutSec) * time.Second
	h.logger.Debug("Making gRPC call to PostModelOutputs", "timeout", callTimeout)
	callCtx, cancel := context.WithTimeout(authCtx, callTimeout)
	defer cancel()

	// Make the gRPC call
	resp, err := grpcClient.PostModelOutputs(callCtx, grpcRequest)
	h.logger.Debug("gRPC call to PostModelOutputs finished.")

	if err != nil {
		h.logger.Error("gRPC PostModelOutputs error", "error", err, "raw_response", resp)
		return nil, clarifai.MapGRPCErrorToJSONRPC(err)
	}
	if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
		h.logger.Error("gRPC PostModelOutputs non-success status", "status_code", resp.GetStatus().GetCode(), "description", resp.GetStatus().GetDescription(), "details", resp.GetStatus().GetDetails())
		return nil, &mcp.RPCError{
			Code:    -32000, // Generic internal error for API non-success
			Message: resp.GetStatus().GetDescription(),
			Data:    resp.GetStatus().GetDetails(),
		}
	}
	if len(resp.Outputs) == 0 || resp.Outputs[0].Data == nil {
		h.logger.Error("gRPC PostModelOutputs response missing expected data structure")
		return nil, &mcp.RPCError{Code: -32000, Message: "API response did not contain output data"}
	}

	// Process successful response - Extract concepts as a simple example
	// TODO: Handle other output types (regions, embeddings, etc.) based on model
	concepts := resp.Outputs[0].Data.GetConcepts()
	var conceptStrings []string
	if concepts != nil {
		for _, c := range concepts {
			conceptStrings = append(conceptStrings, fmt.Sprintf("%s: %.2f", c.Name, c.Value))
		}
	} else {
		h.logger.Warn("No concepts found in inference response")
		// Consider returning raw JSON or a different format if concepts aren't the primary output
	}

	h.logger.Info("Inference successful", "concepts_found", len(conceptStrings))

	// Return concepts as a simple text string
	resultText := "Inference Concepts: " + strings.Join(conceptStrings, ", ")
	if len(conceptStrings) == 0 {
		resultText = "Inference successful, but no concepts met the threshold or model doesn't output concepts."
	}

	toolResult := map[string]interface{}{
		"content": []map[string]any{
			{"type": "text", "text": resultText},
		},
	}
	return toolResult, nil
}

func (h *Handler) callGenerateImage(args map[string]interface{}) (interface{}, *mcp.RPCError) {
	h.logger.Debug("Executing callGenerateImage tool") // Use slog
	grpcClient := h.clarifaiClient.API

	// Extract arguments
	textPrompt, promptOk := args["text_prompt"].(string)
	if !promptOk || textPrompt == "" {
		return nil, &mcp.RPCError{Code: -32602, Message: "Invalid params: missing or invalid 'text_prompt'"}
	}

	// Optional arguments with defaults
	modelID, _ := args["model_id"].(string)
	userID, _ := args["user_id"].(string)
	appID, _ := args["app_id"].(string)

	// Default model and context if not provided
	// TODO: Make defaults configurable
	if modelID == "" {
		modelID = "stable-diffusion-xl"
		h.logger.Debug("No model_id provided, defaulting", "model_id", modelID) // Use slog
		if userID == "" {
			userID = "stability-ai"
			h.logger.Debug("Defaulting user_id", "user_id", userID, "model_id", modelID) // Use slog
		}
		if appID == "" {
			appID = "stable-diffusion-2"
			h.logger.Debug("Defaulting app_id", "app_id", appID, "model_id", modelID) // Use slog
		}
	} else {
		// If model is provided, use user/app from args if present.
		// Otherwise, let the backend infer from the PAT by leaving them empty.
	}

	h.logger.Info("Calling PostModelOutputs for generate_image", "user_id", userID, "app_id", appID, "model_id", modelID) // Use slog

	// Prepare gRPC request
	grpcRequest := &pb.PostModelOutputsRequest{
		UserAppId: &pb.UserAppIDSet{UserId: userID, AppId: appID},
		ModelId:   modelID,
		Inputs: []*pb.Input{
			{
				Data: &pb.Data{
					Text: &pb.Text{
						Raw: textPrompt,
					},
				},
			},
		},
	}

	// Create context with authentication and timeout
	baseCtx := context.Background()
	authCtx := clarifai.CreateContextWithAuth(baseCtx, h.pat)
	callTimeout := time.Duration(h.timeoutSec) * time.Second
	h.logger.Debug("Making gRPC call to PostModelOutputs", "timeout", callTimeout) // Use slog
	callCtx, cancel := context.WithTimeout(authCtx, callTimeout)
	defer cancel()

	// Make the gRPC call
	resp, err := grpcClient.PostModelOutputs(callCtx, grpcRequest)
	h.logger.Debug("gRPC call to PostModelOutputs finished.") // Use slog

	if err != nil {
		h.logger.Error("gRPC PostModelOutputs error", "error", err, "raw_response", resp) // Use slog
		return nil, clarifai.MapGRPCErrorToJSONRPC(err)
	}
	if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
		h.logger.Error("gRPC PostModelOutputs non-success status", "status_code", resp.GetStatus().GetCode(), "description", resp.GetStatus().GetDescription(), "details", resp.GetStatus().GetDetails()) // Use slog
		return nil, &mcp.RPCError{
			Code:    -32000, // Generic internal error for API non-success
			Message: resp.GetStatus().GetDescription(),
			Data:    resp.GetStatus().GetDetails(),
		}
	}
	if len(resp.Outputs) == 0 || resp.Outputs[0].Data == nil || resp.Outputs[0].Data.Image == nil {
		h.logger.Error("gRPC PostModelOutputs response missing expected data structure") // Use slog
		return nil, &mcp.RPCError{Code: -32000, Message: "API response did not contain image data"}
	}

	// Process successful response
	imageBase64Bytes := resp.Outputs[0].Data.Image.Base64
	h.logger.Info("Successfully generated image", "size_bytes", len(imageBase64Bytes)) // Use slog

	// Define image size threshold - TODO: Make configurable
	const imageSizeThreshold = 10 * 1024 // 10 KB

	// Check if output path is set and image size exceeds threshold
	if h.outputPath != "" && len(imageBase64Bytes) > imageSizeThreshold {
		h.logger.Debug("Image size exceeds threshold, saving to disk", "size_bytes", len(imageBase64Bytes), "threshold", imageSizeThreshold, "output_path", h.outputPath) // Use slog

		// Use the utility function to save the image
		savedPath, saveErr := utils.SaveImage(h.outputPath, imageBase64Bytes)
		if saveErr != nil {
			h.logger.Error("Error saving image using utility function", "error", saveErr) // Use slog
			return nil, &mcp.RPCError{Code: -32000, Message: "Failed to save generated image to disk"}
		}
		h.logger.Info("Successfully saved image to disk via utility function", "path", savedPath) // Use slog
		// Return only the file path as text content
		toolResult := map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": "Image saved to: " + savedPath,
				},
			},
		}
		return toolResult, nil
	}

	// Return base64 bytes directly as image content (small image or no output path)
	h.logger.Debug("Image size within threshold or output path not set, returning base64 data", "size_bytes", len(imageBase64Bytes)) // Use slog
	cleanedBase64String := utils.CleanBase64Data(imageBase64Bytes)
	toolResult := map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type":  "image",
				"bytes": cleanedBase64String,
			},
		},
	}
	return toolResult, nil
}
