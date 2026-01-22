package templates

import (
	"fmt"

	"github.com/haasonsaas/nexus/internal/multiagent"
)

// Instantiator creates agents from templates.
type Instantiator struct {
	registry   *Registry
	varsEngine *VariableEngine
}

// NewInstantiator creates a new template instantiator.
func NewInstantiator(registry *Registry) *Instantiator {
	return &Instantiator{
		registry:   registry,
		varsEngine: NewVariableEngine(),
	}
}

// Instantiate creates an agent from a template with the given parameters.
func (inst *Instantiator) Instantiate(req *InstantiationRequest) (*InstantiationResult, error) {
	// Get the template
	tmpl, ok := inst.registry.Get(req.TemplateName)
	if !ok {
		return nil, fmt.Errorf("template not found: %s", req.TemplateName)
	}

	// Load content if not loaded
	if tmpl.Content == "" {
		content, err := inst.registry.LoadContent(req.TemplateName)
		if err != nil {
			return nil, fmt.Errorf("load template content: %w", err)
		}
		tmpl.Content = content
	}

	return inst.InstantiateFromTemplate(tmpl, req)
}

// InstantiateFromTemplate creates an agent from a template object directly.
func (inst *Instantiator) InstantiateFromTemplate(tmpl *AgentTemplate, req *InstantiationRequest) (*InstantiationResult, error) {
	result := &InstantiationResult{
		Template:      tmpl,
		UsedVariables: make(map[string]any),
	}

	// Build the variable context
	varCtx, err := inst.buildVariableContext(tmpl, req)
	if err != nil {
		return nil, fmt.Errorf("build variable context: %w", err)
	}
	result.UsedVariables = varCtx

	// Process the system prompt template
	systemPrompt, err := inst.varsEngine.Process(tmpl.Content, varCtx)
	if err != nil {
		return nil, fmt.Errorf("process system prompt template: %w", err)
	}

	// Create the agent definition
	agent := &multiagent.AgentDefinition{
		ID:                 req.AgentID,
		Name:               req.AgentName,
		Description:        tmpl.Description,
		SystemPrompt:       systemPrompt,
		Model:              tmpl.Agent.Model,
		Provider:           tmpl.Agent.Provider,
		Tools:              tmpl.Agent.Tools,
		ToolPolicy:         tmpl.Agent.ToolPolicy,
		HandoffRules:       tmpl.Agent.HandoffRules,
		CanReceiveHandoffs: tmpl.Agent.CanReceiveHandoffs,
		MaxIterations:      tmpl.Agent.MaxIterations,
		Metadata:           make(map[string]any),
	}

	// Use template name if agent name not provided
	if agent.Name == "" {
		agent.Name = tmpl.Name
	}

	// Copy metadata from template
	if tmpl.Agent.Metadata != nil {
		for k, v := range tmpl.Agent.Metadata {
			agent.Metadata[k] = v
		}
	}

	// Add template source info to metadata
	agent.Metadata["template_name"] = tmpl.Name
	agent.Metadata["template_version"] = tmpl.Version
	agent.Metadata["template_source"] = string(tmpl.Source)

	// Apply overrides if provided
	if req.Overrides != nil {
		applyOverrides(agent, req.Overrides, &result.Warnings)
	}

	// Process variable substitution in agent fields
	if err := inst.processAgentVariables(agent, varCtx); err != nil {
		return nil, fmt.Errorf("process agent variables: %w", err)
	}

	result.Agent = agent
	return result, nil
}

// buildVariableContext creates the variable context for template processing.
func (inst *Instantiator) buildVariableContext(tmpl *AgentTemplate, req *InstantiationRequest) (map[string]any, error) {
	ctx := make(map[string]any)

	// Start with defaults from template variables
	for _, v := range tmpl.Variables {
		if v.Default != nil {
			ctx[v.Name] = v.Default
		}
	}

	// Apply provided values
	if req.Variables != nil {
		for k, v := range req.Variables {
			ctx[k] = v
		}
	}

	// Validate required variables
	for _, v := range tmpl.Variables {
		if v.Required {
			if _, ok := ctx[v.Name]; !ok {
				return nil, fmt.Errorf("missing required variable: %s", v.Name)
			}
		}
	}

	// Validate variable values
	for _, v := range tmpl.Variables {
		value, ok := ctx[v.Name]
		if !ok {
			continue
		}

		if err := inst.validateVariable(&v, value); err != nil {
			return nil, fmt.Errorf("variable %q: %w", v.Name, err)
		}
	}

	// Add built-in variables
	ctx["agent_id"] = req.AgentID
	ctx["agent_name"] = req.AgentName
	ctx["template_name"] = tmpl.Name
	ctx["template_version"] = tmpl.Version

	return ctx, nil
}

// validateVariable validates a variable value against its definition.
func (inst *Instantiator) validateVariable(v *TemplateVariable, value any) error {
	// Type validation
	if v.Type != "" {
		if err := validateValueType(value, v.Type); err != nil {
			return err
		}
	}

	// Options validation (enum)
	if len(v.Options) > 0 {
		found := false
		for _, opt := range v.Options {
			if opt == value {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("value not in allowed options: %v", value)
		}
	}

	// Custom validation rules
	if v.Validation != nil {
		if err := inst.applyValidationRules(v, value); err != nil {
			return err
		}
	}

	return nil
}

// applyValidationRules applies custom validation rules to a value.
func (inst *Instantiator) applyValidationRules(v *TemplateVariable, value any) error {
	val := v.Validation

	switch v.Type {
	case VariableTypeString, "":
		str, ok := value.(string)
		if !ok {
			return nil // Type mismatch handled elsewhere
		}

		if val.MinLength != nil && len(str) < *val.MinLength {
			return fmt.Errorf("string too short: minimum %d characters", *val.MinLength)
		}
		if val.MaxLength != nil && len(str) > *val.MaxLength {
			return fmt.Errorf("string too long: maximum %d characters", *val.MaxLength)
		}
		if val.Pattern != "" {
			if err := inst.varsEngine.ValidatePattern(val.Pattern, str); err != nil {
				return fmt.Errorf("pattern validation failed: %w", err)
			}
		}

	case VariableTypeNumber:
		num, err := toFloat64(value)
		if err != nil {
			return nil // Type mismatch handled elsewhere
		}

		if val.Min != nil && num < *val.Min {
			return fmt.Errorf("value too small: minimum %v", *val.Min)
		}
		if val.Max != nil && num > *val.Max {
			return fmt.Errorf("value too large: maximum %v", *val.Max)
		}

	case VariableTypeArray:
		arr, ok := value.([]any)
		if !ok {
			return nil // Type mismatch handled elsewhere
		}

		if val.MinItems != nil && len(arr) < *val.MinItems {
			return fmt.Errorf("array too short: minimum %d items", *val.MinItems)
		}
		if val.MaxItems != nil && len(arr) > *val.MaxItems {
			return fmt.Errorf("array too long: maximum %d items", *val.MaxItems)
		}
	}

	return nil
}

// processAgentVariables processes variable substitution in agent string fields.
func (inst *Instantiator) processAgentVariables(agent *multiagent.AgentDefinition, ctx map[string]any) error {
	var err error

	// Process model and provider (they might contain variables)
	if agent.Model != "" {
		agent.Model, err = inst.varsEngine.Process(agent.Model, ctx)
		if err != nil {
			return fmt.Errorf("process model: %w", err)
		}
	}

	if agent.Provider != "" {
		agent.Provider, err = inst.varsEngine.Process(agent.Provider, ctx)
		if err != nil {
			return fmt.Errorf("process provider: %w", err)
		}
	}

	// Process description
	if agent.Description != "" {
		agent.Description, err = inst.varsEngine.Process(agent.Description, ctx)
		if err != nil {
			return fmt.Errorf("process description: %w", err)
		}
	}

	return nil
}

// applyOverrides applies request overrides to the agent definition.
func applyOverrides(agent *multiagent.AgentDefinition, overrides *AgentTemplateSpec, warnings *[]string) {
	if overrides.Model != "" {
		agent.Model = overrides.Model
	}
	if overrides.Provider != "" {
		agent.Provider = overrides.Provider
	}
	if len(overrides.Tools) > 0 {
		agent.Tools = overrides.Tools
	}
	if overrides.ToolPolicy != nil {
		agent.ToolPolicy = overrides.ToolPolicy
	}
	if len(overrides.HandoffRules) > 0 {
		agent.HandoffRules = overrides.HandoffRules
	}
	if overrides.MaxIterations > 0 {
		agent.MaxIterations = overrides.MaxIterations
	}
	if overrides.Metadata != nil {
		if agent.Metadata == nil {
			agent.Metadata = make(map[string]any)
		}
		for k, v := range overrides.Metadata {
			agent.Metadata[k] = v
		}
	}
}

// toFloat64 converts a numeric value to float64.
func toFloat64(v any) (float64, error) {
	switch val := v.(type) {
	case int:
		return float64(val), nil
	case int8:
		return float64(val), nil
	case int16:
		return float64(val), nil
	case int32:
		return float64(val), nil
	case int64:
		return float64(val), nil
	case uint:
		return float64(val), nil
	case uint8:
		return float64(val), nil
	case uint16:
		return float64(val), nil
	case uint32:
		return float64(val), nil
	case uint64:
		return float64(val), nil
	case float32:
		return float64(val), nil
	case float64:
		return val, nil
	default:
		return 0, fmt.Errorf("not a number: %T", v)
	}
}

// QuickInstantiate is a convenience function for simple instantiation.
func QuickInstantiate(registry *Registry, templateName, agentID string, variables map[string]any) (*multiagent.AgentDefinition, error) {
	inst := NewInstantiator(registry)
	result, err := inst.Instantiate(&InstantiationRequest{
		TemplateName: templateName,
		AgentID:      agentID,
		Variables:    variables,
	})
	if err != nil {
		return nil, err
	}
	return result.Agent, nil
}
