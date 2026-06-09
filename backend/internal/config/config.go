package config

import (
	"os"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type Config struct {
	GRPCPort        int           `mapstructure:"grpc_port"`
	HTTPPort        int           `mapstructure:"http_port"`
	DatabaseURL     string        `mapstructure:"database_url"`
	LogLevel        string        `mapstructure:"log_level"`
	RetentionDays   int           `mapstructure:"retention_days"`
	OrphanTimeout   time.Duration `mapstructure:"orphan_timeout"`
	TraceComplete   time.Duration `mapstructure:"trace_complete_timeout"`
	MaxSpansPerTrace int          `mapstructure:"max_spans_per_trace"`
	MaxBatchSize    int           `mapstructure:"max_batch_size"`
	WindowUpdateInterval time.Duration `mapstructure:"window_update_interval"`
	TopologyInactiveHours int     `mapstructure:"topology_inactive_hours"`
	Sampling        SamplingConfig `mapstructure:"sampling"`
}

type SamplingConfig struct {
	HeadRate         float64 `mapstructure:"head_rate"`
	TailNormalRate   float64 `mapstructure:"tail_normal_rate"`
	TailAnomalyRate  float64 `mapstructure:"tail_anomaly_rate"`
}

var AppConfig *Config

func Load() {
	viper.SetDefault("grpc_port", 4317)
	viper.SetDefault("http_port", 8080)
	viper.SetDefault("database_url", "postgres://postgres:postgres@postgres:5432/trace?sslmode=disable")
	viper.SetDefault("log_level", "info")
	viper.SetDefault("retention_days", 30)
	viper.SetDefault("orphan_timeout", 30*time.Second)
	viper.SetDefault("trace_complete_timeout", 30*time.Second)
	viper.SetDefault("max_spans_per_trace", 500)
	viper.SetDefault("max_batch_size", 1000)
	viper.SetDefault("window_update_interval", 1*time.Minute)
	viper.SetDefault("topology_inactive_hours", 24)
	viper.SetDefault("sampling.head_rate", 1.0)
	viper.SetDefault("sampling.tail_normal_rate", 0.1)
	viper.SetDefault("sampling.tail_anomaly_rate", 1.0)

	viper.AutomaticEnv()
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("/etc/trace-topo")

	if err := viper.ReadInConfig(); err != nil {
		logrus.Warnf("Config file not found, using environment variables and defaults: %v", err)
	}

	AppConfig = &Config{}
	if err := viper.Unmarshal(AppConfig); err != nil {
		logrus.Fatalf("Failed to unmarshal config: %v", err)
	}

	if env := os.Getenv("GRPC_PORT"); env != "" {
		AppConfig.GRPCPort, _ = strconv.Atoi(env)
	}
	if env := os.Getenv("HTTP_PORT"); env != "" {
		AppConfig.HTTPPort, _ = strconv.Atoi(env)
	}
	if env := os.Getenv("DATABASE_URL"); env != "" {
		AppConfig.DatabaseURL = env
	}
	if env := os.Getenv("RETENTION_DAYS"); env != "" {
		AppConfig.RetentionDays, _ = strconv.Atoi(env)
	}

	level, err := logrus.ParseLevel(AppConfig.LogLevel)
	if err != nil {
		level = logrus.InfoLevel
	}
	logrus.SetLevel(level)
	logrus.SetFormatter(&logrus.JSONFormatter{})

	logrus.Infof("Configuration loaded: %+v", AppConfig)
}
