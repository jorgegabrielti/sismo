package db

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// UserPreference armazena as preferências e dados cadastrais de um usuário no Telegram
type UserPreference struct {
	ChatID       int64     `json:"chat_id"`
	MinMagnitude float64   `json:"min_magnitude"`
	RegisteredAt time.Time `json:"registered_at"`
}

// Database gerencia a leitura e escrita thread-safe de dados do usuário em um arquivo JSON
type Database struct {
	filePath string
	mu       sync.RWMutex
	users    map[int64]UserPreference
}

// NewDatabase inicializa e carrega o arquivo de banco de dados JSON
func NewDatabase(filePath string) (*Database, error) {
	db := &Database{
		filePath: filePath,
		users:    make(map[int64]UserPreference),
	}

	if err := db.load(); err != nil {
		return nil, err
	}

	return db, nil
}

// load lê o arquivo JSON e carrega na memória
func (db *Database) load() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Garante que o diretório pai existe
	dir := filepath.Dir(db.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Se o arquivo não existe, inicializa ele vazio
	if _, err := os.Stat(db.filePath); os.IsNotExist(err) {
		return db.saveLocked()
	}

	file, err := os.Open(db.filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	var data map[int64]UserPreference
	if err := json.NewDecoder(file).Decode(&data); err != nil {
		// Em caso de erro de parse (arquivo vazio/corrompido), reinicia em memória
		db.users = make(map[int64]UserPreference)
		return nil
	}

	db.users = data
	return nil
}

// saveLocked grava o estado atual no arquivo JSON. Deve ser chamado com o Lock ativo.
func (db *Database) saveLocked() error {
	file, err := os.Create(db.filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(db.users)
}

// SaveUser adiciona ou atualiza um usuário no banco
func (db *Database) SaveUser(pref UserPreference) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	db.users[pref.ChatID] = pref
	return db.saveLocked()
}

// GetUser busca as preferências de um usuário pelo Chat ID
func (db *Database) GetUser(chatID int64) (UserPreference, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	pref, exists := db.users[chatID]
	return pref, exists
}

// DeleteUser remove o cadastro do usuário
func (db *Database) DeleteUser(chatID int64) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if _, exists := db.users[chatID]; !exists {
		return nil
	}

	delete(db.users, chatID)
	return db.saveLocked()
}

// GetAllUsers retorna uma cópia de todos os usuários registrados
func (db *Database) GetAllUsers() []UserPreference {
	db.mu.RLock()
	defer db.mu.RUnlock()

	list := make([]UserPreference, 0, len(db.users))
	for _, pref := range db.users {
		list = append(list, pref)
	}
	return list
}
