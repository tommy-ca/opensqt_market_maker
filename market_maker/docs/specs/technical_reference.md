# Technical Reference: Grid Trading System

This document provides a deep dive into the technical details of the Grid Trading system, serving as the definitive guide for developers.

## Table of Contents

1. [Data Models (Protobuf)](#data-models-protobuf)
2. [Core Interfaces](#core-interfaces)
3. [Durable Engine (DBOS)](#durable-engine-dbos)
4. [Developer Workflows](#developer-workflows)

## Data Models (Protobuf)

The system uses Protocol Buffers as the source of truth for data models. See `market_maker/api/proto/opensqt/market_maker/v1/`.

### InventorySlot

Defined in `state.proto`. Represents a single grid level.

### State

Defined in `state.proto`. Represents the global checkpointable state.

## Core Interfaces

Defined in `market_maker/internal/engine/interfaces.go`.

### Engine

The main orchestrator interface.

### Store

The persistence abstraction.

## Durable Engine (DBOS)

Integration specifics for the Durable Engine.

## Developer Workflows

### Working with Protobufs

How to compile and version Protobuf files.
