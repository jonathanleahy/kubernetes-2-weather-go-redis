// main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/gorilla/mux"
)

// WeatherData represents the weather information for a location
type WeatherData struct {
	Temperature float64 `json:"temperature"`
	Humidity    float64 `json:"humidity"`
	WindSpeed   float64 `json:"windSpeed"`
	Description string  `json:"description"`
	Location    string  `json:"location"`
	Timestamp   string  `json:"timestamp"`
}

// Config holds the application configuration
type Config struct {
	WeatherAPIKey string
	RedisHost     string
	RedisPort     string
	Port          string
	Environment   string
}

var (
	redisClient *redis.Client
	config      Config
)

func init() {
	// Add debug logging
	log.Printf("Starting application initialization...")

	// Load configuration from environment variables
	config = Config{
		WeatherAPIKey: os.Getenv("WEATHER_API_KEY"),
		RedisHost:     os.Getenv("REDIS_HOST"),
		RedisPort:     os.Getenv("REDIS_PORT"),
		Port:          os.Getenv("PORT"),
		Environment:   os.Getenv("ENVIRONMENT"),
	}

	// Log configuration (excluding sensitive data)
	log.Printf("Configuration loaded:")
	log.Printf("REDIS_HOST: %s", config.RedisHost)
	log.Printf("REDIS_PORT: %s", config.RedisPort)
	log.Printf("PORT: %s", config.Port)
	log.Printf("ENVIRONMENT: %s", config.Environment)
	log.Printf("WEATHER_API_KEY length: %d", len(config.WeatherAPIKey))

	// Set default values
	if config.RedisHost == "" {
		config.RedisHost = "localhost"
		log.Printf("Using default Redis host: localhost")
	}
	if config.RedisPort == "" {
		config.RedisPort = "6379"
		log.Printf("Using default Redis port: 6379")
	}
	if config.Port == "" {
		config.Port = "8080"
		log.Printf("Using default port: 8080")
	}
	if config.Environment == "" {
		config.Environment = "development"
		log.Printf("Using default environment: development")
	}

	log.Printf("Connecting to Redis at %s:%s...", config.RedisHost, config.RedisPort)

	// Initialize Redis client with retry logic
	redisClient = redis.NewClient(&redis.Options{
		Addr:            fmt.Sprintf("%s:%s", config.RedisHost, config.RedisPort),
		Password:        "",
		DB:              0,
		MaxRetries:      5,
		MinRetryBackoff: time.Second,
		MaxRetryBackoff: time.Second * 5,
	})

	// Test Redis connection with retry
	ctx := context.Background()
	var err error
	for i := 0; i < 5; i++ {
		_, err = redisClient.Ping(ctx).Result()
		if err == nil {
			log.Printf("Successfully connected to Redis")
			break
		}
		log.Printf("Attempt %d: Failed to connect to Redis: %v", i+1, err)
		time.Sleep(time.Second * time.Duration(i+1))
	}
	if err != nil {
		log.Printf("Warning: Could not establish initial Redis connection: %v", err)
	}
}

func main() {
	r := mux.NewRouter()

	// API routes
	api := r.PathPrefix("/api").Subrouter()
	api.HandleFunc("/weather/{location}", getWeatherHandler).Methods("GET")
	api.HandleFunc("/cache/stats", getRedisCacheStats).Methods("GET")
	api.HandleFunc("/cache/{key}", getRedisKey).Methods("GET")
	api.HandleFunc("/cache", listRedisKeys).Methods("GET")
	api.HandleFunc("/health", healthCheckHandler).Methods("GET")

	// CORS middleware
	r.Use(corsMiddleware)

	// Start server
	log.Printf("Server starting on port %s in %s mode", config.Port, config.Environment)
	log.Fatal(http.ListenAndServe(":"+config.Port, r))
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func getWeatherHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	location := vars["location"]

	log.Printf("Getting weather data for location: %s", location)

	// Try to get cached data
	ctx := r.Context()
	cachedData, err := redisClient.Get(ctx, location).Result()
	if err == nil {
		log.Printf("Cache hit for location: %s", location)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(cachedData))
		return
	}
	log.Printf("Cache miss for location: %s, error: %v", location, err)

	// If not in cache, fetch from weather API
	weatherData, err := fetchWeatherData(location)
	if err != nil {
		log.Printf("Error fetching weather data for %s: %v", location, err)
		http.Error(w, "Error fetching weather data", http.StatusInternalServerError)
		return
	}

	// Cache the result
	jsonData, err := json.Marshal(weatherData)
	if err != nil {
		log.Printf("Error marshaling weather data: %v", err)
		http.Error(w, "Error processing weather data", http.StatusInternalServerError)
		return
	}

	// Set cache with 5-minute expiration
	err = redisClient.Set(ctx, location, jsonData, 5*time.Minute).Err()
	if err != nil {
		log.Printf("Error caching weather data: %v", err)
		// Continue even if caching fails
	} else {
		log.Printf("Successfully cached weather data for: %s", location)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonData)
}

func fetchWeatherData(location string) (*WeatherData, error) {
	// Mock implementation - replace with actual API call
	// Using random variations for demo purposes
	return &WeatherData{
		Temperature: 22.5 + float64(time.Now().UnixNano()%5),
		Humidity:    65.0 + float64(time.Now().UnixNano()%10),
		WindSpeed:   12.0 + float64(time.Now().UnixNano()%8),
		Description: "Partly cloudy",
		Location:    location,
		Timestamp:   time.Now().Format(time.RFC3339),
	}, nil
}

func getRedisCacheStats(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	stats := struct {
		TotalKeys       int                    `json:"totalKeys"`
		KeysWithTTL     int                    `json:"keysWithTTL"`
		CachedLocations []string               `json:"cachedLocations"`
		Data            map[string]WeatherData `json:"data"`
	}{
		Data: make(map[string]WeatherData),
	}

	// Get all keys
	iter := redisClient.Scan(ctx, 0, "*", 0).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		stats.TotalKeys++

		// Get TTL for the key
		ttl, err := redisClient.TTL(ctx, key).Result()
		if err == nil && ttl.Seconds() > 0 {
			stats.KeysWithTTL++
		}

		// Get the actual data
		val, err := redisClient.Get(ctx, key).Result()
		if err == nil {
			var weatherData WeatherData
			if err := json.Unmarshal([]byte(val), &weatherData); err == nil {
				stats.Data[key] = weatherData
				stats.CachedLocations = append(stats.CachedLocations, weatherData.Location)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func getRedisKey(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key := vars["key"]
	ctx := context.Background()

	val, err := redisClient.Get(ctx, key).Result()
	if err != nil {
		http.Error(w, "Key not found", http.StatusNotFound)
		return
	}

	var weatherData WeatherData
	if err := json.Unmarshal([]byte(val), &weatherData); err != nil {
		http.Error(w, "Invalid data format", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(weatherData)
}

func listRedisKeys(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	var keys []string

	iter := redisClient.Scan(ctx, 0, "*", 0).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(keys)
}

func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("Health check request received")

	ctx := context.Background()
	status := "healthy"
	redisStatus := "connected"

	// Check Redis connection
	_, err := redisClient.Ping(ctx).Result()
	if err != nil {
		log.Printf("Health check Redis ping failed: %v", err)
		status = "degraded"
		redisStatus = "disconnected"
	}

	response := map[string]string{
		"status":      status,
		"redis":       redisStatus,
		"environment": config.Environment,
		"version":     "1.0.0",
	}

	w.Header().Set("Content-Type", "application/json")
	if status != "healthy" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	json.NewEncoder(w).Encode(response)
	log.Printf("Health check completed with status: %s", status)
}
