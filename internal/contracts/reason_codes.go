package contracts

// ReasonCode is a stable machine-readable code attached to per-issue results.
type ReasonCode string

const (
	ReasonCodeConflictFieldChangedBoth     ReasonCode = "conflict_field_changed_both"
	ReasonCodeConflictBaseSnapshotMissing  ReasonCode = "conflict_base_snapshot_missing"
	ReasonCodeDescriptionRiskyBlocked      ReasonCode = "description_risky_blocked"
	ReasonCodeDescriptionADFBlockMissing   ReasonCode = "description_adf_block_missing"
	ReasonCodeDescriptionADFBlockMalformed ReasonCode = "description_adf_block_malformed"
	ReasonCodeTransitionAmbiguous          ReasonCode = "transition_ambiguous"
	ReasonCodeTransitionUnavailable        ReasonCode = "transition_unavailable"
	ReasonCodeUnsupportedFieldIgnored      ReasonCode = "unsupported_field_ignored"
	ReasonCodeValidationFailed             ReasonCode = "validation_failed"
	ReasonCodeAuthFailed                   ReasonCode = "auth_failed"
	ReasonCodeTransportError               ReasonCode = "transport_error"
	ReasonCodeLockAcquireFailed            ReasonCode = "lock_acquire_failed"
	ReasonCodeLockStaleRecovered           ReasonCode = "lock_stale_recovered"
	ReasonCodeDryRunNoWrite                ReasonCode = "dry_run_no_write"
	ReasonCodeTempIDRewriteOutOfScope      ReasonCode = "temp_id_rewrite_out_of_scope"
)

// StableReasonCodes freezes the contract taxonomy and ordering.
var StableReasonCodes = []ReasonCode{
	ReasonCodeConflictFieldChangedBoth,
	ReasonCodeConflictBaseSnapshotMissing,
	ReasonCodeDescriptionRiskyBlocked,
	ReasonCodeDescriptionADFBlockMissing,
	ReasonCodeDescriptionADFBlockMalformed,
	ReasonCodeTransitionAmbiguous,
	ReasonCodeTransitionUnavailable,
	ReasonCodeUnsupportedFieldIgnored,
	ReasonCodeValidationFailed,
	ReasonCodeAuthFailed,
	ReasonCodeTransportError,
	ReasonCodeLockAcquireFailed,
	ReasonCodeLockStaleRecovered,
	ReasonCodeDryRunNoWrite,
	ReasonCodeTempIDRewriteOutOfScope,
}

func IsStableReasonCode(code ReasonCode) bool {
	for _, stable := range StableReasonCodes {
		if stable == code {
			return true
		}
	}
	return false
}
