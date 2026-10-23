package config

import (
	"fmt"
	"log"
	"os"

	"github.com/ilyakaznacheev/cleanenv"
	"github.com/joho/godotenv"
)

type Config struct {
	HTTP       HTTPConfig
	DB         DBConfig
	App        AppConfig
	Log        LogConfig
	KeyStorage KeyStorageConfig
	Ethereum   EthereumConfig
	Arbitrum   ArbitrumConfig
}

type HTTPConfig struct {
	Host string `env:"HTTP_HOST" env-default:"0.0.0.0"`
	Port int    `env:"HTTP_PORT" env-default:"8080"`
}

func (c HTTPConfig) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

type DBConfig struct {
	Host     string `env:"DB_HOST" env-required:"true"`
	Port     int    `env:"DB_PORT" env-required:"true"`
	User     string `env:"DB_USER" env-required:"true"`
	Password string `env:"DB_PASSWORD" env-required:"true"`
	Name     string `env:"DB_NAME" env-required:"true"`
	SSLMode  string `env:"DB_SSL_MODE" env-required:"true"`
}

func (db DBConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		db.Host, db.Port, db.User, db.Password, db.Name, db.SSLMode,
	)
}

type KeyStorageConfig struct {
	KEK        string `env:"KEY_STORAGE_KEK" env-required:"true"` // hex, 32 bytes
	KEKVersion int32  `env:"KEY_STORAGE_KEK_VERSION" env-default:"1"`
}

type EthereumConfig struct {
	ChainID int64 `env:"ETH_CHAIN_ID" env-required:"true"`
}

type ArbitrumConfig struct {
	ChainID int64 `env:"ARBITRUM_CHAIN_ID" env-required:"true"`
}

type AppConfig struct {
	Environment string `env:"APP_ENVIRONMENT" env-default:"development"`
}

type LogConfig struct {
	Level  string `env:"LOG_LEVEL" env-default:"info"`
	Pretty bool   `env:"LOG_PRETTY" env-default:"false"`
}

const envFile = ".env"

func MustLoad() *Config {
	var cfg Config

	if _, err := os.Stat(envFile); err == nil {
		if err := godotenv.Load(envFile); err != nil {
			log.Fatalf("failed to read %s: %v", envFile, err)
		}
	}

	if err := cleanenv.ReadEnv(&cfg); err != nil {
		log.Fatalf("failed to read config: %v", err)
	}

	return &cfg
}
