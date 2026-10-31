package agent

import "encoding/json"

// SchemaName is passed as llm.Request.SchemaName so providers that support
// named JSON schemas (response_format: json_schema) can use it.
const SchemaName = "poker_decision"

// DecisionSchema is the JSON Schema the model's output must satisfy
// (PLAN §4.2). reasoning comes first on purpose: it works as chain-of-thought
// that conditions the action.
var DecisionSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["reasoning", "action", "amount", "say"],
  "properties": {
    "reasoning": {
      "type": "object",
      "additionalProperties": false,
      "required": ["hand", "odds", "reads", "plan"],
      "properties": {
        "hand":  {"type": "string"},
        "odds":  {"type": "string"},
        "reads": {"type": "string"},
        "plan":  {"type": "string"}
      }
    },
    "action": {"type": "string", "enum": ["fold", "check", "call", "bet", "raise", "all_in"]},
    "amount": {"type": "integer", "minimum": 0},
    "say":    {"type": "string", "maxLength": 120}
  }
}`)
