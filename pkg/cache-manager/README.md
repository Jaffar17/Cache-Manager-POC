# Cache Manager - Comprehensive Guide

## 📋 Table of Contents
- [Overview](#overview)
- [Architecture & Flow](#architecture--flow)
- [Key Features](#key-features)
- [Use Cases](#use-cases)
- [Integration Guide](#integration-guide)
- [API Reference](#api-reference)
- [Best Practices](#best-practices)

---

## 🎯 Overview

**Cache Manager** is a production-ready, multi-level caching library for Go that provides:

- **Two-tier caching**: L1 (in-memory/BigCache) + L2 (distributed/Redis)
- **Flexible modes**: Choose your caching strategy at service or endpoint level
- **Type-safe operations**: Generic serialization with compile-time safety
- **Smart warmup**: Automatic L1 population from L2 hits
- **Per-call overrides**: Fine-grained control when needed
- **Strict validation**: Catch misconfigurations at initialization, not runtime

### When to Use Cache Manager

✅ **Use when:**
- You need fast, distributed caching across multiple service instances
- You want to reduce database/API load with intelligent caching layers
- You need flexibility to cache different data types with different strategies
- You want automatic cache warmup without manual orchestration
- You need both speed (L1) and reliability (L2) in production

❌ **Don't use when:**
- Simple in-memory caching is sufficient (use `sync.Map` or similar)
- You don't need distributed caching
- Cache invalidation patterns are extremely complex

---

## 🏗️ Architecture & Flow

### Component Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      Application Layer                       │
│                  (Your Service / Handlers)                   │
└────────────────────────┬────────────────────────────────────┘
                         │
                         │ Cache Interface
                         │ (Get, Set, Delete)
                         ▼
┌─────────────────────────────────────────────────────────────┐
│                   MultiLevelCache                            │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  Mode Configuration (ModeBothLevels/L1Only/L2Only)   │   │
│  │  • Default behavior per service                      │   │
│  │  • Per-call override support (when both levels exist)│   │
│  │  • Automatic warmup control                          │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────┬──────────────────────────┬────────────────────┘
              │                          │
       ┌──────▼───────┐          ┌────────▼──────────┐
       │   L1 Cache   │          │   L2 Cache        │
       │  (BigCache)  │          │    (Redis)        │
       │              │          │                   │
       │ • In-Memory  │          │ • Distributed     │
       │ • Ultra-fast │          │ • Non-Persistent  │
       │ • Per-node   │          │   or Persistent   │
       │              │          │ • Shared          │
       └──────────────┘          └───────────────────┘
```

See full documentation at [Cache Manager Package README](/Users/jafferabbas/GolandProjects/Laam-Go/pkg/cache-manager/README.md) for detailed architecture diagrams, use cases, and best practices.

