package usgs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Properties representa os metadados de cada terremoto
type Properties struct {
	Mag     float64  `json:"mag"`
	Place   string   `json:"place"`
	Time    int64    `json:"time"` // Timestamp em milisegundos
	URL     string   `json:"url"`
	Felt    *int     `json:"felt"`
	CDI     *float64 `json:"cdi"`
	MMI     *float64 `json:"mmi"`
	Alert   string   `json:"alert"`
	Status  string   `json:"status"`
	Tsunami int      `json:"tsunami"`
	Sig     int      `json:"sig"`
	MagType string   `json:"magType"`
	NST     *int     `json:"nst"`
	DMin    *float64 `json:"dmin"`
	Gap     *float64 `json:"gap"`
	RMS     *float64 `json:"rms"`
	Type    string   `json:"type"`
}

//Geometry representa as coordenadas geográficas do sismo
type Geometry struct {
	Coordinates []float64 `json:"coordinates"` // [longitude, latitude, profundidade]
}

// Feature representa um sismo individual com suas propriedades e geometria
type Feature struct {
	ID  		string `json:"id"`
	Properties 	Properties `json:"properties"`
	Geometry	Geometry 	 `json:"geometry"`
}

//EathquakeFeed é a estrutura principal retornada pelo GeoJSON do USGS
type EarthquakeFeed struct {
	Features []Feature `json:"features"`
}

//Client gerencia a comunicação com a API do USGS
type Client struct {
	url	string
	httpClient *http.Client
}

//NewClient crie uma nova instância do cliente USGS
func NewClient(url string) *Client {
	return &Client{
		url: url,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

//Fetch busca o feed de terremotos recentes
func (c *Client) Fetch() (*EarthquakeFeed, error) {
	resp, err := c.httpClient.Get(c.url)
	if err != nil {
		return nil, fmt.Errorf("Falha ao consultar a API da USGS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Status HTTP inesperado da API da USGS: %d", resp.StatusCode)
	}

	var feed EarthquakeFeed
	if err := json.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("falha no parse do JSON: %w", err)
	}
	return &feed, nil
}

// BoundingBox representa limites geográficos retangulares
type BoundingBox struct {
	MinLat float64
	MaxLat float64
	MinLon float64
	MaxLon float64
}

// Query realiza uma busca customizada de terremotos históricos/recentes no USGS
func (c *Client) Query(ctx context.Context, start time.Time, minMag float64, lat, lon *float64, maxDistKm float64, bbox *BoundingBox) (*EarthquakeFeed, error) {
	baseURL := "https://earthquake.usgs.gov/fdsnws/event/1/query"

	req, err := http.NewRequestWithContext(ctx, "GET", baseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("falha ao criar requisição de busca: %w", err)
	}

	q := req.URL.Query()
	q.Add("format", "geojson")
	q.Add("starttime", start.UTC().Format("2006-01-02T15:04:05"))
	q.Add("minmagnitude", fmt.Sprintf("%.2f", minMag))
	q.Add("orderby", "time")

	if bbox != nil {
		q.Add("minlatitude", fmt.Sprintf("%.4f", bbox.MinLat))
		q.Add("maxlatitude", fmt.Sprintf("%.4f", bbox.MaxLat))
		q.Add("minlongitude", fmt.Sprintf("%.4f", bbox.MinLon))
		q.Add("maxlongitude", fmt.Sprintf("%.4f", bbox.MaxLon))
	} else if lat != nil && lon != nil && maxDistKm > 0 {
		q.Add("latitude", fmt.Sprintf("%.4f", *lat))
		q.Add("longitude", fmt.Sprintf("%.4f", *lon))
		q.Add("maxradiuskm", fmt.Sprintf("%.2f", maxDistKm))
	}

	req.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("falha na requisição de busca USGS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status HTTP inesperado na busca da USGS: %d", resp.StatusCode)
	}

	var feed EarthquakeFeed
	if err := json.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("falha no parse do JSON de busca: %w", err)
	}
	return &feed, nil
}