package config

import (
	"encoding/json"
	"os"
)

type Config struct {
	Server struct {
		Host   string `json:"host"`
		Port   int    `json:"port"`
		APIKey string `json:"api_key"`
	} `json:"server"`
	Fshare struct {
		AppKey    string `json:"app_key"`
		UserAgent string `json:"user_agent"`
		User      string `json:"user"`
		Password  string `json:"password"`
	} `json:"fshare"`
	Aria2 struct {
		Secret   string `json:"secret"`
		MovieRPC string `json:"movie_rpc"`
		TVRPC    string `json:"tv_rpc"`
	} `json:"aria2"`
	Arr struct {
		SonarrURL      string `json:"sonarr_url"`
		SonarrAPIKey   string `json:"sonarr_api_key"`
		RadarrURL      string `json:"radarr_url"`
		RadarrAPIKey   string `json:"radarr_api_key"`
		ProwlarrURL    string `json:"prowlarr_url"`
		ProwlarrAPIKey string `json:"prowlarr_api_key"`
	} `json:"arr"`
	Storage struct {
		TVBaseDir          string `json:"tv_base_dir"`
		MovieBaseDir       string `json:"movie_base_dir"`
		DefaultDownloadDir string `json:"default_download_dir"`
	} `json:"storage"`
}

func LoadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var cfg Config
	if err := json.NewDecoder(file).Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
