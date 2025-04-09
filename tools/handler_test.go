package tools

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"

	"clarifai-mcp-server-local/clarifai"
	"clarifai-mcp-server-local/config"
	"clarifai-mcp-server-local/mcp"

	// "clarifai-mcp-server-local/internal/mcp" // Removed unused import

	pb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api"
	statuspb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api/status"
	"google.golang.org/grpc" // Added missing import
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Helper to create a Handler with a manual mock client for testing
func setupTestHandler(t *testing.T) (*Handler, *clarifai.MockV2Client) {
	// ctrl := gomock.NewController(t) // No controller needed
	mockClient := &clarifai.MockV2Client{} // Create instance of manual mock
	// Use discard logger for tests unless specific log output is needed
	discardLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.Level(100)})) // Level well above debug

	// Create a dummy config for testing
	testConfig := &config.Config{
		Pat:           "test-pat",
		OutputPath:    t.TempDir(),
		TimeoutSec:    5,
		LogLevel:      slog.Level(100),
		DefaultUserID: "default-user", // Add default IDs for testing list without URI
		DefaultAppID:  "default-app",
	}

	handler := NewHandler(&clarifai.Client{API: mockClient}, testConfig) // Pass config to NewHandler
	handler.logger = discardLogger                                       // Override logger for tests

	return handler, mockClient
}

func TestCallGenerateImage(t *testing.T) {
	handler, mockClient := setupTestHandler(t)
	// defer ctrl.Finish() // No controller needed

	testPrompt := "a cat wearing a hat"
	testModelID := "test-image-gen-model"
	testUserID := "test-user"
	testAppID := "test-app"
	testImageBytes := []byte("fake-base64-image-data")

	// --- Test Case 1: Successful generation, small image (return base64) ---
	t.Run("Success_SmallImage", func(t *testing.T) {
		args := map[string]interface{}{
			"text_prompt": testPrompt,
			"model_id":    testModelID,
			"user_id":     testUserID,
			"app_id":      testAppID,
		}

		expectedResponse := &pb.MultiOutputResponse{ // Correct type
			Status: &statuspb.Status{Code: statuspb.StatusCode_SUCCESS},
			Outputs: []*pb.Output{
				{Data: &pb.Data{Image: &pb.Image{Base64: testImageBytes}}},
			},
		}

		// Assign the mock function directly
		mockClient.PostModelOutputsFunc = func(ctx context.Context, req *pb.PostModelOutputsRequest, opts ...grpc.CallOption) (*pb.MultiOutputResponse, error) {
			// Basic validation of the request
			if req.ModelId != testModelID {
				return nil, fmt.Errorf("expected model ID %s, got %s", testModelID, req.ModelId)
			}
			if len(req.Inputs) != 1 || req.Inputs[0].Data.Text.Raw != testPrompt {
				return nil, fmt.Errorf("invalid input prompt")
			}
			return expectedResponse, nil
		}
		// .Times(1) // Manual mock doesn't track calls

		result, err := handler.callGenerateImage(args)

		if err != nil {
			t.Fatalf("Expected no error, but got: %v", err)
		}
		if result == nil {
			t.Fatal("Expected non-nil result, but got nil")
		}

		content, ok := result.(map[string]interface{})["content"].([]map[string]interface{})
		if !ok || len(content) != 1 {
			t.Fatalf("Expected result content to be a slice with one element, got: %v", result)
		}
		if content[0]["type"] != "image" {
			t.Errorf("Expected content type 'image', got '%v'", content[0]["type"])
		}
		if content[0]["bytes"] != string(testImageBytes) {
			t.Errorf("Expected content bytes '%s', got '%v'", string(testImageBytes), content[0]["bytes"])
		}
	})

	// --- Test Case 2: Successful generation, large image (save to disk) ---
	t.Run("Success_LargeImage", func(t *testing.T) {
		largeImageBytes := []byte(strings.Repeat("a", 15*1024)) // > 10KB threshold
		args := map[string]interface{}{
			"text_prompt": testPrompt,
		} // Use default model

		expectedResponse := &pb.MultiOutputResponse{ // Correct type
			Status: &statuspb.Status{Code: statuspb.StatusCode_SUCCESS},
			Outputs: []*pb.Output{
				{Data: &pb.Data{Image: &pb.Image{Base64: largeImageBytes}}},
			},
		}

		// Assign the mock function directly
		mockClient.PostModelOutputsFunc = func(ctx context.Context, req *pb.PostModelOutputsRequest, opts ...grpc.CallOption) (*pb.MultiOutputResponse, error) {
			return expectedResponse, nil
		}
		// .Times(1) // Manual mock doesn't track calls

		result, err := handler.callGenerateImage(args)

		if err != nil {
			t.Fatalf("Expected no error, but got: %v", err)
		}
		if result == nil {
			t.Fatal("Expected non-nil result, but got nil")
		}

		content, ok := result.(map[string]interface{})["content"].([]map[string]interface{})
		if !ok || len(content) != 1 {
			t.Fatalf("Expected result content to be a slice with one element, got: %v", result)
		}
		if content[0]["type"] != "text" {
			t.Errorf("Expected content type 'text', got '%v'", content[0]["type"])
		}
		savedPath, ok := content[0]["text"].(string)
		if !ok || !strings.HasPrefix(savedPath, "Image saved to: ") {
			t.Errorf("Expected text content to start with 'Image saved to: ', got '%v'", content[0]["text"])
		} else {
			// Verify the file exists (basic check)
			filePath := strings.TrimPrefix(savedPath, "Image saved to: ")
			if _, statErr := os.Stat(filePath); os.IsNotExist(statErr) {
				t.Errorf("Expected image file to be saved at '%s', but it doesn't exist", filePath)
			}
			// Cleanup: remove the created file
			os.Remove(filePath)
		}
	})

	// --- Test Case 3: API Error ---
	t.Run("API_Error", func(t *testing.T) {
		args := map[string]interface{}{"text_prompt": testPrompt}
		apiError := status.Error(codes.Unauthenticated, "invalid PAT")

		// Assign the mock function directly
		mockClient.PostModelOutputsFunc = func(ctx context.Context, req *pb.PostModelOutputsRequest, opts ...grpc.CallOption) (*pb.MultiOutputResponse, error) {
			return nil, apiError
		}
		// .Times(1) // Manual mock doesn't track calls

		result, err := handler.callGenerateImage(args)

		if err == nil {
			t.Fatal("Expected an error, but got nil")
		}
		if result != nil {
			t.Errorf("Expected nil result on error, got: %v", result)
		}
		// Check if the error is correctly mapped
		if err.Code != -32001 { // Mapped code for Unauthenticated
			t.Errorf("Expected error code -32001, got %d", err.Code)
		}
		if err.Message != "invalid PAT" {
			t.Errorf("Expected error message 'invalid PAT', got '%s'", err.Message)
		}
	})

	// --- Test Case 4: Missing text_prompt ---
	t.Run("Missing_Prompt", func(t *testing.T) {
		args := map[string]interface{}{} // No prompt

		// No API call expected, so don't set PostModelOutputsFunc or set it to panic
		mockClient.PostModelOutputsFunc = func(ctx context.Context, req *pb.PostModelOutputsRequest, opts ...grpc.CallOption) (*pb.MultiOutputResponse, error) {
			panic("PostModelOutputs should not have been called")
		}
		// .Times(0) // Manual mock doesn't track calls

		result, err := handler.callGenerateImage(args)

		if err == nil {
			t.Fatal("Expected an error for missing prompt, but got nil")
		}
		if result != nil {
			t.Errorf("Expected nil result on error, got: %v", result)
		}
		if err.Code != -32602 { // Invalid Params
			t.Errorf("Expected error code -32602, got %d", err.Code)
		}
	})
}

func TestCallInferImage(t *testing.T) {
	handler, mockClient := setupTestHandler(t)
	// defer ctrl.Finish() // No controller needed

	testImageURL := "http://example.com/image.jpg"
	testModelID := "test-image-rec-model"
	testConcept1 := &pb.Concept{Name: "cat", Value: 0.99}
	testConcept2 := &pb.Concept{Name: "hat", Value: 0.95}

	// --- Test Case 1: Successful inference with URL ---
	t.Run("Success_URL", func(t *testing.T) {
		args := map[string]interface{}{
			"image_url": testImageURL,
			"model_id":  testModelID,
		}

		expectedResponse := &pb.MultiOutputResponse{ // Correct type
			Status: &statuspb.Status{Code: statuspb.StatusCode_SUCCESS},
			Outputs: []*pb.Output{
				{Data: &pb.Data{Concepts: []*pb.Concept{testConcept1, testConcept2}}},
			},
		}

		// Assign mock function
		mockClient.PostModelOutputsFunc = func(ctx context.Context, req *pb.PostModelOutputsRequest, opts ...grpc.CallOption) (*pb.MultiOutputResponse, error) {
			if req.ModelId != testModelID {
				return nil, fmt.Errorf("expected model ID %s, got %s", testModelID, req.ModelId)
			}
			if len(req.Inputs) != 1 || req.Inputs[0].Data.Image.Url != testImageURL {
				return nil, fmt.Errorf("invalid input URL")
			}
			return expectedResponse, nil
		}
		// .Times(1)

		result, err := handler.callInferImage(args)

		if err != nil {
			t.Fatalf("Expected no error, but got: %v", err)
		}
		content, ok := result.(map[string]interface{})["content"].([]map[string]interface{})
		if !ok || len(content) != 1 || content[0]["type"] != "text" {
			t.Fatalf("Expected text result, got: %v", result)
		}
		expectedText := "Inference Concepts: cat: 0.99, hat: 0.95"
		if content[0]["text"] != expectedText {
			t.Errorf("Expected text '%s', got '%v'", expectedText, content[0]["text"])
		}
	})

	// --- Test Case 2: Successful inference with Bytes ---
	t.Run("Success_Bytes", func(t *testing.T) {
		// Simulate base64 encoded bytes, potentially with data URI prefix
		testBytes := []byte("data:image/png;base64,ZmFrZS1ieXRlcw==") // "fake-bytes"
		cleanedBytesStr := "ZmFrZS1ieXRlcw=="
		args := map[string]interface{}{
			"image_bytes": string(testBytes), // Pass as string like JSON would
		} // Use default model

		expectedResponse := &pb.MultiOutputResponse{ // Correct type
			Status: &statuspb.Status{Code: statuspb.StatusCode_SUCCESS},
			Outputs: []*pb.Output{
				{Data: &pb.Data{Concepts: []*pb.Concept{testConcept1}}},
			},
		}

		// Assign mock function
		mockClient.PostModelOutputsFunc = func(ctx context.Context, req *pb.PostModelOutputsRequest, opts ...grpc.CallOption) (*pb.MultiOutputResponse, error) {
			if req.ModelId != "general-image-recognition" { // Default model
				return nil, fmt.Errorf("expected default model ID, got %s", req.ModelId)
			}
			// Check if cleaned bytes match
			if len(req.Inputs) != 1 || string(req.Inputs[0].Data.Image.Base64) != cleanedBytesStr {
				return nil, fmt.Errorf("invalid input bytes, expected '%s', got '%s'", cleanedBytesStr, string(req.Inputs[0].Data.Image.Base64))
			}
			return expectedResponse, nil
		}
		// .Times(1)

		result, err := handler.callInferImage(args)

		if err != nil {
			t.Fatalf("Expected no error, but got: %v", err)
		}
		content, ok := result.(map[string]interface{})["content"].([]map[string]interface{})
		if !ok || len(content) != 1 || content[0]["type"] != "text" {
			t.Fatalf("Expected text result, got: %v", result)
		}
		expectedText := "Inference Concepts: cat: 0.99"
		if content[0]["text"] != expectedText {
			t.Errorf("Expected text '%s', got '%v'", expectedText, content[0]["text"])
		}
	})

	// --- Test Case 3: API Error ---
	t.Run("API_Error", func(t *testing.T) {
		args := map[string]interface{}{"image_url": testImageURL}
		apiError := status.Error(codes.Internal, "internal API error")

		// Assign mock function
		mockClient.PostModelOutputsFunc = func(ctx context.Context, req *pb.PostModelOutputsRequest, opts ...grpc.CallOption) (*pb.MultiOutputResponse, error) {
			return nil, apiError
		}
		// .Times(1)

		result, err := handler.callInferImage(args)

		if err == nil {
			t.Fatal("Expected an error, but got nil")
		}
		if result != nil {
			t.Errorf("Expected nil result on error, got: %v", result)
		}
		if err.Code != -32000 { // Mapped code for Internal/Default
			t.Errorf("Expected error code -32000, got %d", err.Code)
		}
	})

	// --- Test Case 4: Missing image_bytes and image_url ---
	t.Run("Missing_Input", func(t *testing.T) {
		args := map[string]interface{}{} // No input

		// Assign mock function to panic if called
		mockClient.PostModelOutputsFunc = func(ctx context.Context, req *pb.PostModelOutputsRequest, opts ...grpc.CallOption) (*pb.MultiOutputResponse, error) {
			panic("PostModelOutputs should not have been called")
		}
		// .Times(0)

		result, err := handler.callInferImage(args)

		if err == nil {
			t.Fatal("Expected an error for missing input, but got nil")
		}
		if result != nil {
			t.Errorf("Expected nil result on error, got: %v", result)
		}
		if err.Code != -32602 { // Invalid Params
			t.Errorf("Expected error code -32602, got %d", err.Code)
		}
	})

	// --- Test Case 5: Both image_bytes and image_url provided ---
	t.Run("Both_Inputs", func(t *testing.T) {
		args := map[string]interface{}{
			"image_bytes": "somebytes",
			"image_url":   testImageURL,
		}

		// Assign mock function to panic if called
		mockClient.PostModelOutputsFunc = func(ctx context.Context, req *pb.PostModelOutputsRequest, opts ...grpc.CallOption) (*pb.MultiOutputResponse, error) {
			panic("PostModelOutputs should not have been called")
		}
		// .Times(0)

		result, err := handler.callInferImage(args)

		if err == nil {
			t.Fatal("Expected an error for providing both inputs, but got nil")
		}
		if result != nil {
			t.Errorf("Expected nil result on error, got: %v", result)
		}
		if err.Code != -32602 { // Invalid Params
			t.Errorf("Expected error code -32602, got %d", err.Code)
		}
	})
}

func TestHandleListResourceTemplates(t *testing.T) {
	handler, _ := setupTestHandler(t) // Mock client not needed for this simple handler

	request := mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "test-req-1",
		Method:  "resources/templates/list",
		// Params are empty for this request
	}

	responsePtr := handler.HandleRequest(request)
	if responsePtr == nil {
		t.Fatal("Expected a response, but got nil")
	}
	response := *responsePtr

	if response.Error != nil {
		t.Fatalf("Expected no error, but got: %+v", response.Error)
	}
	if response.Result == nil {
		t.Fatal("Expected non-nil result, but got nil")
	}

	resultMap, ok := response.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("Expected result to be a map, got: %T", response.Result)
	}

	templates, ok := resultMap["resourceTemplates"].([]map[string]interface{})
	if !ok {
		t.Fatalf("Expected result.resourceTemplates to be a slice of maps, got: %T", resultMap["resourceTemplates"])
	}

	if len(templates) != 1 {
		t.Fatalf("Expected 1 resource template, got %d", len(templates))
	}

	expectedURI := "clarifai://{user_id}/{app_id}/inputs?query={search_term}"
	if templates[0]["uriTemplate"] != expectedURI {
		t.Errorf("Expected uriTemplate '%s', got '%v'", expectedURI, templates[0]["uriTemplate"])
	}
	if templates[0]["name"] != "Search Clarifai Inputs" {
		t.Errorf("Expected name 'Search Clarifai Inputs', got '%v'", templates[0]["name"])
	}
}

// TODO: Add tests for handleInitialize and handleListTools if needed (they are simple now)

func TestHandleListResources(t *testing.T) {
	handler, mockClient := setupTestHandler(t)
	testUserID := "test-user"
	testAppID := "test-app"
	testQuery := "cats"
	testInputID1 := "input-id-1"
	testInputID2 := "input-id-2"

	// --- Test Case 1: Successful search with results ---
	t.Run("Success", func(t *testing.T) {
		request := mcp.JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      "test-list-1",
			Method:  "resources/list",
			Params: mcp.RequestParams{
				URI: fmt.Sprintf("clarifai://%s/%s/inputs?query=%s", testUserID, testAppID, testQuery),
			},
		}

		expectedGRPCResponse := &pb.MultiSearchResponse{
			Status: &statuspb.Status{Code: statuspb.StatusCode_SUCCESS},
			Hits: []*pb.Hit{
				{Input: &pb.Input{Id: testInputID1, Data: &pb.Data{Image: &pb.Image{}}}}, // Image input
				{Input: &pb.Input{Id: testInputID2, Data: &pb.Data{Text: &pb.Text{}}}},   // Text input
			},
		}

		mockClient.PostInputsSearchesFunc = func(ctx context.Context, req *pb.PostInputsSearchesRequest, opts ...grpc.CallOption) (*pb.MultiSearchResponse, error) {
			if req.UserAppId.UserId != testUserID || req.UserAppId.AppId != testAppID {
				return nil, fmt.Errorf("unexpected user/app ID")
			}
			if len(req.Searches) != 1 || len(req.Searches[0].Query.Filters) != 1 || req.Searches[0].Query.Filters[0].Annotation.Data.Text.Raw != testQuery {
				return nil, fmt.Errorf("unexpected search query filter")
			}
			if req.Pagination.Page != 1 || req.Pagination.PerPage != 20 { // Default pagination
				return nil, fmt.Errorf("unexpected pagination")
			}
			return expectedGRPCResponse, nil
		}

		responsePtr := handler.HandleRequest(request)
		if responsePtr == nil {
			t.Fatal("Expected a response, but got nil")
		}
		response := *responsePtr

		if response.Error != nil {
			t.Fatalf("Expected no error, but got: %+v", response.Error)
		}
		resultMap, ok := response.Result.(map[string]interface{})
		if !ok {
			t.Fatalf("Expected result to be a map, got: %T", response.Result)
		}
		resources, ok := resultMap["resources"].([]map[string]interface{})
		if !ok {
			t.Fatalf("Expected result.resources to be a slice, got: %T", resultMap["resources"])
		}
		if len(resources) != 2 {
			t.Fatalf("Expected 2 resources, got %d", len(resources))
		}

		// Check resource 1
		if resources[0]["uri"] != fmt.Sprintf("clarifai://%s/%s/inputs/%s", testUserID, testAppID, testInputID1) {
			t.Errorf("Resource 1 URI mismatch")
		}
		if resources[0]["name"] != testInputID1 {
			t.Errorf("Resource 1 name mismatch")
		}
		if resources[0]["mimeType"] != "image/*" { // Based on input data type
			t.Errorf("Resource 1 mimeType mismatch")
		}

		// Check resource 2
		if resources[1]["uri"] != fmt.Sprintf("clarifai://%s/%s/inputs/%s", testUserID, testAppID, testInputID2) {
			t.Errorf("Resource 2 URI mismatch")
		}
		if resources[1]["name"] != testInputID2 {
			t.Errorf("Resource 2 name mismatch")
		}
		if resources[1]["mimeType"] != "text/plain" { // Based on input data type
			t.Errorf("Resource 2 mimeType mismatch")
		}

		// Check pagination (assuming less results than perPage, so no next cursor)
		if resultMap["nextCursor"] != "" {
			t.Errorf("Expected empty nextCursor, got '%v'", resultMap["nextCursor"])
		}
	})

	// --- Test Case 2: Successful search with pagination ---
	t.Run("Success_Pagination", func(t *testing.T) {
		// Request page 2
		request := mcp.JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      "test-list-page2",
			Method:  "resources/list",
			Params: mcp.RequestParams{
				URI:    fmt.Sprintf("clarifai://%s/%s/inputs?query=%s", testUserID, testAppID, testQuery),
				Cursor: "2", // Request page 2
			},
		}

		// Simulate API returning exactly perPage results (implying more pages)
		hits := make([]*pb.Hit, 20)
		for i := 0; i < 20; i++ {
			hits[i] = &pb.Hit{Input: &pb.Input{Id: fmt.Sprintf("input-page2-%d", i)}}
		}
		expectedGRPCResponse := &pb.MultiSearchResponse{
			Status: &statuspb.Status{Code: statuspb.StatusCode_SUCCESS},
			Hits:   hits,
		}

		mockClient.PostInputsSearchesFunc = func(ctx context.Context, req *pb.PostInputsSearchesRequest, opts ...grpc.CallOption) (*pb.MultiSearchResponse, error) {
			if req.Pagination.Page != 2 { // Check if page 2 was requested
				return nil, fmt.Errorf("expected page 2, got %d", req.Pagination.Page)
			}
			return expectedGRPCResponse, nil
		}

		responsePtr := handler.HandleRequest(request)
		response := *responsePtr

		if response.Error != nil {
			t.Fatalf("Expected no error, but got: %+v", response.Error)
		}
		resultMap, ok := response.Result.(map[string]interface{})
		if !ok {
			t.Fatalf("Expected result to be a map, got: %T", response.Result)
		}
		resources, ok := resultMap["resources"].([]map[string]interface{})
		if !ok || len(resources) != 20 {
			t.Fatalf("Expected 20 resources, got %d", len(resources))
		}

		// Check pagination cursor for next page (page 3)
		if resultMap["nextCursor"] != "3" {
			t.Errorf("Expected nextCursor '3', got '%v'", resultMap["nextCursor"])
		}
	})

	// --- Test Case 3: API Error ---
	t.Run("API_Error", func(t *testing.T) {
		request := mcp.JSONRPCRequest{
			JSONRPC: "2.0", ID: "test-list-err", Method: "resources/list",
			Params: mcp.RequestParams{URI: fmt.Sprintf("clarifai://%s/%s/inputs?query=%s", testUserID, testAppID, testQuery)},
		}
		apiError := status.Error(codes.Unavailable, "service unavailable")

		mockClient.PostInputsSearchesFunc = func(ctx context.Context, req *pb.PostInputsSearchesRequest, opts ...grpc.CallOption) (*pb.MultiSearchResponse, error) {
			return nil, apiError
		}

		responsePtr := handler.HandleRequest(request)
		response := *responsePtr

		if response.Error == nil {
			t.Fatal("Expected an error, but got nil")
		}
		if response.Error.Code != -32004 { // Mapped code for Unavailable
			t.Errorf("Expected error code -32004, got %d", response.Error.Code)
		}
	})

	// --- Test Case 4: Invalid URI ---
	t.Run("Invalid_URI", func(t *testing.T) {
		request := mcp.JSONRPCRequest{
			JSONRPC: "2.0", ID: "test-list-baduri", Method: "resources/list",
			Params: mcp.RequestParams{URI: "invalid-uri-format"},
		}
		mockClient.PostInputsSearchesFunc = func(ctx context.Context, req *pb.PostInputsSearchesRequest, opts ...grpc.CallOption) (*pb.MultiSearchResponse, error) {
			panic("API should not be called with invalid URI")
		}
		responsePtr := handler.HandleRequest(request)
		response := *responsePtr
		if response.Error == nil || response.Error.Code != -32602 {
			t.Fatalf("Expected Invalid Params error (-32602), got: %+v", response.Error)
		}
	})

	// --- Test Case 5: Missing Query in URI ---
	t.Run("Missing_Query", func(t *testing.T) {
		request := mcp.JSONRPCRequest{
			JSONRPC: "2.0", ID: "test-list-noquery", Method: "resources/list",
			Params: mcp.RequestParams{URI: fmt.Sprintf("clarifai://%s/%s/inputs", testUserID, testAppID)}, // No query param
		}
		mockClient.PostInputsSearchesFunc = func(ctx context.Context, req *pb.PostInputsSearchesRequest, opts ...grpc.CallOption) (*pb.MultiSearchResponse, error) {
			panic("API should not be called with missing query")
		}
		responsePtr := handler.HandleRequest(request)
		response := *responsePtr
		if response.Error == nil || response.Error.Code != -32602 {
			t.Fatalf("Expected Invalid Params error (-32602) for missing query, got: %+v", response.Error)
		}
	})

	// --- Test Case 6: List without URI, using default config ---
	t.Run("Success_DefaultConfig", func(t *testing.T) {
		request := mcp.JSONRPCRequest{
			JSONRPC: "2.0", ID: "test-list-default", Method: "resources/list",
			Params: mcp.RequestParams{}, // No URI
		}

		expectedGRPCResponse := &pb.MultiInputResponse{
			Status: &statuspb.Status{Code: statuspb.StatusCode_SUCCESS},
			Inputs: []*pb.Input{
				{Id: "default-input-1", Data: &pb.Data{Image: &pb.Image{}}},
			},
		}

		// Mock ListInputs this time
		mockClient.ListInputsFunc = func(ctx context.Context, req *pb.ListInputsRequest, opts ...grpc.CallOption) (*pb.MultiInputResponse, error) {
			// Check if default user/app ID from config is used
			if req.UserAppId.UserId != "default-user" || req.UserAppId.AppId != "default-app" {
				return nil, fmt.Errorf("expected default user/app ID, got %s/%s", req.UserAppId.UserId, req.UserAppId.AppId)
			}
			if req.Page != 1 || req.PerPage != 20 { // Default pagination
				return nil, fmt.Errorf("unexpected pagination")
			}
			return expectedGRPCResponse, nil
		}
		// Ensure the other mock is nil so we know the right one is called
		mockClient.PostInputsSearchesFunc = nil

		responsePtr := handler.HandleRequest(request)
		response := *responsePtr

		if response.Error != nil {
			t.Fatalf("Expected no error, but got: %+v", response.Error)
		}
		resultMap, ok := response.Result.(map[string]interface{})
		if !ok {
			t.Fatalf("Expected result to be a map, got: %T", response.Result)
		}
		resources, ok := resultMap["resources"].([]map[string]interface{})
		if !ok || len(resources) != 1 {
			t.Fatalf("Expected 1 resource, got %d", len(resources))
		}
		// Check URI uses default user/app ID
		expectedURI := fmt.Sprintf("clarifai://%s/%s/inputs/%s", "default-user", "default-app", "default-input-1")
		if resources[0]["uri"] != expectedURI {
			t.Errorf("Resource URI mismatch, expected '%s', got '%s'", expectedURI, resources[0]["uri"])
		}
	})

	// --- Test Case 7: List without URI, default config not set ---
	t.Run("Error_DefaultConfigNotSet", func(t *testing.T) {
		// Create a handler with config where defaults are empty
		handlerNoDefaults, _ := setupTestHandler(t)
		handlerNoDefaults.config.DefaultUserID = ""
		handlerNoDefaults.config.DefaultAppID = ""

		request := mcp.JSONRPCRequest{
			JSONRPC: "2.0", ID: "test-list-nodefault", Method: "resources/list",
			Params: mcp.RequestParams{}, // No URI
		}

		responsePtr := handlerNoDefaults.HandleRequest(request)
		response := *responsePtr

		if response.Error == nil {
			t.Fatal("Expected an error, but got nil")
		}
		if response.Error.Code != -32602 { // Invalid Params
			t.Errorf("Expected error code -32602, got %d", response.Error.Code)
		}
		if !strings.Contains(response.Error.Message, "configure default-user-id") {
			t.Errorf("Expected error message about missing default config, got: %s", response.Error.Message)
		}
	})

}

func TestHandleReadResource(t *testing.T) {
	handler, mockClient := setupTestHandler(t)
	testUserID := "test-user"
	testAppID := "test-app"
	testInputID := "input-to-read"
	testURI := fmt.Sprintf("clarifai://%s/%s/inputs/%s", testUserID, testAppID, testInputID)

	// --- Test Case 1: Successful read ---
	t.Run("Success", func(t *testing.T) {
		request := mcp.JSONRPCRequest{
			JSONRPC: "2.0", ID: "test-read-1", Method: "resources/read",
			Params: mcp.RequestParams{URI: testURI},
		}

		// Define the expected Input object to be returned by the mock
		expectedInput := &pb.Input{
			Id:     testInputID,
			Data:   &pb.Data{Text: &pb.Text{Raw: "content of the input"}},
			Status: &statuspb.Status{Code: statuspb.StatusCode_INPUT_DOWNLOAD_SUCCESS}, // Example status
		}
		expectedGRPCResponse := &pb.SingleInputResponse{
			Status: &statuspb.Status{Code: statuspb.StatusCode_SUCCESS},
			Input:  expectedInput,
		}

		mockClient.GetInputFunc = func(ctx context.Context, req *pb.GetInputRequest, opts ...grpc.CallOption) (*pb.SingleInputResponse, error) {
			if req.UserAppId.UserId != testUserID || req.UserAppId.AppId != testAppID {
				return nil, fmt.Errorf("unexpected user/app ID")
			}
			if req.InputId != testInputID {
				return nil, fmt.Errorf("unexpected input ID")
			}
			return expectedGRPCResponse, nil
		}

		responsePtr := handler.HandleRequest(request)
		response := *responsePtr

		if response.Error != nil {
			t.Fatalf("Expected no error, but got: %+v", response.Error)
		}
		resultMap, ok := response.Result.(map[string]interface{})
		if !ok {
			t.Fatalf("Expected result to be a map, got: %T", response.Result)
		}
		contents, ok := resultMap["contents"].([]map[string]interface{})
		if !ok || len(contents) != 1 {
			t.Fatalf("Expected result.contents to be a slice with one element, got: %v", resultMap["contents"])
		}

		content := contents[0]
		if content["uri"] != testURI {
			t.Errorf("Expected content URI '%s', got '%v'", testURI, content["uri"])
		}
		if content["mimeType"] != "application/json" {
			t.Errorf("Expected content mimeType 'application/json', got '%v'", content["mimeType"])
		}

		// Check if the text content is the marshalled JSON of the input
		contentText, ok := content["text"].(string)
		if !ok {
			t.Fatalf("Expected content text to be a string, got %T", content["text"])
		}
		// Basic check: does it look like JSON and contain the ID?
		if !strings.Contains(contentText, fmt.Sprintf(`"id": "%s"`, testInputID)) || !strings.HasPrefix(contentText, "{") {
			t.Errorf("Expected content text to be JSON representation of input, got: %s", contentText)
		}
		// Optionally, unmarshal and compare fields for a stricter check
	})

	// --- Test Case 2: Resource Not Found ---
	t.Run("NotFound", func(t *testing.T) {
		request := mcp.JSONRPCRequest{
			JSONRPC: "2.0", ID: "test-read-nf", Method: "resources/read",
			Params: mcp.RequestParams{URI: testURI},
		}

		// Simulate API returning INPUT_DOES_NOT_EXIST status
		expectedGRPCResponse := &pb.SingleInputResponse{
			Status: &statuspb.Status{Code: statuspb.StatusCode_INPUT_DOES_NOT_EXIST, Description: "Input not found"},
		}

		mockClient.GetInputFunc = func(ctx context.Context, req *pb.GetInputRequest, opts ...grpc.CallOption) (*pb.SingleInputResponse, error) {
			return expectedGRPCResponse, nil // Return non-success status, not a gRPC error
		}

		responsePtr := handler.HandleRequest(request)
		response := *responsePtr

		if response.Error == nil {
			t.Fatal("Expected an error, but got nil")
		}
		if response.Error.Code != -32002 { // MCP Not Found code
			t.Errorf("Expected error code -32002, got %d", response.Error.Code)
		}
		if response.Error.Message != "Resource not found" {
			t.Errorf("Expected error message 'Resource not found', got '%s'", response.Error.Message)
		}
		if response.Error.Data != testURI {
			t.Errorf("Expected error data to be the URI '%s', got '%v'", testURI, response.Error.Data)
		}
	})

	// --- Test Case 3: Other API Error ---
	t.Run("API_Error", func(t *testing.T) {
		request := mcp.JSONRPCRequest{
			JSONRPC: "2.0", ID: "test-read-err", Method: "resources/read",
			Params: mcp.RequestParams{URI: testURI},
		}
		apiError := status.Error(codes.PermissionDenied, "permission denied")

		mockClient.GetInputFunc = func(ctx context.Context, req *pb.GetInputRequest, opts ...grpc.CallOption) (*pb.SingleInputResponse, error) {
			return nil, apiError // Return gRPC error
		}

		responsePtr := handler.HandleRequest(request)
		response := *responsePtr

		if response.Error == nil {
			t.Fatal("Expected an error, but got nil")
		}
		if response.Error.Code != -32003 { // Mapped code for PermissionDenied
			t.Errorf("Expected error code -32003, got %d", response.Error.Code)
		}
	})

	// --- Test Case 4: Invalid URI ---
	t.Run("Invalid_URI", func(t *testing.T) {
		request := mcp.JSONRPCRequest{
			JSONRPC: "2.0", ID: "test-read-baduri", Method: "resources/read",
			Params: mcp.RequestParams{URI: "clarifai://user/app/wrongplace/id"}, // Invalid path structure
		}
		mockClient.GetInputFunc = func(ctx context.Context, req *pb.GetInputRequest, opts ...grpc.CallOption) (*pb.SingleInputResponse, error) {
			panic("API should not be called with invalid URI")
		}
		responsePtr := handler.HandleRequest(request)
		response := *responsePtr
		if response.Error == nil || response.Error.Code != -32602 {
			t.Fatalf("Expected Invalid Params error (-32602), got: %+v", response.Error)
		}
	})

	// --- Test Case 5: Missing URI ---
	t.Run("Missing_URI", func(t *testing.T) {
		request := mcp.JSONRPCRequest{
			JSONRPC: "2.0", ID: "test-read-nouri", Method: "resources/read",
			Params: mcp.RequestParams{}, // Missing URI
		}
		mockClient.GetInputFunc = func(ctx context.Context, req *pb.GetInputRequest, opts ...grpc.CallOption) (*pb.SingleInputResponse, error) {
			panic("API should not be called with missing URI")
		}
		responsePtr := handler.HandleRequest(request)
		response := *responsePtr
		if response.Error == nil || response.Error.Code != -32602 {
			t.Fatalf("Expected Invalid Params error (-32602) for missing URI, got: %+v", response.Error)
		}
	})
}
