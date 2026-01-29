package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
)

// InfoResponse structure pour les réponses API
type InfoResponse struct {
	Status  string      `json:"status"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// HealthResponse structure pour le health check
type HealthResponse struct {
	Status string `json:"status"`
	Code   int    `json:"code"`
}

// corsMiddleware ajoute les headers CORS test
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowedOrigins := os.Getenv("ALLOWED_ORIGINS")
		if allowedOrigins == "" {
			allowedOrigins = "*"
		}

		w.Header().Set("Access-Control-Allow-Origin", allowedOrigins)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept")
		w.Header().Set("Access-Control-Max-Age", "3600")

		// Gérer les requêtes OPTIONS (preflight)
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// healthHandler endpoint de health check
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	response := HealthResponse{
		Status: "ok",
		Code:   200,
	}
	json.NewEncoder(w).Encode(response)
}

// infoHandler endpoint pour récupérer des infos simples
func infoHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Récupérer un paramètre query optionnel
	queryType := r.URL.Query().Get("type")
	if queryType == "" {
		queryType = "system"
	}

	var data map[string]interface{}

	switch strings.ToLower(queryType) {
	case "system":
		data = map[string]interface{}{
			"hostname":    os.Getenv("HOSTNAME"),
			"port":        os.Getenv("PORT"),
			"environment": os.Getenv("ENVIRONMENT"),
			"version":     "1.0.0",
		}
	case "timestamp":
		data = map[string]interface{}{
			"timestamp": getCurrentTimestamp(),
		}
	default:
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(InfoResponse{
			Status: "error",
			Error:  "Type de requête non supporté",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(InfoResponse{
		Status:  "success",
		Message: "Infos récupérées avec succès",
		Data:    data,
	})
}

// getCurrentTimestamp retourne un timestamp formaté
func getCurrentTimestamp() string {
	// Implémentation simple - à adapter selon vos besoins
	return "2026-01-29T00:00:00Z"
}

// errorHandler gère les routes non trouvées
func errorHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(InfoResponse{
		Status: "error",
		Error:  "Route non trouvée",
	})
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8091"
	}

	// Créer le router avec CORS middleware
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/info", infoHandler)
	mux.HandleFunc("/", errorHandler)

	handler := corsMiddleware(mux)

	log.Printf("🚀 Serveur démarré sur :%s", port)
	log.Printf("📍 GET /health - Vérifier l'état du serveur")
	log.Printf("📍 GET /info?type=system - Récupérer les infos système")
	log.Printf("📍 GET /info?type=timestamp - Récupérer le timestamp")

	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("Erreur au démarrage du serveur: %v", err)
	}
}
