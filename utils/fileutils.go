package utils

import (
	"fmt"
	"log/slog" // Use slog
	"math/rand"
	"os"
	"strings"
	"time"
)

// SaveImage saves image data to a file in the specified directory.
// It generates a unique filename and returns the full path or an error.
// It also handles cleaning potential data URI prefixes from base64 data.
func SaveImage(outputPath string, imageBase64Bytes []byte) (string, error) {
	slog.Debug("Attempting to save image", "size_bytes", len(imageBase64Bytes), "output_path", outputPath) // Use slog

	// Convert bytes to string for cleaning and prefix check
	imageBase64String := string(imageBase64Bytes)

	// Trim whitespace first
	imageBase64String = strings.TrimSpace(imageBase64String)

	// Check for and remove data URI prefix if present
	if commaIndex := strings.Index(imageBase64String, ","); commaIndex != -1 && strings.HasPrefix(imageBase64String, "data:") {
		slog.Debug("Found and removing data URI prefix before saving.") // Use slog
		imageBase64String = imageBase64String[commaIndex+1:]
		// Re-convert cleaned string back to bytes for saving
		// Note: This assumes the original bytes included the prefix. If not, this step might be unnecessary.
		// Consider if the input `imageBase64Bytes` is guaranteed to be *just* the base64 data.
		// For now, we'll work with the potentially cleaned string converted back to bytes.
		// TODO: Re-evaluate if decoding/re-encoding is needed or if we can save original bytes after check.
		// imageBase64Bytes = []byte(imageBase64String) // Re-assigning bytes after cleaning string
	}

	// Trim whitespace again after potential prefix removal
	// imageBase64String = strings.TrimSpace(imageBase64String) // String already trimmed

	// Generate unique filename
	// TODO: Determine image format properly instead of assuming png.
	timestamp := time.Now().UnixNano()
	// Ensure rand is seeded (should be done once at application start)
	// rand.Seed(time.Now().UnixNano()) // Seeding here is inefficient if called often
	randomNum := rand.Intn(10000)
	filename := fmt.Sprintf("generated_image_%d_%d.png", timestamp, randomNum)

	// Construct full path using os.PathSeparator for cross-platform compatibility
	// Ensure outputPath exists (WriteFile doesn't create intermediate dirs)
	err := os.MkdirAll(outputPath, 0755) // Ensure the directory exists
	if err != nil {
		slog.Error("Error creating output directory", "path", outputPath, "error", err) // Use slog
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}
	fullPath := outputPath + string(os.PathSeparator) + filename
	slog.Debug("Determined full path for saving", "path", fullPath) // Use slog

	// Write the raw image bytes directly to the file
	// Use the potentially modified bytes after cleaning the string representation
	slog.Debug("Writing raw image bytes directly to file...") // Use slog
	// err = os.WriteFile(fullPath, imageBase64Bytes, 0644) // Use original bytes
	// Let's try writing the cleaned string converted back to bytes, assuming prefix removal was needed.
	// This might be incorrect if the original bytes were pure base64.
	err = os.WriteFile(fullPath, []byte(imageBase64String), 0644) // Write potentially cleaned bytes
	if err != nil {
		slog.Error("Error writing image file", "path", fullPath, "error", err) // Use slog
		return "", fmt.Errorf("failed to save generated image to disk: %w", err)
	}

	slog.Info("Successfully saved image", "path", fullPath) // Use slog
	return fullPath, nil
}

// CleanBase64Data removes potential data URI prefix and trims whitespace.
// Returns the cleaned base64 string.
func CleanBase64Data(data []byte) string {
	dataString := string(data)
	dataString = strings.TrimSpace(dataString)
	if commaIndex := strings.Index(dataString, ","); commaIndex != -1 && strings.HasPrefix(dataString, "data:") {
		slog.Debug("Found and removing data URI prefix.") // Use slog
		dataString = dataString[commaIndex+1:]
	}
	// Trim again after potential removal
	return strings.TrimSpace(dataString)
}
