package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backend/internal/models"
	"backend/middleware"
)

type testSelfieSaver struct {
	id    int
	photo string
	err   error
}

func (s *testSelfieSaver) UpdateSelfie(id int, photo string) error {
	s.id, s.photo = id, photo
	return s.err
}

func TestSelfiePageReplacesUploadAndBankForm(t *testing.T) {
	w, _ := onboardingRequest(t, &models.Farmer{ID: 20}, NewCompleteProfileHandler(&testSelfieSaver{}).Handler, "GET", "/complete-profile", nil)
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, `id="selfie-video"`) || !strings.Contains(body, `id="capture-button"`) {
		t.Fatal("camera page missing")
	}
	if strings.Contains(body, `type="file"`) || strings.Contains(body, "bank-name") || strings.Contains(body, "account-number") {
		t.Fatal("old onboarding fields remain")
	}
}

func TestSelfieUploadMustSaveBeforeAdvancing(t *testing.T) {
	for _, tc := range []struct {
		name    string
		photo   bool
		saveErr error
		want    int
	}{
		{"missing selfie", false, nil, http.StatusBadRequest},
		{"save failed", true, errors.New("database unavailable"), http.StatusBadRequest},
		{"saved selfie", true, nil, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "backend"), 0755); err != nil {
				t.Fatal(err)
			}
			t.Chdir(filepath.Join(root, "backend"))
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			if tc.photo {
				h := textproto.MIMEHeader{}
				h.Set("Content-Disposition", `form-data; name="profile_picture"; filename="selfie.jpg"`)
				h.Set("Content-Type", "image/jpeg")
				part, err := writer.CreatePart(h)
				if err != nil {
					t.Fatal(err)
				}
				if err := jpeg.Encode(part, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "/complete-profile", &body)
			r.Header.Set("Content-Type", writer.FormDataContentType())
			r.Header.Set("Accept", "application/json")
			session := middleware.CreateSession(21)
			t.Cleanup(func() { middleware.DeleteSession(session) })
			cookie := httptest.NewRecorder()
			middleware.SetSessionCookie(cookie, session)
			r.AddCookie(cookie.Result().Cookies()[0])
			saver := &testSelfieSaver{err: tc.saveErr}
			w := httptest.NewRecorder()
			middleware.RequireAuth(onboardingAccount{&models.Farmer{ID: 21}}, NewCompleteProfileHandler(saver).Handler)(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			var result map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if tc.want == http.StatusOK {
				if result["next"] != "/verify-phone" || saver.id != 21 || !strings.HasPrefix(saver.photo, "/static/uploads/farmers/") {
					t.Fatal("selfie not saved before next step")
				}
			} else if result["next"] != "" || result["error"] == "" {
				t.Fatal("failed selfie advanced onboarding")
			}
		})
	}
}

func TestSelfiePageResumesAtPhoneVerification(t *testing.T) {
	f := &models.Farmer{ID: 22, SelfieCapturedAt: sql.NullTime{Time: time.Now(), Valid: true}}
	w, _ := onboardingRequest(t, f, NewCompleteProfileHandler(&testSelfieSaver{}).Handler, "GET", "/complete-profile", nil)
	if w.Header().Get("Location") != "/verify-phone" {
		t.Fatal("completed selfie step repeated")
	}
}
