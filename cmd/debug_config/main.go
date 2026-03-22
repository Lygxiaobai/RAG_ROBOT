package main

import (
	"fmt"
	"rag_robot/internal/pkg/config"
)

func main() {
	path := config.GetConfigPath()
	cfg, err := config.LoadConfig(path)
	if err != nil {
		fmt.Println("load error:", err)
		return
	}
	fmt.Println("ConfigPath:", path)
	fmt.Println("BaseURL:", cfg.OpenAI.BaseURL)
	fmt.Println("Model:", cfg.OpenAI.Model)
}
