package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// UserPreference armazena as preferências e dados cadastrais de um usuário no Telegram
type UserPreference struct {
	ChatID       int64     `json:"chat_id"`
	MinMagnitude float64   `json:"min_magnitude"`
	RegisteredAt time.Time `json:"registered_at"`
}

// Database gerencia a leitura e escrita no banco de dados SQLite local
type Database struct {
	db *sql.DB
}

// NewDatabase inicializa a conexão com o SQLite e executa as migrações de tabelas
func NewDatabase(filePath string) (*Database, error) {
	// Garante que o diretório pai existe
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	dbConn, err := sql.Open("sqlite", filePath)
	if err != nil {
		return nil, err
	}

	// Limita o pool a 1 conexão aberta simultaneamente para evitar concorrência de travas (locking) no SQLite local
	dbConn.SetMaxOpenConns(1)

	db := &Database{db: dbConn}
	if err := db.migrate(); err != nil {
		dbConn.Close()
		return nil, err
	}

	return db, nil
}

// migrate executa a criação da tabela de usuários e do índice de magnitude se não existirem
func (db *Database) migrate() error {
	query := `
	CREATE TABLE IF NOT EXISTS users (
		chat_id INTEGER PRIMARY KEY,
		min_magnitude REAL NOT NULL,
		registered_at TEXT NOT NULL
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

// SaveUser adiciona ou atualiza um usuário no banco (Upsert)
func (db *Database) SaveUser(pref UserPreference) error {
	query := `
	INSERT INTO users (chat_id, min_magnitude, registered_at)
	VALUES (?, ?, ?)
	ON CONFLICT(chat_id) DO UPDATE SET
		min_magnitude = excluded.min_magnitude;
	`
	_, err := db.db.Exec(query, pref.ChatID, pref.MinMagnitude, pref.RegisteredAt.Format(time.RFC3339))
	return err
}

// GetUser busca as preferências de um usuário pelo Chat ID
func (db *Database) GetUser(chatID int64) (UserPreference, bool) {
	query := `SELECT chat_id, min_magnitude, registered_at FROM users WHERE chat_id = ?`
	row := db.db.QueryRow(query, chatID)

	var pref UserPreference
	var regAtStr string
	err := row.Scan(&pref.ChatID, &pref.MinMagnitude, &regAtStr)
	if err != nil {
		if err == sql.ErrNoRows {
			return UserPreference{}, false
		}
		return UserPreference{}, false
	}

	if t, err := time.Parse(time.RFC3339, regAtStr); err == nil {
		pref.RegisteredAt = t
	}

	return pref, true
}

// DeleteUser remove o cadastro do usuário
func (db *Database) DeleteUser(chatID int64) error {
	query := `DELETE FROM users WHERE chat_id = ?`
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
		var regAtStr string
		if err := rows.Scan(&pref.ChatID, &pref.MinMagnitude, &regAtStr); err == nil {
			if t, err := time.Parse(time.RFC3339, regAtStr); err == nil {
				pref.RegisteredAt = t
			}
			list = append(list, pref)
		}
	}
	return list
}

// GetUsersForMagnitude retorna apenas os usuários com magnitude limite compatível com o tremor
func (db *Database) GetUsersForMagnitude(mag float64) []UserPreference {
	query := `SELECT chat_id, min_magnitude, registered_at FROM users WHERE min_magnitude <= ?`
	rows, err := db.db.Query(query, mag)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var list []UserPreference
	for rows.Next() {
		var pref UserPreference
		var regAtStr string
		if err := rows.Scan(&pref.ChatID, &pref.MinMagnitude, &regAtStr); err == nil {
			if t, err := time.Parse(time.RFC3339, regAtStr); err == nil {
				pref.RegisteredAt = t
			}
			list = append(list, pref)
		}
	}
	return list
}
