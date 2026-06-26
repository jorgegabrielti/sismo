package notifier

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sismo/internal/usgs"
)

func TestTelegramNotifier_Notify_Success(t *testing.T) {
	// 1. Criar um servidor mock
	var receivedPayload map[string]interface{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/botfake_token/sendMessage") {
			t.Errorf("URL inesperada chamada: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("falha ao ler payload no servidor mock: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		receivedPayload = payload

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true,"result":{"message_id":123}}`))
	}))
	defer ts.Close()

	// 2. Criar notifier e sobrepor o apiBaseURL
	tn := NewTelegramNotifier("fake_token", "fake_chat_id")
	tn.apiBaseURL = ts.URL

	// 3. Executar Notify
	feature := usgs.Feature{
		ID: "eq_test",
		Properties: usgs.Properties{
			Mag:   5.4,
			Place: "Caracas, Venezuela",
			Time:  time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC).UnixNano() / 1e6,
			URL:   "https://example.com/eq_test",
		},
		Geometry: usgs.Geometry{
			Coordinates: []float64{-66.9, 10.5, 20.0},
		},
	}

	err := tn.Notify(feature)
	if err != nil {
		t.Fatalf("Erro inesperado ao notificar: %v", err)
	}

	// 4. Validar o payload enviado
	if receivedPayload == nil {
		t.Fatal("Nenhum payload foi enviado ao servidor mock")
	}

	if receivedPayload["chat_id"] != "fake_chat_id" {
		t.Errorf("chat_id incorreto no payload: esperado fake_chat_id, obtido %v", receivedPayload["chat_id"])
	}

	text, ok := receivedPayload["text"].(string)
	if !ok {
		t.Fatal("Campo text ausente ou inválido no payload")
	}

	if !strings.Contains(text, "ALERTA DE TERREMOTO DETECTADO") {
		t.Errorf("Mensagem não contém o título esperado: %s", text)
	}
	if !strings.Contains(text, "Caracas, Venezuela") {
		t.Errorf("Mensagem não contém o local esperado: %s", text)
	}
	if !strings.Contains(text, "5.40") {
		t.Errorf("Mensagem não contém a magnitude esperada: %s", text)
	}
}

func TestTelegramNotifier_Notify_APIError(t *testing.T) {
	// Servidor mock retornando erro
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"ok":false,"description":"Bad Request: chat not found"}`))
	}))
	defer ts.Close()

	tn := NewTelegramNotifier("fake_token", "fake_chat_id")
	tn.apiBaseURL = ts.URL

	feature := usgs.Feature{
		ID: "eq_test",
	}

	err := tn.Notify(feature)
	if err == nil {
		t.Fatal("Esperava-se erro devido à resposta da API, mas foi retornado nil")
	}

	if !strings.Contains(err.Error(), "chat not found") {
		t.Errorf("Mensagem de erro não contém a descrição esperada: %v", err)
	}
}
