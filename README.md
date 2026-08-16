# Pine Backend Server

A high-performance Go backend for Pine with PostgreSQL (Supabase), Redis cache, Amazon Bedrock (Claude), and LangSmith tracing.

---

## 🚀 Deploying to Render

You can deploy this backend as a **Web Service** on [Render](https://render.com) using either **Native Go** or **Docker**.

### Option 1: Native Go Web Service (Recommended)
1. In the Render Dashboard, click **New +** -> **Web Service**.
2. Connect your GitHub repository.
3. Configure the service:
   - **Name**: `pine-backend`
   - **Runtime**: `Go`
   - **Build Command**: `go build -o bin/server .`
   - **Start Command**: `./bin/server`
4. Add the Environment Variables (see `.env.example`).

### Option 2: Docker Web Service
1. In the Render Dashboard, click **New +** -> **Web Service**.
2. Connect your GitHub repository.
3. Select **Docker** as runtime (Render will automatically pick up `Dockerfile`).
4. Add the Environment Variables.

---

## 🔑 Required Environment Variables

Set the following in **Environment Variables** on Render:

| Variable | Description | Example |
|---|---|---|
| `PORT` | Web server port (Render sets this automatically) | `8080` |
| `DATABASE_URL` | PostgreSQL connection string | `postgresql://postgres:pass@host:6543/postgres` |
| `REDIS_URL` | Redis URL | `rediss://default:pass@host:port` or `redis://red-xxx:6379` |
| `JWT_SECRET` | Secret string for signing auth tokens | `your_secret_key` |
| `SMTP_EMAIL` | Sender email address for OTP | `pine@example.com` |
| `SMTP_APP_PASSWORD` | App password for SMTP provider | `xxxx-xxxx-xxxx-xxxx` |
| `SMTP_HOST` | SMTP server host | `smtp.gmail.com` |
| `SMTP_PORT` | SMTP port | `587` |
| `AWS_ACCESS_KEY_ID` | AWS key for Bedrock | `AKIA...` |
| `AWS_SECRET_ACCESS_KEY`| AWS secret for Bedrock | `secret` |
| `AWS_REGION` | AWS Region for Bedrock | `us-east-1` |
| `LANGSMITH_API_KEY` | (Optional) LangSmith tracing API key | `lsv2_pt_...` |
| `LANGSMITH_PROJECT` | (Optional) LangSmith project name | `pine-production` |

---

## 💻 Local Development

```bash
# Copy and configure environment variables
cp .env.example .env

# Run the server
go run main.go
```
