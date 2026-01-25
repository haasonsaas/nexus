# Skills Composition & Tool Registration

## Overview

Extends skills to declare JSON-schema tools. This addresses #109 (skill composition) and #91 (skill-provided tools).

## Goals

1. Allow skills to expose tools via metadata.
2. Register tools automatically when skills are eligible.
3. Route tool execution through existing exec manager.

## Skill Metadata Extension

```yaml
metadata:
  tools:
    - name: my_tool
      description: "Run custom workflow"
      command: "bash"
      script: "scripts/run.sh"
      schema:
        type: object
        properties:
          input:
            type: string
```

## Implementation

- `SkillMetadata.Tools` stores `SkillToolSpec` entries.
- `skills.BuildSkillTools` creates `agent.Tool` wrappers for scripts.
- `ToolManager.RegisterTools` registers skill tools for eligible skills.

## Execution

- Tool input JSON is passed via `NEXUS_TOOL_INPUT` and stdin.
- `NEXUS_SKILL_DIR` points to the skill directory for scripts.

## Future Work

- Support tool composition (dependencies/inheritance).
- Allow remote tools via MCP or plugin routing.
- Add tests for script execution and policy gating.
