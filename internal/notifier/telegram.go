package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strings"
	"time"

	"sismo/internal/usgs"
)

// alertJob armazena uma mensagem a ser entregue ao canal
type alertJob struct {
	text     string
	sismoID  string
	hasPhoto bool
	photoURL string
}

// TelegramNotifier gerencia o envio de alertas sísmicos para um canal público do Telegram
type TelegramNotifier struct {
	token      string
	channelID  string
	client     *http.Client
	apiBaseURL string
	jobChan    chan alertJob
}

// NewTelegramNotifier cria uma nova instância do publicador de canal Telegram
func NewTelegramNotifier(token, channelID string) *TelegramNotifier {
	return &TelegramNotifier{
		token:      token,
		channelID:  channelID,
		client:     &http.Client{Timeout: 15 * time.Second},
		apiBaseURL: "https://api.telegram.org",
		jobChan:    make(chan alertJob, 1000),
	}
}

// StartDispatcher processa a fila de publicações aplicando rate limiting seguro
func (t *TelegramNotifier) StartDispatcher(ctx context.Context) {
	log.Println("Iniciando despachador de alertas para o canal (Rate Limiter - 1 msg/s)...")
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Despachador do Telegram encerrado.")
			return
		case job := <-t.jobChan:
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				go func(j alertJob) {
					var err error
					if j.hasPhoto {
						err = t.sendPhoto(j.photoURL, j.text)
						if err != nil {
							log.Printf("Falha ao enviar foto, tentando texto simples: %v", err)
							err = t.sendMessage(j.text)
						}
					} else {
						err = t.sendMessage(j.text)
					}
					if err != nil {
						log.Printf("Falha ao publicar alerta no canal: %v", err)
					}
				}(job)
			}
		}
	}
}

// Notify formata e enfileira o alerta de terremoto para publicação no canal
func (t *TelegramNotifier) Notify(feature usgs.Feature) error {
	tm := time.Unix(feature.Properties.Time/1000, 0)

	lon, lat, depth := 0.0, 0.0, 0.0
	if len(feature.Geometry.Coordinates) > 0 {
		lon = feature.Geometry.Coordinates[0]
	}
	if len(feature.Geometry.Coordinates) > 1 {
		lat = feature.Geometry.Coordinates[1]
	}
	if len(feature.Geometry.Coordinates) > 2 {
		depth = feature.Geometry.Coordinates[2]
	}

	mapURL := fmt.Sprintf(
		"https://static-maps.yandex.ru/1.x/?ll=%f,%f&spn=1.2,1.2&size=600,350&l=map&pt=%f,%f,pm2rdl",
		lon, lat, lon, lat,
	)

	// Título do evento
	eventTitle := "ALERTA DE TERREMOTO DETECTADO"
	if feature.Properties.Type != "" && strings.ToLower(feature.Properties.Type) != "earthquake" {
		eventTitle = fmt.Sprintf("ALERTA DE %s DETECTADO", strings.ToUpper(translateEventType(feature.Properties.Type)))
	}

	// Cabeçalho de tsunami
	tsunamiHeader := ""
	if feature.Properties.Tsunami == 1 {
		tsunamiHeader = "🚨 <b>ALERTA DE TSUNAMI ATIVO! 🌊</b>\n\n"
	}

	// Magnitude com tipo
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

	parts = append(parts, "")
	parts = append(parts, fmt.Sprintf("<a href=\"%s\">Mais detalhes no site da USGS</a>", feature.Properties.URL))

	msg := strings.Join(parts, "\n")

	t.jobChan <- alertJob{
		text:     msg,
		sismoID:  feature.ID,
		hasPhoto: true,
		photoURL: mapURL,
	}

	return nil
}

// sendMessage publica uma mensagem HTML no canal configurado
func (t *TelegramNotifier) sendMessage(text string) error {
	apiURL := fmt.Sprintf("%s/bot%s/sendMessage", t.apiBaseURL, t.token)
	payload := map[string]interface{}{
		"chat_id":    t.channelID,
		"text":       text,
		"parse_mode": "HTML",
	}
	return t.postJSON(apiURL, payload)
}

// sendPhoto publica uma foto com legenda HTML no canal configurado
func (t *TelegramNotifier) sendPhoto(photoURL, caption string) error {
	apiURL := fmt.Sprintf("%s/bot%s/sendPhoto", t.apiBaseURL, t.token)
	payload := map[string]interface{}{
		"chat_id":    t.channelID,
		"photo":      photoURL,
		"caption":    caption,
		"parse_mode": "HTML",
	}
	return t.postJSON(apiURL, payload)
}

// postJSON serializa e envia um payload JSON para a API do Telegram
func (t *TelegramNotifier) postJSON(url string, payload map[string]interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := t.client.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API respondeu com status %d", resp.StatusCode)
	}
	return nil
}

// --- Funções de formatação ---

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
