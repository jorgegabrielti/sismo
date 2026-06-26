package server

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

// StartServer inicia um servidor HTTP simples na porta configurada para servir a pasta de assets da web
func StartServer(port string, webDir string) {
	if port == "" {
		port = "8080"
	}

	// Verifica se o diretório estático existe
	if _, err := os.Stat(webDir); os.IsNotExist(err) {
		log.Printf("Aviso: Diretório de assets web '%s' não encontrado. Servidor estático pode falhar.", webDir)
	}

	fs := http.FileServer(http.Dir(webDir))
	http.Handle("/", fs)

	addr := fmt.Sprintf(":%s", port)
	log.Printf("Servidor da Landing Page disponível em http://localhost%s ...", addr)

	// Executa o servidor de forma síncrona (deve ser invocado em uma goroutine)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Erro crítico no servidor web: %v", err)
	}
}
