package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// conformingReport / nonConformingReport mirror the exact Turtle shape Fuseki 5.5.0's /shacl
// endpoint returns (captured live): a sh:ValidationReport blank node with sh:conforms and, when
// false, one sh:result [ … sh:ValidationResult ] block per violation.
const conformingReport = `PREFIX rdf:  <http://www.w3.org/1999/02/22-rdf-syntax-ns#>
PREFIX sh:   <http://www.w3.org/ns/shacl#>

[ rdf:type     sh:ValidationReport;
  sh:conforms  true
] .`

const nonConformingReport = `PREFIX gs:   <http://gemetenstad.nl/ns#>
PREFIX data: <http://gemetenstad.nl/id/>
PREFIX sh:   <http://www.w3.org/ns/shacl#>

[ rdf:type     sh:ValidationReport;
  sh:conforms  false;
  sh:result    [ rdf:type                      sh:ValidationResult;
                 sh:focusNode                  data:i;
                 sh:resultMessage              "An Intervention must be locatedAt at least one Place.";
                 sh:resultPath                 gs:locatedAt;
                 sh:resultSeverity             sh:Violation;
                 sh:sourceConstraintComponent  sh:MinCountConstraintComponent;
                 sh:sourceShape                []
               ]
] .`

const twoViolationReport = `PREFIX data: <http://gemetenstad.nl/id/>
PREFIX sh:   <http://www.w3.org/ns/shacl#>

[ rdf:type     sh:ValidationReport;
  sh:conforms  false;
  sh:result    [ rdf:type          sh:ValidationResult;
                 sh:focusNode      data:i ;
                 sh:resultMessage  "An Intervention must be locatedAt at least one Place." ] ;
  sh:result    [ rdf:type          sh:ValidationResult;
                 sh:focusNode      data:j ;
                 sh:resultMessage  "locatedAt edge has no gs:confidence annotation." ]
] .`

func TestParseConforms(t *testing.T) {
	tests := []struct {
		name         string
		report       string
		wantConforms bool
		wantErr      bool
		detailHas    []string // substrings the detail must contain (non-conforming only)
	}{
		{
			name:         "conforming report",
			report:       conformingReport,
			wantConforms: true,
		},
		{
			name:         "single-space conforms true is tolerated",
			report:       "[ a sh:ValidationReport ; sh:conforms true ] .",
			wantConforms: true,
		},
		{
			name:         "non-conforming names focus node and message",
			report:       nonConformingReport,
			wantConforms: false,
			detailHas:    []string{"data:i", "An Intervention must be locatedAt at least one Place."},
		},
		{
			name:         "non-conforming reports every violation",
			report:       twoViolationReport,
			wantConforms: false,
			detailHas:    []string{"data:i", "data:j", "no gs:confidence annotation"},
		},
		{
			name:    "empty report is an error",
			report:  "",
			wantErr: true,
		},
		{
			name:    "whitespace-only report is an error",
			report:  "   \n\t ",
			wantErr: true,
		},
		{
			name:    "report without sh:conforms is an error",
			report:  "PREFIX sh: <http://www.w3.org/ns/shacl#>\n[ a sh:ValidationReport ] .",
			wantErr: true,
		},
		{
			name:    "conforms false but no result blocks is an error",
			report:  "[ a sh:ValidationReport ; sh:conforms false ] .",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conforms, detail, err := parseConforms([]byte(tt.report))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantConforms, conforms)
			if tt.wantConforms {
				assert.Empty(t, detail)
			}
			for _, sub := range tt.detailHas {
				assert.Contains(t, detail, sub)
			}
		})
	}
}
