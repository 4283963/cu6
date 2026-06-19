package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Server   ServerConfig
	MySQL    MySQLConfig
	Redis    RedisConfig
	Relay    RelayConfig
	Security SecurityConfig
}

type ServerConfig struct {
	Host         string
	Port         int
	Mode         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

type MySQLConfig struct {
	Host            string
	Port            int
	Username        string
	Password        string
	Database        string
	Charset         string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

func (c MySQLConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
		c.Username, c.Password, c.Host, c.Port, c.Database, c.Charset)
}

type RedisConfig struct {
	Host         string
	Port         int
	Password     string
	DB           int
	PoolSize     int
	MinIdleConns int
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

func (c RedisConfig) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

type RelayConfig struct {
	IdempotentTTL    time.Duration
	MaxRetryCount    int
	BaseRetryBackoff time.Duration
	MaxRetryBackoff  time.Duration
	ReplayTTL        time.Duration
	StateLockTTL     time.Duration
}

type SecurityConfig struct {
	TimestampTolerance time.Duration
	APISecret          string
}

func Load() (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			Host:         getEnv("SERVER_HOST", "0.0.0.0"),
			Port:         getEnvInt("SERVER_PORT", 8080),
			Mode:         getEnv("GIN_MODE", "release"),
			ReadTimeout:  getEnvDuration("SERVER_READ_TIMEOUT", 10*time.Second),
			WriteTimeout: getEnvDuration("SERVER_WRITE_TIMEOUT", 10*time.Second),
		},
		MySQL: MySQLConfig{
			Host:            getEnv("MYSQL_HOST", "127.0.0.1"),
			Port:            getEnvInt("MYSQL_PORT", 3306),
			Username:        getEnv("MYSQL_USERNAME", "root"),
			Password:        getEnv("MYSQL_PASSWORD", "root"),
			Database:        getEnv("MYSQL_DATABASE", "privacy_relay"),
			Charset:         getEnv("MYSQL_CHARSET", "utf8mb4"),
			MaxOpenConns:    getEnvInt("MYSQL_MAX_OPEN_CONNS", 100),
			MaxIdleConns:    getEnvInt("MYSQL_MAX_IDLE_CONNS", 20),
			ConnMaxLifetime: getEnvDuration("MYSQL_CONN_MAX_LIFETIME", time.Hour),
		},
		Redis: RedisConfig{
			Host:         getEnv("REDIS_HOST", "127.0.0.1"),
			Port:         getEnvInt("REDIS_PORT", 6379),
			Password:     getEnv("REDIS_PASSWORD", ""),
			DB:           getEnvInt("REDIS_DB", 0),
			PoolSize:     getEnvInt("REDIS_POOL_SIZE", 200),
			MinIdleConns: getEnvInt("REDIS_MIN_IDLE_CONNS", 20),
			DialTimeout:  getEnvDuration("REDIS_DIAL_TIMEOUT", 3*time.Second),
			ReadTimeout:  getEnvDuration("REDIS_READ_TIMEOUT", 500*time.Millisecond),
			WriteTimeout: getEnvDuration("REDIS_WRITE_TIMEOUT", 500*time.Millisecond),
		},
		Relay: RelayConfig{
			IdempotentTTL:    getEnvDuration("RELAY_IDEMPOTENT_TTL", 24*time.Hour),
			MaxRetryCount:    getEnvInt("RELAY_MAX_RETRY_COUNT", 5),
			BaseRetryBackoff: getEnvDuration("RELAY_BASE_RETRY_BACKOFF", 1*time.Second),
			MaxRetryBackoff:  getEnvDuration("RELAY_MAX_RETRY_BACKOFF", 60*time.Second),
			ReplayTTL:        getEnvDuration("RELAY_REPLAY_TTL", 5*time.Minute),
			StateLockTTL:     getEnvDuration("RELAY_STATE_LOCK_TTL", 10*time.Second),
		},
		Security: SecurityConfig{
			TimestampTolerance: getEnvDuration("SECURITY_TIMESTAMP_TOLERANCE", 5*time.Minute),
			APISecret:          getEnv("SECURITY_API_SECRET", "default-secret-change-in-production"),
		},
	}

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value, exists := os.LookupEnv(key); exists {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value, exists := os.LookupEnv(key); exists {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}
