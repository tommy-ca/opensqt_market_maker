---
status: completed
priority: p2
issue_id: "027"
tags: [architecture, code-review]
dependencies: []
---

# Problem Statement
PortfolioController is tightly coupled with arbengine.EngineConfig, making it difficult to test in isolation and less flexible to configuration changes.

# Findings
PortfolioController directly references arbengine.EngineConfig for its initialization and operation. This dependency chain complicates the architecture and hinders modularity.

# Proposed Solutions
- Define an interface for the required configuration parameters.
- Use dependency injection to provide configuration to PortfolioController.
- Decouple PortfolioController from the specific EngineConfig struct.

# Recommended Action
Refactor PortfolioController to depend on an interface rather than the concrete EngineConfig struct.

# Acceptance Criteria
- [x] PortfolioController decoupled from arbengine.EngineConfig.
- [x] Unit tests for PortfolioController can be written without mocking the entire EngineConfig.
- [x] Improved modularity in the engine architecture.

# Work Log
### 2026-02-01 - Initial Todo Creation
**By:** opencode
**Actions:**
- Created todo for addressing engine config coupling.
