# Software Design Document (SDD) - OpenCode Integration

## 1. Introduction

### 1.1 Purpose
This Software Design Document (SDD) outlines the integration of OpenCode CLI tool capabilities into the OpenSQT Market Maker system. The integration aims to enhance development workflow, ensure security compliance, and provide automated tooling for software engineering tasks.

### 1.2 Scope
The SDD covers:
- CLI tool integration architecture
- Security and safety mechanisms
- Development workflow enhancements
- Test-driven development approach

## 2. System Overview

### 2.1 Architecture
The OpenCode integration adds a CLI layer to the existing market maker architecture:

```
[OpenCode CLI Tools]
        ↓
[Development & Operations]
        ↓
[Market Maker Core System]
```

### 2.2 Components

#### 2.2.1 CLI Interface
- **Purpose**: Provide command-line access to development tools
- **Features**:
  - File operations with security validation
  - Code generation and validation
  - Test execution and reporting
  - Documentation synchronization

#### 2.2.2 Security Layer
- **Input Validation**: All inputs validated against malicious patterns
- **Secret Detection**: Automated scanning for credential leaks
- **Access Control**: Role-based permissions for operations

#### 2.2.3 Development Tools
- **Code Analysis**: Static analysis and linting integration
- **Test Automation**: TDD workflow support
- **Documentation**: Automated spec and design updates

## 3. Detailed Design

### 3.1 CLI Command Structure
```
/opencode [command] [options]

Commands:
- analyze: Code analysis and security scanning
- generate: Code and documentation generation
- test: Test execution with TDD support
- docs: Documentation management
- build: Build and deployment automation
```

### 3.2 Security Mechanisms

#### 3.2.1 Input Sanitization
- Regex-based pattern matching for malicious code detection
- Path validation to prevent directory traversal
- Content filtering for sensitive information
- Implemented in `pkg/cli/input_validation.go` with comprehensive tests

#### 3.2.2 Audit Trail
- All operations logged with timestamps
- User authentication for critical operations
- Immutable audit logs for compliance

### 3.3 Integration Points

#### 3.3.1 Build System
- Pre-build security checks
- Automated testing integration
- Dependency vulnerability scanning

#### 3.3.2 Development Workflow
- Git hooks for code quality
- CI/CD pipeline integration
- Automated documentation updates

## 4. Test-Driven Development (TDD) Approach

### 4.1 TDD Workflow
1. **Red**: Write failing test for new feature
2. **Green**: Implement minimal code to pass test
3. **Refactor**: Improve code while maintaining tests
4. **Verify**: Run full test suite and linting

### 4.2 Test Categories
- **Unit Tests**: Individual component testing
- **Integration Tests**: Component interaction validation
- **Security Tests**: Vulnerability and safety verification
- **Performance Tests**: Latency and resource usage

### 4.3 Test Automation
- Automated test execution on code changes
- Coverage reporting and thresholds
- Regression testing for critical paths

## 5. Implementation Plan

### 5.1 Phase 1: Core CLI Integration
- Implement basic CLI commands
- Add security validation layer
- Integrate with existing build system

### 5.2 Phase 2: TDD Framework
- Set up test automation pipeline
- Implement TDD workflow tools
- Add test coverage monitoring

### 5.3 Phase 3: Advanced Features
- Documentation automation
- Security scanning integration
- Performance monitoring tools

## 6. Security Considerations

### 6.1 Threat Model
- Malicious code injection through CLI
- Unauthorized access to sensitive operations
- Data leakage through logs or outputs

### 6.2 Mitigation Strategies
- Input validation and sanitization
- Role-based access control
- Encrypted logging and secure storage

## 7. Performance Requirements

### 7.1 Response Times
- CLI command execution: < 500ms
- Security scanning: < 2s for typical codebase
- Test execution: < 30s for full suite

### 7.2 Resource Usage
- Memory footprint: < 100MB during operations
- CPU usage: Minimal impact on development machines
- Disk I/O: Efficient caching and incremental operations

## 8. Maintenance and Support

### 8.1 Monitoring
- CLI usage analytics
- Error rate tracking
- Performance metric collection

### 8.2 Updates
- Automated security updates
- Feature enhancement based on usage patterns
- Backward compatibility maintenance

## 9. Go Vet Compliance & Code Quality (Phase 9)

### 9.1 Mutex Copying Prevention
**Problem**: Protobuf messages contain internal mutexes that should not be copied by value.
**Solution**: All protobuf message parameters must be passed as pointers.

**Implementation**:
- Updated all `IExchange` interface methods to use pointer callbacks
- Updated `IPriceMonitor.SubscribePriceChanges()` to return `<-chan *pb.PriceChange`
- Updated `IStrategy.CalculateActions()` to return `[]*pb.OrderAction`
- Updated `IPositionManager` methods to use pointer parameters and return types
- Updated all engine implementations to handle pointer-based APIs

### 9.2 Testing Strategy
**TDD Approach**:
1. Write tests first using pointer-based APIs
2. Implement minimum code to pass tests
3. Refactor while maintaining test coverage
4. Verify go vet passes with zero warnings

### 9.3 Affected Components
- All exchange adapters (Binance, Bitget, OKX, Bybit, Gate)
- **Universe Selector**: Integrated liquidity-based filtering via new `GetTickers` API.
- PriceMonitor and order streaming
- GridStrategy and SuperPositionManager
- SimpleEngine and DurableEngine
- Reconciler and risk components