package handlers

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"backend/internal/repository"
)

// Evidence is downloaded as an attachment. Validate its format using the bytes,
// never the supplied name or Content-Type. Video validation checks the MP4
// container and a video track; the reviewer still assesses its contents.
func evidenceContentType(data []byte) (string, error) {
	kind := http.DetectContentType(data)
	switch kind {
	case "image/jpeg", "image/png":
		config, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 16_000_000 {
			return "", repository.ErrAdminInvalid
		}
		if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
			return "", repository.ErrAdminInvalid
		}
	case "application/pdf":
		if !bytes.HasPrefix(data, []byte("%PDF-")) || !bytes.HasSuffix(bytes.TrimSpace(data), []byte("%%EOF")) {
			return "", repository.ErrAdminInvalid
		}
	case "video/mp4":
		atoms, ok := mp4Atoms(data)
		if !ok || len(atoms["ftyp"]) < 8 || len(atoms["mdat"]) == 0 || !mp4VideoTrack(atoms["moov"]) {
			return "", repository.ErrAdminInvalid
		}
	default:
		return "", repository.ErrAdminInvalid
	}
	return kind, nil
}

func mp4Atoms(data []byte) (map[string][]byte, bool) {
	return mp4AtomsAtDepth(data, 0)
}

func mp4AtomsAtDepth(data []byte, depth int) (map[string][]byte, bool) {
	if depth > 4 {
		return nil, false
	}
	atoms := make(map[string][]byte)
	for len(data) > 0 {
		if len(data) < 8 {
			return nil, false
		}
		size, header := uint64(binary.BigEndian.Uint32(data[:4])), uint64(8)
		if size == 1 {
			if len(data) < 16 {
				return nil, false
			}
			size, header = binary.BigEndian.Uint64(data[8:16]), 16
		} else if size == 0 {
			size = uint64(len(data))
		}
		if size < header || size > uint64(len(data)) {
			return nil, false
		}
		name := string(data[4:8])
		// A movie can have an audio track as well as a video track.
		if name == "trak" {
			child, ok := mp4AtomsAtDepth(data[header:size], depth+1)
			if ok && mp4MediaIsVideo(child["mdia"], depth+1) {
				atoms["video"] = []byte{1}
			}
		} else {
			atoms[name] = data[header:size]
		}
		data = data[size:]
	}
	return atoms, true
}

func mp4MediaIsVideo(data []byte, depth int) bool {
	atoms, ok := mp4AtomsAtDepth(data, depth+1)
	handler := atoms["hdlr"]
	return ok && len(handler) >= 12 && string(handler[8:12]) == "vide" && len(atoms["minf"]) > 0
}

func mp4VideoTrack(data []byte) bool {
	atoms, ok := mp4Atoms(data)
	return ok && len(atoms["video"]) > 0
}

func (h *OrderDisputeHandler) evidencePath(key string, create bool) (string, error) {
	if len(key) != 68 || !strings.HasSuffix(key, ".dat") {
		return "", repository.ErrAdminInvalid
	}
	if _, err := hex.DecodeString(key[:64]); err != nil {
		return "", repository.ErrAdminInvalid
	}
	if strings.TrimSpace(h.privateRoot) == "" {
		return "", repository.ErrAdminInvalid
	}
	root, err := filepath.Abs(h.privateRoot)
	if err != nil {
		return "", err
	}
	if create {
		if err := os.MkdirAll(root, 0700); err != nil {
			return "", err
		}
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	// Reject an accidentally configured directory under the public static tree,
	// including symlink aliases. These are the same working-directory locations
	// supported by the template loader.
	for _, candidate := range []string{"../frontend", "../../frontend", "frontend"} {
		public, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		public, err = filepath.Abs(public)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(public, root)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", repository.ErrAdminInvalid
		}
	}
	return filepath.Join(root, key), nil
}
