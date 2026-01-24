package computeruse

// SchemaJSON defines the JSON schema for computer use actions.
const SchemaJSON = `{
  "type": "object",
  "properties": {
    "action": {
      "type": "string",
      "description": "Computer use action to execute.",
      "enum": [
        "key",
        "type",
        "mouse_move",
        "left_click",
        "left_click_drag",
        "right_click",
        "middle_click",
        "double_click",
        "triple_click",
        "scroll",
        "wait",
        "cursor_position",
        "hold_key",
        "left_mouse_down",
        "left_mouse_up",
        "screenshot"
      ]
    },
    "coordinate": {
      "type": "array",
      "items": {"type": "integer"},
      "minItems": 2,
      "maxItems": 2,
      "description": "Target coordinate [x,y] in pixels."
    },
    "start_coordinate": {
      "type": "array",
      "items": {"type": "integer"},
      "minItems": 2,
      "maxItems": 2,
      "description": "Drag start coordinate [x,y] in pixels."
    },
    "end_coordinate": {
      "type": "array",
      "items": {"type": "integer"},
      "minItems": 2,
      "maxItems": 2,
      "description": "Drag end coordinate [x,y] in pixels."
    },
    "text": {
      "type": "string",
      "description": "Text payload for key/type actions."
    },
    "scroll_direction": {
      "type": "string",
      "enum": ["up", "down", "left", "right"],
      "description": "Scroll direction."
    },
    "scroll_amount": {
      "type": "integer",
      "minimum": 1,
      "description": "Scroll amount in ticks."
    },
    "duration_ms": {
      "type": "integer",
      "minimum": 0,
      "description": "Duration in milliseconds for wait/hold."
    },
    "duration_seconds": {
      "type": "number",
      "minimum": 0,
      "description": "Duration in seconds for wait/hold."
    },
    "edge_id": {
      "type": "string",
      "description": "Override edge id for computer use (core tool only)."
    }
  },
  "required": ["action"]
}`
