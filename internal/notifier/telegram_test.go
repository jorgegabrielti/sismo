package notifier

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sismo/internal/db"
	"sismo/internal/usgs"
)

// MockUserStore implementa a interface db.UserStore em memória para os testes unitários
type MockUserStore struct {
	users   map[int64]db.UserPreference
	reports map[string]map[int64]bool
}

func NewMockUserStore() *MockUserStore {
	return &MockUserStore{
		users:   make(map[int64]db.UserPreference),
		reports: make(map[string]map[int64]bool),
	}
}

func (m *MockUserStore) SaveUser(pref db.UserPreference) error {
	m.users[pref.ChatID] = pref
	return nil
}

func (m *MockUserStore) GetUser(chatID int64) (db.UserPreference, bool) {
	pref, exists := m.users[chatID]
	return pref, exists
}

func (m *MockUserStore) DeleteUser(chatID int64) error {
	delete(m.users, chatID)
	return nil
}

func (m *MockUserStore) GetAllUsers() []db.UserPreference {
	var list []db.UserPreference
	for _, u := range m.users {
		list = append(list, u)
	}
	return list
}

func (m *MockUserStore) GetUsersForMagnitude(mag float64) []db.UserPreference {
	var list []db.UserPreference
	for _, u := range m.users {
		if u.MinMagnitude <= mag {
			list = append(list, u)
		}
	}
	return list
}

func (m *MockUserStore) SaveReport(chatID int64, sismoID string, felt bool) error {
	if m.reports[sismoID] == nil {
		m.reports[sismoID] = make(map[int64]bool)
	}
	m.reports[sismoID][chatID] = felt
	return nil
}

func (m *MockUserStore) GetReportStats(sismoID string) (feltCount int, didNotFeelCount int, err error) {
	userMap, exists := m.reports[sismoID]
	if !exists {
		return 0, 0, nil
	}
	for _, felt := range userMap {
		if felt {
			feltCount++
		} else {
			didNotFeelCount++
		}
	}
	return feltCount, didNotFeelCount, nil
}

func TestTelegramNotifier_Notify_MultiUser(t *testing.T) {
	userDB := NewMockUserStore()

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

	err := tn.Notify(feature)
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
	userDB := NewMockUserStore()

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
	err := tn.pollUpdates()
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

func TestTelegramNotifier_ProximityAndSilentMode(t *testing.T) {
	userDB := NewMockUserStore()

	latCaracas := 10.4806
	lonCaracas := -66.9036

	// 1. Usuário sem localização cadastrada (deve receber sempre)
	userDB.SaveUser(db.UserPreference{ChatID: 100, MinMagnitude: 4.0, RegisteredAt: time.Now()})

	// 2. Usuário com localização próxima (epicentro a ~10km, raio 50km - deve receber)
	userDB.SaveUser(db.UserPreference{
		ChatID:       200,
		MinMagnitude: 4.0,
		RegisteredAt: time.Now(),
		Latitude:     &latCaracas,
		Longitude:    &lonCaracas,
		MaxDistance:  50,
	})

	// 3. Usuário com localização distante (epicentro a ~10km, raio 5km - deve pular)
	userDB.SaveUser(db.UserPreference{
		ChatID:       300,
		MinMagnitude: 4.0,
		RegisteredAt: time.Now(),
		Latitude:     &latCaracas,
		Longitude:    &lonCaracas,
		MaxDistance:  5,
	})

	// 4. Usuário com modo silencioso ativo (deve enviar com disableNotification = true)
	userDB.SaveUser(db.UserPreference{
		ChatID:       400,
		MinMagnitude: 4.0,
		RegisteredAt: time.Now(),
		SilentMode:   true,
	})

	tn := NewTelegramNotifier("fake_token", userDB)

	// Simula sismo a cerca de 10km de Caracas (ex: Lat: 10.50, Lon: -66.85)
	feature := usgs.Feature{
		ID: "eq_nearby",
		Properties: usgs.Properties{
			Mag:   5.0,
			Place: "Próximo a Caracas",
			Time:  time.Now().UnixNano() / 1e6,
		},
		Geometry: usgs.Geometry{
			Coordinates: []float64{-66.85, 10.50, 10.0},
		},
	}

	err := tn.Notify(feature)
	if err != nil {
		t.Fatalf("Notify retornou erro: %v", err)
	}

	// Verifica o conteúdo da fila (tn.jobChan)
	close(tn.jobChan) // Fecha o canal para podermos ler em loop

	receivedJobs := make(map[int64]alertJob)
	for job := range tn.jobChan {
		receivedJobs[job.chatID] = job
	}

	// 1. Chat 100 deve estar na fila
	if _, exists := receivedJobs[100]; !exists {
		t.Error("Esperava-se que o usuário 100 recebesse o alerta (sem filtro de raio)")
	}

	// 2. Chat 200 deve estar na fila
	if _, exists := receivedJobs[200]; !exists {
		t.Error("Esperava-se que o usuário 200 recebesse o alerta (dentro do raio de 50km)")
	}

	// 3. Chat 300 NÃO deve estar na fila
	if _, exists := receivedJobs[300]; exists {
		t.Error("Esperava-se que o usuário 300 não recebesse o alerta (fora do raio de 5km)")
	}

	// 4. Chat 400 deve estar na fila e ter disableNotification = true
	job400, exists := receivedJobs[400]
	if !exists {
		t.Error("Esperava-se que o usuário 400 recebesse o alerta")
	} else if !job400.disableNotification {
		t.Error("Esperava-se que o alerta para o usuário 400 fosse silencioso")
	}
}

func TestParsePeriod(t *testing.T) {
	tests := []struct {
		arg     string
		want    time.Duration
		wantErr bool
	}{
		{"", 24 * time.Hour, false},
		{"12h", 12 * time.Hour, false},
		{"2d", 48 * time.Hour, false},
		{"30d", 30 * 24 * time.Hour, false},
		{"0h", 0, true},
		{"31d", 0, true},
		{"invalid", 0, true},
	}

	for _, tt := range tests {
		got, err := parsePeriod(tt.arg)
		if (err != nil) != tt.wantErr {
			t.Errorf("parsePeriod(%q) err = %v, wantErr = %v", tt.arg, err, tt.wantErr)
		}
		if got != tt.want {
			t.Errorf("parsePeriod(%q) = %v, want %v", tt.arg, got, tt.want)
		}
	}
}
