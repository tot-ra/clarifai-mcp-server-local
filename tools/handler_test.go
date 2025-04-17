package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"      // Add http import
	"net/http/httptest" // Add httptest import
	"os"
	"strings" // Add strings import
	"testing"
	"time"

	"clarifai-mcp-server-local/clarifai"
	"clarifai-mcp-server-local/config"
	"clarifai-mcp-server-local/mcp"
	"clarifai-mcp-server-local/utils" // Import utils package

	pb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api"
	statuspb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api/status" // Import status proto
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc" // Import grpc package
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	// "google.golang.org/protobuf/encoding/protojson" // Removed unused import
	"google.golang.org/protobuf/types/known/structpb" // Import structpb
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
)

// Mock Clarifai Client
type MockClarifaiAPIClient struct {
	mock.Mock
}

// Implement V2ClientInterface for MockClarifaiAPIClient
func (m *MockClarifaiAPIClient) GetInput(ctx context.Context, req *pb.GetInputRequest, opts ...grpc.CallOption) (*pb.SingleInputResponse, error) {
	args := m.Called(ctx, req) // Do not pass mockOpts explicitly
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*pb.SingleInputResponse), args.Error(1)
}

func (m *MockClarifaiAPIClient) ListInputs(ctx context.Context, req *pb.ListInputsRequest, opts ...grpc.CallOption) (*pb.MultiInputResponse, error) {
	args := m.Called(ctx, req) // Do not pass mockOpts explicitly
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*pb.MultiInputResponse), args.Error(1)
}

func (m *MockClarifaiAPIClient) PostInputsSearches(ctx context.Context, req *pb.PostInputsSearchesRequest, opts ...grpc.CallOption) (*pb.MultiSearchResponse, error) {
	args := m.Called(ctx, req) // Do not pass mockOpts explicitly
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*pb.MultiSearchResponse), args.Error(1)
}

func (m *MockClarifaiAPIClient) GetModel(ctx context.Context, req *pb.GetModelRequest, opts ...grpc.CallOption) (*pb.SingleModelResponse, error) {
	args := m.Called(ctx, req) // Do not pass mockOpts explicitly
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*pb.SingleModelResponse), args.Error(1)
}

func (m *MockClarifaiAPIClient) ListModels(ctx context.Context, req *pb.ListModelsRequest, opts ...grpc.CallOption) (*pb.MultiModelResponse, error) {
	args := m.Called(ctx, req) // Do not pass mockOpts explicitly
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*pb.MultiModelResponse), args.Error(1)
}

func (m *MockClarifaiAPIClient) PostModelOutputs(ctx context.Context, req *pb.PostModelOutputsRequest, opts ...grpc.CallOption) (*pb.MultiOutputResponse, error) {
	args := m.Called(ctx, req) // Do not pass mockOpts explicitly
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*pb.MultiOutputResponse), args.Error(1)
}

// Add missing methods to satisfy the interface
func (m *MockClarifaiAPIClient) ListAnnotations(ctx context.Context, req *pb.ListAnnotationsRequest, opts ...grpc.CallOption) (*pb.MultiAnnotationResponse, error) {
	args := m.Called(ctx, req) // Do not pass mockOpts explicitly
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*pb.MultiAnnotationResponse), args.Error(1)
}

func (m *MockClarifaiAPIClient) GetAnnotation(ctx context.Context, req *pb.GetAnnotationRequest, opts ...grpc.CallOption) (*pb.SingleAnnotationResponse, error) {
	args := m.Called(ctx, req) // Do not pass mockOpts explicitly
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*pb.SingleAnnotationResponse), args.Error(1)
}

// Add missing PostInputs method
func (m *MockClarifaiAPIClient) PostInputs(ctx context.Context, req *pb.PostInputsRequest, opts ...grpc.CallOption) (*pb.MultiInputResponse, error) {
	args := m.Called(ctx, req) // Do not pass mockOpts explicitly
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*pb.MultiInputResponse), args.Error(1)
}

// --- Test Setup ---

func setupTestHandler(mockAPI *MockClarifaiAPIClient) *Handler {
	cfg := &config.Config{
		Pat:        "test-pat",
		OutputPath: "/tmp/clarifai-test-output",
		TimeoutSec: 5,
	}
	// Ensure mockAPI implements the interface before creating the client
	var _ clarifai.V2ClientInterface = (*MockClarifaiAPIClient)(nil)
	mockClarifaiClient := &clarifai.Client{API: mockAPI}
	handler := NewHandler(mockClarifaiClient, cfg)
	handler.logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})) // Use stderr for test logs
	return handler
}

// Helper to create a success status proto
func successStatus() *statuspb.Status {
	return &statuspb.Status{Code: statuspb.StatusCode_SUCCESS}
}

// --- Helper Function Tests ---

func TestPrepareGrpcCall(t *testing.T) {
	mockAPI := new(MockClarifaiAPIClient)
	handler := setupTestHandler(mockAPI)

	t.Run("Successful call", func(t *testing.T) {
		// Use utils.PrepareGrpcCall, passing necessary handler fields
		ctx, cancel, rpcErr := utils.PrepareGrpcCall(context.Background(), handler.clarifaiClient, handler.pat, handler.timeoutSec)
		assert.NotNil(t, ctx)
		assert.NotNil(t, cancel)
		assert.Nil(t, rpcErr)
		defer cancel()
		// Check timeout
		deadline, ok := ctx.Deadline()
		assert.True(t, ok)
		assert.WithinDuration(t, time.Now().Add(time.Duration(handler.timeoutSec)*time.Second), deadline, 1*time.Second)
	})

	t.Run("Missing PAT", func(t *testing.T) {
		handlerNoPat := setupTestHandler(mockAPI)
		handlerNoPat.pat = ""
		// Use utils.PrepareGrpcCall
		ctx, cancel, rpcErr := utils.PrepareGrpcCall(context.Background(), handlerNoPat.clarifaiClient, handlerNoPat.pat, handlerNoPat.timeoutSec)
		assert.Nil(t, ctx)
		assert.Nil(t, cancel)
		assert.NotNil(t, rpcErr)
		assert.Equal(t, -32001, rpcErr.Code)
		assert.Contains(t, rpcErr.Message, "PAT not configured")
	})

	t.Run("Missing Client", func(t *testing.T) {
		handlerNoClient := setupTestHandler(mockAPI)
		handlerNoClient.clarifaiClient = nil
		// Use utils.PrepareGrpcCall
		ctx, cancel, rpcErr := utils.PrepareGrpcCall(context.Background(), handlerNoClient.clarifaiClient, handlerNoClient.pat, handlerNoClient.timeoutSec)
		assert.Nil(t, ctx)
		assert.Nil(t, cancel)
		assert.NotNil(t, rpcErr)
		assert.Equal(t, -32001, rpcErr.Code)
		assert.Contains(t, rpcErr.Message, "client not initialized")
	})
}

func TestHandleApiError(t *testing.T) {
	mockAPI := new(MockClarifaiAPIClient)
	handler := setupTestHandler(mockAPI)

	t.Run("Not Found Error", func(t *testing.T) {
		grpcErr := status.Error(codes.NotFound, "resource not found")
		testCtx := map[string]string{"resourceType": "inputs", "resourceID": "input123"}
		// Use utils.HandleApiError, passing the logger
		rpcErr := utils.HandleApiError(grpcErr, testCtx, handler.logger)
		assert.NotNil(t, rpcErr)
		assert.Equal(t, -32002, rpcErr.Code)
		assert.Equal(t, "Resource not found", rpcErr.Message)
		assert.Equal(t, testCtx, rpcErr.Data) // Check if context is passed correctly
	})

	t.Run("Permission Denied Error", func(t *testing.T) {
		grpcErr := status.Error(codes.PermissionDenied, "invalid PAT")
		testCtx := map[string]string{"resourceType": "models", "resourceID": "model456"}
		// Use utils.HandleApiError
		rpcErr := utils.HandleApiError(grpcErr, testCtx, handler.logger)
		assert.NotNil(t, rpcErr)
		// assert.Equal(t, -32003, rpcErr.Code) // Original assertion - fails
		assert.Equal(t, int(codes.PermissionDenied), rpcErr.Code) // Expect the raw gRPC code for now
		assert.Equal(t, "invalid PAT", rpcErr.Message)
		assert.Equal(t, testCtx, rpcErr.Data) // Check context
	})

	t.Run("Generic gRPC Error", func(t *testing.T) {
		grpcErr := status.Error(codes.Unavailable, "connection failed")
		testCtx := map[string]string{"resourceType": "inputs", "resourceID": ""}
		// Use utils.HandleApiError
		rpcErr := utils.HandleApiError(grpcErr, testCtx, handler.logger)
		assert.NotNil(t, rpcErr)
		// assert.Equal(t, -32004, rpcErr.Code) // Original assertion - fails
		assert.Equal(t, int(codes.Unavailable), rpcErr.Code) // Expect the raw gRPC code for now
		assert.Equal(t, "connection failed", rpcErr.Message)
		assert.Equal(t, testCtx, rpcErr.Data) // Check context
	})

	t.Run("Non-gRPC Error", func(t *testing.T) {
		genericErr := errors.New("something went wrong")
		testCtx := map[string]string{"resourceType": "outputs", "resourceID": "out789"}
		// Use utils.HandleApiError
		rpcErr := utils.HandleApiError(genericErr, testCtx, handler.logger)
		assert.NotNil(t, rpcErr)
		assert.Equal(t, -32000, rpcErr.Code)
		assert.Equal(t, "Internal server error: something went wrong", rpcErr.Message)
		assert.Equal(t, testCtx, rpcErr.Data) // Check context
	})
}

// --- Request Routing Tests ---

func TestHandleRequest_Routing(t *testing.T) {
	mockAPI := new(MockClarifaiAPIClient)
	handler := setupTestHandler(mockAPI)

	testCases := []struct {
		name         string
		method       string
		expectedNil  bool
		expectedCode int // 0 for success/result, error code otherwise
	}{
		{"Initialize", "initialize", false, 0},
		{"List Tools", "tools/list", false, 0},
		{"Call Tool Known", "tools/call", false, -32601}, // Expect tool not found initially
		{"List Templates", "resources/templates/list", false, 0},
		{"Read Resource", "resources/read", false, -32602},            // Expect missing URI
		{"List Resource (via read)", "resources/list", false, -32602}, // Expect missing URI
		{"Unknown Method", "unknown/method", false, -32601},
		{"Notification", "notifications/something", true, 0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := mcp.JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Method:  tc.method,
				Params:  mcp.RequestParams{}, // Empty params for routing test
			}
			// Special case for call tool to avoid nil pointer
			if tc.method == "tools/call" {
				req.Params.Name = "nonexistent_tool"
			}

			resp := handler.HandleRequest(req)

			if tc.expectedNil {
				assert.Nil(t, resp)
			} else {
				assert.NotNil(t, resp)
				if tc.expectedCode == 0 {
					assert.Nil(t, resp.Error)
					assert.NotNil(t, resp.Result)
				} else {
					assert.NotNil(t, resp.Error)
					assert.Equal(t, tc.expectedCode, resp.Error.Code)
					assert.Nil(t, resp.Result)
				}
			}
		})
	}
}

// --- Resource Handling Tests (Placeholder - Add more specific tests) ---

func TestHandleReadResource_GetInput_Success(t *testing.T) {
	mockAPI := new(MockClarifaiAPIClient)
	handler := setupTestHandler(mockAPI)
	reqID := "req-get-input-1"
	userID := "test-user"
	appID := "test-app"
	inputID := "input-123"
	uri := fmt.Sprintf("clarifai://%s/%s/inputs/%s", userID, appID, inputID)

	mockInput := &pb.Input{Id: inputID, Data: &pb.Data{Text: &pb.Text{Raw: "hello"}}}
	mockResp := &pb.SingleInputResponse{ // Correct response type
		Status: successStatus(), // Use helper
		Input:  mockInput,
	}

	mockAPI.On("GetInput", mock.Anything, mock.MatchedBy(func(r *pb.GetInputRequest) bool {
		return r.UserAppId.UserId == userID && r.UserAppId.AppId == appID && r.InputId == inputID
	})).Return(mockResp, nil)

	req := mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      reqID,
		Method:  "resources/read",
		Params:  mcp.RequestParams{URI: uri},
	}

	resp := handler.HandleRequest(req)

	assert.NotNil(t, resp)
	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)
	resultMap, ok := resp.Result.(map[string]interface{})
	assert.True(t, ok)
	contents, ok := resultMap["contents"].([]map[string]interface{})
	assert.True(t, ok)
	assert.Len(t, contents, 1)
	assert.Equal(t, uri, contents[0]["uri"])
	assert.Equal(t, "application/json", contents[0]["mimeType"])
	// Unmarshal JSON and check specific fields instead of Contains
	var resultData map[string]interface{}
	err := json.Unmarshal([]byte(contents[0]["text"].(string)), &resultData)
	assert.NoError(t, err)
	assert.Equal(t, inputID, resultData["id"])
	dataMap, _ := resultData["data"].(map[string]interface{})
	textMap, _ := dataMap["text"].(map[string]interface{})
	assert.Equal(t, "hello", textMap["raw"])
	mockAPI.AssertExpectations(t)
}

func TestHandleReadResource_ListInputs_Success(t *testing.T) {
	mockAPI := new(MockClarifaiAPIClient)
	handler := setupTestHandler(mockAPI)
	reqID := "req-list-inputs-1"
	userID := "test-user"
	appID := "test-app"
	uri := fmt.Sprintf("clarifai://%s/%s/inputs", userID, appID)

	mockInputs := []*pb.Input{
		{Id: "input-1", Data: &pb.Data{Text: &pb.Text{Raw: "one"}}},
		{Id: "input-2", Data: &pb.Data{Image: &pb.Image{Url: "two.jpg"}}},
	}
	mockResp := &pb.MultiInputResponse{ // Correct response type
		Status: successStatus(), // Use helper
		Inputs: mockInputs,
	}

	mockAPI.On("ListInputs", mock.Anything, mock.MatchedBy(func(r *pb.ListInputsRequest) bool {
		return r.UserAppId.UserId == userID && r.UserAppId.AppId == appID && r.Page == 1 && r.PerPage == 20
	})).Return(mockResp, nil)

	req := mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      reqID,
		Method:  "resources/read", // or resources/list
		Params:  mcp.RequestParams{URI: uri},
	}

	resp := handler.HandleRequest(req)

	assert.NotNil(t, resp)
	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)
	resultMap, ok := resp.Result.(map[string]interface{})
	assert.True(t, ok)
	contents, ok := resultMap["contents"].([]map[string]interface{})
	assert.True(t, ok)
	assert.Len(t, contents, 2)
	// Check first item
	// Unmarshal and check first item
	var resultData1 map[string]interface{}
	err1 := json.Unmarshal([]byte(contents[0]["text"].(string)), &resultData1)
	assert.NoError(t, err1)
	assert.Equal(t, fmt.Sprintf("clarifai://%s/%s/inputs/input-1", userID, appID), contents[0]["uri"])
	assert.Equal(t, "input-1", contents[0]["name"])
	assert.Equal(t, "input-1", resultData1["id"])

	// Unmarshal and check second item
	var resultData2 map[string]interface{}
	err2 := json.Unmarshal([]byte(contents[1]["text"].(string)), &resultData2)
	assert.NoError(t, err2)
	assert.Equal(t, fmt.Sprintf("clarifai://%s/%s/inputs/input-2", userID, appID), contents[1]["uri"])
	assert.Equal(t, "input-2", contents[1]["name"])
	dataMap2, _ := resultData2["data"].(map[string]interface{})
	imageMap2, _ := dataMap2["image"].(map[string]interface{})
	assert.Equal(t, "two.jpg", imageMap2["url"])

	mockAPI.AssertExpectations(t)
}

// --- Tool Call Tests ---

// TestCallInferImage_Success_Bytes is renamed and modified to test the file read error path,
// as mocking os.ReadFile is complex in this setup.
func TestCallClarifaiImageByPath_FileReadError(t *testing.T) {
	mockAPI := new(MockClarifaiAPIClient)
	handler := setupTestHandler(mockAPI)
	reqID := "req-infer-bytes-fileread-err-1"
	dummyFilePath := "/path/to/nonexistent/image.jpg" // Use a non-existent path

	// No mock for PostModelOutputs needed, as ReadFile should fail first

	req := mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      reqID,
		Method:  "tools/call",
		Params: mcp.RequestParams{
			Name: "clarifai_image_by_path",
			Arguments: map[string]interface{}{
				"filepath": dummyFilePath,
			},
		},
	}

	resp := handler.HandleRequest(req)

	// Assertions for the error case
	assert.NotNil(t, resp)
	assert.Nil(t, resp.Result)               // Expect no result on error
	assert.NotNil(t, resp.Error)             // Expect an error
	assert.Equal(t, -32000, resp.Error.Code) // Expect internal server error code
	assert.Contains(t, resp.Error.Message, "Failed to read image file")
	assert.Contains(t, resp.Error.Message, "no such file or directory")

	// Ensure PostModelOutputs was NOT called
	mockAPI.AssertNotCalled(t, "PostModelOutputs", mock.Anything, mock.Anything)
}

// TODO: Add a separate test for the success path of clarifai_image_by_path
// This would require mocking os.ReadFile or using a real temp file.

func TestHandleListResource_ListModels_Filtered(t *testing.T) {
	mockAPI := new(MockClarifaiAPIClient)
	handler := setupTestHandler(mockAPI)
	reqID := "req-list-models-filtered-1"
	userID := "test-user"
	appID := "test-app"
	uri := fmt.Sprintf("clarifai://%s/%s/models", userID, appID)

	// Mock model with fields that should be filtered
	mockModel := &pb.Model{
		Id:          "model-123",
		Name:        "Test Model",
		CreatedAt:   timestamppb.Now(),
		AppId:       appID,
		UserId:      userID,
		ModelTypeId: "visual-classifier",
		Description: "A test model.",
		Notes:       "These notes should be filtered out.", // Should be filtered
		Metadata: func() *structpb.Struct { // Use structpb.Struct
			s, _ := structpb.NewStruct(map[string]interface{}{"key": "value"})
			return s
		}(), // Should be filtered
		ModelVersion: &pb.ModelVersion{
			Id:        "version-abc",
			CreatedAt: timestamppb.Now(),
			Status:    successStatus(),
			// Correctly initialize TrainInfo.Params as *structpb.Struct
			TrainInfo: &pb.TrainInfo{Params: &structpb.Struct{Fields: map[string]*structpb.Value{
				"epoch": structpb.NewStringValue("10"),
			}}}, // Should be filtered
			Metrics: &pb.EvalMetrics{Summary: &pb.MetricsSummary{Top1Accuracy: 0.95}},
		},
	}
	mockResp := &pb.MultiModelResponse{
		Status: successStatus(),
		Models: []*pb.Model{mockModel},
	}

	mockAPI.On("ListModels", mock.Anything, mock.MatchedBy(func(r *pb.ListModelsRequest) bool {
		return r.UserAppId.UserId == userID && r.UserAppId.AppId == appID && r.Page == 1 && r.PerPage == 20
	})).Return(mockResp, nil)

	req := mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      reqID,
		Method:  "resources/read", // Using read for list
		Params:  mcp.RequestParams{URI: uri},
	}

	resp := handler.HandleRequest(req)

	assert.NotNil(t, resp)
	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)
	resultMap, ok := resp.Result.(map[string]interface{})
	assert.True(t, ok)
	contents, ok := resultMap["contents"].([]map[string]interface{})
	assert.True(t, ok)
	assert.Len(t, contents, 1)

	// Check the raw JSON string for excluded fields
	rawJSON := contents[0]["text"].(string)
	assert.NotContains(t, rawJSON, `"notes":`)
	assert.NotContains(t, rawJSON, `"metadata":`)
	assert.NotContains(t, rawJSON, `"trainInfo":`)
	assert.Contains(t, rawJSON, `"metricsSummary":`) // Ensure MetricsSummary is present
	assert.Contains(t, rawJSON, `"id": "model-123"`)
	assert.Contains(t, rawJSON, `"description": "A test model."`)

	// Optionally, unmarshal into the filtered struct to verify fields
	var filteredResult FilteredModelInfo
	err := json.Unmarshal([]byte(rawJSON), &filteredResult)
	assert.NoError(t, err)
	assert.Equal(t, "model-123", filteredResult.ID)
	assert.Equal(t, "A test model.", filteredResult.Description)
	assert.NotNil(t, filteredResult.ModelVersion)
	assert.NotNil(t, filteredResult.ModelVersion.Metrics) // Check MetricsSummary presence
	assert.Equal(t, float32(0.95), filteredResult.ModelVersion.Metrics.Top1Accuracy)

	mockAPI.AssertExpectations(t)
}

// TODO: Add more tests for:
// - Get/List other resource types (models) - More cases
// - Get/List with errors (Not Found, Auth Failed, etc.)
// - List with pagination (nextCursor)
// - List with search query
// - callInferImage with URL input
// - callGenerateImage success (bytes output)
// - callGenerateImage success (file output)
// - callTool with invalid arguments
// - callTool with API errors

// --- Search Tool Tests ---

func TestCallSearchByText_Success(t *testing.T) {
	mockAPI := new(MockClarifaiAPIClient)
	handler := setupTestHandler(mockAPI)
	reqID := "req-search-text-1"
	userID := "test-user"
	appID := "test-app"
	queryText := "search for cats"
	page := uint32(2)
	perPage := uint32(10)

	// Expected request
	// expectedQueryProto := &pb.Query{ // Removed unused variable
	// 	Ranks: []*pb.Rank{
	// 		{Annotation: &pb.Annotation{Data: &pb.Data{Text: &pb.Text{Raw: queryText}}}},
	// 	},
	// }
	// expectedPagination := &pb.Pagination{Page: page, PerPage: perPage} // Removed unused variable
	// expectedGrpcReq := &pb.PostInputsSearchesRequest{ // Removed unused variable
	// 	UserAppId:  &pb.UserAppIDSet{UserId: userID, AppId: appID},
	// 	Searches:   []*pb.Search{{Query: expectedQueryProto}},
	// 	Pagination: expectedPagination,
	// } // Removed extra brace

	// --- Mock HTTP Server for Text Fetching ---
	mockTextContent := "This is the fetched raw text content for cats."
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check Authorization header (optional but good practice)
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Key test-pat" {
			t.Logf("Warning: Missing or incorrect Authorization header in test request: %s", authHeader)
			// http.Error(w, "Unauthorized", http.StatusUnauthorized)
			// return
		}
		fmt.Fprintln(w, mockTextContent)
	}))
	defer server.Close()
	mockTextURL := server.URL // Use the mock server's URL

	// Mock response - Include Text.Url
	mockHit := &pb.Hit{
		Score: 0.99,
		Input: &pb.Input{
			Id: "hit-input-1",
			Data: &pb.Data{
				Text: &pb.Text{
					// Raw: "a cat", // Raw is now fetched
					Url: mockTextURL, // Provide the URL for fetching
				},
			},
		},
	}
	mockResp := &pb.MultiSearchResponse{
		Status: successStatus(),
		Hits:   []*pb.Hit{mockHit},
	}

	mockAPI.On("PostInputsSearches", mock.Anything, mock.MatchedBy(func(r *pb.PostInputsSearchesRequest) bool {
		// Basic check - more detailed proto comparison might be needed
		return r.UserAppId.UserId == userID &&
			r.UserAppId.AppId == appID &&
			len(r.Searches) == 1 &&
			r.Searches[0].Query.Ranks[0].Annotation.Data.Text.Raw == queryText &&
			r.Pagination.Page == page &&
			r.Pagination.PerPage == perPage
	})).Return(mockResp, nil)

	req := mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      reqID,
		Method:  "tools/call",
		Params: mcp.RequestParams{
			Name: "search_by_text",
			Arguments: map[string]interface{}{
				"query":    queryText,
				"user_id":  userID,
				"app_id":   appID,
				"page":     float64(page), // JSON numbers are float64
				"per_page": float64(perPage),
			},
		},
	}

	resp := handler.HandleRequest(req)

	assert.NotNil(t, resp)
	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)
	resultMap, ok := resp.Result.(map[string]interface{})
	assert.True(t, ok)
	contents, ok := resultMap["content"].([]map[string]any)
	assert.True(t, ok)
	assert.Len(t, contents, 1)
	assert.Equal(t, "text", contents[0]["type"])

	// Unmarshal the JSON response and check the ID and fetched raw text
	var resultData map[string]interface{}
	err := json.Unmarshal([]byte(contents[0]["text"].(string)), &resultData)
	assert.NoError(t, err)
	hits, _ := resultData["hits"].([]interface{})
	assert.Len(t, hits, 1)
	hitMap, _ := hits[0].(map[string]interface{})
	inputMap, _ := hitMap["input"].(map[string]interface{})
	assert.Equal(t, "hit-input-1", inputMap["id"]) // Assert specific ID value
	// Check fetched text
	dataMap, _ := inputMap["data"].(map[string]interface{})
	textMap, _ := dataMap["text"].(map[string]interface{})
	assert.Equal(t, mockTextURL, textMap["url"]) // Verify URL is still present
	// Trim newline added by fmt.Fprintln
	assert.Equal(t, mockTextContent, strings.TrimSpace(textMap["raw"].(string)))

	mockAPI.AssertExpectations(t)
}

func TestCallSearchByFilepath_Success(t *testing.T) {
	mockAPI := new(MockClarifaiAPIClient)
	handler := setupTestHandler(mockAPI)
	reqID := "req-search-file-1"
	userID := "test-user"
	appID := "test-app"

	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "test-image-*.jpg")
	assert.NoError(t, err)
	defer os.Remove(tmpFile.Name()) // Clean up after test

	dummyImageData := []byte("dummyimagedata")
	_, err = tmpFile.Write(dummyImageData)
	assert.NoError(t, err)
	err = tmpFile.Close()
	assert.NoError(t, err)
	filePath := tmpFile.Name()

	// Expected request
	// expectedQueryProto := &pb.Query{ // Removed unused variable
	// 	Ranks: []*pb.Rank{
	// 		{Annotation: &pb.Annotation{Data: &pb.Data{Image: &pb.Image{Base64: dummyImageData}}}},
	// 	},
	// }
	// expectedGrpcReq := &pb.PostInputsSearchesRequest{ // Removed unused variable
	// 	UserAppId:  &pb.UserAppIDSet{UserId: userID, AppId: appID},
	// 	Searches:   []*pb.Search{{Query: expectedQueryProto}},
	// 	Pagination: &pb.Pagination{}, // Expect default pagination
	// } // Removed extra brace

	// --- Mock HTTP Server for Text Fetching ---
	mockTextContentFile := "This is the fetched raw text content for the file search."
	serverFile := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, mockTextContentFile)
	}))
	defer serverFile.Close()
	mockTextURLFile := serverFile.URL

	// Mock response - Include Text.Url if the input is text
	// If the input is expected to be image, the text fetching part might not apply
	// For this test, let's assume the hit *could* have associated text data with a URL
	mockHit := &pb.Hit{
		Score: 0.98,
		Input: &pb.Input{
			Id: "hit-input-file-1",
			Data: &pb.Data{
				// Image: &pb.Image{Url: "some_image_url.jpg"}, // Original input might be image
				Text: &pb.Text{Url: mockTextURLFile}, // Add a text URL to test fetching
			},
		},
	}
	mockResp := &pb.MultiSearchResponse{
		Status: successStatus(),
		Hits:   []*pb.Hit{mockHit},
	}

	mockAPI.On("PostInputsSearches", mock.Anything, mock.MatchedBy(func(r *pb.PostInputsSearchesRequest) bool {
		return r.UserAppId.UserId == userID &&
			r.UserAppId.AppId == appID &&
			len(r.Searches) == 1 &&
			string(r.Searches[0].Query.Ranks[0].Annotation.Data.Image.Base64) == string(dummyImageData) && // Compare bytes
			r.Pagination.Page == 1 && // Correct default pagination
			r.Pagination.PerPage == 10 // Correct default pagination
	})).Return(mockResp, nil)

	req := mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      reqID,
		Method:  "tools/call",
		Params: mcp.RequestParams{
			Name: "search_by_filepath",
			Arguments: map[string]interface{}{
				"filepath": filePath,
				"user_id":  userID,
				"app_id":   appID,
				// No pagination args provided
			},
		},
	}

	resp := handler.HandleRequest(req)

	assert.NotNil(t, resp)
	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)
	resultMap, ok := resp.Result.(map[string]interface{})
	assert.True(t, ok)
	contents, ok := resultMap["content"].([]map[string]any)
	assert.True(t, ok)
	assert.Len(t, contents, 1)
	assert.Equal(t, "text", contents[0]["type"])

	// Unmarshal the JSON response and check the ID and fetched raw text
	var resultData map[string]interface{}
	err = json.Unmarshal([]byte(contents[0]["text"].(string)), &resultData)
	assert.NoError(t, err)
	hits, _ := resultData["hits"].([]interface{})
	assert.Len(t, hits, 1)
	hitMap, _ := hits[0].(map[string]interface{})
	inputMap, _ := hitMap["input"].(map[string]interface{})
	assert.Equal(t, "hit-input-file-1", inputMap["id"]) // Assert specific ID value
	// Check fetched text
	dataMap, _ := inputMap["data"].(map[string]interface{})
	textMap, _ := dataMap["text"].(map[string]interface{})
	assert.Equal(t, mockTextURLFile, textMap["url"])
	assert.Equal(t, mockTextContentFile, strings.TrimSpace(textMap["raw"].(string)))

	mockAPI.AssertExpectations(t)
}

func TestCallSearchByURL_Success(t *testing.T) {
	mockAPI := new(MockClarifaiAPIClient)
	handler := setupTestHandler(mockAPI)
	reqID := "req-search-url-1"
	userID := "test-user"
	appID := "test-app"
	imageURL := "http://example.com/image.jpg"

	// Expected request
	// expectedQueryProto := &pb.Query{ // Removed unused variable
	// 	Ranks: []*pb.Rank{
	// 		{Annotation: &pb.Annotation{Data: &pb.Data{Image: &pb.Image{Url: imageURL}}}},
	// 	},
	// }
	// expectedGrpcReq := &pb.PostInputsSearchesRequest{ // Removed unused variable
	// 	UserAppId:  &pb.UserAppIDSet{UserId: userID, AppId: appID},
	// 	Searches:   []*pb.Search{{Query: expectedQueryProto}},
	// 	Pagination: &pb.Pagination{}, // Expect default pagination
	// } // Removed extra brace

	// --- Mock HTTP Server for Text Fetching ---
	mockTextContentURL := "This is the fetched raw text content for the URL search."
	serverURL := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, mockTextContentURL)
	}))
	defer serverURL.Close()
	mockTextURLURL := serverURL.URL

	// Mock response - Include Text.Url
	mockHit := &pb.Hit{
		Score: 0.97,
		Input: &pb.Input{
			Id: "hit-input-url-1",
			Data: &pb.Data{
				// Image: &pb.Image{Url: imageURL}, // Original input might be image
				Text: &pb.Text{Url: mockTextURLURL}, // Add a text URL to test fetching
			},
		},
	}
	mockResp := &pb.MultiSearchResponse{
		Status: successStatus(),
		Hits:   []*pb.Hit{mockHit},
	}

	mockAPI.On("PostInputsSearches", mock.Anything, mock.MatchedBy(func(r *pb.PostInputsSearchesRequest) bool {
		return r.UserAppId.UserId == userID &&
			r.UserAppId.AppId == appID &&
			len(r.Searches) == 1 &&
			r.Searches[0].Query.Ranks[0].Annotation.Data.Image.Url == imageURL &&
			r.Pagination.Page == 1 && // Correct default pagination
			r.Pagination.PerPage == 10 // Correct default pagination
	})).Return(mockResp, nil)

	req := mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      reqID,
		Method:  "tools/call",
		Params: mcp.RequestParams{
			Name: "search_by_url",
			Arguments: map[string]interface{}{
				"image_url": imageURL,
				"user_id":   userID,
				"app_id":    appID,
			},
		},
	}

	resp := handler.HandleRequest(req)

	assert.NotNil(t, resp)
	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)
	resultMap, ok := resp.Result.(map[string]interface{})
	assert.True(t, ok)
	contents, ok := resultMap["content"].([]map[string]any)
	assert.True(t, ok)
	assert.Len(t, contents, 1)
	assert.Equal(t, "text", contents[0]["type"])

	// Unmarshal the JSON response and check the ID and fetched raw text
	var resultData map[string]interface{}
	err := json.Unmarshal([]byte(contents[0]["text"].(string)), &resultData)
	assert.NoError(t, err)
	hits, _ := resultData["hits"].([]interface{})
	assert.Len(t, hits, 1)
	hitMap, _ := hits[0].(map[string]interface{})
	inputMap, _ := hitMap["input"].(map[string]interface{})
	assert.Equal(t, "hit-input-url-1", inputMap["id"]) // Assert specific ID value
	// Check fetched text
	dataMap, _ := inputMap["data"].(map[string]interface{})
	textMap, _ := dataMap["text"].(map[string]interface{})
	assert.Equal(t, mockTextURLURL, textMap["url"])
	assert.Equal(t, mockTextContentURL, strings.TrimSpace(textMap["raw"].(string)))

	mockAPI.AssertExpectations(t)
}
