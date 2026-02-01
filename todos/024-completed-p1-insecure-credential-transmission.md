---
status: completed
priority: p1
issue_id: "024"
tags: [security, critical, code-review]
dependencies: []
---

# Problem Statement
In the Python connector, Bearer tokens are transmitted in plaintext headers when TLS is not explicitly enabled. This exposes sensitive credentials to potential interception on the network.

# Findings
- The Python connector's gRPC client doesn't enforce TLS by default.
- Authentication interceptors attach the Bearer token to every request regardless of the transport security status.

# Proposed Solutions
1. **Enforce TLS**: Modify the connector to refuse sending credentials over non-TLS connections.
2. **Secure Credentials**: Use `grpc.ssl_channel_credentials()` and ensure they are required for any authenticated call.

# Recommended Action
Update the Python connector to require a secure channel before attaching Bearer tokens to gRPC metadata.

# Acceptance Criteria
- [x] Python connector raises an error if an attempt is made to send credentials over an insecure channel.
- [x] Documentation updated to reflect the requirement for TLS when using authentication.
- [x] Unit tests verify that credentials are not sent over plaintext channels.

# Work Log
### 2026-02-01 - Todo Created
**By:** opencode
**Actions:** Created todo to address insecure credential transmission security risk.
