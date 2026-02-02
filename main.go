package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// --- CONSTANTES ---
const API_TOKEN = "3b8fe35c2885c14c1eaee3248c79472b"

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

// SocieteComResponse : Structure pour lire la réponse brute de Societe.com
type SocieteComResponse struct {
	Denomination        string `json:"denomination"`
	DenominationUsuelle string `json:"denomination_usuelle"`
	Enseigne            string `json:"enseigne"`
	// L'adresse peut être à la racine ou dans un objet imbriqué selon les cas
	Adresse struct {
		CodePostal string `json:"code_postal"`
		Ville      string `json:"ville"`
	} `json:"adresse"`
	Etablissement struct {
		Adresse struct {
			CodePostal string `json:"code_postal"`
			Ville      string `json:"ville"`
		} `json:"adresse"`
	} `json:"etablissement"`
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
		allowedOrigins := []string{
			"http://localhost:8082",
			"https://vintagestandards.fr",
			"https://dev.vintagestandards.fr",
		}

		origin := r.Header.Get("Origin")
		// Autorisation dynamique de l'origine
		for _, allowed := range allowedOrigins {
			if origin == allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				break
			}
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// --- Logique Métier (Le Fetch API) ---

func fetchSocieteComData(siret string) (*EntrepriseResponse, error) {
	// Construction de l'URL
	url := fmt.Sprintf("https://api.societe.com/api/v1/etablissement/%s", siret)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	// Ajout du Token d'authentification
	req.Header.Set("X-Authorization", "socapi "+API_TOKEN)
	req.Header.Set("Accept", "application/json")

	// Exécution de la requête avec timeout
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Gestion des erreurs HTTP
	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("SIRET introuvable")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("erreur API distante : %d", resp.StatusCode)
	}

	// Parsing de la réponse Societe.com
	var apiData SocieteComResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiData); err != nil {
		return nil, err
	}

	// Mapping vers la structure attendue par ton Front
	result := &EntrepriseResponse{}

	// 1. Logique de récupération du nom
	if apiData.Denomination != "" {
		result.Denomination = apiData.Denomination
	} else if apiData.DenominationUsuelle != "" {
		result.Denomination = apiData.DenominationUsuelle
	} else {
		result.Denomination = apiData.Enseigne
	}

	// 2. Logique de récupération de l'adresse (Racine ou Imbriqué)
	if apiData.Adresse.Ville != "" {
		result.AdressePostaleLegale.Ville = apiData.Adresse.Ville
		result.AdressePostaleLegale.CodePostal = apiData.Adresse.CodePostal
	} else {
		result.AdressePostaleLegale.Ville = apiData.Etablissement.Adresse.Ville
		result.AdressePostaleLegale.CodePostal = apiData.Etablissement.Adresse.CodePostal
	}

	return result, nil
}

// --- Handlers ---

// entrepriseHandler : Reçoit le SIRET, appelle Societe.com et renvoie le résultat
func entrepriseHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// 1. Extraction du SIRET depuis l'URL
	siret := strings.TrimPrefix(r.URL.Path, "/api/entreprise/")

	// 2. Validation : Un SIRET doit faire 14 caractères
	if len(siret) != 14 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Le SIRET doit contenir exactement 14 chiffres"})
		return
	}

	log.Printf("🔍 Appel API Societe.com pour le SIRET : %s", siret)

	// 3. APPEL RÉEL (Plus de Mock ici !)
	// On appelle la fonction fetchSocieteComData définie plus haut
	data, err := fetchSocieteComData(siret)

	if err != nil {
		log.Printf("❌ Erreur : %v", err)

		// Gestion fine des erreurs
		if strings.Contains(err.Error(), "introuvable") || err.Error() == "SIRET introuvable" {
			// Cas 404 : L'entreprise n'existe pas
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "Entreprise introuvable pour ce SIRET"})
		} else {
			// Cas 500 : Problème technique (Réseau, Token invalide, API en panne)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "Erreur serveur lors de la communication avec Societe.com"})
		}
		return
	}

	// 4. Succès : On renvoie les vraies données au format JSON
	log.Printf("✅ Données renvoyées pour : %s", data.Denomination)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(data)
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(HealthResponse{Status: "ok", Code: 200})
}

func infoHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	queryType := r.URL.Query().Get("type")
	if queryType == "" {
		queryType = "system"
	}

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