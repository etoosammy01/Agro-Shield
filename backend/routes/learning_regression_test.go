package routes

import (
	"os"
	"strings"
	"testing"
)

// The learning page sends questions to the dedicated Learning AI endpoint. A
// previous edit removed the request while leaving the response handling in
// place, which made every question fail at runtime with "response is not
// defined". Keep the request and its payload in the script so that regression
// cannot return unnoticed.
func TestLearningScriptSendsQuestionsToLearningChat(t *testing.T) {
	content, err := os.ReadFile("../../frontend/js/learning.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(content)

	request := `const response = await fetch("/learning/chat", {`
	requestAt := strings.Index(script, request)
	if requestAt < 0 {
		t.Fatal("learning.js no longer sends a request to /learning/chat")
	}
	if !strings.Contains(script[requestAt:], `method: "POST"`) {
		t.Fatal("learning.js request to /learning/chat is not a POST")
	}
	if !strings.Contains(script[requestAt:], `farmingType: category`) {
		t.Fatal("learning.js request does not send the selected farming category")
	}

	responseAt := strings.Index(script, "if (!response.ok)")
	if responseAt < 0 {
		t.Fatal("learning.js no longer checks the /learning/chat response")
	}
	if requestAt > responseAt {
		t.Fatal("learning.js uses response before it is fetched")
	}

	if strings.Contains(script, "showError(") {
		t.Fatal("learning.js calls showError, which is not defined")
	}
}
