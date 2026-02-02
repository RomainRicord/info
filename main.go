package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
)

// --- Structures de Données ---

// EntrepriseResponse : Structure exacte attendue par ton TypeScript
type EntrepriseResponse struct {
	Denomination         string `json:"denomination"`
	NomComplet           string `json:"nom_complet,omitempty"` // Fallback
	AdressePostaleLegale struct {
		Ville      string `json:"ville"`
		CodePostal string `json:"code_postal"`
	} `json:"adresse_postale_legale"`
}

// InfoResponse : Structure existante pour /info
type InfoResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
}

// HealthResponse : Structure existante pour /health
type HealthResponse struct {
	Status string `json:"status"`
	Code   int    `json:"code"`
}

// --- Middlewares ---

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowedOrigins := "http://localhost:8082, https://vintagestandards.fr, https://dev.vintagestandards.fr"
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigins)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- Handlers ---

// entrepriseHandler : Reçoit le SIRET et renvoie les infos de l'entreprise
func entrepriseHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extraction du SIRET depuis l'URL : /api/entreprise/123456789
	// On retire le préfixe pour garder juste le numéro
	siret := strings.TrimPrefix(r.URL.Path, "/api/entreprise/")
	
	if siret == "" || siret == "/" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "SIRET manquant"})
		return
	}

	log.Printf("🔍 Recherche demandée pour le SIRET : %s", siret)

	// --- SIMULATION DE DONNÉES (MOCK) ---
	// C'est ici que tu appelleras plus tard l'API INSEE réelle.
	// Pour l'instant, on renvoie une réponse statique pour tester la connexion.
	
	response := EntrepriseResponse{
		Denomination: "VINTAGE STANDARDS STUDIO",
		AdressePostaleLegale: struct {
			Ville      string `json:"ville"`
			CodePostal string `json:"code_postal"`
		}{
			Ville:      "PARIS",
			CodePostal: "75011",
		},
	}

	// On encode la réponse en JSON pour le TypeScript
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(HealthResponse{Status: "ok", Code: 200})
}

func infoHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	queryType := r.URL.Query().Get("type")
	if queryType == "" { queryType = "system" }

	var data map[string]any
	switch strings.ToLower(queryType) {
	case "system":
		data = map[string]any{"version": "1.0.0", "env": os.Getenv("ENVIRONMENT")}
	case "timestamp":
		data = map[string]any{"timestamp": "2026-02-02T12:00:00Z"}
	default:
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(InfoResponse{Status: "error", Error: "Type inconnu"})
		return
	}
	json.NewEncoder(w).Encode(InfoResponse{Status: "success", Data: data})
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8091"
	}

	mux := http.NewServeMux()
	
	// Routes existantes
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/info", infoHandler)
	
	// NOUVELLE ROUTE : Note le "/" à la fin pour capturer ce qui suit (le siret)
	mux.HandleFunc("/api/entreprise/", entrepriseHandler)

	handler := corsMiddleware(mux)

	log.Printf("🚀 Serveur démarré sur :%s", port)
	log.Printf("📍 Route active : GET /api/entreprise/{siret}")

	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("Erreur au démarrage: %v", err)
	}
}