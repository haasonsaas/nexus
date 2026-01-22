---
name: example-hook
description: An example bundled hook demonstrating the hook system
events:
  - gateway:startup
priority: 100
enabled: true
---

# Example Bundled Hook

This is an example hook that ships with nexus to demonstrate the hook system.

## Behavior

This hook runs on gateway startup and logs a message to confirm the hooks system is working.

## Usage

This hook is automatically loaded when nexus starts. You can override it by creating a hook with the same name in your workspace or local hooks directory.
