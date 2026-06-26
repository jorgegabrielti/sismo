package notifier

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sismo/internal/db"
	"sismo/internal/usgs"
)

func TestTelegramNotifier_Notify_MultiUser(t *testing.T) {
	// Cria banco temporário
	dbPath := filepath.Join(t.TempDir(), "users.db")
	userDB, err := db.NewDatabase(dbPath)
	if err != nil {
		t.Fatalf("Falha ao inicializar o banco nos testes: %v", err)
	}
	defer userDB.Close() // Fecha o banco no final para liberar travas de arquivo no Windows

	// Cadastra dois usuários com preferências de magnitudes diferentes
	userDB.SaveUser(db.UserPreference{ChatID: 111, MinMagnitude: 4.0, RegisteredAt: time.Now()})
	userDB.SaveUser(db.UserPreference{ChatID: 222, MinMagnitude: 5.0, RegisteredAt: time.Now()})

	// Servidor mock para capturar os envios do Notify
	receivedChats := make(map[int64]bool)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/botfake_token/sendMessage") {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var payload map[string]interface{}
		json.NewDecoder(r.Body).Decode(&payload)

		chatIDFloat, _ := payload["chat_id"].(float64)
		receivedChats[int64(chatIDFloat)] = true

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer ts.Close()

	tn := NewTelegramNotifier("fake_token", userDB)
	tn.apiBaseURL = ts.URL

	// Inicia o despachador de testes assíncrono
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go tn.StartDispatcher(ctx)

	// Evento de sismo com magnitude 4.5
	feature := usgs.Feature{
		ID: "eq_4_5",
		Properties: usgs.Properties{
			Mag:   4.5,
			Place: "Caracas, Venezuela",
			Time:  time.Now().UnixNano() / 1e6,
		},
	}

	err = tn.Notify(feature)
	if err != nil {
		t.Fatalf("Notify retornou erro inesperado: %v", err)
	}

	// Aguarda um curto intervalo para que a goroutine do dispatcher processe a fila
	time.Sleep(100 * time.Millisecond)

	// Usuário 111 deve receber (4.5 >= 4.0)
	if !receivedChats[111] {
		t.Error("Esperava-se que o usuário 111 recebesse o alerta, mas ele foi ignorado")
	}

	// Usuário 222 não deve receber (4.5 < 5.0)
	if receivedChats[222] {
		t.Error("Esperava-se que o usuário 222 fosse ignorado, mas ele recebeu o alerta")
	}
}

func TestTelegramNotifier_PollingAndCommands(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "users_polling.db")
	userDB, err := db.NewDatabase(dbPath)
	if err != nil {
		t.Fatalf("Falha ao inicializar o banco nos testes: %v", err)
	}
	defer userDB.Close() // Fecha o banco no final para liberar travas de arquivo no Windows

	step := 0
	var lastSentText string
	var lastSentChat int64

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if strings.HasSuffix(r.URL.Path, "getUpdates") {
			if step == 0 {
				// Simula o usuário enviando o comando /start
				w.Write([]byte(`{"ok":true,"result":[{"update_id":1000,"message":{"chat":{"id":12345},"text":"/start"}}]}`))
				step++
			} else if step == 1 {
				// Simula o usuário alterando a magnitude para 5.5
				w.Write([]byte(`{"ok":true,"result":[{"update_id":1001,"message":{"chat":{"id":12345},"text":"/magnitude 5.5"}}]}`))
				step++
			} else {
				// Retorna lista vazia
				w.Write([]byte(`{"ok":true,"result":[]}`))
			}
			return
		}

		if strings.HasSuffix(r.URL.Path, "sendMessage") {
			var payload map[string]interface{}
			json.NewDecoder(r.Body).Decode(&payload)
			lastSentText, _ = payload["text"].(string)
			chatIDFloat, _ := payload["chat_id"].(float64)
			lastSentChat = int64(chatIDFloat)
			w.Write([]byte(`{"ok":true,"result":{}}`))
			return
		}
	}))
	defer ts.Close()

	tn := NewTelegramNotifier("fake_token", userDB)
	tn.apiBaseURL = ts.URL

	// Executa o primeiro ciclo de polling (/start)
	err = tn.pollUpdates()
	if err != nil {
		t.Fatalf("pollUpdates retornou erro no ciclo 1: %v", err)
	}

	pref, exists := userDB.GetUser(12345)
	if !exists {
		t.Fatal("Esperava-se que o usuário estivesse cadastrado no banco")
	}
	if pref.MinMagnitude != 4.5 {
		t.Errorf("Magnitude padrão esperada era 4.5, obtida %.2f", pref.MinMagnitude)
	}
	if lastSentChat != 12345 || !strings.Contains(lastSentText, "Bem-vindo ao Sismo Alertas") {
		t.Errorf("Mensagem de boas-vindas não enviada ou incorreta: %s", lastSentText)
	}

	// Executa o segundo ciclo de polling (/magnitude 5.5)
	err = tn.pollUpdates()
	if err != nil {
		t.Fatalf("pollUpdates retornou erro no ciclo 2: %v", err)
	}

	pref, exists = userDB.GetUser(12345)
	if !exists {
		t.Fatal("Usuário sumiu do banco de dados")
	}
	if pref.MinMagnitude != 5.5 {
		t.Errorf("Magnitude esperada era 5.5, obtida %.2f", pref.MinMagnitude)
	}
	if !strings.Contains(lastSentText, "5.50") {
		t.Errorf("Mensagem de sucesso de magnitude incorreta: %s", lastSentText)
	}
}
