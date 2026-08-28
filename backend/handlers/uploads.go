package handlers

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// saveUploadedFiles saves one or more uploaded files.
//
// Responsibility:
// - Read multiple files from the same form field.
// - Save every valid uploaded file.
// - Return the URLs/paths of the saved files.
// - Require the caller to decide whether at least one file is required.
func saveUploadedFiles(
	r *http.Request,
	field string,
	subdir string,
) ([]string, error) {

	if r.MultipartForm == nil {
		return nil, fmt.Errorf("multipart form is not available")
	}

	files := r.MultipartForm.File[field]

	if len(files) == 0 {
		return nil, nil
	}

	var uploadedURLs []string

	for _, header := range files {
		if header == nil {
			continue
		}

		file, err := header.Open()
		if err != nil {
			return nil, fmt.Errorf("could not open uploaded file: %w", err)
		}

		url, err := saveOneUploadedFile(
			file,
			header,
			subdir,
		)

		file.Close()

		if err != nil {
			return nil, err
		}

		if strings.TrimSpace(url) != "" {
			uploadedURLs = append(uploadedURLs, url)
		}
	}

	return uploadedURLs, nil
}

// saveOneUploadedFile saves one uploaded file.
//
// This is the internal helper used by saveUploadedFiles.
func saveOneUploadedFile(
	file multipart.File,
	header *multipart.FileHeader,
	subdir string,
) (string, error) {

	if file == nil {
		return "", fmt.Errorf("uploaded file is nil")
	}

	if header == nil {
		return "", fmt.Errorf("uploaded file information is missing")
	}

	// --------------------------------------------------------
	// Validate file size
	// --------------------------------------------------------

	const maxFileSize = 10 << 20 // 10 MB

	if header.Size > maxFileSize {
		return "", fmt.Errorf(
			"file %q is too large; maximum size is 10 MB",
			header.Filename,
		)
	}

	// --------------------------------------------------------
	// Validate file type
	// --------------------------------------------------------

	contentType := strings.ToLower(
		strings.TrimSpace(
			header.Header.Get("Content-Type"),
		),
	)

	allowedTypes := map[string]string{
		"image/jpeg": ".jpg",
		"image/jpg":  ".jpg",
		"image/png":  ".png",
		"image/webp": ".webp",
	}

	extension, ok := allowedTypes[contentType]

	if !ok {
		return "", fmt.Errorf(
			"unsupported image type %q; allowed types are JPG, PNG and WEBP",
			contentType,
		)
	}

	// --------------------------------------------------------
	// Create upload directory
	// --------------------------------------------------------

	uploadDir := filepath.Join(
		"frontend",
		"static",
		"uploads",
		subdir,
	)

	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		return "", fmt.Errorf(
			"could not create upload directory: %w",
			err,
		)
	}

	// --------------------------------------------------------
	// Generate unique filename
	// --------------------------------------------------------

	filename := fmt.Sprintf(
		"%d_%d%s",
		time.Now().UnixNano(),
		os.Getpid(),
		extension,
	)

	destination := filepath.Join(
		uploadDir,
		filename,
	)

	// --------------------------------------------------------
	// Create destination file
	// --------------------------------------------------------

	output, err := os.Create(destination)

	if err != nil {
		return "", fmt.Errorf(
			"could not create destination file: %w",
			err,
		)
	}

	defer output.Close()

	// --------------------------------------------------------
	// Copy uploaded file
	// --------------------------------------------------------

	if _, err := io.Copy(output, file); err != nil {
		return "", fmt.Errorf(
			"could not save uploaded file: %w",
			err,
		)
	}

	// --------------------------------------------------------
	// Return browser-accessible path
	// --------------------------------------------------------

	return "/static/uploads/" + subdir + "/" + filename, nil
}

// saveUploadedFile keeps compatibility with existing code
// that still uploads only one file.
//
// It uses saveUploadedFiles internally and returns the first image.
func saveUploadedFile(
	r *http.Request,
	field string,
	subdir string,
) (string, error) {

	files, err := saveUploadedFiles(
		r,
		field,
		subdir,
	)

	if err != nil {
		return "", err
	}

	if len(files) == 0 {
		return "", nil
	}

	return files[0], nil
}