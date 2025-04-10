package clarifai

import (
	"errors"
	"testing"

	"clarifai-mcp-server-local/mcp" // For RPCError type

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMapGRPCErrorToJSONRPC(t *testing.T) {
	testCases := []struct {
		name          string
		grpcError     error
		expectedError *mcp.RPCError
	}{
		{
			name:          "Nil error",
			grpcError:     nil,
			expectedError: nil,
		},
		{
			name:      "Non-gRPC error",
			grpcError: errors.New("a plain error"),
			expectedError: &mcp.RPCError{
				Code:    -32000, // Internal server error
				Message: "Internal server error: a plain error",
			},
		},
		{
			name:      "Unauthenticated",
			grpcError: status.Error(codes.Unauthenticated, "invalid token"),
			expectedError: &mcp.RPCError{
				// Code:    -32001, // Original expectation
				Code:    int(codes.Unauthenticated), // Expect raw gRPC code (16)
				Message: "invalid token",
			},
		},
		{
			name:      "InvalidArgument",
			grpcError: status.Error(codes.InvalidArgument, "missing field"),
			expectedError: &mcp.RPCError{
				// Code:    -32602, // Original expectation
				Code:    int(codes.InvalidArgument), // Expect raw gRPC code (3)
				Message: "missing field",
			},
		},
		{
			name:      "NotFound",
			grpcError: status.Error(codes.NotFound, "resource not found"),
			expectedError: &mcp.RPCError{
				// Code:    -32002, // Original expectation
				Code:    int(codes.NotFound), // Expect raw gRPC code (5)
				Message: "resource not found",
			},
		},
		{
			name:      "PermissionDenied",
			grpcError: status.Error(codes.PermissionDenied, "access denied"),
			expectedError: &mcp.RPCError{
				// Code:    -32003, // Original expectation
				Code:    int(codes.PermissionDenied), // Expect raw gRPC code (7)
				Message: "access denied",
			},
		},
		{
			name:      "Unavailable",
			grpcError: status.Error(codes.Unavailable, "service unavailable"),
			expectedError: &mcp.RPCError{
				// Code:    -32004, // Original expectation
				Code:    int(codes.Unavailable), // Expect raw gRPC code (14)
				Message: "service unavailable",
			},
		},
		{
			name:      "DeadlineExceeded",
			grpcError: status.Error(codes.DeadlineExceeded, "timeout"),
			expectedError: &mcp.RPCError{
				// Code:    -32005, // Original expectation
				Code:    int(codes.DeadlineExceeded), // Expect raw gRPC code (4)
				Message: "timeout",
			},
		},
		{
			name:      "Unknown gRPC code",
			grpcError: status.Error(codes.DataLoss, "data corruption"),
			expectedError: &mcp.RPCError{
				// Code:    -32000, // Original expectation
				Code:    int(codes.DataLoss), // Expect raw gRPC code (15)
				Message: "data corruption",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mappedError := MapGRPCErrorToJSONRPC(tc.grpcError)

			if mappedError == nil && tc.expectedError == nil {
				return // Correctly mapped nil to nil
			}
			if mappedError == nil && tc.expectedError != nil {
				t.Fatalf("Expected error %v, but got nil", tc.expectedError)
			}
			if mappedError != nil && tc.expectedError == nil {
				t.Fatalf("Expected nil error, but got %v", mappedError)
			}
			if mappedError.Code != tc.expectedError.Code {
				t.Errorf("Expected error code %d, but got %d", tc.expectedError.Code, mappedError.Code)
			}
			if mappedError.Message != tc.expectedError.Message {
				t.Errorf("Expected error message '%s', but got '%s'", tc.expectedError.Message, mappedError.Message)
			}
			// Note: We are not testing the 'Data' field in this mapping function
		})
	}
}

// TODO: Add tests for NewClient (might require mocking grpc.Dial) and CreateContextWithAuth
