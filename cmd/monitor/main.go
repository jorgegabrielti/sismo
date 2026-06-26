package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"sismo/internal/config"
	"sismo/internal/filter"
	"sismo/internal/notifier"
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

	log.Println("Inicializando componentes do sistema...")
	client := usgs.NewClient(cfg.USGSURL)
	flt := filter.NewFilter(cfg)

	var notifiers []notifier.Notifier
	notifiers = append(notifiers, notifier.NewConsoleNotifier())

	if cfg.TelegramToken != "" && cfg.TelegramChatID != "" {
		log.Println("- Notificador do Telegram ativado e configurado.")
		notifiers = append(notifiers, notifier.NewTelegramNotifier(cfg.TelegramToken, cfg.TelegramChatID))
	} else {
		log.Println("- Notificador do Telegram desativado (TELEGRAM_BOT_TOKEN e TELEGRAM_CHAT_ID não definidos).")
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
			log.Printf("Ciclo concluído. %d novos alertas emitidos.", notifiedCount)
		} else {
			log.Println("Ciclo concluído. Nenhum novo terremoto detectado dentro dos filtros.")
		}

		// Limpa itens antigos do cache (> 24h)
		flt.CleanCache()
	}

	log.Println("Iniciando monitoramento de terremotos...")
	// Executa uma busca inicial imediatamente ao iniciar o monitor
	runCycle()

	for {
		select {
		case <-ticker.C:
			runCycle()
		case sig := <-sigChan:
			log.Printf("Sinal de terminação recebido (%v). Encerrando monitor de forma limpa...", sig)
			return
		}
	}
}
