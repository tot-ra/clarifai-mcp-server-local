package utils

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"clarifai-mcp-server-local/mcp" // Assuming mcp types are needed
)

// Function to write detailed errors to a log file
func LogErrorToFile(err error, context map[string]string, rpcErr *mcp.RPCError) {
	// Use an absolute path to avoid CWD issues
	// TODO: Consider making this path configurable or relative to an execution root
	logFilePath := "/Users/artjom/work/clarifai-mcp-server-local/error.log"
	file, openErr := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if openErr != nil {
		slog.Error("Failed to open error log file", "path", logFilePath, "error", openErr)
		return // Cannot log to file, rely on standard slog
	}
	defer file.Close()

	timestamp := time.Now().Format(time.RFC3339)
	contextJSON, _ := json.Marshal(context) // Ignoring marshalling error for simplicity in logging
	rpcErrJSON, _ := json.Marshal(rpcErr)   // Ignoring marshalling error for simplicity in logging

	logEntry := fmt.Sprintf("[%s] OriginalError: %v | Context: %s | MappedRPCError: %s\n",
		timestamp,
		err,
		string(contextJSON),
		string(rpcErrJSON),
	)

	if _, writeErr := file.WriteString(logEntry); writeErr != nil {
		slog.Error("Failed to write to error log file", "path", logFilePath, "error", writeErr)
	}
}
