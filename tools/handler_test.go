package tools

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"clarifai-mcp-server-local/clarifai"
	"clarifai-mcp-server-local/config"
	"clarifai-mcp-server-local/mcp"

	pb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api" // Import the pb package
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc" // Import grpc for CallOption
)

// Mock Clarifai Client for testing
type MockClarifaiAPIClient struct {
	mock.Mock
	// clarifai.V2ClientInterface // Remove embedded interface to rely on duck typing for mocks
}

// Implement methods used by the handler using the correct pb types and grpc.CallOption
func (m *MockClarifaiAPIClient) GetInput(ctx context.Context, req *pb.GetInputRequest, opts ...grpc.CallOption) (*pb.SingleInputResponse, error) {
	// To satisfy the mock call with variadic args, we might need to pass them explicitly or handle them.
	// For simplicity in testing parsing logic, we can ignore opts in the mock assertion if not needed.
	args := m.Called(ctx, req) // Pass only ctx and req to Called if opts are not asserted
	if args.Get(0) == nil {
		return nil, args.Error(1) // Return error specified in mock setup
	}
	// Return response specified in mock setup
	return args.Get(0).(*pb.SingleInputResponse), args.Error(1)
}

func (m *MockClarifaiAPIClient) GetModel(ctx context.Context, req *pb.GetModelRequest, opts ...grpc.CallOption) (*pb.SingleModelResponse, error) {
	args := m.Called(ctx, req) // Pass only ctx and req to Called
	if args.Get(0) == nil {
		return nil, args.Error(1) // Return error specified in mock setup
	}
	// Return response specified in mock setup
	return args.Get(0).(*pb.SingleModelResponse), args.Error(1)
}

// Add mocks for annotation methods if/when implemented using pb types and grpc.CallOption
func (m *MockClarifaiAPIClient) ListAnnotations(ctx context.Context, req *pb.ListAnnotationsRequest, opts ...grpc.CallOption) (*pb.ListAnnotationsResponse, error) {
	args := m.Called(ctx, req) // Pass only ctx and req to Called
	if args.Get(0) == nil {
		return nil, args.Error(1) // Return error specified in mock setup
	}
	// Return response specified in mock setup
	return args.Get(0).(*pb.ListAnnotationsResponse), args.Error(1)
}
func (m *MockClarifaiAPIClient) GetAnnotation(ctx context.Context, req *pb.GetAnnotationRequest, opts ...grpc.CallOption) (*pb.SingleAnnotationResponse, error) {
	args := m.Called(ctx, req) // Pass only ctx and req to Called
	if args.Get(0) == nil {
		return nil, args.Error(1) // Return error specified in mock setup
	}
	// Return response specified in mock setup
	return args.Get(0).(*pb.SingleAnnotationResponse), args.Error(1)
}

// Add ListInputs mock (needed for interface satisfaction)
func (m *MockClarifaiAPIClient) ListInputs(ctx context.Context, req *pb.ListInputsRequest, opts ...grpc.CallOption) (*pb.ListInputsResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*pb.ListInputsResponse), args.Error(1)
}

// Add PostModelOutputs mock (needed for interface satisfaction)
func (m *MockClarifaiAPIClient) PostModelOutputs(ctx context.Context, req *pb.PostModelOutputsRequest, opts ...grpc.CallOption) (*pb.PostModelOutputsResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*pb.PostModelOutputsResponse), args.Error(1)
}

// Add PostAnnotationsSearches mock if needed
// func (m *MockClarifaiAPIClient) PostAnnotationsSearches(...) ...

func setupTestHandler() *Handler {
	cfg := &config.Config{
		Pat:        "test-pat",
		TimeoutSec: 10,
		OutputPath: "/tmp",
	}
	mockAPIClient := new(MockClarifaiAPIClient)
	mockClient := &clarifai.Client{API: mockAPIClient} // Use the mock API client
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger) // Set default logger for handler

	return NewHandler(mockClient, cfg)
}

func TestHandleReadResource_URIParsing(t *testing.T) {
	handler := setupTestHandler()

	testCases := []struct {
		name          string
		uri           string
		expectedError bool
		errorCode     int // Expected MCP error code if error is expected
		errorContains string
	}{
		// --- Valid URIs ---
		{
			name:          "Valid Get Input",
			uri:           "clarifai://user1/app1/inputs/input123",
			expectedError: false, // Expecting GetInput to be called (mocked later if needed)
		},
		{
			name:          "Valid Get Model",
			uri:           "clarifai://user1/app1/models/model456",
			expectedError: false, // Expecting GetModel to be called
		},
		{
			name:          "Valid Get Annotation (Placeholder)",
			uri:           "clarifai://user1/app1/annotations/anno789",
			expectedError: true, // Expecting "not yet implemented" error
			errorCode:     -32000,
			errorContains: "GetAnnotation not yet implemented",
		},
		{
			name:          "Valid List Annotations (Placeholder)",
			uri:           "clarifai://user1/app1/annotations",
			expectedError: true, // Expecting "not yet implemented" error
			errorCode:     -32000,
			errorContains: "ListAnnotations not yet implemented",
		},
		{
			name:          "Valid List Annotations with Query (Placeholder)",
			uri:           "clarifai://user1/app1/annotations?query=test",
			expectedError: true, // Expecting "not yet implemented" error
			errorCode:     -32000,
			errorContains: "PostAnnotationsSearches (search with query) not yet implemented",
		},
		{
			name:          "Valid List Annotations for Input (Placeholder)",
			uri:           "clarifai://user1/app1/inputs/input123/annotations",
			expectedError: true, // Expecting "not yet implemented" error
			errorCode:     -32000,
			errorContains: "ListAnnotations for Input not yet implemented",
		},
		// --- Invalid URIs ---
		{
			name:          "Missing URI",
			uri:           "",
			expectedError: true,
			errorCode:     -32602,
			errorContains: "Missing required 'uri' parameter",
		},
		{
			name:          "Invalid Scheme",
			uri:           "http://user1/app1/inputs/input123",
			expectedError: true,
			errorCode:     -32602,
			errorContains: "Invalid URI format. Expected clarifai://...",
		},
		{
			name:          "Missing User ID (Host)",
			uri:           "clarifai:///app1/inputs/input123",
			expectedError: true,
			errorCode:     -32602,
			errorContains: "Missing user_id",
		},
		{
			name:          "Missing App ID",
			uri:           "clarifai://user1//inputs/input123", // Note the double slash
			expectedError: true,
			errorCode:     -32602,
			errorContains: "Missing app_id",
		},
		{
			name:          "Too Few Path Parts (1)",
			uri:           "clarifai://user1/app1",
			expectedError: true,
			errorCode:     -32602,
			errorContains: "Expected at least clarifai://{user_id}/{app_id}/{resource_type}",
		},
		{
			name:          "Too Few Path Parts (0)",
			uri:           "clarifai://user1/",
			expectedError: true,
			errorCode:     -32602,
			errorContains: "Expected at least clarifai://{user_id}/{app_id}/{resource_type}",
		},
		{
			name:          "Too Many Path Parts (5)",
			uri:           "clarifai://user1/app1/inputs/id1/annotations/sub",
			expectedError: true,
			errorCode:     -32000, // Generic internal error for unexpected path length
			errorContains: "Unexpected number of path segments: 5",
		},
		{
			name:          "Invalid Resource Type for List (case 2)",
			uri:           "clarifai://user1/app1/datasets", // Assuming datasets aren't listable via read yet
			expectedError: true,
			errorCode:     -32000,
			errorContains: "listing resource type 'datasets' via resources/read is not supported",
		},
		{
			name:          "Invalid Resource Type for Get (case 3)",
			uri:           "clarifai://user1/app1/workflows/wf1", // Assuming workflows aren't gettable via read yet
			expectedError: true,
			errorCode:     -32000,
			errorContains: "reading specific resource type 'workflows' is not supported",
		},
		{
			name:          "Invalid Parent Resource Type for Sub-List (case 4)",
			uri:           "clarifai://user1/app1/models/model1/annotations",
			expectedError: true,
			errorCode:     -32000,
			errorContains: "listing sub-resources for parent type 'models' is not supported",
		},
		{
			name:          "Invalid Sub-Resource Type for Sub-List (case 4)",
			uri:           "clarifai://user1/app1/inputs/input1/versions",
			expectedError: true,
			errorCode:     -32000,
			errorContains: "unsupported sub-resource type 'versions' for parent 'inputs'",
		},
		{
			name:          "Empty Resource ID for Get (case 3)",
			uri:           "clarifai://user1/app1/inputs/",
			expectedError: true,
			errorCode:     -32000,
			errorContains: "invalid URI for specific resource read: resource ID cannot be empty or '*'",
		},
		{
			name:          "Wildcard Resource ID for Get (case 3)",
			uri:           "clarifai://user1/app1/inputs/*",
			expectedError: true,
			errorCode:     -32000,
			errorContains: "invalid URI for specific resource read: resource ID cannot be empty or '*'",
		},
		{
			name:          "Empty Parent ID for Sub-List (case 4)",
			uri:           "clarifai://user1/app1/inputs//annotations",
			expectedError: true,
			errorCode:     -32000,
			errorContains: "invalid URI for sub-resource list: parent resource ID cannot be empty or '*'",
		},
		{
			name:          "Wildcard Parent ID for Sub-List (case 4)",
			uri:           "clarifai://user1/app1/inputs/*/annotations",
			expectedError: true,
			errorCode:     -32000,
			errorContains: "invalid URI for sub-resource list: parent resource ID cannot be empty or '*'",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Mock necessary calls if the URI is expected to be valid for a specific action
			// For now, we primarily test parsing errors or placeholder errors.
			// Example for a valid GetInput call (if we wanted to test deeper):
			// if tc.uri == "clarifai://user1/app1/inputs/input123" {
			// 	mockAPI := handler.clarifaiClient.API.(*MockClarifaiAPIClient)
			// 	mockAPI.On("GetInput", mock.Anything, mock.AnythingOfType("*clarifai.GetInputRequest")).Return(&clarifai.SingleInputResponse{Status: &statuspb.Status{Code: statuspb.StatusCode_SUCCESS}, Input: &pb.Input{Id: "input123"}}, nil)
			// }

			req := mcp.JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      "test-id-" + tc.name,
				Method:  "resources/read", // Testing the read handler directly
				Params:  mcp.RequestParams{URI: tc.uri},
			}

			resp := handler.handleReadResource(req)

			if tc.expectedError {
				assert.NotNil(t, resp.Error, "Expected an error but got none")
				if resp.Error != nil {
					assert.Equal(t, tc.errorCode, resp.Error.Code, "Error code mismatch")
					assert.Contains(t, resp.Error.Message, tc.errorContains, "Error message mismatch")
				}
			} else {
				assert.Nil(t, resp.Error, "Expected no error but got: %v", resp.Error)
				// Optionally assert on resp.Result if testing successful calls
			}
		})
	}
}

// Add more tests for handleCallTool, handleInitialize etc. if needed
