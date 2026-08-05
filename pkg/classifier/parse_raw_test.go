package classifier_test

import (
	"strings"
	"testing"

	"github.com/ImL1s/reinframe/pkg/classifier"
)

func TestParseRawAssessmentStrict_Success(t *testing.T) {
	t.Parallel()
	in := []byte(`{"schema_version":"reinframe.raw_assessment.v1","severity":42,"reason_code":"NORMAL_PROGRESS","evidence_event_ids":["e1"]}`)
	a, err := classifier.ParseRawAssessmentStrict(in, 8192, map[string]struct{}{"e1": {}})
	if err != nil {
		t.Fatal(err)
	}
	if a.Severity != 42 || a.ReasonCode != "NORMAL_PROGRESS" {
		t.Fatalf("%+v", a)
	}
}

func TestParseRawAssessmentStrict_Adversarial(t *testing.T) {
	t.Parallel()
	allowed := map[string]struct{}{"e1": {}, "e2": {}}
	cases := []struct {
		name string
		body string
		want string // substring of error reason
	}{
		{"markdown_fence", "```json\n{\"schema_version\":\"reinframe.raw_assessment.v1\",\"severity\":1,\"reason_code\":\"NORMAL_PROGRESS\"}\n```", "fence"},
		{"prose_prefix", "here you go {\"schema_version\":\"reinframe.raw_assessment.v1\",\"severity\":1,\"reason_code\":\"NORMAL_PROGRESS\"}", "prose"},
		{"unknown_field", `{"schema_version":"reinframe.raw_assessment.v1","severity":1,"reason_code":"NORMAL_PROGRESS","extra":true}`, "unknown_field"},
		{"duplicate_key", `{"schema_version":"reinframe.raw_assessment.v1","severity":1,"severity":2,"reason_code":"NORMAL_PROGRESS"}`, "duplicate"},
		{"multiple_objects", `{"schema_version":"reinframe.raw_assessment.v1","severity":1,"reason_code":"NORMAL_PROGRESS"}{"schema_version":"reinframe.raw_assessment.v1","severity":2,"reason_code":"NORMAL_PROGRESS"}`, "multiple"},
		{"float_severity", `{"schema_version":"reinframe.raw_assessment.v1","severity":1.5,"reason_code":"NORMAL_PROGRESS"}`, "integer"},
		{"string_severity", `{"schema_version":"reinframe.raw_assessment.v1","severity":"10","reason_code":"NORMAL_PROGRESS"}`, "string"},
		{"severity_range", `{"schema_version":"reinframe.raw_assessment.v1","severity":101,"reason_code":"NORMAL_PROGRESS"}`, "range"},
		{"unknown_reason", `{"schema_version":"reinframe.raw_assessment.v1","severity":10,"reason_code":"NOT_A_CODE"}`, "reason_code"},
		{"unknown_evidence", `{"schema_version":"reinframe.raw_assessment.v1","severity":10,"reason_code":"NORMAL_PROGRESS","evidence_event_ids":["nope"]}`, "evidence"},
		{"duplicate_evidence", `{"schema_version":"reinframe.raw_assessment.v1","severity":10,"reason_code":"NORMAL_PROGRESS","evidence_event_ids":["e1","e1"]}`, "duplicate"},
		{"usage_inject", `{"schema_version":"reinframe.raw_assessment.v1","severity":10,"reason_code":"NORMAL_PROGRESS","input_tokens":999}`, "forbidden"},
		{"cache_hit_inject", `{"schema_version":"reinframe.raw_assessment.v1","severity":10,"reason_code":"NORMAL_PROGRESS","cache_hit":true}`, "forbidden"},
		{"provider_request_id_inject", `{"schema_version":"reinframe.raw_assessment.v1","severity":10,"reason_code":"NORMAL_PROGRESS","provider_request_id":"x"}`, "forbidden"},
		{"missing_severity", `{"schema_version":"reinframe.raw_assessment.v1","reason_code":"NORMAL_PROGRESS"}`, "missing"},
		{"invalid_utf8", string([]byte{0xff, 0xfe, '{'}), "utf8"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := classifier.ParseRawAssessmentStrict([]byte(tc.body), 8192, allowed)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				// reason may appear as parse <reason>
				if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.want)) {
					t.Fatalf("err=%v want substring %q", err, tc.want)
				}
			}
		})
	}
}

func TestParseRawAssessmentStrict_Oversized(t *testing.T) {
	t.Parallel()
	body := `{"schema_version":"reinframe.raw_assessment.v1","severity":1,"reason_code":"NORMAL_PROGRESS","pad":"` + strings.Repeat("x", 200) + `"}`
	_, err := classifier.ParseRawAssessmentStrict([]byte(body), 50, nil)
	if err == nil {
		t.Fatal("expected oversized")
	}
}
