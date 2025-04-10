package clarifai

import (
	"context"
	"errors" // Added for errors.As
	"fmt"    // Added for custom error formatting
	"log/slog"

	"clarifai-mcp-server-local/mcp" // For RPCError type

	pb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api"
	statuspb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api/status" // Added for status codes
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes" // Added for gRPC codes
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// APIStatusError represents an error derived from a non-SUCCESS Clarifai API status code.
type APIStatusError struct {
	StatusCode codes.Code
	Message    string
	Details    string
	ReqId      string // Added ReqId
}

func (e *APIStatusError) Error() string {
	// Include ReqId in the error string for better logging
	return fmt.Sprintf("API error (%s): %s - %s (ReqId: %s)", e.StatusCode.String(), e.Message, e.Details, e.ReqId)
}

// NewAPIStatusError creates a new APIStatusError from a statuspb.Status.
func NewAPIStatusError(st *statuspb.Status) *APIStatusError {
	// Always map non-SUCCESS statuspb codes to a generic gRPC code.
	// This avoids issues with potentially undefined statuspb constants.
	// We use codes.Internal as a general indicator of an API-level failure.
	grpcCode := codes.Internal
	if st.Code == statuspb.StatusCode_SUCCESS {
		// This case should ideally not happen when creating an error,
		// but handle it defensively.
		grpcCode = codes.OK
		slog.Warn("Creating APIStatusError from SUCCESS statuspb.Status", "code", st.Code, "description", st.Description)
	} else {
		slog.Debug("Mapping non-SUCCESS statuspb.Status to gRPC Internal", "statuspb_code", st.Code, "description", st.Description)
	}

	return &APIStatusError{
		StatusCode: grpcCode,
		Message:    st.Description, // Use the description from the status
		Details:    st.Details,     // Include details if available
		ReqId:      st.ReqId,       // Capture ReqId
	}
}

// V2ClientInterface defines the subset of pb.V2Client methods used by this server.
// This makes mocking easier for testing.
type V2ClientInterface interface {
	PostModelOutputs(ctx context.Context, in *pb.PostModelOutputsRequest, opts ...grpc.CallOption) (*pb.MultiOutputResponse, error)
	PostInputsSearches(ctx context.Context, in *pb.PostInputsSearchesRequest, opts ...grpc.CallOption) (*pb.MultiSearchResponse, error)
	GetInput(ctx context.Context, in *pb.GetInputRequest, opts ...grpc.CallOption) (*pb.SingleInputResponse, error)
	ListInputs(ctx context.Context, in *pb.ListInputsRequest, opts ...grpc.CallOption) (*pb.MultiInputResponse, error)
	ListModels(ctx context.Context, in *pb.ListModelsRequest, opts ...grpc.CallOption) (*pb.MultiModelResponse, error) // Added ListModels
	GetModel(ctx context.Context, in *pb.GetModelRequest, opts ...grpc.CallOption) (*pb.SingleModelResponse, error)    // Added GetModel
	// Annotation methods used in handler
	ListAnnotations(ctx context.Context, in *pb.ListAnnotationsRequest, opts ...grpc.CallOption) (*pb.MultiAnnotationResponse, error)
	GetAnnotation(ctx context.Context, in *pb.GetAnnotationRequest, opts ...grpc.CallOption) (*pb.SingleAnnotationResponse, error)
	// Add other methods here if they become needed by the server
}

// Client wraps the gRPC connection and the client interface.
type Client struct {
	Conn *grpc.ClientConn
	API  V2ClientInterface // Use the interface type
}

// NewClient establishes a gRPC connection to the Clarifai API and returns a Client.
func NewClient(apiAddress string) (*Client, error) {
	slog.Debug("Determining connection type for gRPC server", "address", apiAddress) // Use slog
	var creds grpc.DialOption

	// TODO: Add configuration option for insecure connections if needed for local testing.
	// For now, always use TLS.
	// if strings.HasPrefix(apiAddress, "localhost:") || strings.HasPrefix(apiAddress, "127.0.0.1:") {
	// 	creds = grpc.WithTransportCredentials(insecure.NewCredentials())
	// 	log.Println("Using insecure gRPC credentials for local connection.")
	// } else
	{
		// Assume public API requires TLS
		// TODO: Add proper certificate handling if needed, for now using default secure creds
		creds = grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, ""))
		slog.Debug("Using secure gRPC credentials (TLS) for non-local connection.") // Use slog
	}

	slog.Debug("Attempting to dial gRPC address", "address", apiAddress) // Use slog
	conn, err := grpc.Dial(apiAddress, creds)
	if err != nil {
		slog.Error("Failed to connect to gRPC server", "address", apiAddress, "error", err) // Use slog
		return nil, err                                                                     // Return error
	}
	// slog.Info("gRPC connection established", "address", apiAddress) // Use slog // Commented out to reduce startup noise
	apiClient := pb.NewV2Client(conn) // This is the real client implementing the full interface

	// The real apiClient implicitly satisfies the smaller V2ClientInterface
	return &Client{
		Conn: conn,
		API:  apiClient, // Assign the real client to the interface field
	}, nil
}

// Close closes the underlying gRPC connection.
func (c *Client) Close() error {
	if c.Conn != nil {
		slog.Debug("Closing gRPC connection...") // Use slog
		return c.Conn.Close()
	}
	return nil
}

// CreateContextWithAuth adds the PAT authorization header to the context.
func CreateContextWithAuth(ctx context.Context, pat string) context.Context {
	// Add Authorization metadata for Clarifai API calls
	return metadata.NewOutgoingContext(ctx, metadata.Pairs("Authorization", "Key "+pat))
}

// MapGRPCErrorToJSONRPC maps gRPC errors to JSON-RPC error objects.
func MapGRPCErrorToJSONRPC(err error) *mcp.RPCError {
	if err == nil {
		return nil
	}

	// Check for our custom APIStatusError first
	var apiStatusErr *APIStatusError
	if errors.As(err, &apiStatusErr) {
		slog.Debug("Mapping APIStatusError", "grpc_code", apiStatusErr.StatusCode, "message", apiStatusErr.Message, "req_id", apiStatusErr.ReqId)
		// Use the mapped gRPC code from the custom error
		// Include ReqId and Details in the Data field
		errorData := map[string]interface{}{
			"details": apiStatusErr.Details,
			"req_id":  apiStatusErr.ReqId,
		}
		return &mcp.RPCError{Code: int(apiStatusErr.StatusCode), Message: apiStatusErr.Message, Data: errorData}
	}

	// Check for standard gRPC status error
	s, ok := status.FromError(err)
	if !ok {
		// Not a gRPC status error or our custom error, return a generic internal error
		slog.Warn("Non-gRPC/APIStatus error encountered during mapping", "error", err)
		return &mcp.RPCError{Code: -32000, Message: "Internal server error: " + err.Error()}
	}

	slog.Debug("Mapping direct gRPC error", "grpc_code", s.Code(), "grpc_message", s.Message())
	// Use the original gRPC code directly from the status error
	code := int(s.Code())
	// Include the original gRPC message in the data field for more context
	errorData := map[string]interface{}{
		"grpc_message": s.Message(),
		"original_error": err.Error(), // Include the original error string
	}
	return &mcp.RPCError{Code: code, Message: s.Message(), Data: errorData}
}
