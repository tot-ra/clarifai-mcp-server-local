package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"sync"
	// "log" // Keep logging commented for now
)

// Server defines the interface for an MCP server transport.
type Server interface {
	Start(ctx context.Context)
	ReadChannel() <-chan JSONRPCRequest
	WriteChannel() chan<- JSONRPCResponse
	Wait()
	Close() error
}

// StdioServer implements the Server interface using standard input/output.
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

// NewStdioServer creates a new StdioServer instance.
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

// Start begins the reader and writer goroutines.
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
				// log.Println("StdioServer reader shutting down.")
				return
			default:
				line := scanner.Bytes()
				// log.Printf("MCP RECV <<< %s", string(line))
				var request JSONRPCRequest
				err := json.Unmarshal(line, &request)
				if err != nil {
					// log.Printf("Error unmarshalling request: %v", err)
					continue // Skip malformed requests
				}
				// Use a select to prevent blocking if readChan buffer is full or receiver is slow
				select {
				case s.readChan <- request:
				case <-s.shutdownCtx.Done():
					// log.Println("StdioServer reader shutting down while sending.")
					return
				}
			}
		}
		if err := scanner.Err(); err != nil && err != io.EOF {
			// log.Printf("Error reading from stdin: %v", err)
		}
		// log.Println("StdioServer reader finished.")
		s.cancelFunc() // Signal shutdown if reader finishes
	}()

	// Start writer goroutine
	go func() {
		defer s.wg.Done()
		writer := bufio.NewWriter(s.writer)
		for {
			select {
			case <-s.shutdownCtx.Done(): // Check for shutdown signal
				// log.Println("StdioServer writer shutting down.")
				// Flush any remaining buffered data before exiting
				_ = writer.Flush() // Ignore error on shutdown flush
				return
			case response, ok := <-s.writeChan:
				if !ok {
					// log.Println("StdioServer write channel closed.")
					_ = writer.Flush() // Flush any remaining buffered data
					return             // Exit if write channel is closed
				}
				respBytes, err := json.Marshal(response)
				if err != nil {
					// log.Printf("Error marshalling response: %v", err)
					continue // Skip responses that cannot be marshalled
				}
				// log.Printf("MCP SEND >>> %s", string(respBytes))
				_, err = writer.Write(respBytes)
				if err != nil {
					// log.Printf("Error writing response bytes: %v", err)
					s.cancelFunc() // Signal shutdown on write error
					return
				}
				_, err = writer.WriteString("\n") // Add newline delimiter
				if err != nil {
					// log.Printf("Error writing newline: %v", err)
					s.cancelFunc() // Signal shutdown on write error
					return
				}
				err = writer.Flush() // Flush buffer after each message
				if err != nil {
					// log.Printf("Error flushing writer: %v", err)
					s.cancelFunc() // Signal shutdown on write error
					return
				}
			}
		}
	}()
}

// ReadChannel returns the channel for receiving incoming requests.
func (s *StdioServer) ReadChannel() <-chan JSONRPCRequest {
	return s.readChan
}

// WriteChannel returns the channel for sending outgoing responses.
func (s *StdioServer) WriteChannel() chan<- JSONRPCResponse {
	return s.writeChan
}

// Wait blocks until the server has shut down completely.
func (s *StdioServer) Wait() {
	<-s.shutdownCtx.Done() // Wait until context is cancelled
	s.wg.Wait()            // Wait for reader and writer goroutines to finish
	// log.Println("StdioServer Wait completed.")
}

// Close initiates a graceful shutdown of the server.
func (s *StdioServer) Close() error {
	s.cancelFunc()     // Trigger shutdown
	s.Wait()           // Wait for clean shutdown
	close(s.writeChan) // Close writeChan only after writer goroutine has exited
	// log.Println("StdioServer Closed.")
	return nil // Assuming no specific close error for stdio
}
