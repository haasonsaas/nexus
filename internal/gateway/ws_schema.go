package gateway

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

type wsSchemaRegistry struct {
	once    sync.Once
	initErr error
	request *jsonschema.Schema
	methods map[string]*jsonschema.Schema
}

var wsSchemas wsSchemaRegistry

func initWSSchemas() error {
	wsSchemas.once.Do(func() {
		reqSchema, err := jsonschema.CompileString("ws_request", wsRequestSchema)
		if err != nil {
			wsSchemas.initErr = err
			return
		}
		wsSchemas.request = reqSchema

		methods := map[string]string{
			"connect":        wsConnectParamsSchema,
			"health":         wsHealthParamsSchema,
			"ping":           wsPingParamsSchema,
			"chat.send":      wsChatSendParamsSchema,
			"chat.history":   wsChatHistoryParamsSchema,
			"chat.abort":     wsChatAbortParamsSchema,
			"sessions.list":  wsSessionsListParamsSchema,
			"sessions.patch": wsSessionsPatchParamsSchema,
		}

		wsSchemas.methods = make(map[string]*jsonschema.Schema, len(methods))
		for name, schema := range methods {
			compiled, err := jsonschema.CompileString("ws_method_"+name, schema)
			if err != nil {
				wsSchemas.initErr = err
				return
			}
			wsSchemas.methods[name] = compiled
		}
	})
	return wsSchemas.initErr
}

func validateWSRequestFrame(raw []byte, frame *wsFrame) error {
	if err := initWSSchemas(); err != nil {
		return err
	}

	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}
	if err := wsSchemas.request.Validate(payload); err != nil {
		return err
	}
	if frame == nil {
		return fmt.Errorf("missing frame")
	}
	if schema := wsSchemas.methods[frame.Method]; schema != nil {
		var params any
		if len(frame.Params) == 0 {
			params = map[string]any{}
		} else if err := json.Unmarshal(frame.Params, &params); err != nil {
			return err
		}
		if err := schema.Validate(params); err != nil {
			return err
		}
	}
	return nil
}

const wsRequestSchema = `{
  "type": "object",
  "required": ["type", "id", "method"],
  "properties": {
    "type": { "const": "req" },
    "id": { "type": "string", "minLength": 1 },
    "method": { "type": "string", "minLength": 1 },
    "params": {}
  },
  "additionalProperties": true
}`

const wsConnectParamsSchema = `{
  "type": "object",
  "required": ["minProtocol", "maxProtocol", "client"],
  "properties": {
    "minProtocol": { "type": "integer", "minimum": 1 },
    "maxProtocol": { "type": "integer", "minimum": 1 },
    "client": {
      "type": "object",
      "required": ["id", "version", "platform"],
      "properties": {
        "id": { "type": "string", "minLength": 1 },
        "version": { "type": "string", "minLength": 1 },
        "platform": { "type": "string", "minLength": 1 },
        "mode": { "type": "string" },
        "userAgent": { "type": "string" }
      },
      "additionalProperties": true
    },
    "auth": {
      "type": "object",
      "properties": {
        "token": { "type": "string" }
      },
      "additionalProperties": true
    },
    "caps": {
      "type": "array",
      "items": { "type": "string" }
    }
  },
  "additionalProperties": true
}`

const wsHealthParamsSchema = `{
  "type": "object",
  "additionalProperties": true
}`

const wsPingParamsSchema = `{
  "type": "object",
  "additionalProperties": true
}`

const wsChatSendParamsSchema = `{
  "type": "object",
  "required": ["content"],
  "properties": {
    "sessionId": { "type": "string" },
    "content": { "type": "string", "minLength": 1 },
    "metadata": {
      "type": "object",
      "additionalProperties": { "type": "string" }
    },
    "attachments": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "id": { "type": "string" },
          "type": { "type": "string" },
          "url": { "type": "string" },
          "filename": { "type": "string" },
          "mimeType": { "type": "string" },
          "size": { "type": "integer" }
        },
        "additionalProperties": true
      }
    },
    "idempotencyKey": { "type": "string" }
  },
  "additionalProperties": true
}`

const wsChatHistoryParamsSchema = `{
  "type": "object",
  "required": ["sessionId"],
  "properties": {
    "sessionId": { "type": "string", "minLength": 1 },
    "limit": { "type": "integer", "minimum": 1, "maximum": 500 }
  },
  "additionalProperties": true
}`

const wsChatAbortParamsSchema = `{
  "type": "object",
  "required": ["sessionId"],
  "properties": {
    "sessionId": { "type": "string", "minLength": 1 }
  },
  "additionalProperties": true
}`

const wsSessionsListParamsSchema = `{
  "type": "object",
  "properties": {
    "agentId": { "type": "string" },
    "channel": { "type": "string" },
    "limit": { "type": "integer", "minimum": 1, "maximum": 500 },
    "offset": { "type": "integer", "minimum": 0 }
  },
  "additionalProperties": true
}`

const wsSessionsPatchParamsSchema = `{
  "type": "object",
  "required": ["sessionId"],
  "properties": {
    "sessionId": { "type": "string", "minLength": 1 },
    "title": { "type": "string" },
    "metadata": {
      "type": "object",
      "additionalProperties": { "type": "string" }
    }
  },
  "additionalProperties": true
}`
