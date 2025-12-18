package espressotee

import (
	"math/big"
	"testing"
	"time"
)

func TestBaseFeeCheck_NilBaseFee(t *testing.T) {
	t.Run("retries when base fee is nil", func(t *testing.T) {
		attempts := 0
		fn := func() (*big.Int, error) {
			attempts++
			if attempts < 2 {
				return nil, nil
			}
			return big.NewInt(1000000), nil // Succeed on 3rd retry
		}

		err := BaseFeeCheck(10000000, 3, 10*time.Millisecond, fn, "test")
		if err != nil {
			t.Errorf("Expected success after retry, got: %v", err)
		}
		if attempts != 2 {
			t.Errorf("Expected 2 attempts, got %d", attempts)
		}
	})

	t.Run("fails when base fee always nil", func(t *testing.T) {
		attempts := 0
		fn := func() (*big.Int, error) {
			attempts++
			return nil, nil // Always nil
		}

		err := BaseFeeCheck(10000000, 3, 10*time.Millisecond, fn, "test")
		if err == nil {
			t.Error("Expected error when base fee always nil")
		}
		if attempts != 3 {
			t.Errorf("Expected 3 attempts, got %d", attempts)
		}
	})
}
