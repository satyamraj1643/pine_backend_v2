import os
import logging
import json
import re
from typing import List, Dict, Any, Optional

from dotenv import load_dotenv
from langchain_groq import ChatGroq
from langchain_core.messages import HumanMessage, AIMessage

from services.prompts import (
    reflect_prompt,
    suggest_mood_existing_prompt,
    suggest_mood_new_prompt,
    ask_journal_prompt,
    weekly_recap_prompt,
    insights_prompt,
    chat_prompt,
    personality_prompt
)

load_dotenv()

logger = logging.getLogger("pine-ai")

AI_API_KEY = os.environ["AI_API_KEY"]
AI_MODEL = os.environ["AI_MODEL"]


def _get_llm() -> ChatGroq:
    """Factory for the LangChain Groq LLM."""
    return ChatGroq(model=AI_MODEL, api_key=AI_API_KEY)


def _parse_json_response(ai_message) -> Dict[str, Any]:
    """Strip potential markdown fences and parse JSON from AIMessage."""
    text = ai_message.content.strip()
    cleaned = re.sub(r"^```json\s*", "", text, flags=re.MULTILINE)
    cleaned = re.sub(r"^```\s*", "", cleaned, flags=re.MULTILINE)
    cleaned = re.sub(r"\s*```$", "", cleaned, flags=re.MULTILINE)
    cleaned = cleaned.strip()
    return json.loads(cleaned)

# ---------------------------------------------------------------------------
# Endpoint handler implementations – these are called from FastAPI routes.
# ---------------------------------------------------------------------------

def reflect_handler(title: str, content: str) -> Dict[str, str]:
    chain = reflect_prompt | _get_llm()
    result = chain.invoke({"title": title, "content": content})
    return {"reflection": result.content.strip()}


def suggest_mood_handler(content: str, existing_moods: Optional[str] = None) -> Dict[str, Any]:
    if existing_moods:
        chain = suggest_mood_existing_prompt | _get_llm()
        result = chain.invoke({"existing_moods": existing_moods, "content": content})
    else:
        chain = suggest_mood_new_prompt | _get_llm()
        result = chain.invoke({"content": content})
    return _parse_json_response(result)


def ask_handler(question: str, journal_entries: str) -> Dict[str, str]:
    chain = ask_journal_prompt | _get_llm()
    result = chain.invoke({"journal_entries": journal_entries, "question": question})
    return {"answer": result.content.strip()}


def weekly_recap_handler(entry_count: int, weekly_entries: str) -> Dict[str, str]:
    chain = weekly_recap_prompt | _get_llm()
    result = chain.invoke({"entry_count": str(entry_count), "weekly_entries": weekly_entries})
    return {"recap": result.content.strip()}


def insights_handler(entry_count: int, journal_entries: str) -> Dict[str, Any]:
    chain = insights_prompt | _get_llm()
    result = chain.invoke({"entry_count": str(entry_count), "journal_entries": journal_entries})
    return _parse_json_response(result)


def chat_handler(title: str, content: str, messages: List[Dict[str, str]]) -> Dict[str, str]:
    history = []
    for m in messages:
        if m["role"].lower() == "user":
            history.append(HumanMessage(content=m["content"]))
        else:
            history.append(AIMessage(content=m["content"]))
            
    chain = chat_prompt | _get_llm()
    result = chain.invoke({
        "title": title,
        "content": content,
        "messages": history
    })
    return {"reply": result.content.strip()}


def personality_handler(entry_count: int, journal_entries: str) -> Dict[str, Any]:
    chain = personality_prompt | _get_llm()
    result = chain.invoke({"entry_count": str(entry_count), "journal_entries": journal_entries})
    return _parse_json_response(result)
