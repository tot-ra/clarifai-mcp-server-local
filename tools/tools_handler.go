package tools

import (
	"context"
	"fmt"
	"os"

	"clarifai-mcp-server-local/clarifai"
	"clarifai-mcp-server-local/mcp"
	"clarifai-mcp-server-local/utils"

	pb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api"
	statuspb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api/status"
	"google.golang.org/protobuf/encoding/protojson"
)

// Updated toolsDefinitionMap (Moved from handler.go)
var toolsDefinitionMap = map[string]interface{}{
	"clarifai_image_by_path": map[string]interface{}{
		"description": "Performs inference on a local image file using a specified or default Clarifai model. Defaults to 'general-image-detection' model if none specified.",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"filepath": map[string]interface{}{
					"type":        "string",
					"description": "Absolute path to the local image file.",
				},
				"model_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional: Specific model ID to use. Defaults to 'general-image-detection' if omitted.",
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
			"required": []string{"filepath"},
		},
	},
	"clarifai_image_by_url": map[string]interface{}{
		"description": "Performs inference on an image URL using a specified or default Clarifai model.",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"image_url": map[string]interface{}{
					"type":        "string",
					"description": "URL of the image file.",
				},
				"model_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional: Specific model ID to use. Defaults to a general-image-detection if omitted.",
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
			"required": []string{"image_url"},
		},
	},
	"generate_image": map[string]interface{}{
		"description": "Generates an image based on a text prompt using a specified or default Clarifai text-to-image model. Requires the server to be started with a valid --pat flag.",
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
	"upload_file": map[string]interface{}{
		"description": "Uploads a local file to Clarifai as an input.",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"filepath": map[string]interface{}{
					"type":        "string",
					"description": "Absolute path to the local file to upload.",
				},
				"app_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional: App ID context. Defaults to the app associated with the PAT.",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional: User ID context. Defaults to the user associated with the PAT.",
				},
				// TODO: Add optional input_id, concepts, metadata, geo?
			},
			"required": []string{"filepath"},
		},
	},
}

// handleListTools lists the available tools. (Moved from handler.go)
func (h *Handler) handleListTools(request mcp.JSONRPCRequest) mcp.JSONRPCResponse {
	toolsSlice := make([]map[string]interface{}, 0, len(toolsDefinitionMap))
	for name, definition := range toolsDefinitionMap {
		toolDef := definition.(map[string]interface{})
		toolDef["name"] = name
		toolsSlice = append(toolsSlice, toolDef)
	}
	return mcp.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
		Result:  map[string]interface{}{"tools": toolsSlice},
	}
}

// handleCallTool routes tool calls to the appropriate function. (Moved from handler.go)
func (h *Handler) handleCallTool(request mcp.JSONRPCRequest) mcp.JSONRPCResponse {
	h.logger.Debug("Handling tools/call request", "tool_name", request.Params.Name, "id", request.ID)
	var toolResult interface{}
	var toolError *mcp.RPCError

	switch request.Params.Name {
	case "clarifai_image_by_path":
		toolResult, toolError = h.callClarifaiImageByPath(request.Params.Arguments)
	case "clarifai_image_by_url":
		toolResult, toolError = h.callClarifaiImageByURL(request.Params.Arguments)
	case "generate_image":
		toolResult, toolError = h.callGenerateImage(request.Params.Arguments)
	case "upload_file":
		toolResult, toolError = h.callUploadFile(request.Params.Arguments)
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

// callClarifaiImageByPath handles inference requests using a local file path. (Moved from handler.go)
func (h *Handler) callClarifaiImageByPath(args map[string]interface{}) (interface{}, *mcp.RPCError) {
	h.logger.Debug("Executing callClarifaiImageByPath tool")

	filepath, pathOk := args["filepath"].(string)
	if !pathOk || filepath == "" {
		return nil, &mcp.RPCError{Code: -32602, Message: "Invalid params: missing or invalid 'filepath'"}
	}

	modelID, _ := args["model_id"].(string)
	userID, _ := args["user_id"].(string)
	appID, _ := args["app_id"].(string)

	// Determine effective user/app/model IDs
	effectiveUserID := userID
	effectiveAppID := appID
	effectiveModelID := modelID

	if effectiveModelID == "" {
		effectiveModelID = "general-image-detection" // Default model
		h.logger.Debug("No model_id provided, defaulting", "model_id", effectiveModelID)
	}

	// Special handling for general-image-detection model
	if effectiveModelID == "general-image-detection" && effectiveUserID == "" && effectiveAppID == "" {
		effectiveUserID = "clarifai"
		effectiveAppID = "main"
		h.logger.Debug("Using default user/app for general-image-detection", "user_id", effectiveUserID, "app_id", effectiveAppID)
	}

	// Use configured defaults if args are empty
	if effectiveUserID == "" {
		effectiveUserID = h.config.DefaultUserID
		h.logger.Debug("Using default user ID from config", "user_id", effectiveUserID)
	}
	if effectiveAppID == "" {
		effectiveAppID = h.config.DefaultAppID
		h.logger.Debug("Using default app ID from config", "app_id", effectiveAppID)
	}

	// Prepare error context map
	errCtx := map[string]string{
		"tool":     "clarifai_image_by_path",
		"filepath": filepath,
		"userID":   effectiveUserID,
		"appID":    effectiveAppID,
		"modelID":  effectiveModelID,
	}

	// Read file content
	imageBytes, err := os.ReadFile(filepath)
	if err != nil {
		h.logger.Error("Failed to read image file", "filepath", filepath, "error", err)
		return nil, &mcp.RPCError{Code: -32000, Message: fmt.Sprintf("Failed to read image file: %v", err), Data: errCtx}
	}

	inputData := &pb.Data{
		Image: &pb.Image{Base64: imageBytes}, // Send raw image bytes directly
	}
	h.logger.Debug("Using raw image_bytes from file for inference", "filepath", filepath, "byte_count", len(imageBytes))

	grpcRequest := &pb.PostModelOutputsRequest{
		UserAppId: &pb.UserAppIDSet{UserId: effectiveUserID, AppId: effectiveAppID},
		ModelId:   effectiveModelID,
		Inputs:    []*pb.Input{{Data: inputData}},
	}

	ctx, cancel, rpcErr := utils.PrepareGrpcCall(context.Background(), h.clarifaiClient, h.pat, h.timeoutSec)
	if rpcErr != nil {
		rpcErr.Data = errCtx // Add context to initialization errors
		return nil, rpcErr
	}
	defer cancel()

	h.logger.Debug("Making gRPC call to PostModelOutputs (by path)", "timeout", h.timeoutSec, "user_id", effectiveUserID, "app_id", effectiveAppID, "model_id", effectiveModelID)
	resp, err := h.clarifaiClient.API.PostModelOutputs(ctx, grpcRequest)
	h.logger.Debug("gRPC call to PostModelOutputs (by path) finished.")

	if err != nil {
		return nil, utils.HandleApiError(err, errCtx, h.logger)
	}
	if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
		apiErr := clarifai.NewAPIStatusError(resp.GetStatus())
		return nil, utils.HandleApiError(apiErr, errCtx, h.logger)
	}
	if len(resp.Outputs) == 0 || resp.Outputs[0].Data == nil {
		apiErr := fmt.Errorf("API response did not contain output data")
		return nil, utils.HandleApiError(apiErr, errCtx, h.logger)
	}

	// Marshal the raw response to JSON
	m := protojson.MarshalOptions{Indent: "  ", EmitUnpopulated: true}
	rawResponseJSON, marshalErr := m.Marshal(resp)
	if marshalErr != nil {
		h.logger.Error("Failed to marshal raw API response", "error", marshalErr)
		apiErr := fmt.Errorf("failed to marshal raw API response: %w", marshalErr)
		return nil, utils.HandleApiError(apiErr, errCtx, h.logger)
	}

	h.logger.Debug("Inference successful (by path), returning raw response.")

	toolResult := map[string]interface{}{
		"content": []map[string]any{
			{"type": "text", "text": string(rawResponseJSON)}, // Return raw JSON response
		},
	}
	return toolResult, nil
}

// callUploadFile handles uploading a local file as a Clarifai input.
func (h *Handler) callUploadFile(args map[string]interface{}) (interface{}, *mcp.RPCError) {
	h.logger.Debug("Executing callUploadFile tool")

	filepath, pathOk := args["filepath"].(string)
	if !pathOk || filepath == "" {
		return nil, &mcp.RPCError{Code: -32602, Message: "Invalid params: missing or invalid 'filepath'"}
	}

	userID, _ := args["user_id"].(string)
	appID, _ := args["app_id"].(string)

	// Determine effective user/app IDs
	effectiveUserID := userID
	effectiveAppID := appID

	// Use configured defaults if args are empty
	if effectiveUserID == "" {
		effectiveUserID = h.config.DefaultUserID
		h.logger.Debug("Using default user ID from config", "user_id", effectiveUserID)
	}
	if effectiveAppID == "" {
		effectiveAppID = h.config.DefaultAppID
		h.logger.Debug("Using default app ID from config", "app_id", effectiveAppID)
	}

	// Prepare error context map
	errCtx := map[string]string{
		"tool":     "upload_file",
		"filepath": filepath,
		"userID":   effectiveUserID,
		"appID":    effectiveAppID,
	}

	// Read file content
	fileBytes, err := os.ReadFile(filepath)
	if err != nil {
		h.logger.Error("Failed to read file for upload", "filepath", filepath, "error", err)
		return nil, &mcp.RPCError{Code: -32000, Message: fmt.Sprintf("Failed to read file: %v", err), Data: errCtx}
	}

	h.logger.Debug("Read file", "filepath", filepath, "original_size", len(fileBytes))

	// Prepare input proto - Assuming image for now.
	// Pass raw file bytes directly, matching e2e tests. gRPC library handles encoding.
	inputData := &pb.Input{
		Data: &pb.Data{
			Image: &pb.Image{Base64: fileBytes},
		},
		// TODO: Add optional fields like ID, concepts, metadata, geo here if provided in args
	}

	userAppIDSet := &pb.UserAppIDSet{UserId: effectiveUserID, AppId: effectiveAppID}

	ctx, cancel, rpcErr := utils.PrepareGrpcCall(context.Background(), h.clarifaiClient, h.pat, h.timeoutSec)
	if rpcErr != nil {
		rpcErr.Data = errCtx // Add context to initialization errors
		return nil, rpcErr
	}
	defer cancel()

	h.logger.Debug("Making gRPC call to PostInputs (upload)", "timeout", h.timeoutSec, "user_id", effectiveUserID, "app_id", effectiveAppID)
	resp, err := h.clarifaiClient.PostInputs(ctx, userAppIDSet, []*pb.Input{inputData}, h.logger) // Use the new API wrapper
	h.logger.Debug("gRPC call to PostInputs (upload) finished.")

	if err != nil {
		return nil, utils.HandleApiError(err, errCtx, h.logger)
	}
	// PostInputs already checks status code in the wrapper

	// Marshal the raw response to JSON for user visibility
	m := protojson.MarshalOptions{Indent: "  ", EmitUnpopulated: true}
	rawResponseJSON, marshalErr := m.Marshal(resp)
	if marshalErr != nil {
		h.logger.Error("Failed to marshal PostInputs response", "error", marshalErr)
		// Don't fail the whole operation, just log it. The upload succeeded.
	}

	h.logger.Debug("File upload successful.")

	resultText := "File uploaded successfully."
	if rawResponseJSON != nil {
		resultText += "\nAPI Response:\n" + string(rawResponseJSON)
	}

	toolResult := map[string]interface{}{
		"content": []map[string]any{
			{"type": "text", "text": resultText},
		},
	}
	return toolResult, nil
}

// callClarifaiImageByURL handles inference requests using an image URL. (Moved from handler.go)
func (h *Handler) callClarifaiImageByURL(args map[string]interface{}) (interface{}, *mcp.RPCError) {
	h.logger.Debug("Executing callClarifaiImageByURL tool")

	imageURL, urlOk := args["image_url"].(string)
	if !urlOk || imageURL == "" {
		return nil, &mcp.RPCError{Code: -32602, Message: "Invalid params: missing or invalid 'image_url'"}
	}

	modelID, _ := args["model_id"].(string)
	userID, _ := args["user_id"].(string)
	appID, _ := args["app_id"].(string)

	// Determine effective user/app/model IDs
	effectiveUserID := userID
	effectiveAppID := appID
	effectiveModelID := modelID

	if effectiveModelID == "" {
		effectiveModelID = "general-image-detection" // Default model
		h.logger.Debug("No model_id provided, defaulting", "model_id", effectiveModelID)
	}

	// Special handling for general-image-detection model
	if effectiveModelID == "general-image-detection" && effectiveUserID == "" && effectiveAppID == "" {
		effectiveUserID = "clarifai"
		effectiveAppID = "main"
		h.logger.Debug("Using default user/app for general-image-detection", "user_id", effectiveUserID, "app_id", effectiveAppID)
	}

	// Use configured defaults if args are empty
	if effectiveUserID == "" {
		effectiveUserID = h.config.DefaultUserID
		h.logger.Debug("Using default user ID from config", "user_id", effectiveUserID)
	}
	if effectiveAppID == "" {
		effectiveAppID = h.config.DefaultAppID
		h.logger.Debug("Using default app ID from config", "app_id", effectiveAppID)
	}

	// Prepare error context map
	errCtx := map[string]string{
		"tool":     "clarifai_image_by_url",
		"imageURL": imageURL,
		"userID":   effectiveUserID,
		"appID":    effectiveAppID,
		"modelID":  effectiveModelID,
	}

	inputData := &pb.Data{
		Image: &pb.Image{Url: imageURL},
	}
	h.logger.Debug("Using image_url for inference", "url", imageURL)

	grpcRequest := &pb.PostModelOutputsRequest{
		UserAppId: &pb.UserAppIDSet{UserId: effectiveUserID, AppId: effectiveAppID},
		ModelId:   effectiveModelID,
		Inputs:    []*pb.Input{{Data: inputData}},
	}

	ctx, cancel, rpcErr := utils.PrepareGrpcCall(context.Background(), h.clarifaiClient, h.pat, h.timeoutSec)
	if rpcErr != nil {
		rpcErr.Data = errCtx // Add context to initialization errors
		return nil, rpcErr
	}
	defer cancel()

	h.logger.Debug("Making gRPC call to PostModelOutputs (by URL)", "timeout", h.timeoutSec, "user_id", effectiveUserID, "app_id", effectiveAppID, "model_id", effectiveModelID)
	resp, err := h.clarifaiClient.API.PostModelOutputs(ctx, grpcRequest)
	h.logger.Debug("gRPC call to PostModelOutputs (by URL) finished.")

	if err != nil {
		return nil, utils.HandleApiError(err, errCtx, h.logger)
	}
	if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
		apiErr := clarifai.NewAPIStatusError(resp.GetStatus())
		return nil, utils.HandleApiError(apiErr, errCtx, h.logger)
	}
	if len(resp.Outputs) == 0 || resp.Outputs[0].Data == nil {
		apiErr := fmt.Errorf("API response did not contain output data")
		return nil, utils.HandleApiError(apiErr, errCtx, h.logger)
	}

	// Marshal the raw response to JSON
	m := protojson.MarshalOptions{Indent: "  ", EmitUnpopulated: true}
	rawResponseJSON, marshalErr := m.Marshal(resp)
	if marshalErr != nil {
		h.logger.Error("Failed to marshal raw API response", "error", marshalErr)
		apiErr := fmt.Errorf("failed to marshal raw API response: %w", marshalErr)
		return nil, utils.HandleApiError(apiErr, errCtx, h.logger)
	}

	h.logger.Debug("Inference successful (by URL), returning raw response.")

	toolResult := map[string]interface{}{
		"content": []map[string]any{
			{"type": "text", "text": string(rawResponseJSON)}, // Return raw JSON response
		},
	}
	return toolResult, nil
}

// callGenerateImage handles image generation requests. (Moved from handler.go)
func (h *Handler) callGenerateImage(args map[string]interface{}) (interface{}, *mcp.RPCError) {
	h.logger.Debug("Executing callGenerateImage tool")

	textPrompt, promptOk := args["text_prompt"].(string)
	if !promptOk || textPrompt == "" {
		return nil, &mcp.RPCError{Code: -32602, Message: "Invalid params: missing or invalid 'text_prompt'"}
	}

	modelID, _ := args["model_id"].(string)
	userID, _ := args["user_id"].(string)
	appID, _ := args["app_id"].(string)

	// Determine effective user/app/model IDs
	effectiveUserID := userID
	effectiveAppID := appID
	effectiveModelID := modelID

	if effectiveModelID == "" {
		effectiveModelID = "stable-diffusion-xl"
		h.logger.Debug("No model_id provided, defaulting", "model_id", effectiveModelID)
		if effectiveUserID == "" {
			effectiveUserID = "stability-ai"
			h.logger.Debug("Defaulting user_id", "user_id", effectiveUserID, "model_id", effectiveModelID)
		}
		if effectiveAppID == "" {
			effectiveAppID = "stable-diffusion-2"
			h.logger.Debug("Defaulting app_id", "app_id", effectiveAppID, "model_id", effectiveModelID)
		}
	}

	// Use configured defaults if args are empty (though less likely for generation)
	if effectiveUserID == "" {
		effectiveUserID = h.config.DefaultUserID
		h.logger.Debug("Using default user ID from config", "user_id", effectiveUserID)
	}
	if effectiveAppID == "" {
		effectiveAppID = h.config.DefaultAppID
		h.logger.Debug("Using default app ID from config", "app_id", effectiveAppID)
	}

	// Prepare error context map
	errCtx := map[string]string{
		"tool":       "generate_image",
		"textPrompt": textPrompt, // Be mindful of logging sensitive prompts if applicable
		"userID":     effectiveUserID,
		"appID":      effectiveAppID,
		"modelID":    effectiveModelID,
	}

	grpcRequest := &pb.PostModelOutputsRequest{
		UserAppId: &pb.UserAppIDSet{UserId: effectiveUserID, AppId: effectiveAppID},
		ModelId:   effectiveModelID,
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

	ctx, cancel, rpcErr := utils.PrepareGrpcCall(context.Background(), h.clarifaiClient, h.pat, h.timeoutSec)
	if rpcErr != nil {
		rpcErr.Data = errCtx // Add context to initialization errors
		return nil, rpcErr
	}
	defer cancel()

	h.logger.Debug("Making gRPC call to PostModelOutputs (generate)", "timeout", h.timeoutSec, "user_id", effectiveUserID, "app_id", effectiveAppID, "model_id", effectiveModelID)
	resp, err := h.clarifaiClient.API.PostModelOutputs(ctx, grpcRequest)
	h.logger.Debug("gRPC call to PostModelOutputs (generate) finished.")

	if err != nil {
		return nil, utils.HandleApiError(err, errCtx, h.logger)
	}
	if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
		apiErr := clarifai.NewAPIStatusError(resp.GetStatus())
		return nil, utils.HandleApiError(apiErr, errCtx, h.logger)
	}
	if len(resp.Outputs) == 0 || resp.Outputs[0].Data == nil || resp.Outputs[0].Data.Image == nil {
		apiErr := fmt.Errorf("API response did not contain image data")
		return nil, utils.HandleApiError(apiErr, errCtx, h.logger)
	}

	imageBase64Bytes := resp.Outputs[0].Data.Image.Base64
	h.logger.Debug("Successfully generated image", "size_bytes", len(imageBase64Bytes))

	const imageSizeThreshold = 10 * 1024

	if h.outputPath != "" && len(imageBase64Bytes) > imageSizeThreshold {
		h.logger.Debug("Image size exceeds threshold, saving to disk", "size_bytes", len(imageBase64Bytes), "threshold", imageSizeThreshold, "output_path", h.outputPath)
		savedPath, saveErr := utils.SaveImage(h.outputPath, imageBase64Bytes)
		if saveErr != nil {
			h.logger.Error("Error saving image using utility function", "error", saveErr)
			// Consider returning the error instead of a generic message
			return nil, &mcp.RPCError{Code: -32000, Message: fmt.Sprintf("Failed to save generated image to disk: %v", saveErr), Data: errCtx}
		}
		h.logger.Debug("Successfully saved image to disk via utility function", "path", savedPath)
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

	h.logger.Debug("Image size within threshold or output path not set, returning base64 data", "size_bytes", len(imageBase64Bytes))
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
