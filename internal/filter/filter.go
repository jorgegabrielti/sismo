package filter 

import (
	"sismo/internal/config"
	"sismo/internal/usgs"
	"sync"
	"time"
)

// Filter contém a lógica de filtragem e armazena o cache de terremotos já notificados
type Filter struct {
	cfg			*config.Config
	seenCache	map[string]time.Time
	mu			sync.RWMutex
}

// NewFilter cria uma nova instância do filtro com cache limpo
func NewFilter(cfg *config.Config) *Filter {
	return &Filter{
		cfg: cfg,
		seenCache: make(map[string]time.Time),
	}
}

// Evaluate verifica se o sismo atende aos critérios (magnitude, região) e se ainda não foi notificado
func (f *Filter) Evaluate(feature usgs.Feature) bool {
	// 1. Validar magniture
	if feature.Properties.Mag < f.cfg.MinMagnitude {
		return false
	}

	// 2. Validar localização
	// O GeoJSON da USGS armazena no formato [longitude, latitude, profundidade]
	if len(feature.Geometry.Coordinates) < 2 {
		return false
	}

	lon := feature.Geometry.Coordinates[0]
	lat := feature.Geometry.Coordinates[1]

	if lat < f.cfg.MinLatitude || lat > f.cfg.MaxLatitude {
		return false
	}

	if lon < f.cfg.MinLongitude || lon > f.cfg.MaxLongitude {
		return false
	}

	// 3. Validar se já foi notificado (Deduplicação)
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, seen := f.seenCache[feature.ID]; seen {
		return false
	}

	// 4. Validar idade do sismo (evita enviar alertas antigos ao iniciar/reiniciar o monitor)
	eventTime := time.Unix(feature.Properties.Time/1000, 0)
	if time.Since(eventTime) > f.cfg.MaxAge {
		// Adiciona ao cache para evitar reprocessamento de buscas futuras, mas não notifica
		f.seenCache[feature.ID] = time.Now()
		return false
	}

	// Salva no cache com o timestamp atual
	f.seenCache[feature.ID] = time.Now()
	return true
}

// CleanCache remove do cache registros de terremotos com mais de 24 hora para evitar vazamento de memória
func (f *Filter) CleanCache() {
	f.mu.Lock()
	defer f.mu.Unlock()

	cutoff := time.Now().Add(-24 * time.Hour)
	for id, timestamp := range f.seenCache {
		if timestamp.Before(cutoff) {
			delete(f.seenCache, id)
		}
	}
}