"""Plain-English questions about what to reorder, answered with Claude.

Claude never computes a quantity. It can only call two read-only tools that
return numbers worked out in code (the forecast, safety stock and order plan),
and the system prompt tells it to quote those numbers and say where they came
from. Nothing here can place an order.
"""

from __future__ import annotations

import json
import os

SYSTEM = """You help a rail maintenance depot's supply chain planners decide what to reorder.

Rules:
- Every number you give must come from a tool result. Never estimate, add up or adjust quantities yourself; if a figure isn't in a tool result, say you don't have it.
- You cannot place orders. Suggest them; a planner places them.
- The demand history is simulated, so say so if asked how reliable the forecast is, and quote the backtest error for that part.
- Be brief: lead with what to order, then why, in a short list."""

TOOLS = [
    {
        "name": "get_reorder_plan",
        "description": "This week's suggested order for every part: units available now (live from StockLedger), forecast demand per day, the order-up-to level, safety stock and the suggested order quantity rounded to the supplier's case pack.",
        "input_schema": {"type": "object", "properties": {}, "additionalProperties": False},
    },
    {
        "name": "get_part_detail",
        "description": "One part's plan row plus its lead time, service level target, backtest forecast error (WAPE) and its last 8 weeks of demand by week.",
        "input_schema": {
            "type": "object",
            "properties": {"sku": {"type": "string", "description": "Part number, e.g. BRK-PAD-220"}},
            "required": ["sku"],
            "additionalProperties": False,
        },
    },
]

MODEL = os.environ.get("REPLENISH_MODEL", "claude-opus-5-5")
MAX_TURNS = 6


class Tools:
    """Answers tool calls from a computed plan; holds no model or network state."""

    def __init__(self, plan_rows: list[dict], details: dict):
        self.plan_rows = plan_rows
        self.details = details

    def call(self, name: str, args: dict) -> tuple[str, bool]:
        """Returns (JSON result, is_error)."""
        if name == "get_reorder_plan":
            return json.dumps(self.plan_rows), False
        if name == "get_part_detail":
            sku = str(args.get("sku", "")).upper()
            if sku not in self.details:
                return f"Unknown part {sku!r}. Known parts: {', '.join(sorted(self.details))}", True
            return json.dumps(self.details[sku]), False
        return f"Unknown tool {name!r}", True


def ask(question: str, tools: Tools, client=None) -> str:
    """Runs the tool loop until Claude answers. client defaults to anthropic.Anthropic()."""
    if client is None:
        import anthropic

        client = anthropic.Anthropic()
    messages = [{"role": "user", "content": question}]
    for _ in range(MAX_TURNS):
        response = client.messages.create(
            model=MODEL,
            max_tokens=4000,
            thinking={"type": "adaptive"},
            system=SYSTEM,
            tools=TOOLS,
            messages=messages,
        )
        if response.stop_reason == "refusal":
            return "Claude declined to answer this question."
        calls = [b for b in response.content if b.type == "tool_use"]
        if response.stop_reason != "tool_use" or not calls:
            text = "".join(b.text for b in response.content if b.type == "text")
            if response.stop_reason == "max_tokens":
                text += "\n[answer cut off at the token limit]"
            return text
        messages.append({"role": "assistant", "content": response.content})
        results = []
        for call in calls:
            content, is_error = tools.call(call.name, call.input)
            results.append({"type": "tool_result", "tool_use_id": call.id, "content": content, "is_error": is_error})
        messages.append({"role": "user", "content": results})
    return "Stopped after too many tool calls without an answer."
