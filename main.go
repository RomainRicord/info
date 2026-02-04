package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/smtp"
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
	Tva                  string `json:"tva"` // <--- AJOUTE CETTE LIGNE
	AdressePostaleLegale struct {
		Ville      string `json:"ville"`
		CodePostal string `json:"code_postal"`
	} `json:"adresse_postale_legale"`
}

// 2. Ce que Societe.com renvoie via l'endpoint /exist
// CORRECTION ICI : On ajoute le niveau "common"
type SocieteExistResponse struct {
	Common struct {
		Siren      string `json:"siren"`
		SiretSiege string `json:"siretsiege"`
		NumTVA     string `json:"numtva"`
		Deno       string `json:"deno"`
		Status     string `json:"status"`
		ImmatInsee string `json:"immatinsee"`
	} `json:"common"`
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

type EmailRequest struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
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
	url := fmt.Sprintf("https://api.societe.com/api/v1/entreprise/%s/exist", numid)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Authorization", "socapi "+API_TOKEN)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("entreprise introuvable (numid invalide)")
	}
	// Note sur ton erreur 400 précédente : c'était parce que le numéro 112121212 est invalide (mauvais format/checksum).
	// Le code 321525156 est valide et renvoie 200 OK.
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("erreur API distante : %d - %s", resp.StatusCode, string(bodyBytes))
	}

	log.Printf("DEBUG - Réponse API brute pour %s : %s", numid, string(bodyBytes))

	var apiData SocieteExistResponse
	if err := json.Unmarshal(bodyBytes, &apiData); err != nil {
		return nil, fmt.Errorf("erreur de décodage JSON : %v", err)
	}

	result := &EntrepriseResponse{}

	// CORRECTION ICI : On doit passer par .Common pour accéder aux données
	result.Denomination = apiData.Common.Deno
	result.Siren = apiData.Common.Siren
	result.Siret = apiData.Common.SiretSiege
	result.Tva = apiData.Common.NumTVA

	if result.Denomination == "" {
		log.Println("⚠️ ATTENTION : La dénomination est toujours vide. Vérifie si l'API n'a pas changé de structure.")
	}

	return result, nil
}

// --- HANDLERS ---

func entrepriseHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	numid := strings.TrimPrefix(r.URL.Path, "/api/entreprise/")

	if len(numid) != 9 && len(numid) != 14 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Le paramètre doit être un SIREN (9 chiffres) ou un SIRET (14 chiffres)",
		})
		return
	}


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

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(data)
}

func sendEmailHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// 1. Vérification de la méthode (POST uniquement)
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "Méthode non autorisée. Utilisez POST."})
		return
	}

	// 2. Décodage du JSON reçu
	var req EmailRequest
	
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "JSON invalide"})
		return
	}

	// 3. Validation des champs
	if req.To == "" || req.Subject == "" || req.Body == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Les champs 'to', 'subject' et 'body' sont requis"})
		return
	}

	log.Printf("📧 Tentative d'envoi d'email à : %s", req.To)

	// 4. Configuration SMTP (Récupérée des variables d'environnement)
	smtpHost := os.Getenv("SMTP_HOST") // Exemple Gmail
	smtpPort := os.Getenv("SMTP_PORT")
	smtpUser := os.Getenv("SMTP_ADMIN_EMAIL")    // ex: monmail@gmail.com
	smtpPass := os.Getenv("SMTP_PASS") // ex: mot de passe d'application

	if smtpUser == "" || smtpPass == "" {
		log.Println("❌ Erreur : Identifiants SMTP manquants dans l'environnement")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Configuration serveur email incorrecte"})
		return
	}

	// 5. Construction du message
	msg := []byte(fmt.Sprintf("To: %s\r\n"+
		"Subject: %s\r\n"+
		"MIME-Version: 1.0\r\n"+
		"Content-Type: text/plain; charset=\"UTF-8\"\r\n"+
		"\r\n"+
		"%s\r\n", req.To, req.Subject, req.Body))

	// 6. Envoi
	auth := smtp.PlainAuth("", smtpUser, smtpPass, smtpHost)
	err := smtp.SendMail(smtpHost+":"+smtpPort, auth, smtpUser, []string{req.To}, msg)

	if err != nil {
		log.Printf("❌ Erreur lors de l'envoi SMTP : %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Échec de l'envoi de l'email : " + err.Error()})
		return
	}

	log.Printf("✅ Email envoyé avec succès à : %s", req.To)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Email envoyé avec succès"})
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
	mux.HandleFunc("/Send/", sendEmailHandler)

	handler := corsMiddleware(mux)

	log.Printf("🚀 Serveur démarré sur :%s", port)
	log.Printf("📍 Route active : GET /api/entreprise/{siren_ou_siret}")

	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("Erreur au démarrage: %v", err)
	}
}