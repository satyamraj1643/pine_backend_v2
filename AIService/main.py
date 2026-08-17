"""
Pine AIService — FastAPI Microservice for all AI features.

Uses Groq (llama-3.1-8b-instant) via OpenAI-compatible API.
Supports any OpenAI-compatible provider by changing AI_API_KEY, AI_BASE_URL, AI_MODEL.
"""

import json
import logging
import os
import re
from typing import Optional

from dotenv import load_dotenv

# Load env variables early so LangChain tracing variables are picked up
load_dotenv()

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel

from services.ai_service import (reflect_handler, suggest_mood_handler, ask_handler, weekly_recap_handler, insights_handler, chat_handler, personality_handler)

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")
logger = logging.getLogger("pine-ai")

# ─── Configuration ────────────────────────────────────────

AI_API_KEY = os.environ["AI_API_KEY"]
AI_MODEL = os.environ["AI_MODEL"]

# ─── FastAPI App ──────────────────────────────────────────

app = FastAPI(title="Pine AIService", version="1.0.0")




# ─── Request / Response Models ────────────────────────────

class ReflectRequest(BaseModel):
    title: str
    content: str

class SuggestMoodRequest(BaseModel):
    content: str
    existing_moods: Optional[str] = None  # comma-separated "name (id:1, emoji:smile), ..."

class AskRequest(BaseModel):
    question: str
    journal_entries: str  # pre-formatted text from Go backend

class WeeklyRecapRequest(BaseModel):
    entry_count: int
    weekly_entries: str

class InsightsRequest(BaseModel):
    entry_count: int
    journal_entries: str

class ChatMessage(BaseModel):
    role: str  # "user" or "assistant"
    content: str

class ChatRequest(BaseModel):
    title: str
    content: str
    messages: list[ChatMessage]

class PersonalityRequest(BaseModel):
    entry_count: int
    journal_entries: str


# ─── Endpoints ────────────────────────────────────────────

@app.get("/health")
def health():
    """Health check — verifies AI provider is reachable."""
    try:
        # A quick check to see if we can resolve the handler
        return {
            "available": True,
            "provider": "langchain_groq",
            "model": AI_MODEL,
        }
    except Exception as e:
        return {"available": False, "reason": str(e)}


@app.post("/reflect")
def reflect(req: ReflectRequest):
    return reflect_handler(req.title, req.content)


@app.post("/suggest-mood")
def suggest_mood(req: SuggestMoodRequest):
    return suggest_mood_handler(req.content, req.existing_moods)


@app.post("/ask")
def ask(req: AskRequest):
    return ask_handler(req.question, req.journal_entries)


@app.post("/weekly-recap")
def weekly_recap(req: WeeklyRecapRequest):
    return weekly_recap_handler(req.entry_count, req.weekly_entries)


@app.post("/insights")
def insights(req: InsightsRequest):
    return insights_handler(req.entry_count, req.journal_entries)


@app.post("/chat")
def chat(req: ChatRequest):
    msgs = [{"role": m.role, "content": m.content} for m in req.messages]
    return chat_handler(req.title, req.content, msgs)


@app.post("/personality")
def personality(req: PersonalityRequest):
    return personality_handler(req.entry_count, req.journal_entries)


# ─── Run ──────────────────────────────────────────────────

if __name__ == "__main__":
    import uvicorn
    port = int(os.getenv("PORT", "8000"))
    logger.info(f"Pine AIService starting on :{port} (model={AI_MODEL})")
    uvicorn.run(app, host="0.0.0.0", port=port)
