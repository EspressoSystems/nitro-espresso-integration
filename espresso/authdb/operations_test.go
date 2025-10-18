package authdb

import (
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
)

func TestSecurityEnforcement(t *testing.T) {
	// Create a plain memorydb (not AuthDB)
	plainDB, err := rawdb.NewDatabaseWithFreezer(memorydb.New(), "testancient", "test", false)
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer plainDB.Close()

	// This should fail because plainDB is not *AuthDB or *AuthBatch
	err = WriteNextHotshotBlockNum(plainDB, 123)
	if err == nil {
		t.Fatal("expected error when using plain memorydb, but got nil")
	}

	expectedErrMsg := "db must be *AuthDB or *AuthBatch to ensure authenticated operations"
	if !strings.Contains(err.Error(), expectedErrMsg) {
		t.Fatalf("expected error message to contain %q, got %q", expectedErrMsg, err.Error())
	}

	// Test with AuthDB - this should work
	authDB, err := NewAuthDB(plainDB, nil)
	if err != nil {
		t.Fatalf("failed to create AuthDB: %v", err)
	}

	err = WriteNextHotshotBlockNum(&authDB, 123)
	if err != nil {
		t.Fatalf("expected no error with AuthDB, got %v", err)
	}

	// Verify it was stored correctly
	value, err := ReadNextHotshotBlockNum(&authDB)
	if err != nil {
		t.Fatalf("failed to read value: %v", err)
	}

	if value != 123 {
		t.Fatalf("expected value 123, got %d", value)
	}
}

