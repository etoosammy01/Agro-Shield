package handlers

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Upload failures that callers should explain to the user in their own words
// rather than treating like an internal error.
var (
	ErrUploadTooLarge        = errors.New("uploaded file is too large")
	ErrUploadUnsupportedType = errors.New("uploaded file is not a supported image")
)

// uploadErrorMessage turns a save error into a short message that is safe to
// show on the page. It never includes a filesystem path or other internals.
func uploadErrorMessage(err error) string {
	switch {
	case errors.Is(err, ErrUploadTooLarge):
		return "That image is too large. Choose a file no bigger than 10 MB."
	case errors.Is(err, ErrUploadUnsupportedType):
		return "That file isn't a supported image. Upload a JPG, PNG or WEBP photo."
	default:
		return "We couldn't save that image. Please try again."
	}
}

// acceptedImageSubtype sniffs the first bytes of an upload and returns the
// canonical MIME type when they are a supported image. The browser-supplied
// Content-Type is not trusted on its own.
func acceptedImageSubtype(head []byte) (string, bool) {
	switch strings.ToLower(http.DetectContentType(head)) {
	case "image/jpeg":
		return "image/jpeg", true
	case "image/png":
		return "image/png", true
	case "image/webp":
		return "image/webp", true
	default:
		return "", false
	}
}

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
			"%w: file %q is too large; maximum size is 10 MB",
			ErrUploadTooLarge,
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

	if _, ok := allowedTypes[contentType]; !ok {
		return "", fmt.Errorf(
			"%w: unsupported image type %q; allowed types are JPG, PNG and WEBP",
			ErrUploadUnsupportedType,
			contentType,
		)
	}

	// The declared Content-Type is supplied by the browser and can be wrong or
	// forged, so sniff the actual bytes too. This stops a renamed text or HTML
	// file from being stored and served as a photo.
	head := make([]byte, 512)
	read, readErr := io.ReadFull(file, head)
	if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
		return "", fmt.Errorf("could not read uploaded file: %w", readErr)
	}
	head = head[:read]

	sniffed, ok := acceptedImageSubtype(head)
	if !ok {
		return "", fmt.Errorf(
			"%w: file content is %q; allowed types are JPG, PNG and WEBP",
			ErrUploadUnsupportedType,
			http.DetectContentType(head),
		)
	}
	contentType = sniffed
	extension := allowedTypes[contentType]

	// --------------------------------------------------------
	// Create upload directory
	// --------------------------------------------------------

	// Routes exposes ../frontend at /static/.  Store uploads beneath that
	// same directory so a returned /static/uploads/... URL resolves to the
	// file that was just saved.
	uploadDir := filepath.Join(
		"..",
		"frontend",
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

	// Write the bytes consumed for sniffing, then the rest of the file.
	if _, err := io.Copy(output, io.MultiReader(bytes.NewReader(head), file)); err != nil {
		return "", fmt.Errorf("could not save uploaded file: %w", err)
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
