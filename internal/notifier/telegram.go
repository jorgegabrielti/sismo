package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"sismo/internal/db"
	"sismo/internal/usgs"
)

// TelegramLocation representa a geolocalização enviada pelo usuário
type TelegramLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// TelegramUpdate representa um update recebido da API do Telegram
type TelegramUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		MessageID int64             `json:"message_id"`
		Chat      struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		Text     string            `json:"text"`
		Location *TelegramLocation `json:"location"`
	} `json:"message"`
	CallbackQuery *struct {
		ID      string `json:"id"`
		From    struct {
			ID int64 `json:"id"`
		} `json:"from"`
		Message *struct {
			MessageID int64 `json:"message_id"`
			Chat      struct {
				ID int64 `json:"id"`
			} `json:"chat"`
		} `json:"message"`
		Data    string `json:"data"`
	} `json:"callback_query"`
}

// TelegramUpdatesResponse representa o payload de getUpdates
type TelegramUpdatesResponse struct {
	OK     bool             `json:"ok"`
	Result []TelegramUpdate `json:"result"`
}

// alertJob armazena uma mensagem a ser entregue em lote a um usuário
type alertJob struct {
	chatID              int64
	text                string
	sismoID             string
	hasPhoto            bool
	photoURL            string
	disableNotification bool
}

// TelegramNotifier gerencia o envio de mensagens com controle de vazão (rate limiting) e leitura de comandos
type TelegramNotifier struct {
	token        string
	userDB       db.UserStore
	client       *http.Client
	apiBaseURL   string
	lastUpdateID int64
	jobChan      chan alertJob
}

// NewTelegramNotifier cria uma nova instância do notificador do Telegram conectado ao banco
func NewTelegramNotifier(token string, userDB db.UserStore) *TelegramNotifier {
	return &TelegramNotifier{
		token:      token,
		userDB:     userDB,
		client: &http.Client{
			Timeout: 15 * time.Second, // Timeout ligeiramente maior que o long polling do Telegram (10s)
		},
		apiBaseURL: "https://api.telegram.org",
		jobChan:    make(chan alertJob, 150000), // Buffer de até 150.000 alertas em fila
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

// StartDispatcher processa a fila de disparos assíncronos aplicando o Rate Limiting
func (t *TelegramNotifier) StartDispatcher(ctx context.Context) {
	log.Println("Iniciando despachador de alertas (Rate Limiter - 25 msg/s)...")
	ticker := time.NewTicker(40 * time.Millisecond) // Garante o limite seguro de no máximo 25 mensagens por segundo (limite do TG é 30/s)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Despachador de alertas do Telegram encerrado.")
			return
		case job := <-t.jobChan:
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Dispara o envio HTTP em goroutine separada para não travar o ticker de contagem
				go func(j alertJob) {
					var err error
					if j.hasPhoto {
						err = t.sendRawPhoto(j.chatID, j.photoURL, j.text, j.sismoID, j.disableNotification)
						if err != nil {
							log.Printf("Falha ao enviar foto com alerta do Telegram, tentando texto simples: %v", err)
							err = t.sendRawMessageWithKeyboard(j.chatID, j.text, j.sismoID, j.disableNotification)
						}
					} else {
						err = t.sendRawMessageWithKeyboard(j.chatID, j.text, j.sismoID, j.disableNotification)
					}
					if err != nil {
						log.Printf("Falha ao entregar alerta do Telegram para o chat %d: %v", j.chatID, err)
					}
				}(job)
			}
		}
	}
}

// pollUpdates faz a chamada de Long Polling para obter novos comandos e ações
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

		// Trata as mensagens
		if update.Message != nil {
			if update.Message.Location != nil {
				t.handleLocation(update.Message.Chat.ID, update.Message.Location.Latitude, update.Message.Location.Longitude)
			} else if update.Message.Text != "" {
				t.handleMessage(update.Message.Chat.ID, update.Message.Text)
			}
		}

		// Trata os cliques nos botões inline
		if update.CallbackQuery != nil {
			t.handleCallbackQuery(
				update.CallbackQuery.ID,
				update.CallbackQuery.From.ID,
				update.CallbackQuery.Message.Chat.ID,
				update.CallbackQuery.Message.MessageID,
				update.CallbackQuery.Data,
			)
		}
	}

	return nil
}

// handleLocation configura as coordenadas e ativa o raio de proximidade
func (t *TelegramNotifier) handleLocation(chatID int64, lat, lon float64) {
	pref, exists := t.userDB.GetUser(chatID)
	if !exists {
		pref = db.UserPreference{
			ChatID:       chatID,
			MinMagnitude: 4.5,
			RegisteredAt: time.Now(),
		}
	}
	pref.Latitude = &lat
	pref.Longitude = &lon
	if pref.MaxDistance <= 0 {
		pref.MaxDistance = 300 // Raio padrão de 300km ao registrar a localização
	}

	if err := t.userDB.SaveUser(pref); err != nil {
		t.sendRawMessage(chatID, "❌ Erro ao salvar sua localização no banco de dados.")
		return
	}

	msg := fmt.Sprintf(
		"📍 <b>Localização cadastrada com sucesso!</b>\n\n"+
			"- Coordenadas: <code>Lat: %.4f, Lon: %.4f</code>\n"+
			"- Raio de alerta atual: <b>%.0f km</b>\n\n"+
			"Você agora receberá apenas alertas de sismos ocorridos dentro desse raio. Para mudar o raio, envie e.g. <code>/raio 150</code>. Para desativar o filtro de distância e receber todos no mundo, envie <code>/raio 0</code>.",
		lat, lon, pref.MaxDistance,
	)
	t.sendRawMessage(chatID, msg)
}

// handleCallbackQuery registra as respostas interativas aos botões
func (t *TelegramNotifier) handleCallbackQuery(queryID string, userID int64, chatID int64, messageID int64, data string) {
	t.answerCallbackQuery(queryID, "Relato processado!")

	parts := strings.SplitN(data, ":", 2)
	if len(parts) < 2 {
		return
	}
	action := parts[0]
	sismoID := parts[1]

	felt := false
	var responseText string
	if action == "felt_yes" {
		felt = true
		responseText = "🟢 <b>Obrigado pelo seu relato!</b> Você registrou que <b>sentiu</b> o tremor. Suas informações ajudam a mapear o sismo."
	} else if action == "felt_no" {
		felt = false
		responseText = "⚪ <b>Relato registrado.</b> Você registrou que <b>não sentiu</b> o tremor."
	} else {
		return
	}

	// Salva o feedback usando a chave exclusiva do usuário/sismo
	if err := t.userDB.SaveReport(userID, sismoID, felt); err != nil {
		log.Printf("Erro ao salvar relato do usuário %d: %v", userID, err)
		return
	}

	// Remove os botões inline originais da mensagem para evitar novas cliques duplicados
	_ = t.editMessageReplyMarkup(chatID, messageID)
	t.sendRawMessage(chatID, responseText)
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
			MaxDistance:  0,
			SilentMode:   false,
		}
		if err := t.userDB.SaveUser(pref); err != nil {
			t.sendRawMessage(chatID, "❌ Ocorreu um erro interno ao salvar suas preferências de cadastro.")
			return
		}
		welcome := "👋 <b>Bem-vindo ao Sismo Alertas!</b>\n\n" +
			"Você foi cadastrado com sucesso para receber alertas de terremotos em todo o mundo.\n\n" +
			"<b>Configurações iniciais:</b>\n" +
			"- Magnitude mínima: <b>4.5</b>\n" +
			"- Filtro de proximidade: <b>Desativado</b> (recebe todos)\n" +
			"- Modo Silencioso: <b>Desativado</b>\n\n" +
			"<b>📍 Filtros de Distância Inteligente:</b>\n" +
			"Compartilhe sua localização (usando o menu de anexar do Telegram) para receber alertas baseados na sua distância real do epicentro!\n\n" +
			"<b>Comandos disponíveis:</b>\n" +
			"- /magnitude &lt;valor&gt; : Altera a magnitude mínima (ex: <code>/magnitude 5.0</code>)\n" +
			"- /localizacao : Como compartilhar sua localização para filtros de raio\n" +
			"- /raio &lt;km&gt; : Altera o raio máximo em km para alertas (ex: <code>/raio 200</code>)\n" +
			"- /removerlocal : Remove sua localização e desativa filtros de raio\n" +
			"- /silencioso : Ativa/desativa alertas sem som (modo DND)\n" +
			"- /prevencao : Guia de segurança e preparação sísmica\n" +
			"- /status : Mostra suas configurações ativas\n" +
			"- /stop : Cancela a inscrição\n" +
			"- /help : Mostra este menu de ajuda"
		t.sendRawMessage(chatID, welcome)

	case "/help":
		help := "📚 <b>Instruções de Uso - Sismo Alertas</b>\n\n" +
			"Envie comandos simples para configurar o bot:\n" +
			"- /magnitude &lt;valor&gt; : Define a magnitude mínima (valores de 1.0 a 9.9)\n" +
			"- /localizacao : Veja como compartilhar sua localização\n" +
			"- /raio &lt;km&gt; : Altera o raio limite de alertas. Use <code>/raio 0</code> para receber todos.\n" +
			"- /removerlocal : Remove as coordenadas de localização de seu perfil\n" +
			"- /silencioso : Alterna o envio com/sem som de notificação\n" +
			"- /prevencao : Exibe dicas de segurança e preparação sísmica\n" +
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
		locStr := "Não informada"
		raioStr := "Desativado (recebe em todo o mundo)"
		if pref.Latitude != nil && pref.Longitude != nil {
			locStr = fmt.Sprintf("%.4f, %.4f", *pref.Latitude, *pref.Longitude)
			if pref.MaxDistance > 0 {
				raioStr = fmt.Sprintf("%.0f km", pref.MaxDistance)
			}
		}
		silentStr := "Desativado (com som)"
		if pref.SilentMode {
			silentStr = "Ativado (sem som)"
		}
		statusMsg := fmt.Sprintf(
			"⚙️ <b>Suas Preferências de Alertas:</b>\n\n"+
				"- Magnitude mínima: <b>%.2f</b>\n"+
				"- Localização: <b>%s</b>\n"+
				"- Raio de alerta: <b>%s</b>\n"+
				"- Modo silencioso: <b>%s</b>\n"+
				"- Inscrito em: %s",
			pref.MinMagnitude,
			locStr,
			raioStr,
			silentStr,
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

	case "/localizacao":
		msg := "📍 <b>Como compartilhar sua localização:</b>\n\n" +
			"1. Clique no ícone de <b>clipe de papel</b> (anexo) no rodapé do chat.\n" +
			"2. Selecione a opção <b>Localização</b>.\n" +
			"3. Escolha <b>Enviar minha localização atual</b>.\n\n" +
			"Após enviar, o bot configurará automaticamente um raio padrão de 300 km para alertas sísmicos. Você poderá personalizar esse raio a qualquer momento usando o comando /raio."
		t.sendRawMessage(chatID, msg)

	case "/raio":
		if len(parts) < 2 {
			t.sendRawMessage(chatID, "⚠️ Uso correto: <code>/raio [distancia_em_km]</code> (ex: <code>/raio 150</code>). Use <code>/raio 0</code> para receber todos.")
			return
		}
		distStr := parts[1]
		dist, err := strconv.ParseFloat(distStr, 64)
		if err != nil || dist < 0 || dist > 10000 {
			t.sendRawMessage(chatID, "❌ Distância inválida. Por favor, insira um número entre 0 e 10000 em quilômetros.")
			return
		}

		pref, exists := t.userDB.GetUser(chatID)
		if !exists {
			pref = db.UserPreference{
				ChatID:       chatID,
				MinMagnitude: 4.5,
				RegisteredAt: time.Now(),
			}
		}
		pref.MaxDistance = dist
		if err := t.userDB.SaveUser(pref); err != nil {
			t.sendRawMessage(chatID, "❌ Erro ao salvar o novo raio de alerta.")
			return
		}

		if dist == 0 {
			t.sendRawMessage(chatID, "✅ Filtro de distância desativado. Você receberá alertas para qualquer sismo relevante no mundo.")
		} else {
			t.sendRawMessage(chatID, fmt.Sprintf("✅ Sucesso! Agora você receberá apenas alertas para terremotos em um raio de até <b>%.0f km</b> de você.", dist))
		}

	case "/silencioso":
		pref, exists := t.userDB.GetUser(chatID)
		if !exists {
			pref = db.UserPreference{
				ChatID:       chatID,
				MinMagnitude: 4.5,
				RegisteredAt: time.Now(),
			}
		}
		pref.SilentMode = !pref.SilentMode
		if err := t.userDB.SaveUser(pref); err != nil {
			t.sendRawMessage(chatID, "❌ Erro ao atualizar as preferências do modo silencioso.")
			return
		}

		if pref.SilentMode {
			t.sendRawMessage(chatID, "🔔 <b>Modo Silencioso ATIVADO!</b>\n\nVocê continuará recebendo todos os alertas de terremotos, mas as notificações serão enviadas sem som de notificação (silenciosas).")
		} else {
			t.sendRawMessage(chatID, "🔊 <b>Modo Silencioso DESATIVADO!</b>\n\nOs alertas voltarão a emitir alertas normais com som.")
		}

	case "/removerlocal":
		pref, exists := t.userDB.GetUser(chatID)
		if !exists {
			t.sendRawMessage(chatID, "⚠️ Você não está cadastrado nos alertas. Envie /start para se inscrever.")
			return
		}
		pref.Latitude = nil
		pref.Longitude = nil
		pref.MaxDistance = 0
		if err := t.userDB.SaveUser(pref); err != nil {
			t.sendRawMessage(chatID, "❌ Erro ao remover a localização.")
			return
		}
		t.sendRawMessage(chatID, "✅ Localização e filtros de distância removidos com sucesso. Você voltará a receber alertas de todo o mundo.")

	case "/prevencao", "/dicas":
		prevMsg := "🧭 <b>Guia de Preparação e Segurança Sísmica</b>\n\n" +
			"<b>1. Antes do Terremoto:</b>\n" +
			"- Monte um kit de emergência (água, lanterna, rádio, pilhas, remédios básicos).\n" +
			"- Fixe móveis pesados (armários, estantes) na parede.\n" +
			"- Identifique os locais mais seguros na sua casa (sob mesas fortes, vigas estruturais).\n\n" +
			"<b>2. Durante o Terremoto:</b>\n" +
			"- <b>AGACHE, PROTEJA-SE E AGUARDE (Drop, Cover, Hold On)</b>.\n" +
			"- Fique embaixo de uma mesa resistente e segure-se nela.\n" +
			"- Afaste-se de janelas de vidro, espelhos e objetos suspensos.\n" +
			"- Se estiver na rua, afaste-se de prédios, postes de luz e fiação elétrica.\n\n" +
			"<b>3. Depois do Terremoto:</b>\n" +
			"- Prepare-se para eventuais réplicas (tremores secundários).\n" +
			"- Verifique vazamentos de gás e desligue o registro de energia caso perceba danos.\n" +
			"- Use escadas, <b>nunca</b> o elevador.\n" +
			"- Mantenha as linhas telefônicas livres apenas para emergências graves."
		t.sendRawMessage(chatID, prevMsg)

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
	return t.sendRawMessageWithKeyboard(chatID, text, "", false)
}

// sendRawMessageWithKeyboard envia mensagem de texto com teclado interativo opcional
func (t *TelegramNotifier) sendRawMessageWithKeyboard(chatID int64, text string, sismoID string, disableNotification bool) error {
	apiURL := fmt.Sprintf("%s/bot%s/sendMessage", t.apiBaseURL, t.token)
	payload := map[string]interface{}{
		"chat_id":              chatID,
		"text":                 text,
		"parse_mode":           "HTML",
		"disable_notification": disableNotification,
	}

	if sismoID != "" {
		payload["reply_markup"] = map[string]interface{}{
			"inline_keyboard": [][]map[string]interface{}{
				{
					{"text": "🟢 Sim, senti!", "callback_data": fmt.Sprintf("felt_yes:%s", sismoID)},
					{"text": "⚪ Não senti", "callback_data": fmt.Sprintf("felt_no:%s", sismoID)},
				},
			},
		}
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

// sendRawPhoto envia uma foto via URL (mapa estático) com legenda HTML e teclado interativo opcional
func (t *TelegramNotifier) sendRawPhoto(chatID int64, photoURL string, caption string, sismoID string, disableNotification bool) error {
	apiURL := fmt.Sprintf("%s/bot%s/sendPhoto", t.apiBaseURL, t.token)
	payload := map[string]interface{}{
		"chat_id":              chatID,
		"photo":                photoURL,
		"caption":              caption,
		"parse_mode":           "HTML",
		"disable_notification": disableNotification,
	}

	if sismoID != "" {
		payload["reply_markup"] = map[string]interface{}{
			"inline_keyboard": [][]map[string]interface{}{
				{
					{"text": "🟢 Sim, senti!", "callback_data": fmt.Sprintf("felt_yes:%s", sismoID)},
					{"text": "⚪ Não senti", "callback_data": fmt.Sprintf("felt_no:%s", sismoID)},
				},
			},
		}
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

// editMessageReplyMarkup remove os botões inline de uma mensagem específica
func (t *TelegramNotifier) editMessageReplyMarkup(chatID int64, messageID int64) error {
	apiURL := fmt.Sprintf("%s/bot%s/editMessageReplyMarkup", t.apiBaseURL, t.token)
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": messageID,
		"reply_markup": map[string]interface{}{
			"inline_keyboard": [][]map[string]interface{}{},
		},
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
	return nil
}

// answerCallbackQuery responde a chamadas de botões inline para sumir com o ícone de carregamento no Telegram
func (t *TelegramNotifier) answerCallbackQuery(queryID string, text string) error {
	apiURL := fmt.Sprintf("%s/bot%s/answerCallbackQuery", t.apiBaseURL, t.token)
	payload := map[string]interface{}{
		"callback_query_id": queryID,
		"text":              text,
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
	return nil
}

func formatMercalli(val float64) string {
	intVal := int(math.Round(val))
	switch intVal {
	case 1:
		return "I (Não sentido)"
	case 2:
		return "II (Muito Fraco)"
	case 3:
		return "III (Fraco)"
	case 4:
		return "IV (Moderado)"
	case 5:
		return "V (Forte)"
	case 6:
		return "VI (Bastante Forte)"
	case 7:
		return "VII (Muito Forte)"
	case 8:
		return "VIII (Severo)"
	case 9:
		return "IX (Violento)"
	case 10:
		return "X (Extremo)"
	default:
		if val > 10 {
			return "X+ (Extremo)"
		}
		return "I (Não sentido)"
	}
}

func translateEventType(t string) string {
	switch strings.ToLower(t) {
	case "earthquake":
		return "Terremoto"
	case "quarry blast":
		return "Explosão em Pedreira"
	case "explosion":
		return "Explosão"
	case "ice quake":
		return "Criossismo (Terremoto de Gelo)"
	case "volcanic eruption":
		return "Erupção Vulcânica"
	case "mine collapse":
		return "Desabamento de Mina"
	case "landslide":
		return "Deslizamento de Terra"
	case "sonic boom":
		return "Estrondo Sônico"
	default:
		if t == "" {
			return "Terremoto"
		}
		runes := []rune(t)
		runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
		return string(runes)
	}
}

func formatPager(alert string) string {
	switch strings.ToLower(alert) {
	case "green":
		return "🟢 Verde (Baixo impacto / Sem vítimas)"
	case "yellow":
		return "🟡 Amarelo (Impacto moderado / Danos parciais)"
	case "orange":
		return "🟠 Laranja (Impacto significativo / Danos prováveis)"
	case "red":
		return "🔴 Vermelho (Impacto grave / Danos severos)"
	default:
		return ""
	}
}

func formatStatus(status string) string {
	switch strings.ToLower(status) {
	case "reviewed":
		return "Revisado por Sismólogos 🔍"
	case "automatic":
		return "Automático (Não revisado) 🤖"
	default:
		return status
	}
}

// Notify filtra e enfileira o alerta de terremoto para envio assíncrono aos usuários compatíveis
func (t *TelegramNotifier) Notify(feature usgs.Feature) error {
	// Busca do banco apenas os usuários cujo limite seja inferior ou igual à magnitude registrada
	users := t.userDB.GetUsersForMagnitude(feature.Properties.Mag)
	if len(users) == 0 {
		return nil // Nenhum usuário afetado por este sismo
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

	// Gera o link de mapa estático (Yandex Static API) com pin vermelho no epicentro
	mapURL := fmt.Sprintf("https://static-maps.yandex.ru/1.x/?ll=%f,%f&spn=1.2,1.2&size=600,350&l=map&pt=%f,%f,pm2rdl", lon, lat, lon, lat)

	// Formatação comum da legenda da notificação
	eventTitle := "ALERTA DE TERREMOTO DETECTADO"
	if feature.Properties.Type != "" && strings.ToLower(feature.Properties.Type) != "earthquake" {
		eventTitle = fmt.Sprintf("ALERTA DE %s DETECTADO", strings.ToUpper(translateEventType(feature.Properties.Type)))
	}

	tsunamiHeader := ""
	if feature.Properties.Tsunami == 1 {
		tsunamiHeader = "🚨 <b>ALERTA DE TSUNAMI ATIVO! 🌊</b>\n\n"
	}

	magStr := fmt.Sprintf("%.2f", feature.Properties.Mag)
	if feature.Properties.MagType != "" {
		magStr = fmt.Sprintf("%.2f (%s)", feature.Properties.Mag, strings.ToUpper(feature.Properties.MagType))
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("%s⚠️ <b>%s</b>", tsunamiHeader, eventTitle))
	parts = append(parts, "")
	parts = append(parts, fmt.Sprintf("<b>Local:</b> %s", feature.Properties.Place))
	parts = append(parts, fmt.Sprintf("<b>Magnitude:</b> %s", magStr))
	parts = append(parts, fmt.Sprintf("<b>Data/Hora:</b> %s", tm.Format("2006-01-02 15:04:05 MST")))
	parts = append(parts, fmt.Sprintf("<b>Coordenadas:</b> Lat: %.4f, Lon: %.4f", lat, lon))
	parts = append(parts, fmt.Sprintf("<b>Profundidade:</b> %.2f km", depth))

	if pagerStr := formatPager(feature.Properties.Alert); pagerStr != "" {
		parts = append(parts, fmt.Sprintf("🚨 <b>Alerta PAGER:</b> %s", pagerStr))
	}

	if feature.Properties.Sig > 0 {
		parts = append(parts, fmt.Sprintf("⭐ <b>Significância:</b> %d/1000", feature.Properties.Sig))
	}

	if feature.Properties.Felt != nil && *feature.Properties.Felt > 0 {
		feltStr := fmt.Sprintf("👥 <b>Relatos na USGS (DYFI):</b> %d relatos", *feature.Properties.Felt)
		if feature.Properties.CDI != nil {
			feltStr += fmt.Sprintf(" (Máx: %s)", formatMercalli(*feature.Properties.CDI))
		}
		parts = append(parts, feltStr)
	}

	if feature.Properties.MMI != nil {
		parts = append(parts, fmt.Sprintf("📈 <b>Intensidade Estimada (MMI):</b> %s", formatMercalli(*feature.Properties.MMI)))
	}

	var qualStats []string
	if feature.Properties.NST != nil && *feature.Properties.NST > 0 {
		qualStats = append(qualStats, fmt.Sprintf("Estações: %d", *feature.Properties.NST))
	}
	if feature.Properties.Gap != nil && *feature.Properties.Gap > 0 {
		qualStats = append(qualStats, fmt.Sprintf("Lacuna: %.0f°", *feature.Properties.Gap))
	}
	if feature.Properties.RMS != nil && *feature.Properties.RMS > 0 {
		qualStats = append(qualStats, fmt.Sprintf("Resíduo: %.2fs", *feature.Properties.RMS))
	}
	if len(qualStats) > 0 {
		parts = append(parts, fmt.Sprintf("📡 <b>Qualidade:</b> %s", strings.Join(qualStats, " | ")))
	}

	if feature.Properties.Status != "" {
		parts = append(parts, fmt.Sprintf("🔍 <b>Status:</b> %s", formatStatus(feature.Properties.Status)))
	}

	// Enfileira os alertas na fila em memória para processamento controlado do Rate Limiter
	for _, user := range users {
		userDistStr := ""
		// Se o usuário tiver localização cadastrada, faz o cálculo de proximidade
		if user.Latitude != nil && user.Longitude != nil {
			dist := haversine(*user.Latitude, *user.Longitude, lat, lon)
			// Se o raio de distância estiver ativado ( > 0) e a distância for superior, pula envio
			if user.MaxDistance > 0 && dist > user.MaxDistance {
				continue
			}
			userDistStr = fmt.Sprintf("\n📍 <b>Epicentro a %.1f km de você!</b>\n", dist)
		}

		userParts := make([]string, len(parts))
		copy(userParts, parts)

		if userDistStr != "" {
			userParts = append(userParts, userDistStr)
		} else {
			userParts = append(userParts, "")
		}

		userParts = append(userParts, fmt.Sprintf("<a href=\"%s\">Mais detalhes no site da USGS</a>", feature.Properties.URL))
		msg := strings.Join(userParts, "\n")

		t.jobChan <- alertJob{
			chatID:              user.ChatID,
			text:                msg,
			sismoID:             feature.ID,
			hasPhoto:            true,
			photoURL:            mapURL,
			disableNotification: user.SilentMode,
		}
	}

	return nil
}

// haversine calcula a distância em km entre duas coordenadas geográficas
func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371.0 // Raio médio da Terra em km
	dLat := (lat2 - lat1) * math.Pi / 180.0
	dLon := (lon2 - lon1) * math.Pi / 180.0
	rLat1 := lat1 * math.Pi / 180.0
	rLat2 := lat2 * math.Pi / 180.0

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Sin(dLon/2)*math.Sin(dLon/2)*math.Cos(rLat1)*math.Cos(rLat2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}
