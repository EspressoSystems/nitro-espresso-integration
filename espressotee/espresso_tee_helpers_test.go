package espressotee

import (
	"fmt"
	"math/big"
	"testing"
	"time"
)

func TestBaseFeeCheck_NilBaseFee(t *testing.T) {
	t.Run("nil base fee on all attempts", func(t *testing.T) {
		attemptCount := 0
		fn := func() (*big.Int, error) {
			attemptCount++
			// Always return nil
			return nil, nil
		}

		err := BaseFeeCheck(
			10000000, // maxBaseFee
			3,        // maxRetries
			10*time.Millisecond,
			fn,
			"test message",
		)

		// Should return error when base fee is always nil
		if err == nil {
			t.Error("Expected error when base fee is always nil, but got nil")
		}

		// Should have retried maxRetries times
		if attemptCount != 3 {
			t.Errorf("Expected 3 attempts, but got %d", attemptCount)
		}
	})

	t.Run("nil base fee then valid base fee", func(t *testing.T) {
		attemptCount := 0
		fn := func() (*big.Int, error) {
			attemptCount++
			// Return nil first two times, then valid value
			if attemptCount < 3 {
				return nil, nil
			}
			// Return a valid low base fee
			return big.NewInt(1000000), nil
		}

		err := BaseFeeCheck(
			10000000, // maxBaseFee
			5,        // maxRetries
			10*time.Millisecond,
			fn,
			"test message",
		)

		// Should succeed after retries
		if err != nil {
			t.Errorf("Expected success after retries, but got error: %v", err)
		}

		// Should have retried until success
		if attemptCount != 3 {
			t.Errorf("Expected 3 attempts, but got %d", attemptCount)
		}
	})

	t.Run("nil base fee with error", func(t *testing.T) {
		attemptCount := 0
		fn := func() (*big.Int, error) {
			attemptCount++
			// Return nil with error
			return nil, fmt.Errorf("network error")
		}

		err := BaseFeeCheck(
			10000000, // maxBaseFee
			3,        // maxRetries
			10*time.Millisecond,
			fn,
			"test message",
		)

		// Should eventually fail after retries
		if err == nil {
			t.Error("Expected error after all retries fail, but got nil")
		}

		// Should have retried maxRetries times
		if attemptCount != 3 {
			t.Errorf("Expected 3 attempts, but got %d", attemptCount)
		}
	})
}

func TestBaseFeeCheck_NilBaseFeeRetryLogic(t *testing.T) {
	attemptCount := 0
	maxRetries := 3
	retryDelay := 10 * time.Millisecond

	fn := func() (*big.Int, error) {
		attemptCount++
		// Return nil for first two attempts
		if attemptCount < maxRetries {
			return nil, nil
		}
		// Return valid base fee on last attempt
		return big.NewInt(5000000), nil
	}

	startTime := time.Now()
	err := BaseFeeCheck(
		10000000, // maxBaseFee
		maxRetries,
		retryDelay,
		fn,
		"test nil retry",
	)
	duration := time.Since(startTime)

	if err != nil {
		t.Errorf("Expected success after retries, but got error: %v", err)
	}

	// Verify retry delay was respected (should have waited at least retryDelay * (maxRetries-1))
	expectedMinDuration := retryDelay * time.Duration(maxRetries-1)
	if duration < expectedMinDuration {
		t.Errorf("Expected duration >= %v, but got %v", expectedMinDuration, duration)
	}

	// Verify we retried the expected number of times
	if attemptCount != maxRetries {
		t.Errorf("Expected %d attempts, but got %d", maxRetries, attemptCount)
	}
}
