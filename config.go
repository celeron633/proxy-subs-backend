package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
)

type SubsConfig struct {
	Tag      string `json:"tag"`
	FilePath string `json:"file_path"`
	Comment  string `json:"comment"`
}

type SubsServerConfig struct {
	ListenPort         int          `json:"listen_port"`
	ListenHost         string       `json:"listen_host"`
	NeedAuth           bool         `json:"need_auth"`
	EnableApiWhenStart bool         `json:"enable_api_when_start"`
	DebugMode          bool         `json:"debug_mode"`
	SubsConfigs        []SubsConfig `json:"subs_configs"`
}

func (s *SubsServerConfig) SetDefaults() {
	s.ListenPort = 8080
	s.ListenHost = "127.0.0.1"
	s.NeedAuth = true
	s.EnableApiWhenStart = true
	s.DebugMode = true
	s.SubsConfigs = append(s.SubsConfigs, SubsConfig{})
}

func (s *SubsServerConfig) LoadJsonConfig(jsonPath string) error {
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		log.Default().Println("Error loading json config:", err, "path:", jsonPath)
		return err
	}
	err = json.Unmarshal(data, s)
	if err != nil {
		log.Default().Println("Error loading json config:", err, "path:", jsonPath)
		return err
	}

	return nil
}

func (s *SubsServerConfig) ShowConfig() {
	fmt.Printf("SubsServerConfig:\n")
	fmt.Printf("  ListenHost: [%s]\n", s.ListenHost)
	fmt.Printf("  ListenPort: [%d]\n", s.ListenPort)
	fmt.Printf("  NeedAuth: [%t]\n", s.NeedAuth)
	fmt.Printf("  EnableApiWhenStart: [%t]\n", s.EnableApiWhenStart)
	fmt.Printf("  DebugMode: [%t]\n", s.DebugMode)
	fmt.Printf("  SubsConfigs:\n")
	for i, cfg := range s.SubsConfigs {
		fmt.Printf("    SubsConfig #%d:\n", i)
		fmt.Printf("      Tag: [%s]\n", cfg.Tag)
		fmt.Printf("      FilePath: [%s]\n", cfg.FilePath)
		fmt.Printf("      Comment: [%s]\n", cfg.Comment)
	}
}
