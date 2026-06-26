package notifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"sismo/internal/usgs"
)

// TelegramNotifier implementa a interface Notifier para enviar alertas ao Telegram
type TelegramNotifier struct {
	token      string
	chatID     string
	client     *http.Client
	apiBaseURL string
}

// NewTelegramNotifier cria uma nova instância do notificador do Telegram
func NewTelegramNotifier(token, chatID string) *TelegramNotifier {
	return &TelegramNotifier{
		token:      token,
		chatID:     chatID,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		apiBaseURL: "https://api.telegram.org",
	}
}

// Notify formata e envia a notificação de terremoto ao chat do Telegram
func (t *TelegramNotifier) Notify(feature usgs.Feature) error {
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

	apiURL := fmt.Sprintf("%s/bot%s/sendMessage", t.apiBaseURL, t.token)
	payload := map[string]interface{}{
		"chat_id":    t.chatID,
		"text":       msg,
		"parse_mode": "HTML",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("falha ao codificar payload do Telegram: %w", err)
	}

	resp, err := t.client.Post(apiURL, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("falha ao enviar requisição HTTP ao Telegram: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Description string `json:"description"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Description != "" {
			return fmt.Errorf("erro da API do Telegram (status %d): %s", resp.StatusCode, errResp.Description)
		}
		return fmt.Errorf("erro inesperado da API do Telegram com status %d", resp.StatusCode)
	}

	return nil
}
