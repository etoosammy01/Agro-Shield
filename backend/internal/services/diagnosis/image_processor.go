package diagnosis

import (
	"bytes"
	"image/jpeg"

	"github.com/disintegration/imaging"
)

// ============================================================
// RESIZE AND COMPRESS IMAGE
//
// Farmers may upload very large phone pictures.
// This function makes the image smaller before we send it
// to Gemini.
//
// Smaller image = less data to process and send.
// ============================================================

func prepareImage(imageData []byte) ([]byte, error) {

	// ----------------------------------------------------------
	// 1. Read the image from the farmer's uploaded file.
	// ----------------------------------------------------------

	img, err := imaging.Decode(bytes.NewReader(imageData))
	if err != nil {
		return nil, err
	}

	// ----------------------------------------------------------
	// 2. Resize the image.
	//
	// The longest side will be at most 1600 pixels.
	//
	// We keep the original shape of the picture.
	// ----------------------------------------------------------

	img = imaging.Fit(
		img,
		1600,
		1600,
		imaging.Lanczos,
	)

	// ----------------------------------------------------------
	// 3. Create a place to store the smaller image.
	// ----------------------------------------------------------

	var output bytes.Buffer

	// ----------------------------------------------------------
	// 4. Compress the image as JPEG.
	//
	// Quality 80 gives us a good balance between:
	// - image quality
	// - smaller file size
	// ----------------------------------------------------------

	err = jpeg.Encode(
		&output,
		img,
		&jpeg.Options{
			Quality: 80,
		},
	)
	if err != nil {
		return nil, err
	}

	// ----------------------------------------------------------
	// 5. Return the smaller image.
	// ----------------------------------------------------------

	return output.Bytes(), nil
}