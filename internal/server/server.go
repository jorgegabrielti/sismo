package server

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

// StartServer inicia um servidor HTTP simples para servir a landing page estática
func StartServer(port string, webDir string) {
	if port == "" {
		port = "8080"
	}

	// Verifica se o diretório estático existe
	if _, err := os.Stat(webDir); os.IsNotExist(err) {
		log.Printf("Aviso: Diretório de assets web '%s' não encontrado. Servidor estático pode falhar.", webDir)
	}

	mux := http.NewServeMux()

	// Serve arquivos estáticos da pasta web/
	fs := http.FileServer(http.Dir(webDir))
	mux.Handle("/", fs)

	addr := fmt.Sprintf(":%s", port)
	log.Printf("Servidor da Landing Page disponível em http://localhost%s ...", addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Erro crítico no servidor web: %v", err)
	}
}
