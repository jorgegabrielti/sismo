package notifier

import (
	"fmt"
	"sismo/internal/usgs"
	"time"
)

// Notifier define a interface comum para envio de alertas de terremotos
type Notifier interface {
	Notify(feature usgs.Feature) error
}

// ConsoleNotifier implementa a interface Notifier para exibir os alertas no terminal
type ConsoleNotifier struct{}

// NewConsoleNotifier cria uma nova instância do notificador de console
func NewConsoleNotifier() *ConsoleNotifier {
	return &ConsoleNotifier{}
}

// Notify formata e exibe o alerta do terremoto no console
func (c *ConsoleNotifier) Notify(feature usgs.Feature) error {
	// A API da USGS retorna o tempo em milissegundos
	tm := time.Unix(feature.Properties.Time/1000, 0).UTC()

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

	fmt.Println()
	fmt.Println("==================================================")
	if feature.Properties.Tsunami == 1 {
		fmt.Println("🚨 ALERTA DE TSUNAMI ATIVO! 🌊")
	}
	fmt.Println("⚠️  ALERTA DE EVENTO SÍSMICO DETECTADO")
	fmt.Println("==================================================")
	fmt.Printf("ID:            %s\n", feature.ID)
	if feature.Properties.Type != "" {
		fmt.Printf("Tipo:          %s\n", feature.Properties.Type)
	}
	fmt.Printf("Local:         %s\n", feature.Properties.Place)
	magStr := fmt.Sprintf("%.2f", feature.Properties.Mag)
	if feature.Properties.MagType != "" {
		magStr += " (" + feature.Properties.MagType + ")"
	}
	fmt.Printf("Magnitude:     %s\n", magStr)
	fmt.Printf("Data/Hora:     %s\n", tm.Format("2006-01-02 15:04:05 MST"))
	fmt.Printf("Coordenadas:   Lat: %.4f, Lon: %.4f, Profundidade: %.2f km\n", lat, lon, depth)
	if feature.Properties.Alert != "" {
		fmt.Printf("Alerta PAGER:  %s\n", feature.Properties.Alert)
	}
	if feature.Properties.Sig > 0 {
		fmt.Printf("Significância: %d/1000\n", feature.Properties.Sig)
	}
	if feature.Properties.Felt != nil && *feature.Properties.Felt > 0 {
		fmt.Printf("Relatos USGS:  %d\n", *feature.Properties.Felt)
	}
	fmt.Printf("Link USGS:     %s\n", feature.Properties.URL)
	fmt.Println("==================================================")
	return nil
}