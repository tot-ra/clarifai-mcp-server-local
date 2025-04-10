package utils

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"time"

	"clarifai-mcp-server-local/clarifai"
	"clarifai-mcp-server-local/mcp"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// PrepareGrpcCall sets up the context with authentication and timeout for a gRPC call.
// It requires the Clarifai client, PAT, and timeout duration.
func PrepareGrpcCall(baseCtx context.Context, client *clarifai.Client, pat string, timeoutSec int) (context.Context, context.CancelFunc, *mcp.RPCError) {
	if client == nil || client.API == nil {
		return nil, nil, &mcp.RPCError{Code: -32001, Message: "Clarifai client not initialized"}
	}
	if pat == "" {
		return nil, nil, &mcp.RPCError{Code: -32001, Message: "Authentication failed: PAT not configured"}
	}

	authCtx := clarifai.CreateContextWithAuth(baseCtx, pat)
	callTimeout := time.Duration(timeoutSec) * time.Second
	callCtx, cancel := context.WithTimeout(authCtx, callTimeout)
	return callCtx, cancel, nil
}

// HandleApiError logs the error with context, writes details to a file, and maps it to an RPCError.
// It requires a logger instance.
func HandleApiError(err error, context map[string]string, logger *slog.Logger) *mcp.RPCError {
	logArgs := []interface{}{"error", err}
	for k, v := range context {
		if v != "" { // Only log non-empty context values
			logArgs = append(logArgs, slog.String(k, v))
		}
	}
	logger.Error("API error", logArgs...) // Standard log

	var rpcErr *mcp.RPCError // Declare rpcErr variable

	st, ok := status.FromError(err)
	if ok {
		// Map specific gRPC codes if needed
		// Example: Map NotFound specifically
		if st.Code() == codes.NotFound {
			rpcErr = &mcp.RPCError{Code: -32002, Message: "Resource not found", Data: context}
		}
		// Add more specific mappings here if needed (e.g., PermissionDenied, Unauthenticated)
	}

	// If not specifically mapped yet, use generic mapping
	if rpcErr == nil {
		grpcErr := clarifai.MapGRPCErrorToJSONRPC(err)
		if grpcErr != nil {
			grpcErr.Data = context // Add context to the generic error data
			rpcErr = grpcErr
		} else {
			// Fallback for non-gRPC errors
			rpcErr = &mcp.RPCError{Code: -32000, Message: fmt.Sprintf("Internal server error: %v", err), Data: context}
		}
	}

	// Log detailed error to file before returning
	LogErrorToFile(err, context, rpcErr) // Assumes LogErrorToFile is in the same utils package or imported

	return rpcErr
}

// ParsePagination extracts page and perPage values from query parameters or cursor.
// It requires a logger instance.
func ParsePagination(queryParams url.Values, cursor string, logger *slog.Logger) (page, perPage uint32) {
	// Defaults
	page = 1
	perPage = 20 // Default perPage, adjust as needed

	const maxPerPage = 1000 // Define a maximum perPage limit

	if cursor != "" {
		pageInt, err := strconv.Atoi(cursor)
		if err == nil && pageInt > 0 {
			page = uint32(pageInt)
		} else {
			logger.Warn("Invalid pagination cursor provided, using default page 1", "cursor", cursor, "error", err)
			page = 1 // Reset to default if cursor is invalid
		}
		// Note: perPage is not typically carried in a simple page number cursor
		// If cursor encodes more info, adjust parsing logic.
		// For now, use default perPage when cursor is present.
		if perPageStr := queryParams.Get("per_page"); perPageStr != "" {
			perPageInt, err := strconv.Atoi(perPageStr)
			if err == nil && perPageInt > 0 {
				if perPageInt > maxPerPage {
					logger.Warn("Requested per_page exceeds maximum, capping", "requested", perPageInt, "max", maxPerPage)
					perPage = maxPerPage
				} else {
					perPage = uint32(perPageInt)
				}
			} else {
				logger.Warn("Invalid per_page query parameter with cursor, using default", "per_page", perPageStr, "error", err)
				// Keep default perPage
			}
		}

	} else {
		// No cursor, parse from query parameters
		if pageStr := queryParams.Get("page"); pageStr != "" {
			pageInt, err := strconv.Atoi(pageStr)
			if err == nil && pageInt > 0 {
				page = uint32(pageInt)
			} else {
				logger.Warn("Invalid page query parameter, using default", "page", pageStr, "error", err)
				// Keep default page
			}
		}
		if perPageStr := queryParams.Get("per_page"); perPageStr != "" {
			perPageInt, err := strconv.Atoi(perPageStr)
			if err == nil && perPageInt > 0 {
				if perPageInt > maxPerPage {
					logger.Warn("Requested per_page exceeds maximum, capping", "requested", perPageInt, "max", maxPerPage)
					perPage = maxPerPage
				} else {
					perPage = uint32(perPageInt)
				}
			} else {
				logger.Warn("Invalid per_page query parameter, using default", "per_page", perPageStr, "error", err)
				// Keep default perPage
			}
		}
	}

	logger.Debug("Parsed pagination", "page", page, "perPage", perPage)
	return page, perPage
}
