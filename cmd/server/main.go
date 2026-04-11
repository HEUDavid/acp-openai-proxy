package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/yourname/acp-openai-proxy/internal/api"
	"github.com/yourname/acp-openai-proxy/internal/backend"
	"github.com/yourname/acp-openai-proxy/internal/backend/gemini"
	"github.com/yourname/acp-openai-proxy/internal/config"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to config file")
	flag.Parse()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Printf("[Warning] Failed to load config from %s: %v. Using defaults.", *configPath, err)
		cfg = &config.Config{}
	}

	port := cfg.Server.Port
	log.Printf("Starting Gemini OpenAI Bridge on port %d...", port)

	registry := backend.NewRegistry()
	manager := gemini.NewManager()
	registry.Register(gemini.NewGeminiBackend(manager))

	if err == nil && cfg != nil && cfg.Backends.Gemini.Enabled {
		for _, m := range cfg.Backends.Gemini.PreloadModels {
			log.Printf("Preloading model process: %s ...", m)
			go func(modelName string) {
				_, e := manager.GetWorker(modelName)
				if e != nil {
					log.Printf("[Error] Failed to preload worker for %s: %v", modelName, e)
				} else {
					log.Printf("[Success] Model %s is preloaded and ready.", modelName)
				}
			}(m)
		}
	}

	router := api.SetupRouter(registry, cfg.Server.APIKey)
	if err := router.Run(fmt.Sprintf(":%d", port)); err != nil {
		log.Fatalf("Server exited with error: %v", err)
	}
}
