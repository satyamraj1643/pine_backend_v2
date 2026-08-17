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

## Deployment (Render)

Deploy as two separate **Web Services**:

1. **AIService (Python)**
   - Root Directory: `AIService`
   - Build Command: `pip install -r requirements.txt`
   - Start Command: `uvicorn main:app --host 0.0.0.0 --port $PORT`
2. **CoreService (Go)**
   - Root Directory: `CoreService`
   - Build Command: `go build -o bin/server main.go`
   - Start Command: `./bin/server`
   - Set `AI_SERVICE_URL` to the Render URL of the deployed AIService.
