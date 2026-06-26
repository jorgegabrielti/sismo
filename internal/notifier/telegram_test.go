package notifier

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"sismo/internal/usgs"
)

// TestTelegramNotifier_Notify_SendsPhotoWithCaption verifica que um sismo válido é
// enfileirado e publicado no canal como foto (mapa) com legenda contendo os dados do evento.
func TestTelegramNotifier_Notify_SendsPhotoWithCaption(t *testing.T) {
	var mu sync.Mutex
	var receivedChat string
	var receivedCaption string
	var sendPhotoCalled bool

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/botfake_token/sendPhoto") {
			var payload map[string]interface{}
			json.NewDecoder(r.Body).Decode(&payload)

			mu.Lock()
			sendPhotoCalled = true
			receivedChat, _ = payload["chat_id"].(string)
			receivedCaption, _ = payload["caption"].(string)
			mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true,"result":{}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	tn := NewTelegramNotifier("fake_token", "@canal_teste")
	tn.apiBaseURL = ts.URL

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go tn.StartDispatcher(ctx)

	feature := usgs.Feature{
		ID: "eq_caracas",
		Properties: usgs.Properties{
			Mag:   5.4,
			Place: "Caracas, Venezuela",
			Time:  time.Now().UnixNano() / 1e6,
			URL:   "https://earthquake.usgs.gov/earthquakes/eventpage/eq_caracas",
		},
		Geometry: usgs.Geometry{
			Coordinates: []float64{-66.9036, 10.4806, 12.5},
		},
	}

	if err := tn.Notify(feature); err != nil {
		t.Fatalf("Notify retornou erro inesperado: %v", err)
	}

	// Aguarda o despachador processar a fila (rate limiter de 1 msg/s)
	time.Sleep(1200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if !sendPhotoCalled {
		t.Fatal("Esperava-se que sendPhoto fosse chamado para publicar o alerta com mapa")
	}
	if receivedChat != "@canal_teste" {
		t.Errorf("chat_id esperado '@canal_teste', obtido '%s'", receivedChat)
	}
	if !strings.Contains(receivedCaption, "Caracas, Venezuela") {
		t.Errorf("legenda não contém o local do sismo: %s", receivedCaption)
	}
	if !strings.Contains(receivedCaption, "5.40") {
		t.Errorf("legenda não contém a magnitude esperada: %s", receivedCaption)
	}
}

// TestTelegramNotifier_Notify_FallbackToTextOnPhotoFailure verifica que, quando o envio
// da foto falha, o despachador rebaixa a mensagem para texto simples (resiliência).
func TestTelegramNotifier_Notify_FallbackToTextOnPhotoFailure(t *testing.T) {
	var mu sync.Mutex
	var sendMessageCalled bool
	var receivedText string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/botfake_token/sendPhoto"):
			// Simula falha no servidor de mapas
			w.WriteHeader(http.StatusInternalServerError)
		case strings.HasSuffix(r.URL.Path, "/botfake_token/sendMessage"):
			var payload map[string]interface{}
			json.NewDecoder(r.Body).Decode(&payload)

			mu.Lock()
			sendMessageCalled = true
			receivedText, _ = payload["text"].(string)
			mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true,"result":{}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	tn := NewTelegramNotifier("fake_token", "@canal_teste")
	tn.apiBaseURL = ts.URL

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go tn.StartDispatcher(ctx)

	feature := usgs.Feature{
		ID: "eq_fallback",
		Properties: usgs.Properties{
			Mag:   6.1,
			Place: "Offshore Chile",
			Time:  time.Now().UnixNano() / 1e6,
		},
		Geometry: usgs.Geometry{
			Coordinates: []float64{-71.5, -33.0, 20.0},
		},
	}

	if err := tn.Notify(feature); err != nil {
		t.Fatalf("Notify retornou erro inesperado: %v", err)
	}

	time.Sleep(1200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if !sendMessageCalled {
		t.Fatal("Esperava-se fallback para sendMessage após falha do sendPhoto")
	}
	if !strings.Contains(receivedText, "Offshore Chile") {
		t.Errorf("texto de fallback não contém o local do sismo: %s", receivedText)
	}
}

func TestFormatMercalli(t *testing.T) {
	tests := []struct {
		val  float64
		want string
	}{
		{1, "I (Não sentido)"},
		{5, "V (Forte)"},
		{10, "X (Extremo)"},
		{12, "X+ (Extremo)"},
		{0, "I (Não sentido)"},
	}

	for _, tt := range tests {
		if got := formatMercalli(tt.val); got != tt.want {
			t.Errorf("formatMercalli(%v) = %q, want %q", tt.val, got, tt.want)
		}
	}
}

func TestTranslateEventType(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"earthquake", "Terremoto"},
		{"quarry blast", "Explosão em Pedreira"},
		{"volcanic eruption", "Erupção Vulcânica"},
		{"", "Terremoto"},
		{"landslide", "Deslizamento de Terra"},
	}

	for _, tt := range tests {
		if got := translateEventType(tt.in); got != tt.want {
			t.Errorf("translateEventType(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatPager(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"green", "🟢 Verde (Baixo impacto / Sem vítimas)"},
		{"red", "🔴 Vermelho (Impacto grave / Danos severos)"},
		{"", ""},
		{"unknown", ""},
	}

	for _, tt := range tests {
		if got := formatPager(tt.in); got != tt.want {
			t.Errorf("formatPager(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatStatus(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"reviewed", "Revisado por Sismólogos 🔍"},
		{"automatic", "Automático (Não revisado) 🤖"},
		{"outro", "outro"},
	}

	for _, tt := range tests {
		if got := formatStatus(tt.in); got != tt.want {
			t.Errorf("formatStatus(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
