package config

import (
	"os"
	"strconv"
	"time"
)

// Config armazena os parâmetros de inicialização do monitor
type Config struct {
	USGSURL      string
	MinMagnitude float64
	Interval     time.Duration
	// Bounding Box (Limites Geográficos)
	MinLatitude  float64
	MaxLatitude  float64
	MinLongitude float64
	MaxLongitude float64

	// Configurações do Telegram
	TelegramToken     string
	TelegramChannelID string // ID ou @username do canal público (ex: @sismo_alertas)
}

// Load carrega as configurações do ambiente ou define valores padrão
func Load() *Config {
	return &Config{
		USGSURL:      getEnv("USGS_URL", "https://earthquake.usgs.gov/earthquakes/feed/v1.0/summary/2.5_day.geojson"),
		MinMagnitude: getEnvFloat("MIN_MAGNITUDE", 4.5),
		Interval:     getEnvDuration("MONITOR_INTERVAL", 1*time.Minute),
		// Padrões geográficos globais (cobre toda a Terra por padrão)
		MinLatitude:  getEnvFloat("MIN_LATITUDE", -90.0),
		MaxLatitude:  getEnvFloat("MAX_LATITUDE", 90.0),
		MinLongitude: getEnvFloat("MIN_LONGITUDE", -180.0),
		MaxLongitude: getEnvFloat("MAX_LONGITUDE", 180.0),

		// Configurações do canal do Telegram
		TelegramToken:     getEnv("TELEGRAM_BOT_TOKEN", ""),
		TelegramChannelID: getEnv("TELEGRAM_CHANNEL_ID", ""),
	}
}

func getEnv(key, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultVal
}

func getEnvFloat(key string, defaultVal float64) float64 {
	if value, exists := os.LookupEnv(key); exists {
		if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
			return floatVal
		}
	}
	return defaultVal
}

func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	if value, exists := os.LookupEnv(key); exists {
		if dur, err := time.ParseDuration(value); err == nil {
			return dur
		}
	}
	return defaultVal
}