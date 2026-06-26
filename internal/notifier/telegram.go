package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"sismo/internal/db"
	"sismo/internal/usgs"
)

// TelegramUpdate representa um update recebido da API do Telegram
type TelegramUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		MessageID int64 `json:"message_id"`
		Chat      struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		Text string `json:"text"`
	} `json:"message"`
}

// TelegramUpdatesResponse representa o payload de getUpdates
type TelegramUpdatesResponse struct {
	OK     bool             `json:"ok"`
	Result []TelegramUpdate `json:"result"`
}

// TelegramNotifier gerencia o envio de mensagens e a leitura de comandos
type TelegramNotifier struct {
	token        string
	userDB       *db.Database
	client       *http.Client
	apiBaseURL   string
	lastUpdateID int64
}

// NewTelegramNotifier cria uma nova instância do notificador do Telegram conectado ao banco
func NewTelegramNotifier(token string, userDB *db.Database) *TelegramNotifier {
	return &TelegramNotifier{
		token:      token,
		userDB:     userDB,
		client: &http.Client{
			Timeout: 15 * time.Second, // Timeout ligeiramente maior que o long polling do Telegram (10s)
		},
		apiBaseURL: "https://api.telegram.org",
	}
}

// StartListener inicia a escuta de comandos do Telegram de forma contínua e assíncrona
func (t *TelegramNotifier) StartListener(ctx context.Context) {
	log.Println("Iniciando escuta de comandos do bot no Telegram (Long Polling)...")
	for {
		select {
		case <-ctx.Done():
			log.Println("Escuta de comandos do Telegram encerrada.")
			return
		default:
			if err := t.pollUpdates(); err != nil {
				log.Printf("Erro ao buscar atualizações do Telegram: %v", err)
				// Delay de segurança em caso de erro para não sobrecarregar
				time.Sleep(5 * time.Second)
			}
		}
	}
}

// pollUpdates faz a chamada de Long Polling para obter novos comandos
func (t *TelegramNotifier) pollUpdates() error {
	url := fmt.Sprintf("%s/bot%s/getUpdates?offset=%d&timeout=10", t.apiBaseURL, t.token, t.lastUpdateID+1)
	resp, err := t.client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API retornou status HTTP %d", resp.StatusCode)
	}

	var updateResp TelegramUpdatesResponse
	if err := json.NewDecoder(resp.Body).Decode(&updateResp); err != nil {
		return err
	}

	if !updateResp.OK {
		return fmt.Errorf("sucesso falso no retorno da API do Telegram")
	}

	for _, update := range updateResp.Result {
		if update.UpdateID > t.lastUpdateID {
			t.lastUpdateID = update.UpdateID
		}

		if update.Message != nil && update.Message.Text != "" {
			t.handleMessage(update.Message.Chat.ID, update.Message.Text)
		}
	}

	return nil
}

// handleMessage interpreta os comandos e atualiza o banco de dados
func (t *TelegramNotifier) handleMessage(chatID int64, text string) {
	text = strings.TrimSpace(text)
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return
	}

	command := strings.ToLower(parts[0])

	switch command {
	case "/start":
		pref := db.UserPreference{
			ChatID:       chatID,
			MinMagnitude: 4.5,
			RegisteredAt: time.Now(),
		}
		if err := t.userDB.SaveUser(pref); err != nil {
			t.sendRawMessage(chatID, "❌ Ocorreu um erro interno ao salvar suas preferências de cadastro.")
			return
		}
		welcome := "👋 <b>Bem-vindo ao Sismo Alertas!</b>\n\n" +
			"Você foi cadastrado com sucesso para receber alertas de terremotos na Venezuela.\n\n" +
			"<b>Configurações iniciais:</b>\n" +
			"- Magnitude mínima: <b>4.5</b>\n\n" +
			"<b>Comandos disponíveis:</b>\n" +
			"- /magnitude &lt;valor&gt; : Altera a magnitude mínima dos alertas (ex: <code>/magnitude 5.0</code>)\n" +
			"- /status : Mostra suas configurações ativas\n" +
			"- /stop : Cancela a inscrição de alertas\n" +
			"- /help : Mostra este menu de ajuda"
		t.sendRawMessage(chatID, welcome)

	case "/help":
		help := "📚 <b>Instruções de Uso - Sismo Alertas</b>\n\n" +
			"Envie comandos simples para configurar o bot:\n" +
			"- /magnitude &lt;valor&gt; : Define o limite mínimo para alertas. Ex: <code>/magnitude 5.2</code> (valores de 1.0 a 9.9)\n" +
			"- /status : Mostra as configurações e dados de cadastro\n" +
			"- /stop : Cancela a inscrição nos alertas\n" +
			"- /help : Mostra esta ajuda"
		t.sendRawMessage(chatID, help)

	case "/status":
		pref, exists := t.userDB.GetUser(chatID)
		if !exists {
			t.sendRawMessage(chatID, "⚠️ Você não está cadastrado nos alertas. Envie /start para se inscrever.")
			return
		}
		statusMsg := fmt.Sprintf(
			"⚙️ <b>Suas Preferências de Alertas:</b>\n\n"+
				"- Alerta de magnitude mínima: <b>%.2f</b>\n"+
				"- Inscrito em: %s",
			pref.MinMagnitude,
			pref.RegisteredAt.Format("2006-01-02 15:04:05 MST"),
		)
		t.sendRawMessage(chatID, statusMsg)

	case "/magnitude":
		if len(parts) < 2 {
			t.sendRawMessage(chatID, "⚠️ Uso correto: <code>/magnitude [valor]</code> (Ex: <code>/magnitude 5.0</code>)")
			return
		}
		magStr := parts[1]
		mag, err := strconv.ParseFloat(magStr, 64)
		if err != nil || mag < 1.0 || mag > 9.9 {
			t.sendRawMessage(chatID, "❌ Por favor, insira uma magnitude válida entre 1.0 e 9.9.")
			return
		}

		pref, exists := t.userDB.GetUser(chatID)
		if !exists {
			pref = db.UserPreference{
				ChatID:       chatID,
				RegisteredAt: time.Now(),
			}
		}
		pref.MinMagnitude = mag
		if err := t.userDB.SaveUser(pref); err != nil {
			t.sendRawMessage(chatID, "❌ Ocorreu um erro interno ao salvar a nova magnitude.")
			return
		}

		t.sendRawMessage(chatID, fmt.Sprintf("✅ Sucesso! Agora você só receberá alertas para terremotos de magnitude <b>%.2f</b> ou superior.", mag))

	case "/stop":
		if err := t.userDB.DeleteUser(chatID); err != nil {
			t.sendRawMessage(chatID, "❌ Ocorreu um erro interno ao remover seu cadastro.")
			return
		}
		t.sendRawMessage(chatID, "📴 <b>Inscrição cancelada.</b> Você não receberá mais alertas. Se desejar se reinscrever, envie /start a qualquer momento.")

	default:
		t.sendRawMessage(chatID, "🤔 Comando não reconhecido. Envie /help para ver os comandos disponíveis.")
	}
}

// sendRawMessage envia uma mensagem HTML bruta para um chat ID específico
func (t *TelegramNotifier) sendRawMessage(chatID int64, text string) error {
	apiURL := fmt.Sprintf("%s/bot%s/sendMessage", t.apiBaseURL, t.token)
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := t.client.Post(apiURL, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Telegram API respondeu com status %d", resp.StatusCode)
	}

	return nil
}

// Notify envia o alerta de terremoto para todos os usuários cadastrados cujos filtros sejam compatíveis
func (t *TelegramNotifier) Notify(feature usgs.Feature) error {
	users := t.userDB.GetAllUsers()
	if len(users) == 0 {
		return nil // Nenhum usuário cadastrado para receber alertas
	}

	tm := time.Unix(feature.Properties.Time/1000, 0)
	lon := 0.0
	lat := 0.0
	depth := 0.0
	if len(feature.Geometry.Coordinates) > 0 {
		lon = feature.Geometry.Coordinates[0]
	}
	if len(feature.Geometry.Coordinates) > 1 {
		lat = feature.Geometry.Coordinates[1]
	}
	if len(feature.Geometry.Coordinates) > 2 {
		depth = feature.Geometry.Coordinates[2]
	}

	msg := fmt.Sprintf(
		"⚠️ <b>ALERTA DE TERREMOTO DETECTADO</b>\n\n"+
			"<b>Local:</b> %s\n"+
			"<b>Magnitude:</b> %.2f\n"+
			"<b>Data/Hora:</b> %s\n"+
			"<b>Coordenadas:</b> Lat: %.4f, Lon: %.4f\n"+
			"<b>Profundidade:</b> %.2f km\n\n"+
			"<a href=\"%s\">Mais detalhes no site da USGS</a>",
		feature.Properties.Place,
		feature.Properties.Mag,
		tm.Format("2006-01-02 15:04:05 MST"),
		lat, lon, depth,
		feature.Properties.URL,
	)

	// Dispara o alerta para cada usuário inscrito
	for _, user := range users {
		if feature.Properties.Mag >= user.MinMagnitude {
			if err := t.sendRawMessage(user.ChatID, msg); err != nil {
				log.Printf("Falha ao enviar notificação de sismo para usuário %d: %v", user.ChatID, err)
			}
		}
	}

	return nil
}
