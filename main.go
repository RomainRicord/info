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
// Ton token API Societe.com
const API_TOKEN = "3b8fe35c2885c14c1eaee3248c79472b"

// --- STRUCTURES DE DONNÉES ---

// 1. Ce que ton Front (React) attend
type EntrepriseResponse struct {
	Denomination         string `json:"denomination"`
	NomComplet           string `json:"nom_complet,omitempty"`
	AdressePostaleLegale struct {
		Ville      string `json:"ville"`
		CodePostal string `json:"code_postal"`
	} `json:"adresse_postale_legale"`
}

// 2. Ce que Societe.com renvoie (Structure partielle pour cibler ce qu'on veut)
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

// Structures utilitaires existantes
type InfoResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
}

type HealthResponse struct {
	Status string `json:"status"`
	Code   int    `json:"code"`
}

// --- MIDDLEWARES ---

// CORS corrigé : Gère correctement la liste des origines
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Liste des domaines autorisés
		allowedOrigins := []string{
			"http://localhost:8082",
			"https://vintagestandards.fr",
			"https://dev.vintagestandards.fr",
		}

		origin := r.Header.Get("Origin")
		// On vérifie si l'origine est dans la liste
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

// --- LOGIQUE MÉTIER (Appel Externe) ---

func fetchSocieteComData(siret string) (*EntrepriseResponse, error) {
	// Construction de l'URL vers l'API Societe.com
	url := fmt.Sprintf("https://api.societe.com/api/v1/etablissement/%s", siret)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	// Ajout du Token d'authentification
	req.Header.Set("X-Authorization", "socapi "+API_TOKEN)
	req.Header.Set("Accept", "application/json")

	// Exécution de la requête avec un timeout de 10s
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Gestion des codes erreurs
	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("SIRET introuvable")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("erreur API distante : %d", resp.StatusCode)
	}

	// Parsing du JSON reçu de Societe.com
	var apiData SocieteComResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiData); err != nil {
		return nil, err
	}

	// Mapping des données (On transforme le format Societe.com vers ton format React)
	result := &EntrepriseResponse{}

	// 1. Récupération du nom (On prend le premier qui n'est pas vide)
	if apiData.Denomination != "" {
		result.Denomination = apiData.Denomination
	} else if apiData.DenominationUsuelle != "" {
		result.Denomination = apiData.DenominationUsuelle
	} else {
		result.Denomination = apiData.Enseigne
	}

	// 2. Récupération de l'adresse (Racine ou Etablissement)
	if apiData.Adresse.Ville != "" {
		result.AdressePostaleLegale.Ville = apiData.Adresse.Ville
		result.AdressePostaleLegale.CodePostal = apiData.Adresse.CodePostal
	} else {
		result.AdressePostaleLegale.Ville = apiData.Etablissement.Adresse.Ville
		result.AdressePostaleLegale.CodePostal = apiData.Etablissement.Adresse.CodePostal
	}

	return result, nil
}

// --- HANDLERS ---

func entrepriseHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extraction du SIRET
	siret := strings.TrimPrefix(r.URL.Path, "/api/entreprise/")
	
	// Validation simple de la longueur
	if len(siret) != 14 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Le SIRET doit contenir 14 chiffres"})
		return
	}

	log.Printf("🔍 Appel API Societe.com pour le SIRET : %s", siret)

	// APPEL RÉEL À L'API
	data, err := fetchSocieteComData(siret)
	
	if err != nil {
		log.Printf("❌ Erreur : %v", err)
		
		// Si l'entreprise n'existe pas -> 404
		if err.Error() == "SIRET introuvable" {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "Entreprise non trouvée sur Societe.com"})
		} else {
			// Autre erreur (réseau, token, serveur externe HS) -> 500
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		}
		return
	}

	// Succès
	log.Printf("✅ Données trouvées : %s - %s", data.Denomination, data.AdressePostaleLegale.Ville)
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
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/info", infoHandler)
	mux.HandleFunc("/api/entreprise/", entrepriseHandler)

	handler := corsMiddleware(mux)

	log.Printf("🚀 Serveur démarré sur :%s", port)
	log.Printf("📍 Route active : GET /api/entreprise/{siret}")

	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("Erreur au démarrage: %v", err)
	}
}