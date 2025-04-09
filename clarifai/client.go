package clarifai

import (
	"context"
	"log/slog" // Use slog

	"clarifai-mcp-server-local/mcp" // For RPCError type

	pb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// V2ClientInterface defines the subset of pb.V2Client methods used by this server.
// This makes mocking easier for testing.
type V2ClientInterface interface {
	PostModelOutputs(ctx context.Context, in *pb.PostModelOutputsRequest, opts ...grpc.CallOption) (*pb.MultiOutputResponse, error)
	PostInputsSearches(ctx context.Context, in *pb.PostInputsSearchesRequest, opts ...grpc.CallOption) (*pb.MultiSearchResponse, error)
	GetInput(ctx context.Context, in *pb.GetInputRequest, opts ...grpc.CallOption) (*pb.SingleInputResponse, error)
	ListInputs(ctx context.Context, in *pb.ListInputsRequest, opts ...grpc.CallOption) (*pb.MultiInputResponse, error)
	ListModels(ctx context.Context, in *pb.ListModelsRequest, opts ...grpc.CallOption) (*pb.MultiModelResponse, error) // Added ListModels
	GetModel(ctx context.Context, in *pb.GetModelRequest, opts ...grpc.CallOption) (*pb.SingleModelResponse, error)   // Added GetModel
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
	s, ok := status.FromError(err)
	if !ok {
		// Not a gRPC status error, return a generic internal error
		slog.Warn("Non-gRPC error encountered during mapping", "error", err) // Use slog
		return &mcp.RPCError{Code: -32000, Message: "Internal server error: " + err.Error()}
	}

	slog.Debug("Mapping gRPC error", "grpc_code", s.Code(), "grpc_message", s.Message()) // Use slog

	var code int
	// Map gRPC status codes to JSON-RPC error codes (adjust mapping as needed)
	switch s.Code() {
	case codes.Unauthenticated:
		code = -32001 // Custom code for Authentication Failed
	case codes.InvalidArgument:
		code = -32602 // Standard JSON-RPC Invalid Params
	case codes.NotFound:
		code = -32002 // Custom code for Not Found
	case codes.PermissionDenied:
		code = -32003 // Custom code for Permission Denied
	case codes.Unavailable:
		code = -32004 // Custom code for Service Unavailable
	case codes.DeadlineExceeded:
		code = -32005 // Custom code for Timeout
	default:
		code = -32000 // Generic Internal Error for other gRPC errors
	}
	return &mcp.RPCError{Code: code, Message: s.Message()} // Use the gRPC message
}
