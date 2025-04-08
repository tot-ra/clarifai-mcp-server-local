package main

import (
	"bufio"
	"context"

	// "encoding/base64" // No longer needed as we write raw bytes
	"encoding/json"
	"flag" // Import flag package
	"fmt"  // Needed for filename generation
	"io"
	"log"       // Uncomment log for potential debugging during implementation
	"math/rand" // Added for filename generation
	"os"        // Needed for joining paths
	"strings"
	"sync"
	"time" // Added for filename generation

	// "crypto/tls" // Removed unused import

	// gRPC related imports
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"       // Changed from _ import
	"google.golang.org/grpc/credentials" // Added

	// Added for insecure local connections
	"google.golang.org/grpc/metadata" // Changed from _ import
	"google.golang.org/grpc/status"

	// Use gogo/protobuf types for Struct
	pb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api"
	statuspb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api/status" // Import status explicitly
)

// --- JSON-RPC Structures (same as before) ---
type JSONRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      interface{}   `json:"id"`
	Method  string        `json:"method"`
	Params  RequestParams `json:"params"`
}
type RequestParams struct {
	Name            string                 `json:"name,omitempty"`
	Arguments       map[string]interface{} `json:"arguments,omitempty"` // Changed 'Args' field name and json tag from 'args' to 'arguments'
	ProtocolVersion string                 `json:"protocolVersion,omitempty"`
	ClientInfo      map[string]interface{} `json:"clientInfo,omitempty"`
	Capabilities    map[string]interface{} `json:"capabilities,omitempty"`
	PAT             string                 `json:"pat,omitempty"`
	ImageBytes      string                 `json:"image_bytes,omitempty"`
	ImageURL        string                 `json:"image_url,omitempty"`
	ModelID         string                 `json:"model_id,omitempty"`
	AppID           string                 `json:"app_id,omitempty"`
	UserID          string                 `json:"user_id,omitempty"`
	TextPrompt      string                 `json:"text_prompt,omitempty"`
}
type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}
type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// --- Server Interface & Stdio Implementation (from example) ---

type Server interface {
	Start(ctx context.Context)
	ReadChannel() <-chan JSONRPCRequest
	WriteChannel() chan<- JSONRPCResponse
	Wait()
	Close() error
}

type StdioServer struct {
	reader      io.Reader
	writer      io.Writer
	readChan    chan JSONRPCRequest
	writeChan   chan JSONRPCResponse
	shutdown    chan struct{}
	shutdownCtx context.Context
	cancelFunc  context.CancelFunc
	wg          sync.WaitGroup
}

func NewStdioServer(reader io.Reader, writer io.Writer) *StdioServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &StdioServer{
		reader:      reader,
		writer:      writer,
		readChan:    make(chan JSONRPCRequest),
		writeChan:   make(chan JSONRPCResponse),
		shutdown:    make(chan struct{}),
		shutdownCtx: ctx,
		cancelFunc:  cancel,
	}
}

func (s *StdioServer) Start(ctx context.Context) {
	s.wg.Add(2) // Add wait group counter for reader and writer goroutines

	// Start reader goroutine
	go func() {
		defer s.wg.Done()
		defer close(s.readChan) // Close readChan when reader exits
		scanner := bufio.NewScanner(s.reader)
		for scanner.Scan() {
			select {
			case <-s.shutdownCtx.Done(): // Check for shutdown signal
				// log.Println("StdioServer reader shutting down.") // Keep logging commented
				return
			default:
				line := scanner.Bytes()
				// log.Printf("MCP RECV <<< %s", string(line)) // Keep logging commented
				var request JSONRPCRequest
				err := json.Unmarshal(line, &request)
				if err != nil {
					// log.Printf("Error unmarshalling request: %v", err) // Keep logging commented
					continue
				}
				s.readChan <- request
			}
		}
		if err := scanner.Err(); err != nil && err != io.EOF {
			// log.Printf("Error reading from stdin: %v", err) // Keep logging commented
		}
		// log.Println("StdioServer reader finished.") // Keep logging commented
		s.cancelFunc() // Signal shutdown if reader finishes
	}()

	// Start writer goroutine
	go func() {
		defer s.wg.Done()
		writer := bufio.NewWriter(s.writer)
		for {
			select {
			case <-s.shutdownCtx.Done(): // Check for shutdown signal
				// log.Println("StdioServer writer shutting down.") // Keep logging commented
				return
			case response, ok := <-s.writeChan:
				if !ok {
					// log.Println("StdioServer write channel closed.") // Keep logging commented
					return // Exit if write channel is closed
				}
				respBytes, err := json.Marshal(response)
				if err != nil {
					// log.Printf("Error marshalling response: %v", err) // Keep logging commented
					continue
				}
				// log.Printf("MCP SEND >>> %s", string(respBytes)) // Keep logging commented
				_, err = writer.Write(respBytes)
				if err != nil {
					// log.Printf("Error writing response bytes: %v", err) // Keep logging commented
					s.cancelFunc() // Signal shutdown on write error
					return
				}
				_, err = writer.WriteString("\n") // Add newline delimiter
				if err != nil {
					// log.Printf("Error writing newline: %v", err) // Keep logging commented
					s.cancelFunc() // Signal shutdown on write error
					return
				}
				err = writer.Flush() // Flush buffer after each message
				if err != nil {
					// log.Printf("Error flushing writer: %v", err) // Keep logging commented
					s.cancelFunc() // Signal shutdown on write error
					return
				}
			}
		}
	}()
}

func (s *StdioServer) ReadChannel() <-chan JSONRPCRequest {
	return s.readChan
}

func (s *StdioServer) WriteChannel() chan<- JSONRPCResponse {
	return s.writeChan
}

func (s *StdioServer) Wait() {
	<-s.shutdownCtx.Done() // Wait until context is cancelled
	s.wg.Wait()            // Wait for reader and writer goroutines to finish
	// log.Println("StdioServer Wait completed.") // Keep logging commented
}

func (s *StdioServer) Close() error {
	s.cancelFunc()     // Trigger shutdown
	s.Wait()           // Wait for clean shutdown
	close(s.writeChan) // Close writeChan after writer goroutine exits
	return nil         // Assuming no specific close error for stdio
}

// --- End Server Interface & Stdio Implementation ---

// Use actual V2Client from protobuf
type V2Client pb.V2Client

// serverState holds the state for a single MCP connection.
// Removed authentication-related fields (isAuthenticated, userID, appID, pat)
// We now rely solely on the command-line PAT flag passed into the handler goroutine.
type serverState struct {
	grpcClient V2Client
	grpcConn   *grpc.ClientConn
	mu         sync.RWMutex // Keep mutex for potential future state needs or client access
}

var globalState serverState

// Define the tools map globally with full schemas
// Removed authenticate_pat tool definition
var toolsDefinitionMap = map[string]interface{}{ // Renamed to avoid confusion
	"infer_image": map[string]interface{}{
		"description": "Performs inference on an image using a specified or default Clarifai model. Requires the server to be started with a valid --pat flag.", // Updated description
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
		"description": "Generates an image based on a text prompt using a specified or default Clarifai text-to-image model. Requires the server to be started with a valid --pat flag.", // Updated description
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

func main() {
	// Define command-line flag for PAT
	patFlag := flag.String("pat", "", "Clarifai Personal Access Token (optional)")
	outputPathFlag := flag.String("output-path", "", "Directory to save large generated images (optional)") // Added output-path flag
	flag.Parse()                                                                                            // Parse command-line flags

	// // --- Setup gRPC Connection ---
	// grpcAddress := os.Getenv("CLARIFAI_GRPC_API_ADDRESS")
	// if grpcAddress == "" {
	// 	// Default to the public Clarifai API endpoint if not set locally
	// 	grpcAddress = "api.clarifai.com:443"
	// 	log.Printf("CLARIFAI_GRPC_API_ADDRESS not set, defaulting to public API: %s", grpcAddress)
	// } else {
	// 	log.Printf("Using gRPC address from CLARIFAI_GRPC_API_ADDRESS: %s", grpcAddress)
	// }

	grpcAddress := "api.clarifai.com:443"

	conn, client := setupGRPCConnection(grpcAddress)
	globalState.grpcConn = conn
	globalState.grpcClient = client // Store the client in global state
	defer func() {
		if err := conn.Close(); err != nil {
			log.Printf("Error closing gRPC connection: %v", err)
		}
	}() // Ensure connection is closed on exit

	log.Println("Starting Clarifai MCP Server Bridge on stdio with gRPC connection...")

	// No longer initializing global auth state here.
	// The patFlag is passed directly to the handler goroutine.

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use the new StdioServer implementation
	server := NewStdioServer(os.Stdin, os.Stdout)
	server.Start(ctx) // Start reader/writer goroutines

	// Main processing loop (reading from channel) - Pass patFlag and outputPathFlag pointers
	go func(cliPat *string, cliOutputPath *string) {
		// Seed random number generator for filenames
		rand.Seed(time.Now().UnixNano())

		// Define image size threshold
		const imageSizeThreshold = 10 * 1024 // 10 KB

		for request := range server.ReadChannel() {
			// log.Printf("Processing method: %s", request.Method) // Keep logging commented

			// Check for notifications first and ignore them
			if strings.HasPrefix(request.Method, "notifications/") {
				// log.Printf("Ignoring notification: %s", request.Method) // Keep logging commented
				continue // Don't send a response for notifications
			}

			var response JSONRPCResponse
			switch request.Method {
			case "initialize":
				// log.Println("--- Handling initialize ---") // Keep logging commented
				response = JSONRPCResponse{
					JSONRPC: "2.0",
					ID:      request.ID,
					Result: map[string]interface{}{
						"protocolVersion": "2024-11-05",
						"serverInfo": map[string]interface{}{
							"name":    "clarifai-mcp-bridge",
							"version": "0.1.0",
						},
						"capabilities": map[string]interface{}{
							"tools":             map[string]interface{}{}, // Empty tools map
							"resources":         map[string]interface{}{},
							"resourceTemplates": map[string]interface{}{},
							"experimental":      map[string]any{},
							"prompts":           map[string]any{"listChanged": false},
						},
					},
				}

			case "tools/list":
				// log.Println("--- Handling tools/list ---") // Keep logging commented
				// Convert the map to a slice/array as required by the schema
				toolsSlice := make([]map[string]interface{}, 0, len(toolsDefinitionMap))
				for name, definition := range toolsDefinitionMap {
					toolDef := definition.(map[string]interface{}) // Assert type
					toolDef["name"] = name                         // Add the name field
					toolsSlice = append(toolsSlice, toolDef)
				}
				response = JSONRPCResponse{
					JSONRPC: "2.0",
					ID:      request.ID,
					Result:  map[string]interface{}{"tools": toolsSlice}, // Return the slice
				}

			case "tools/call":
				// log.Printf("--- Handling tools/call: %s ---", request.Params.Name) // Keep logging commented
				// --- Placeholder Logic for tool calls ---
				var toolResult interface{}
				var toolError *RPCError

				switch request.Params.Name {
				// Removed authenticate_pat case
				case "infer_image": // Keep infer_image simulated for now, but add PAT check
					var patToUse string
					if cliPat != nil && *cliPat != "" {
						patToUse = *cliPat
						log.Println("Using PAT from command-line flag for infer_image.")
					} else {
						toolError = &RPCError{Code: -32001, Message: "Authentication required (server not started with --pat flag)"}
						break
					}

					// Read necessary info from global state (client)
					globalState.mu.RLock()
					grpcClient := globalState.grpcClient
					globalState.mu.RUnlock()

					if grpcClient == nil {
						toolError = &RPCError{Code: -32001, Message: "gRPC client not initialized"}
						break
					}

					// TODO: Implement actual infer_image logic using patToUse for auth context
					log.Printf("Simulating successful call for %s using PAT ending in ...%s", request.Params.Name, patToUse[len(patToUse)-4:])
					toolResult = map[string]interface{}{"content": []map[string]any{{"type": "text", "text": "Simulated success for " + request.Params.Name}}}

				case "generate_image":
					// Directly use the command-line PAT. Error if not provided.
					var patToUse string
					if cliPat != nil && *cliPat != "" {
						patToUse = *cliPat
						log.Println("Using PAT from command-line flag for generate_image.")
					} else {
						toolError = &RPCError{Code: -32001, Message: "Authentication required (server not started with --pat flag)"}
						break
					}

					// Read necessary info from global state (client)
					globalState.mu.RLock()
					grpcClient := globalState.grpcClient
					// UserID/AppID from global state are no longer relevant for auth context
					globalState.mu.RUnlock()

					if grpcClient == nil {
						toolError = &RPCError{Code: -32001, Message: "gRPC client not initialized"}
						break
					}

					// Extract arguments
					textPrompt, promptOk := request.Params.Arguments["text_prompt"].(string) // Changed Args to Arguments
					if !promptOk || textPrompt == "" {
						toolError = &RPCError{Code: -32602, Message: "Invalid params: missing or invalid 'text_prompt'"}
						break
					}

					// Optional arguments with defaults
					modelID, _ := request.Params.Arguments["model_id"].(string) // Changed Args to Arguments
					userID, _ := request.Params.Arguments["user_id"].(string)   // Changed Args to Arguments
					appID, _ := request.Params.Arguments["app_id"].(string)     // Changed Args to Arguments

					// Default model and context if not provided
					if modelID == "" {
						modelID = "stable-diffusion-xl" // Updated default model_id
						log.Printf("No model_id provided, defaulting to %s", modelID)
						// Use provided defaults for user/app context when model is defaulted
						if userID == "" {
							userID = "stability-ai" // Updated default user_id
							log.Printf("Defaulting user_id to %s for %s", userID, modelID)
						}
						if appID == "" {
							appID = "stable-diffusion-2" // Updated default app_id to match model_id
							log.Printf("Defaulting app_id to %s for %s", appID, modelID)
						}
					} else {
						// If model is provided, use user/app from args if present.
						// Otherwise, let the backend infer from the PAT by leaving them empty.
					}

					log.Printf("Calling PostModelOutputs for generate_image: UserID=%s, AppID=%s, ModelID=%s", userID, appID, modelID)

					// Prepare gRPC request
					grpcRequest := &pb.PostModelOutputsRequest{
						UserAppId: &pb.UserAppIDSet{UserId: userID, AppId: appID},
						ModelId:   modelID,
						// VersionId: "", // Optional: Specify a version ID if needed
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

					// Create context with authentication
					baseCtx := context.Background()
					authCtx := createContextWithAuth(baseCtx, patToUse) // Use patToUse from flag

					// Create a context with a longer timeout for the gRPC call
					callCtx, cancel := context.WithTimeout(authCtx, 120*time.Second) // 120 seconds timeout
					defer cancel()                                                   // Ensure cancel is called

					// Make the gRPC call
					log.Println("DEBUG: Making gRPC call to PostModelOutputs with 120s timeout...") // Added log
					resp, err := grpcClient.PostModelOutputs(callCtx, grpcRequest)                  // Use callCtx with timeout
					log.Println("DEBUG: gRPC call to PostModelOutputs finished.")                   // Added log

					if err != nil {
						log.Printf("gRPC PostModelOutputs error: %v", err)
						log.Printf("gRPC PostModelOutputs raw response: %+v", resp) // Log the raw response

						toolError = mapGRPCErrorToJSONRPC(err)
					} else if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
						log.Printf("gRPC PostModelOutputs non-success status: %s - %s", resp.GetStatus().GetCode(), resp.GetStatus().GetDescription())
						toolError = &RPCError{
							Code:    -32000, // Generic internal error for API non-success
							Message: resp.GetStatus().GetDescription(),
							Data:    resp.GetStatus().GetDetails(),
						}
					} else if len(resp.Outputs) == 0 || resp.Outputs[0].Data == nil || resp.Outputs[0].Data.Image == nil {
						log.Printf("gRPC PostModelOutputs response missing expected data structure")
						toolError = &RPCError{Code: -32000, Message: "API response did not contain image data"}
					} else {
						// Extract base64 image data (as bytes)
						imageBase64Bytes := resp.Outputs[0].Data.Image.Base64
						log.Printf("Successfully generated image (received %d bytes of base64 data)", len(imageBase64Bytes))

						// Check if output path is set and image size exceeds threshold
						if cliOutputPath != nil && *cliOutputPath != "" && len(imageBase64Bytes) > imageSizeThreshold {
							log.Printf("Image size (%d bytes) exceeds threshold (%d bytes) and output path is set. Saving to disk.", len(imageBase64Bytes), imageSizeThreshold)

							// Convert bytes to string for cleaning and prefix check
							imageBase64String := string(imageBase64Bytes)

							// Trim whitespace first
							imageBase64String = strings.TrimSpace(imageBase64String)

							// Check for and remove data URI prefix if present
							if commaIndex := strings.Index(imageBase64String, ","); commaIndex != -1 && strings.HasPrefix(imageBase64String, "data:") {
								log.Println("DEBUG: Found and removing data URI prefix.")
								imageBase64String = imageBase64String[commaIndex+1:]
							}

							// Trim whitespace again after potential prefix removal
							imageBase64String = strings.TrimSpace(imageBase64String)

							// Log the beginning of the string before decoding for debugging
							logString := imageBase64String
							if len(logString) > 50 {
								logString = logString[:50] + "..." // Log only the first 50 chars
							}
							// Generate unique filename (assuming JPEG - TODO: determine format properly)
							timestamp := time.Now().UnixNano()
							randomNum := rand.Intn(10000)
							// Use a generic .png extension for now, as the data started with PNG header
							filename := fmt.Sprintf("generated_image_%d_%d.png", timestamp, randomNum)
							// Construct full path using os.PathSeparator for cross-platform compatibility
							// and ensure cliOutputPath ends with a separator if needed.
							fullPath := *cliOutputPath + string(os.PathSeparator) + filename
							log.Printf("DEBUG: Determined full path for saving: %s", fullPath)

							// Ensure directory exists (optional, WriteFile creates it)
							// os.MkdirAll(*cliOutputPath, 0755)

							// Write the raw image bytes directly to the file
							log.Println("DEBUG: Writing raw image bytes directly to file...")
							err = os.WriteFile(fullPath, imageBase64Bytes, 0644) // Use imageBase64Bytes directly
							if err != nil {
								log.Printf("Error writing image file to %s: %v", fullPath, err)
								toolError = &RPCError{Code: -32000, Message: "Failed to save generated image to disk"}
							} else {
								log.Printf("Successfully saved image to %s", fullPath)
								// Return only the file path as text content
								toolResult = map[string]interface{}{
									"content": []map[string]interface{}{
										{
											"type": "text",
											"text": "Image saved to: " + fullPath, // Return the path
										},
									},
								}
							}
						} else {
							// Return base64 bytes directly as image content (small image or no output path)
							log.Printf("Image size (%d bytes) within threshold or output path not set. Returning base64 image data.", len(imageBase64Bytes))

							// Convert bytes to string for cleaning
							imageBase64String := string(imageBase64Bytes)
							// Trim whitespace first
							imageBase64String = strings.TrimSpace(imageBase64String)
							// Check for and remove data URI prefix if present
							if commaIndex := strings.Index(imageBase64String, ","); commaIndex != -1 && strings.HasPrefix(imageBase64String, "data:") {
								log.Println("DEBUG: Found and removing data URI prefix before returning base64.")
								imageBase64String = imageBase64String[commaIndex+1:]
							}
							// Trim whitespace again after potential prefix removal
							imageBase64String = strings.TrimSpace(imageBase64String)

							toolResult = map[string]interface{}{
								"content": []map[string]interface{}{
									{
										"type":  "image",           // Set type to image
										"bytes": imageBase64String, // Return cleaned base64 string in 'bytes' field
									},
								},
							}
						}
					}

				default:
					toolError = &RPCError{Code: -32601, Message: "Tool not found: " + request.Params.Name}
				}

				response = JSONRPCResponse{
					JSONRPC: "2.0",
					ID:      request.ID,
					Result:  toolResult,
					Error:   toolError,
				}
				// --- End Placeholder Logic ---

			default:
				// log.Printf("Unknown method: %s", request.Method) // Keep logging commented
				response = JSONRPCResponse{
					JSONRPC: "2.0",
					ID:      request.ID,
					Error:   &RPCError{Code: -32601, Message: "Method not found", Data: request.Method},
				}
			}

			// Send response via channel
			server.WriteChannel() <- response
		}
		// log.Println("Main processing loop finished.") // Keep logging commented
		server.Close() // Close server when read channel closes
	}(patFlag, outputPathFlag) // Pass flags to the goroutine

	// Wait for server shutdown
	server.Wait()
	// log.Println("Server exited.") // Keep logging commented
}

// --- gRPC Setup ---
func setupGRPCConnection(apiAddress string) (*grpc.ClientConn, V2Client) {
	log.Printf("Determining connection type for gRPC server at %s", apiAddress)
	var creds grpc.DialOption

	// Use insecure credentials for local connections, secure for public API
	// if strings.HasPrefix(apiAddress, "localhost:") || strings.HasPrefix(apiAddress, "127.0.0.1:") {
	// 	creds = grpc.WithTransportCredentials(insecure.NewCredentials())
	// 	log.Println("Using insecure gRPC credentials for local connection.")
	// } else
	{
		// Assume public API requires TLS
		// TODO: Add proper certificate handling if needed, for now using default secure creds
		creds = grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, ""))
		log.Println("Using secure gRPC credentials (TLS) for non-local connection.")
	}

	log.Printf("Attempting to dial gRPC address: %s", apiAddress)
	conn, err := grpc.Dial(apiAddress, creds)
	if err != nil {
		log.Fatalf("Failed to connect to gRPC server: %v", err) // Keep fatal log for setup failure
	}
	log.Println("gRPC connection established")
	client := pb.NewV2Client(conn)
	return conn, client
}

// --- Other Helper Functions ---
func createContextWithAuth(ctx context.Context, pat string) context.Context {
	// Add Authorization metadata for Clarifai API calls
	return metadata.NewOutgoingContext(ctx, metadata.Pairs("Authorization", "Key "+pat))
}

// Maps gRPC errors to JSON-RPC error objects
func mapGRPCErrorToJSONRPC(err error) *RPCError {
	if err == nil {
		return nil
	}
	s, ok := status.FromError(err)
	if !ok {
		// Not a gRPC status error, return a generic internal error
		log.Printf("Non-gRPC error encountered: %v", err)
		return &RPCError{Code: -32000, Message: "Internal server error: " + err.Error()}
	}

	log.Printf("gRPC error details: Code=%s, Message=%s", s.Code(), s.Message())

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
	return &RPCError{Code: code, Message: s.Message()} // Use the gRPC message
}
