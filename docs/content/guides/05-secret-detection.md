---
title: "Secret detection"
navTitle: "Secret detection"
description: "Automatically detect and redact secrets in API calls using gitleaks patterns."
keywords: ["keep", "secrets", "detection", "redaction", "gitleaks", "credentials"]
---

# Secret detection

Keep includes built-in secret detection powered by gitleaks, an open-source library with approximately 160 regex patterns for known credential formats. This guide covers enabling secret detection in redact rules, using `hasSecrets()` in CEL expressions, and understanding what the detector catches.

## What it detects

The gitleaks pattern set covers credentials from major cloud providers, SaaS platforms, and common secret formats:

- AWS access keys and secret keys
- GitHub personal access tokens, fine-grained tokens, and OAuth tokens
- Stripe API keys (live and test)
- Google Cloud API keys and service account credentials
- Private keys (RSA, DSA, EC, PGP)
- Generic API keys, passwords in URLs, and high-entropy strings
- Tokens for Slack, GitLab, npm, PyPI, Twilio, SendGrid, and many others

The full list is maintained in the [gitleaks default config](https://github.com/gitleaks/gitleaks).

## Redacting secrets

Add `secrets: true` to a redact block. The target field is scanned and any detected secrets are replaced with `[REDACTED:<rule-id>]`, where `<rule-id>` identifies the gitleaks pattern that matched (for example, `[REDACTED:aws-access-token]`).

```yaml
rules:
  - name: redact-secrets-in-tool-results
    match:
      operation: "llm.tool_result"
    action: redact
    redact:
      target: "params.content"
      secrets: true
```

This rule scans every tool result before it reaches the model. If the tool returned text containing `AKIAIOSFODNN7EXAMPLE`, the model sees `[REDACTED:aws-access-token]` instead.

### Combining with custom patterns

When a redact rule has both `secrets: true` and custom `patterns`, gitleaks patterns run first. Custom regex patterns then run on the already-redacted text. This means custom patterns will not interfere with secret placeholders, and you can layer additional redaction on top.

```yaml
rules:
  - name: redact-all-sensitive
    match:
      operation: "llm.tool_result"
    action: redact
    redact:
      target: "params.content"
      secrets: true
      patterns:
        - match: "[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}"
          replace: "[REDACTED:email]"
```

## Checking for secrets with `hasSecrets()`

The `hasSecrets()` CEL function detects secrets without redacting them. It takes a string and returns `true` if gitleaks finds any matches. This is useful for deny rules that should block calls containing leaked credentials rather than silently redacting them.

```yaml
rules:
  - name: deny-leaked-credentials
    match:
      operation: "llm.text"
      when: "hasSecrets(params.text)"
    action: deny
    message: "Request contains credentials. Remove secrets before sending."
```

## Example: full rule file

This rule file redacts secrets from both user text and tool results, and logs all LLM operations for auditing:

```yaml
scope: my-gateway
mode: enforce

rules:
  - name: redact-secrets-in-text
    match:
      operation: "llm.text"
    action: redact
    redact:
      target: "params.text"
      secrets: true

  - name: redact-secrets-in-tool-results
    match:
      operation: "llm.tool_result"
    action: redact
    redact:
      target: "params.content"
      secrets: true

  - name: audit-all
    match:
      operation: "llm.*"
    action: log
```

## Verify it works

Test secret detection by writing a fixture that sends a known secret and expects redaction:

```yaml
# fixtures/secret-test.yaml
scope: my-gateway
tests:
  - name: "redacts AWS key in tool result"
    call:
      operation: "llm.tool_result"
      params:
        content: "key is AKIAIOSFODNN7REALKEY"
    expect:
      decision: "redact"
```

Run the fixture with `keep test`:

```bash
$ keep test ./rules --fixtures ./fixtures/secret-test.yaml
```

The test passes when the redact action fires, replacing the AWS key with `[REDACTED:aws-access-token]`.

## Limitations

- **Pattern-based, not semantic.** Detection relies on regex patterns matching known credential formats. Secrets that do not match a gitleaks pattern -- such as custom internal tokens or credentials with non-standard formats -- are not detected.
- **False positives on test data.** Strings that happen to match credential formats (for example, test fixtures or documentation examples) may be flagged. Gitleaks has built-in allowlists for known example values like `AKIAIOSFODNN7EXAMPLE`, but not all test data is excluded.
- **No cross-field analysis.** Each field is scanned independently. The detector does not correlate values across multiple fields.
- **Local detection only.** Secrets are detected at evaluation time in the Keep process. There is no external service call or database lookup involved.