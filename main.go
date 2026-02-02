package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// --- CONSTANTES ---
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

// 2. Ce que Societe.com renvoie (Structure large pour tout capturer)
type SocieteComResponse struct {
	Denomination        string `json:"denomination"`
	DenominationUsuelle string `json:"denomination_usuelle"`
	Enseigne            string `json:"enseigne"`
	
	// Cas 1 : Adresse à la racine
	Adresse struct {
		CodePostal string `json:"code_postal"`
		Ville      string `json:"ville"`
	} `json:"adresse"`
	
	// Cas 2 : Adresse dans un objet etablissement
	Etablissement struct {
		Adresse struct {
			CodePostal string `json:"code_postal"`
			Ville      string `json:"ville"`
		} `json:"adresse"`
	} `json:"etablissement"`
}

// Structures utilitaires
type InfoResponse struct {
	Status  string `json:"status"`
	Data    any    `json:"data,omitempty"`
}

type HealthResponse struct {
	Status string `json:"status"`
	Code   int    `json:"code"`
}

// --- MIDDLEWARES ---

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowedOrigins := []string{
			"http://localhost:8082",
			"https://vintagestandards.fr",
			"https://dev.vintagestandards.fr",
		}

		origin := r.Header.Get("Origin")
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

// --- LOGIQUE MÉTIER (Fetch API) ---

func fetchSocieteComData(siret string) (*EntrepriseResponse, error) {
	// Construction de l'URL
	url := fmt.Sprintf("https://api.societe.com/api/v1/etablissement/%s", siret)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	// Authentification
	req.Header.Set("X-Authorization", "socapi "+API_TOKEN)
	req.Header.Set("Accept", "application/json")

	// Exécution
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// --- SECTION DEBUG : Lecture de la réponse brute ---
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	
	// AFFICHE LE JSON REÇU DANS LE TERMINAL (Pour vérifier si les champs sont vides ou non)
	log.Printf("📢 RAW JSON Societe.com : %s", string(bodyBytes))

	// Gestion des erreurs HTTP
	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("SIRET introuvable")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("erreur API distante : %d", resp.StatusCode)
	}

	// Parsing
	var apiData SocieteComResponse
	// On utilise json.Unmarshal sur les bytes qu'on vient de lire
	if err := json.Unmarshal(bodyBytes, &apiData); err != nil {
		return nil, err
	}

	// Mapping
	result := &EntrepriseResponse{}

	// Priorité nom
	if apiData.Denomination != "" {
		result.Denomination = apiData.Denomination
	} else if apiData.DenominationUsuelle != "" {
		result.Denomination = apiData.DenominationUsuelle
	} else {
		result.Denomination = apiData.Enseigne
	}

	// Priorité adresse (Racine vs Etablissement)
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

	siret := strings.TrimPrefix(r.URL.Path, "/api/entreprise/")

	// Validation 14 chiffres obligatoire pour l'endpoint /etablissement
	if len(siret) != 14 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Le SIRET doit contenir exactement 14 chiffres"})
		return
	}

	log.Printf("🔍 Appel API Societe.com pour le SIRET : %s", siret)

	data, err := fetchSocieteComData(siret)

	if err != nil {
		log.Printf("❌ Erreur : %v", err)
		if strings.Contains(err.Error(), "introuvable") {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "Entreprise introuvable"})
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		}
		return
	}

	log.Printf("✅ Données renvoyées : %s", data.Denomination)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(data)
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(HealthResponse{Status: "ok", Code: 200})
}

func infoHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(InfoResponse{Status: "success", Data: map[string]string{"version": "1.0.0"}})
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
	log.Printf("📍 Route active : GET /api/entreprise/{siret} (14 chiffres)")

	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("Erreur au démarrage: %v", err)
	}
}