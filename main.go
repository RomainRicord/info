package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"time"
)

// --- CONSTANTES ---
const API_TOKEN = "3b8fe35c2885c14c1eaee3248c79472b"

// --- STRUCTURES DE DONNÉES ---

// 1. Structure pour la réponse Entreprise
type EntrepriseResponse struct {
	Denomination         string `json:"denomination"`
	Siren                string `json:"siren"`
	Siret                string `json:"siret"`
	Tva                  string `json:"tva"`
	AdressePostaleLegale struct {
		Ville      string `json:"ville"`
		CodePostal string `json:"code_postal"`
	} `json:"adresse_postale_legale"`
}

// 2. Structure interne API Societe.com
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

// 3. Structure pour la requête d'envoi d'email
type EmailRequest struct {
	To             string `json:"to"`
	Subject        string `json:"subject"`
	Body           string `json:"body"`
	AttachmentName string `json:"attachment_name"` // Nom du fichier (ex: devis.pdf)
	AttachmentData string `json:"attachment_data"` // Contenu en Base64
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

// --- UTILITAIRES ---

// splitLines découpe une longue chaîne (Base64) en lignes de 76 caractères
// C'est INDISPENSABLE pour respecter le protocole MIME/SMTP
func splitLines(s string) string {
	var lines []string
	for len(s) > 76 {
		lines = append(lines, s[:76])
		s = s[76:]
	}
	lines = append(lines, s)
	return strings.Join(lines, "\r\n")
}

// --- MIDDLEWARES ---

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowedOrigins := []string{
			"http://localhost:8082",
			"https://vintagestandards.fr",
			"https://dev.vintagestandards.fr/",
		}

		origin := r.Header.Get("Origin")
		for _, allowed := range allowedOrigins {
			if origin == allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				break
			}
		}
//
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

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("erreur API distante : %d - %s", resp.StatusCode, string(bodyBytes))
	}

	log.Printf("DEBUG - Réponse API brute pour %s : %s", numid, string(bodyBytes))

	var apiData SocieteExistResponse
	if err := json.Unmarshal(bodyBytes, &apiData); err != nil {
		return nil, fmt.Errorf("erreur de décodage JSON : %v", err)
	}

	result := &EntrepriseResponse{}
	result.Denomination = apiData.Common.Deno
	result.Siren = apiData.Common.Siren
	result.Siret = apiData.Common.SiretSiege
	result.Tva = apiData.Common.NumTVA

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

	log.Printf("🔍 Vérification existence (SIREN/SIRET) : %s", numid)

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

// Handler d'envoi d'email (Support PDF + Fix SSL/TLS + MIME Fix)
func sendEmailHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "Méthode non autorisée. Utilisez POST."})
		return
	}

	var req EmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "JSON invalide"})
		return
	}

	if req.To == "" || req.Subject == "" || req.Body == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Les champs 'to', 'subject' et 'body' sont requis"})
		return
	}

	// --- RECUPERATION ENV ---
	smtpHost := os.Getenv("SMTP_HOST")
	smtpPort := os.Getenv("SMTP_PORT")
	smtpUser := os.Getenv("SMTP_ADMIN_EMAIL")
	smtpPass := os.Getenv("SMTP_PASS")

	log.Printf("📧 Config SMTP -> Host: %s | Port: %s | User: %s", smtpHost, smtpPort, smtpUser)

	if smtpHost == "" || smtpPort == "" || smtpUser == "" || smtpPass == "" {
		log.Println("❌ Erreur : Configuration SMTP incomplète (ENV)")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Configuration serveur email incomplète"})
		return
	}

	// Debug : Vérifier si on reçoit le PDF
	if req.AttachmentData != "" {
		log.Printf("📎 Backend : PDF reçu ! Taille: %d caractères", len(req.AttachmentData))
	} else {
		log.Println("⚠️ Backend : Pas de données PDF reçues")
	}

	// --- CONSTRUCTION EMAIL (MIME Multipart) ---
	boundary := "MyBoundarySeparation12345"

	// En-têtes
	header := make(map[string]string)
	header["From"] = smtpUser
	header["To"] = req.To
	header["Subject"] = req.Subject
	header["MIME-Version"] = "1.0"
	header["Content-Type"] = "multipart/mixed; boundary=" + boundary

	message := ""
	for k, v := range header {
		message += fmt.Sprintf("%s: %s\r\n", k, v)
	}
	message += "\r\n"

	// PARTIE 1 : Corps du texte
	message += fmt.Sprintf("--%s\r\n", boundary)
	message += "Content-Type: text/plain; charset=\"utf-8\"\r\n"
	message += "Content-Transfer-Encoding: 7bit\r\n"
	message += "\r\n"
	message += req.Body + "\r\n"

	// PARTIE 2 : Pièce jointe (si présente)
	if req.AttachmentData != "" && req.AttachmentName != "" {
		// Nettoyage nom de fichier
		cleanName := strings.ReplaceAll(req.AttachmentName, "\n", "")

		message += fmt.Sprintf("--%s\r\n", boundary)
		message += "Content-Type: application/pdf\r\n"
		message += "Content-Transfer-Encoding: base64\r\n"
		message += fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n", cleanName)
		message += "\r\n"
		// IMPORTANT : Découpage du Base64
		message += splitLines(req.AttachmentData) + "\r\n"
	}

	message += fmt.Sprintf("--%s--\r\n", boundary)

	// --- ENVOI ---
	msgBytes := []byte(message)
	addr := smtpHost + ":" + smtpPort
	auth := smtp.PlainAuth("", smtpUser, smtpPass, smtpHost)

	var err error

	// GESTION SSL (Port 465) vs STARTTLS (587)
	if smtpPort == "465" {
		log.Println("🔒 Connexion SSL Implicite détectée (Port 465)")
		err = sendMail465(addr, auth, smtpUser, []string{req.To}, msgBytes)
	} else {
		log.Println("🔓 Connexion STARTTLS standard")
		err = smtp.SendMail(addr, auth, smtpUser, []string{req.To}, msgBytes)
	}

	if err != nil {
		log.Printf("❌ Erreur lors de l'envoi SMTP : %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Échec de l'envoi : " + err.Error()})
		return
	}

	log.Printf("✅ Email envoyé avec succès à : %s", req.To)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Email envoyé avec succès"})
}

// Fonction utilitaire pour gérer le SSL (Port 465)
func sendMail465(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
	host, _, _ := net.SplitHostPort(addr)

	tlsConfig := &tls.Config{
		InsecureSkipVerify: false,
		ServerName:         host,
	}

	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()

	if auth != nil {
		if ok, _ := client.Extension("AUTH"); ok {
			if err = client.Auth(auth); err != nil {
				return err
			}
		}
	}

	if err = client.Mail(from); err != nil {
		return err
	}
	for _, addr := range to {
		if err = client.Rcpt(addr); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	_, err = w.Write(msg)
	if err != nil {
		return err
	}
	err = w.Close()
	if err != nil {
		return err
	}
	return client.Quit()
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(HealthResponse{Status: "ok", Code: 200})
}

func infoHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(InfoResponse{Status: "success", Data: map[string]string{"version": "1.0.0"}})
}

// --- MAIN ---

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8091"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/info", infoHandler)

	// Routes Métier
	mux.HandleFunc("/api/entreprise/", entrepriseHandler)
	mux.HandleFunc("/api/send-email", sendEmailHandler)
	mux.HandleFunc("/Send/", sendEmailHandler) // Alias

	handler := corsMiddleware(mux)

	log.Printf("🚀 Serveur démarré sur :%s", port)
	log.Printf("📍 Route Entreprise : GET /api/entreprise/{siren}")
	log.Printf("📍 Route Email      : POST /api/send-email (Support PDF)")

	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("Erreur au démarrage: %v", err)
	}
}