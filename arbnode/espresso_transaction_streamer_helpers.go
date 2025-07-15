package arbnode

// This file contains a few helper functions to help interoperate with the
// TransactionStreamer and the Espresso code.
//
// These functions are used for testing and configuration purposes, and are
// not intended to general common usage.

import (
	"github.com/offchainlabs/nitro/arbnode/espresso/submitter"
)

// SetEspressoSubmitter sets the EspressoSubmitter for the TransactionStreamer.
//
// Since the espressoSubnmitter is an internal, optional field of the
// TransactionStreamer, this function allows you to set it after the streamer
// has been created.
//
// NOTE: This is intended for testing purposes only.
func SetEspressoSubmitter(
	streamer *TransactionStreamer,
	espressoSubmitter submitter.EspressoSubmitter,
) {
	streamer.espressoSubmitter = espressoSubmitter
}

// GetEspressoSubmitter retrieves the EspressoSubmitter from the
// TransactionStreamer.
func GetEspressoSubmitter(streamer *TransactionStreamer) submitter.EspressoSubmitter {
	if streamer == nil {
		return nil
	}

	if streamer.espressoSubmitter == nil {
		return nil
	}

	return streamer.espressoSubmitter
}

// GetUseEscapeHatch retrieves the UseEscapeHatch setting from the
// TransactionStreamer.
//
// This function checks to see if the escape hatch is the UseEscapeHatch
// is configured to be utilized.
func GetUseEscapeHatch(streamer *TransactionStreamer) bool {
	if streamer == nil {
		return false
	}

	if streamer.espressoSubmitter == nil {
		return false
	}

	switch t := streamer.espressoSubmitter.(type) {
	default:
		return false
	case *submitter.EspressoOriginalSubmitter:
		return t.UseEscapeHatch
	}
}

// SetUseEscapeHatch sets the UseEscapeHatch setting for the TransactionStreamer.
//
// This function allows you to enable or disable the escape hatch feature in the
// EspressoSubmitter.
//
// NOTE: This is intended for testing purposes only.
func SetUseEscapeHatch(streamer *TransactionStreamer, enabled bool) {
	if streamer == nil || streamer.espressoSubmitter == nil {
		return
	}

	switch t := streamer.espressoSubmitter.(type) {
	case *submitter.EspressoOriginalSubmitter:
		t.UseEscapeHatch = enabled
	}
}

// GetEscapeHatchEnabled retrieves the EscapeHatchEnabled setting from the
// TransactionStreamer.
//
// This function checks to see if the escape hatch is enabled in the
// EspressoSubmitter.
func GetEscapeHatchEnabled(streamer *TransactionStreamer) bool {
	if streamer == nil {
		return false
	}

	if streamer.espressoSubmitter == nil {
		return false
	}

	return streamer.espressoSubmitter.IsEscapeHatchEnabled()
}

// SetEscapeHatchEnabled sets the EscapeHatchEnabled setting for the
// TransactionStreamer.
//
// This function allows you to enable or disable the escape hatch feature in the
// EspressoSubmitter.
//
// NOTE: This is intended for testing purposes only.
func SetEscapeHatchEnabled(streamer *TransactionStreamer, enabled bool) {
	if streamer == nil || streamer.espressoSubmitter == nil {
		return
	}

	switch t := streamer.espressoSubmitter.(type) {
	case *submitter.EspressoOriginalSubmitter:
		t.EscapeHatchEnabled = enabled
	}
}

// WithTransactionStreamer is an `EspressoSubmitterConfig` option that
// configures the `EspressoSubmitter` with information provided by the
// given `TransactionStreamer`.
//
// NOTE: This is defined here to avoid circular dependencies between
// `arbnode/espresso/submitter` and `arbnode`
func WithTransactionStreamer(
	streamer *TransactionStreamer,
) func(config *submitter.EspressoSubmitterConfig) {
	config := streamer.config()
	return submitter.WithMultipleOptions(
		submitter.WithChainID(streamer.chainConfig.ChainID.Uint64()),
		submitter.WithMessageGetter(streamer),
		submitter.WithDatabase(streamer.db),
		submitter.WithAttestationFiles(config.UserDataAttestationFile, config.QuoteFile),
	)
}
