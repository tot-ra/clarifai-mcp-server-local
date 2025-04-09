package tools

import (
	"context"
	"encoding/json" // Added for marshalling input data in resource read
	"fmt"           // Added for Sprintf
	"log/slog"      // Use slog
	"net/url"       // Added for URI parsing
	"strconv"       // Added for pagination parsing
	"strings"
	"time"

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
		response = h.handleListResources(request)
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

func (h *Handler) handleListResources(request mcp.JSONRPCRequest) mcp.JSONRPCResponse {
	// This method is intentionally disabled to force clients to use specific resource URIs
	// obtained from resources/templates/list. Listing resources without a specific URI
	// (e.g., relying on default user/app) is no longer supported directly via this method.
	h.logger.Debug("Handling resources/list request", "id", request.ID, "uri", request.Params.URI, "cursor", request.Params.Cursor)

	var userID, appID, query string
	var page, perPage uint32 = 1, 20 // Default pagination

	// --- Parameter Extraction ---
	if request.Params.URI == "" {
		// No URI provided, try using default config
		if h.config.DefaultUserID == "" || h.config.DefaultAppID == "" {
			h.logger.Warn("resources/list called without URI and default user/app not configured.", "id", request.ID)
			return mcp.JSONRPCResponse{
				JSONRPC: "2.0", ID: request.ID,
				Error: &mcp.RPCError{Code: -32602, Message: "Missing resource URI and default user/app not configured. Use resources/templates/list or provide a specific URI."},
			}
		}
		userID = h.config.DefaultUserID
		appID = h.config.DefaultAppID
		h.logger.Debug("Using default user/app ID for resources/list", "user_id", userID, "app_id", appID)
		// No query possible without URI
	} else {
		// Parse the provided URI
		parsedURI, err := url.Parse(request.Params.URI)
		if err != nil || parsedURI.Scheme != "clarifai" {
			h.logger.Warn("Invalid URI format for resources/list", "uri", request.Params.URI, "error", err)
			return mcp.JSONRPCResponse{
				JSONRPC: "2.0", ID: request.ID,
				Error: &mcp.RPCError{Code: -32602, Message: "Invalid URI format. Expected clarifai://{user_id}/{app_id}/inputs[?query=...]"},
			}
		}

		// Expected path format: /{user_id}/{app_id}/inputs
		pathParts := strings.Split(strings.TrimPrefix(parsedURI.Path, "/"), "/")
		// Allow path ending only in /inputs for now
		// TODO: Extend to support other listable resource types (models, datasets, etc.)
		if len(pathParts) != 3 || pathParts[2] != "inputs" {
			h.logger.Warn("Invalid URI path format for resources/list", "path", parsedURI.Path)
			return mcp.JSONRPCResponse{
				JSONRPC: "2.0", ID: request.ID,
				Error: &mcp.RPCError{Code: -32602, Message: "Invalid URI path. Expected /{user_id}/{app_id}/inputs"},
			}
		}
		userID = pathParts[0]
		appID = pathParts[1]

		// Extract query parameters
		queryParams := parsedURI.Query()
		query = queryParams.Get("query") // Get search query if present

		// Handle pagination from query params OR cursor
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
					// TODO: Add max per_page limit? (e.g., 1000)
					if perPageInt > 1000 {
						perPageInt = 1000
					} // Example limit
					perPage = uint32(perPageInt)
				}
			}
		}
	}

	// --- gRPC Call ---
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

	if query != "" {
		// --- Call PostInputsSearches ---
		h.logger.Debug("Calling PostInputsSearches", "user_id", userID, "app_id", appID, "query", query, "page", page, "per_page", perPage)
		// Construct search query (simple text search for now)
		// TODO: Support more complex search queries if needed
		// searchQueryStruct, _ := structpb.NewStruct(map[string]interface{}{"text": query}) // Basic text search - Unused
		searchQuery := &pb.Query{
			// Filters: []*pb.Filter{
			// 	{Annotation: &pb.Annotation{Data: &pb.Data{Text: &pb.Text{Raw: query}}}},
			// },
			// Use Ranks for similarity search
			Ranks: []*pb.Rank{
				{Annotation: &pb.Annotation{Data: &pb.Data{Text: &pb.Text{Raw: query}}}},
			},
			// Example for searching by concept:
			// Filters: []*pb.Filter{
			// 	{Annotation: &pb.Annotation{Data: &pb.Data{Concepts: []*pb.Concept{{Name: query, Value: 1}}}}},
			// },
		}
		grpcRequest := &pb.PostInputsSearchesRequest{
			UserAppId:  userAppIDSet,
			Searches:   []*pb.Search{{Query: searchQuery}},
			Pagination: pagination,
		}

		resp, err := grpcClient.PostInputsSearches(callCtx, grpcRequest)
		apiErr = err // Store potential gRPC error

		if err == nil {
			if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
				h.logger.Error("gRPC PostInputsSearches non-success status", "status", resp.GetStatus())
				return mcp.JSONRPCResponse{
					JSONRPC: "2.0", ID: request.ID,
					Error: &mcp.RPCError{Code: -32000, Message: resp.GetStatus().GetDescription(), Data: resp.GetStatus().GetDetails()},
				}
			}
			// Process Hits
			resources = make([]map[string]interface{}, 0, len(resp.Hits))
			for _, hit := range resp.Hits {
				if hit.Input != nil {
					resources = append(resources, mapInputToResource(hit.Input, userID, appID))
				}
			}
			// Determine next cursor if more results might exist
			if uint32(len(resp.Hits)) == perPage {
				nextCursor = strconv.Itoa(int(page + 1))
			}
		}

	} else {
		// --- Call ListInputs ---
		h.logger.Debug("Calling ListInputs", "user_id", userID, "app_id", appID, "page", page, "per_page", perPage)
		grpcRequest := &pb.ListInputsRequest{
			UserAppId: userAppIDSet,
			Page:      page,
			PerPage:   perPage,
		}

		resp, err := grpcClient.ListInputs(callCtx, grpcRequest)
		apiErr = err // Store potential gRPC error

		if err == nil {
			if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
				h.logger.Error("gRPC ListInputs non-success status", "status", resp.GetStatus())
				return mcp.JSONRPCResponse{
					JSONRPC: "2.0", ID: request.ID,
					Error: &mcp.RPCError{Code: -32000, Message: resp.GetStatus().GetDescription(), Data: resp.GetStatus().GetDetails()},
				}
			}
			// Process Inputs
			resources = make([]map[string]interface{}, 0, len(resp.Inputs))
			for _, input := range resp.Inputs {
				resources = append(resources, mapInputToResource(input, userID, appID))
			}
			// Determine next cursor if more results might exist
			if uint32(len(resp.Inputs)) == perPage {
				nextCursor = strconv.Itoa(int(page + 1))
			}
		}
	}

	// Handle gRPC errors from either call
	if apiErr != nil {
		h.logger.Error("gRPC error during resources/list", "error", apiErr)
		return mcp.JSONRPCResponse{JSONRPC: "2.0", ID: request.ID, Error: clarifai.MapGRPCErrorToJSONRPC(apiErr)}
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

func (h *Handler) handleReadResource(request mcp.JSONRPCRequest) mcp.JSONRPCResponse {
	h.logger.Debug("Handling resources/read request", "id", request.ID, "uri", request.Params.URI) // Changed to Debug

	// --- Parameter Extraction ---
	if request.Params.URI == "" {
		return mcp.JSONRPCResponse{
			JSONRPC: "2.0", ID: request.ID,
			Error: &mcp.RPCError{Code: -32602, Message: "Missing required 'uri' parameter"},
		}
	}

	parsedURI, err := url.Parse(request.Params.URI)
	if err != nil || parsedURI.Scheme != "clarifai" {
		h.logger.Warn("Invalid URI format for resources/read", "uri", request.Params.URI, "error", err)
		return mcp.JSONRPCResponse{
			JSONRPC: "2.0", ID: request.ID,
			Error: &mcp.RPCError{Code: -32602, Message: "Invalid URI format. Expected clarifai://{user_id}/{app_id}/inputs/{input_id}"},
		}
	}

	// Expected path format: /{user_id}/{app_id}/inputs/{input_id}
	pathParts := strings.Split(strings.TrimPrefix(parsedURI.Path, "/"), "/")
	if len(pathParts) != 4 || pathParts[2] != "inputs" || pathParts[3] == "" {
		h.logger.Warn("Invalid URI path format for resources/read", "path", parsedURI.Path)
		return mcp.JSONRPCResponse{
			JSONRPC: "2.0", ID: request.ID,
			Error: &mcp.RPCError{Code: -32602, Message: "Invalid URI path. Expected /{user_id}/{app_id}/inputs/{input_id}"},
		}
	}
	userID := pathParts[0]
	appID := pathParts[1]
	inputID := pathParts[3]

	// --- gRPC Call ---
	grpcClient := h.clarifaiClient.API
	if grpcClient == nil {
		return mcp.JSONRPCResponse{JSONRPC: "2.0", ID: request.ID, Error: &mcp.RPCError{Code: -32001, Message: "Clarifai client not initialized"}}
	}
	if h.pat == "" {
		return mcp.JSONRPCResponse{JSONRPC: "2.0", ID: request.ID, Error: &mcp.RPCError{Code: -32001, Message: "Authentication failed: PAT not configured"}}
	}

	h.logger.Debug("Calling GetInput", "user_id", userID, "app_id", appID, "input_id", inputID) // Changed to Debug

	grpcRequest := &pb.GetInputRequest{
		UserAppId: &pb.UserAppIDSet{UserId: userID, AppId: appID},
		InputId:   inputID,
	}

	baseCtx := context.Background()
	authCtx := clarifai.CreateContextWithAuth(baseCtx, h.pat)
	callTimeout := time.Duration(h.timeoutSec) * time.Second
	callCtx, cancel := context.WithTimeout(authCtx, callTimeout)
	defer cancel()

	resp, err := grpcClient.GetInput(callCtx, grpcRequest)

	if err != nil {
		h.logger.Error("gRPC GetInput error", "error", err)
		return mcp.JSONRPCResponse{JSONRPC: "2.0", ID: request.ID, Error: clarifai.MapGRPCErrorToJSONRPC(err)}
	}
	if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
		h.logger.Error("gRPC GetInput non-success status", "status", resp.GetStatus())
		// Specific check for Not Found
		if resp.GetStatus().GetCode() == statuspb.StatusCode_INPUT_DOES_NOT_EXIST {
			return mcp.JSONRPCResponse{
				JSONRPC: "2.0", ID: request.ID,
				Error: &mcp.RPCError{Code: -32002, Message: "Resource not found", Data: request.Params.URI}, // Use MCP Not Found code
			}
		}
		return mcp.JSONRPCResponse{
			JSONRPC: "2.0", ID: request.ID,
			Error: &mcp.RPCError{Code: -32000, Message: resp.GetStatus().GetDescription(), Data: resp.GetStatus().GetDetails()},
		}
	}
	if resp.Input == nil {
		h.logger.Error("gRPC GetInput response missing input data")
		return mcp.JSONRPCResponse{
			JSONRPC: "2.0", ID: request.ID,
			Error: &mcp.RPCError{Code: -32000, Message: "API response did not contain input data"},
		}
	}

	// --- Process Result ---
	// Marshal the Input object to JSON string
	inputJSON, err := json.MarshalIndent(resp.Input, "", "  ") // Use MarshalIndent for readability
	if err != nil {
		h.logger.Error("Failed to marshal input data to JSON", "error", err)
		return mcp.JSONRPCResponse{
			JSONRPC: "2.0", ID: request.ID,
			Error: &mcp.RPCError{Code: -32000, Message: "Failed to process resource data"},
		}
	}

	h.logger.Debug("Successfully read resource", "uri", request.Params.URI) // Changed to Debug

	// Determine mimeType (consistent with listResources)
	mimeType := "application/json" // We are returning JSON

	return mcp.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
		Result: map[string]interface{}{
			"contents": []map[string]interface{}{
				{
					"uri":      request.Params.URI,
					"mimeType": mimeType,
					"text":     string(inputJSON),
				},
			},
		},
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
