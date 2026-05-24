package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/spf13/viper"
)

type Config struct {
	Site    SiteConfig    `mapstructure:"site" toml:"site"`
	Server  ServerConfig  `mapstructure:"server" toml:"server"`
	PG      PGConfig      `mapstructure:"pg" toml:"pg"`
	Redis   RedisConfig   `mapstructure:"redis" toml:"redis"`
	CMS     CMSConfig     `mapstructure:"cms" toml:"cms"`
	Install InstallConfig `mapstructure:"install" toml:"install"`
}

type SiteConfig struct {
	Name     string `mapstructure:"name" toml:"name"`
	URL      string `mapstructure:"url" toml:"url"`
	Language string `mapstructure:"language" toml:"language"`
	Timezone string `mapstructure:"timezone" toml:"timezone"`
	Theme    string `mapstructure:"theme" toml:"theme"`
}

type ServerConfig struct {
	Host string `mapstructure:"host" toml:"host"`
	Port int    `mapstructure:"port" toml:"port"`
	Mode string `mapstructure:"mode" toml:"mode"`
}

type PGConfig struct {
	User            string `mapstructure:"user" toml:"user"`
	Password        string `mapstructure:"password" toml:"password"`
	Hostname        string `mapstructure:"hostname" toml:"hostname"`
	Port            string `mapstructure:"port" toml:"port"`
	Database        string `mapstructure:"database" toml:"database"`
	Schema          string `mapstructure:"schema" toml:"schema"`
	TablePrefix     string `mapstructure:"table_prefix" toml:"table_prefix"`
	Version         string `mapstructure:"version" toml:"version"`
	MaxOpenConns    int    `mapstructure:"max_open_conns" toml:"max_open_conns"`
	MaxIdleConns    int    `mapstructure:"max_idle_conns" toml:"max_idle_conns"`
	ConnMaxLifetime string `mapstructure:"conn_max_lifetime" toml:"conn_max_lifetime"`
}

type RedisConfig struct {
	Host     string `mapstructure:"host" toml:"host"`
	Port     int    `mapstructure:"port" toml:"port"`
	Password string `mapstructure:"password" toml:"password"`
	DB       int    `mapstructure:"db" toml:"db"`
}

type CMSConfig struct {
	JWTSecret       string   `mapstructure:"jwt_secret" toml:"jwt_secret"`
	JWTExpireHours  int      `mapstructure:"jwt_expire_hours" toml:"jwt_expire_hours"`
	UploadDir       string   `mapstructure:"upload_dir" toml:"upload_dir"`
	UploadMaxSizeMB int      `mapstructure:"upload_max_size_mb" toml:"upload_max_size_mb"`
	APIKeys         []string `mapstructure:"api_keys" toml:"api_keys"`
}

type InstallConfig struct {
	Completed   bool   `mapstructure:"completed" toml:"completed"`
	InstalledAt string `mapstructure:"installed_at" toml:"installed_at"`
}

func Load(path string) (*Config, error) {
	viper.SetConfigFile(path)
	viper.SetConfigType("toml")

	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer file.Close()

	if err := toml.NewEncoder(file).Encode(cfg); err != nil {
		return err
	}

	return os.Chmod(path, 0600)
}
