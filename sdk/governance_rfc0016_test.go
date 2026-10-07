package sdk

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
)

// rfc0016PackJSON carries every field RFC 0016 adds: the three governance
// fields, Validator.id, and extensions on each definition that gained one.
const rfc0016PackJSON = `{
  "id": "rfc0016",
  "name": "RFC 0016",
  "version": "1.0.0",
  "template_engine": {"version": "v1", "syntax": "{{variable}}"},
  "prompts": {
    "billing": {
      "id": "billing", "name": "Billing", "version": "1.0.0",
      "system_template": "You handle billing.",
      "validators": [{"id": "no-card-numbers", "type": "banned_words",
        "params": {"words": ["4111"]}, "extensions": {"acme:control": "PCI-3.4"}}],
      "evals": [{"id": "tone", "type": "contains", "trigger": "every_turn",
        "params": {"patterns": ["thanks"]}, "extensions": {"acme:metric": "csat"}}],
      "extensions": {"acme:tier": "gold"}
    }
  },
  "workflow": {
    "version": 1, "entry": "billing",
    "states": {"billing": {"prompt_task": "billing", "extensions": {"acme:gate": "pci"}}}
  },
  "metadata": {
    "governance": {
      "accountable_owner": "payments-risk",
      "independent_of": {"axes": ["accountable_owner", "model"], "enforcement": "strict"},
      "obligations": [{
        "id": "pci", "obligation": "acme:PCI-DSS",
        "applies_to": {"data_class": "dpv:FinancialData"},
        "controls": [{"validator": "no-card-numbers"}, {"eval": "tone"},
          {"field": "accountable_owner"}, {"external": "quarterly QSA audit"}],
        "note": "Requirement 3.4",
        "extensions": {"acme:framework": "PCI-DSS-4.0"}
      }],
      "reviews": [{
        "id": "pci-review", "type": "acme:PCIReview", "cadence": "P3M", "owner": "payments-risk",
        "satisfies": ["pci"], "eval": "tone", "extensions": {"acme:evidence": "s3://audits/pci"}
      }]
    }
  },
  "agents": {
    "entry": "billing",
    "members": {"billing": {"description": "Billing agent", "extensions": {"acme:approver": "risk-oncall"}}}
  }
}`

// TestRFC0016PackLoadsThroughOpenAndReadsBack opens the pack with schema
// validation on, so the embedded schema accepts every new field, and reads each
// governance field back through the public accessor.
func TestRFC0016PackLoadsThroughOpenAndReadsBack(t *testing.T) {
	path := createTestPackFile(t, rfc0016PackJSON)
	prov := mock.NewProviderWithRepository("m", "m", false, mock.NewInMemoryMockRepository("ok"))

	conv, err := Open(path, "billing", WithProvider(prov))
	require.NoError(t, err)
	defer conv.Close()

	g := conv.Governance()
	require.NotNil(t, g)

	require.Equal(t, []any{"accountable_owner", "model"}, g.IndependentOf.Axes)
	require.Equal(t, "strict", *g.IndependentOf.Enforcement)

	require.Len(t, g.Obligations, 1)
	o := g.Obligations[0]
	require.Equal(t, "pci", o.ID)
	require.Equal(t, "acme:PCI-DSS", o.Obligation)
	require.Equal(t, "dpv:FinancialData", o.AppliesTo.DataClass)
	require.Equal(t, "Requirement 3.4", o.Note)
	require.Equal(t, map[string]any{"acme:framework": "PCI-DSS-4.0"}, o.Extensions)
	require.Len(t, o.Controls, 4)
	require.Equal(t, "no-card-numbers", o.Controls[0].Validator)
	require.Equal(t, "tone", o.Controls[1].Eval)
	require.Equal(t, "accountable_owner", o.Controls[2].Field)
	require.Equal(t, "quarterly QSA audit", o.Controls[3].External)

	require.Len(t, g.Reviews, 1)
	r := g.Reviews[0]
	require.Equal(t, "pci-review", r.ID)
	require.Equal(t, "acme:PCIReview", r.Type)
	require.Equal(t, "P3M", r.Cadence)
	require.Equal(t, "payments-risk", r.Owner)
	require.Equal(t, []string{"pci"}, r.Satisfies)
	require.Equal(t, "tone", r.Eval)
	require.Equal(t, map[string]any{"acme:evidence": "s3://audits/pci"}, r.Extensions)

	// The non-governance declarations are carried on the loaded pack.
	p := conv.pack
	require.Equal(t, map[string]any{"acme:tier": "gold"}, p.Prompts["billing"].Extensions)
	require.Equal(t, "no-card-numbers", p.Prompts["billing"].Validators[0].ID)
	require.Equal(t, map[string]any{"acme:control": "PCI-3.4"}, p.Prompts["billing"].Validators[0].Extensions)
	require.Equal(t, map[string]any{"acme:metric": "csat"}, p.Prompts["billing"].Evals[0].Extensions)
	require.Equal(t, map[string]any{"acme:gate": "pci"}, p.Workflow.States["billing"].Extensions)
	require.Equal(t, map[string]any{"acme:approver": "risk-oncall"}, p.Agents.Members["billing"].Extensions)
}

// TestRFC0016CadenceIsSchemaChecked — the schema's cadence pattern rejects the
// empty duration. This is the pattern 1.8.0 wrote with lookaheads, which Go's
// regexp cannot compile; it pins that the embedded schema still compiles and
// still enforces the rule.
func TestRFC0016CadenceIsSchemaChecked(t *testing.T) {
	require.Equal(t, 1, strings.Count(rfc0016PackJSON, `"cadence": "P3M"`))
	bad := strings.Replace(rfc0016PackJSON, `"cadence": "P3M"`, `"cadence": "P"`, 1)
	path := createTestPackFile(t, bad)
	prov := mock.NewProviderWithRepository("m", "m", false, mock.NewInMemoryMockRepository("ok"))

	_, err := Open(path, "billing", WithProvider(prov))
	require.Error(t, err)
	require.Contains(t, err.Error(), "cadence")
}

// TestUnresolvedGovernanceReferenceFailsOpen — RFC 0016 rules 4 to 7 make a
// pack with a dangling reference invalid, so it fails at Open the same way an
// unresolved workflow or agent reference does.
func TestUnresolvedGovernanceReferenceFailsOpen(t *testing.T) {
	require.Equal(t, 1, strings.Count(rfc0016PackJSON, `{"validator": "no-card-numbers"}`))
	bad := strings.Replace(rfc0016PackJSON, `{"validator": "no-card-numbers"}`, `{"validator": "missing"}`, 1)
	path := createTestPackFile(t, bad)
	prov := mock.NewProviderWithRepository("m", "m", false, mock.NewInMemoryMockRepository("ok"))

	_, err := Open(path, "billing", WithProvider(prov))

	var govErr *pack.GovernanceValidationError
	require.ErrorAs(t, err, &govErr)
	require.Equal(t, []string{
		`metadata.governance.obligations[0].controls[0]: validator "missing" does not resolve ` +
			`to a validator id on any prompt`,
	}, govErr.Errors)
}

// TestGovernanceCadenceIsCheckedWithSchemaValidationSkipped — rule 8 is in the
// schema, but a pack opened with WithSkipSchemaValidation must not slip past it.
func TestGovernanceCadenceIsCheckedWithSchemaValidationSkipped(t *testing.T) {
	bad := strings.Replace(rfc0016PackJSON, `"cadence": "P3M"`, `"cadence": "P"`, 1)
	path := createTestPackFile(t, bad)
	prov := mock.NewProviderWithRepository("m", "m", false, mock.NewInMemoryMockRepository("ok"))

	_, err := Open(path, "billing", WithProvider(prov), WithSkipSchemaValidation())

	require.ErrorContains(t, err, `metadata.governance.reviews[0]: cadence "P" is not a non-empty ISO 8601 duration`)
}

// TestValidatePackReportsGovernanceWarnings — an undeclared CURIE prefix is a
// warning: the pack still loads, and preflight reports it as an issue.
func TestValidatePackReportsGovernanceWarnings(t *testing.T) {
	path := createTestPackFile(t, rfc0016PackJSON)

	issues, err := ValidatePack(path, false)

	require.NoError(t, err)
	var gov []string
	for _, is := range issues {
		if is.Kind == "governance" {
			gov = append(gov, is.String())
		}
	}
	// The fixture uses acme: without declaring it, in obligation and review type.
	require.Equal(t, []string{
		`warning governance: metadata.governance.obligations[0].obligation: term "acme:PCI-DSS" uses ` +
			`prefix "acme", which is neither well-known nor declared in vocabularies`,
		`warning governance: metadata.governance.reviews[0].type: term "acme:PCIReview" uses ` +
			`prefix "acme", which is neither well-known nor declared in vocabularies`,
	}, gov)
}
