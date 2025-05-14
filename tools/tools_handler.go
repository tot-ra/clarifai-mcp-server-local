package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io" // Import io package
	"net/http"
	"os"
	"reflect"
	"sync"
	"time"

	"clarifai-mcp-server-local/clarifai"
	"clarifai-mcp-server-local/mcp"
	"clarifai-mcp-server-local/utils"

	pb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api"
	statuspb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api/status"
	"google.golang.org/protobuf/encoding/protojson"
)

// cleanMapRecursively removes nil, empty string, and empty slice/map values from a map.
func cleanMapRecursively(data map[string]interface{}) map[string]interface{} {
	cleaned := make(map[string]interface{})
	for key, value := range data {
		if value == nil {
			continue
		}

		val := reflect.ValueOf(value)
		switch val.Kind() {
		case reflect.String:
			if val.String() == "" {
				continue
			}
		case reflect.Slice, reflect.Map:
			if val.Len() == 0 {
				continue
			}
			// If it's a slice of maps or map of maps, recurse
			if val.Kind() == reflect.Slice {
				cleanedSlice := []interface{}{}
				for i := 0; i < val.Len(); i++ {
					elem := val.Index(i).Interface()
					if subMap, ok := elem.(map[string]interface{}); ok {
						cleanedSubMap := cleanMapRecursively(subMap)
						if len(cleanedSubMap) > 0 {
							cleanedSlice = append(cleanedSlice, cleanedSubMap)
						}
					} else {
						// Keep non-map elements if the slice isn't just for maps
						cleanedSlice = append(cleanedSlice, elem)
					}
				}
				if len(cleanedSlice) == 0 {
					continue // Skip empty slice after cleaning
				}
				value = cleanedSlice // Update value with cleaned slice
			} else { // It's a map
				if subMap, ok := value.(map[string]interface{}); ok {
					cleanedSubMap := cleanMapRecursively(subMap)
					if len(cleanedSubMap) == 0 {
						continue // Skip empty map after cleaning
					}
					value = cleanedSubMap // Update value with cleaned map
				}
			}

		case reflect.Ptr, reflect.Interface:
			if val.IsNil() {
				continue
			}
			// If it's a pointer to a map, dereference and recurse
			if val.Elem().Kind() == reflect.Map {
				if subMap, ok := val.Elem().Interface().(map[string]interface{}); ok {
					cleanedSubMap := cleanMapRecursively(subMap)
					if len(cleanedSubMap) == 0 {
						continue
					}
					value = cleanedSubMap
				}
			}
		}
		cleaned[key] = value
	}
	return cleaned
}

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
	"search_by_text": map[string]interface{}{
		"description": "Searches inputs based on a text query using Clarifai's PostInputSearches.",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "The text query string to search for.",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional: User ID context. Defaults to the user associated with the PAT.",
				},
				"app_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional: App ID context. Defaults to the app associated with the PAT.",
				},
				"page": map[string]interface{}{
					"type":        "integer",
					"description": "Optional: Page number for pagination (starts from 1).",
				},
				"per_page": map[string]interface{}{
					"type":        "integer",
					"description": "Optional: Number of results per page.",
				},
				// TODO: Add filters based on API capabilities (metadata, geo, concepts, etc.)
			},
			"required": []string{"query"},
		},
	},
	"search_by_filepath": map[string]interface{}{
		"description": "Searches inputs based on similarity to a local image file using Clarifai's PostInputSearches.",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"filepath": map[string]interface{}{
					"type":        "string",
					"description": "Absolute path to the local image file to use for similarity search.",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional: User ID context. Defaults to the user associated with the PAT.",
				},
				"app_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional: App ID context. Defaults to the app associated with the PAT.",
				},
				"page": map[string]interface{}{
					"type":        "integer",
					"description": "Optional: Page number for pagination (starts from 1).",
				},
				"per_page": map[string]interface{}{
					"type":        "integer",
					"description": "Optional: Number of results per page.",
				},
				// TODO: Add filters
			},
			"required": []string{"filepath"},
		},
	},
	"search_by_url": map[string]interface{}{
		"description": "Searches inputs based on similarity to an image URL using Clarifai's PostInputSearches.",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"image_url": map[string]interface{}{
					"type":        "string",
					"description": "URL of the image to use for similarity search.",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional: User ID context. Defaults to the user associated with the PAT.",
				},
				"app_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional: App ID context. Defaults to the app associated with the PAT.",
				},
				"page": map[string]interface{}{
					"type":        "integer",
					"description": "Optional: Page number for pagination (starts from 1).",
				},
				"per_page": map[string]interface{}{
					"type":        "integer",
					"description": "Optional: Number of results per page.",
				},
				// TODO: Add filters
			},
			"required": []string{"image_url"},
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
	case "search_by_text":
		toolResult, toolError = h.callSearchByText(request.Params.Arguments)
	case "search_by_filepath":
		toolResult, toolError = h.callSearchByFilepath(request.Params.Arguments)
	case "search_by_url":
		toolResult, toolError = h.callSearchByURL(request.Params.Arguments)
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

// callSearchByText handles search requests using a text query.
func (h *Handler) callSearchByText(args map[string]interface{}) (interface{}, *mcp.RPCError) {
	h.logger.Debug("Executing callSearchByText tool")

	queryText, queryOk := args["query"].(string)
	if !queryOk || queryText == "" {
		return nil, &mcp.RPCError{Code: -32602, Message: "Invalid params: missing or invalid 'query'"}
	}

	userID, _ := args["user_id"].(string)
	appID, _ := args["app_id"].(string)
	page, pageOk := args["page"].(float64) // JSON numbers are float64
	perPage, perPageOk := args["per_page"].(float64)

	// Determine effective user/app IDs
	effectiveUserID := userID
	effectiveAppID := appID
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
		"tool":   "search_by_text",
		"query":  queryText,
		"userID": effectiveUserID,
		"appID":  effectiveAppID,
	}

	// Construct the search query proto
	// Assuming text search ranks based on text similarity
	searchQueryProto := &pb.Query{
		Ranks: []*pb.Rank{
			{
				Annotation: &pb.Annotation{
					Data: &pb.Data{
						Text: &pb.Text{Raw: queryText},
					},
				},
			},
		},
		// TODO: Add Filters here if needed based on API structure
	}

	// Construct pagination
	pagination := &pb.Pagination{} // Correct type
	if pageOk && perPageOk && page > 0 && perPage > 0 {
		pagination.Page = uint32(page)
		pagination.PerPage = uint32(perPage)
		errCtx["page"] = fmt.Sprintf("%d", uint32(page))
		errCtx["per_page"] = fmt.Sprintf("%d", uint32(perPage))
		h.logger.Debug("Using pagination", "page", pagination.Page, "per_page", pagination.PerPage)
	} else {
		pagination.PerPage = 10 // Default to 10 results per page
		if pageOk && page > 0 {
			pagination.Page = uint32(page) // Still respect page if provided
			h.logger.Debug("Using default per_page, provided page", "page", pagination.Page, "per_page", pagination.PerPage)
		} else {
			pagination.Page = 1 // Default to page 1 if not provided
			h.logger.Debug("Using default pagination", "page", pagination.Page, "per_page", pagination.PerPage)
		}
	}

	grpcRequest := &pb.PostInputsSearchesRequest{
		UserAppId:  &pb.UserAppIDSet{UserId: effectiveUserID, AppId: effectiveAppID},
		Searches:   []*pb.Search{{Query: searchQueryProto}},
		Pagination: pagination,
	}

	ctx, cancel, rpcErr := utils.PrepareGrpcCall(context.Background(), h.clarifaiClient, h.pat, h.timeoutSec)
	if rpcErr != nil {
		rpcErr.Data = errCtx
		return nil, rpcErr
	}
	defer cancel()

	h.logger.Debug("Making gRPC call to PostInputsSearches (by text)", "timeout", h.timeoutSec, "user_id", effectiveUserID, "app_id", effectiveAppID)
	resp, err := h.clarifaiClient.API.PostInputsSearches(ctx, grpcRequest)
	h.logger.Debug("gRPC call to PostInputsSearches (by text) finished.")

	if err != nil {
		return nil, utils.HandleApiError(err, errCtx, h.logger)
	}
	if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
		apiErr := clarifai.NewAPIStatusError(resp.GetStatus())
		return nil, utils.HandleApiError(apiErr, errCtx, h.logger)
	}

	// --- Fetch raw text content concurrently ---
	var wg sync.WaitGroup
	fetchResults := make(map[string][]byte) // Map URL to fetched content
	fetchErrors := make(map[string]error)
	urlsToFetch := make(map[string]struct{}) // Use a map as a set to avoid duplicates

	for _, hit := range resp.Hits {
		if hit != nil && hit.Input != nil && hit.Input.Data != nil && hit.Input.Data.Text != nil && hit.Input.Data.Text.Url != "" {
			urlsToFetch[hit.Input.Data.Text.Url] = struct{}{}
		}
	}

	h.logger.Debug("Found URLs to fetch raw text for", "count", len(urlsToFetch))

	httpClient := &http.Client{Timeout: time.Duration(h.timeoutSec) * time.Second} // Reuse client with timeout

	for url := range urlsToFetch {
		wg.Add(1)
		go func(urlToFetch string) {
			defer wg.Done()
			h.logger.Debug("Fetching raw text", "url", urlToFetch)

			// Create request with Authorization header
			req, err := http.NewRequest("GET", urlToFetch, nil)
			if err != nil {
				h.logger.Error("Failed to create request for raw text URL", "url", urlToFetch, "error", err)
				fetchErrors[urlToFetch] = err
				return
			}
			req.Header.Set("Authorization", "Key "+h.pat) // Add PAT for authorization

			httpResp, err := httpClient.Do(req) // Execute request
			if err != nil {
				h.logger.Error("Failed to fetch raw text URL", "url", urlToFetch, "error", err)
				fetchErrors[urlToFetch] = err
				return
			}
			defer httpResp.Body.Close()

			if httpResp.StatusCode != http.StatusOK {
				err := fmt.Errorf("bad status code %d", httpResp.StatusCode)
				h.logger.Error("Failed to fetch raw text URL", "url", urlToFetch, "status", httpResp.Status, "error", err)
				fetchErrors[urlToFetch] = err
				return
			}

			bodyBytes, err := io.ReadAll(httpResp.Body) // Use io.ReadAll
			if err != nil {
				h.logger.Error("Failed to read raw text response body", "url", urlToFetch, "error", err)
				fetchErrors[urlToFetch] = err
				return
			}
			fetchResults[urlToFetch] = bodyBytes
			h.logger.Debug("Successfully fetched raw text", "url", urlToFetch, "size", len(bodyBytes))
		}(url)
	}

	wg.Wait()
	h.logger.Debug("Finished fetching all raw text URLs")
	// --- End Fetch raw text content ---

	// Clear embeddings and add raw text before marshaling
	for _, hit := range resp.Hits {
		// Add fetched raw text if available
		if hit != nil && hit.Input != nil && hit.Input.Data != nil && hit.Input.Data.Text != nil && hit.Input.Data.Text.Url != "" {
			url := hit.Input.Data.Text.Url
			if content, ok := fetchResults[url]; ok {
				hit.Input.Data.Text.Raw = string(content) // Add raw content to the proto
			} else if fetchErr, ok := fetchErrors[url]; ok {
				// Optionally add error message or leave Raw empty
				h.logger.Warn("Could not fetch raw text, leaving field empty", "url", url, "error", fetchErr)
				// hit.Input.Data.Text.Raw = fmt.Sprintf("Error fetching content: %v", fetchErr)
			}
		}

		// Clear from Input data (Embeddings)
		if hit != nil && hit.Input != nil && hit.Input.Data != nil {
			hit.Input.Data.Embeddings = nil
		}
		// Clear from Annotation data
		if hit != nil && hit.Annotation != nil && hit.Annotation.Data != nil {
			hit.Annotation.Data.Embeddings = nil
		}
	}

	// Marshal the proto response to a generic map for cleaning
	m := protojson.MarshalOptions{UseProtoNames: true} // Use proto names for consistency if needed
	tempJSON, marshalErr := m.Marshal(resp)
	if marshalErr != nil {
		h.logger.Error("Failed to marshal proto response to temporary JSON", "error", marshalErr)
		apiErr := fmt.Errorf("failed to marshal proto response: %w", marshalErr)
		return nil, utils.HandleApiError(apiErr, errCtx, h.logger)
	}

	var responseMap map[string]interface{}
	unmarshalErr := json.Unmarshal(tempJSON, &responseMap)
	if unmarshalErr != nil {
		h.logger.Error("Failed to unmarshal temporary JSON to map", "error", unmarshalErr)
		apiErr := fmt.Errorf("failed to unmarshal temporary JSON: %w", unmarshalErr)
		return nil, utils.HandleApiError(apiErr, errCtx, h.logger)
	}

	// Clean the map recursively
	cleanedResponseMap := cleanMapRecursively(responseMap)

	// Manually filter specific unwanted fields after cleaning
	if hits, ok := cleanedResponseMap["hits"].([]interface{}); ok {
		for _, hitInterface := range hits {
			if hitMap, ok := hitInterface.(map[string]interface{}); ok {
				// Filter annotation fields
				if annotation, ok := hitMap["annotation"].(map[string]interface{}); ok {
					delete(annotation, "worker")
					delete(annotation, "status")
				}
				// Filter input fields
				if input, ok := hitMap["input"].(map[string]interface{}); ok {
					delete(input, "status")
					// Filter input.data fields
					if data, ok := input["data"].(map[string]interface{}); ok {
						delete(data, "clusters")
					}
				}
			}
		}
	}

	// Marshal the cleaned map back to JSON for the final output
	finalJSON, finalMarshalErr := json.MarshalIndent(cleanedResponseMap, "", "  ")
	if finalMarshalErr != nil {
		h.logger.Error("Failed to marshal cleaned map to final JSON", "error", finalMarshalErr)
		apiErr := fmt.Errorf("failed to marshal cleaned response: %w", finalMarshalErr)
		return nil, utils.HandleApiError(apiErr, errCtx, h.logger)
	}

	h.logger.Debug("Search successful (by text), returning cleaned JSON response.")

	toolResult := map[string]interface{}{
		"content": []map[string]any{
			{"type": "text", "text": string(finalJSON)}, // Return cleaned JSON response
		},
	}
	return toolResult, nil
}

// callSearchByFilepath handles search requests using a local image file.
func (h *Handler) callSearchByFilepath(args map[string]interface{}) (interface{}, *mcp.RPCError) {
	h.logger.Debug("Executing callSearchByFilepath tool")

	filepath, pathOk := args["filepath"].(string)
	if !pathOk || filepath == "" {
		return nil, &mcp.RPCError{Code: -32602, Message: "Invalid params: missing or invalid 'filepath'"}
	}

	userID, _ := args["user_id"].(string)
	appID, _ := args["app_id"].(string)
	page, pageOk := args["page"].(float64)
	perPage, perPageOk := args["per_page"].(float64)

	// Determine effective user/app IDs
	effectiveUserID := userID
	effectiveAppID := appID
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
		"tool":     "search_by_filepath",
		"filepath": filepath,
		"userID":   effectiveUserID,
		"appID":    effectiveAppID,
	}

	// Read file content
	imageBytes, err := os.ReadFile(filepath)
	if err != nil {
		h.logger.Error("Failed to read image file for search", "filepath", filepath, "error", err)
		return nil, &mcp.RPCError{Code: -32000, Message: fmt.Sprintf("Failed to read image file: %v", err), Data: errCtx}
	}
	h.logger.Debug("Read image file for search", "filepath", filepath, "byte_count", len(imageBytes))

	// Construct the search query proto
	searchQueryProto := &pb.Query{
		Ranks: []*pb.Rank{
			{
				Annotation: &pb.Annotation{
					Data: &pb.Data{
						Image: &pb.Image{Base64: imageBytes},
					},
				},
			},
		},
		// TODO: Add Filters here if needed
	}

	// Construct pagination
	pagination := &pb.Pagination{} // Correct type
	if pageOk && perPageOk && page > 0 && perPage > 0 {
		pagination.Page = uint32(page)
		pagination.PerPage = uint32(perPage)
		errCtx["page"] = fmt.Sprintf("%d", uint32(page))
		errCtx["per_page"] = fmt.Sprintf("%d", uint32(perPage))
		h.logger.Debug("Using pagination", "page", pagination.Page, "per_page", pagination.PerPage)
	} else {
		pagination.PerPage = 10 // Default to 10 results per page
		if pageOk && page > 0 {
			pagination.Page = uint32(page) // Still respect page if provided
			h.logger.Debug("Using default per_page, provided page", "page", pagination.Page, "per_page", pagination.PerPage)
		} else {
			pagination.Page = 1 // Default to page 1 if not provided
			h.logger.Debug("Using default pagination", "page", pagination.Page, "per_page", pagination.PerPage)
		}
	}

	grpcRequest := &pb.PostInputsSearchesRequest{
		UserAppId:  &pb.UserAppIDSet{UserId: effectiveUserID, AppId: effectiveAppID},
		Searches:   []*pb.Search{{Query: searchQueryProto}},
		Pagination: pagination,
	}

	ctx, cancel, rpcErr := utils.PrepareGrpcCall(context.Background(), h.clarifaiClient, h.pat, h.timeoutSec)
	if rpcErr != nil {
		rpcErr.Data = errCtx
		return nil, rpcErr
	}
	defer cancel()

	h.logger.Debug("Making gRPC call to PostInputsSearches (by filepath)", "timeout", h.timeoutSec, "user_id", effectiveUserID, "app_id", effectiveAppID)
	resp, err := h.clarifaiClient.API.PostInputsSearches(ctx, grpcRequest)
	h.logger.Debug("gRPC call to PostInputsSearches (by filepath) finished.")

	if err != nil {
		return nil, utils.HandleApiError(err, errCtx, h.logger)
	}
	if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
		apiErr := clarifai.NewAPIStatusError(resp.GetStatus())
		return nil, utils.HandleApiError(apiErr, errCtx, h.logger)
	}

	// --- Fetch raw text content concurrently ---
	var wg sync.WaitGroup
	fetchResults := make(map[string][]byte) // Map URL to fetched content
	fetchErrors := make(map[string]error)
	urlsToFetch := make(map[string]struct{}) // Use a map as a set to avoid duplicates

	for _, hit := range resp.Hits {
		if hit != nil && hit.Input != nil && hit.Input.Data != nil && hit.Input.Data.Text != nil && hit.Input.Data.Text.Url != "" {
			urlsToFetch[hit.Input.Data.Text.Url] = struct{}{}
		}
	}

	h.logger.Debug("Found URLs to fetch raw text for", "count", len(urlsToFetch))

	httpClient := &http.Client{Timeout: time.Duration(h.timeoutSec) * time.Second} // Reuse client with timeout

	for url := range urlsToFetch {
		wg.Add(1)
		go func(urlToFetch string) {
			defer wg.Done()
			h.logger.Debug("Fetching raw text", "url", urlToFetch)

			// Create request with Authorization header
			req, err := http.NewRequest("GET", urlToFetch, nil)
			if err != nil {
				h.logger.Error("Failed to create request for raw text URL", "url", urlToFetch, "error", err)
				fetchErrors[urlToFetch] = err
				return
			}
			req.Header.Set("Authorization", "Key "+h.pat) // Add PAT for authorization

			httpResp, err := httpClient.Do(req) // Execute request
			if err != nil {
				h.logger.Error("Failed to fetch raw text URL", "url", urlToFetch, "error", err)
				fetchErrors[urlToFetch] = err
				return
			}
			defer httpResp.Body.Close()

			if httpResp.StatusCode != http.StatusOK {
				err := fmt.Errorf("bad status code %d", httpResp.StatusCode)
				h.logger.Error("Failed to fetch raw text URL", "url", urlToFetch, "status", httpResp.Status, "error", err)
				fetchErrors[urlToFetch] = err
				return
			}

			bodyBytes, err := io.ReadAll(httpResp.Body) // Use io.ReadAll
			if err != nil {
				h.logger.Error("Failed to read raw text response body", "url", urlToFetch, "error", err)
				fetchErrors[urlToFetch] = err
				return
			}
			fetchResults[urlToFetch] = bodyBytes
			h.logger.Debug("Successfully fetched raw text", "url", urlToFetch, "size", len(bodyBytes))
		}(url)
	}

	wg.Wait()
	h.logger.Debug("Finished fetching all raw text URLs")
	// --- End Fetch raw text content ---

	// Clear embeddings and add raw text before marshaling
	for _, hit := range resp.Hits {
		// Add fetched raw text if available
		if hit != nil && hit.Input != nil && hit.Input.Data != nil && hit.Input.Data.Text != nil && hit.Input.Data.Text.Url != "" {
			url := hit.Input.Data.Text.Url
			if content, ok := fetchResults[url]; ok {
				hit.Input.Data.Text.Raw = string(content) // Add raw content to the proto
			} else if fetchErr, ok := fetchErrors[url]; ok {
				// Optionally add error message or leave Raw empty
				h.logger.Warn("Could not fetch raw text, leaving field empty", "url", url, "error", fetchErr)
				// hit.Input.Data.Text.Raw = fmt.Sprintf("Error fetching content: %v", fetchErr)
			}
		}

		// Clear from Input data (Embeddings)
		if hit != nil && hit.Input != nil && hit.Input.Data != nil {
			hit.Input.Data.Embeddings = nil
		}
		// Clear from Annotation data
		if hit != nil && hit.Annotation != nil && hit.Annotation.Data != nil {
			hit.Annotation.Data.Embeddings = nil
		}
	}

	// Marshal the proto response to a generic map for cleaning
	m := protojson.MarshalOptions{UseProtoNames: true}
	tempJSON, marshalErr := m.Marshal(resp)
	if marshalErr != nil {
		h.logger.Error("Failed to marshal proto response to temporary JSON", "error", marshalErr)
		apiErr := fmt.Errorf("failed to marshal proto response: %w", marshalErr)
		return nil, utils.HandleApiError(apiErr, errCtx, h.logger)
	}

	var responseMap map[string]interface{}
	unmarshalErr := json.Unmarshal(tempJSON, &responseMap)
	if unmarshalErr != nil {
		h.logger.Error("Failed to unmarshal temporary JSON to map", "error", unmarshalErr)
		apiErr := fmt.Errorf("failed to unmarshal temporary JSON: %w", unmarshalErr)
		return nil, utils.HandleApiError(apiErr, errCtx, h.logger)
	}

	// Clean the map recursively
	cleanedResponseMap := cleanMapRecursively(responseMap)

	// Manually filter specific unwanted fields after cleaning
	if hits, ok := cleanedResponseMap["hits"].([]interface{}); ok {
		for _, hitInterface := range hits {
			if hitMap, ok := hitInterface.(map[string]interface{}); ok {
				// Filter annotation fields
				if annotation, ok := hitMap["annotation"].(map[string]interface{}); ok {
					delete(annotation, "worker")
					delete(annotation, "status")
				}
				// Filter input fields
				if input, ok := hitMap["input"].(map[string]interface{}); ok {
					delete(input, "status")
					// Filter input.data fields
					if data, ok := input["data"].(map[string]interface{}); ok {
						delete(data, "clusters")
					}
				}
			}
		}
	}

	// Marshal the cleaned map back to JSON for the final output
	finalJSON, finalMarshalErr := json.MarshalIndent(cleanedResponseMap, "", "  ")
	if finalMarshalErr != nil {
		h.logger.Error("Failed to marshal cleaned map to final JSON", "error", finalMarshalErr)
		apiErr := fmt.Errorf("failed to marshal cleaned response: %w", finalMarshalErr)
		return nil, utils.HandleApiError(apiErr, errCtx, h.logger)
	}

	h.logger.Debug("Search successful (by filepath), returning cleaned JSON response.")

	toolResult := map[string]interface{}{
		"content": []map[string]any{
			{"type": "text", "text": string(finalJSON)}, // Return cleaned JSON response
		},
	}
	return toolResult, nil
}

// callSearchByURL handles search requests using an image URL.
func (h *Handler) callSearchByURL(args map[string]interface{}) (interface{}, *mcp.RPCError) {
	h.logger.Debug("Executing callSearchByURL tool")

	imageURL, urlOk := args["image_url"].(string)
	if !urlOk || imageURL == "" {
		return nil, &mcp.RPCError{Code: -32602, Message: "Invalid params: missing or invalid 'image_url'"}
	}

	userID, _ := args["user_id"].(string)
	appID, _ := args["app_id"].(string)
	page, pageOk := args["page"].(float64)
	perPage, perPageOk := args["per_page"].(float64)

	// Determine effective user/app IDs
	effectiveUserID := userID
	effectiveAppID := appID
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
		"tool":      "search_by_url",
		"image_url": imageURL,
		"userID":    effectiveUserID,
		"appID":     effectiveAppID,
	}

	// Construct the search query proto
	searchQueryProto := &pb.Query{
		Ranks: []*pb.Rank{
			{
				Annotation: &pb.Annotation{
					Data: &pb.Data{
						Image: &pb.Image{Url: imageURL},
					},
				},
			},
		},
		// TODO: Add Filters here if needed
	}

	// Construct pagination
	pagination := &pb.Pagination{} // Correct type
	if pageOk && perPageOk && page > 0 && perPage > 0 {
		pagination.Page = uint32(page)
		pagination.PerPage = uint32(perPage)
		errCtx["page"] = fmt.Sprintf("%d", uint32(page))
		errCtx["per_page"] = fmt.Sprintf("%d", uint32(perPage))
		h.logger.Debug("Using pagination", "page", pagination.Page, "per_page", pagination.PerPage)
	} else {
		pagination.PerPage = 10 // Default to 10 results per page
		if pageOk && page > 0 {
			pagination.Page = uint32(page) // Still respect page if provided
			h.logger.Debug("Using default per_page, provided page", "page", pagination.Page, "per_page", pagination.PerPage)
		} else {
			pagination.Page = 1 // Default to page 1 if not provided
			h.logger.Debug("Using default pagination", "page", pagination.Page, "per_page", pagination.PerPage)
		}
	}

	grpcRequest := &pb.PostInputsSearchesRequest{
		UserAppId:  &pb.UserAppIDSet{UserId: effectiveUserID, AppId: effectiveAppID},
		Searches:   []*pb.Search{{Query: searchQueryProto}},
		Pagination: pagination,
	}

	ctx, cancel, rpcErr := utils.PrepareGrpcCall(context.Background(), h.clarifaiClient, h.pat, h.timeoutSec)
	if rpcErr != nil {
		rpcErr.Data = errCtx
		return nil, rpcErr
	}
	defer cancel()

	h.logger.Debug("Making gRPC call to PostInputsSearches (by URL)", "timeout", h.timeoutSec, "user_id", effectiveUserID, "app_id", effectiveAppID)
	resp, err := h.clarifaiClient.API.PostInputsSearches(ctx, grpcRequest)
	h.logger.Debug("gRPC call to PostInputsSearches (by URL) finished.")

	if err != nil {
		return nil, utils.HandleApiError(err, errCtx, h.logger)
	}
	if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
		apiErr := clarifai.NewAPIStatusError(resp.GetStatus())
		return nil, utils.HandleApiError(apiErr, errCtx, h.logger)
	}

	// --- Fetch raw text content concurrently ---
	var wg sync.WaitGroup
	fetchResults := make(map[string][]byte) // Map URL to fetched content
	fetchErrors := make(map[string]error)
	urlsToFetch := make(map[string]struct{}) // Use a map as a set to avoid duplicates

	for _, hit := range resp.Hits {
		if hit != nil && hit.Input != nil && hit.Input.Data != nil && hit.Input.Data.Text != nil && hit.Input.Data.Text.Url != "" {
			urlsToFetch[hit.Input.Data.Text.Url] = struct{}{}
		}
	}

	h.logger.Debug("Found URLs to fetch raw text for", "count", len(urlsToFetch))

	httpClient := &http.Client{Timeout: time.Duration(h.timeoutSec) * time.Second} // Reuse client with timeout

	for url := range urlsToFetch {
		wg.Add(1)
		go func(urlToFetch string) {
			defer wg.Done()
			h.logger.Debug("Fetching raw text", "url", urlToFetch)

			// Create request with Authorization header
			req, err := http.NewRequest("GET", urlToFetch, nil)
			if err != nil {
				h.logger.Error("Failed to create request for raw text URL", "url", urlToFetch, "error", err)
				fetchErrors[urlToFetch] = err
				return
			}
			req.Header.Set("Authorization", "Key "+h.pat) // Add PAT for authorization

			httpResp, err := httpClient.Do(req) // Execute request
			if err != nil {
				h.logger.Error("Failed to fetch raw text URL", "url", urlToFetch, "error", err)
				fetchErrors[urlToFetch] = err
				return
			}
			defer httpResp.Body.Close()

			if httpResp.StatusCode != http.StatusOK {
				err := fmt.Errorf("bad status code %d", httpResp.StatusCode)
				h.logger.Error("Failed to fetch raw text URL", "url", urlToFetch, "status", httpResp.Status, "error", err)
				fetchErrors[urlToFetch] = err
				return
			}

			bodyBytes, err := io.ReadAll(httpResp.Body) // Use io.ReadAll
			if err != nil {
				h.logger.Error("Failed to read raw text response body", "url", urlToFetch, "error", err)
				fetchErrors[urlToFetch] = err
				return
			}
			fetchResults[urlToFetch] = bodyBytes
			h.logger.Debug("Successfully fetched raw text", "url", urlToFetch, "size", len(bodyBytes))
		}(url)
	}

	wg.Wait()
	h.logger.Debug("Finished fetching all raw text URLs")
	// --- End Fetch raw text content ---

	// Clear embeddings and add raw text before marshaling
	for _, hit := range resp.Hits {
		// Add fetched raw text if available
		if hit != nil && hit.Input != nil && hit.Input.Data != nil && hit.Input.Data.Text != nil && hit.Input.Data.Text.Url != "" {
			url := hit.Input.Data.Text.Url
			if content, ok := fetchResults[url]; ok {
				hit.Input.Data.Text.Raw = string(content) // Add raw content to the proto
			} else if fetchErr, ok := fetchErrors[url]; ok {
				// Optionally add error message or leave Raw empty
				h.logger.Warn("Could not fetch raw text, leaving field empty", "url", url, "error", fetchErr)
				// hit.Input.Data.Text.Raw = fmt.Sprintf("Error fetching content: %v", fetchErr)
			}
		}

		// Clear from Input data (Embeddings)
		if hit != nil && hit.Input != nil && hit.Input.Data != nil {
			hit.Input.Data.Embeddings = nil
		}
		// Clear from Annotation data
		if hit != nil && hit.Annotation != nil && hit.Annotation.Data != nil {
			hit.Annotation.Data.Embeddings = nil
		}
	}

	// Marshal the proto response to a generic map for cleaning
	m := protojson.MarshalOptions{UseProtoNames: true}
	tempJSON, marshalErr := m.Marshal(resp)
	if marshalErr != nil {
		h.logger.Error("Failed to marshal proto response to temporary JSON", "error", marshalErr)
		apiErr := fmt.Errorf("failed to marshal proto response: %w", marshalErr)
		return nil, utils.HandleApiError(apiErr, errCtx, h.logger)
	}

	var responseMap map[string]interface{}
	unmarshalErr := json.Unmarshal(tempJSON, &responseMap)
	if unmarshalErr != nil {
		h.logger.Error("Failed to unmarshal temporary JSON to map", "error", unmarshalErr)
		apiErr := fmt.Errorf("failed to unmarshal temporary JSON: %w", unmarshalErr)
		return nil, utils.HandleApiError(apiErr, errCtx, h.logger)
	}

	// Clean the map recursively
	cleanedResponseMap := cleanMapRecursively(responseMap)

	// Manually filter specific unwanted fields after cleaning
	if hits, ok := cleanedResponseMap["hits"].([]interface{}); ok {
		for _, hitInterface := range hits {
			if hitMap, ok := hitInterface.(map[string]interface{}); ok {
				// Filter annotation fields
				if annotation, ok := hitMap["annotation"].(map[string]interface{}); ok {
					delete(annotation, "worker")
					delete(annotation, "status")
				}
				// Filter input fields
				if input, ok := hitMap["input"].(map[string]interface{}); ok {
					delete(input, "status")
					// Filter input.data fields
					if data, ok := input["data"].(map[string]interface{}); ok {
						delete(data, "clusters")
					}
				}
			}
		}
	}

	// Marshal the cleaned map back to JSON for the final output
	finalJSON, finalMarshalErr := json.MarshalIndent(cleanedResponseMap, "", "  ")
	if finalMarshalErr != nil {
		h.logger.Error("Failed to marshal cleaned map to final JSON", "error", finalMarshalErr)
		apiErr := fmt.Errorf("failed to marshal cleaned response: %w", finalMarshalErr)
		return nil, utils.HandleApiError(apiErr, errCtx, h.logger)
	}

	h.logger.Debug("Search successful (by URL), returning cleaned JSON response.")

	toolResult := map[string]interface{}{
		"content": []map[string]any{
			{"type": "text", "text": string(finalJSON)}, // Return cleaned JSON response
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

	// Determine file type
	fileType := utils.GetFileType(filepath)
	h.logger.Debug("Determined file type", "filepath", filepath, "file_type", fileType)

	// Prepare input proto based on file type
	inputData := &pb.Input{
		Data: &pb.Data{},
		// TODO: Add optional fields like ID, concepts, metadata, geo here if provided in args
	}

	switch fileType {
	case "image":
		inputData.Data.Image = &pb.Image{Base64: fileBytes}
		h.logger.Debug("Populating Image field for upload")
	case "video":
		inputData.Data.Video = &pb.Video{Base64: fileBytes}
		h.logger.Debug("Populating Video field for upload")
	case "audio":
		inputData.Data.Audio = &pb.Audio{Base64: fileBytes}
		h.logger.Debug("Populating Audio field for upload")
	case "text":
		inputData.Data.Text = &pb.Text{Raw: string(fileBytes)}
		h.logger.Debug("Populating Text field for upload")
	default:
		// Handle unknown file types - maybe return an error or default to text?
		// For now, let's return an error.
		h.logger.Error("Unsupported file type for upload", "filepath", filepath, "file_type", fileType)
		return nil, &mcp.RPCError{Code: -32602, Message: fmt.Sprintf("Unsupported file type for upload: %s", fileType), Data: errCtx}
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
