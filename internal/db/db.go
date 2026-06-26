package db

import (
	"database/sql"
	"time"

	_ "github.com/lib/pq"
)

// UserPreference armazena as preferências e dados cadastrais de um usuário no Telegram
type UserPreference struct {
	ChatID       int64     `json:"chat_id"`
	MinMagnitude float64   `json:"min_magnitude"`
	RegisteredAt time.Time `json:"registered_at"`
}

// UserStore define o contrato para persistência e busca de usuários e suas preferências
type UserStore interface {
	SaveUser(pref UserPreference) error
	GetUser(chatID int64) (UserPreference, bool)
	DeleteUser(chatID int64) error
	GetAllUsers() []UserPreference
	GetUsersForMagnitude(mag float64) []UserPreference
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
		registered_at TIMESTAMP WITH TIME ZONE NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_users_magnitude ON users(min_magnitude);
	`
	_, err := db.db.Exec(query)
	return err
}

// Close encerra a conexão com o banco de dados
func (db *Database) Close() error {
	return db.db.Close()
}

// SaveUser adiciona ou atualiza um usuário no banco (Upsert) usando sintaxe PostgreSQL
func (db *Database) SaveUser(pref UserPreference) error {
	query := `
	INSERT INTO users (chat_id, min_magnitude, registered_at)
	VALUES ($1, $2, $3)
	ON CONFLICT (chat_id) DO UPDATE SET
		min_magnitude = EXCLUDED.min_magnitude;
	`
	_, err := db.db.Exec(query, pref.ChatID, pref.MinMagnitude, pref.RegisteredAt)
	return err
}

// GetUser busca as preferências de um usuário pelo Chat ID
func (db *Database) GetUser(chatID int64) (UserPreference, bool) {
	query := `SELECT chat_id, min_magnitude, registered_at FROM users WHERE chat_id = $1`
	row := db.db.QueryRow(query, chatID)

	var pref UserPreference
	err := row.Scan(&pref.ChatID, &pref.MinMagnitude, &pref.RegisteredAt)
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
	query := `SELECT chat_id, min_magnitude, registered_at FROM users`
	rows, err := db.db.Query(query)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var list []UserPreference
	for rows.Next() {
		var pref UserPreference
		if err := rows.Scan(&pref.ChatID, &pref.MinMagnitude, &pref.RegisteredAt); err == nil {
			list = append(list, pref)
		}
	}
	return list
}

// GetUsersForMagnitude retorna apenas os usuários com magnitude limite compatível com o tremor
func (db *Database) GetUsersForMagnitude(mag float64) []UserPreference {
	query := `SELECT chat_id, min_magnitude, registered_at FROM users WHERE min_magnitude <= $1`
	rows, err := db.db.Query(query, mag)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var list []UserPreference
	for rows.Next() {
		var pref UserPreference
		if err := rows.Scan(&pref.ChatID, &pref.MinMagnitude, &pref.RegisteredAt); err == nil {
			list = append(list, pref)
		}
	}
	return list
}
