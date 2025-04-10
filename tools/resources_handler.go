package tools

import (
	"context"
	"encoding/json"
	"fmt"
	// "log/slog" // Removed unused import
	"net/url"
	"strings"

	"clarifai-mcp-server-local/mcp"
	"clarifai-mcp-server-local/utils"

	pb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api"
	statuspb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
)

// FilteredModelInfo defines the subset of fields to return for list operations. (Moved from handler.go)
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
	StarCount    int32                     `json:"starCount"`
	Image        *pb.Image                 `json:"image,omitempty"`
}

// FilteredModelVersionInfo defines the subset of model version fields. (Moved from handler.go)
type FilteredModelVersionInfo struct {
	ID                 string                 `json:"id"`
	CreatedAt          *timestamppb.Timestamp `json:"createdAt"`
	Status             *statuspb.Status       `json:"status,omitempty"`
	ActiveConceptCount uint32                 `json:"activeConceptCount"`
	Metrics            *pb.MetricsSummary     `json:"metricsSummary,omitempty"`
	Description        string                 `json:"description,omitempty"`
	Visibility         *pb.Visibility         `json:"visibility,omitempty"`
	AppID              string                 `json:"appId"`
	UserID             string                 `json:"userId"`
	License            string                 `json:"license,omitempty"`
	OutputInfo         *pb.OutputInfo         `json:"outputInfo,omitempty"`
	InputInfo          *pb.InputInfo          `json:"inputInfo,omitempty"`
}

// resourceTemplates defines the available resource templates. (Moved from handler.go)
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

// handleListResourceTemplates lists the available resource templates. (Moved from handler.go)
func (h *Handler) handleListResourceTemplates(request mcp.JSONRPCRequest) mcp.JSONRPCResponse {
	h.logger.Debug("Handling resources/templates/list request", "id", request.ID)
	return mcp.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
		Result:  map[string]interface{}{"resourceTemplates": resourceTemplates},
	}
}

// handleReadResource parses the resource URI and routes to get or list handlers. (Moved from handler.go)
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

// handleGetResource fetches a single resource. (Moved from handler.go)
func (h *Handler) handleGetResource(request mcp.JSONRPCRequest, userID, appID, resourceType, resourceID string) mcp.JSONRPCResponse {
	h.logger.Debug("Handling GetResource", "userID", userID, "appID", appID, "resourceType", resourceType, "resourceID", resourceID)

	ctx, cancel, rpcErr := utils.PrepareGrpcCall(context.Background(), h.clarifaiClient, h.pat, h.timeoutSec)
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
		resourceProto, apiErr = h.clarifaiClient.GetInput(ctx, userAppIDSet, resourceID, h.logger)
	case "models":
		// Corrected: Use resourceID for GetModel as well
		resourceProto, apiErr = h.clarifaiClient.GetModel(ctx, userAppIDSet, resourceID, h.logger)
	case "annotations":
		apiErr = fmt.Errorf("GetAnnotation not yet implemented")
	// Add cases for datasets, versions etc. if needed
	default:
		apiErr = fmt.Errorf("reading specific resource type '%s' is not supported or implemented", resourceType)
	}

	if apiErr != nil {
		rpcErr = utils.HandleApiError(apiErr, errCtx, h.logger)
		return mcp.NewErrorResponse(request.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
	}

	if resourceProto == nil {
		// This case might indicate a successful API call but no data returned (e.g., 404 handled gracefully by API)
		// Or it could be an internal logic error where the proto wasn't assigned.
		// Let's assume it's an unexpected lack of data for now.
		rpcErr = utils.HandleApiError(fmt.Errorf("API response did not contain expected data for %s/%s", resourceType, resourceID), errCtx, h.logger)
		return mcp.NewErrorResponse(request.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
	}

	m := protojson.MarshalOptions{Indent: "  ", EmitUnpopulated: true}
	resourceJSON, marshalErr := m.Marshal(resourceProto)
	if marshalErr != nil {
		h.logger.Error("Failed to marshal resource proto", "error", marshalErr, "resourceType", resourceType, "resourceID", resourceID)
		rpcErr = utils.HandleApiError(fmt.Errorf("failed to marshal resource data: %w", marshalErr), errCtx, h.logger)
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

// handleListResource fetches a list of resources. (Moved from handler.go)
func (h *Handler) handleListResource(request mcp.JSONRPCRequest, userID, appID, resourceType, parentType, parentID string, queryParams url.Values) mcp.JSONRPCResponse {
	h.logger.Debug("Handling ListResource", "userID", userID, "appID", appID, "resourceType", resourceType, "parentType", parentType, "parentID", parentID, "queryParams", queryParams)

	ctx, cancel, rpcErr := utils.PrepareGrpcCall(context.Background(), h.clarifaiClient, h.pat, h.timeoutSec)
	if rpcErr != nil {
		return mcp.NewErrorResponse(request.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
	}
	defer cancel()

	page, perPage := utils.ParsePagination(queryParams, request.Params.Cursor, h.logger)
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
			results, nextCursor, apiErr = h.clarifaiClient.ListInputs(ctx, userAppIDSet, pagination, query, h.logger)
		}
	case "models":
		if parentType != "" {
			apiErr = fmt.Errorf("listing models as sub-resource is not supported")
		} else {
			results, nextCursor, apiErr = h.clarifaiClient.ListModels(ctx, userAppIDSet, pagination, query, h.logger)
		}
	case "annotations":
		if parentType == "inputs" && parentID != "" {
			// TODO: Implement ListAnnotations for a specific input
			apiErr = fmt.Errorf("ListAnnotations for Input not yet implemented")
		} else if parentType == "" {
			// TODO: Implement ListAnnotations for an app
			apiErr = fmt.Errorf("ListAnnotations not yet implemented")
		} else {
			apiErr = fmt.Errorf("listing annotations under parent type '%s' is not supported", parentType)
		}
	// Add cases for datasets, versions etc. if needed
	default:
		apiErr = fmt.Errorf("listing resource type '%s' is not supported or implemented", resourceType)
	}

	if apiErr != nil {
		rpcErr = utils.HandleApiError(apiErr, errCtx, h.logger)
		return mcp.NewErrorResponse(request.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
	}

	resultContents := make([]map[string]interface{}, 0, len(results))
	jsonMarshaller := json.MarshalIndent // Use standard JSON marshaller for filtered structs

	for _, item := range results {
		var itemID, itemName, itemDesc string
		var itemURI string
		var marshaledJSON []byte
		var marshalErr error

		switch v := item.(type) {
		case *pb.Input:
			itemID = v.Id
			itemName = v.Id // Use ID as name for inputs
			itemURI = fmt.Sprintf("clarifai://%s/%s/inputs/%s", userID, appID, itemID)
			m := protojson.MarshalOptions{Indent: "  ", EmitUnpopulated: true}
			marshaledJSON, marshalErr = m.Marshal(v)
		case *pb.Model:
			itemID = v.Id
			itemName = v.Name
			if itemName == "" {
				itemName = itemID // Fallback to ID if name is empty
			}
			itemDesc = v.Description
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
				StarCount:   v.StarCount,
				Image:       v.Image,
			}
			if v.ModelVersion != nil {
				filteredModel.ModelVersion = &FilteredModelVersionInfo{
					ID:                 v.ModelVersion.Id,
					CreatedAt:          v.ModelVersion.CreatedAt,
					Status:             v.ModelVersion.Status,
					ActiveConceptCount: v.ModelVersion.ActiveConceptCount,
					Metrics:            nil, // Initialize Metrics as nil
					Description:        v.ModelVersion.Description,
					Visibility:         v.ModelVersion.Visibility,
					AppID:              v.ModelVersion.AppId,
					UserID:             v.ModelVersion.UserId,
					License:            v.ModelVersion.License,
					OutputInfo:         v.ModelVersion.OutputInfo,
					InputInfo:          v.ModelVersion.InputInfo,
				}
				if v.ModelVersion.Metrics != nil {
					filteredModel.ModelVersion.Metrics = v.ModelVersion.Metrics.Summary
				}
			}
			marshaledJSON, marshalErr = jsonMarshaller(&filteredModel, "", "  ")
		case *pb.Annotation:
			itemID = v.Id
			itemName = v.Id // Use ID as name for annotations
			itemURI = fmt.Sprintf("clarifai://%s/%s/annotations/%s", userID, appID, itemID)
			m := protojson.MarshalOptions{Indent: "  ", EmitUnpopulated: true}
			marshaledJSON, marshalErr = m.Marshal(v)
		// Add cases for datasets, versions etc. if needed
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
			"text":        string(marshaledJSON),
			"name":        itemName,
			"description": itemDesc, // Description might be empty for some types
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
