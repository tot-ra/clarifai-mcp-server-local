package tools

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"

	"clarifai-mcp-server-local/internal/clarifai"
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

	handler := &Handler{
		clarifaiClient: &clarifai.Client{API: mockClient}, // Wrap mock in Client struct
		pat:            "test-pat",
		outputPath:     t.TempDir(), // Use a temporary directory for output path
		timeoutSec:     5,
		logger:         discardLogger,
	}
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

// TODO: Add tests for handleInitialize and handleListTools if needed (they are simple now)
