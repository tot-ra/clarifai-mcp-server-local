package clarifai

import (
	"context"

	pb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// MockV2Client is a mock implementation of the limited V2ClientInterface for testing.
type MockV2Client struct {
	PostModelOutputsFunc   func(ctx context.Context, in *pb.PostModelOutputsRequest, opts ...grpc.CallOption) (*pb.MultiOutputResponse, error) // Correct return type
	PostInputsSearchesFunc func(ctx context.Context, in *pb.PostInputsSearchesRequest, opts ...grpc.CallOption) (*pb.MultiSearchResponse, error)
	GetInputFunc           func(ctx context.Context, in *pb.GetInputRequest, opts ...grpc.CallOption) (*pb.SingleInputResponse, error)
	ListInputsFunc         func(ctx context.Context, in *pb.ListInputsRequest, opts ...grpc.CallOption) (*pb.MultiInputResponse, error)
	ListModelsFunc         func(ctx context.Context, in *pb.ListModelsRequest, opts ...grpc.CallOption) (*pb.MultiModelResponse, error) // Added ListModelsFunc
	GetModelFunc           func(ctx context.Context, in *pb.GetModelRequest, opts ...grpc.CallOption) (*pb.SingleModelResponse, error)   // Added GetModelFunc
	// Add fields for new interface methods
	ListAnnotationsFunc func(ctx context.Context, in *pb.ListAnnotationsRequest, opts ...grpc.CallOption) (*pb.MultiAnnotationResponse, error)
	GetAnnotationFunc   func(ctx context.Context, in *pb.GetAnnotationRequest, opts ...grpc.CallOption) (*pb.SingleAnnotationResponse, error)
}

// Ensure MockV2Client implements the V2ClientInterface.
var _ V2ClientInterface = (*MockV2Client)(nil)

// PostModelOutputs calls the mock function or returns default values.
func (m *MockV2Client) PostModelOutputs(ctx context.Context, in *pb.PostModelOutputsRequest, opts ...grpc.CallOption) (*pb.MultiOutputResponse, error) { // Correct return type
	if m.PostModelOutputsFunc != nil {
		return m.PostModelOutputsFunc(ctx, in, opts...)
	}
	// Default mock behavior
	return &pb.MultiOutputResponse{}, nil // Correct return type
}

// PostInputsSearches calls the mock function or returns default values.
func (m *MockV2Client) PostInputsSearches(ctx context.Context, in *pb.PostInputsSearchesRequest, opts ...grpc.CallOption) (*pb.MultiSearchResponse, error) {
	if m.PostInputsSearchesFunc != nil {
		return m.PostInputsSearchesFunc(ctx, in, opts...)
	}
	// Default mock behavior
	return &pb.MultiSearchResponse{}, nil
}

// GetInput calls the mock function or returns default values.
func (m *MockV2Client) GetInput(ctx context.Context, in *pb.GetInputRequest, opts ...grpc.CallOption) (*pb.SingleInputResponse, error) {
	if m.GetInputFunc != nil {
		return m.GetInputFunc(ctx, in, opts...)
	}
	// Default mock behavior
	return &pb.SingleInputResponse{}, nil
}

// ListInputs calls the mock function or returns default values.
func (m *MockV2Client) ListInputs(ctx context.Context, in *pb.ListInputsRequest, opts ...grpc.CallOption) (*pb.MultiInputResponse, error) {
	if m.ListInputsFunc != nil {
		return m.ListInputsFunc(ctx, in, opts...)
	}
	// Default mock behavior
	return &pb.MultiInputResponse{}, nil
}

// ListModels calls the mock function or returns default values.
func (m *MockV2Client) ListModels(ctx context.Context, in *pb.ListModelsRequest, opts ...grpc.CallOption) (*pb.MultiModelResponse, error) {
	if m.ListModelsFunc != nil {
		return m.ListModelsFunc(ctx, in, opts...)
	}
	// Default mock behavior
	return &pb.MultiModelResponse{}, nil
}

// GetModel calls the mock function or returns default values.
func (m *MockV2Client) GetModel(ctx context.Context, in *pb.GetModelRequest, opts ...grpc.CallOption) (*pb.SingleModelResponse, error) {
	if m.GetModelFunc != nil {
		return m.GetModelFunc(ctx, in, opts...)
	}
	// Default mock behavior
	return &pb.SingleModelResponse{}, nil
}

// ListAnnotations calls the mock function or returns default values.
func (m *MockV2Client) ListAnnotations(ctx context.Context, in *pb.ListAnnotationsRequest, opts ...grpc.CallOption) (*pb.MultiAnnotationResponse, error) {
	if m.ListAnnotationsFunc != nil {
		return m.ListAnnotationsFunc(ctx, in, opts...)
	}
	// Default mock behavior
	return &pb.MultiAnnotationResponse{}, nil
}

// GetAnnotation calls the mock function or returns default values.
func (m *MockV2Client) GetAnnotation(ctx context.Context, in *pb.GetAnnotationRequest, opts ...grpc.CallOption) (*pb.SingleAnnotationResponse, error) {
	if m.GetAnnotationFunc != nil {
		return m.GetAnnotationFunc(ctx, in, opts...)
	}
	// Default mock behavior
	return &pb.SingleAnnotationResponse{}, nil
}

// Helper to create a context with expected metadata for testing PostModelOutputs calls
func ContextWithMockAuth(pat string) context.Context {
	md := metadata.Pairs("Authorization", "Key "+pat)
	return metadata.NewOutgoingContext(context.Background(), md)
}
