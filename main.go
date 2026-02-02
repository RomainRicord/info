package main

import (
	"bytes"
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
// Ton token est intégré ici pour simplifier le déploiement
const API_TOKEN = "3b8fe35c2885c14c1eaee3248c79472b"

// --- 1. Structures de réponse (Vers le Frontend React) ---
type EntrepriseResponse struct {
	Denomination         string `json:"denomination"`
	NomComplet           string `json:"nom_complet,omitempty"`
	AdressePostaleLegale struct {
		Ville      string `json:"ville"`
		CodePostal string `json:"code_postal"`
	} `json:"adresse_postale_legale"`
}

// --- 2. Structures de réception (Depuis Societe.com) ---
type SocieteComResponse struct {
	Denomination        string `json:"denomination"`
	DenominationUsuelle string `json:"denomination_usuelle"`
	NomCommercial       string `json:"nom_commercial"`
	Enseigne            string `json:"enseigne"`
	// Societe.com met parfois l'adresse dans un objet imbriqué
	Etablissement struct {
		Adresse struct {
			CodePostal string `json:"code_postal"`
			Ville      string `json:"ville"`
		} `json:"adresse"`
	} `json:"etablissement"`
	// Ou parfois directement à la racine selon l'endpoint
	Adresse struct {
		CodePostal string `json:"code_postal"`
		Ville      string `json:"ville"`
	} `json:"adresse"`
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

// --- Middlewares (CORS) ---
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

// --- Logique Métier : Appel Societe.com ---
func fetchSocieteComData(siret string) (*EntrepriseResponse, error) {
	// 1. Construction de l'URL
	// On utilise l'endpoint /etablissement car on a un SIRET
	url := fmt.Sprintf("https://api.societe.com/api/v1/etablissement/%s", siret)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	// 2. Authentification avec ton Token
	req.Header.Set("X-Authorization", "socapi "+API_TOKEN)
	req.Header.Set("Accept", "application/json")

	// 3. Exécution de la requête
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 4. Lecture du corps de la réponse (Pour debug et parsing)
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// DEBUG : Affiche ce que Societe.com renvoie vraiment dans la console
	// log.Printf("📝 Réponse brute Societe.com : %s", string(bodyBytes))

	// 5. Gestion des erreurs HTTP
	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("entreprise introuvable sur Societe.com")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("erreur API (%d): %s", resp.StatusCode, string(bodyBytes))
	}

	// 6. Parsing du JSON
	var apiData SocieteComResponse
	if err := json.NewDecoder(bytes.NewReader(bodyBytes)).Decode(&apiData); err != nil {
		return nil, fmt.Errorf("erreur parsing JSON: %v", err)
	}

	// 7. Mapping vers NOTRE structure (Adaptateur)
	result := &EntrepriseResponse{}

	// Logique de récupération du nom (ordre de priorité)
	if apiData.Denomination != "" {
		result.Denomination = apiData.Denomination
	} else if apiData.DenominationUsuelle != "" {
		result.Denomination = apiData.DenominationUsuelle
	} else if apiData.Enseigne != "" {
		result.Denomination = apiData.Enseigne
	} else {
		result.Denomination = "Nom Inconnu"
	}

	// Logique de récupération de l'adresse
	// On essaie d'abord à la racine, sinon dans l'objet etablissement
	if apiData.Adresse.Ville != "" {
		result.AdressePostaleLegale.Ville = apiData.Adresse.Ville
		result.AdressePostaleLegale.CodePostal = apiData.Adresse.CodePostal
	} else if apiData.Etablissement.Adresse.Ville != "" {
		result.AdressePostaleLegale.Ville = apiData.Etablissement.Adresse.Ville
		result.AdressePostaleLegale.CodePostal = apiData.Etablissement.Adresse.CodePostal
	}

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

	log.Printf("🔍 Recherche SIRET : %s", siret)

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
	log.Printf("✅ Succès : %s (%s)", data.Denomination, data.AdressePostaleLegale.Ville)
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

	log.Printf("🚀 Serveur Proxy (avec Token Societe.com) démarré sur :%s", port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("Erreur au démarrage: %v", err)
	}
}