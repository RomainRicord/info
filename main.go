package main

import (
	"encoding/json"
	"fmt"
	"io" // <--- AJOUTÉ : Nécessaire pour lire le corps de la réponse
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
	Siren                string `json:"siren"`
	Siret                string `json:"siret"`
	AdressePostaleLegale struct {
		Ville      string `json:"ville"`
		CodePostal string `json:"code_postal"`
	} `json:"adresse_postale_legale"`
}

// 2. Ce que Societe.com renvoie via l'endpoint /exist
// J'ai ajouté des tags json alternatifs fréquents pour maximiser les chances de mapping
type SocieteExistResponse struct {
	Siren      string `json:"siren"`
	SiretSiege string `json:"siretsiege"` // Vérifie si l'API ne renvoie pas plutôt "siret" ou "siret_siege"
	NumTVA     string `json:"numtva"`
	Deno       string `json:"deno"`       // Vérifie si l'API ne renvoie pas plutôt "denomination"
	Status     string `json:"status"`
	ImmatInsee string `json:"immatinsee"`
}

// Structures utilitaires
type InfoResponse struct {
	Status string `json:"status"`
	Data   any    `json:"data,omitempty"`
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
	// Construction de l'URL
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

	// Lecture du corps de la réponse AVANT de décoder
	// Cela nous permet d'afficher le JSON brut dans les logs
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Gestion des erreurs HTTP
	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("entreprise introuvable (numid invalide)")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("erreur API distante : %d - %s", resp.StatusCode, string(bodyBytes))
	}

	// LOG CRUCIAL : Affiche ce que l'API renvoie vraiment
	log.Printf("DEBUG - Réponse API brute pour %s : %s", numid, string(bodyBytes))

	// Parsing de la réponse /exist depuis les octets lus
	var apiData SocieteExistResponse
	if err := json.Unmarshal(bodyBytes, &apiData); err != nil {
		return nil, fmt.Errorf("erreur de décodage JSON : %v", err)
	}

	// Mapping vers le format Front
	result := &EntrepriseResponse{}

	// On essaie de récupérer la dénomination
	result.Denomination = apiData.Deno
	
	// Si Deno est vide, l'API utilise peut-être un autre champ, regarde les logs !
	if result.Denomination == "" {
		log.Println("⚠️ ATTENTION : La dénomination est vide. Vérifie les tags JSON dans SocieteExistResponse.")
	}

	result.Siren = apiData.Siren
	result.Siret = apiData.SiretSiege

	return result, nil
}

// --- HANDLERS ---

func entrepriseHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extraction du paramètre (SIREN ou SIRET)
	numid := strings.TrimPrefix(r.URL.Path, "/api/entreprise/")

	// Validation
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