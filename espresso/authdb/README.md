# Authenticated Storage (AuthDB) Architecture

This document explains the authenticated storage mechanism designed to ensure data integrity for security-critical operations in the Espresso-Nitro integration.

## Core Security Model

**AuthDB** provides cryptographic integrity guarantees for all database operations using HMAC (Hash-based Message Authentication Code). Every stored value is protected by an authentication tag that prevents tampering and detects corruption.

### Key Principle
All security-critical data passes through AuthDB, which enforces that:
1. **Write operations** automatically generate and store HMAC tags alongside data
2. **Read operations** verify HMAC tags before returning data
3. **Invalid/tampered data** causes read operations to fail with authentication errors

## Architecture Overview

```
Application Layer
        ↓
    AuthDB (wrapper)
        ↓
  Underlying Database (Geth's ethdb)
```

### Core Components

- **`AuthDB`** - Main wrapper implementing `ethdb.Database` interface
- **`AuthBatch`** - Authenticated batch operations for atomic writes
- **`AuthIterator`** - Iterator that skips internal tag entries and validates data
- **`AuthAncientWriteOp`** - Handles freezer (ancient) data with authentication

## Authentication Schemes

### 1. Key-Value Store Authentication
**Location:** `Put()`, `Get()`, `Has()` methods

**Tag Generation:**
```go
tag = HMAC(key || value)
```

**Storage Pattern:**
- Data: `key → value` 
- Tag: `key-tag → HMAC(key || value)`

**Critical Security Properties:**
- Tags stored separately from data prevent value substitution attacks
- Key inclusion in HMAC prevents key confusion attacks
- Constant-time comparison (`hmac.Equal`) prevents timing attacks

### 2. Ancient Store Authentication
**Location:** `Ancient()`, `AncientRange()`, `ModifyAncients()` methods

Ancient data authentication uses a **separate tag freezer** to store HMAC tags independently from the main ancient store.

**Directory Structure:**
```
<ancient_dir>/
├── chain/          # Main chain freezer (unchanged format)
│   ├── headers
│   ├── bodies
│   ├── receipts
│   └── hashes
└── auth-tags/      # Tag freezer (parallel structure)
    ├── tag-headers  # HMAC tags for headers
    ├── tag-bodies   # HMAC tags for bodies
    ├── tag-receipts # HMAC tags for receipts
    └── tag-hashes   # HMAC tags for hashes
```

**Tag Computation:**
```go
// For all ancient data types (raw and structured)
tag = HMAC(kind || number || data)

// For structured data (headers/bodies/receipts), data is RLP-encoded
tag = HMAC(kind || number || RLP(item))
```

**Storage Pattern:**
- **Main Data**: Stored unchanged in main chain freezer tables
- **Tags**: Stored at same index in corresponding tag freezer tables

**Atomicity:** Both main data and tags are written together via `ModifyAncients()`. If either write fails, the operation is aborted.

**Modes:**
- **Enabled**: Tag freezer created in `<ancient_dir>/auth-tags/` (requires valid ancient directory)
- **Disabled**: No tags when `mac=nil` (authentication bypassed)

## Security-Critical Functions

### Enforcement Functions (`operations.go`)
```go
func enforceAuthenticatedWriter(db ethdb.KeyValueWriter) error
func enforceAuthenticatedReader(db ethdb.KeyValueReader) error
```

**Purpose:** Compile-time + runtime guarantee that only `AuthDB`/`AuthBatch` instances are used for security-critical operations.

**Audit Focus:** Verify all security-sensitive database operations in the codebase use these enforcement functions.

### Domain-Specific Operations
Functions like `WriteNextHotshotBlockNum`, `ReadInitAddresses` enforce authenticated storage for:
- Hotshot consensus state tracking
- Batcher address monitoring  
- Event processing checkpoints

**Audit Focus:** Confirm these operations cannot be bypassed with direct database access.

## Attack Resistance

### Tampering Detection
- **Bit-flip attacks:** Any corruption in stored data fails HMAC verification
- **Substitution attacks:** Cannot replace values due to key-specific tags
- **Rollback attacks:** Sequence numbers in ancient data prevent replay

### Authentication Bypasses
- **Type confusion:** `enforceAuthenticated*` functions prevent use of raw database
- **Tag forgery:** HMAC requires secret key unknown to attackers
- **Timing attacks:** Constant-time comparison prevents tag extraction

## Key Security Boundaries

### 1. HMAC Key Management
**Location:** `NewAuthDB(db ethdb.Database, mac hash.Hash)`

**Security Requirement:** The `mac` parameter must be initialized with a cryptographically secure key. This key represents the root of trust for all authentication.

**Initialization Requirement:** When `mac` is non-nil (authentication enabled), the database must provide a valid ancient directory via `AncientDatadir()`. If the ancient directory is missing or empty, `NewAuthDB` will return an error. This ensures tag persistence and prevents security degradation.

**Audit Focus:** Verify HMAC key derivation and lifecycle management in calling code.

### 2. Authentication Bypass Prevention
**Critical Code Paths:**
- All `Write*` and `Read*` functions in `operations.go` call enforcement functions
- Iterator skips tag entries to prevent information disclosure
- Batch operations maintain authentication invariants

**Audit Focus:** Confirm no code paths exist that bypass AuthDB for security-critical data.

### 3. Ancient Data Integrity
**Critical Invariant:** Ancient data authentication handles both metadata (kind, number) and content in HMAC computation.

**Audit Focus:** Verify freezer data cannot be modified without triggering authentication failures.

## Testing Verification

### Authentication Test Coverage
- **`authdb_test.go`:** Full database suite compliance testing
- **`operations_test.go`:** Security enforcement verification

**Key Test:** `TestSecurityEnforcement` confirms raw database access fails with clear error messages.

### Audit Recommendations
1. **Static Analysis:** Verify no direct `ethdb.Database` usage for security-critical data
2. **Integration Testing:** Confirm authentication failures halt system operation
3. **Key Management Review:** Audit HMAC key generation and rotation procedures
4. **Performance Impact:** Measure authentication overhead in production scenarios

## Non-Security Operations

Several database operations remain unauthenticated by design:
- **Metadata operations:** `Ancients()`, `Tail()`, `AncientSize()` (not security-sensitive)
- **Maintenance operations:** `Sync()`, `Compact()`, `Close()` (operational, not data integrity)

**Rationale:** These operations don't affect data integrity and authentication would add unnecessary overhead.

### WASM Database (Stylus Compilation Cache)

**Authentication Status:** **SKIPPED** - WASM DB operations are not authenticated.

**Location:** The WASM database stores pre-compiled native machine code for Stylus smart contracts, accessible via `WasmStore()` in the wrapped database.

**Directory Structure:**
```
<datadir>/
├── chaindata/     # Main authenticated chain database
└── wasm/          # Unauthenticated WASM compilation cache
```

**Why WASM DB is Persisted:**

Stylus programs (WebAssembly smart contracts) are compiled to native machine code (ARM64/AMD64) when first activated on-chain. This compilation is computationally expensive, so compiled artifacts are cached to disk:

1. **Contract Activation** (`arbos/programs/native.go`): When a Stylus contract is deployed/activated, WASM bytecode is compiled to native machine code for all configured targets (WAVM, ARM64, AMD64)
2. **StateDB Tracking** (`statedb_arbitrum.go`): Compiled artifacts are tracked in `StateDB.arbExtraData.activatedWasms` during transaction execution
3. **Block Commit** (`statedb.go:1418-1428`): On block commit, activated WASMs are persisted to the WASM DB via `WriteActivation()` to avoid recompilation
4. **Cache Lookup** (`native.go:236-291`): On subsequent executions, `getLocalAsm()` checks StateDB → WASM DB → recompile as fallback

**Security Rationale for Skipping Authentication:**

WASM DB corruption affects **performance only, not correctness**:

- **Fraud Proof Generation:** WASM compilation is part of fraud proof verification, but tampering only impacts efficiency (forces recompilation). The fraud proof system validates execution results, not compilation artifacts.
- **Block Processing:** If WASM DB is corrupted/tampered:
  - Node detects mismatch between stored `moduleHash` and recompiled hash
  - Falls back to on-the-fly recompilation from on-chain WASM bytecode
  - Execution results remain deterministic and verifiable
- **No State Transition Impact:** WASM DB never participates in consensus-critical state transitions. The canonical state is always derived from on-chain WASM bytecode in `StateDB`, not from the compilation cache.

**Trade-off:** Authenticating WASM DB would add significant overhead for minimal security benefit, as the system self-heals through recompilation and hash verification.
