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

	fmt.Println()
	fmt.Println("==================================================")
	fmt.Println("⚠️  ALERTA DE TERREMOTO DETECTADO!")
	fmt.Println("==================================================")
	fmt.Printf("ID:          %s\n", feature.ID)
	fmt.Printf("Local:       %s\n", feature.Properties.Place)
	fmt.Printf("Magnitude:   %.2f\n", feature.Properties.Mag)
	fmt.Printf("Data/Hora:   %s\n", tm.Format("2006-01-02 15:04:05 MST"))
	fmt.Printf("Coordenadas: Lat: %.4f, Lon: %.4f, Profundidade: %.2f km\n", lat, lon, depth)
	fmt.Printf("Link USGS:   %s\n", feature.Properties.URL)
	fmt.Println("==================================================")
	return nil
}