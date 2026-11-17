package handlers

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func evidenceAtom(name string, content []byte) []byte {
	atom := make([]byte, 8+len(content))
	binary.BigEndian.PutUint32(atom, uint32(len(atom)))
	copy(atom[4:8], name)
	copy(atom[8:], content)
	return atom
}

func testVideoEvidence(handler string) []byte {
	hdlr := make([]byte, 24)
	copy(hdlr[8:12], handler)
	mdia := append(evidenceAtom("hdlr", hdlr), evidenceAtom("minf", evidenceAtom("vmhd", make([]byte, 12)))...)
	data := evidenceAtom("ftyp", []byte("mp42\x00\x00\x00\x00mp42"))
	data = append(data, evidenceAtom("moov", evidenceAtom("trak", evidenceAtom("mdia", mdia)))...)
	return append(data, evidenceAtom("mdat", []byte{1, 2, 3, 4})...)
}

func TestDisputeEvidenceRejectsSpoofedAndTruncatedFormats(t *testing.T) {
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{pngData.Bytes(), testVideoEvidence("vide"), []byte("%PDF-1.7\n1 0 obj\n<< /Type /Catalog >>\nendobj\n%%EOF\n")} {
		if _, err := evidenceContentType(data); err != nil {
			t.Fatal("supported content", err)
		}
	}
	bad := [][]byte{[]byte("<html><script>alert(1)</script></html>"), pngData.Bytes()[:30], testVideoEvidence("soun"), testVideoEvidence("vide")[:25], []byte("%PDF-1.7\ntruncated")}
	// Oversized atom and maliciously nested containers must be rejected without
	// allocations or recursion proportional to the claimed size.
	atom := evidenceAtom("ftyp", []byte("mp42\x00\x00\x00\x00"))
	binary.BigEndian.PutUint32(atom, 0xffffffff)
	bad = append(bad, atom)
	for _, data := range bad {
		if _, err := evidenceContentType(data); err == nil {
			t.Fatal("invalid evidence accepted")
		}
	}
}

func TestDisputeEvidenceStorageRejectsPublicPathsAndTraversal(t *testing.T) {
	public, err := filepath.Abs("../../frontend")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(public); err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("a", 64) + ".dat"
	h := &OrderDisputeHandler{privateRoot: public}
	if _, err := h.evidencePath(key, false); err == nil {
		t.Fatal("public evidence directory accepted")
	}
	alias := filepath.Join(t.TempDir(), "public-link")
	if err := os.Symlink(public, alias); err != nil {
		t.Fatal(err)
	}
	h.privateRoot = alias
	if _, err := h.evidencePath(key, false); err == nil {
		t.Fatal("public symlink accepted")
	}
	h.privateRoot = t.TempDir()
	for _, bad := range []string{"../secret.dat", strings.Repeat("/", 64) + ".dat", strings.Repeat("g", 64) + ".dat"} {
		if _, err := h.evidencePath(bad, false); err == nil {
			t.Fatal("invalid evidence key accepted")
		}
	}
	if path, err := h.evidencePath(key, false); err != nil || filepath.Dir(path) != h.privateRoot {
		t.Fatal(path, err)
	}
}
