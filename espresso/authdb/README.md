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

Ancient data uses two different authentication approaches based on data type:

**Hash Table (Raw bytes):**
```go
stored_data = original_data || HMAC(kind || number || original_data)
```

**Structured Data (Headers/Bodies/Receipts):**
```go
auth_item = AncientItemWithTag{
    item: original_item,
    tag: HMAC(kind || number || RLP(original_item))
}
stored_data = RLP(auth_item)
```

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