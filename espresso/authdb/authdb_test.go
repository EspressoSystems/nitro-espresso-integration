package authdb

import (
	"testing"

	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/dbtest"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"

	"github.com/offchainlabs/nitro/cmd/util/integrityattestation"
	"github.com/offchainlabs/nitro/util/testhelpers"
)

func Require(t *testing.T, err error, printables ...any) {
	t.Helper()
	testhelpers.RequireImpl(t, err, printables...)
}

func Assert(t *testing.T, cond bool, printables ...any) {
	t.Helper()
	if !cond {
		testhelpers.FailImpl(t, printables...)
	}
}

func RequireBench(b *testing.B, err error, printables ...any) {
	b.Helper()
	testhelpers.RequireImpl(b, err, printables...)
}

func TestAuthDB(t *testing.T) {
	t.Run("AuthDBSuite", func(t *testing.T) {
		dbtest.TestDatabaseSuite(t, func() ethdb.KeyValueStore {
			db, err := rawdb.NewDatabaseWithFreezer(memorydb.New(), "authdbancient", "authdbtest", false)
			Require(t, err)

			hmac, err := integrityattestation.GenerateHMAC()
			Require(t, err)
			authdb, err := NewAuthDB(db, hmac)
			Require(t, err)

			return &authdb
		})
	})

}

func BenchmarkAuthDB(b *testing.B) {
	dbtest.BenchDatabaseSuite(b, func() ethdb.KeyValueStore {
		db, err := rawdb.NewDatabaseWithFreezer(memorydb.New(), "authdbancient", "authdbtest", false)
		RequireBench(b, err)

		hmac, err := integrityattestation.GenerateHMAC()
		RequireBench(b, err)
		authdb, err := NewAuthDB(db, hmac)
		RequireBench(b, err)

		return &authdb
	})
}
