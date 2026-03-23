---
title: "Installation"
navTitle: "Installation"
description: "Install Keep via go install or build from source."
keywords: ["keep", "installation", "go install", "build"]
---

# Installation

Keep provides three binaries:

- `keep` -- the CLI for validating rules and inspecting configuration
- `keep-mcp-relay` -- the MCP relay proxy
- `keep-llm-gateway` -- the LLM gateway proxy

## Prerequisites

Keep requires [Go](https://go.dev/dl/) 1.25.6 or later.

## Install with `go install`

```bash
$ go install github.com/majorcontext/keep/cmd/keep@latest
$ go install github.com/majorcontext/keep/cmd/keep-mcp-relay@latest
$ go install github.com/majorcontext/keep/cmd/keep-llm-gateway@latest
```

This places binaries in your `$GOBIN` directory (defaults to `$HOME/go/bin`). Ensure it is in your `PATH`.

## Build from source

1. Clone the repository:

    ```bash
    $ git clone https://github.com/majorcontext/keep.git
    $ cd keep
    ```

2. Build all packages:

    ```bash
    $ make build
    ```

    To build individual binaries into the current directory:

    ```bash
    $ make build-cli
    $ make build-relay
    $ make build-gateway
    ```

3. Move the binaries to a location in your `PATH`, or run them directly from the project root.

## Verify installation

```bash
$ keep version

keep version dev (abc1234) built 2026-03-23T00:00:00Z
```

The exact output varies depending on how the binary was built. If `keep version` prints version information, the installation is working.
