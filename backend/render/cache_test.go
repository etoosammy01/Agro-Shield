package render

import (
	"bytes"
	"html/template"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"backend/internal/models"
)

func cacheFixtures() map[string]any {
	stamp := time.Date(2026, 1, 2, 12, 34, 0, 0, time.UTC)
	farmer := &models.Farmer{ID: 1, FullName: "A < B & C"}
	return map[string]any{
		"chat.html":         map[string]any{"Farmer": farmer, "Users": []models.Farmer{{ID: 2, FullName: "<script>alert(1)</script>", Location: "A & B"}}, "Conversations": []models.Conversation{{ID: 1, Type: "private", OtherName: "<b>Buyer</b>", UnreadCount: 1}}, "UnreadCount": 1, "Query": "\"<query>&", "Error": "<error>"},
		"chat_thread.html":  map[string]any{"Farmer": farmer, "Conversation": models.Conversation{ID: 1, Type: "private", OtherName: "<buyer>"}, "Members": []models.ConversationMember{{UserID: 2, FullName: "<buyer>"}}, "Messages": []models.ChatMessage{{ID: 1, SenderID: 2, Message: "<script>alert(1)</script> & \"hello\"", CreatedAt: stamp}}},
		"notification.html": map[string]any{"Farmer": farmer, "UnreadCount": 1, "Notifications": []models.Notification{{ID: 1, Title: "<script>alert(1)</script>", Message: "A & B < C", Type: "<order>", CreatedAt: stamp}}},
	}
}

func TestCachedTemplatesMatchRepeatedParsing(t *testing.T) {
	for name, fixture := range cacheFixtures() {
		for _, variant := range []string{"populated", "empty", "invalid", "missing"} {
			t.Run(name+"/"+variant, func(t *testing.T) {
				data := fixture
				if variant == "empty" {
					data = map[string]any{"Farmer": &models.Farmer{ID: 1}, "Conversation": models.Conversation{}, "UnreadCount": 0, "Users": []models.Farmer{}, "Conversations": []models.Conversation{}, "Messages": []models.ChatMessage{}, "Members": []models.ConversationMember{}, "Notifications": []models.Notification{}}
				}
				if variant == "invalid" {
					data = 42
				}
				if variant == "missing" {
					data = nil
				}
				baseline, err := template.ParseFiles(filepath.Join("../../frontend/pages", name))
				if err != nil {
					t.Fatal(err)
				}
				var before bytes.Buffer
				oldErr := baseline.Execute(&before, data)
				after := httptest.NewRecorder()
				newErr := RenderTemplates(after, name, data)
				if (oldErr == nil) != (newErr == nil) || before.String() != after.Body.String() {
					t.Fatal("cached output or error behavior changed")
				}
				if variant == "invalid" && newErr == nil {
					t.Fatal("invalid data unexpectedly rendered")
				}
				if strings.Contains(after.Body.String(), "<script>alert(1)</script>") {
					t.Fatal("user content was not escaped")
				}
				if variant == "populated" && !strings.Contains(after.Body.String(), "&lt;") {
					t.Fatal("escaping fixture not exercised")
				}
			})
		}
	}
}

func TestConcurrentCachedRendering(t *testing.T) {
	var wg sync.WaitGroup
	for name, fixture := range cacheFixtures() {
		name, fixture := name, fixture
		expected := httptest.NewRecorder()
		if err := RenderTemplates(expected, name, fixture); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 24; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				result := httptest.NewRecorder()
				if err := RenderTemplates(result, name, fixture); err != nil {
					t.Error(err)
					return
				}
				if result.Body.String() != expected.Body.String() {
					t.Error("concurrent output differs")
				}
			}()
		}
	}
	wg.Wait()
}

func TestMissingCachedTemplateReturnsError(t *testing.T) {
	if err := RenderTemplates(httptest.NewRecorder(), "does-not-exist.html", nil); err == nil {
		t.Fatal("missing template accepted")
	}
}

func BenchmarkTemplateRendering(b *testing.B) {
	for name, fixture := range cacheFixtures() {
		b.Run(name+"/RepeatedParse", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				tmpl, err := template.ParseFiles(filepath.Join("../../frontend/pages", name))
				if err != nil {
					b.Fatal(err)
				}
				if err := tmpl.Execute(io.Discard, fixture); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(name+"/Cached", func(b *testing.B) {
			// Include HTML contextual escaping once before timing, as in a warm server.
			if err := TMPL.ExecuteTemplate(io.Discard, name, fixture); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := TMPL.ExecuteTemplate(io.Discard, name, fixture); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
