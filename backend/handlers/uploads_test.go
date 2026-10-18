package handlers

import (
	"errors"
	"testing"
)

// The browser's Content-Type header is not evidence that the bytes are an
// image. A renamed text or HTML file must not be accepted for a photo field.
func TestAcceptedImageSubtypeUsesFileContent(t *testing.T) {
	png := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, make([]byte, 32)...)
	jpeg := append([]byte{0xff, 0xd8, 0xff, 0xe0}, make([]byte, 32)...)
	webp := append([]byte("RIFF"), 0x24, 0, 0, 0)
	webp = append(webp, []byte("WEBPVP8 ")...)

	cases := []struct {
		name string
		head []byte
		want string
		ok   bool
	}{
		{"png", png, "image/png", true},
		{"jpeg", jpeg, "image/jpeg", true},
		{"webp", webp, "image/webp", true},
		{"renamed text", []byte("not really an image"), "", false},
		{"renamed html", []byte("<!doctype html><script>alert(1)</script>"), "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := acceptedImageSubtype(tc.head)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("acceptedImageSubtype(%q) = %q, %v; want %q, %v", tc.head, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestUploadErrorMessageHidesInternals(t *testing.T) {
	if got := uploadErrorMessage(ErrUploadTooLarge); got != "That image is too large. Choose a file no bigger than 10 MB." {
		t.Fatalf("too-large message = %q", got)
	}
	if got := uploadErrorMessage(ErrUploadUnsupportedType); got != "That file isn't a supported image. Upload a JPG, PNG or WEBP photo." {
		t.Fatalf("type message = %q", got)
	}
	internal := errors.New("open ../frontend/uploads/farmers/123.jpg: permission denied")
	if got := uploadErrorMessage(internal); got != "We couldn't save that image. Please try again." {
		t.Fatalf("internal message leaked detail: %q", got)
	}
}
