package config

import (
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

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
	OldKEKs    string `env:"KEY_STORAGE_OLD_KEKS"` // version:hex,...: decrypt keys until rewrap-keys moves them to current KEK
}

func (c KeyStorageConfig) KEKs() (map[int32][]byte, error) {
	keks := map[int32][]byte{}
	add := func(version int32, kekHex string) error {
		kek, err := hex.DecodeString(kekHex)
		if err != nil || len(kek) != 32 {
			return fmt.Errorf("kek version %d must be 32 bytes in hex", version)
		}

		if _, ok := keks[version]; ok || version <= 0 {
			return fmt.Errorf("kek version %d is duplicate or not positive", version)
		}

		keks[version] = kek
		return nil
	}

	if err := add(c.KEKVersion, c.KEK); err != nil {
		return nil, err
	}

	if c.OldKEKs == "" {
		return keks, nil
	}

	for _, item := range strings.Split(c.OldKEKs, ",") {
		versionStr, kekHex, ok := strings.Cut(strings.TrimSpace(item), ":")
		version, err := strconv.ParseInt(versionStr, 10, 32)
		if !ok || err != nil {
			return nil, fmt.Errorf("KEY_STORAGE_OLD_KEKS: want version:hex, got %q", versionStr)
		}

		if err := add(int32(version), kekHex); err != nil {
			return nil, err
		}
	}

	return keks, nil
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
