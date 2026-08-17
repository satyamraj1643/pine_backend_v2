# Pine Backend

Microservices backend for the Pine application.

## Architecture

- **CoreService (Go)**: API Gateway, Auth, Postgres/Redis access. Proxies AI requests to AIService.
- **AIService (Python/FastAPI)**: LLM integration using LangChain and Groq API.

## Requirements

- Go 1.21+
- Python 3.11+
- PostgreSQL (Supabase)
- Redis

## Setup & Run

### 1. AIService (Python)

Handles all AI tasks.

```bash
cd AIService
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
cp .env.example .env # Configure your API keys
uvicorn main:app --port 8000 --reload
```

**AIService Environment Variables:**
- `AI_API_KEY`: Groq API key
- `AI_MODEL`: e.g., `llama-3.1-8b-instant`
- `LANGCHAIN_TRACING_V2`: `true`
- `LANGCHAIN_API_KEY`: LangSmith API key
- `LANGCHAIN_PROJECT`: LangSmith project name

### 2. CoreService (Go)

Handles auth and data storage, routes to AIService.

```bash
cd CoreService
cp .env.example .env # Set AI_SERVICE_URL=http://localhost:8000
go mod tidy
go run main.go
```

**CoreService Environment Variables:**
- `DATABASE_URL`: Postgres connection string
- `REDIS_URL`: Redis connection string
- `JWT_SECRET`: Auth signing key
- `SMTP_*`: Credentials for email delivery
- `AI_SERVICE_URL`: URL of the Python AIService (e.g. `http://localhost:8000`)

## Deployment (EC2 + Docker Compose)

The recommended production deployment uses Docker Compose and Caddy on a single EC2 instance. This automatically provisions SSL certificates for `api.pine.brink.co.in` and securely isolates the AI service.

### 1. Initial Server Setup
SSH into your EC2 instance and install Docker:
```bash
# Update packages and install Docker
sudo apt-get update
sudo apt-get install -y docker.io docker-compose-v2 git

# Enable Docker service
sudo systemctl enable --now docker
sudo usermod -aG docker $USER
newgrp docker
```

### 2. Deploy the Application
```bash
# Clone your repository
git clone https://github.com/satyamraj1643/pine_backend_v2.git
cd pine_backend_v2

# Configure environment variables
cp CoreService/.env.example CoreService/.env
cp AIService/.env.example AIService/.env
# Edit the .env files to add your production keys (Database URL, Groq API, etc.)
# NOTE: Inside CoreService/.env, AI_SERVICE_URL must be set to http://ai-service:8000
nano CoreService/.env
nano AIService/.env

# Start the stack
docker compose up -d --build
```

### 3. DNS Configuration
Since your frontend is on `pine.brink.co.in`, you should host this backend at `api.pine.brink.co.in`. 
In your Cloudflare (or DNS provider) dashboard, add the following:
- **Type**: `A`
- **Name**: `api`
- **IPv4 address**: `34.192.254.221` (Your EC2 Elastic IP)
- **Proxy status**: Turn Proxy (Orange Cloud) **OFF** (DNS Only). Caddy needs to communicate directly with Let's Encrypt to get the SSL certificate.

Once this is set, Caddy will automatically provision a Let's Encrypt SSL certificate.

You can check the logs of your services at any time using:
```bash
docker compose logs -f
```
