package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"sismo/internal/config"
	"sismo/internal/db"
	"sismo/internal/filter"
	"sismo/internal/notifier"
	"sismo/internal/server"
	"sismo/internal/usgs"
)

func main() {
	log.Println("Carregando configurações...")
	cfg := config.Load()

	log.Printf("Configurações carregadas:")
	log.Printf("- USGS URL: %s", cfg.USGSURL)
	log.Printf("- Intervalo: %v", cfg.Interval)
	log.Printf("- Magnitude Mínima: %.2f", cfg.MinMagnitude)
	log.Printf("- Bounding Box: Lat [%.4f, %.4f], Lon [%.4f, %.4f]",
		cfg.MinLatitude, cfg.MaxLatitude, cfg.MinLongitude, cfg.MaxLongitude)

	// 1. Inicializar Banco de Dados de Usuários
	log.Println("Carregando banco de dados de usuários...")
	userDB, err := db.NewDatabase("data/users.json")
	if err != nil {
		log.Fatalf("Erro ao inicializar banco de dados: %v", err)
	}

	// 2. Inicializar Servidor Web da Landing Page em Background
	webPort := os.Getenv("PORT")
	if webPort == "" {
		webPort = "8080"
	}
	go server.StartServer(webPort, "web")

	// 3. Inicializar Notificadores e Componentes
	log.Println("Inicializando componentes do monitor...")
	client := usgs.NewClient(cfg.USGSURL)
	flt := filter.NewFilter(cfg)

	var notifiers []notifier.Notifier
	notifiers = append(notifiers, notifier.NewConsoleNotifier())

	// Contexto para encerramento gracioso das goroutines em segundo plano
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if cfg.TelegramToken != "" {
		log.Println("- Notificador do Telegram ativado e conectado ao banco.")
		tgNotifier := notifier.NewTelegramNotifier(cfg.TelegramToken, userDB)
		notifiers = append(notifiers, tgNotifier)

		// Inicia escuta assíncrona de comandos recebidos no Telegram (Long Polling)
		go tgNotifier.StartListener(ctx)
	} else {
		log.Println("- Notificador do Telegram desativado (TELEGRAM_BOT_TOKEN não definido).")
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	// Função auxiliar para executar um ciclo completo de monitoramento
	runCycle := func() {
		log.Println("Buscando terremotos recentes da USGS...")
		feed, err := client.Fetch()
		if err != nil {
			log.Printf("Erro ao buscar dados da USGS: %v", err)
			return
		}

		notifiedCount := 0
		for _, feature := range feed.Features {
			if flt.Evaluate(feature) {
				notifiedCount++
				for _, ntf := range notifiers {
					if err := ntf.Notify(feature); err != nil {
						log.Printf("Erro ao enviar notificação (%T) para %s: %v", ntf, feature.ID, err)
					}
				}
			}
		}

		if notifiedCount > 0 {
			log.Printf("Ciclo concluído. %d novos alertas emitidos para usuários correspondentes.", notifiedCount)
		} else {
			log.Println("Ciclo concluído. Nenhum novo terremoto detectado dentro dos filtros.")
		}

		// Limpa itens antigos do cache (> 24h)
		flt.CleanCache()
	}

	log.Println("Iniciando monitoramento de terremotos...")
	// Executa busca inicial imediata
	runCycle()

	for {
		select {
		case <-ticker.C:
			runCycle()
		case sig := <-sigChan:
			log.Printf("Sinal de terminação recebido (%v). Encerrando monitor de forma limpa...", sig)
			cancel() // Cancela a goroutine de escuta do Telegram
			return
		}
	}
}
