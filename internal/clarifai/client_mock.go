package clarifai

import (
	"context"

	pb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// MockV2Client is a mock implementation of the limited V2ClientInterface for testing.
type MockV2Client struct {
	PostModelOutputsFunc func(ctx context.Context, in *pb.PostModelOutputsRequest, opts ...grpc.CallOption) (*pb.MultiOutputResponse, error) // Correct return type
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

// Helper to create a context with expected metadata for testing PostModelOutputs calls
func ContextWithMockAuth(pat string) context.Context {
	md := metadata.Pairs("Authorization", "Key "+pat)
	return metadata.NewOutgoingContext(context.Background(), md)
}
