package inference

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTranscribeSendsMultipartToConfiguredModel(t *testing.T) {
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" || r.Header.Get("Authorization") != "Bearer private-key" {
			t.Error("wrong transcription request")
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error("not multipart:", err)
		}
		if r.FormValue("model") != "stt-test" {
			t.Errorf("wrong model field %q", r.FormValue("model"))
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatal("missing audio file part")
		}
		data, _ := io.ReadAll(file)
		if string(data) != "RIFFfake" {
			t.Error("audio bytes not forwarded")
		}
		w.WriteHeader(status)
		if status == http.StatusOK {
			fmt.Fprint(w, `{"text":" hello team "}`)
		} else {
			fmt.Fprint(w, "private-key secret audio")
		}
	}))
	defer server.Close()

	settings := Settings{Enabled: true, BaseURL: server.URL + "/v1", APIKey: "private-key", TranscriptionModel: "stt-test"}
	text, err := Transcribe(context.Background(), settings, []byte("RIFFfake"))
	if err != nil || text != "hello team" {
		t.Fatalf("unexpected result %q, %v", text, err)
	}

	status = http.StatusUnauthorized
	_, err = Transcribe(context.Background(), settings, []byte("RIFFfake"))
	if err == nil || strings.Contains(err.Error(), "private-key") || strings.Contains(err.Error(), "secret audio") {
		t.Fatalf("unsafe provider error: %v", err)
	}
}

func TestTranscribeRequiresModelAndEnabledProvider(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer server.Close()

	if _, err := Transcribe(context.Background(), Settings{Enabled: true, BaseURL: server.URL}, []byte("x")); err == nil {
		t.Fatal("transcribed without a configured model")
	}
	if _, err := Transcribe(context.Background(), Settings{BaseURL: server.URL, TranscriptionModel: "m"}, []byte("x")); err == nil {
		t.Fatal("transcribed while AI is disabled")
	}
	if calls != 0 {
		t.Fatal("provider contacted without configuration")
	}
}

func TestTranscriptionReadyNeedsModelAndSafeEndpoint(t *testing.T) {
	ready := Settings{Enabled: true, BaseURL: "https://api.example.com/v1", TranscriptionModel: "stt"}
	if !ready.TranscriptionReady() {
		t.Fatal("valid transcription settings reported as not ready")
	}
	noModel := ready
	noModel.TranscriptionModel = " "
	if noModel.TranscriptionReady() {
		t.Fatal("blank transcription model accepted")
	}
	insecure := ready
	insecure.BaseURL = "http://example.com/v1"
	if insecure.TranscriptionReady() {
		t.Fatal("plain HTTP remote endpoint accepted")
	}
	// A chat model is not needed for speech to text.
	if ready.ChatModel != "" || ready.Validate() == nil {
		t.Fatal("chat validation should still require a chat model")
	}
}
