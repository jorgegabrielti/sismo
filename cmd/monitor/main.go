package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"sismo/internal/config"
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

	// 1. Inicializar Servidor Web da Landing Page em background
	webPort := os.Getenv("PORT")
	if webPort == "" {
		webPort = "8080"
	}
	go server.StartServer(webPort, "web")

	// 2. Inicializar componentes do monitor
	log.Println("Inicializando componentes do monitor...")
	client := usgs.NewClient(cfg.USGSURL)
	flt := filter.NewFilter(cfg)

	var notifiers []notifier.Notifier
	notifiers = append(notifiers, notifier.NewConsoleNotifier())

	// Contexto para encerramento gracioso das goroutines em segundo plano
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if cfg.TelegramToken != "" && cfg.TelegramChannelID != "" {
		log.Printf("- Publicador do Telegram ativado → canal: %s", cfg.TelegramChannelID)
		tgNotifier := notifier.NewTelegramNotifier(cfg.TelegramToken, cfg.TelegramChannelID)
		notifiers = append(notifiers, tgNotifier)

		// Inicia o despachador assíncrono de alertas com controle de vazão
		go tgNotifier.StartDispatcher(ctx)
	} else {
		log.Println("- Publicador do Telegram desativado (TELEGRAM_BOT_TOKEN ou TELEGRAM_CHANNEL_ID não definidos).")
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	// Função auxiliar para executar um ciclo completo de monitoramento
	runCycle := func(isStartup bool) {
		log.Println("Buscando terremotos recentes da USGS...")
		feed, err := client.Fetch()
		if err != nil {
			log.Printf("Erro ao buscar dados da USGS: %v", err)
			return
		}

		notifiedCount := 0
		for _, feature := range feed.Features {
			if flt.Evaluate(feature, isStartup) {
				notifiedCount++
				for _, ntf := range notifiers {
					if err := ntf.Notify(feature); err != nil {
						log.Printf("Erro ao enviar notificação (%T) para %s: %v", ntf, feature.ID, err)
					}
				}
			}
		}

		if notifiedCount > 0 {
			log.Printf("Ciclo concluído. %d novos alertas publicados no canal.", notifiedCount)
		} else {
			log.Println("Ciclo concluído. Nenhum novo terremoto detectado dentro dos filtros.")
		}

		// Limpa itens antigos do cache (>24h)
		flt.CleanCache()
	}

	log.Println("Iniciando monitoramento de terremotos...")
	// Executa busca inicial imediata
	runCycle(true)

	for {
		select {
		case <-ticker.C:
			runCycle(false)
		case sig := <-sigChan:
			log.Printf("Sinal de terminação recebido (%v). Encerrando monitor de forma limpa...", sig)
			cancel()
			return
		}
	}
}
