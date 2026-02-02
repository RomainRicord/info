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

// --- STRUCTURES DE DONNÉES ---

// 1. Ce que ton Front (React) attend
// Note : Comme /exist ne renvoie pas l'adresse, les champs adresse seront vides.
type EntrepriseResponse struct {
	Denomination         string `json:"denomination"`
	Siren                string `json:"siren"`      // Ajouté pour info
	Siret                string `json:"siret"`      // Ajouté pour info
	AdressePostaleLegale struct {
		Ville      string `json:"ville"`
		CodePostal string `json:"code_postal"`
	} `json:"adresse_postale_legale"`
}

// 2. Ce que Societe.com renvoie via l'endpoint /exist
type SocieteExistResponse struct {
	Siren      string `json:"siren"`
	SiretSiege string `json:"siretsiege"`
	NumTVA     string `json:"numtva"`
	Deno       string `json:"deno"`           // La raison sociale
	Status     string `json:"status"`
	ImmatInsee string `json:"immatinsee"`
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

// --- LOGIQUE MÉTIER (Appel /exist) ---

func fetchSocieteExistData(numid string) (*EntrepriseResponse, error) {
	// Construction de l'URL vers l'endpoint /exist
	// numid peut être un SIREN ou un SIRET
	url := fmt.Sprintf("https://api.societe.com/api/v1/entreprise/%s/exist", numid)

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

	// Gestion des erreurs
	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("entreprise introuvable (numid invalide)")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("erreur API distante : %d", resp.StatusCode)
	}

	// Parsing de la réponse /exist
	var apiData SocieteExistResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiData); err != nil {
		return nil, err
	}

	// Mapping vers le format Front
	result := &EntrepriseResponse{}
	
	// On récupère le nom (deno)
	result.Denomination = apiData.Deno
	
	// On renvoie aussi les identifiants si besoin
	result.Siren = apiData.Siren
	result.Siret = apiData.SiretSiege

	// ⚠️ L'endpoint /exist ne donne PAS l'adresse (ville/CP). 
	// On laisse donc AdressePostaleLegale vide ou on met une valeur par défaut si besoin.
	
	return result, nil
}

// --- HANDLERS ---

func entrepriseHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extraction du paramètre (SIREN ou SIRET)
	numid := strings.TrimPrefix(r.URL.Path, "/api/entreprise/")
	
	// Validation : On accepte 9 chiffres (SIREN) OU 14 chiffres (SIRET)
	if len(numid) != 9 && len(numid) != 14 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Le paramètre doit être un SIREN (9 chiffres) ou un SIRET (14 chiffres)",
		})
		return
	}

	log.Printf("🔍 Vérification existence (SIREN/SIRET) : %s", numid)

	// Appel à la fonction /exist
	data, err := fetchSocieteExistData(numid)
	
	if err != nil {
		log.Printf("❌ Erreur : %v", err)
		if strings.Contains(err.Error(), "introuvable") {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "Entreprise inconnue"})
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		}
		return
	}

	log.Printf("✅ Trouvé : %s (SIREN: %s)", data.Denomination, data.Siren)
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
	log.Printf("📍 Route active : GET /api/entreprise/{siren_ou_siret}")

	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("Erreur au démarrage: %v", err)
	}
}