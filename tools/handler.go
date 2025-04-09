package tools

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
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
)

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

var toolsDefinitionMap = map[string]interface{}{
	"infer_image": map[string]interface{}{
		"description": "Performs inference on an image using a specified or default Clarifai model. Requires the server to be started with a valid --pat flag.",
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
	case "resources/list":
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
		rpcErr = h.handleApiError(apiErr, resourceType, resourceID)
		return mcp.NewErrorResponse(request.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
	}

	if resourceProto == nil {
		rpcErr = h.handleApiError(fmt.Errorf("API response did not contain data"), resourceType, resourceID)
		return mcp.NewErrorResponse(request.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
	}

	m := protojson.MarshalOptions{Indent: "  ", EmitUnpopulated: true}
	resourceJSON, marshalErr := m.Marshal(resourceProto)
	if marshalErr != nil {
		h.logger.Error("Failed to marshal resource proto", "error", marshalErr)
		rpcErr = h.handleApiError(fmt.Errorf("failed to marshal resource data: %w", marshalErr), resourceType, resourceID)
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
		rpcErr = h.handleApiError(apiErr, resourceType, "")
		return mcp.NewErrorResponse(request.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
	}

	resultContents := make([]map[string]interface{}, 0, len(results))
	m := protojson.MarshalOptions{Indent: "  ", EmitUnpopulated: true}

	for _, item := range results {
		itemJSON, marshalErr := m.Marshal(item)
		if marshalErr != nil {
			h.logger.Warn("Failed to marshal list item", "error", marshalErr)
			continue
		}

		var itemID, itemName, itemDesc string
		var itemURI string

		switch v := item.(type) {
		case *pb.Input:
			itemID = v.Id
			itemName = v.Id
			itemURI = fmt.Sprintf("clarifai://%s/%s/inputs/%s", userID, appID, itemID)
		case *pb.Model:
			itemID = v.Id
			itemName = v.Name
			if itemName == "" {
				itemName = itemID
			}
			itemDesc = v.Notes
			itemURI = fmt.Sprintf("clarifai://%s/%s/models/%s", userID, appID, itemID)
		case *pb.Annotation:
			itemID = v.Id
			itemName = v.Id
			itemURI = fmt.Sprintf("clarifai://%s/%s/annotations/%s", userID, appID, itemID)
		default:
			h.logger.Warn("Unsupported type in list results", "type", fmt.Sprintf("%T", item))
			continue
		}

		resultContents = append(resultContents, map[string]interface{}{
			"uri":         itemURI,
			"mimeType":    "application/json",
			"text":        string(itemJSON),
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

func (h *Handler) handleApiError(err error, resourceType, resourceID string) *mcp.RPCError {
	h.logger.Error("API error", "resource_type", resourceType, "resource_id", resourceID, "error", err)

	st, ok := status.FromError(err)
	if ok {
		switch st.Code() {
		case codes.NotFound:
			return &mcp.RPCError{Code: -32002, Message: "Resource not found", Data: fmt.Sprintf("%s/%s", resourceType, resourceID)}
		}
	}

	grpcErr := clarifai.MapGRPCErrorToJSONRPC(err)
	if grpcErr != nil {
		return grpcErr
	}

	return &mcp.RPCError{Code: -32000, Message: fmt.Sprintf("Failed to process resource '%s/%s': %v", resourceType, resourceID, err)}
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
		return nil, fmt.Errorf("API error: %s - %s", resp.GetStatus().GetDescription(), resp.GetStatus().GetDetails())
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
		return nil, fmt.Errorf("API error: %s - %s", resp.GetStatus().GetDescription(), resp.GetStatus().GetDetails())
	}
	return resp.Model, nil
}

func (h *Handler) listInputs(ctx context.Context, userAppID *pb.UserAppIDSet, pagination *pb.Pagination, query string) ([]proto.Message, string, error) {
	var results []proto.Message
	var nextCursor string
	var apiErr error

	if query != "" {
		h.logger.Debug("Calling PostInputsSearches", "user_id", userAppID.UserId, "app_id", userAppID.AppId, "query", query, "page", pagination.Page, "per_page", pagination.PerPage)
		searchQueryProto := &pb.Query{Ranks: []*pb.Rank{{Annotation: &pb.Annotation{Data: &pb.Data{Text: &pb.Text{Raw: query}}}}}}
		grpcRequest := &pb.PostInputsSearchesRequest{UserAppId: userAppID, Searches: []*pb.Search{{Query: searchQueryProto}}, Pagination: pagination}
		resp, err := h.clarifaiClient.API.PostInputsSearches(ctx, grpcRequest)
		apiErr = err
		if err == nil {
			if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
				apiErr = fmt.Errorf("API error: %s - %s", resp.GetStatus().GetDescription(), resp.GetStatus().GetDetails())
			} else {
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
		}
	} else {
		h.logger.Debug("Calling ListInputs", "user_id", userAppID.UserId, "app_id", userAppID.AppId, "page", pagination.Page, "per_page", pagination.PerPage)
		grpcRequest := &pb.ListInputsRequest{UserAppId: userAppID, Page: pagination.Page, PerPage: pagination.PerPage}
		resp, err := h.clarifaiClient.API.ListInputs(ctx, grpcRequest)
		apiErr = err
		if err == nil {
			if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
				apiErr = fmt.Errorf("API error: %s - %s", resp.GetStatus().GetDescription(), resp.GetStatus().GetDetails())
			} else {
				results = make([]proto.Message, 0, len(resp.Inputs))
				for _, input := range resp.Inputs {
					results = append(results, input)
				}
				if uint32(len(resp.Inputs)) == pagination.PerPage {
					nextCursor = strconv.Itoa(int(pagination.Page + 1))
				}
			}
		}
	}
	return results, nextCursor, apiErr
}

func (h *Handler) listModels(ctx context.Context, userAppID *pb.UserAppIDSet, pagination *pb.Pagination, query string) ([]proto.Message, string, error) {
	var results []proto.Message
	var nextCursor string
	var apiErr error

	if query != "" {
		h.logger.Warn("Query parameter is not yet supported for listing models", "query", query)
		apiErr = fmt.Errorf("query parameter not supported for listing models")
	} else {
		h.logger.Debug("Calling ListModels", "user_id", userAppID.UserId, "app_id", userAppID.AppId, "page", pagination.Page, "per_page", pagination.PerPage)
		grpcRequest := &pb.ListModelsRequest{UserAppId: userAppID, Page: pagination.Page, PerPage: pagination.PerPage}
		resp, err := h.clarifaiClient.API.ListModels(ctx, grpcRequest)
		apiErr = err
		if err == nil {
			if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
				apiErr = fmt.Errorf("API error: %s - %s", resp.GetStatus().GetDescription(), resp.GetStatus().GetDetails())
			} else {
				results = make([]proto.Message, 0, len(resp.Models))
				for _, model := range resp.Models {
					results = append(results, model)
				}
				if uint32(len(resp.Models)) == pagination.PerPage {
					nextCursor = strconv.Itoa(int(pagination.Page + 1))
				}
			}
		}
	}
	return results, nextCursor, apiErr
}

func (h *Handler) callInferImage(args map[string]interface{}) (interface{}, *mcp.RPCError) {
	h.logger.Debug("Executing callInferImage tool")

	imageBytesB64, bytesOk := args["image_bytes"].(string)
	imageURL, urlOk := args["image_url"].(string)

	if (!bytesOk || imageBytesB64 == "") && (!urlOk || imageURL == "") {
		return nil, &mcp.RPCError{Code: -32602, Message: "Invalid params: missing or invalid 'image_bytes' or 'image_url'"}
	}
	if bytesOk && imageBytesB64 != "" && urlOk && imageURL != "" {
		return nil, &mcp.RPCError{Code: -32602, Message: "Invalid params: provide either 'image_bytes' or 'image_url', not both"}
	}

	modelID, _ := args["model_id"].(string)
	userID, _ := args["user_id"].(string)
	appID, _ := args["app_id"].(string)

	if modelID == "" {
		modelID = "general-image-recognition"
		h.logger.Debug("No model_id provided, defaulting", "model_id", modelID)
	}

	inputData := &pb.Data{}
	if bytesOk && imageBytesB64 != "" {
		inputData.Image = &pb.Image{Base64: []byte(utils.CleanBase64Data([]byte(imageBytesB64)))}
		h.logger.Debug("Using image_bytes for inference")
	} else {
		inputData.Image = &pb.Image{Url: imageURL}
		h.logger.Debug("Using image_url for inference", "url", imageURL)
	}

	grpcRequest := &pb.PostModelOutputsRequest{
		UserAppId: &pb.UserAppIDSet{UserId: userID, AppId: appID},
		ModelId:   modelID,
		Inputs:    []*pb.Input{{Data: inputData}},
	}

	ctx, cancel, rpcErr := h.prepareGrpcCall(context.Background())
	if rpcErr != nil {
		return nil, rpcErr
	}
	defer cancel()

	h.logger.Debug("Making gRPC call to PostModelOutputs", "timeout", h.timeoutSec)
	resp, err := h.clarifaiClient.API.PostModelOutputs(ctx, grpcRequest)
	h.logger.Debug("gRPC call to PostModelOutputs finished.")

	if err != nil {
		return nil, h.handleApiError(err, "model_output", modelID)
	}
	if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
		return nil, h.handleApiError(fmt.Errorf("API error: %s - %s", resp.GetStatus().GetDescription(), resp.GetStatus().GetDetails()), "model_output", modelID)
	}
	if len(resp.Outputs) == 0 || resp.Outputs[0].Data == nil {
		return nil, h.handleApiError(fmt.Errorf("API response did not contain output data"), "model_output", modelID)
	}

	concepts := resp.Outputs[0].Data.GetConcepts()
	var conceptStrings []string
	if concepts != nil {
		for _, c := range concepts {
			conceptStrings = append(conceptStrings, fmt.Sprintf("%s: %.2f", c.Name, c.Value))
		}
	} else {
		h.logger.Warn("No concepts found in inference response")
	}

	h.logger.Debug("Inference successful", "concepts_found", len(conceptStrings))

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

	if modelID == "" {
		modelID = "stable-diffusion-xl"
		h.logger.Debug("No model_id provided, defaulting", "model_id", modelID)
		if userID == "" {
			userID = "stability-ai"
			h.logger.Debug("Defaulting user_id", "user_id", userID, "model_id", modelID)
		}
		if appID == "" {
			appID = "stable-diffusion-2"
			h.logger.Debug("Defaulting app_id", "app_id", appID, "model_id", modelID)
		}
	}

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

	ctx, cancel, rpcErr := h.prepareGrpcCall(context.Background())
	if rpcErr != nil {
		return nil, rpcErr
	}
	defer cancel()

	h.logger.Debug("Making gRPC call to PostModelOutputs", "timeout", h.timeoutSec)
	resp, err := h.clarifaiClient.API.PostModelOutputs(ctx, grpcRequest)
	h.logger.Debug("gRPC call to PostModelOutputs finished.")

	if err != nil {
		return nil, h.handleApiError(err, "model_output", modelID)
	}
	if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
		return nil, h.handleApiError(fmt.Errorf("API error: %s - %s", resp.GetStatus().GetDescription(), resp.GetStatus().GetDetails()), "model_output", modelID)
	}
	if len(resp.Outputs) == 0 || resp.Outputs[0].Data == nil || resp.Outputs[0].Data.Image == nil {
		return nil, h.handleApiError(fmt.Errorf("API response did not contain image data"), "model_output", modelID)
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
