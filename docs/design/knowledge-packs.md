# RAG Knowledge Packs Design

## Overview

Knowledge packs package curated documents for fast RAG bootstrapping. Packs can be installed via CLI and indexed into the RAG store. This addresses issue #82.

## Goals

1. Simple pack format (YAML + files).
2. CLI install command for indexing.
3. Deterministic document IDs for idempotent installs.

## Pack Format

```
pack.yaml
files/...
```

```yaml
name: ops-runbooks
version: "1.0"
description: "On-call runbooks"
documents:
  - name: Pager Duty Guide
    path: docs/pager.md
    content_type: text/markdown
    tags: ["oncall", "pager"]
    source: runbooks
```

## CLI

```bash
nexus rag pack install --path ./packs/ops-runbooks
```

## Implementation

- `internal/rag/packs` handles pack loading + installation.
- `nexus rag pack install` loads `pack.yaml`, indexes documents, prints a report.

## Future Work

- Remote pack registry + version updates.
- Pack signatures for integrity.
- Pack uninstall / reindex.
