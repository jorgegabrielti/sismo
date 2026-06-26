package server

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"sismo/internal/db"
)

// StartServer inicia um servidor HTTP simples na porta configurada para servir a pasta de assets da web e a API de relatos
func StartServer(port string, webDir string, userDB db.UserStore) {
	if port == "" {
		port = "8080"
	}

	// Verifica se o diretório estático existe
	if _, err := os.Stat(webDir); os.IsNotExist(err) {
		log.Printf("Aviso: Diretório de assets web '%s' não encontrado. Servidor estático pode falhar.", webDir)
	}

	// Cria um multiplexador de rotas local e isolado
	mux := http.NewServeMux()

	// Registra o endpoint da API de relatos da comunidade
	mux.HandleFunc("/api/reports", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		sismoID := r.URL.Query().Get("sismo_id")
		if sismoID == "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error":"sismo_id é obrigatório"}`)
			return
		}

		felt, notFelt, err := userDB.GetReportStats(sismoID)
		if err != nil {
			log.Printf("Erro ao buscar estatísticas de relato para sismo %s: %v", sismoID, err)
			fmt.Fprintf(w, `{"sismo_id":%q,"felt":0,"not_felt":0}`, sismoID)
			return
		}

		fmt.Fprintf(w, `{"sismo_id":%q,"felt":%d,"not_felt":%d}`, sismoID, felt, notFelt)
	})

	// Serve arquivos estáticos (catch-all)
	fs := http.FileServer(http.Dir(webDir))
	mux.Handle("/", fs)

	addr := fmt.Sprintf(":%s", port)
	log.Printf("Servidor da Landing Page disponível em http://localhost%s ...", addr)

	// Executa o servidor de forma síncrona
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Erro crítico no servidor web: %v", err)
	}
}
