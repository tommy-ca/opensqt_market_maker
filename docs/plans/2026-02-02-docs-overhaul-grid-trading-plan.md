---
title: Overhaul Grid Trading Documentation
type: docs
date: 2026-02-02
---

# Overhaul Grid Trading Documentation

## Overview

The Grid Trading system has evolved into a sophisticated "Dual-Engine" architecture (Simple & Durable) with complex Protocol Buffer data models. However, the current documentation (`market_maker_design.md`) is monolithic and outdated, mixing high-level design with low-level implementation details.

This plan executes a comprehensive documentation overhaul to separate concerns, reflect the current architecture, and provide actionable guides for operators and developers.

## Problem Statement

*   **Outdated Architecture**: Current docs don't clearly distinguish between the `Simple` (dev/test) and `Durable` (DBOS/prod) engines.
*   **Missing Operational Context**: Operators handling "Risk Mode" alerts or stuck slots have no clear runbook.
*   **Developer Friction**: New developers struggle to find the "source of truth" for data models, often looking at Go structs instead of the definitive `.proto` files.

## Proposed Solution

Split the documentation into three focused files based on User Intent:

1.  **Architecture & Design** (`market_maker_design.md`): The "Why" and "What". Focus on Strategy Logic and Dual-Engine patterns.
2.  **Technical Reference** (`technical_reference.md`): The "Deep Dive". Definitive guide to Protobufs, Interfaces, and Developer Workflows (e.g., "How to compile protos").
3.  **Operations Guide** (`operations_guide.md`): The "Runbook". Actionable steps for Configuration, Monitoring, and Emergency Recovery.

## Implementation Phases

### Phase 1: Preparation & Skeleton

*   [ ] Create `market_maker/docs/specs/technical_reference.md` skeleton.
*   [ ] Create `market_maker/docs/specs/operations_guide.md` skeleton.
*   [ ] Add a "Documentation Map" (Dispatch Section) to the top of `market_maker_design.md` pointing to the new files.

### Phase 2: Technical Reference (The Developer View)

*   [ ] **Data Models**: Document `InventorySlot` and `State` based on `market_maker/api/proto/opensqt/market_maker/v1/state.proto`.
*   [ ] **Interfaces**: Document the `Engine` and `Store` interfaces found in `market_maker/internal/engine/interfaces.go`.
*   [ ] **Workflows**: Add a section on "Working with Protobufs" (compilation steps).
*   [ ] **Durable Engine**: Document the DBOS integration specifics found in `market_maker/internal/engine/durable/`.

### Phase 3: Operations Guide (The Operator View)

*   [ ] **Emergency Runbooks**: Create "Risk Mode Triggered" and "Stuck Slot Recovery" sections (based on `RiskMonitor` logic).
*   [ ] **Configuration**: Document `StrategyConfig` parameters (GridSize, SkewFactor) and their effects.
*   [ ] **Monitoring**: List key metrics (PNL, Skew, Active Slots) to watch in Grafana.

### Phase 4: Design Doc Refactor (The Architect View)

*   [ ] **Refactor**: Remove low-level struct definitions and configuration tables from `market_maker_design.md`.
*   [ ] **Enhance**: Clarify the "Dual-Engine" architecture diagram/description.
*   [ ] **Focus**: Ensure the Strategy Logic (ATR scaling, Inventory Skew) is explained conceptually without code dumping.

## Acceptance Criteria

*   [ ] `market_maker_design.md` is focused on theory and high-level architecture.
*   [ ] `technical_reference.md` accurately reflects the `.proto` definitions.
*   [ ] `operations_guide.md` contains at least two concrete runbooks ("Risk Mode" and "Stuck Slots").
*   [ ] All three documents cross-reference each other correctly.
*   [ ] No broken links remain in the `docs/` directory.

## References

*   **Brainstorm**: `docs/brainstorms/2026-02-02-grid-docs-overhaul-brainstorm.md`
*   **Original Doc**: `market_maker/docs/specs/market_maker_design.md`
*   **Code Sources**:
    *   Strategy: `market_maker/internal/trading/grid/strategy.go`
    *   Engine: `market_maker/internal/engine/gridengine/engine.go`
    *   Protos: `market_maker/api/proto/opensqt/market_maker/v1/`
