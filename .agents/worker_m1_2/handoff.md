# Handoff Report — Worker 2 (Milestone 1 / Issue #7: Capability Manifest & Handshake Protocol — Iteration 2 Remediation)

## 1. Observation

- **Task Scope**: Remediation of `CapabilityManifest` bitmask handling for Issue #7 Iteration 2.
- **Files Modified**:
  - `pkg/protocol/schema.go`: Added unexported fields `rawBitmask uint64` and `hasRawBitmask bool` to `CapabilityManifest` struct.
  - `pkg/protocol/capability.go`: Updated `FromBitmask`, `ToBitmask`, `HasCapability`, and `EvaluateAchievableLevel`.
  - `pkg/protocol/capability_test.go`: Added `TestChallenger_BoundaryBitmasks` asserting boundary bitmasks, zero bitmask, full bitmask, and isolated flag behavior.

- **Exact Terminal Output of `go test -v -count=1 -race ./pkg/protocol/...`**:
```
=== RUN   TestCapabilityFlag_ConstantsAndStringer
--- PASS: TestCapabilityFlag_ConstantsAndStringer (0.00s)
=== RUN   TestCapabilityManifest_BitmaskHelpers
--- PASS: TestCapabilityManifest_BitmaskHelpers (0.00s)
=== RUN   TestEvaluateAchievableLevel
=== RUN   TestEvaluateAchievableLevel/nil_manifest
=== RUN   TestEvaluateAchievableLevel/zero_manifest
=== RUN   TestEvaluateAchievableLevel/level_1_manifest
=== RUN   TestEvaluateAchievableLevel/level_2_manifest
=== RUN   TestEvaluateAchievableLevel/level_3_manifest
=== RUN   TestEvaluateAchievableLevel/booleans_without_ToolInspection_yields_level_0
=== RUN   TestEvaluateAchievableLevel/booleans_with_IntegrationLevel_1_yields_level_1
=== RUN   TestEvaluateAchievableLevel/booleans_with_IntegrationLevel_2_yields_level_2
--- PASS: TestEvaluateAchievableLevel (0.00s)
    --- PASS: TestEvaluateAchievableLevel/nil_manifest (0.00s)
    --- PASS: TestEvaluateAchievableLevel/zero_manifest (0.00s)
    --- PASS: TestEvaluateAchievableLevel/level_1_manifest (0.00s)
    --- PASS: TestEvaluateAchievableLevel/level_2_manifest (0.00s)
    --- PASS: TestEvaluateAchievableLevel/level_3_manifest (0.00s)
    --- PASS: TestEvaluateAchievableLevel/booleans_without_ToolInspection_yields_level_0 (0.00s)
    --- PASS: TestEvaluateAchievableLevel/booleans_with_IntegrationLevel_1_yields_level_1 (0.00s)
    --- PASS: TestEvaluateAchievableLevel/booleans_with_IntegrationLevel_2_yields_level_2 (0.00s)
=== RUN   TestNegotiateLevel_Matrix
=== RUN   TestNegotiateLevel_Matrix/level_0_exact_match
=== RUN   TestNegotiateLevel_Matrix/level_3_exact_match
=== RUN   TestNegotiateLevel_Matrix/over-capable_agent_requesting_level_1_with_level_3_manifest
=== RUN   TestNegotiateLevel_Matrix/degradation_from_3_to_1
=== RUN   TestNegotiateLevel_Matrix/total_degradation_from_3_to_0
--- PASS: TestNegotiateLevel_Matrix (0.00s)
    --- PASS: TestNegotiateLevel_Matrix/level_0_exact_match (0.00s)
    --- PASS: TestNegotiateLevel_Matrix/level_3_exact_match (0.00s)
    --- PASS: TestNegotiateLevel_Matrix/over-capable_agent_requesting_level_1_with_level_3_manifest (0.00s)
    --- PASS: TestNegotiateLevel_Matrix/degradation_from_3_to_1 (0.00s)
    --- PASS: TestNegotiateLevel_Matrix/total_degradation_from_3_to_0 (0.00s)
=== RUN   TestNegotiateLevel_EdgeCases
=== RUN   TestNegotiateLevel_EdgeCases/nil_request
=== RUN   TestNegotiateLevel_EdgeCases/empty_session_ID
=== RUN   TestNegotiateLevel_EdgeCases/invalid_requested_level_negative
=== RUN   TestNegotiateLevel_EdgeCases/invalid_requested_level_overflow
--- PASS: TestNegotiateLevel_EdgeCases (0.00s)
    --- PASS: TestNegotiateLevel_EdgeCases/nil_request (0.00s)
    --- PASS: TestNegotiateLevel_EdgeCases/empty_session_ID (0.00s)
    --- PASS: TestNegotiateLevel_EdgeCases/invalid_requested_level_negative (0.00s)
    --- PASS: TestNegotiateLevel_EdgeCases/invalid_requested_level_overflow (0.00s)
=== RUN   TestNegotiateLevel_ConcurrentRace
--- PASS: TestNegotiateLevel_ConcurrentRace (0.01s)
=== RUN   TestChallenger_BoundaryBitmasks
=== RUN   TestChallenger_BoundaryBitmasks/Zero_bitmask
=== RUN   TestChallenger_BoundaryBitmasks/Full_uint64_bitmask_(all_bits_set)_bit19
=== RUN   TestChallenger_BoundaryBitmasks/Full_uint64_bitmask_(all_bits_set)_bit63
=== RUN   TestChallenger_BoundaryBitmasks/Bit_19_only_(CapSDK)
=== RUN   TestChallenger_BoundaryBitmasks/Bit_20_(undefined_flag)
=== RUN   TestChallenger_BoundaryBitmasks/Bit_63_(highest_uint64_bit)
=== RUN   TestChallenger_BoundaryBitmasks/Level_1_required_mask_minus_CapPause_(off-by-one_flag)
=== RUN   TestChallenger_BoundaryBitmasks/Level_2_required_mask_minus_CapCheckpoint
=== RUN   TestChallenger_BoundaryBitmasks/Level_3_required_mask_minus_CapSwitchModel_bit5
=== RUN   TestChallenger_BoundaryBitmasks/Level_3_required_mask_minus_CapSwitchModel_bit6
=== RUN   TestChallenger_BoundaryBitmasks/Level_3_required_mask_minus_CapSwitchModel_bit13
--- PASS: TestChallenger_BoundaryBitmasks (0.00s)
    --- PASS: TestChallenger_BoundaryBitmasks/Zero_bitmask (0.00s)
    --- PASS: TestChallenger_BoundaryBitmasks/Full_uint64_bitmask_(all_bits_set)_bit19 (0.00s)
    --- PASS: TestChallenger_BoundaryBitmasks/Full_uint64_bitmask_(all_bits_set)_bit63 (0.00s)
    --- PASS: TestChallenger_BoundaryBitmasks/Bit_19_only_(CapSDK) (0.00s)
    --- PASS: TestChallenger_BoundaryBitmasks/Bit_20_(undefined_flag) (0.00s)
    --- PASS: TestChallenger_BoundaryBitmasks/Bit_63_(highest_uint64_bit) (0.00s)
    --- PASS: TestChallenger_BoundaryBitmasks/Level_1_required_mask_minus_CapPause_(off-by-one_flag) (0.00s)
    --- PASS: TestChallenger_BoundaryBitmasks/Level_2_required_mask_minus_CapCheckpoint (0.00s)
    --- PASS: TestChallenger_BoundaryBitmasks/Level_3_required_mask_minus_CapSwitchModel_bit5 (0.00s)
    --- PASS: TestChallenger_BoundaryBitmasks/Level_3_required_mask_minus_CapSwitchModel_bit6 (0.00s)
    --- PASS: TestChallenger_BoundaryBitmasks/Level_3_required_mask_minus_CapSwitchModel_bit13 (0.00s)
=== RUN   TestAdversarialCapability_BitmaskCategoryIntegrity
--- PASS: TestAdversarialCapability_BitmaskCategoryIntegrity (0.00s)
=== RUN   TestAdversarialCapability_OverlapsAndDisjointness
--- PASS: TestAdversarialCapability_OverlapsAndDisjointness (0.00s)
=== RUN   TestAdversarialCapability_LevelMaskThresholdStrictness
--- PASS: TestAdversarialCapability_LevelMaskThresholdStrictness (0.00s)
=== RUN   TestAdversarialCapability_WeirdRequestedLevels
=== RUN   TestAdversarialCapability_WeirdRequestedLevels/ZeroManifest_ReqLevel-1
=== RUN   TestAdversarialCapability_WeirdRequestedLevels/ZeroManifest_ReqLevel-999
=== RUN   TestAdversarialCapability_WeirdRequestedLevels/ZeroManifest_ReqLevel4
=== RUN   TestAdversarialCapability_WeirdRequestedLevels/ZeroManifest_ReqLevel100
=== RUN   TestAdversarialCapability_WeirdRequestedLevels/ZeroManifest_ReqLevel2147483647
=== RUN   TestAdversarialCapability_WeirdRequestedLevels/FullManifest_ReqLevel-1
=== RUN   TestAdversarialCapability_WeirdRequestedLevels/FullManifest_ReqLevel-999
=== RUN   TestAdversarialCapability_WeirdRequestedLevels/FullManifest_ReqLevel4
=== RUN   TestAdversarialCapability_WeirdRequestedLevels/FullManifest_ReqLevel100
=== RUN   TestAdversarialCapability_WeirdRequestedLevels/FullManifest_ReqLevel2147483647
--- PASS: TestAdversarialCapability_WeirdRequestedLevels (0.00s)
    --- PASS: TestAdversarialCapability_WeirdRequestedLevels/ZeroManifest_ReqLevel-1 (0.00s)
    --- PASS: TestAdversarialCapability_WeirdRequestedLevels/ZeroManifest_ReqLevel-999 (0.00s)
    --- PASS: TestAdversarialCapability_WeirdRequestedLevels/ZeroManifest_ReqLevel4 (0.00s)
    --- PASS: TestAdversarialCapability_WeirdRequestedLevels/ZeroManifest_ReqLevel100 (0.00s)
    --- PASS: TestAdversarialCapability_WeirdRequestedLevels/ZeroManifest_ReqLevel2147483647 (0.00s)
    --- PASS: TestAdversarialCapability_WeirdRequestedLevels/FullManifest_ReqLevel-1 (0.00s)
    --- PASS: TestAdversarialCapability_WeirdRequestedLevels/FullManifest_ReqLevel-999 (0.00s)
    --- PASS: TestAdversarialCapability_WeirdRequestedLevels/FullManifest_ReqLevel4 (0.00s)
    --- PASS: TestAdversarialCapability_WeirdRequestedLevels/FullManifest_ReqLevel100 (0.00s)
    --- PASS: TestAdversarialCapability_WeirdRequestedLevels/FullManifest_ReqLevel2147483647 (0.00s)
=== RUN   TestAdversarialCapability_ZeroMasks
=== RUN   TestAdversarialCapability_ZeroMasks/EvaluateAchievableLevelFromMask_Zero
=== RUN   TestAdversarialCapability_ZeroMasks/FromBitmask_Zero
=== RUN   TestAdversarialCapability_ZeroMasks/CapabilityManifest_ZeroStruct_ToBitmask
=== RUN   TestAdversarialCapability_ZeroMasks/NegotiateLevel_ZeroManifest_RequestedLevel0
=== RUN   TestAdversarialCapability_ZeroMasks/NegotiateLevel_ZeroManifest_RequestedLevel3_Degradation
=== RUN   TestAdversarialCapability_ZeroMasks/NegativeIntegrationLevel_NoBooleans_Degradation
--- PASS: TestAdversarialCapability_ZeroMasks (0.00s)
    --- PASS: TestAdversarialCapability_ZeroMasks/EvaluateAchievableLevelFromMask_Zero (0.00s)
    --- PASS: TestAdversarialCapability_ZeroMasks/FromBitmask_Zero (0.00s)
    --- PASS: TestAdversarialCapability_ZeroMasks/CapabilityManifest_ZeroStruct_ToBitmask (0.00s)
    --- PASS: TestAdversarialCapability_ZeroMasks/NegotiateLevel_ZeroManifest_RequestedLevel0 (0.00s)
    --- PASS: TestAdversarialCapability_ZeroMasks/NegotiateLevel_ZeroManifest_RequestedLevel3_Degradation (0.00s)
    --- PASS: TestAdversarialCapability_ZeroMasks/NegativeIntegrationLevel_NoBooleans_Degradation (0.00s)
=== RUN   TestAdversarialCapability_BitFlips
=== RUN   TestAdversarialCapability_BitFlips/Level3_SingleBitCleared_Degradation
=== RUN   TestAdversarialCapability_BitFlips/Level2_SingleBitCleared_Degradation
=== RUN   TestAdversarialCapability_BitFlips/Level1_SingleBitCleared_Degradation
=== RUN   TestAdversarialCapability_BitFlips/CapEventStreamCleared_TotalDegradation
=== RUN   TestAdversarialCapability_BitFlips/HighBitsSet_NoEffectOnEvaluation
--- PASS: TestAdversarialCapability_BitFlips (0.00s)
    --- PASS: TestAdversarialCapability_BitFlips/Level3_SingleBitCleared_Degradation (0.00s)
    --- PASS: TestAdversarialCapability_BitFlips/Level2_SingleBitCleared_Degradation (0.00s)
    --- PASS: TestAdversarialCapability_BitFlips/Level1_SingleBitCleared_Degradation (0.00s)
    --- PASS: TestAdversarialCapability_BitFlips/CapEventStreamCleared_TotalDegradation (0.00s)
    --- PASS: TestAdversarialCapability_BitFlips/HighBitsSet_NoEffectOnEvaluation (0.00s)
=== RUN   TestAdversarialCapability_InvalidStructPointers
=== RUN   TestAdversarialCapability_InvalidStructPointers/NilHandshakeRequest
=== RUN   TestAdversarialCapability_InvalidStructPointers/NilManifestPointerInEvaluateAchievableLevel
=== RUN   TestAdversarialCapability_InvalidStructPointers/EmptySessionIDInHandshakeRequest
--- PASS: TestAdversarialCapability_InvalidStructPointers (0.00s)
    --- PASS: TestAdversarialCapability_InvalidStructPointers/NilHandshakeRequest (0.00s)
    --- PASS: TestAdversarialCapability_InvalidStructPointers/NilManifestPointerInEvaluateAchievableLevel (0.00s)
    --- PASS: TestAdversarialCapability_InvalidStructPointers/EmptySessionIDInHandshakeRequest (0.00s)
=== RUN   TestAdversarialCapability_MissingFlagStringRepresentations
=== RUN   TestAdversarialCapability_MissingFlagStringRepresentations/UndefinedCapabilityFlags
=== RUN   TestAdversarialCapability_MissingFlagStringRepresentations/All20CanonicalFlags_HaveValidStringRepresentation
=== RUN   TestAdversarialCapability_MissingFlagStringRepresentations/DegradationResponse_MissingFlagsStringValues
--- PASS: TestAdversarialCapability_MissingFlagStringRepresentations (0.00s)
    --- PASS: TestAdversarialCapability_MissingFlagStringRepresentations/UndefinedCapabilityFlags (0.00s)
    --- PASS: TestAdversarialCapability_MissingFlagStringRepresentations/All20CanonicalFlags_HaveValidStringRepresentation (0.00s)
    --- PASS: TestAdversarialCapability_MissingFlagStringRepresentations/DegradationResponse_MissingFlagsStringValues (0.00s)
=== RUN   TestChallenger_ConcurrentStress
--- PASS: TestChallenger_ConcurrentStress (0.50s)
=== RUN   TestChallenger_SchemaTypeNormalization
=== RUN   TestChallenger_SchemaTypeNormalization/Type_AgentSession
=== RUN   TestChallenger_SchemaTypeNormalization/Type_agent_session
=== RUN   TestChallenger_SchemaTypeNormalization/Type_AGENT_SESSION
=== RUN   TestChallenger_SchemaTypeNormalization/Type_agentSession
=== RUN   TestChallenger_SchemaTypeNormalization/Type_Agent_Session
=== RUN   TestChallenger_SchemaTypeNormalization/Type_
=== RUN   TestChallenger_SchemaTypeNormalization/Type_UnknownType
=== RUN   TestChallenger_SchemaTypeNormalization/Type_agent_session_extra
--- PASS: TestChallenger_SchemaTypeNormalization (0.00s)
    --- PASS: TestChallenger_SchemaTypeNormalization/Type_AgentSession (0.00s)
    --- PASS: TestChallenger_SchemaTypeNormalization/Type_agent_session (0.00s)
    --- PASS: TestChallenger_SchemaTypeNormalization/Type_AGENT_SESSION (0.00s)
    --- PASS: TestChallenger_SchemaTypeNormalization/Type_agentSession (0.00s)
    --- PASS: TestChallenger_SchemaTypeNormalization/Type_Agent_Session (0.00s)
    --- PASS: TestChallenger_SchemaTypeNormalization/Type_ (0.00s)
    --- PASS: TestChallenger_SchemaTypeNormalization/Type_UnknownType (0.00s)
    --- PASS: TestChallenger_SchemaTypeNormalization/Type_agent_session_extra (0.00s)
=== RUN   TestChallenger_BoundaryAndEdgeCases
=== RUN   TestChallenger_BoundaryAndEdgeCases/EmptyPayload
=== RUN   TestChallenger_BoundaryAndEdgeCases/NullPayload
=== RUN   TestChallenger_BoundaryAndEdgeCases/EmptyJSONObject
=== RUN   TestChallenger_BoundaryAndEdgeCases/ExtraPropertiesViolation
=== RUN   TestChallenger_BoundaryAndEdgeCases/MinLengthViolation_EmptySessionID
=== RUN   TestChallenger_BoundaryAndEdgeCases/DateTimeFormatInvalid
=== RUN   TestChallenger_BoundaryAndEdgeCases/IntegrationLevelNegative
=== RUN   TestChallenger_BoundaryAndEdgeCases/IntegrationLevelAboveMax
=== RUN   TestChallenger_BoundaryAndEdgeCases/OptionalEndedAtValid
=== RUN   TestChallenger_BoundaryAndEdgeCases/EndedAtNullHandling
--- PASS: TestChallenger_BoundaryAndEdgeCases (0.00s)
    --- PASS: TestChallenger_BoundaryAndEdgeCases/EmptyPayload (0.00s)
    --- PASS: TestChallenger_BoundaryAndEdgeCases/NullPayload (0.00s)
    --- PASS: TestChallenger_BoundaryAndEdgeCases/EmptyJSONObject (0.00s)
    --- PASS: TestChallenger_BoundaryAndEdgeCases/ExtraPropertiesViolation (0.00s)
    --- PASS: TestChallenger_BoundaryAndEdgeCases/MinLengthViolation_EmptySessionID (0.00s)
    --- PASS: TestChallenger_BoundaryAndEdgeCases/DateTimeFormatInvalid (0.00s)
    --- PASS: TestChallenger_BoundaryAndEdgeCases/IntegrationLevelNegative (0.00s)
    --- PASS: TestChallenger_BoundaryAndEdgeCases/IntegrationLevelAboveMax (0.00s)
    --- PASS: TestChallenger_BoundaryAndEdgeCases/OptionalEndedAtValid (0.00s)
    --- PASS: TestChallenger_BoundaryAndEdgeCases/EndedAtNullHandling (0.00s)
=== RUN   TestChallenger_All22SchemasValidation
=== RUN   TestChallenger_All22SchemasValidation/agent_session
=== RUN   TestChallenger_All22SchemasValidation/task_envelope
=== RUN   TestChallenger_All22SchemasValidation/agent_event
=== RUN   TestChallenger_All22SchemasValidation/tool_call_event
=== RUN   TestChallenger_All22SchemasValidation/file_change_event
=== RUN   TestChallenger_All22SchemasValidation/test_result_event
=== RUN   TestChallenger_All22SchemasValidation/error_fingerprint
=== RUN   TestChallenger_All22SchemasValidation/evidence_item
=== RUN   TestChallenger_All22SchemasValidation/evidence_pack
=== RUN   TestChallenger_All22SchemasValidation/hypothesis
=== RUN   TestChallenger_All22SchemasValidation/assumption
=== RUN   TestChallenger_All22SchemasValidation/tunnel_signal
=== RUN   TestChallenger_All22SchemasValidation/tunnel_assessment
=== RUN   TestChallenger_All22SchemasValidation/review_request
=== RUN   TestChallenger_All22SchemasValidation/review_decision
=== RUN   TestChallenger_All22SchemasValidation/intervention
=== RUN   TestChallenger_All22SchemasValidation/budget_state
=== RUN   TestChallenger_All22SchemasValidation/capability_manifest
=== RUN   TestChallenger_All22SchemasValidation/checkpoint
=== RUN   TestChallenger_All22SchemasValidation/rollback_result
=== RUN   TestChallenger_All22SchemasValidation/providerUsage
=== RUN   TestChallenger_All22SchemasValidation/audit_record
--- PASS: TestChallenger_All22SchemasValidation (0.00s)
    --- PASS: TestChallenger_All22SchemasValidation/agent_session (0.00s)
    --- PASS: TestChallenger_All22SchemasValidation/task_envelope (0.00s)
    --- PASS: TestChallenger_All22SchemasValidation/agent_event (0.00s)
    --- PASS: TestChallenger_All22SchemasValidation/tool_call_event (0.00s)
    --- PASS: TestChallenger_All22SchemasValidation/file_change_event (0.00s)
    --- PASS: TestChallenger_All22SchemasValidation/test_result_event (0.00s)
    --- PASS: TestChallenger_All22SchemasValidation/error_fingerprint (0.00s)
    --- PASS: TestChallenger_All22SchemasValidation/evidence_item (0.00s)
    --- PASS: TestChallenger_All22SchemasValidation/evidence_pack (0.00s)
    --- PASS: TestChallenger_All22SchemasValidation/hypothesis (0.00s)
    --- PASS: TestChallenger_All22SchemasValidation/assumption (0.00s)
    --- PASS: TestChallenger_All22SchemasValidation/tunnel_signal (0.00s)
    --- PASS: TestChallenger_All22SchemasValidation/tunnel_assessment (0.00s)
    --- PASS: TestChallenger_All22SchemasValidation/review_request (0.00s)
    --- PASS: TestChallenger_All22SchemasValidation/review_decision (0.00s)
    --- PASS: TestChallenger_All22SchemasValidation/intervention (0.00s)
    --- PASS: TestChallenger_All22SchemasValidation/budget_state (0.00s)
    --- PASS: TestChallenger_All22SchemasValidation/capability_manifest (0.00s)
    --- PASS: TestChallenger_All22SchemasValidation/checkpoint (0.00s)
    --- PASS: TestChallenger_All22SchemasValidation/rollback_result (0.00s)
    --- PASS: TestChallenger_All22SchemasValidation/providerUsage (0.00s)
    --- PASS: TestChallenger_All22SchemasValidation/audit_record (0.00s)
=== RUN   TestValidateEvent_ValidPayloads
=== RUN   TestValidateEvent_ValidPayloads/AgentSession
=== RUN   TestValidateEvent_ValidPayloads/TaskEnvelope
=== RUN   TestValidateEvent_ValidPayloads/AgentEvent
=== RUN   TestValidateEvent_ValidPayloads/ToolCallEvent
=== RUN   TestValidateEvent_ValidPayloads/FileChangeEvent
=== RUN   TestValidateEvent_ValidPayloads/TestResultEvent
=== RUN   TestValidateEvent_ValidPayloads/ErrorFingerprint
=== RUN   TestValidateEvent_ValidPayloads/EvidenceItem
=== RUN   TestValidateEvent_ValidPayloads/EvidencePack
=== RUN   TestValidateEvent_ValidPayloads/Hypothesis
=== RUN   TestValidateEvent_ValidPayloads/Assumption
=== RUN   TestValidateEvent_ValidPayloads/TunnelSignal
=== RUN   TestValidateEvent_ValidPayloads/TunnelAssessment
=== RUN   TestValidateEvent_ValidPayloads/ReviewRequest
=== RUN   TestValidateEvent_ValidPayloads/ReviewDecision
=== RUN   TestValidateEvent_ValidPayloads/Intervention
=== RUN   TestValidateEvent_ValidPayloads/BudgetState
=== RUN   TestValidateEvent_ValidPayloads/CapabilityManifest
=== RUN   TestValidateEvent_ValidPayloads/Checkpoint
=== RUN   TestValidateEvent_ValidPayloads/RollbackResult
=== RUN   TestValidateEvent_ValidPayloads/ProviderUsage
=== RUN   TestValidateEvent_ValidPayloads/AuditRecord
--- PASS: TestValidateEvent_ValidPayloads (0.00s)
    --- PASS: TestValidateEvent_ValidPayloads/AgentSession (0.00s)
    --- PASS: TestValidateEvent_ValidPayloads/TaskEnvelope (0.00s)
    --- PASS: TestValidateEvent_ValidPayloads/AgentEvent (0.00s)
    --- PASS: TestValidateEvent_ValidPayloads/ToolCallEvent (0.00s)
    --- PASS: TestValidateEvent_ValidPayloads/FileChangeEvent (0.00s)
    --- PASS: TestValidateEvent_ValidPayloads/TestResultEvent (0.00s)
    --- PASS: TestValidateEvent_ValidPayloads/ErrorFingerprint (0.00s)
    --- PASS: TestValidateEvent_ValidPayloads/EvidenceItem (0.00s)
    --- PASS: TestValidateEvent_ValidPayloads/EvidencePack (0.00s)
    --- PASS: TestValidateEvent_ValidPayloads/Hypothesis (0.00s)
    --- PASS: TestValidateEvent_ValidPayloads/Assumption (0.00s)
    --- PASS: TestValidateEvent_ValidPayloads/TunnelSignal (0.00s)
    --- PASS: TestValidateEvent_ValidPayloads/TunnelAssessment (0.00s)
    --- PASS: TestValidateEvent_ValidPayloads/ReviewRequest (0.00s)
    --- PASS: TestValidateEvent_ValidPayloads/ReviewDecision (0.00s)
    --- PASS: TestValidateEvent_ValidPayloads/Intervention (0.00s)
    --- PASS: TestValidateEvent_ValidPayloads/BudgetState (0.00s)
    --- PASS: TestValidateEvent_ValidPayloads/CapabilityManifest (0.00s)
    --- PASS: TestValidateEvent_ValidPayloads/Checkpoint (0.00s)
    --- PASS: TestValidateEvent_ValidPayloads/RollbackResult (0.00s)
    --- PASS: TestValidateEvent_ValidPayloads/ProviderUsage (0.00s)
    --- PASS: TestValidateEvent_ValidPayloads/AuditRecord (0.00s)
=== RUN   TestValidateEvent_InvalidPayloads
=== RUN   TestValidateEvent_InvalidPayloads/UnknownSchemaType
=== RUN   TestValidateEvent_InvalidPayloads/MalformedJSON
=== RUN   TestValidateEvent_InvalidPayloads/MissingRequiredField_AgentSession
=== RUN   TestValidateEvent_InvalidPayloads/MissingRequiredField_TaskEnvelope
=== RUN   TestValidateEvent_InvalidPayloads/TypeMismatch_ToolCallEvent
=== RUN   TestValidateEvent_InvalidPayloads/TypeMismatch_AgentSessionID
=== RUN   TestValidateEvent_InvalidPayloads/InvalidEnum_FileChangeEvent
=== RUN   TestValidateEvent_InvalidPayloads/InvalidEnum_AgentSessionStatus
=== RUN   TestValidateEvent_InvalidPayloads/OutOfBoundScore_TunnelSignal
=== RUN   TestValidateEvent_InvalidPayloads/OutOfBoundScore_TunnelAssessment
=== RUN   TestValidateEvent_InvalidPayloads/OutOfBoundIntegrationLevel
--- PASS: TestValidateEvent_InvalidPayloads (0.00s)
    --- PASS: TestValidateEvent_InvalidPayloads/UnknownSchemaType (0.00s)
    --- PASS: TestValidateEvent_InvalidPayloads/MalformedJSON (0.00s)
    --- PASS: TestValidateEvent_InvalidPayloads/MissingRequiredField_AgentSession (0.00s)
    --- PASS: TestValidateEvent_InvalidPayloads/MissingRequiredField_TaskEnvelope (0.00s)
    --- PASS: TestValidateEvent_InvalidPayloads/TypeMismatch_ToolCallEvent (0.00s)
    --- PASS: TestValidateEvent_InvalidPayloads/TypeMismatch_AgentSessionID (0.00s)
    --- PASS: TestValidateEvent_InvalidPayloads/InvalidEnum_FileChangeEvent (0.00s)
    --- PASS: TestValidateEvent_InvalidPayloads/InvalidEnum_AgentSessionStatus (0.00s)
    --- PASS: TestValidateEvent_InvalidPayloads/OutOfBoundScore_TunnelSignal (0.00s)
    --- PASS: TestValidateEvent_InvalidPayloads/OutOfBoundScore_TunnelAssessment (0.00s)
    --- PASS: TestValidateEvent_InvalidPayloads/OutOfBoundIntegrationLevel (0.00s)
=== RUN   TestStructJSONRoundtrip
=== RUN   TestStructJSONRoundtrip/AgentSession
=== RUN   TestStructJSONRoundtrip/TaskEnvelope
=== RUN   TestStructJSONRoundtrip/AgentEvent
=== RUN   TestStructJSONRoundtrip/ToolCallEvent
=== RUN   TestStructJSONRoundtrip/FileChangeEvent
=== RUN   TestStructJSONRoundtrip/TestResultEvent
=== RUN   TestStructJSONRoundtrip/ErrorFingerprint
=== RUN   TestStructJSONRoundtrip/EvidenceItem
=== RUN   TestStructJSONRoundtrip/EvidencePack
=== RUN   TestStructJSONRoundtrip/Hypothesis
=== RUN   TestStructJSONRoundtrip/Assumption
=== RUN   TestStructJSONRoundtrip/TunnelSignal
=== RUN   TestStructJSONRoundtrip/TunnelAssessment
=== RUN   TestStructJSONRoundtrip/ReviewRequest
=== RUN   TestStructJSONRoundtrip/ReviewDecision
=== RUN   TestStructJSONRoundtrip/Intervention
=== RUN   TestStructJSONRoundtrip/BudgetState
=== RUN   TestStructJSONRoundtrip/CapabilityManifest
=== RUN   TestStructJSONRoundtrip/Checkpoint
=== RUN   TestStructJSONRoundtrip/RollbackResult
=== RUN   TestStructJSONRoundtrip/ProviderUsage
=== RUN   TestStructJSONRoundtrip/AuditRecord
--- PASS: TestStructJSONRoundtrip (0.00s)
    --- PASS: TestStructJSONRoundtrip/AgentSession (0.00s)
    --- PASS: TestStructJSONRoundtrip/TaskEnvelope (0.00s)
    --- PASS: TestStructJSONRoundtrip/AgentEvent (0.00s)
    --- PASS: TestStructJSONRoundtrip/ToolCallEvent (0.00s)
    --- PASS: TestStructJSONRoundtrip/FileChangeEvent (0.00s)
    --- PASS: TestStructJSONRoundtrip/TestResultEvent (0.00s)
    --- PASS: TestStructJSONRoundtrip/ErrorFingerprint (0.00s)
    --- PASS: TestStructJSONRoundtrip/EvidenceItem (0.00s)
    --- PASS: TestStructJSONRoundtrip/EvidencePack (0.00s)
    --- PASS: TestStructJSONRoundtrip/Hypothesis (0.00s)
    --- PASS: TestStructJSONRoundtrip/Assumption (0.00s)
    --- PASS: TestStructJSONRoundtrip/TunnelSignal (0.00s)
    --- PASS: TestStructJSONRoundtrip/TunnelAssessment (0.00s)
    --- PASS: TestStructJSONRoundtrip/ReviewRequest (0.00s)
    --- PASS: TestStructJSONRoundtrip/ReviewDecision (0.00s)
    --- PASS: TestStructJSONRoundtrip/Intervention (0.00s)
    --- PASS: TestStructJSONRoundtrip/BudgetState (0.00s)
    --- PASS: TestStructJSONRoundtrip/CapabilityManifest (0.00s)
    --- PASS: TestStructJSONRoundtrip/Checkpoint (0.00s)
    --- PASS: TestStructJSONRoundtrip/RollbackResult (0.00s)
    --- PASS: TestStructJSONRoundtrip/ProviderUsage (0.00s)
    --- PASS: TestStructJSONRoundtrip/AuditRecord (0.00s)
=== RUN   TestRedactionTags
=== RUN   TestRedactionTags/AgentSession
=== RUN   TestRedactionTags/TaskEnvelope
=== RUN   TestRedactionTags/AgentEvent
=== RUN   TestRedactionTags/ToolCallEvent
=== RUN   TestRedactionTags/FileChangeEvent
=== RUN   TestRedactionTags/TestResultEvent
=== RUN   TestRedactionTags/ErrorFingerprint
=== RUN   TestRedactionTags/EvidenceItem
=== RUN   TestRedactionTags/EvidencePack
=== RUN   TestRedactionTags/Hypothesis
=== RUN   TestRedactionTags/Assumption
=== RUN   TestRedactionTags/TunnelSignal
=== RUN   TestRedactionTags/TunnelAssessment
=== RUN   TestRedactionTags/ReviewRequest
=== RUN   TestRedactionTags/ReviewDecision
=== RUN   TestRedactionTags/Intervention
=== RUN   TestRedactionTags/BudgetState
=== RUN   TestRedactionTags/CapabilityManifest
=== RUN   TestRedactionTags/Checkpoint
=== RUN   TestRedactionTags/RollbackResult
=== RUN   TestRedactionTags/ProviderUsage
=== RUN   TestRedactionTags/AuditRecord
--- PASS: TestRedactionTags (0.00s)
    --- PASS: TestRedactionTags/AgentSession (0.00s)
    --- PASS: TestRedactionTags/TaskEnvelope (0.00s)
    --- PASS: TestRedactionTags/AgentEvent (0.00s)
    --- PASS: TestRedactionTags/ToolCallEvent (0.00s)
    --- PASS: TestRedactionTags/FileChangeEvent (0.00s)
    --- PASS: TestRedactionTags/TestResultEvent (0.00s)
    --- PASS: TestRedactionTags/ErrorFingerprint (0.00s)
    --- PASS: TestRedactionTags/EvidenceItem (0.00s)
    --- PASS: TestRedactionTags/EvidencePack (0.00s)
    --- PASS: TestRedactionTags/Hypothesis (0.00s)
    --- PASS: TestRedactionTags/Assumption (0.00s)
    --- PASS: TestRedactionTags/TunnelSignal (0.00s)
    --- PASS: TestRedactionTags/TunnelAssessment (0.00s)
    --- PASS: TestRedactionTags/ReviewRequest (0.00s)
    --- PASS: TestRedactionTags/ReviewDecision (0.00s)
    --- PASS: TestRedactionTags/Intervention (0.00s)
    --- PASS: TestRedactionTags/BudgetState (0.00s)
    --- PASS: TestRedactionTags/CapabilityManifest (0.00s)
    --- PASS: TestRedactionTags/Checkpoint (0.00s)
    --- PASS: TestRedactionTags/RollbackResult (0.00s)
    --- PASS: TestRedactionTags/ProviderUsage (0.00s)
    --- PASS: TestRedactionTags/AuditRecord (0.00s)
PASS
ok  	github.com/reinframe/reinframe/pkg/protocol	1.966s
```

## 2. Logic Chain

1. **Root Cause Analysis of Iteration 1 Failure**:
   - In Iteration 1, converting a raw bitmask via `FromBitmask` to `CapabilityManifest` was a lossy transformation because `CapabilityManifest` only contained 6 explicit boolean fields (`SupportsPause`, `SupportsCancel`, `SupportsResume`, `SupportsCheckpoint`, `SupportsRollback`, `SupportsMCP`) plus `IntegrationLevel`.
   - Any bits in a uint64 bitmask that were isolated (such as `CapSDK` at bit 19 or undefined bits 20..63) or belonged to a supervision level mask whose threshold was not fully satisfied were lost during conversion.
   - When `ToBitmask()` was subsequently called on the converted `CapabilityManifest`, it reconstructed the bitmask from the 6 boolean flags and `IntegrationLevel`. For `mask = 0`, `IntegrationLevel 0` forced the addition of `Level0RequiredMask` (`CapEventStream`, bit 0 = 0x1), causing `FromBitmask(0).ToBitmask()` to return `0x1` instead of `0x0`.

2. **Unexported Raw Bitmask Preservation Design**:
   - Adding unexported fields `rawBitmask uint64` and `hasRawBitmask bool` to `CapabilityManifest` in `pkg/protocol/schema.go` solves the bitmask loss issue completely.
   - Because `rawBitmask` and `hasRawBitmask` are unexported in Go, Go's `encoding/json` package ignores them during JSON marshaling and unmarshaling.
   - When `CapabilityManifest` is serialized to JSON or validated by `ValidateEvent` against `pkg/protocol/schemas/capability_manifest.json`, the JSON output contains strictly the 9 exported fields. Schema validation with `"additionalProperties": false` passes 100% cleanly.
   - When `FromBitmask(mask)` is called, it populates `rawBitmask: mask` and `hasRawBitmask: true`.
   - When `ToBitmask()` is called, if `m.hasRawBitmask` is true, it returns `m.rawBitmask` immediately. Otherwise, it falls back to reconstructing the bitmask from `IntegrationLevel` and the 6 boolean fields.

3. **Method & Test Validation**:
   - `FromBitmask(0).ToBitmask()` returns `0x0` losslessly.
   - `FromBitmask(0xFFFFFFFFFFFFFFFF).ToBitmask()` returns `0xFFFFFFFFFFFFFFFF` losslessly.
   - `HasCapability(flag)` evaluates `(m.ToBitmask() & uint64(flag)) == uint64(flag)`, returning true for single and compound capability flags when present.
   - `TestChallenger_BoundaryBitmasks` in `pkg/protocol/capability_test.go` asserts lossless bitmask preservation for `Zero_bitmask`, `Full_uint64_bitmask`, `CapSDK`, `Bit_20`, `Bit_63`, and off-by-one required masks.

---

## 3. Caveats

- **Unmarshaled JSON Manifests**: Manifests constructed via standard JSON unmarshaling or struct literals without calling `FromBitmask` will have `hasRawBitmask == false`. In this case, `ToBitmask()` uses the standard fallback computation based on `IntegrationLevel` and the 6 boolean flags, which is the expected behavior for network-received JSON manifests.
- **No Caveats**: No other caveats remain. All unit, race, and e2e tests pass cleanly across the repository.

---

## 4. Conclusion

- The implementation in `pkg/protocol/schema.go` and `pkg/protocol/capability.go` fully resolves the Iteration 1 failure while preserving 100% JSON schema validation compatibility (`additionalProperties: false`).
- All tests in `pkg/protocol/...` pass with 0 failures and 0 race warnings under `go test -v -count=1 -race ./pkg/protocol/...`.
- All tests across the entire repository (`go test ./...`) pass cleanly.

---

## 5. Verification Method

To independently verify this work:

1. **Run Protocol Package Test Suite with Race Detector**:
   ```bash
   go test -v -count=1 -race ./pkg/protocol/...
   ```
   Verify exit code 0, 0 test failures, and 0 race condition warnings.

2. **Verify Boundary Bitmasks & Schema Validation Specifically**:
   ```bash
   go test -v -count=1 -race -run "TestChallenger_BoundaryBitmasks|TestValidateEvent" ./pkg/protocol/...
   ```
   Verify `TestChallenger_BoundaryBitmasks` and `TestValidateEvent` pass 100%.

3. **Run Workspace End-to-End Tests**:
   ```bash
   go test ./...
   ```
   Verify all packages (`pkg/protocol`, `pkg/state`, `tests/e2e`) pass with status `ok`.
