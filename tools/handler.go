package tools

import (
	"context"       // Needed for image encoding
	"encoding/json" // Import standard JSON package
	"fmt"
	"log/slog"
	"net/url"
	"os" // Needed for reading files
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"clarifai-mcp-server-local/clarifai"
	"clarifai-mcp-server-local/config"
	"clarifai-mcp-server-local/mcp"
	"clarifai-mcp-server-local/utils"

	pb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api"
	statuspb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api/status"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb" // Needed for FilteredModelInfo
)

// Function to write detailed errors to a log file
func logErrorToFile(err error, context map[string]string, rpcErr *mcp.RPCError) {
	// Use an absolute path to avoid CWD issues
	logFilePath := "/Users/artjom/work/clarifai-mcp-server-local/error.log"
	file, openErr := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if openErr != nil {
		slog.Error("Failed to open error log file", "path", logFilePath, "error", openErr)
		return // Cannot log to file, rely on standard slog
	}
	defer file.Close()

	timestamp := time.Now().Format(time.RFC3339)
	contextJSON, _ := json.Marshal(context)
	rpcErrJSON, _ := json.Marshal(rpcErr)

	logEntry := fmt.Sprintf("[%s] OriginalError: %v | Context: %s | MappedRPCError: %s\n",
		timestamp,
		err,
		string(contextJSON),
		string(rpcErrJSON),
	)

	if _, writeErr := file.WriteString(logEntry); writeErr != nil {
		slog.Error("Failed to write to error log file", "path", logFilePath, "error", writeErr)
	}
}

// FilteredModelInfo defines the subset of fields to return for list operations.
type FilteredModelInfo struct {
	ID           string                    `json:"id"`
	Name         string                    `json:"name"`
	CreatedAt    *timestamppb.Timestamp    `json:"createdAt"`
	AppID        string                    `json:"appId"`
	UserID       string                    `json:"userId"`
	ModelTypeID  string                    `json:"modelTypeId"`
	Description  string                    `json:"description,omitempty"`
	Visibility   *pb.Visibility            `json:"visibility,omitempty"`
	ModelVersion *FilteredModelVersionInfo `json:"modelVersion,omitempty"`
	DisplayName  string                    `json:"displayName,omitempty"`
	Task         string                    `json:"task,omitempty"`
	Toolkits     []string                  `json:"toolkits,omitempty"`
	UseCases     []string                  `json:"useCases,omitempty"`
	IsStarred    bool                      `json:"isStarred"`
	StarCount    int32                     `json:"starCount"` // Corrected type
	Image        *pb.Image                 `json:"image,omitempty"`
}

// FilteredModelVersionInfo defines the subset of model version fields.
type FilteredModelVersionInfo struct {
	ID                 string                 `json:"id"`
	CreatedAt          *timestamppb.Timestamp `json:"createdAt"`
	Status             *statuspb.Status       `json:"status,omitempty"`
	ActiveConceptCount uint32                 `json:"activeConceptCount"`
	Metrics            *pb.MetricsSummary     `json:"metricsSummary,omitempty"` // Use MetricsSummary
	Description        string                 `json:"description,omitempty"`
	Visibility         *pb.Visibility         `json:"visibility,omitempty"`
	AppID              string                 `json:"appId"`
	UserID             string                 `json:"userId"`
	License            string                 `json:"license,omitempty"`
	OutputInfo         *pb.OutputInfo         `json:"outputInfo,omitempty"` // Keep output info
	InputInfo          *pb.InputInfo          `json:"inputInfo,omitempty"`  // Keep input info
	// TrainInfo is intentionally omitted
}

type Handler struct {
	clarifaiClient *clarifai.Client
	pat            string
	outputPath     string
	timeoutSec     int
	logger         *slog.Logger
	config         *config.Config
}

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

// Updated toolsDefinitionMap
var toolsDefinitionMap = map[string]interface{}{
	"clarifai_image_by_path": map[string]interface{}{
		"description": "Performs inference on a local image file using a specified or default Clarifai model. Defaults to 'general-image-detection' model if none specified.",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"filepath": map[string]interface{}{
					"type":        "string",
					"description": "Absolute path to the local image file.", // Updated description
				},
				"model_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional: Specific model ID to use. Defaults to 'general-image-detection' if omitted.", // Updated default model name
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
}

var resourceTemplates = []map[string]interface{}{
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
	{
		"uriTemplate": "clarifai://{user_id}/{app_id}/datasets/{dataset_id}/versions",
		"name":        "List Dataset Versions",
		"description": "List versions for a specific dataset.",
		"mimeType":    "application/json",
	},
}

func (h *Handler) HandleRequest(request mcp.JSONRPCRequest) *mcp.JSONRPCResponse {
	if strings.HasPrefix(request.Method, "notifications/") {
		h.logger.Debug("Ignoring notification", "method", request.Method)
		return nil
	}
	h.logger.Debug("Handling request", "method", request.Method, "id", request.ID)

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
	case "resources/list": // Note: resources/list should ideally call handleListResource, but current code calls handleReadResource. Keeping as is for now.
		response = h.handleReadResource(request)
	case "resources/read":
		response = h.handleReadResource(request)
	default:
		response = mcp.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Error:   &mcp.RPCError{Code: -32601, Message: "Method not found", Data: request.Method},
		}
	}
	return &response
}

func (h *Handler) handleInitialize(request mcp.JSONRPCRequest) mcp.JSONRPCResponse {
	h.logger.Debug("Handling initialize request", "id", request.ID)
	return mcp.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
		Result: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"serverInfo": map[string]interface{}{
				"name":    "clarifai-mcp-bridge",
				"version": "0.1.0",
			},
			"capabilities": map[string]interface{}{
				"tools":             map[string]interface{}{},
				"resources":         map[string]interface{}{},
				"resourceTemplates": map[string]interface{}{"templates": resourceTemplates},
				"experimental":      map[string]any{},
				"prompts":           map[string]any{"listChanged": false},
			},
		},
	}
}

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

func (h *Handler) handleListResourceTemplates(request mcp.JSONRPCRequest) mcp.JSONRPCResponse {
	h.logger.Debug("Handling resources/templates/list request", "id", request.ID)
	return mcp.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
		Result:  map[string]interface{}{"resourceTemplates": resourceTemplates},
	}
}

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

func (h *Handler) handleReadResource(request mcp.JSONRPCRequest) mcp.JSONRPCResponse {
	h.logger.Debug("Handling resources/read request", "id", request.ID, "uri", request.Params.URI)

	if request.Params.URI == "" {
		return mcp.NewErrorResponse(request.ID, -32602, "Missing required 'uri' parameter", nil)
	}

	parsedURI, err := url.Parse(request.Params.URI)
	if err != nil || parsedURI.Scheme != "clarifai" {
		h.logger.Warn("Invalid URI scheme for resources/read", "uri", request.Params.URI, "error", err)
		return mcp.NewErrorResponse(request.ID, -32602, "Invalid URI format. Expected clarifai://...", nil)
	}

	userID := parsedURI.Host
	if userID == "" {
		h.logger.Warn("Invalid URI format: Missing user_id (host part)", "uri", request.Params.URI)
		return mcp.NewErrorResponse(request.ID, -32602, "Invalid URI format: Missing user_id. Expected clarifai://{user_id}/{app_id}/...", nil)
	}

	trimmedPath := strings.TrimPrefix(parsedURI.Path, "/")
	pathParts := strings.Split(trimmedPath, "/")

	if len(pathParts) < 2 {
		h.logger.Warn("Invalid URI path format (too few path parts)", "path", parsedURI.Path, "parts", len(pathParts))
		return mcp.NewErrorResponse(request.ID, -32602, fmt.Sprintf("Invalid URI path format. Expected at least clarifai://{user_id}/{app_id}/{resource_type}, got %d parts", len(pathParts)), nil)
	}

	appID := pathParts[0]
	if appID == "" {
		h.logger.Warn("Invalid URI format: Missing app_id", "uri", request.Params.URI)
		return mcp.NewErrorResponse(request.ID, -32602, "Invalid URI format: Missing app_id. Expected clarifai://{user_id}/{app_id}/...", nil)
	}

	resourceType := pathParts[1]
	h.logger.Debug("Parsed resource details", "userID", userID, "appID", appID, "resourceType", resourceType, "pathPartsCount", len(pathParts))

	switch len(pathParts) {
	case 2: // List operation (e.g., clarifai://user/app/inputs)
		return h.handleListResource(request, userID, appID, resourceType, "", "", parsedURI.Query())
	case 3: // Get specific resource (e.g., clarifai://user/app/inputs/input123)
		resourceID := pathParts[2]
		if resourceID == "" || resourceID == "*" {
			return mcp.NewErrorResponse(request.ID, -32602, "Invalid URI for specific resource read: resource ID cannot be empty or '*'", nil)
		}
		return h.handleGetResource(request, userID, appID, resourceType, resourceID)
	case 4: // List sub-resource (e.g., clarifai://user/app/inputs/input123/annotations)
		parentResourceType := pathParts[1]
		parentResourceID := pathParts[2]
		subResourceType := pathParts[3]
		if parentResourceID == "" || parentResourceID == "*" {
			return mcp.NewErrorResponse(request.ID, -32602, "Invalid URI for sub-resource list: parent resource ID cannot be empty or '*'", nil)
		}
		return h.handleListResource(request, userID, appID, subResourceType, parentResourceType, parentResourceID, parsedURI.Query())
	default:
		h.logger.Warn("Invalid URI path format", "path", parsedURI.Path, "parts", len(pathParts))
		return mcp.NewErrorResponse(request.ID, -32602, fmt.Sprintf("Invalid URI format. Unexpected number of path segments: %d", len(pathParts)), nil)
	}
}

func (h *Handler) handleGetResource(request mcp.JSONRPCRequest, userID, appID, resourceType, resourceID string) mcp.JSONRPCResponse {
	h.logger.Debug("Handling GetResource", "userID", userID, "appID", appID, "resourceType", resourceType, "resourceID", resourceID)

	ctx, cancel, rpcErr := h.prepareGrpcCall(context.Background())
	if rpcErr != nil {
		return mcp.NewErrorResponse(request.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
	}
	defer cancel()

	userAppIDSet := &pb.UserAppIDSet{UserId: userID, AppId: appID}
	var resourceProto proto.Message
	var apiErr error
	var errCtx = map[string]string{"userID": userID, "appID": appID, "resourceType": resourceType, "resourceID": resourceID}

	switch resourceType {
	case "inputs":
		resourceProto, apiErr = h.getInput(ctx, userAppIDSet, resourceID)
	case "models":
		resourceProto, apiErr = h.getModel(ctx, userAppIDSet, resourceID)
	case "annotations":
		apiErr = fmt.Errorf("GetAnnotation not yet implemented")
	default:
		apiErr = fmt.Errorf("reading specific resource type '%s' is not supported or implemented", resourceType)
	}

	if apiErr != nil {
		rpcErr = h.handleApiError(apiErr, errCtx)
		return mcp.NewErrorResponse(request.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
	}

	if resourceProto == nil {
		rpcErr = h.handleApiError(fmt.Errorf("API response did not contain data"), errCtx)
		return mcp.NewErrorResponse(request.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
	}

	m := protojson.MarshalOptions{Indent: "  ", EmitUnpopulated: true}
	resourceJSON, marshalErr := m.Marshal(resourceProto)
	if marshalErr != nil {
		h.logger.Error("Failed to marshal resource proto", "error", marshalErr, "resourceType", resourceType, "resourceID", resourceID)
		rpcErr = h.handleApiError(fmt.Errorf("failed to marshal resource data: %w", marshalErr), errCtx)
		return mcp.NewErrorResponse(request.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
	}

	resultContents := []map[string]interface{}{
		{
			"uri":      request.Params.URI,
			"mimeType": "application/json",
			"text":     string(resourceJSON),
		},
	}

	h.logger.Debug("Successfully processed GetResource request", "uri", request.Params.URI)
	return mcp.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
		Result:  map[string]interface{}{"contents": resultContents},
	}
}

func (h *Handler) handleListResource(request mcp.JSONRPCRequest, userID, appID, resourceType, parentType, parentID string, queryParams url.Values) mcp.JSONRPCResponse {
	h.logger.Debug("Handling ListResource", "userID", userID, "appID", appID, "resourceType", resourceType, "parentType", parentType, "parentID", parentID, "queryParams", queryParams)

	ctx, cancel, rpcErr := h.prepareGrpcCall(context.Background())
	if rpcErr != nil {
		return mcp.NewErrorResponse(request.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
	}
	defer cancel()

	page, perPage := parsePagination(queryParams, request.Params.Cursor, h.logger)
	query := queryParams.Get("query")
	userAppIDSet := &pb.UserAppIDSet{UserId: userID, AppId: appID}
	pagination := &pb.Pagination{Page: page, PerPage: perPage}

	var results []proto.Message
	var nextCursor string
	var apiErr error
	var errCtx = map[string]string{
		"userID":       userID,
		"appID":        appID,
		"resourceType": resourceType,
		"parentType":   parentType,
		"parentID":     parentID,
		"query":        query,
	}

	switch resourceType {
	case "inputs":
		if parentType != "" {
			apiErr = fmt.Errorf("listing inputs as sub-resource is not supported")
		} else {
			results, nextCursor, apiErr = h.listInputs(ctx, userAppIDSet, pagination, query)
		}
	case "models":
		if parentType != "" {
			apiErr = fmt.Errorf("listing models as sub-resource is not supported")
		} else {
			results, nextCursor, apiErr = h.listModels(ctx, userAppIDSet, pagination, query)
		}
	case "annotations":
		if parentType == "inputs" && parentID != "" {
			apiErr = fmt.Errorf("ListAnnotations for Input not yet implemented")
		} else if parentType == "" {
			apiErr = fmt.Errorf("ListAnnotations not yet implemented")
		} else {
			apiErr = fmt.Errorf("listing annotations under parent type '%s' is not supported", parentType)
		}
	default:
		apiErr = fmt.Errorf("listing resource type '%s' is not supported or implemented", resourceType)
	}

	if apiErr != nil {
		rpcErr = h.handleApiError(apiErr, errCtx)
		return mcp.NewErrorResponse(request.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
	}

	resultContents := make([]map[string]interface{}, 0, len(results))
	// Use standard JSON marshaller for potentially filtered structs
	jsonMarshaller := json.MarshalIndent

	for _, item := range results {
		var itemID, itemName, itemDesc string
		var itemURI string
		var marshaledJSON []byte // Will hold the JSON for the response
		var marshalErr error

		switch v := item.(type) {
		case *pb.Input:
			itemID = v.Id
			itemName = v.Id
			itemURI = fmt.Sprintf("clarifai://%s/%s/inputs/%s", userID, appID, itemID)
			// Use protojson for full protobuf message
			m := protojson.MarshalOptions{Indent: "  ", EmitUnpopulated: true}
			marshaledJSON, marshalErr = m.Marshal(v)
		case *pb.Model:
			itemID = v.Id
			itemName = v.Name
			if itemName == "" {
				itemName = itemID
			}
			itemDesc = v.Description // Use model description directly
			itemURI = fmt.Sprintf("clarifai://%s/%s/models/%s", userID, appID, itemID)

			// Create and populate the filtered struct
			filteredModel := FilteredModelInfo{
				ID:          v.Id,
				Name:        v.Name,
				CreatedAt:   v.CreatedAt,
				AppID:       v.AppId,
				UserID:      v.UserId,
				ModelTypeID: v.ModelTypeId,
				Description: v.Description,
				Visibility:  v.Visibility,
				DisplayName: v.DisplayName,
				Task:        v.Task,
				Toolkits:    v.Toolkits,
				UseCases:    v.UseCases,
				IsStarred:   v.IsStarred,
				StarCount:   v.StarCount, // Corrected type
				Image:       v.Image,
			}
			if v.ModelVersion != nil {
				filteredModel.ModelVersion = &FilteredModelVersionInfo{
					ID:                 v.ModelVersion.Id,
					CreatedAt:          v.ModelVersion.CreatedAt,
					Status:             v.ModelVersion.Status,
					ActiveConceptCount: v.ModelVersion.ActiveConceptCount,
					// Assign Summary from Metrics if Metrics is not nil
					Metrics:     nil,
					Description: v.ModelVersion.Description,
					Visibility:  v.ModelVersion.Visibility,
					AppID:       v.ModelVersion.AppId,
					UserID:      v.ModelVersion.UserId,
					License:     v.ModelVersion.License,
					OutputInfo:  v.ModelVersion.OutputInfo,
					InputInfo:   v.ModelVersion.InputInfo,
				}
				// Safely access Summary
				if v.ModelVersion.Metrics != nil {
					filteredModel.ModelVersion.Metrics = v.ModelVersion.Metrics.Summary
				}
			}
			// Marshal the filtered struct using standard JSON
			marshaledJSON, marshalErr = jsonMarshaller(&filteredModel, "", "  ")
		case *pb.Annotation:
			itemID = v.Id
			itemName = v.Id
			itemURI = fmt.Sprintf("clarifai://%s/%s/annotations/%s", userID, appID, itemID)
			// Use protojson for full protobuf message
			m := protojson.MarshalOptions{Indent: "  ", EmitUnpopulated: true}
			marshaledJSON, marshalErr = m.Marshal(v)
		default:
			h.logger.Warn("Unsupported type in list results", "type", fmt.Sprintf("%T", item))
			continue
		}

		if marshalErr != nil {
			h.logger.Warn("Failed to marshal list item", "id", itemID, "error", marshalErr)
			continue // Skip this item if marshalling fails
		}

		resultContents = append(resultContents, map[string]interface{}{
			"uri":         itemURI,
			"mimeType":    "application/json",
			"text":        string(marshaledJSON), // Use potentially filtered JSON
			"name":        itemName,
			"description": itemDesc,
		})
	}

	responseResult := map[string]interface{}{
		"contents": resultContents,
	}
	if nextCursor != "" {
		responseResult["nextCursor"] = nextCursor
	}

	h.logger.Debug("Successfully processed ListResource request", "uri", request.Params.URI, "count", len(resultContents), "next_cursor", nextCursor)
	return mcp.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
		Result:  responseResult,
	}
}

func (h *Handler) prepareGrpcCall(baseCtx context.Context) (context.Context, context.CancelFunc, *mcp.RPCError) {
	if h.clarifaiClient == nil || h.clarifaiClient.API == nil {
		return nil, nil, &mcp.RPCError{Code: -32001, Message: "Clarifai client not initialized"}
	}
	if h.pat == "" {
		return nil, nil, &mcp.RPCError{Code: -32001, Message: "Authentication failed: PAT not configured"}
	}

	authCtx := clarifai.CreateContextWithAuth(baseCtx, h.pat)
	callTimeout := time.Duration(h.timeoutSec) * time.Second
	callCtx, cancel := context.WithTimeout(authCtx, callTimeout)
	return callCtx, cancel, nil
}

// handleApiError logs the error with context, writes details to a file, and maps it to an RPCError.
func (h *Handler) handleApiError(err error, context map[string]string) *mcp.RPCError {
	logArgs := []interface{}{"error", err}
	for k, v := range context {
		if v != "" { // Only log non-empty context values
			logArgs = append(logArgs, slog.String(k, v))
		}
	}
	h.logger.Error("API error", logArgs...) // Standard log

	var rpcErr *mcp.RPCError // Declare rpcErr variable

	st, ok := status.FromError(err)
	if ok {
		// Map specific gRPC codes if needed
		// Example: Map NotFound specifically
		if st.Code() == codes.NotFound {
			rpcErr = &mcp.RPCError{Code: -32002, Message: "Resource not found", Data: context}
		}
	}

	// If not specifically mapped yet, use generic mapping
	if rpcErr == nil {
		grpcErr := clarifai.MapGRPCErrorToJSONRPC(err)
		if grpcErr != nil {
			grpcErr.Data = context // Add context to the generic error data
			rpcErr = grpcErr
		} else {
			// Fallback for non-gRPC errors
			rpcErr = &mcp.RPCError{Code: -32000, Message: fmt.Sprintf("Internal server error: %v", err), Data: context}
		}
	}

	// Log detailed error to file before returning
	logErrorToFile(err, context, rpcErr)

	return rpcErr
}

func parsePagination(queryParams url.Values, cursor string, logger *slog.Logger) (page, perPage uint32) {
	page = 1
	perPage = 20

	if cursor != "" {
		pageInt, err := strconv.Atoi(cursor)
		if err == nil && pageInt > 0 {
			page = uint32(pageInt)
		} else {
			logger.Warn("Invalid pagination cursor provided, using default", "cursor", cursor)
		}
	} else {
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
				}
				perPage = uint32(perPageInt)
			}
		}
	}
	return page, perPage
}

func (h *Handler) getInput(ctx context.Context, userAppID *pb.UserAppIDSet, inputID string) (*pb.Input, error) {
	h.logger.Debug("Calling GetInput", "user_id", userAppID.UserId, "app_id", userAppID.AppId, "input_id", inputID)
	grpcRequest := &pb.GetInputRequest{UserAppId: userAppID, InputId: inputID}
	resp, err := h.clarifaiClient.API.GetInput(ctx, grpcRequest)
	if err != nil {
		return nil, err
	}
	if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
		// Use the new custom error type
		return nil, clarifai.NewAPIStatusError(resp.GetStatus())
	}
	return resp.Input, nil
}

func (h *Handler) getModel(ctx context.Context, userAppID *pb.UserAppIDSet, modelID string) (*pb.Model, error) {
	h.logger.Debug("Calling GetModel", "user_id", userAppID.UserId, "app_id", userAppID.AppId, "model_id", modelID)
	grpcRequest := &pb.GetModelRequest{UserAppId: userAppID, ModelId: modelID}
	resp, err := h.clarifaiClient.API.GetModel(ctx, grpcRequest)
	if err != nil {
		return nil, err
	}
	if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
		// Use the new custom error type
		return nil, clarifai.NewAPIStatusError(resp.GetStatus())
	}
	return resp.Model, nil
}

func (h *Handler) listInputs(ctx context.Context, userAppID *pb.UserAppIDSet, pagination *pb.Pagination, query string) ([]proto.Message, string, error) {
	var results []proto.Message
	var nextCursor string
	var apiErr error
	// var errCtx = map[string]string{"userID": userAppID.UserId, "appID": userAppID.AppId, "resourceType": "input", "query": query} // Removed unused declaration

	if query != "" {
		h.logger.Debug("Calling PostInputsSearches", "user_id", userAppID.UserId, "app_id", userAppID.AppId, "query", query, "page", pagination.Page, "per_page", pagination.PerPage)
		searchQueryProto := &pb.Query{Ranks: []*pb.Rank{{Annotation: &pb.Annotation{Data: &pb.Data{Text: &pb.Text{Raw: query}}}}}}
		grpcRequest := &pb.PostInputsSearchesRequest{UserAppId: userAppID, Searches: []*pb.Search{{Query: searchQueryProto}}, Pagination: pagination}
		resp, err := h.clarifaiClient.API.PostInputsSearches(ctx, grpcRequest)
		apiErr = err
		if err != nil {
			// Error occurred during the API call itself
		} else { // No API call error, check status
			if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
				// Use the new custom error type
				apiErr = clarifai.NewAPIStatusError(resp.GetStatus())
			} else {
				// Success case
				results = make([]proto.Message, 0, len(resp.Hits))
				for _, hit := range resp.Hits {
					if hit.Input != nil {
						results = append(results, hit.Input)
					}
				}
				if uint32(len(resp.Hits)) == pagination.PerPage {
					nextCursor = strconv.Itoa(int(pagination.Page + 1))
				}
			}
		} // End of outer else (no API call error)
	} else {
		h.logger.Debug("Calling ListInputs", "user_id", userAppID.UserId, "app_id", userAppID.AppId, "page", pagination.Page, "per_page", pagination.PerPage)
		grpcRequest := &pb.ListInputsRequest{UserAppId: userAppID, Page: pagination.Page, PerPage: pagination.PerPage}
		resp, err := h.clarifaiClient.API.ListInputs(ctx, grpcRequest)
		apiErr = err
		if err != nil {
			// Error occurred during the API call itself
		} else { // No API call error, check status
			if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
				// Use the new custom error type
				apiErr = clarifai.NewAPIStatusError(resp.GetStatus())
			} else {
				// Success case
				results = make([]proto.Message, 0, len(resp.Inputs))
				for _, input := range resp.Inputs {
					results = append(results, input)
				}
				if uint32(len(resp.Inputs)) == pagination.PerPage {
					nextCursor = strconv.Itoa(int(pagination.Page + 1))
				}
			}
		} // End of outer else (no API call error)
	}
	// Check apiErr before returning
	if apiErr != nil {
		// If there was an error (either from the call or status check), log it with context
		// Note: handleApiError is called within the main handleListResource function if apiErr is not nil
	}
	return results, nextCursor, apiErr
}

func (h *Handler) listModels(ctx context.Context, userAppID *pb.UserAppIDSet, pagination *pb.Pagination, query string) ([]proto.Message, string, error) {
	var results []proto.Message
	var nextCursor string
	var apiErr error
	// var errCtx = map[string]string{"userID": userAppID.UserId, "appID": userAppID.AppId, "resourceType": "model", "query": query} // Removed unused declaration

	if query != "" {
		h.logger.Warn("Query parameter is not yet supported for listing models", "query", query)
		apiErr = fmt.Errorf("query parameter not supported for listing models")
	} else {
		h.logger.Debug("Calling ListModels", "user_id", userAppID.UserId, "app_id", userAppID.AppId, "page", pagination.Page, "per_page", pagination.PerPage)
		grpcRequest := &pb.ListModelsRequest{UserAppId: userAppID, Page: pagination.Page, PerPage: pagination.PerPage}
		resp, err := h.clarifaiClient.API.ListModels(ctx, grpcRequest)
		apiErr = err
		if err != nil {
			// Error occurred during the API call itself
		} else { // No API call error, check status
			if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
				// Use the new custom error type
				apiErr = clarifai.NewAPIStatusError(resp.GetStatus())
			} else {
				// Success case
				results = make([]proto.Message, 0, len(resp.Models))
				for _, model := range resp.Models {
					results = append(results, model)
				}
				if uint32(len(resp.Models)) == pagination.PerPage {
					nextCursor = strconv.Itoa(int(pagination.Page + 1))
				}
			}
		} // End of outer else (no API call error)
	}
	// Check apiErr before returning
	if apiErr != nil {
		// If there was an error (either from the call or status check), log it with context
		// Note: handleApiError is called within the main handleListResource function if apiErr is not nil
	}
	return results, nextCursor, apiErr
}

// callClarifaiImageByPath handles inference requests using a local file path.
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

	// Encode to base64
	// Encode to base64 - NO LONGER NEEDED FOR API CALL
	// imageBase64 := base64.StdEncoding.EncodeToString(imageBytes)

	inputData := &pb.Data{
		Image: &pb.Image{Base64: imageBytes}, // Send raw image bytes directly
	}
	h.logger.Debug("Using raw image_bytes from file for inference", "filepath", filepath, "byte_count", len(imageBytes))

	grpcRequest := &pb.PostModelOutputsRequest{
		UserAppId: &pb.UserAppIDSet{UserId: effectiveUserID, AppId: effectiveAppID},
		ModelId:   effectiveModelID,
		Inputs:    []*pb.Input{{Data: inputData}},
	}

	ctx, cancel, rpcErr := h.prepareGrpcCall(context.Background())
	if rpcErr != nil {
		rpcErr.Data = errCtx // Add context to initialization errors
		return nil, rpcErr
	}
	defer cancel()

	h.logger.Debug("Making gRPC call to PostModelOutputs (by path)", "timeout", h.timeoutSec, "user_id", effectiveUserID, "app_id", effectiveAppID, "model_id", effectiveModelID)
	resp, err := h.clarifaiClient.API.PostModelOutputs(ctx, grpcRequest)
	h.logger.Debug("gRPC call to PostModelOutputs (by path) finished.")

	if err != nil {
		return nil, h.handleApiError(err, errCtx)
	}
	if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
		// Use the new custom error type
		apiErr := clarifai.NewAPIStatusError(resp.GetStatus())
		return nil, h.handleApiError(apiErr, errCtx)
	}
	if len(resp.Outputs) == 0 || resp.Outputs[0].Data == nil {
		// Keep this as fmt.Errorf as it's not directly from API status
		apiErr := fmt.Errorf("API response did not contain output data")
		return nil, h.handleApiError(apiErr, errCtx)
	}

	// Marshal the raw response to JSON
	m := protojson.MarshalOptions{Indent: "  ", EmitUnpopulated: true}
	rawResponseJSON, marshalErr := m.Marshal(resp)
	if marshalErr != nil {
		h.logger.Error("Failed to marshal raw API response", "error", marshalErr)
		// Keep this as fmt.Errorf as it's not directly from API status
		apiErr := fmt.Errorf("failed to marshal raw API response: %w", marshalErr)
		return nil, h.handleApiError(apiErr, errCtx)
	}

	h.logger.Debug("Inference successful (by path), returning raw response.")

	// // Process and return results (similar to original infer_image)
	// concepts := resp.Outputs[0].Data.GetConcepts() // Commented out as requested
	// var conceptStrings []string
	// if concepts != nil {
	// 	for _, c := range concepts {
	// 		conceptStrings = append(conceptStrings, fmt.Sprintf("%s: %.2f", c.Name, c.Value))
	// 	}
	// } else {
	// 	h.logger.Warn("No concepts found in inference response")
	// }

	// h.logger.Debug("Inference successful (by path)", "concepts_found", len(conceptStrings))

	// resultText := "Inference Concepts: " + strings.Join(conceptStrings, ", ")
	// if len(conceptStrings) == 0 {
	// 	resultText = "Inference successful, but no concepts met the threshold or model doesn't output concepts."
	// }

	toolResult := map[string]interface{}{
		"content": []map[string]any{
			{"type": "text", "text": string(rawResponseJSON)}, // Return raw JSON response
		},
	}
	return toolResult, nil
}

// callClarifaiImageByURL handles inference requests using an image URL.
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

	ctx, cancel, rpcErr := h.prepareGrpcCall(context.Background())
	if rpcErr != nil {
		rpcErr.Data = errCtx // Add context to initialization errors
		return nil, rpcErr
	}
	defer cancel()

	h.logger.Debug("Making gRPC call to PostModelOutputs (by URL)", "timeout", h.timeoutSec, "user_id", effectiveUserID, "app_id", effectiveAppID, "model_id", effectiveModelID)
	resp, err := h.clarifaiClient.API.PostModelOutputs(ctx, grpcRequest)
	h.logger.Debug("gRPC call to PostModelOutputs (by URL) finished.")

	if err != nil {
		return nil, h.handleApiError(err, errCtx)
	}
	if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
		// Use the new custom error type
		apiErr := clarifai.NewAPIStatusError(resp.GetStatus())
		return nil, h.handleApiError(apiErr, errCtx)
	}
	if len(resp.Outputs) == 0 || resp.Outputs[0].Data == nil {
		// Keep this as fmt.Errorf as it's not directly from API status
		apiErr := fmt.Errorf("API response did not contain output data")
		return nil, h.handleApiError(apiErr, errCtx)
	}

	// Process and return results (similar to original infer_image)
	concepts := resp.Outputs[0].Data.GetConcepts()
	var conceptStrings []string
	if concepts != nil {
		for _, c := range concepts {
			conceptStrings = append(conceptStrings, fmt.Sprintf("%s: %.2f", c.Name, c.Value))
		}
	} else {
		h.logger.Warn("No concepts found in inference response")
	}

	h.logger.Debug("Inference successful (by URL)", "concepts_found", len(conceptStrings))

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

	ctx, cancel, rpcErr := h.prepareGrpcCall(context.Background())
	if rpcErr != nil {
		rpcErr.Data = errCtx // Add context to initialization errors
		return nil, rpcErr
	}
	defer cancel()

	h.logger.Debug("Making gRPC call to PostModelOutputs (generate)", "timeout", h.timeoutSec, "user_id", effectiveUserID, "app_id", effectiveAppID, "model_id", effectiveModelID)
	resp, err := h.clarifaiClient.API.PostModelOutputs(ctx, grpcRequest)
	h.logger.Debug("gRPC call to PostModelOutputs (generate) finished.")

	if err != nil {
		return nil, h.handleApiError(err, errCtx)
	}
	if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
		// Use the new custom error type
		apiErr := clarifai.NewAPIStatusError(resp.GetStatus())
		return nil, h.handleApiError(apiErr, errCtx)
	}
	if len(resp.Outputs) == 0 || resp.Outputs[0].Data == nil || resp.Outputs[0].Data.Image == nil {
		// Keep this as fmt.Errorf as it's not directly from API status
		apiErr := fmt.Errorf("API response did not contain image data")
		return nil, h.handleApiError(apiErr, errCtx)
	}

	imageBase64Bytes := resp.Outputs[0].Data.Image.Base64
	h.logger.Debug("Successfully generated image", "size_bytes", len(imageBase64Bytes))

	const imageSizeThreshold = 10 * 1024

	if h.outputPath != "" && len(imageBase64Bytes) > imageSizeThreshold {
		h.logger.Debug("Image size exceeds threshold, saving to disk", "size_bytes", len(imageBase64Bytes), "threshold", imageSizeThreshold, "output_path", h.outputPath)
		savedPath, saveErr := utils.SaveImage(h.outputPath, imageBase64Bytes)
		if saveErr != nil {
			h.logger.Error("Error saving image using utility function", "error", saveErr)
			return nil, &mcp.RPCError{Code: -32000, Message: "Failed to save generated image to disk"}
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
