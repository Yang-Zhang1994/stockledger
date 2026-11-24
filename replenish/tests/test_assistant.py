import json
from types import SimpleNamespace as NS

from replenish.assistant import Tools, ask

ROWS = [{"sku": "BRK-PAD-220", "available": 12, "order_qty": 260}]
TOOLS = Tools(ROWS, {"BRK-PAD-220": {**ROWS[0], "backtest_wape": 0.08}})


class FakeClient:
    """Plays back scripted responses and records each request."""

    def __init__(self, *responses):
        self.responses = list(responses)
        self.requests = []
        self.messages = self

    def create(self, **kw):
        self.requests.append(json.loads(json.dumps(kw["messages"], default=lambda o: vars(o))))
        return self.responses.pop(0)


def tool_use(name, args, id="t1"):
    return NS(stop_reason="tool_use", content=[NS(type="tool_use", id=id, name=name, input=args)])


def text(s):
    return NS(stop_reason="end_turn", content=[NS(type="text", text=s)])


def test_answers_from_tool_results():
    client = FakeClient(tool_use("get_reorder_plan", {}), text("Order 260 brake pads."))
    assert ask("What should I order?", TOOLS, client) == "Order 260 brake pads."
    result = client.requests[1][-1]["content"][0]
    assert result["type"] == "tool_result" and json.loads(result["content"]) == ROWS


def test_unknown_part_is_an_error_result():
    client = FakeClient(tool_use("get_part_detail", {"sku": "NOPE"}), text("I don't know that part."))
    ask("How about NOPE?", TOOLS, client)
    result = client.requests[1][-1]["content"][0]
    assert result["is_error"] is True and "BRK-PAD-220" in result["content"]


def test_stops_if_it_never_answers():
    client = FakeClient(*[tool_use("get_reorder_plan", {}) for _ in range(10)])
    assert "too many" in ask("loop", TOOLS, client)
