package tools

import (
	"context" // Added for marshalling input data in resource read
	"fmt"     // Added for Sprintf

	"log/slog" // Use slog
	"net/url"  // Added for URI parsing
	"strconv"  // Added for pagination parsing
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson" // Added for marshalling model data

	"clarifai-mcp-server-local/clarifai"
	"clarifai-mcp-server-local/config"
	"clarifai-mcp-server-local/mcp"
	"clarifai-mcp-server-local/utils"

	pb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api"
	statuspb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api/status"
)

// Handler processes MCP requests and calls appropriate tool implementations.
type Handler struct {
	clarifaiClient *clarifai.Client
	pat            string
	outputPath     string
	timeoutSec     int
	logger         *slog.Logger   // Add logger dependency
	config         *config.Config // Add config dependency
}

// NewHandler creates a new tool handler.
// TODO: Accept logger as a parameter instead of relying on default.
func NewHandler(client *clarifai.Client, cfg *config.Config) *Handler { // Accept config struct
	return &Handler{
		clarifaiClient: client,
		pat:            cfg.Pat, // Get PAT from config
		outputPath:     cfg.OutputPath,
		timeoutSec:     cfg.TimeoutSec,
		logger:         slog.Default(), // Use default slog logger for now
		config:         cfg,            // Store config
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
	// "list_models" tool removed - use resources/list with URI clarifai://{user_id}/{app_id}/models instead
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
	case "resources/templates/list":
		response = h.handleListResourceTemplates(request)
	case "resources/list":
		// response = h.handleListResources(request) // Deprecated in favor of read with list logic
		response = h.handleReadResource(request) // Route list requests to read handler
	case "resources/read":
		response = h.handleReadResource(request)
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
	h.logger.Debug("Handling initialize request", "id", request.ID) // Changed to Debug
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
				"tools":     map[string]interface{}{}, // Empty tools map, tools/list provides details
				"resources": map[string]interface{}{}, // Empty resources map, resources/list with URI provides details
				// Also include templates here in case client relies *only* on initialize response
				"resourceTemplates": map[string]interface{}{
					"templates": []map[string]interface{}{
						// Inputs
						{
							"uriTemplate": "clarifai://{user_id}/{app_id}/inputs",
							"name":        "List Clarifai Inputs",
							"description": "List inputs (images, videos, text) within a specific Clarifai app. Supports pagination via 'page' and 'per_page' query parameters.",
							"mimeType":    "application/json",
						},
						{
							"uriTemplate": "clarifai://{user_id}/{app_id}/inputs?query={search_term}",
							"name":        "Search Clarifai Inputs",
							"description": "Search for inputs within a specific Clarifai app using a query string. Supports pagination.",
							"mimeType":    "application/json",
						},
						{
							"uriTemplate": "clarifai://{user_id}/{app_id}/inputs/{input_id}",
							"name":        "Get Clarifai Input",
							"description": "Get details for a specific input.",
							"mimeType":    "application/json",
						},
						// Annotations
						{
							"uriTemplate": "clarifai://{user_id}/{app_id}/annotations",
							"name":        "List Clarifai Annotations",
							"description": "List annotations within a specific Clarifai app. Supports pagination and filtering.",
							"mimeType":    "application/json",
						},
						{
							"uriTemplate": "clarifai://{user_id}/{app_id}/annotations?query={search_term}",
							"name":        "Search Clarifai Annotations",
							"description": "Search for annotations within a specific Clarifai app using a query string. Supports pagination.",
							"mimeType":    "application/json",
						},
						{
							"uriTemplate": "clarifai://{user_id}/{app_id}/annotations/{annotation_id}",
							"name":        "Get Clarifai Annotation",
							"description": "Get details for a specific annotation.",
							"mimeType":    "application/json",
						},
						{
							"uriTemplate": "clarifai://{user_id}/{app_id}/inputs/{input_id}/annotations",
							"name":        "List Annotations for Input",
							"description": "List annotations associated with a specific input.",
							"mimeType":    "application/json",
						},
						// Models
						{
							"uriTemplate": "clarifai://{user_id}/{app_id}/models",
							"name":        "List Clarifai Models",
							"description": "List models within a specific Clarifai app or public models. Supports pagination.",
							"mimeType":    "application/json",
						},
						{
							"uriTemplate": "clarifai://{user_id}/{app_id}/models?query={search_term}",
							"name":        "Search Clarifai Models",
							"description": "Search for models within a specific Clarifai app or public models. Supports pagination.",
							"mimeType":    "application/json",
						},
						{
							"uriTemplate": "clarifai://{user_id}/{app_id}/models/{model_id}",
							"name":        "Get Clarifai Model",
							"description": "Get details for a specific model.",
							"mimeType":    "application/json",
						},
						// Model Versions
						{
							"uriTemplate": "clarifai://{user_id}/{app_id}/models/{model_id}/versions",
							"name":        "List Model Versions",
							"description": "List versions for a specific model.",
							"mimeType":    "application/json",
						},
						{
							"uriTemplate": "clarifai://{user_id}/{app_id}/models/{model_id}/versions/{version_id}",
							"name":        "Get Model Version",
							"description": "Get details for a specific model version.",
							"mimeType":    "application/json",
						},
						// Datasets
						{
							"uriTemplate": "clarifai://{user_id}/{app_id}/datasets",
							"name":        "List Clarifai Datasets",
							"description": "List datasets within a specific Clarifai app.",
							"mimeType":    "application/json",
						},
						{
							"uriTemplate": "clarifai://{user_id}/{app_id}/datasets/{dataset_id}",
							"name":        "Get Clarifai Dataset",
							"description": "Get details for a specific dataset.",
							"mimeType":    "application/json",
						},
						// Dataset Versions
						{
							"uriTemplate": "clarifai://{user_id}/{app_id}/datasets/{dataset_id}/versions",
							"name":        "List Dataset Versions",
							"description": "List versions for a specific dataset.",
							"mimeType":    "application/json",
						},
					},
				},
				"experimental": map[string]any{},
				"prompts":      map[string]any{"listChanged": false},
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
	h.logger.Debug("Handling tools/call request", "tool_name", request.Params.Name, "id", request.ID) // Changed to Debug
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
	// case "list_models": // Removed - use resources/list
	// 	toolResult, toolError = h.callListModels(request.Params.Arguments)
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

func (h *Handler) handleListResourceTemplates(request mcp.JSONRPCRequest) mcp.JSONRPCResponse {
	h.logger.Debug("Handling resources/templates/list request", "id", request.ID)
	templates := []map[string]interface{}{
		// Inputs
		{
			"uriTemplate": "clarifai://{user_id}/{app_id}/inputs",
			"name":        "List Clarifai Inputs",
			"description": "List inputs (images, videos, text) within a specific Clarifai app. Supports pagination via 'page' and 'per_page' query parameters.",
			"mimeType":    "application/json",
		},
		{
			"uriTemplate": "clarifai://{user_id}/{app_id}/inputs?query={search_term}",
			"name":        "Search Clarifai Inputs",
			"description": "Search for inputs within a specific Clarifai app using a query string. Supports pagination.",
			"mimeType":    "application/json",
		},
		{
			"uriTemplate": "clarifai://{user_id}/{app_id}/inputs/{input_id}",
			"name":        "Get Clarifai Input",
			"description": "Get details for a specific input.",
			"mimeType":    "application/json",
		},
		// Annotations
		{
			"uriTemplate": "clarifai://{user_id}/{app_id}/annotations",
			"name":        "List Clarifai Annotations",
			"description": "List annotations within a specific Clarifai app. Supports pagination and filtering.",
			"mimeType":    "application/json",
		},
		{
			"uriTemplate": "clarifai://{user_id}/{app_id}/annotations?query={search_term}",
			"name":        "Search Clarifai Annotations",
			"description": "Search for annotations within a specific Clarifai app using a query string. Supports pagination.",
			"mimeType":    "application/json",
		},
		{
			"uriTemplate": "clarifai://{user_id}/{app_id}/annotations/{annotation_id}",
			"name":        "Get Clarifai Annotation",
			"description": "Get details for a specific annotation.",
			"mimeType":    "application/json",
		},
		{
			"uriTemplate": "clarifai://{user_id}/{app_id}/inputs/{input_id}/annotations",
			"name":        "List Annotations for Input",
			"description": "List annotations associated with a specific input.",
			"mimeType":    "application/json",
		},
		// Models
		{
			"uriTemplate": "clarifai://{user_id}/{app_id}/models",
			"name":        "List Clarifai Models",
			"description": "List models within a specific Clarifai app or public models. Supports pagination.",
			"mimeType":    "application/json",
		},
		{
			"uriTemplate": "clarifai://{user_id}/{app_id}/models?query={search_term}",
			"name":        "Search Clarifai Models",
			"description": "Search for models within a specific Clarifai app or public models. Supports pagination.",
			"mimeType":    "application/json",
		},
		{
			"uriTemplate": "clarifai://{user_id}/{app_id}/models/{model_id}",
			"name":        "Get Clarifai Model",
			"description": "Get details for a specific model.",
			"mimeType":    "application/json",
		},
		// Model Versions
		{
			"uriTemplate": "clarifai://{user_id}/{app_id}/models/{model_id}/versions",
			"name":        "List Model Versions",
			"description": "List versions for a specific model.",
			"mimeType":    "application/json",
		},
		{
			"uriTemplate": "clarifai://{user_id}/{app_id}/models/{model_id}/versions/{version_id}",
			"name":        "Get Model Version",
			"description": "Get details for a specific model version.",
			"mimeType":    "application/json",
		},
		// Datasets
		{
			"uriTemplate": "clarifai://{user_id}/{app_id}/datasets",
			"name":        "List Clarifai Datasets",
			"description": "List datasets within a specific Clarifai app.",
			"mimeType":    "application/json",
		},
		{
			"uriTemplate": "clarifai://{user_id}/{app_id}/datasets/{dataset_id}",
			"name":        "Get Clarifai Dataset",
			"description": "Get details for a specific dataset.",
			"mimeType":    "application/json",
		},
		// Dataset Versions
		{
			"uriTemplate": "clarifai://{user_id}/{app_id}/datasets/{dataset_id}/versions",
			"name":        "List Dataset Versions",
			"description": "List versions for a specific dataset.",
			"mimeType":    "application/json",
		},
		// TODO: Add Workflows if List/Get methods exist and are needed
	}

	return mcp.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
		Result:  map[string]interface{}{"resourceTemplates": templates},
	}
}

// handleListResources is deprecated and functionality moved to handleReadResource
// func (h *Handler) handleListResources(request mcp.JSONRPCRequest) mcp.JSONRPCResponse { ... }

// Helper function to map a Clarifai Input to an MCP Resource map
func mapInputToResource(input *pb.Input, userID, appID string) map[string]interface{} {
	resource := map[string]interface{}{
		"uri":         fmt.Sprintf("clarifai://%s/%s/inputs/%s", userID, appID, input.Id),
		"name":        input.Id, // Use ID as name for now
		"description": "",       // Add description if available/useful
	}
	// Determine mimeType based on data type
	if input.Data != nil {
		if input.Data.Image != nil {
			resource["mimeType"] = "image/*" // Generic image type
		} else if input.Data.Video != nil {
			resource["mimeType"] = "video/*" // Generic video type
		} else if input.Data.Audio != nil {
			resource["mimeType"] = "audio/*" // Generic audio type
		} else if input.Data.Text != nil {
			resource["mimeType"] = "text/plain"
		} else {
			resource["mimeType"] = "application/octet-stream" // Default binary
		}
	} else {
		resource["mimeType"] = "application/octet-stream"
	}
	return resource
}

// Helper function to map a Clarifai Model to an MCP Resource map
func mapModelToResource(model *pb.Model, userID, appID string) map[string]interface{} {
	resource := map[string]interface{}{
		"uri":         fmt.Sprintf("clarifai://%s/%s/models/%s", userID, appID, model.Id),
		"name":        model.Name,         // Use model name if available, otherwise ID
		"description": model.Notes,        // Use notes as description
		"mimeType":    "application/json", // Models are represented as JSON
	}
	if resource["name"] == "" {
		resource["name"] = model.Id // Fallback to ID if name is empty
	}
	// Add more details if needed, e.g., model type
	// resource["modelTypeId"] = model.ModelTypeId
	return resource
}

// --- Helper function to perform the actual gRPC list/search call ---
// Takes parsed components and returns the MCP response
func (h *Handler) performListResourceCall(request mcp.JSONRPCRequest, userID, appID, resourceType string, queryParams url.Values) mcp.JSONRPCResponse {
	h.logger.Debug("Performing list resource call", "userID", userID, "appID", appID, "resourceType", resourceType, "queryParams", queryParams)

	// --- Pagination & Query Extraction ---
	var page, perPage uint32 = 1, 20  // Default pagination
	query := queryParams.Get("query") // Get search query if present

	// Handle pagination from query params OR cursor (Cursor takes precedence if provided in original request)
	if request.Params.Cursor != "" {
		pageInt, err := strconv.Atoi(request.Params.Cursor)
		if err == nil && pageInt > 0 {
			page = uint32(pageInt)
		} else {
			h.logger.Warn("Invalid pagination cursor provided, using default", "cursor", request.Params.Cursor)
		}
	} else {
		// Check query params if cursor not used
		if pageStr := queryParams.Get("page"); pageStr != "" {
			pageInt, err := strconv.Atoi(pageStr)
			if err == nil && pageInt > 0 {
				page = uint32(pageInt)
			}
		}
		if perPageStr := queryParams.Get("per_page"); perPageStr != "" {
			perPageInt, err := strconv.Atoi(perPageStr)
			if err == nil && perPageInt > 0 {
				if perPageInt > 1000 {
					perPageInt = 1000
				} // Limit per_page
				perPage = uint32(perPageInt)
			}
		}
	}

	// --- gRPC Setup ---
	grpcClient := h.clarifaiClient.API
	if grpcClient == nil {
		return mcp.JSONRPCResponse{JSONRPC: "2.0", ID: request.ID, Error: &mcp.RPCError{Code: -32001, Message: "Clarifai client not initialized"}}
	}
	if h.pat == "" {
		return mcp.JSONRPCResponse{JSONRPC: "2.0", ID: request.ID, Error: &mcp.RPCError{Code: -32001, Message: "Authentication failed: PAT not configured"}}
	}

	baseCtx := context.Background()
	authCtx := clarifai.CreateContextWithAuth(baseCtx, h.pat)
	callTimeout := time.Duration(h.timeoutSec) * time.Second
	callCtx, cancel := context.WithTimeout(authCtx, callTimeout)
	defer cancel()

	var resources []map[string]interface{}
	var nextCursor string
	var apiErr error

	userAppIDSet := &pb.UserAppIDSet{UserId: userID, AppId: appID}
	pagination := &pb.Pagination{Page: page, PerPage: perPage}

	// --- Perform gRPC Call based on resourceType ---
	switch resourceType {
	case "inputs":
		if query != "" {
			// PostInputsSearches
			h.logger.Debug("Calling PostInputsSearches", "user_id", userID, "app_id", appID, "query", query, "page", page, "per_page", perPage)
			searchQueryProto := &pb.Query{Ranks: []*pb.Rank{{Annotation: &pb.Annotation{Data: &pb.Data{Text: &pb.Text{Raw: query}}}}}}
			grpcRequest := &pb.PostInputsSearchesRequest{UserAppId: userAppIDSet, Searches: []*pb.Search{{Query: searchQueryProto}}, Pagination: pagination}
			resp, err := grpcClient.PostInputsSearches(callCtx, grpcRequest)
			apiErr = err
			if err == nil {
				if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
					apiErr = fmt.Errorf("API error: %s - %s", resp.GetStatus().GetDescription(), resp.GetStatus().GetDetails())
				} else {
					resources = make([]map[string]interface{}, 0, len(resp.Hits))
					for _, hit := range resp.Hits {
						if hit.Input != nil {
							resources = append(resources, mapInputToResource(hit.Input, userID, appID))
						}
					}
					if uint32(len(resp.Hits)) == perPage {
						nextCursor = strconv.Itoa(int(page + 1))
					}
				}
			}
		} else {
			// ListInputs
			h.logger.Debug("Calling ListInputs", "user_id", userID, "app_id", appID, "page", page, "per_page", perPage)
			grpcRequest := &pb.ListInputsRequest{UserAppId: userAppIDSet, Page: page, PerPage: perPage}
			resp, err := grpcClient.ListInputs(callCtx, grpcRequest)
			apiErr = err
			if err == nil {
				if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
					apiErr = fmt.Errorf("API error: %s - %s", resp.GetStatus().GetDescription(), resp.GetStatus().GetDetails())
				} else {
					resources = make([]map[string]interface{}, 0, len(resp.Inputs))
					for _, input := range resp.Inputs {
						resources = append(resources, mapInputToResource(input, userID, appID))
					}
					if uint32(len(resp.Inputs)) == perPage {
						nextCursor = strconv.Itoa(int(page + 1))
					}
				}
			}
		}
	case "models":
		if query != "" {
			h.logger.Warn("Query parameter is not yet supported for listing models", "query", query)
			// TODO: Implement PostModelSearches if needed
			apiErr = fmt.Errorf("query parameter not supported for listing models")
		} else {
			// ListModels
			h.logger.Debug("Calling ListModels", "user_id", userID, "app_id", appID, "page", page, "per_page", perPage)
			grpcRequest := &pb.ListModelsRequest{UserAppId: userAppIDSet, Page: page, PerPage: perPage}
			resp, err := grpcClient.ListModels(callCtx, grpcRequest)
			apiErr = err
			if err == nil {
				if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
					apiErr = fmt.Errorf("API error: %s - %s", resp.GetStatus().GetDescription(), resp.GetStatus().GetDetails())
				} else {
					resources = make([]map[string]interface{}, 0, len(resp.Models))
					for _, model := range resp.Models {
						resources = append(resources, mapModelToResource(model, userID, appID))
					}
					if uint32(len(resp.Models)) == perPage {
						nextCursor = strconv.Itoa(int(page + 1))
					}
				}
			}
		}
	// TODO: Add cases for other listable resource types (datasets, annotations, etc.)
	default:
		apiErr = fmt.Errorf("unsupported resource type for list: %s", resourceType)
	}

	// --- Handle Errors & Return Response ---
	if apiErr != nil {
		h.logger.Error("gRPC error during list resource call", "resource_type", resourceType, "error", apiErr)
		// Check if it's a gRPC error or other error
		grpcErr := clarifai.MapGRPCErrorToJSONRPC(apiErr)
		if grpcErr != nil {
			return mcp.JSONRPCResponse{JSONRPC: "2.0", ID: request.ID, Error: grpcErr}
		}
		// Generic internal error if not a gRPC error
		return mcp.JSONRPCResponse{
			JSONRPC: "2.0", ID: request.ID,
			Error: &mcp.RPCError{Code: -32000, Message: fmt.Sprintf("Failed to list resources: %v", apiErr)},
		}
	}

	h.logger.Debug("Successfully listed resources", "count", len(resources), "next_cursor", nextCursor)
	return mcp.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
		Result: map[string]interface{}{
			"resources":  resources,
			"nextCursor": nextCursor,
		},
	}
}

func (h *Handler) handleReadResource(request mcp.JSONRPCRequest) mcp.JSONRPCResponse {
	h.logger.Debug("Handling resources/read request", "id", request.ID, "uri", request.Params.URI)

	// --- Parameter Extraction & URI Validation ---
	if request.Params.URI == "" {
		return mcp.JSONRPCResponse{
			JSONRPC: "2.0", ID: request.ID,
			Error: &mcp.RPCError{Code: -32602, Message: "Missing required 'uri' parameter"},
		}
	}

	parsedURI, err := url.Parse(request.Params.URI)
	if err != nil || parsedURI.Scheme != "clarifai" {
		h.logger.Warn("Invalid URI scheme for resources/read", "uri", request.Params.URI, "error", err)
		return mcp.JSONRPCResponse{
			JSONRPC: "2.0", ID: request.ID,
			Error: &mcp.RPCError{Code: -32602, Message: "Invalid URI format. Expected clarifai://..."},
		}
	}

	// --- Revised Path Parsing Logic ---
	userID := parsedURI.Host // User ID is expected in the host part
	if userID == "" {
		h.logger.Warn("Invalid URI format for resources/read: Missing user_id (host part)", "uri", request.Params.URI)
		return mcp.JSONRPCResponse{
			JSONRPC: "2.0", ID: request.ID,
			Error: &mcp.RPCError{Code: -32602, Message: "Invalid URI format: Missing user_id. Expected clarifai://{user_id}/{app_id}/..."},
		}
	}

	trimmedPath := strings.TrimPrefix(parsedURI.Path, "/")
	pathParts := strings.Split(trimmedPath, "/") // Split the path part (e.g., "app_id/resource_type/resource_id")

	if len(pathParts) < 2 {
		h.logger.Warn("Invalid URI path format for resources/read (too few path parts)", "path", parsedURI.Path, "parts", len(pathParts))
		return mcp.JSONRPCResponse{
			JSONRPC: "2.0", ID: request.ID,
			Error: &mcp.RPCError{Code: -32602, Message: fmt.Sprintf("Invalid URI path format for resources/read. Expected at least clarifai://{user_id}/{app_id}/{resource_type}, got %d parts", len(pathParts))},
		}
	}

	appID := pathParts[0]
	if appID == "" {
		h.logger.Warn("Invalid URI format for resources/read: Missing app_id", "uri", request.Params.URI)
		return mcp.JSONRPCResponse{
			JSONRPC: "2.0", ID: request.ID,
			Error: &mcp.RPCError{Code: -32602, Message: "Invalid URI format: Missing app_id. Expected clarifai://{user_id}/{app_id}/..."},
		}
	}

	// --- gRPC Setup --- (Common for all operations)
	grpcClient := h.clarifaiClient.API
	if grpcClient == nil {
		return mcp.JSONRPCResponse{JSONRPC: "2.0", ID: request.ID, Error: &mcp.RPCError{Code: -32001, Message: "Clarifai client not initialized"}}
	}
	if h.pat == "" {
		return mcp.JSONRPCResponse{JSONRPC: "2.0", ID: request.ID, Error: &mcp.RPCError{Code: -32001, Message: "Authentication failed: PAT not configured"}}
	}

	baseCtx := context.Background()
	authCtx := clarifai.CreateContextWithAuth(baseCtx, h.pat)
	callTimeout := time.Duration(h.timeoutSec) * time.Second
	callCtx, cancel := context.WithTimeout(authCtx, callTimeout)
	defer cancel()

	var resourceJSON []byte
	var apiErr error
	var statusCode statuspb.StatusCode = statuspb.StatusCode_ZERO // Default to zero
	var resultContents []map[string]interface{}                   // To hold multiple results for list operations
	var nextCursor string                                         // For pagination in list operations

	// --- Determine Action based on Path Parts ---
	// pathParts: [app_id, resource_type, [resource_id], [sub_resource_type]]
	// Examples:
	// [app1, inputs, input123] -> GetInput
	// [app1, annotations] -> ListAnnotations
	// [app1, annotations, anno456] -> GetAnnotation
	// [app1, inputs, input123, annotations] -> ListAnnotationsForInput

	resourceType := pathParts[1] // e.g., inputs, annotations, models

	switch len(pathParts) {
	case 2: // List operation (e.g., clarifai://user/app/annotations)
		queryParams := parsedURI.Query()
		pageStr := queryParams.Get("page")
		perPageStr := queryParams.Get("per_page")
		query := queryParams.Get("query") // For search

		page, _ := strconv.ParseUint(pageStr, 10, 32)
		if page == 0 {
			page = 1
		}
		perPage, _ := strconv.ParseUint(perPageStr, 10, 32)
		if perPage == 0 {
			perPage = 20 // Default per page
		}
		if perPage > 1000 {
			perPage = 1000 // Max per page
		}

		// userAppIDSet := &pb.UserAppIDSet{UserId: userID, AppId: appID} // Commented out as it's unused due to placeholder below
		// pagination := &pb.Pagination{Page: uint32(page), PerPage: uint32(perPage)} // Removed unused variable

		switch resourceType {
		case "annotations":
			if query != "" {
				h.logger.Debug("Calling PostAnnotationsSearches", "user_id", userID, "app_id", appID, "page", page, "per_page", perPage, "query", query)
				// TODO: Implement PostAnnotationsSearches gRPC call
				// searchQueryProto := &pb.Query{ ... } // Construct query proto based on search_term
				// grpcRequest := &pb.PostAnnotationsSearchesRequest{UserAppId: userAppIDSet, Searches: []*pb.Search{{Query: searchQueryProto}}, Pagination: pagination}
				// resp, err := grpcClient.PostAnnotationsSearches(callCtx, grpcRequest)
				// apiErr = err
				// if err == nil { ... process resp.Hits ... }
				apiErr = fmt.Errorf("PostAnnotationsSearches (search with query) not yet implemented") // Placeholder
			} else {
				h.logger.Debug("Calling ListAnnotations", "user_id", userID, "app_id", appID, "page", page, "per_page", perPage)
				// TODO: Implement ListAnnotations gRPC call
				// grpcRequest := &pb.ListAnnotationsRequest{UserAppId: userAppIDSet, Page: uint32(page), PerPage: uint32(perPage)}
				// resp, err := grpcClient.ListAnnotations(callCtx, grpcRequest)
				// apiErr = err
				// if err == nil {
				// 	statusCode = resp.GetStatus().GetCode()
				apiErr = fmt.Errorf("ListAnnotations not yet implemented") // Placeholder
				/*
						if statusCode == statuspb.StatusCode_SUCCESS {
							resultContents = make([]map[string]interface{}, 0, len(resp.Annotations))
							for _, anno := range resp.Annotations {
								// Map annotation to resource content
								m := protojson.MarshalOptions{Indent: "  ", EmitUnpopulated: true}
								annoJSON, marshalErr := m.Marshal(anno)
								if marshalErr != nil {
									h.logger.Warn("Failed to marshal annotation", "id", anno.Id, "error", marshalErr)
									continue // Skip this annotation
								}
								resultContents = append(resultContents, map[string]interface{}{
									"uri":      fmt.Sprintf("clarifai://%s/%s/annotations/%s", userID, appID, anno.Id),
									"mimeType": "application/json",
									"text":     string(annoJSON),
								})
							}
							if uint32(len(resp.Annotations)) == uint32(perPage) {
								nextCursor = strconv.FormatUint(page+1, 10)
							}
						} else {
							apiErr = fmt.Errorf("API error: %s - %s", resp.GetStatus().GetDescription(), resp.GetStatus().GetDetails())
						}
					}
				*/
			}
		case "models":
			h.logger.Debug("Attempting to list models via resources/read", "user_id", userID, "app_id", appID, "page", page, "per_page", perPage, "query", query)
			if query != "" {
				h.logger.Warn("Query parameter is not yet supported for listing models via resources/read", "query", query)
				apiErr = fmt.Errorf("query parameter not supported for listing models")
			} else {
				// ListModels
				h.logger.Debug("Calling ListModels gRPC", "user_id", userID, "app_id", appID, "page", page, "per_page", perPage)
				userAppIDSet := &pb.UserAppIDSet{UserId: userID, AppId: appID}
				// pagination := &pb.Pagination{Page: uint32(page), PerPage: uint32(perPage)} // Removed unused variable
				grpcRequest := &pb.ListModelsRequest{UserAppId: userAppIDSet, Page: uint32(page), PerPage: uint32(perPage)} // Use correct page/perPage types
				resp, err := grpcClient.ListModels(callCtx, grpcRequest)
				apiErr = err
				if err == nil {
					statusCode = resp.GetStatus().GetCode()
					h.logger.Debug("ListModels gRPC call completed", "status_code", statusCode)
					if statusCode == statuspb.StatusCode_SUCCESS {
						resultContents = make([]map[string]interface{}, 0, len(resp.Models))
						for _, model := range resp.Models {
							// Map model to resource content
							m := protojson.MarshalOptions{Indent: "  ", EmitUnpopulated: true}
							modelJSON, marshalErr := m.Marshal(model)
							if marshalErr != nil {
								h.logger.Warn("Failed to marshal model", "id", model.Id, "error", marshalErr)
								continue // Skip this model
							}
							resultContents = append(resultContents, map[string]interface{}{
								"uri":      fmt.Sprintf("clarifai://%s/%s/models/%s", userID, appID, model.Id),
								"mimeType": "application/json",
								"text":     string(modelJSON),
								// Optionally add name/description directly to the list item
								"name":        model.Name,
								"description": model.Notes,
							})
						}
						if uint32(len(resp.Models)) == uint32(perPage) {
							nextCursor = strconv.FormatUint(page+1, 10)
						}
						h.logger.Debug("Successfully processed ListModels response", "count", len(resultContents), "next_cursor", nextCursor)
					} else {
						apiErr = fmt.Errorf("API error: %s - %s", resp.GetStatus().GetDescription(), resp.GetStatus().GetDetails())
						h.logger.Error("ListModels API error", "description", resp.GetStatus().GetDescription(), "details", resp.GetStatus().GetDetails())
					}
				} else {
					h.logger.Error("ListModels gRPC call failed", "error", apiErr)
				}
			}
		// Add other listable types here (e.g., datasets)
		default:
			apiErr = fmt.Errorf("listing resource type '%s' via resources/read is not supported or implemented", resourceType)
			h.logger.Error("Unsupported resource type for list via resources/read", "resource_type", resourceType)
		}

	case 3: // Get specific resource (e.g., clarifai://user/app/inputs/input123)
		resourceID := pathParts[2]
		if resourceID == "" || resourceID == "*" {
			apiErr = fmt.Errorf("invalid URI for specific resource read: resource ID cannot be empty or '*'")
			break // Exit switch
		}
		userAppIDSet := &pb.UserAppIDSet{UserId: userID, AppId: appID}

		switch resourceType {
		case "inputs":
			h.logger.Debug("Calling GetInput", "user_id", userID, "app_id", appID, "input_id", resourceID)
			grpcRequest := &pb.GetInputRequest{UserAppId: userAppIDSet, InputId: resourceID}
			resp, err := grpcClient.GetInput(callCtx, grpcRequest)
			apiErr = err
			if err == nil {
				statusCode = resp.GetStatus().GetCode()
				if statusCode == statuspb.StatusCode_SUCCESS && resp.Input != nil {
					m := protojson.MarshalOptions{Indent: "  ", EmitUnpopulated: true}
					resourceJSON, apiErr = m.Marshal(resp.Input)
				} else if statusCode != statuspb.StatusCode_SUCCESS {
					apiErr = fmt.Errorf("API error: %s - %s", resp.GetStatus().GetDescription(), resp.GetStatus().GetDetails())
				} else {
					apiErr = fmt.Errorf("API response did not contain input data")
				}
			}
		case "models":
			h.logger.Debug("Calling GetModel", "user_id", userID, "app_id", appID, "model_id", resourceID)
			grpcRequest := &pb.GetModelRequest{UserAppId: userAppIDSet, ModelId: resourceID}
			resp, err := h.clarifaiClient.API.GetModel(callCtx, grpcRequest)
			apiErr = err
			if err == nil {
				statusCode = resp.GetStatus().GetCode()
				if statusCode == statuspb.StatusCode_SUCCESS && resp.Model != nil {
					m := protojson.MarshalOptions{Indent: "  ", EmitUnpopulated: true}
					resourceJSON, apiErr = m.Marshal(resp.Model)
				} else if statusCode != statuspb.StatusCode_SUCCESS {
					apiErr = fmt.Errorf("API error: %s - %s", resp.GetStatus().GetDescription(), resp.GetStatus().GetDetails())
				} else {
					apiErr = fmt.Errorf("API response did not contain model data")
				}
			}
		case "annotations":
			h.logger.Debug("Calling GetAnnotation", "user_id", userID, "app_id", appID, "annotation_id", resourceID)
			// TODO: Implement GetAnnotation gRPC call
			// grpcRequest := &pb.GetAnnotationRequest{UserAppId: userAppIDSet, AnnotationId: resourceID}
			// resp, err := grpcClient.GetAnnotation(callCtx, grpcRequest)
			// apiErr = err
			// if err == nil {
			// 	statusCode = resp.GetStatus().GetCode()
			apiErr = fmt.Errorf("GetAnnotation not yet implemented") // Placeholder
			/*
					if statusCode == statuspb.StatusCode_SUCCESS && resp.Annotation != nil {
						m := protojson.MarshalOptions{Indent: "  ", EmitUnpopulated: true}
						resourceJSON, apiErr = m.Marshal(resp.Annotation)
					} else if statusCode != statuspb.StatusCode_SUCCESS {
						apiErr = fmt.Errorf("API error: %s - %s", resp.GetStatus().GetDescription(), resp.GetStatus().GetDetails())
					} else {
						apiErr = fmt.Errorf("API response did not contain annotation data")
					}
				}
			*/
		// Add other gettable types here (e.g., datasets)
		default:
			apiErr = fmt.Errorf("reading specific resource type '%s' is not supported or implemented", resourceType)
		}

	case 4: // List sub-resource (e.g., clarifai://user/app/inputs/input123/annotations)
		parentResourceType := pathParts[1]
		parentResourceID := pathParts[2]
		subResourceType := pathParts[3]

		if parentResourceID == "" || parentResourceID == "*" {
			apiErr = fmt.Errorf("invalid URI for sub-resource list: parent resource ID cannot be empty or '*'")
			break // Exit switch
		}

		queryParams := parsedURI.Query()
		pageStr := queryParams.Get("page")
		perPageStr := queryParams.Get("per_page")

		page, _ := strconv.ParseUint(pageStr, 10, 32)
		if page == 0 {
			page = 1
		}
		perPage, _ := strconv.ParseUint(perPageStr, 10, 32)
		if perPage == 0 {
			perPage = 20 // Default per page
		}
		if perPage > 1000 {
			perPage = 1000 // Max per page
		}

		// userAppIDSet := &pb.UserAppIDSet{UserId: userID, AppId: appID} // Commented out as it's unused due to placeholder below
		// pagination := &pb.Pagination{Page: uint32(page), PerPage: uint32(perPage)} // Already defined above

		switch parentResourceType {
		case "inputs":
			if subResourceType == "annotations" {
				h.logger.Debug("Calling ListAnnotations for Input", "user_id", userID, "app_id", appID, "input_id", parentResourceID, "page", page, "per_page", perPage)
				// TODO: Implement ListAnnotations gRPC call with input_id filter
				// grpcRequest := &pb.ListAnnotationsRequest{UserAppId: userAppIDSet, InputIds: []string{parentResourceID}, Page: uint32(page), PerPage: uint32(perPage)}
				// resp, err := grpcClient.ListAnnotations(callCtx, grpcRequest)
				// apiErr = err
				// if err == nil {
				// 	statusCode = resp.GetStatus().GetCode()
				apiErr = fmt.Errorf("ListAnnotations for Input not yet implemented") // Placeholder
				/*
						if statusCode == statuspb.StatusCode_SUCCESS {
							resultContents = make([]map[string]interface{}, 0, len(resp.Annotations))
							for _, anno := range resp.Annotations {
								// Map annotation to resource content
								m := protojson.MarshalOptions{Indent: "  ", EmitUnpopulated: true}
								annoJSON, marshalErr := m.Marshal(anno)
								if marshalErr != nil {
									h.logger.Warn("Failed to marshal annotation", "id", anno.Id, "error", marshalErr)
									continue // Skip this annotation
								}
								resultContents = append(resultContents, map[string]interface{}{
									"uri":      fmt.Sprintf("clarifai://%s/%s/annotations/%s", userID, appID, anno.Id), // URI of the annotation itself
									"mimeType": "application/json",
									"text":     string(annoJSON),
								})
							}
							if uint32(len(resp.Annotations)) == uint32(perPage) {
								nextCursor = strconv.FormatUint(page+1, 10)
							}
						} else {
							apiErr = fmt.Errorf("API error: %s - %s", resp.GetStatus().GetDescription(), resp.GetStatus().GetDetails())
						}
					}
				*/
			} else {
				apiErr = fmt.Errorf("unsupported sub-resource type '%s' for parent '%s'", subResourceType, parentResourceType)
			}
		// Add other parent types if needed (e.g., models/{model_id}/versions)
		default:
			apiErr = fmt.Errorf("listing sub-resources for parent type '%s' is not supported or implemented", parentResourceType)
		}

	default:
		h.logger.Warn("Invalid URI path format for resources/read", "path", parsedURI.Path, "parts", len(pathParts))
		apiErr = fmt.Errorf("invalid URI format for resources/read. Unexpected number of path segments: %d", len(pathParts))
	}

	// --- Handle Errors & Return Response ---
	if apiErr != nil {
		h.logger.Error("Error during resources/read", "uri", request.Params.URI, "error", apiErr)
		// Specific Not Found check
		if statusCode == statuspb.StatusCode_INPUT_DOES_NOT_EXIST ||
			statusCode == statuspb.StatusCode_MODEL_DOES_NOT_EXIST { // Removed ANNOTATION_DOES_NOT_EXIST as it's not defined
			// TODO: Add specific status codes for other resource types (e.g., annotations) when implemented
			return mcp.JSONRPCResponse{
				JSONRPC: "2.0", ID: request.ID,
				Error: &mcp.RPCError{Code: -32002, Message: "Resource not found", Data: request.Params.URI},
			}
		}
		// Map gRPC errors
		grpcErr := clarifai.MapGRPCErrorToJSONRPC(apiErr)
		if grpcErr != nil {
			return mcp.JSONRPCResponse{JSONRPC: "2.0", ID: request.ID, Error: grpcErr}
		}
		// Generic internal error
		return mcp.JSONRPCResponse{
			JSONRPC: "2.0", ID: request.ID,
			Error: &mcp.RPCError{Code: -32000, Message: fmt.Sprintf("Failed to read resource: %v", apiErr)},
		}
	}

	// --- Construct Success Response ---
	mimeType := "application/json" // We are returning JSON

	// Handle single resource vs list results
	if resourceJSON != nil { // Single resource was read
		resultContents = []map[string]interface{}{
			{
				"uri":      request.Params.URI,
				"mimeType": mimeType,
				"text":     string(resourceJSON),
			},
		}
	} else if len(resultContents) > 0 { // List operation was performed
		// resultContents was populated by the list logic above
		h.logger.Debug("Successfully listed resources via read", "uri", request.Params.URI, "count", len(resultContents), "next_cursor", nextCursor)
	} else if len(pathParts) == 2 || len(pathParts) == 4 { // List operation returned empty results
		h.logger.Debug("List operation via read returned no results", "uri", request.Params.URI)
		resultContents = []map[string]interface{}{} // Return empty list explicitly
	} else {
		// Should not happen if apiErr is nil for a Get operation, but handle defensively
		h.logger.Error("No error but also no content generated for resources/read Get operation", "uri", request.Params.URI)
		return mcp.JSONRPCResponse{
			JSONRPC: "2.0", ID: request.ID,
			Error: &mcp.RPCError{Code: -32000, Message: "Internal error: Failed to generate content for resource read"},
		}
	}

	responseResult := map[string]interface{}{
		"contents": resultContents,
	}
	if nextCursor != "" {
		responseResult["nextCursor"] = nextCursor
	}

	h.logger.Debug("Successfully processed resources/read request", "uri", request.Params.URI)
	return mcp.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
		Result:  responseResult,
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

	h.logger.Debug("Calling PostModelOutputs for infer_image", "user_id", userID, "app_id", appID, "model_id", modelID) // Changed to Debug

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

	h.logger.Debug("Inference successful", "concepts_found", len(conceptStrings)) // Changed to Debug

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

// --- Tool Implementation for list_models (REMOVED) ---
// func (h *Handler) callListModels(args map[string]interface{}) (interface{}, *mcp.RPCError) { ... }

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

	h.logger.Debug("Calling PostModelOutputs for generate_image", "user_id", userID, "app_id", appID, "model_id", modelID) // Changed to Debug

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
	h.logger.Debug("Successfully generated image", "size_bytes", len(imageBase64Bytes)) // Changed to Debug

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
		h.logger.Debug("Successfully saved image to disk via utility function", "path", savedPath) // Changed to Debug
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
