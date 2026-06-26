package db

import (
	"database/sql"
	"log"
	"time"

	_ "github.com/lib/pq"
)

// UserPreference armazena as preferências e dados cadastrais de um usuário no Telegram
type UserPreference struct {
	ChatID       int64     `json:"chat_id"`
	MinMagnitude float64   `json:"min_magnitude"`
	RegisteredAt time.Time `json:"registered_at"`
	Latitude     *float64  `json:"latitude,omitempty"`
	Longitude    *float64  `json:"longitude,omitempty"`
	MaxDistance  float64   `json:"max_distance"`
	SilentMode   bool      `json:"silent_mode"`
}

// UserStore define o contrato para persistência e busca de usuários e suas preferências
type UserStore interface {
	SaveUser(pref UserPreference) error
	GetUser(chatID int64) (UserPreference, bool)
	DeleteUser(chatID int64) error
	GetAllUsers() []UserPreference
	GetUsersForMagnitude(mag float64) []UserPreference
	SaveReport(chatID int64, sismoID string, felt bool) error
	GetReportStats(sismoID string) (feltCount int, didNotFeelCount int, err error)
}

// Database gerencia a leitura e escrita no banco de dados PostgreSQL
type Database struct {
	db *sql.DB
}

// NewDatabase inicializa a conexão com o PostgreSQL e executa as migrações de tabelas
func NewDatabase(connStr string) (*Database, error) {
	dbConn, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}

	// Configura limites de conexão adequados para alta concorrência
	dbConn.SetMaxOpenConns(25)
	dbConn.SetMaxIdleConns(25)
	dbConn.SetConnMaxLifetime(5 * time.Minute)

	db := &Database{db: dbConn}
	if err := db.migrate(); err != nil {
		dbConn.Close()
		return nil, err
	}

	return db, nil
}

// migrate cria a tabela e os índices compatíveis com PostgreSQL
func (db *Database) migrate() error {
	query := `
	CREATE TABLE IF NOT EXISTS users (
		chat_id BIGINT PRIMARY KEY,
		min_magnitude DOUBLE PRECISION NOT NULL,
		registered_at TIMESTAMP WITH TIME ZONE NOT NULL,
		latitude DOUBLE PRECISION,
		longitude DOUBLE PRECISION,
		max_distance DOUBLE PRECISION NOT NULL DEFAULT 0,
		silent_mode BOOLEAN NOT NULL DEFAULT FALSE
	);
	CREATE INDEX IF NOT EXISTS idx_users_magnitude ON users(min_magnitude);

	CREATE TABLE IF NOT EXISTS reports (
		id SERIAL PRIMARY KEY,
		chat_id BIGINT NOT NULL,
		sismo_id VARCHAR(50) NOT NULL,
		felt BOOLEAN NOT NULL,
		reported_at TIMESTAMP WITH TIME ZONE NOT NULL,
		UNIQUE(chat_id, sismo_id)
	);
	`
	_, err := db.db.Exec(query)
	if err != nil {
		return err
	}

	// Executa migrações adicionais caso o banco de dados já exista de execuções anteriores
	alterQueries := []string{
		"ALTER TABLE users ADD COLUMN IF NOT EXISTS latitude DOUBLE PRECISION;",
		"ALTER TABLE users ADD COLUMN IF NOT EXISTS longitude DOUBLE PRECISION;",
		"ALTER TABLE users ADD COLUMN IF NOT EXISTS max_distance DOUBLE PRECISION NOT NULL DEFAULT 0;",
		"ALTER TABLE users ADD COLUMN IF NOT EXISTS silent_mode BOOLEAN NOT NULL DEFAULT FALSE;",
	}
	for _, q := range alterQueries {
		if _, err := db.db.Exec(q); err != nil {
			log.Printf("Aviso ao rodar migração de coluna: %v", err)
		}
	}

	return nil
}

// Close encerra a conexão com o banco de dados
func (db *Database) Close() error {
	return db.db.Close()
}

// SaveUser adiciona ou atualiza um usuário no banco (Upsert) usando sintaxe PostgreSQL
func (db *Database) SaveUser(pref UserPreference) error {
	query := `
	INSERT INTO users (chat_id, min_magnitude, registered_at, latitude, longitude, max_distance, silent_mode)
	VALUES ($1, $2, $3, $4, $5, $6, $7)
	ON CONFLICT (chat_id) DO UPDATE SET
		min_magnitude = EXCLUDED.min_magnitude,
		latitude = EXCLUDED.latitude,
		longitude = EXCLUDED.longitude,
		max_distance = EXCLUDED.max_distance,
		silent_mode = EXCLUDED.silent_mode;
	`
	_, err := db.db.Exec(query, pref.ChatID, pref.MinMagnitude, pref.RegisteredAt, pref.Latitude, pref.Longitude, pref.MaxDistance, pref.SilentMode)
	return err
}

// GetUser busca as preferências de um usuário pelo Chat ID
func (db *Database) GetUser(chatID int64) (UserPreference, bool) {
	query := `SELECT chat_id, min_magnitude, registered_at, latitude, longitude, max_distance, silent_mode FROM users WHERE chat_id = $1`
	row := db.db.QueryRow(query, chatID)

	var pref UserPreference
	err := row.Scan(&pref.ChatID, &pref.MinMagnitude, &pref.RegisteredAt, &pref.Latitude, &pref.Longitude, &pref.MaxDistance, &pref.SilentMode)
	if err != nil {
		return UserPreference{}, false
	}

	return pref, true
}

// DeleteUser remove o cadastro do usuário
func (db *Database) DeleteUser(chatID int64) error {
	query := `DELETE FROM users WHERE chat_id = $1`
	_, err := db.db.Exec(query, chatID)
	return err
}

// GetAllUsers retorna uma lista de todos os usuários registrados
func (db *Database) GetAllUsers() []UserPreference {
	query := `SELECT chat_id, min_magnitude, registered_at, latitude, longitude, max_distance, silent_mode FROM users`
	rows, err := db.db.Query(query)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var list []UserPreference
	for rows.Next() {
		var pref UserPreference
		if err := rows.Scan(&pref.ChatID, &pref.MinMagnitude, &pref.RegisteredAt, &pref.Latitude, &pref.Longitude, &pref.MaxDistance, &pref.SilentMode); err == nil {
			list = append(list, pref)
		}
	}
	return list
}

// GetUsersForMagnitude retorna apenas os usuários com magnitude limite compatível com o tremor
func (db *Database) GetUsersForMagnitude(mag float64) []UserPreference {
	query := `SELECT chat_id, min_magnitude, registered_at, latitude, longitude, max_distance, silent_mode FROM users WHERE min_magnitude <= $1`
	rows, err := db.db.Query(query, mag)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var list []UserPreference
	for rows.Next() {
		var pref UserPreference
		if err := rows.Scan(&pref.ChatID, &pref.MinMagnitude, &pref.RegisteredAt, &pref.Latitude, &pref.Longitude, &pref.MaxDistance, &pref.SilentMode); err == nil {
			list = append(list, pref)
		}
	}
	return list
}

// SaveReport registra ou atualiza o relato de um terremoto por um usuário
func (db *Database) SaveReport(chatID int64, sismoID string, felt bool) error {
	query := `
	INSERT INTO reports (chat_id, sismo_id, felt, reported_at)
	VALUES ($1, $2, $3, $4)
	ON CONFLICT (chat_id, sismo_id) DO UPDATE SET
		felt = EXCLUDED.felt,
		reported_at = EXCLUDED.reported_at;
	`
	_, err := db.db.Exec(query, chatID, sismoID, felt, time.Now())
	return err
}

// GetReportStats retorna a contagem agregada de quantos sentiram ou não sentiram um sismo
func (db *Database) GetReportStats(sismoID string) (feltCount int, didNotFeelCount int, err error) {
	query := `
	SELECT 
		COUNT(CASE WHEN felt = TRUE THEN 1 END) as felt_count,
		COUNT(CASE WHEN felt = FALSE THEN 1 END) as not_felt_count
	FROM reports
	WHERE sismo_id = $1
	`
	err = db.db.QueryRow(query, sismoID).Scan(&feltCount, &didNotFeelCount)
	return feltCount, didNotFeelCount, err
}
