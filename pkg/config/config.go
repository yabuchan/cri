package config

import (
	"os"
	"strconv"
)

// GetEnvStr is os.getenv with default value.
// If environment value is not set for key, it returns default value as string.
func GetEnvStr(key string, fallback string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return fallback
	}
	return value
}

// GetEnvInt is os.getenv with default value.
// If environment value is not set for key, it returns default value as integer.
func GetEnvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if len(value) == 0 {
		return fallback
	}
	i, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return i
}

// GetEnvBool is os.getenv with default value.
// If environment value is not set for key, it returns default value as boolean.
func GetEnvBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if len(value) == 0 {
		return fallback
	}
	i, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return i
}
