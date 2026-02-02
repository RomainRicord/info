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

// --- 1. Structures de réponse (Vers le Frontend) ---
// C'est ce que ton React attend
type EntrepriseResponse struct {
	Denomination         string `json:"denomination"`
	NomComplet           string `json:"nom_complet,omitempty"`
	AdressePostaleLegale struct {
		Ville      string `json:"ville"`
		CodePostal string `json:"code_postal"`
	} `json:"adresse_postale_legale"`
}

// --- 2. Structures de réception (Depuis Societe.com) ---
// C'est (approximativement) ce que Societe.com renvoie.
// ⚠️ Note : La structure exacte dépend de leur documentation précise pour l'endpoint /etablissement.
// J'ai mis ici les champs standards.
type SocieteComResponse struct {
	DenominationUsuelle string `json:"denomination_usuelle"`
	Enseigne            string `json:"enseigne"`
	NomCommercial       string `json:"nom_commercial"`
	Adresse             struct {
		CodePostal string `json:"code_postal"`
		Ville      string `json:"ville"`
	} `json:"adresse"`
	// Parfois le nom est directement à la racine ou dans un objet identite
	Denomination string `json:"denomination"` 
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

// --- Middlewares (CORS) ---
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

// --- Logique Métier : Appel Societe.com ---
func fetchSocieteComData(siret string) (*EntrepriseResponse, error) {
	// 1. Récupération du Token (Variable d'environnement)
	apiToken := os.Getenv("SOCIETE_API_TOKEN")
	if apiToken == "" {
		return nil, fmt.Errorf("token API Societe.com manquant dans les variables d'environnement")
	}

	// 2. Construction de la requête
	// On utilise l'endpoint /etablissement car on a un SIRET (14 chiffres)
	url := fmt.Sprintf("https://api.societe.com/api/v1/etablissement/%s", siret)
	
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	// 3. Authentification via Header (Recommandé par la doc)
	req.Header.Set("X-Authorization", "socapi "+apiToken)
	req.Header.Set("Accept", "application/json")

	// 4. Exécution
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 5. Gestion des erreurs HTTP de l'API externe
	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("entreprise introuvable sur Societe.com")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("erreur API Societe.com: %d", resp.StatusCode)
	}

	// 6. Parsing de la réponse brute de Societe.com
	var apiData SocieteComResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiData); err != nil {
		return nil, err
	}

	// 7. Mapping vers NOTRE structure (Adaptateur)
	result := &EntrepriseResponse{}
	
	// Logique de choix du nom (Societe.com a plusieurs champs pour le nom)
	if apiData.Denomination != "" {
		result.Denomination = apiData.Denomination
	} else if apiData.DenominationUsuelle != "" {
		result.Denomination = apiData.DenominationUsuelle
	} else if apiData.Enseigne != "" {
		result.Denomination = apiData.Enseigne
	} else {
		result.Denomination = "Nom inconnu"
	}

	// Remplissage Adresse
	result.AdressePostaleLegale.Ville = apiData.Adresse.Ville
	result.AdressePostaleLegale.CodePostal = apiData.Adresse.CodePostal

	return result, nil
}

// --- Handler Principal ---
func entrepriseHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	siret := strings.TrimPrefix(r.URL.Path, "/api/entreprise/")
	
	// Validation basique
	if len(siret) != 14 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Le SIRET doit contenir 14 chiffres"})
		return
	}

	log.Printf("🔍 Appel API Societe.com pour le SIRET : %s", siret)

	// APPEL RÉEL
	data, err := fetchSocieteComData(siret)
	
	if err != nil {
		log.Printf("❌ Erreur : %v", err)
		if strings.Contains(err.Error(), "introuvable") {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "Entreprise non trouvée"})
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		}
		return
	}

	// Succès
	log.Printf("✅ Données trouvées : %s (%s)", data.Denomination, data.AdressePostaleLegale.Ville)
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
	if port == "" { port = "8091" }

	// ⚠️ IMPORTANT : Vérifie que le token est présent au démarrage
	if os.Getenv("SOCIETE_API_TOKEN") == "3b8fe35c2885c14c1eaee3248c79472b" {
		log.Println("⚠️ ATTENTION : La variable SOCIETE_API_TOKEN n'est pas définie. Les appels API échoueront.")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/info", infoHandler)
	mux.HandleFunc("/api/entreprise/", entrepriseHandler)

	handler := corsMiddleware(mux)

	log.Printf("🚀 Serveur Proxy démarré sur :%s", port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("Erreur au démarrage: %v", err)
	}
}