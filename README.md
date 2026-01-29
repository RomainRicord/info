# Info Go - API de récupération d'informations

Service API simple en Go pour récupérer des informations système.

## 🚀 Endpoints

### Health Check

```bash
GET /health
```

Réponse:

```json
{
	"status": "ok",
	"code": 200
}
```

### Récupérer les infos

```bash
GET /info?type=system
GET /info?type=timestamp
```

Paramètres:

- `type` (optionnel): `system` (défaut) ou `timestamp`

Réponse:

```json
{
	"status": "success",
	"message": "Infos récupérées avec succès",
	"data": {
		"hostname": "...",
		"port": "8091",
		"environment": "...",
		"version": "1.0.0"
	}
}
```

## 🐳 Docker

### Build

```bash
docker build -t info_go .
```

### Run

```bash
docker run -d \
  --name info_go \
  -p 127.0.0.1:8091:8091 \
  -e ALLOWED_ORIGINS="http://localhost:3000" \
  -e ENVIRONMENT="development" \
  info_go
```

### Docker Compose

```bash
docker-compose up -d
```

## 📝 Variables d'environnement

- `PORT` - Port d'écoute (défaut: 8091)
- `ALLOWED_ORIGINS` - Origines autorisées pour CORS
- `ENVIRONMENT` - Environnement (development, staging, production)

## ✅ CORS

Le service supporte CORS avec les headers suivants:

- `Access-Control-Allow-Origin`
- `Access-Control-Allow-Methods`
- `Access-Control-Allow-Headers`
- `Access-Control-Max-Age`

## 🧪 Tests

```bash
# Health check
curl http://localhost:8091/health

# Infos système
curl http://localhost:8091/info?type=system

# Timestamp
curl http://localhost:8091/info?type=timestamp

# CORS preflight
curl -X OPTIONS http://localhost:8091/info \
  -H "Access-Control-Request-Method: GET"
```
